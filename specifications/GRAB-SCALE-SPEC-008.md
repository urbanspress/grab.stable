# GRAB 0.8 — Integrations, client adapters, and server scaling

**Status:** Owner-approved specification and design; implementation in progress  
**Draft:** 1 — November 12, 2026  
**Current Owner:** OpenAI Codex  
**Implementation authorization:** The owner explicitly approved this specification
and the GRAB 0.8 design on November 12, 2026 and directed OpenAI Codex to implement
it. This approval does not waive Section 12 acceptance. The platform/cache slice
is being implemented; the full milestone is not complete. See `docs/CACHE.md`
and `HANDOFF.md` for verified scope and remaining work.

## 1. Authority, scope, and compatibility

Blueprint v1.0 remains the baseline: Sections 6, 8, 9, 12, and 13 establish
server scaling, modular connectors, durable events, least privilege, and release
gates. Blueprint v1.1 Sections 8, 10, 12, and ADR-014 inform the narrowly approved
React/TypeScript integration target; v1.1 is not approved wholesale. The current
repository roadmap controls milestone numbering: database access shipped in 0.7;
connector and advanced-server work are combined here in 0.8.

Included: Redis caching/rate-limit store (`use cache`), explicit memory-only local
testing, typed JSON contracts, generated TypeScript and React clients (ADR-014),
connector SDK/packages, verified webhooks, durable inbox/outbox retries,
WebSocket channels, supervised worker processes, metrics, tracing, and fault/load
verification. These are release requirements, not optional unnamed future tracks.

Preserve specifications 001–007, AST execution, deterministic statement order,
explicit task groups, request isolation, prepared SQL, default-deny capabilities,
and existing `server.json`/`json` behavior. No new language grammar is required;
new library calls use existing `use`, `using`, records, and functions. New host
adapters must remain separate from interpreter semantics. No new Go package or
dependency is created by this design task. During implementation, choose pinned,
maintained dependencies with license/security review and pure-Go Windows builds;
raising the Go minimum or toolchain requires separate owner approval.

Excluded: browser compiler, GRAB UI/components/hydration, CMS, VM/IR, general
package registry or install scripts, arbitrary plugin execution, arbitrary Redis
commands/Lua, general cron/job scheduler, orchestration cluster, automatic
production deployment, and a new authentication/session framework. Application
authentication and authorization remain explicit application responsibilities.

## 2. Host configuration and capabilities

One new strict, versioned JSON file, `grab.platform.json`, selected by
`--platform-config`, declares named caches, connectors, webhook endpoints,
channels, event stores, workers, and telemetry destinations. It complements
`--db-config`; it does not replace database manifests or grants. Unknown fields,
duplicate keys/IDs, unsafe paths, unresolved references, invalid bounds, cycles,
and incompatible schemas fail before serving. Names are ASCII
`[a-z][a-z0-9_-]{0,63}`. Configuration and connector paths resolve relative to
their owning manifest and remain within its project root after symlink checks.

The top-level shape is `{version: 1, caches: {}, connectors: {}, webhooks: {},
channels: {}, events: {}, workers: {}, telemetry: {}}`; unused sections may be
omitted. Empty sections grant nothing. Secret fields contain environment-variable
names, never secret values. Host configuration is private and never client input.
Aliases and operation/channel IDs in GRAB calls must be direct text literals,
validated on load. Handles are opaque, generation-owned, nonserializable, and
cannot cross task snapshots or escape through JSON/cache/client generation.

Proposed capabilities (repeatable, no wildcard or implicit grant):

| Grant | Authority |
| --- | --- |
| `--allow-cache alias:read` | Get ordinary cached values in that alias namespace. |
| `--allow-cache alias:read-write` | Get/set/delete ordinary cache values; no counters or pub/sub. |
| `--allow-cache alias:limit` | Atomic rate-limit operation only, in a separate counter namespace. |
| `--allow-connector alias:operation` | Invoke one configured package operation, including its declared destination and secret reference. |
| `--allow-events alias:accept` | Host webhook ingestion into the configured durable inbox. |
| `--allow-events alias:work` | Claim/process that inbox/outbox with configured handlers and destinations. |
| `--allow-channel alias:connect` | Host may upgrade and subscribe authorized clients to that channel. |
| `--allow-channel alias:publish` | Publish to that channel's authorized audience. |
| `--allow-telemetry alias` | Export to one configured telemetry destination. |

Sensitive destinations are bound to aliases, exact scheme/host/port, namespaces,
and operations, not merely unrestricted sockets. Redis and telemetry use verified
TLS, DNS resolution/pinning and SSRF rules equivalent to existing remote adapters;
private services require explicit configured address/CIDR authorization. Plaintext
is permitted only with an explicit development setting and a literal loopback IP.
Redirects and Redis topology redirections are refused in 0.8: standalone Redis
only; Cluster/Sentinel discovery are deferred. Secrets, DB grants, filesystem
permissions, and `--allow-net` never implicitly grant any new subsystem. A connector
grant authorizes only its constrained HTTP operation; general `http` calls still
require `--allow-net`. No application shell/process API is introduced.

Event operations also require existing `--allow-db alias:read-write` for their
configured store. Migrations need `alias:migrate` in an explicit maintenance
command; web serving, workers, readiness, and reload never migrate automatically.
Broker transport for channels is declared in channel configuration and authorized
by channel grants; a cache grant cannot authorize publish/subscribe. Separate
Redis ACL identities/prefixes must isolate cache, counters, and channel transport.

`grab check --platform-config ...` performs static validation without opening
sockets, reading secret values, or changing storage. Serving prepares an immutable
configuration generation, resolves secrets from the host, validates grants, opens
bounded pools, and checks readiness. Failure leaves the last known good generation
active; no partial grant/configuration switch. Secret rotation occurs through a
validated reload. Configured but unused subsystems do not open connections.

## 3. Redis caching and atomic rate limiting — `use cache`

### 3.1 Configuration and API

Each cache declares `driver` (`redis` or `memory`), a fixed application namespace,
key/value and pool budgets, and a default operation timeout. Redis additionally
declares endpoint, TLS, and environment credential references. Memory needs no
external dependency; it is explicitly selected, never an automatic Redis outage
fallback. The following is a proposed configuration fragment, not a current CLI:

```json
{"version":1,"caches":{"catalog":{"driver":"memory","namespace":"shop-dev","max_keys":10000,"max_bytes":16777216}}}
```

| Call | Result and rules |
| --- | --- |
| `cache.open using alias` | Opaque authorized handle; shared bounded pool/state per application generation. |
| `cache.get using handle, key` | `{found: Boolean, value: value-or-nothing, ttl_ms: integer}`. Miss: `found=no`, `value=nothing`, `ttl_ms=0`. |
| `cache.set using handle, key, value, ttl_ms` | `nothing`; TTL is mandatory, positive integer milliseconds. Replace value and expiry atomically. |
| `cache.delete using handle, key` | Boolean: whether a live entry was removed. |
| `cache.limit using handle, key, maximum, window_ms` | `{allowed: Boolean, count: integer, remaining: integer, retry_after_ms: integer}`; atomic fixed-window counter. |

Missing differs from stored `nothing` (`found=yes`). Cache values use a versioned,
tagged canonical codec for text, booleans, nothing, arbitrary integers, exact
decimals (including scale), bytes, lists, ordered maps/records, and finite floats.
No float conversion for integers/decimals; unsupported/corrupt encodings raise an
error. Timestamps remain explicitly formatted text, matching existing runtime
values. No executable values, handles, cyclic objects, or implicit JSON coercion.

Keys are UTF-8 text, 1–256 bytes, with no control characters. The adapter encodes
them beneath its fixed application/alias namespace; it exposes no wildcard scan,
flush, cross-namespace access, or caller-selected Redis commands. Values are at
most 1 MiB encoded, depth 32, and 10,000 collection elements total. TTL range is
1–604,800,000 ms (seven days); expired entries behave as misses. TTLs use the store
clock; the memory implementation uses an injectable monotonic clock for tests.

### 3.2 Atomic fixed-window semantics and outage behavior

The first attempt creates a counter with count 1 and a fixed expiry. Every attempt
in that window increments once (including denied attempts); expiry is not extended.
`allowed` means `count <= maximum`; `remaining=max(0, maximum-count)`;
`retry_after_ms` is zero when allowed, otherwise remaining window duration, at least
1 ms. Maximum is 1–1,000,000; window is 1–86,400,000 ms. A changed maximum/window
for an existing live key is a configuration/type error, not a reset. Saturation
at signed 64-bit maximum denies without wrapping. Rate keys use a separate prefix
and cannot be deleted/overwritten by the ordinary cache API.

Redis executes validation, increment, and first-expiry as one fixed, host-owned
atomic script; separate INCR/EXPIRE calls are prohibited. Memory uses one protected
critical section with equivalent semantics. An unknown write result is reported
as uncertain and never transparently retried: repeating it could double-count.
There is no exact-once guarantee across network failures or Redis data loss.

Cache reads fail with a structured error during outages; applications may catch
that error and read the authoritative database. Rate checks fail closed: middleware
returns 503 when the store is unavailable/uncertain and 429 with rounded-up
`Retry-After` seconds when denied. A store outage must not look like a zero count.
Identity/bucket selection comes from authenticated application identity or trusted
proxy policy; never trust a client-supplied arbitrary bucket/IP header. Bound the
number of distinct buckets to prevent attacker-driven memory growth.

Memory defaults: 10,000 live keys and 16 MiB total, including keys/metadata; expired
entries are reclaimed first. At capacity, writes/new buckets fail; no live counter
is evicted to admit traffic. Redis rate-limit deployments require a dedicated
`noeviction` store and operator-verified limits. Cache state is temporary: neither
memory nor Redis is the order ledger, webhook dedupe ledger, session authority,
or a durable job queue. Restart/failover can reset limits; document this operational
boundary and use database-backed policy for decisions needing durable enforcement.
Shared limits in multi-worker mode require Redis; memory limits are local-only.

## 4. Typed JSON contracts and React/TypeScript adapters — ADR-014

### 4.1 Contract and wire rules

`grab.api.json` is a version-1, strict JSON contract document, explicitly supplied
to `serve` and generation with `--api-contract`. It declares `types` and
`operations`. Each operation has a unique stable ID, HTTP method, existing route
path, path/query fields, optional JSON body type, success status/type pairs, and
declared error statuses. It must match one existing GRAB route; undeclared routes
continue to work unchanged and are not generated. Duplicate/mismatched bindings,
ambiguous IDs, recursive types, and unsupported fields fail static checking.

Schema forms: `text`, `boolean`, `integer`, `decimal`, `float`, `bytes`,
`timestamp` (UTC RFC3339 text, up to nine fractional digits), `nothing`, named
`ref`, homogeneous `list`, and closed `record` with separately declared optional
fields and nullable types. Record field order determines canonical output;
unknown fields are rejected. No arbitrary executable validators or external
schema fetches. Text/bytes have explicit maximum length, lists a maximum count,
and integers/decimals optional declared range/precision constraints. Global
bounds are depth 32, 10,000 elements, and 1 MiB request/response bodies.

Integer wire values are canonical base-10 **strings**, never JSON numbers;
decimal wire values are decimal **strings**, preserving declared scale. Generated
TypeScript uses validated branded strings (`GrabInteger`, `GrabDecimal`), with
explicit integer-to-BigInt helpers; currency never converts silently to Number.
Finite floats use JSON numbers; bytes use canonical base64 text; booleans use
JSON booleans; nothing uses null. Missing optional fields stay missing. No NaN,
Infinity, duplicate JSON members, numeric coercion, or silent precision truncation.
Path/query values are text decoded exactly once and validated against their
declared type, with duplicate scalar query parameters rejected.

`use api` adds `api.input using request, operation_id` returning the validated
record `{path, query, body}` and `api.respond using operation_id, status, value`
returning a typed server response. The host validates contracted requests before
handler invocation and contracted responses before sending. Incorrect client
input yields 400/413; handler contract violations yield sanitized 500. Error wire
envelope is `{error:{code:text,message:text,request_id:text}}`; responses may not
expose stack traces/secrets. Existing uncontracted `server.json` is unchanged.
Calls to `api.respond` must match the current operation; client generation alone
does not imply a route is protected, authenticated, or correctly implemented.

### 4.2 Generator and React behavior

```text
grab check --api-contract grab.api.json app.grab
grab generate client --api-contract grab.api.json --out generated/shop --react
grab serve --api-contract grab.api.json --platform-config grab.platform.json app.grab
```

Generation is offline and deterministic: no GRAB execution, sockets, secret
resolution, package downloads, npm scripts, or application-source bundling.
Output: `types.ts`, `client.ts`, `validators.ts`, `contract.json` (public types,
operation IDs, wire version/hash only), README, and optional `react.ts`. Generated
files have an ownership manifest; refuse to overwrite handwritten files or
symlinks, use atomic writes, and refuse removal of files absent from that manifest.
The command's explicit output directory authorizes writes only within that
resolved directory. No timestamps or absolute local paths in generated files.

The base client uses standard fetch, safe URL/path encoding, AbortSignal,
runtime request/response validators, structured errors, and a configurable timeout
(default 10 seconds, max 60). Same-origin relative base URL and credentials
`same-origin` are defaults. No automatic mutation retries. Cross-origin endpoints
require an explicit client base URL and server origin allowlist; credentialed CORS
cannot use `*`. Cookie-authenticated mutations require configured CSRF protection
(Origin checks plus an application-issued CSRF token), rather than treating CORS
or TypeScript as authorization. Bearer tokens are supplied at call time and never
written to generated files or persistent browser storage by the adapter.

The optional React adapter exposes one query hook per read-only GET operation
with `{data,error,loading,refetch}` and explicit mutation functions for other
methods. Effects cancel on unmount/dependency changes and ignore stale responses;
Strict Mode re-execution cannot trigger a mutation automatically. No new frontend
framework, router, component system, SSR/hydration, or GRAB browser compiler.
React is an explicitly documented peer dependency of React output only. The
implementation must pin a tested TypeScript/React fixture matrix in its test
lockfile; GRAB itself never installs browser dependencies automatically.

## 5. Connector SDK and reusable packages

`use connect` exposes `connect.call using connector_id, operation_id, input`,
returning a validated operation result. Calls require a connector grant; neither
input nor route parameters may choose an endpoint, HTTP method, secret, or path
outside the operation template. Transport inherits specification 005 cancellation,
TLS, DNS/SSRF protections, bounded response/body limits, redaction, and safe retries.

A connector is a vendored, data-only package directory containing
`connector.json`, input/output contracts, static request templates, declarative
field mappings, webhook verifier declarations, README, and sanitized test vectors.
The SDK supplies manifest validation, a local fake-provider harness, signature
test-vector tooling, permission inspection, and deterministic packaging/checksums.
No native dynamic libraries, arbitrary Go loading, scripts, general source imports,
or template evaluation. JSON field mappings support named field selection,
renaming, and contract-checked literals only, not arbitrary expressions.

Manifest fields: format version, package ID/version, documented provider API
version/source links, exact allowed origins/methods/path templates, operations
with input/output contracts and credential requirements, idempotency/retry
classification, timeout/body bounds, and optional verifier profile. Path variables
are encoded as individual segments; headers prohibit Host/hop-by-hop manipulation;
request inputs cannot override credentials. Auth mechanisms in 0.8 are API-key,
Bearer, and Basic from host secret references; interactive OAuth flows and token
refresh storage require a later scoped design. HTTPS only except explicit loopback
test mode; no redirects to undeclared origins or arbitrary pagination URLs.

Host manifest binds local package paths and exact versions; `grab.connect.lock`
records sorted file paths and SHA-256 integrity hashes. Startup verifies the lock
before granting access. Updating packages/locks is an explicit reviewed maintenance
action, not runtime auto-update. Registry/download/installation are deferred;
vendoring plus integrity locking is the 0.8 package foundation.

Retries are off by default for side effects; only declared read-only operations
or provider-documented idempotent operations with a stable persisted idempotency
key may retry. Retry policy is bounded by the durable-work rules below. Do not
claim provider support from a generic HTTP fixture: each named provider requires
documented endpoints, signature fixtures, and sandbox end-to-end evidence.
Stripe/Lulu remain candidate packages, not required core dependencies. Release
requires one reusable provider-neutral reference package and two separately
configured app instances proving package reuse and tenant/secret isolation.

## 6. Webhooks: verify first, durably accept second

Webhook endpoints are exact POST paths in host configuration, reserved against
ordinary-route collisions. Each binds connector/verifier, secret references,
durable event-store alias, tenant binding, and handler. The host captures bounded
raw bytes before text/JSON decoding; signature verification never reserializes or
normalizes the body. Reject unsupported content encodings and duplicate signature
headers. Invalid signatures get a generic 401, oversized bodies 413; neither
creates an inbox entry or invokes GRAB application code.

0.8 includes one precisely defined reference profile: headers `Grab-Event-Id`,
`Grab-Event-Time` (decimal Unix seconds), and `Grab-Signature` (`v1=` plus lowercase
hex HMAC-SHA256). Signed bytes are UTF-8 event ID, `.`, timestamp, `.`, then exact
body bytes. ID is 1–128 ASCII alphanumeric/underscore/hyphen characters. Compare
MACs in constant time, reject malformed/duplicate fields, and enforce absolute
clock skew at most 300 seconds with synchronized host clocks. Allow at most two
configured active keys for rotation; no algorithm chosen by an untrusted header.
Verified signed IDs define dedupe identity. Other provider profiles must explicitly
specify their documented signed components, algorithms, timestamp rules and
signature tests; the reference profile is not advertised as Stripe compatibility.

After verification, parse and validate the provider contract and bind tenant from
trusted endpoint configuration/verified claims, never unsigned input. Persist
`(connector_instance, tenant, event_id)` under a unique constraint, body digest,
validated payload, received time, state and attempt metadata. Commit before a 202
acknowledgment. A previously accepted identical event returns 200 without new
work; the same ID with a different digest returns 409 and an audit alert. Storage
failure or uncertain commit returns 503 so the provider retries. Do not acknowledge
an event merely because it reached a memory queue or Redis.

Database persistence must meet the selected engine's documented durable-commit
settings; GRAB cannot promise survival of misconfigured storage or host loss.
Persist only required fields; protect database access/backups and provide bounded
retention maintenance. No raw body, signature, auth header, or payload in ordinary
logs/traces. Encrypted storage/backups are operator responsibilities, not implied
by TLS. Keep completed-event ID/digest tombstones for at least 30 days by default;
providers with longer replay windows require a longer configured policy. Old
replays outside retention are not globally deduplicated: business identifiers
and provider idempotency must still protect lasting effects.

## 7. Durable inbox/outbox and retries

### 7.1 Store, APIs, and state transitions

An event store names an existing data alias and a literal application namespace.
Versioned internal tables/migrations are published for SQLite, MariaDB, and
PostgreSQL and applied through explicit `grab migrate` maintenance. Use unique
constraints and engine-specific prepared claim/compare-and-swap statements; SQL
dialects remain adapter-owned. No SQL fragments from webhook/connector input.
Local single-worker tests use SQLite; multi-process durable workers require
MariaDB or PostgreSQL. Existing 0.7 SQLite platform/journal boundaries remain.

Inbox states: `pending -> leased -> completed`, or `retry_wait -> leased`, or
`dead`. Outbox has the same states plus `uncertain` for remote outcomes that cannot
be safely repeated. Lease expiry makes unfinished work claimable after a crash.
Claims carry an incrementing fencing token; renewal, completion, and retry writes
compare the current owner/token. A stale worker cannot commit effects or mark
another worker's item complete.

`use events` adds:

| Call | Contract |
| --- | --- |
| `events.enqueue using transaction, store_id, connector_id, operation_id, input, idempotency_key` | Insert outbox work in the caller's explicit transaction on that same database; return durable work ID. Requires `work`, DB write, and connector-operation grants. |
| `events.status using store_id, work_id` | Sanitized `{id,state,attempts,next_attempt_at}` under `work` grant; no raw payload/secret. |
| `events.complete using transaction, event` | Mark current claimed inbox item completed within its matching fenced transaction; return nothing. |

Configured inbox handlers use existing `make handler using event` functions. Each
invocation receives an immutable `{id,tenant,type,payload,attempt,transaction}`;
the host supplies a transaction bound to that lease/store. Handlers execute
prepared business changes and outbox enqueue in this transaction and call
`events.complete`. Host commit happens only after successful return and completion;
failure, missing completion, deadline, or cancellation rolls it all back. Handlers
cannot directly commit/rollback this host-owned transaction, change its lease,
perform connector/HTTP side effects, or return it. Remote I/O goes through outbox.
This is an explicitly restricted worker context, not a change to ordinary
`data.begin` semantics. Database grants still bound accessible business data.

Before committing, the worker locks/rechecks the live lease and fencing token in
the same transaction; competing reclaim must serialize with that check. Business
changes, outgoing intents, and inbox completion then commit atomically. No network
call occurs while holding this transaction. Each outbox operation commits its
result with its own lease check. Independent application databases cannot share
this atomic boundary: cross-database distributed transactions are not supported.

### 7.2 Delivery and recovery policy

Delivery is **at least once**, never exactly once. Persist a stable idempotency key
before the first remote attempt; retries use the same key. A crash after provider
success but before recording success can cause another delivery. Only a provider's
documented idempotency behavior or explicit reconciliation can prevent duplicate
external effects. Unsafe operations with an uncertain response enter `uncertain`
for operator reconciliation rather than blind retry.

Default attempts: 8 total; maximum configurable 20. Exponential backoff starts at
1 second, caps at 15 minutes, with full jitter; an accepted `Retry-After` delays
at least that long but cannot exceed the 24-hour overall work deadline (then dead).
Retry only transport failures, 408/429, and declared transient 5xx responses for
safe/idempotent operations. Auth/contract failures and other 4xx fail permanently.
Attempt counts/deadlines survive restarts. Lease defaults to 30 seconds, handler
deadline 10 seconds, renew every 5 seconds; deadlines must fit lease policy.

Queue admission defaults to 100,000 pending items and 256 MiB payload per store;
enforce atomically across workers, reject with 503 before webhook acknowledgment
or fail enqueue transaction when full. Poll batches at most 100; worker concurrency
defaults to 4 per process, bounded by DB pool budget. No promise of global event
ordering; concurrent events for one business object need explicit version checks
or database locking. Retries do not undo already committed external effects.

`grab events inspect --store alias --id id` provides sanitized state;
`grab events retry --store alias --id id --reason text` is explicit maintenance
under work/DB grants, with audit trail, and preserves the original idempotency key.
It refuses uncertain work until an operator records a reconciled outcome through
`grab events resolve --store alias --id id --outcome delivered|not-delivered --reason text`.
Resolving as not-delivered permits a separate retry, never an automatic send.
Dead-letter records are not silently discarded. Retention/purge is a separately
invoked audited maintenance command, never background deletion of pending work.

## 8. WebSocket channels

`use sockets` offers `sockets.publish using channel_id, audience, value`, returning
`{accepted: Boolean}` for bounded local/broker admission, not recipient receipt.
Channel configuration declares one reserved GET upgrade path, named authorization
function, named message handler, client/server contract types, origin allowlist,
limits, and broker (`memory` for one process or standalone Redis pub/sub).
Colliding HTTP/webhook/channel paths are load errors. No raw socket handle or
arbitrary user-selected subscription/Redis topic enters GRAB code.

Before upgrade, `make authorize using request` must return
`{allowed,principal,audiences,expires_at}`; explicit denial or malformed output
rejects the request. Audience IDs are server-created, bounded text identifiers;
publication and membership must be authorized for the current principal/tenant.
The message handler `make receive using message` receives an immutable
`{principal,audience,value}` in a fresh, bounded interpreter frame. It may not
change its authenticated identity or obtain another tenant's audiences. Publishing
from HTTP/worker contexts requires a configured authorization callback for the
current trusted context and requested audience; a host publish grant alone is not
tenant authorization. Authentication expiry closes the connection; renew through
a new authenticated handshake. No secrets/tokens in URL query strings.

Require exact Origin allowlist for browser handshakes; missing Origin is refused
unless explicitly enabled for authenticated non-browser clients. CORS is not
WebSocket authorization. Production requires `wss` at a verified TLS edge (or
existing explicit trusted-proxy policy), plus application authentication. Text
messages are contract-validated JSON; binary application frames and compression
are deferred. Implement RFC 6455 fragmentation/control handling through a reviewed
library; fragmented messages count toward the same total limit.

Defaults: 64 KiB per message, 1,000 connections/process, 100 messages/second/client,
outgoing queue 64 messages AND 1 MiB/client, incoming queue 16 messages/client,
handler deadline 5 seconds, ping every 20 seconds and pong timeout 10 seconds.
Reject oversize with 1009, policy violations with 1008, invalid application data
with 1007, overload/slow consumers with 1013, shutdown/replacement with 1012.
No unbounded buffering; one serialized message handler per connection preserves
arrival order. Concurrent connections do not imply global message order.

Redis pub/sub broadcasts to authorized workers, with namespace and audience
binding; it is transient, not durable/replayable. Pub/sub outage closes affected
channels as unavailable instead of pretending global delivery succeeded.
Multi-worker channels require the shared broker. Memory channels are one-process
only. Disconnects/restarts may lose notifications: clients refetch authoritative
typed HTTP data on reconnect with bounded backoff/jitter. Durable notifications
must be persisted through the outbox before publication; even then pub/sub is a
hint and reconnect catch-up uses database/API state. No exactly-once channel claim.

## 9. Multi-process scaling and lifecycle

`grab serve --workers N --worker-base-port P ...` supervises 1–32 application
processes, default 1. For N>1, each worker binds consecutive loopback ports
P..P+N-1; startup refuses conflicts/overflow. A caller-supplied external reverse
proxy/load balancer routes traffic and WebSocket upgrades to these ports; GRAB
does not edit proxy configuration or production services. Standalone single-worker
serve remains unchanged. No SO_REUSEPORT portability assumption or hidden shell.
The supervisor spawns only its own verified executable with structured arguments,
restricted inherited environment and host configuration, never application-chosen
commands. Private supervisor control uses authenticated local IPC, not public
routes. A process is useful isolation, not an untrusted-code sandbox.

All workers must load the same code/configuration/contract/connector-lock hashes.
Readiness requires compatible DB migration versions, required stores/brokers,
pool budgets, and grants. Supervisor reports unhealthy if required capacity is
missing; external health checks can query a loopback aggregate endpoint. Fail
startup if declared total DB/Redis pools exceed configured application budgets:
calculate N times per-worker pools plus control/maintenance headroom. Request
bindings remain isolated; no shared mutable language globals or inferred sessions.
Shared sessions/business state must live in explicit external storage.

Restart crashed workers with bounded exponential delay (1–30 seconds, at most
5 restarts in 5 minutes); then mark the deployment unready for operator attention.
On shutdown/reload stop new HTTP admissions and event claims, finish/rollback
in-flight work within a 30-second default drain deadline, close WebSockets with
reconnect guidance, release leases safely, then close generation resources.
Forced termination leaves durable claims recoverable by expiry/fencing.

Application replacement validates a complete new generation before admission,
keeps per-request generation ownership, and drains old resources. Multiprocess
reload is rolling, not a distributed atomic switch: old/new contracts must be
backward-compatible during overlap, and public contract hashes identify versions.
Incompatible changes require a stopped maintenance deployment; never run migrations
inside reload. Schema expand/contract procedures are explicit operator work.
Zero downtime or zero dropped WebSockets is not promised. Multi-host operation
uses independently supervised instances behind an external balancer with shared
stores; automated cluster membership/leader election are excluded.

## 10. Metrics, tracing, budgets, and errors

Provide OpenMetrics-compatible metrics on a separate loopback-only management
listener, disabled by default, and optional OpenTelemetry OTLP/HTTP traces through
a configured TLS collector requiring `--allow-telemetry`. No exporter by default.
Management authentication uses a host secret token; never expose diagnostics on
ordinary application routes. Do not introduce remote profiler or admin execution.

Metrics cover admitted/rejected requests, active connections, route-template
latency, cache hits/errors, rate denials, inbox/outbox lag/attempt/dead counts,
lease recovery, worker readiness/restarts, bounded pool/queue use, and trace drops.
Use bounded configured labels (route template, operation ID, status class, worker),
never raw paths, keys, users, event IDs, payloads, authorization headers or SQL
values. Trace attributes use a strict allowlist; W3C trace context is validated,
incoming baggage is dropped, and remote sampling flags cannot override local
sampling/privacy policy. Trace links correlate asynchronous work without storing
payloads. Buffer at most 2,048 spans/process, batch at most 512, flush deadline
5 seconds, default sample rate 1%; exporter failure drops telemetry with a count,
never blocks business commits or recursively logs its own errors.

| Resource | Default / hard maximum |
| --- | --- |
| Cache/Redis operation timeout | 1 second / 10 seconds, capped by owning context |
| Redis pool per alias/process | 8 / 64, subject to aggregate budget |
| API/webhook body | 1 MiB / 8 MiB with explicit host limit |
| Connector body/timeout | 1 MiB / 8 MiB; 10 seconds / 60 seconds |
| Worker handler concurrency | 4 / 64 per process, aggregate bounded |
| Outbox overall deadline | 24 hours / 7 days with explicit policy |

Where a subsystem specifies a stricter bound, that bound wins. All limits validate
at startup; operator increases stay below hard maxima and require memory/pool
budget accounting. Cancellation propagates into network, database, cache, handlers,
and draining; requests cannot spawn orphan workers. Resource cleanup is idempotent.

Proposed diagnostics: `GRAB-E210` static contract/platform/connector validation,
`GRAB-E314` cache/limit, `GRAB-E315` typed API contract, `GRAB-E316` connector,
`GRAB-E317` durable events/webhooks, `GRAB-E318` channels, `GRAB-E403` capability
denial for these subsystems. Retain structured error records/source locations,
beginner/developer/production modes, and fatal non-catchable runtime safety limits.
No errors reveal secret names/values, connection strings, signatures, or bodies.
An uncertain outcome is a distinct structured cause, never a success or safe retry.

## 11. Implementation sequence after approval

1. Strict host configuration/capabilities, memory adapter and real Redis cache/
   rate-limit conformance; keep transport and codec outside the interpreter.
2. Typed contracts/runtime validation, deterministic TS generator, React adapter,
   and browser/server authorization boundary fixtures.
3. Connector manifest/lock/SDK, reference package, verifier core and provider harness.
4. Three-engine event migrations, durable inbox/outbox, fencing, retry/reconciliation
   maintenance, and crash-recovery tests.
5. Authorized WebSocket channels, Redis broker, bounded connection lifecycle.
6. Cross-platform worker supervision, rolling replacement, telemetry, resource
   accounting, sustained load/fault acceptance and operator guides.

Candidate implementation boundaries: host-owned `internal/cache`,
`internal/contracts`, `internal/connect`, `internal/events`, `internal/realtime`,
existing `internal/server` supervision, thin interpreter module bindings, and CLI
commands in `cmd/grab`. Create packages only during approved implementation;
avoid moving existing packages merely to resemble blueprint diagrams.

## 12. Mandatory release/conformance gate

Approval of this draft is a design gate, not release acceptance. Implementation
is complete only after all of the following are automated, executed, and recorded:

| Area | Required evidence |
| --- | --- |
| Compatibility | Existing `go test ./...` and `go build ./...` pass; no 0.1–0.7 behavior regression; minimum-Go and pure-Go Windows/Linux builds pass. |
| Cache | Table-driven codec/TTL/expiry/null/precision/error/capability tests on memory and real Redis; same vectors; concurrent atomic expiry/counters, namespaces, overflow, full store, denied attempts, uncertain writes, outage fail-closed, pool cancellation and cleanup. |
| Typed clients | Static invalid contracts; wire golden tests; very large integers/exact prices/bytes/null/optional fields; duplicate/unknown JSON rejection; deterministic generation and overwrite confinement; compile generated TS and run React fixture with real runtime validators, cancellation, Strict Mode and explicit mutation checks. No secrets in output. |
| Connectors/webhooks | Reference package reused across apps; integrity/permission/SSRF/header/path tests; raw-byte signatures, tamper/replay/rotation/skew, duplicate IDs/body mismatch, tenant isolation, queue saturation, acknowledgment only after commit, DB commit uncertainty, and any claimed provider's sandbox tests. |
| Durable work | Real SQLite, MariaDB and PostgreSQL tests for atomic business+outbox commit, rollback, competing claims, fenced stale worker, restart after claim/commit/provider send, safe/unsafe retries, Retry-After, deadlines, dead letters, reconciliation, retention, and migration dirty/recovery states. Multiworker tests use both remote engines. |
| WebSockets | Real handshake/frame tests: Origin/auth/expiry/audience isolation, fragmentation, malformed/oversized input, compression refusal, ping/pong, slow readers, full queues, cancellation, broker outages, two-worker delivery and reconnect/refetch. |
| Scaling/telemetry | Windows/Linux supervised processes; 1/2/4-worker isolation and budget checks; worker kill/restart-loop/rolling reload/drain; no orphan resources; sanitized bounded labels/traces, collector outage, readiness failures and race tests. |
| End-to-end | Permanent `tests/grab08_conformance_test.go` plus fixtures exercising cached catalog API, generated React/TS client, verified webhook -> durable transaction -> outbox -> realtime notification and authoritative refetch. Runnable examples and operator/recovery docs. |
| Sustained load | Reproducible 30-minute runs at 1/2/4 workers with specified hardware, warmup, payloads and concurrency; report p50/p95/p99, throughput, RSS, GC, pools/queues, rejected/failed work and recovery. Inject Redis/DB/collector faults and kill a worker under load. No lost acknowledged inbox rows, duplicate committed business effects for the idempotent fixture, cross-tenant leaks, or unbounded growth beyond declared budgets. |

Offline developer tests use memory and SQLite with fake provider/clock/transport;
they cannot substitute for real Redis, remote-database, browser-adapter, worker,
race, and fault/load acceptance. CI reports skipped integration gates explicitly;
no milestone-complete claim with skipped required acceptance. Numeric performance
claims/SLOs require measured baselines and owner review (ADR-016), not invented
throughput promises. Dependency integrity/security review and the full passing
suite are required before requesting release approval.

## 13. Technical references and proposed decisions

These references inform protocol choices; they do not authorize GRAB features:

- [Redis INCR](https://redis.io/docs/latest/commands/incr/): counter/rate-limit patterns and the increment/expiry race; motivates a single atomic operation here.
- [RFC 8259](https://www.rfc-editor.org/rfc/rfc8259): interoperable JSON number limits; motivates exact integer/decimal strings in typed clients.
- [React useEffect](https://react.dev/reference/react/useEffect): cleanup and development re-execution; motivates cancellation and explicit mutation calls.
- [Stripe signature troubleshooting](https://docs.stripe.com/webhooks/signature): provider-specific signatures depend on the original request bytes; not a claim of a Stripe connector implementation.
- [RFC 6455](https://www.rfc-editor.org/rfc/rfc6455): WebSocket handshake, origin and framing requirements.
- [OpenTelemetry sensitive data guidance](https://opentelemetry.io/docs/security/handling-sensitive-data/): telemetry needs explicit data minimization and redaction.

Proposed ADR-025a–025h and the narrow ADR-014 planning update are recorded in
[`ADR-DECISION-REGISTER.md`](../docs/adr/ADR-DECISION-REGISTER.md). All implementation
details above are authorized by the owner's November 12 approval of specification 008. GRAB 0.9, 1.0, and
Post-1.0 features remain planned and unapproved.
