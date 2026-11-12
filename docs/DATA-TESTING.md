# GRAB 0.7 verification

The implementation is in `internal/data`, exposed by `internal/interpreter/data.go`.
The approved minimum is Go 1.26. `go.mod` and `go.sum` pin modernc SQLite v1.62.0,
go-sql-driver/mysql v1.10.1, and pgx v5.13.0. SQLite and its confined VFS use Go,
including the generated modernc SQLite implementation; no CGo compiler is needed
to build or run GRAB on Windows. A C compiler is needed only for Go's race detector.

## Ordinary verification

```sh
go test ./...
go build ./...
go test ./internal/data -run '^$' -bench BenchmarkSQLitePreparedOne -benchmem -benchtime=200ms
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

SQLite tests run offline. MariaDB/PostgreSQL cases explicitly skip unless
`GRAB_TEST_DATABASES=1`; a skipped service is not evidence of adapter support.
All three real adapters were run successfully during milestone verification on
Windows/amd64 against MariaDB 11.8 and PostgreSQL 18 disposable containers. Cases
cover malicious bound text, large exact currency, native booleans, empty bytes,
NULL, UTC/naive fractional timestamps, DATE/TIME text, transaction rollback,
rollback-only errors, cancellation of blocked writes, native read-only reconnects,
migration no-op/changed/ahead history, transactional DDL rollback, and durable
MariaDB dirty-state refusal. No remote-driver support claim relies on mocks.

The no-retry and ambiguous-commit tests additionally use a deliberately failing
driver to prove a single attempt and `GRAB-E318`; these cannot safely be made
deterministic by inducing an actual network failure at an unknown commit point.
Ordinary pool, statement, and result paths still use the real engines above.

## Reproduce disposable services

These commands create local test data only. The fixed password is a public test
fixture, never a production credential. Use an unused pair of local ports.

```sh
docker run --detach --name grab07-postgres -p 127.0.0.1:55432:5432 -e POSTGRES_PASSWORD=grab_disposable_test -e POSTGRES_DB=grab_test postgres:18
docker run --detach --name grab07-mariadb -p 127.0.0.1:53306:3306 -e MARIADB_ROOT_PASSWORD=grab_disposable_test -e MARIADB_DATABASE=grab_test -e MARIADB_USER=grab -e MARIADB_PASSWORD=grab_disposable_test mariadb:11.8
docker exec grab07-postgres pg_isready -U postgres -d grab_test
docker exec grab07-mariadb healthcheck.sh --connect --innodb_initialized
docker exec grab07-postgres psql -U postgres -d grab_test -c "CREATE ROLE grab LOGIN PASSWORD 'grab_disposable_test'; GRANT USAGE, CREATE ON SCHEMA public TO grab;"
```

Wait for both health checks to succeed before testing. The suite connects as
`grab`, not as PostgreSQL/MariaDB administrator. MariaDB's account is confined to
`grab_test`; PostgreSQL's account has schema creation/use there and owns its own
test objects, without superuser privileges. Do not point these tests at an
existing or valuable database: they create/delete their named fixture rows and
exercise deliberate failed schema changes with operator-style cleanup.

PowerShell:

```powershell
$env:GRAB_TEST_DATABASES = '1'
$env:GRAB_TEST_DB_PASSWORD = 'grab_disposable_test'
$env:GOTOOLCHAIN = 'go1.27.4'
go test -count=1 ./...
go build ./...
docker rm --force grab07-postgres grab07-mariadb
```

POSIX shell:

```sh
GRAB_TEST_DATABASES=1 GRAB_TEST_DB_PASSWORD=grab_disposable_test go test -race -count=1 ./...
go build ./...
docker rm --force grab07-postgres grab07-mariadb
```

`.github/workflows/data.yml` provides an Ubuntu CI job with fresh real services,
restricted accounts, race tests, build, and vulnerability audit. The workflow has
been added locally; no hosted CI run or repository push is claimed.

## Acceptance environment and resource baseline

Windows verification uses the approved Go 1.27.4 toolchain. The original installed
Go 1.27.3 scan reported standard-library advisory GO-2026-6859; after the approved
patch, the full-project `govulncheck` reports **No vulnerabilities found**. This is
a point-in-time dependency/call-graph check, not a proof of security. Use patched
Go releases for deployment; the project minimum stays 1.26.

Linux/amd64 race tests passed in official `golang:1.27.4` and `golang:1.26`
(Go 1.26.11) containers, also verifying the approved Go 1.26 minimum. Both mount the
repository read-only. The ordinary SQLite suite includes confinement, hardlink and
symlink journal denial, competing writers, abrupt-process rollback-journal
recovery, cancellation, limits, and leaks. Windows lacks symlink-creation
privilege, so that one fixture runs on Linux instead; hardlink checks run on both.

```powershell
docker run --rm --mount 'type=bind,source=C:/Development/GRAB,target=/src,readonly' --workdir /src golang:1.27.4 go test -race -mod=readonly ./...
```

Initial Windows/amd64 baseline on AMD Ryzen 5 7535HS:
`BenchmarkSQLitePreparedOne-12`: approximately **7.1 microseconds/op**,
**3,550 bytes/op**, **58 allocations/op**, 200 ms benchmark duration. Reproduce
with the command above; timings vary with toolchain and system load. This measures
one materialized prepared two-field row from memory, not disk durability, remote
latency, or production capacity. The public resource caps and measured cursor/pool
cleanup are tested separately. Server concurrency/queue caps remain those of 0.6.

See [operator guide](DATA.md) for SQL/platform boundaries and
[catalog guide](../examples/DATA.md) for query/transaction/migration/server examples.

Acceptance results: `go test -count=1 ./...` with real remote services enabled
and `go build ./...` passed on Windows/amd64 Go 1.27.4. Full Linux/amd64 race
tests and build passed on Go 1.26.11. The full patched-toolchain vulnerability
audit passed. HTTP catalog tests include simultaneous reads, automatic rollback
at request exit, old-generation drain, failed reload preservation, and closure.
The two disposable database containers were removed after acceptance. Docker
Desktop remains installed under the owner's explicit approval.
