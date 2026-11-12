# GRAB 0.8 channels, workers, and telemetry

These are implemented features of specification 008. The milestone's complete
release gate, including sustained fault/load acceptance, is tracked separately in
[SCALE-TESTING.md](SCALE-TESTING.md). Ownership remains OpenAI Codex.

## Authorized notifications

`use sockets` provides `sockets.publish using "alias", audience, value`. The alias
must be literal, declared in the host platform manifest, and independently granted
`--allow-channel alias:publish`. Success returns `{accepted: yes}`: the broker
accepted a transient notification, not a durable delivery receipt. Clients must
reconnect and refetch authoritative HTTP state after interruptions. No replay,
offline queue, inferred session, or exactly-once delivery is provided.

Each configured channel reserves a GET upgrade path. A literal `authorize` function
with one request argument is mandatory. It must return exactly
`record {allowed: yes, principal: ..., audiences: [...], expires_at: "...Z"}`.
The principal cannot be `nothing` or a resource. Audiences are unique opaque ASCII
IDs (letters, digits, underscore, hyphen, colon; 1–128 bytes), selected by the
application, never by a client subscription command. Expiry is an RFC3339 timestamp;
expired sessions disconnect. Host `:connect` grants cannot replace this callback.

The configured `receive` callback takes `{principal, audience, value}` in a fresh
execution frame. Its publications are confined to the same channel and audience.
HTTP publication additionally requires a configured `publish_authorize` callback
returning a Boolean for `{context, channel, audience}`. `context` is host-created
request metadata, not a client-supplied principal. Authorization callbacks cannot
publish recursively. Inbox handlers cannot publish before their transaction
commits: use the durable outbox and a separately authorized notification endpoint.

Frames are UTF-8 JSON text:

```json
{"audience":"tenant-123","value":"Catalog changed; refetch /catalog"}
```

Both directions validate the value against the named `client_type`/`server_type`
in the API contract. Duplicate/unknown envelope fields, invalid contracts, binary
messages, and compression are rejected. Redis input is revalidated against the
receiving generation's contract. Exact Origin allowlists are mandatory; missing
Origin requires explicit host opt-in. Query strings are refused. Production uses
WSS, directly or through explicitly trusted TLS-terminating proxies; forwarded
headers from other peers are ignored. Plaintext is limited to explicit development
mode and actual loopback peers. Browser clients should use the application's secure
session authentication; the example's Authorization header is for native test
clients, since browser WebSocket APIs cannot set it.

Defaults: 64 KiB messages; 1,000 connections/channel/process; 16 audiences/session;
100 incoming messages/second; 64 outgoing messages and 1 MiB queued bytes;
16 serialized pending incoming messages; 5-second handler timeout; 20-second ping
interval and 10-second pong/write deadline. Host configuration validates hard
maximums from Section 8. Authorization uses bounded HTTP admissions, independently
of established connections. Queue overflow disconnects instead of blocking other
clients. Close codes include 1007 invalid data, 1008 policy/expiry, 1009 size,
1012 generation replacement, and 1013 overload/broker failure.

Memory transport is single-process. Standalone Redis enables cross-process pub/sub;
Cluster/Sentinel are excluded. Its namespace is separate from cache keys. Use
separate least-privilege Redis ACL identities for cache and pub/sub, verified TLS,
and operator-managed credentials in production. The host never retries writes;
an uncertain Redis publication is not success. Subscription failure makes readiness
fail closed until a healthy replacement generation is loaded.

## Multiple processes

Build the CLI and run the [example](../examples/REALTIME.md). `--workers N` accepts
1–32. More than one worker requires `--worker-base-port P` and a `workers` manifest
budget. Workers listen only on `127.0.0.1:P` through `P+N-1`. An external reverse
proxy/load balancer owns the public listener, TLS, and WebSocket forwarding. GRAB
does not install or configure that proxy. Shared channels require Redis.

The supervisor validates a complete generation, verifies its own executable before
each spawn, hashes source/templates/config/contracts/connector locks, and passes
only necessary OS variables and configured secret references. Children independently
verify the source hash. No shell executes application arguments. Pool admission
requires `N * per-process pools + headroom` within both DB and Redis budgets. A
channel counts its command pool plus one subscription connection. Event concurrency
cannot exceed its owning database pool. Rolling validation also reserves one extra
generation's pool capacity in headroom.

Crashed processes restart after 1, 2, 4, 8, 16, then at most 30 seconds; more than
five restarts in five minutes leaves that worker unready for operator attention.
Private authenticated loopback heartbeats aggregate readiness and command drains.
To expose a stable management endpoint to local operators, configure
`workers.management_port` and `workers.token_env` (at least 32 secret bytes);
`GET /_ready` and `/_health` require `Authorization: Bearer <token>`. Without a
fixed management port, the supervisor generates a private ephemeral IPC credential.
Children terminate after supervisor contact is lost; no independent orphan worker
continues serving indefinitely.

`--reload` validates first, then replaces workers one at a time with a 30-second
drain. New admissions and event claims stop; sockets receive reconnect guidance;
unfinished transactions roll back or claims recover through leases/fencing.
Public contract bytes, database manifests, worker budget policy and per-process
Redis pool totals must remain identical during this conservative rolling mode. Changed contracts require a stopped maintenance deployment. Migrations never
run inside reload. Invalid candidates keep the last healthy generation. Changes
to telemetry topology or management listener require a stopped restart; granted
telemetry secret values rotate only after successful generation validation.

## Optional operational visibility

Telemetry is disabled unless a host declares an alias **and** grants
`--allow-telemetry alias`. The `openmetrics` driver binds a separate literal
loopback listener at `port` (plus worker index in multiprocess mode), serving only
`/metrics` with a host secret bearer token. Never publish this listener through the
ordinary application proxy. Example manifest fragment:

```json
"telemetry": {
  "metrics": {"driver":"openmetrics","port":19090,"token_env":"GRAB_METRICS_TOKEN"},
  "traces": {"driver":"otlp","endpoint":"https://collector.example/v1/traces",
             "token_env":"GRAB_OTLP_TOKEN","sample_rate":0.01}
}
```

OTLP/HTTP JSON requires verified TLS. DNS is pinned; private collectors require
explicit `allowed_cidrs`; metadata/link-local endpoints, redirects and proxy
environment routing are refused. Buffers are capped at 2,048 spans per process
across exporters, batches at 512, flush interval/deadline at five seconds. Export
failure drops telemetry and increments a counter; it never blocks a business
commit. Sampling defaults to 1%. Valid W3C trace parents may correlate requests;
remote sampling flags cannot override local sampling and baggage is discarded.

Metrics cover request admissions/rejections, configured-route latency/status,
connections, cache hits/errors, rate denials, durable work lag/attempts/dead rows,
lease recovery, readiness/restarts, pools/queues, and trace drops. Labels are bounded
configured routes or fixed categories. Traces contain IDs, route template and
timing only. Bodies, raw URLs, users, tenants, tokens, SQL, keys and event payloads
are never telemetry attributes. Durable-work attempts share opaque hashed trace identities and link to the prior
attempt across processes and lease recovery. This does not propagate an HTTP
parent through every external provider; do not infer complete distributed trace
coverage from those attempt links.
