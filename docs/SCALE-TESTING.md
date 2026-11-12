# GRAB 0.8 Part 3 verification

Recorded November 12, 2026. **Part 3 is implemented and the checks below passed.
GRAB 0.8 is not release-complete:** the complete specification-008 Section 12
acceptance gate, particularly sustained fault/load evidence, is still open.
Current Owner remains OpenAI Codex. No commit, push or deployment was performed.

## Executed checks

| Check | Result and limits |
| --- | --- |
| Windows Go 1.27.4 `go test -count=1 -timeout=240s ./...` | All packages passed. Real Redis and remote database flags enabled, including the new MariaDB/PostgreSQL multiprocess workflow tests. AST has no test files. |
| Windows Go 1.27.4 `go build ./...` | Passed on the final source. |
| Linux minimum Go 1.26 full `go test -race -mod=readonly -count=1 -timeout=300s ./...` | All packages passed in the official disposable Go container with real Redis. Remote database opt-in workflow vectors were not enabled in this Linux run; they passed separately on Windows. |
| Minimum Go 1.26 pure-Go builds | Linux/amd64 and Windows/amd64 with `CGO_ENABLED=0`; both passed. |
| Real durable-store vectors | `GRAB_EVENT_ENGINE=mariadb` and `postgres` runs of `go test -count=1 ./internal/events` passed, including atomic business/outbox, competing claims, fencing, recovery, maintenance and work metrics. SQLite vectors also passed. |
| Dependency review | `go mod verify` passed. Patched Go 1.27.4 with pinned `govulncheck v1.9.0` reported **No vulnerabilities found** on the final source. New pinned `coder/websocket v1.8.14` ISC notice is in `docs/licenses/`. |
| Example | Offline `grab check` passed for `examples/realtime_app.grab` with its platform and API manifests. Wire tests execute the example. |

Disposable services: Redis `redis:8.2.2` on loopback 16388, MariaDB `mariadb:11.8`
on 53306 and PostgreSQL `postgres:18` on 55432. Redis uses 64 MiB and `noeviction`.
No production infrastructure was used. Ordinary sandbox network calls are denied;
wire/service tests were run through the authorized network-enabled command path.

## Permanent coverage

- `internal/platform/scaling_test.go`: strict channel/origin/telemetry schemas,
  independent grants, immutable copies and aggregate pool budgets.
- `internal/realtime/*_test.go`: actual handshakes/frames, exact Origins,
  compression refusal, audience boundaries, malformed/duplicate/binary messages,
  fragmentation, contracts in both directions, expiry, close codes, rate/size/
  incoming/outgoing queue caps, cancellation and Redis broker delivery/outage.
- `internal/server/workers_test.go` and
  `tests/grab08_workers_conformance_test.go`: private management authentication,
  source/contract snapshots, bounded restart delay, actual 1/2/4 processes,
  rolling replacement, killed-worker recovery and supervisor-death orphan cleanup.
- `internal/telemetry/*_test.go`: authenticated metrics and token rotation,
  bounded configured labels and trace buffers shared across exporters, validated
  trace context, local sampling, collector SSRF/certificate rejection, TLS wire
  export, outage/drop behavior, and private stable durable-attempt links.
- `internal/events/metrics_test.go`: pending/dead counts, lag and lease recovery.
- `tests/grab08_realtime_conformance_test.go`: real GRAB authorization and receive
  handlers, HTTP publication/refetch, channel-only apps, independent admissions,
  and spoofed HTTPS headers versus explicitly trusted TLS proxies.
- `tests/grab08_workflow_conformance_test.go`: signed webhook → durable inbox →
  atomic business row plus outbox → idempotent fake provider → separately
  authorized notification → typed cached catalog refetch. Duplicate ingestion
  leaves one business row. SQLite runs normally; remote opt-in vectors run two
  actual supervised processes with shared Redis for **each** remote engine,
  connecting the WebSocket to the other worker from the HTTP publisher.

## Reproduce the finite checks

With the disposable database setup from [DATA-TESTING.md](DATA-TESTING.md) and a
standalone local Redis, use PowerShell from the repository root:

```powershell
$env:GRAB_TEST_REDIS_PORT = '16388'
$env:GRAB_TEST_DATABASES = '1'
$env:GRAB_TEST_DB_PASSWORD = 'grab_disposable_test'
go test -count=1 -timeout=240s ./...
go build ./...

$env:GRAB_EVENT_ENGINE = 'mariadb'
go test -count=1 ./internal/events
$env:GRAB_EVENT_ENGINE = 'postgres'
go test -count=1 ./internal/events
Remove-Item Env:GRAB_EVENT_ENGINE
```

Use patched Go 1.27.4 for the security scan; the installed host default 1.27.3 is
not the verification toolchain. The minimum language/toolchain declaration remains
Go 1.26. The Linux race command uses the official `golang:1.26` image, a read-only
repository mount and Redis's disposable container network namespace. The normal
developer suite explicitly skips optional real-service vectors when their flags
are absent; those skips cannot satisfy release acceptance.

## Open release gates — do not mark the milestone complete

The finite tests above do **not** establish the required sustained-load gate.
The reproducible 30-minute **1/2/4-worker** load/fault harness and its recorded
results remain to be completed. It must declare hardware, warmup, concurrency and
payloads; measure p50/p95/p99, throughput, RSS/GC, pool/queue usage, rejected/failed
work and recovery; inject Redis, database and collector faults plus worker kills;
and reconcile every acknowledged inbox identity against committed business effects.
It must test tenant isolation and growth against declared budgets. No capacity/SLO
claim has been made.

Before release, reconcile **every** Section 12 row against executed evidence,
including the existing generated React/TypeScript/browser fixtures, migration and
provider recovery cases, restart-loop exhaustion, sustained slow consumers, and
cross-subsystem fault behavior. Passing a package does not prove that every item
in the specification was exercised. The sample connector is provider-neutral;
there is no claim of a named provider's sandbox certification.

Conservative operator boundaries: rolling mode keeps public contract bytes,
database manifests, worker policy and Redis pool totals fixed; changes to those
need a stopped deployment. Event handlers persist an outbox item before a
separately authorized publication endpoint is invoked. Durable-attempt trace
links correlate retries; complete HTTP-to-provider distributed trace propagation
is not claimed. See [REALTIME-SCALING.md](REALTIME-SCALING.md).
