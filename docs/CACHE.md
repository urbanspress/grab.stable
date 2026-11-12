# GRAB 0.8 cache implementation (milestone in progress)

Specification 008 and its design were approved by the owner on November 12,
2026. The first implementation slice supplies strict platform configuration and
temporary caches. **GRAB 0.8 is not release-complete.** Typed APIs/client generation,
connectors, durable events, channels, supervised workers and telemetry remain
unimplemented. Their nonempty configuration and grants fail closed.

## Host configuration and grants

`run`, `serve`, and `check` accept `--platform-config path/to/grab.platform.json`.
The version must be 1. Unknown/duplicate fields, null settings, unsafe aliases,
invalid limits and undeclared cache references are rejected. Check is offline:
it does not resolve environment secrets or connect to Redis. Cache aliases in
`cache.open` must be direct text literals, even in unexecuted functions/branches.

```json
{"version":1,"caches":{"catalog":{"driver":"memory","namespace":"shop-dev","max_keys":10000,"max_bytes":16777216}}}
```

Grants are independent and repeatable:

| Flag | Authority |
| --- | --- |
| `--allow-cache catalog:read` | Open and read values only |
| `--allow-cache catalog:read-write` | Open, read, set and delete values |
| `--allow-cache catalog:limit` | Open and increment rate counters only |

No wildcards or implicit network/database/file authority. Both read-write and
limit are needed to combine ordinary caching with rate limiting. Other planned
platform capability flags are recognized but rejected until their adapters exist.
An empty section grants nothing. Existing database configuration is unchanged.

## Cache calls

| Call | Result |
| --- | --- |
| `cache.open using "catalog"` | Opaque generation-owned handle |
| `cache.get using handle, key` | `{found, value, ttl_ms}` |
| `cache.set using handle, key, value, ttl_ms` | `nothing` |
| `cache.delete using handle, key` | Boolean |
| `cache.limit using handle, key, maximum, window_ms` | `{allowed,count,remaining,retry_after_ms}` |

Handles cannot be displayed, cached, serialized or passed through task snapshots
or task results. They become invalid when the owning generation closes. Ordinary
functions and requests in that generation may share the underlying store.
Server preparation checks referenced stores/grants before traffic admission;
reload rereads the platform file and resolves credentials for a new generation.
A rejected candidate leaves the previous generation active. Configured caches
without literal references do not open connections.

Values retain exact integer and decimal coefficient/scale, collection order,
bytes, finite floats, text, booleans and `nothing` through a versioned tagged
codec. A stored `nothing` has `found=yes`; a miss has `found=no`, `value=nothing`,
`ttl_ms=0`. Corrupt/noncanonical encodings fail, without returning their contents.
Maximum encoded value size is 1 MiB, nesting depth 32 and total values 10,000.
Keys are valid UTF-8, 1–256 bytes, without control characters. TTL is mandatory,
1–604,800,000 milliseconds. Cache and counter keys have separate prefixes.

Every fixed-window rate attempt increments, including denied attempts; expiry
does not extend. Maximum is 1–1,000,000 and window is 1–86,400,000 ms. Changing
either for a live bucket is an error. Counters saturate at signed int64 maximum
and deny without wrapping. No live bucket is evicted to admit another. Expired
entries are reclaimed before capacity checks. Errors use `GRAB-E314`; denied
capabilities use `GRAB-E403`; static errors use `GRAB-E210`. Specification 008's
proposed cache diagnostic number overlaps existing 0.7 database diagnostics;
the subsystem and structured message distinguish them for now.

## Standalone Redis

```json
{
  "version": 1,
  "caches": {
    "catalog": {
      "driver": "redis",
      "namespace": "shop",
      "host": "redis.example.com",
      "port": 6379,
      "tls": "verify",
      "username_env": "SHOP_REDIS_USER",
      "password_env": "SHOP_REDIS_PASSWORD",
      "pool_size": 8,
      "operation_timeout_ms": 1000,
      "max_keys": 10000,
      "max_bytes": 16777216
    }
  }
}
```

Set secrets in the host environment, never in the manifest or source. Secret
values are read only while preparing an authorized adapter and remain fixed for
that generation. TLS uses system certificate roots and verifies the configured
hostname. Each reconnect resolves, checks every result and dials a checked IP.
Private/loopback services require explicit `allowed_cidrs`, except development
mode on a literal loopback address. Link-local/metadata, unspecified and multicast
addresses are always denied. There is no plaintext fallback. For disposable local
tests only, use `host: "127.0.0.1"`, `tls: "disabled"`, `development: true`.

Standalone only: topology discovery, redirects, arbitrary Redis commands and
application-authored Lua are unavailable. Command retries are disabled. Transport
failures on writes report an uncertain outcome, never success or an automatic
resend. Rate checks must return unavailable/503 on errors, not allow the request.
Denial uses 429 and rounded-up Retry-After seconds. See the runnable
[catalog example](../examples/CACHE.md).

Use a dedicated Redis store configured with `maxmemory-policy noeviction` and an
operator-verified memory ceiling. Preparation verifies noeviction using CONFIG
GET; the host identity needs that read permission plus PING, script and relevant
hash/sorted-set/expiry commands restricted to its `grab:v1:<namespace>:<alias>:`
prefix. GRAB does not change Redis configuration. Keep operator-selected budgets
identical for every process sharing an alias: a stored policy fingerprint rejects
conflicts. Pools default to 8, maximum 64; operation timeout defaults to one second,
maximum ten seconds, capped by the owning context. There are no pipeline pools,
idle warmup connections or background credential refreshes.

`max_bytes` is an admission accounting budget (encoded values, keys and a fixed
metadata charge); it is not a promise about Redis RSS or Go allocator overhead.
The Redis index and size ledger also need server memory. Actual RSS accounting,
cross-worker aggregate budgets and sustained fault/load acceptance remain open
release gates. Redis state and index/metadata must not be modified outside GRAB.
Memory and Redis are temporary; restart/failover/data loss can reset limits.
Neither is an event deduplication ledger, order ledger or durable session authority.

## Verification and remaining release gates

Unit tests cover strict parsing, independent grants, redacted secret errors,
canonical codec, exact numbers, null/miss, expiry, capacity, counter saturation,
concurrent attempts, cancellation, ownership, opaque handles and cleanup.
`tests/grab08_conformance_test.go` starts the permanent 0.8 conformance suite;
it currently covers cache/configuration only. HTTP tests cover readiness, failed
reload preservation and new-generation memory isolation.

Executed November 12, 2026: Windows Go 1.27.4 full `go test -count=1 ./...`
with real Redis enabled, then `go build ./...`; Linux Go 1.26 full race suite
with real Redis and minimum-Go Linux build; pinned govulncheck v1.9.0 on patched
Go 1.27.4 reported no vulnerabilities. All these commands passed. Redis was a
disposable loopback-only 8.2.2 container with noeviction and 64 MiB maxmemory.
Optional remote-database tests were not enabled in these runs.
Final source also passed minimum-Go 1.26 Windows/amd64 and Linux/amd64 builds
with `CGO_ENABLED=0`. The full Windows suite/build and Linux race suite were
rerun successfully after the final hostname and collection-boundary corrections.

Real Redis runs the same value/rate vectors as memory when
`GRAB_TEST_REDIS_PORT` is set. Without it, the real-service test explicitly skips;
that skip is not release acceptance. Only disposable services may be used.

All remaining Section 12 gates are still required: broader Redis failure/TLS/ACL
tests, three-engine durable-work tests, generated TypeScript/React execution,
channel wire tests, Windows/Linux worker supervision, telemetry privacy/outages,
minimum-Go/race verification, dependency security review and reproducible
30-minute runs at 1/2/4 workers. Passing the present Go suite does not close them.
