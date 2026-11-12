# GRAB 0.7 — Database access (GRAB Data)

**Status: Approved and implemented; acceptance verification passed**

The owner explicitly approved this specification and GRAB 0.7 implementation on
November 11, 2026, subsequently approving Go 1.26 minimum, dependency downloads,
and disposable database containers. Current Owner remains OpenAI Codex. The
implementation and its supported platform/SQL boundaries are documented in
`docs/DATA.md`; verification evidence is in `docs/DATA-TESTING.md`.

## 1. Purpose and boundaries

Add a modular, bounded database API to the AST interpreter and persistent server.
Follow Blueprint v1.0 Section 7 and ADR-006: implement and verify SQLite first,
then MariaDB, then PostgreSQL, with the same GRAB API and explicit dialect SQL.
SQLite needs no separately installed database service or C toolchain. Completion
of 0.7 requires all three adapters and their conformance tests, not just SQLite.

Included: prepared queries, ordered record/list results, lossless adapters,
connection pools, transactions, versioned schema migrations, capability checks,
server lifecycle integration, diagnostics, examples, and automated tests.

Excluded: ORM/model/relationship syntax, arbitrary SQL generation, database vendor
objects, replication, distributed transactions, background job workers, connector
SDK, and browser database access. Durable job *storage* becomes possible; a durable
job scheduler is deferred to later design. No VM or new general-purpose language
keywords are proposed. SQL dialects and supported server versions must be documented
in the implementation compatibility matrix; SQL is not automatically translated.

## 2. Natural GRAB syntax

Use existing module calls, records, lists, and error handling. A statement is
prepared once, then run repeatedly with a separate record of bound values:

```grab
use data

set shop to data.open using "shop"
set find_product to data.prepare using shop, "SELECT id, name FROM products WHERE id = :id", record {params: record {id: "integer"}, columns: record {id: "integer", name: "text"}}
set product to data.one using find_product, record {id: 42}
if product is nothing
  show "Product not found"
otherwise
  show product.name
end
```

`shop` names an operator-configured connection, not a password or connection URL.
SQL and contracts above are single-line literals, as required by current GRAB
string/collection syntax. Formatting may wrap documentation visually, but does
not introduce multiline string or record grammar.

### Proposed module API

| Call | Result and rules |
| --- | --- |
| `data.open using alias` | Database handle; literal configured alias; verify grant then open/ping within deadline. Reuse one pool per alias per application generation. |
| `data.prepare using db, sql, contract` | Prepared statement handle; SQL and contract must be AST literals; exactly one statement. Prepare on the server/driver, never client-side string interpolation. |
| `data.query using statement, params [, transaction]` | Fully materialized ordered list of records, including an empty list for zero rows. |
| `data.one using statement, params [, transaction]` | One record, or `nothing` for zero rows; more than one row is an error (no silent first-row selection). |
| `data.execute using statement, params [, transaction]` | Record `{rows_affected: integer}`. No portable implicit insert-ID promise; use dialect-specific return queries when supported. |
| `data.begin using db [, options]` | Transaction handle; options are `{read_only: yes/no, isolation: "serializable"/"read_committed"/"repeatable_read"}`. Default serializable, read_only no. Unsupported isolation is rejected, never silently downgraded. |
| `data.commit using transaction` | `nothing` after successful commit. |
| `data.rollback using transaction` | `nothing`; repeated rollback is harmless; commit/use after terminal state is an error. |
| `data.migrate using db, manifest` | Record `{applied: list, current_version: integer}`; literal migration manifest; CLI `run` maintenance execution only, with migration grant. |
| `data.close using db` | `nothing`; CLI-owned pools only, idempotent, rejects active transactions. Server pools are host-owned and cannot be closed by language code. |

`use data` without permission is legal; operations are denied by default. `grab
check` validates imports, arities, SQL-literal requirements, contracts, and manifests
without opening any connection, reading secrets, running migrations, or requiring
a database service. Runtime checks also enforce these rules for embedded callers.

Database, statement, and transaction handles are new opaque **host resources**,
not forgeable records, strings, or vendor values. They are not JSON/template/text
serializable, comparable, or valid database parameters. Ordinary containers may
hold them, but serialization must reject nested handles too. Their internals never
expose passwords. Pool/statement handles may be shared within their owning app;
transactions may not cross a request, task, or host execution boundary. Returned
rows contain only existing portable GRAB values. Resources cannot escape their
owner's lifetime; use after owner disposal fails predictably.

## 3. Prepared SQL and injection protection

1. `data.prepare` accepts a direct text literal, not joined text, a variable,
   function result, or request value. Contracts are literal records. These static
   restrictions intentionally favor auditable queries over dynamic SQL.
2. Portable placeholders are `:name`, with ASCII GRAB identifier names. Bind a
   record whose keys exactly match distinct placeholder names: missing/extra keys
   fail before execution. Repeated names reuse the same value. A query without
   parameters uses `record {}`. Lists do not expand into SQL syntax.
3. A dialect-aware scanner ignores placeholders inside quoted strings,
   identifiers, line/block comments, and PostgreSQL dollar quotes; `::` casts are
   not placeholders. It rewrites real placeholders to driver placeholders and
   prepares SQL through driver APIs. SQL and bound values never share a string.
4. Parameters represent values only. Table/column names, ordering directions,
   operators, and SQL fragments cannot be bound. Choose among explicitly prepared
   literal queries for dynamic sorting/filter shapes. Do not add an unsafe escape
   hatch or a query-concatenation API.
5. Reject multiple statements (one optional trailing semicolon is permitted).
   Disable MariaDB multiStatements and interpolateParams. Prepared statements are
   mandatory even for SQL with zero parameters. Bound values are checked against
   the contract before any driver conversion.
6. Reject transaction-control, connection/session reconfiguration, attachment,
   file import/export, extension loading, and privilege-changing SQL through the
   public query API. Schema DDL is only accepted by `data.migrate`. A real dialect
   classifier/SQLite authorization layer is required; substring/regex checks alone
   are not a SQL security boundary. Read-only grants use both classification and
   database/driver read-only enforcement; writable CTEs, PRAGMAs, and side-effecting
   functions must not bypass the grant. Unclassifiable statements fail closed.
7. Remote database privileges are also mandatory: a GRAB SQL filter cannot prove
   arbitrary server functions/triggers are harmless. Use dedicated least-privilege
   accounts; no FILE, extension, superuser, program-execution, or administrative
   privileges. Procedures and user-supplied functions are outside the initial API.

Parameter separation follows [Go's SQL injection guidance](https://go.dev/doc/database/sql-injection).
Prepared values do not authorize a database, table, or business action: application
authorization is still required for each visitor's operation.

## 4. Contracts and lossless values

A contract has exactly `params` and `columns` records. Each entry is a type name
or descriptor record. A descriptor uses `type`, optional `nullable` (default no),
and type-specific constraints. Allowed names: `integer`, `decimal`, `text`,
`boolean`, `timestamp`, `bytes`, `float`. Unknown fields/types fail validation.
Decimal requires `precision` and `scale` (positive precision, 0 <= scale <= precision);
timestamp requires `zone: "utc"` or `zone: "naive"` and `fraction_digits` (0–9).
Integer optionally supplies `min`/`max` integer bounds. SQLite decimal additionally
requires `storage: "text"`. Other adapters reject that storage option.

For example, an optional exact currency column is described as:

```grab
set price_query to data.prepare using shop, "SELECT price FROM products WHERE id = :id", record {params: record {id: "integer"}, columns: record {price: record {type: "decimal", precision: 18, scale: 2, storage: "text", nullable: yes}}}
```

Parameter and result contracts are checked; mismatches fail, rather than guessing.
Result names must be unique valid GRAB identifiers and exactly match the columns
contract, with records in SELECT-column order. Use explicit aliases; duplicate,
unnamed, or unsupported columns fail even for an empty result. `SELECT *` is rejected
to avoid hidden schema drift. Row order is SQL result order; require `ORDER BY`
when deterministic application ordering matters. Writes use empty columns; a
statement returning columns must use query/one, not execute.

| SQL value | GRAB representation and binding rule |
| --- | --- |
| Signed/unsigned integer | Arbitrary-precision `integer`; parse digit text or native integer exactly. Reject out-of-range binds for the declared SQL target; never wrap or convert through float. SQLite native integers are signed 64-bit; larger values require an explicitly text-backed schema and text contract, not a fake native integer promise. |
| DECIMAL / NUMERIC / currency | Exact base-10 `decimal`, transmitted as canonical decimal text, never float64. Reject values requiring rounding or overflowing the declared precision/scale. Trailing-zero padding is allowed; value and declared column scale are retained on decoding. No automatic currency conversion; store currency codes separately. PostgreSQL MONEY is unsupported; use NUMERIC. |
| TEXT / VARCHAR | UTF-8 `text`, with no numeric/date coercion; invalid UTF-8 is an error. Database collation may affect comparison, never adapter conversion. |
| BOOLEAN | `yes` / `no`. PostgreSQL native bool; SQLite constrained INTEGER 0/1; MariaDB explicitly contracted TINYINT 0/1. Other numbers are errors. Disable driver's broad nonzero-to-true conversion. |
| Timestamp with instant semantics | Canonical RFC3339 UTC `text` retaining declared fractional digits (up to nanoseconds). Normalize offsets without losing the instant. PostgreSQL timestamptz and MariaDB TIMESTAMP use UTC sessions. Reject precision overflow and invalid/infinite/zero dates. |
| Timestamp without zone | Canonical `YYYY-MM-DDTHH:mm:ss[.fraction]` text with `zone: "naive"`; do not invent UTC or the machine timezone. SQL DATE/TIME alone are text contracts in this milestone, not silently converted into instants. |
| BLOB / BYTEA / binary | Copied GRAB `bytes`, including empty bytes; never decode as text/base64 implicitly. |
| NULL | `nothing`, only when contract says `nullable: yes`; a NULL column remains a present record field. Empty text, zero, no, and empty bytes are distinct. |
| REAL / DOUBLE | Explicit `float` contract only, finite binary64; SQL NaN/infinity are errors. Never silently convert approximate floats into exact decimal/currency. |

SQL arrays, JSON-native objects, intervals, vendor enums, spatial types, and other
unsupported types require explicit SQL conversion to supported text/binary columns
or fail. GRAB records/lists are result containers, not automatically serialized SQL
parameter values. No flattening, implicit JSON encoding, or locale conversions.

### SQLite exact-money rule

SQLite's DECIMAL/NUMERIC affinity can store numbers as binary floating point;
the type name alone does not promise exact currency. Store exact decimal values
in **TEXT-affinity columns**, bind canonical decimal text, and decode through the
decimal contract. Reject REAL/INTEGER storage classes for such result columns,
including a caller's mistaken `CAST(... AS REAL)`. Use GRAB exact arithmetic for
money; SQLite text ordering/SUM is not decimal arithmetic. A future decimal SQL
extension needs separate approval. See [SQLite's type rules](https://www.sqlite.org/datatype3.html).

For MariaDB and PostgreSQL use genuine DECIMAL/NUMERIC columns matching the
contract, and check available column metadata. Their database-side casts,
expressions, triggers, and wrongly declared schemas can intentionally round values;
the adapter guarantees lossless transport/conversion of supported values, not
proof of arbitrary SQL computation. Migration schemas and query contracts must
agree; conformance tests must detect ordinary precision/scale mismatches. No claim
that strict SQL mode alone eliminates decimal rounding.

Timestamp text deliberately avoids the current second-resolution `dates` helpers;
feeding a SQL timestamp through those helpers is not a lossless round trip.

## 5. Configuration and default-deny permissions

Proposed host CLI flags for `run` and `serve`:

```sh
grab run --db-config grab.data.json --allow-db shop:read-write app.grab
grab serve --db-config grab.data.json --allow-db shop:read app.grab
grab run --db-config grab.data.json --allow-db shop:migrate migrations.grab
```

`--db-config` selects a strict JSON manifest, relative to the invocation directory.
No implicit discovery of secrets or environment DSNs. Example local configuration:

```json
{
  "connections": {
    "shop": {"driver": "sqlite", "path": "storage/shop.db"}
  }
}
```

This is zero-service setup, not a promise of zero configuration. Connection paths
resolve relative to this manifest; parent directories must already exist. `:memory:`
is also allowed for isolated tests and uses one persistent connection per pool;
reject max_idle 0, idle eviction, and lifetime rotation for that configuration so
an idle pool cannot silently erase its database.

Repeated `--allow-db alias:mode` grants only manifest aliases, never `*`:

- `read`: SELECT/read access, read-only database connection/transaction.
- `read-write`: read plus DML; no schema changes.
- `migrate`: read/write plus the migration API, CLI maintenance execution only.

Duplicate conflicting grants, missing aliases, unknown modes/options, and invalid
targets fail before application execution. Grants bind to canonical immutable
connection definitions, not merely a reusable string. The manifest is operator
configuration explicitly authorized by the flag; language code cannot replace
it, mutate connection options, or choose a new DSN. Without a matching grant,
opening a connection raises `GRAB-E403`; no file/network activity occurs first.

`--allow-db` is a **separate database capability**. `--allow-read`/`--allow-write`
do not grant SQL access; `--allow-net` does not grant database access. A database
grant authorizes only its SQLite database plus required journal/WAL/SHM sidecars,
or its configured remote endpoint. It does not grant general file/HTTP access.
Apply existing sensitive-path denial, canonical path/symlink confinement, safe
creation without traversal races, and read-only opening without accidental file
creation. Reject SQLite URI filenames, ATTACH, arbitrary VFS selection and
extension loading; ensure journal/temp files cannot escape the authorized area
(use memory temp storage). Review and verify driver authorization/VFS mechanisms
before accepting the SQLite adapter; do not substitute a superficial SQL blacklist.

Remote manifest definitions have `driver`, `host`, `port`, `database`, `user`,
`password_env`, and `tls` (`verify` default). Resolve password only from the named
host environment variable after permission validation; literal passwords, arbitrary
DSNs, driver passthrough options, and passwords in GRAB source/CLI arguments are
rejected. Reject unix sockets in 0.7; specify TCP destinations. Hostname/IP grants
pin the configured host and port. Apply existing SSRF address checks on every
connection/reconnect: private/loopback targets require literal IP configuration,
not a public-looking hostname resolving privately. Cloud metadata and link-local
targets remain denied. Verified TLS 1.2+ is required remotely; `tls: "disabled"`
is permitted only for an explicitly configured literal loopback address. No
skip-verification option. Custom CA file paths are explicitly scoped host config,
not language filesystem permissions. Never log credentials, parameter values,
connection URLs, or raw driver errors containing them.

## 6. Drivers, pooling, limits, and lifecycle

Use a host-owned `internal/data` abstraction over Go `database/sql`; do not embed
SQL/vendor behavior into parser or server packages. Proposed adapters:

1. `modernc.org/sqlite` (pure Go, no CGO) for SQLite.
2. `github.com/go-sql-driver/mysql` for MariaDB with server-side preparation,
   multi-statements off, interpolation off, explicit boolean decoding, UTC session,
   strict SQL mode, and local file loading disabled.
3. `github.com/jackc/pgx/v5/stdlib` for PostgreSQL via database/sql, extended
   prepared protocol, UTC session, verified TLS, and no simple-protocol fallback.

Exact dependency versions/server compatibility are an implementation gate: select
maintained releases with security review and verify Go 1.22.2 compatibility; if a
Go toolchain upgrade is needed, obtain owner approval rather than silently
changing go.mod or pinning an insecure dependency. No dependencies added in this
design phase. Record pinned versions and tested servers before milestone completion.

Pool configuration is optional per-alias host manifest configuration, with strict
integer ranges. Defaults (SQLite / remote): max_open 1 / 10; max_idle 1 / 5;
max_lifetime_ms 0 / 1,800,000; idle_timeout_ms 300,000. Ranges: max_open 1–100,
max_idle 0–max_open, lifetime 0 or 1,000–3,600,000 ms, idle timeout
1,000–3,600,000 ms. SQLite stays max_open 1 initially; serialize writes, enable
foreign keys on **every** connection, no automatic WAL change during read-only
opening. Do not enable shared-cache SQLite to solve pool lifetime issues.

Per-alias limits: operation_timeout_ms default 5,000 (1–60,000),
max_rows default 10,000 (1–100,000), max_result_bytes default 10 MiB
(1–100 MiB), max_value_bytes default 1 MiB (1–10 MiB), max_statements default
128 (1–1,024), transaction_timeout_ms default 10,000 (1–60,000).
Count actual UTF-8/binary/numeric payload bytes with overflow-safe accounting.
Exceeding limits fails the whole query; no partial list or truncated value.
Statements have at most 256 distinct parameters and 1 MiB SQL text. Manifest
limit is 1 MiB; migrations have at most 1,000 versions/100 steps per version.

The earliest request/task/transaction/operation deadline wins, including pool
waiting and connection creation. Apply server-side statement/lock timeouts where
supported, in addition to context cancellation. If a driver cannot promptly cancel
and release/discard connections, its adapter fails acceptance. No goroutine per
query that survives cancellation and no unbounded queue. Never automatically retry
writes, transactions, or an ambiguous commit. Busy/conflict errors return to the
application. Read retries are also off initially.

Prepared handles bound the statement count per pool; no unbounded hidden cache.
Row cursors always close on success, error, cancellation, or limits. Execute errors
do not promise no side effects; use transactions for multi-step atomicity.

For `run`, host scope closes every statement/pool and rolls back outstanding
transactions on all exits. For `serve`, startup opens/prepares once; pool and
statement handles are the only intentionally shared host resources in the otherwise
immutable prepared environment. Request rows/bindings remain isolated. A reload
creates a new resource generation: preparation failure disposes only new resources;
successful swap lets old requests drain before closing old statements/pools.
Shutdown follows the same rule. Readiness includes startup connection checks but
health endpoints never run migrations or expose database errors/secrets. Live
credential/config reload uses a full generation change; never mutate active pools.

These rules extend [Go connection pooling](https://go.dev/doc/database/manage-connections)
and [transaction handling](https://go.dev/doc/database/execute-transactions).

## 7. Transactions

```grab
use data
set shop to data.open using "shop"
set debit to data.prepare using shop, "UPDATE accounts SET cents = cents - :amount WHERE id = :id", record {params: record {amount: "integer", id: "integer"}, columns: record {}}
set tx to data.begin using shop
try
  call data.execute using debit, record {amount: 500, id: 1}, tx
  call data.execute using debit, record {amount: -500, id: 2}, tx
  call data.commit using tx
catch problem
  call data.rollback using tx
  raise problem
end
```

This illustrates atomic transfer only; real applications must check account
existence, authorization, affected-row counts, and nonnegative balance constraints.

A transaction leases one connection. All operations within it use transaction
APIs and a statement from the same database handle. While a request/task owns a
transaction on a database, calls on that database without the transaction argument
fail immediately (avoid single-connection SQLite deadlock/accidental autocommit).
Other requests retain independent pool waiting with bounded deadlines. At most one
active transaction per execution/task, no nested transactions/savepoints in 0.7.
No sharing transactions in `do together`; pool/statement handles may be shared,
but concurrent effects are database-ordered, not source-ordered. GRAB's deterministic
task *binding/output* commit does not undo SQL already committed by another task.

Any statement error marks the transaction rollback-only for all drivers. Catching
an error does not make the transaction committable. Leaving its request/task/run
scope or exceeding its deadline always rolls back; no implicit commit. After
commit uncertainty, raise a distinct outcome-unknown error and never retry or claim
rollback succeeded. Closing/discarding a connection is host cleanup, not evidence
that a remote commit did not happen. Use unique business operation IDs for safe
application reconciliation; automatic transaction retries are out of scope.

SQLite serializable transactions are its supported initial isolation; reject
other isolation options. Remote adapters map supported standard levels explicitly
and test actual behavior. No claim of identical lock/concurrency behavior across
engines.

## 8. Schema migrations

Migration files are ordinary `.grab` maintenance programs, not automatically run
on requests/startup/reload. Example proposal for SQLite exact money:

```grab
use data
set shop to data.open using "shop"
call data.migrate using shop, [record {version: 1, name: "create_products", steps: ["CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price TEXT NOT NULL)"]}]
```

The manifest is a literal list of records with exactly `version`, `name`, `steps`.
Versions are consecutive positive integers beginning at 1; names are unique ASCII
identifiers; each step is one literal DDL statement. No arbitrary source execution,
SQL file includes, interpolated SQL, or runtime seed values in migrations. SQL
schema constants (e.g. DEFAULT 0) are permitted. Data backfills/seeds use separately
prepared DML in explicit maintenance programs; resumable backfill orchestration is
deferred. Select a dialect-specific literal manifest in separate migration programs
when schemas differ; do not pretend DDL is vendor-neutral.

Maintain reserved `grab_schema_migrations` ledger: version, name, SHA-256 checksum
over version/name/ordered exact LF-normalized step texts with length delimiters,
UTC applied_at, dirty flag. Applications cannot write the reserved ledger through
the public API. Compare all already-applied versions against the manifest; changes,
gaps, duplicates, dirty entries, or a database ahead of the manifest fail closed.
Re-running an identical complete manifest is a no-op.

Take an engine-specific exclusive migration lock with a bounded deadline before
checking/applying (SQLite write transaction, PostgreSQL advisory lock, MariaDB named
lock scoped by database). Protect ledger creation under the same lock. Apply each
version plus ledger atomically where transactional DDL supports it (SQLite and
PostgreSQL supported subset); rollback failed versions. Reject operations disallowed
inside those transactions. MariaDB DDL can implicitly commit: persist dirty status
before a version, mark clean only after success, stop on failure/crash, and require
operator-reviewed repair/restore. Never advertise rollback of already committed
MariaDB DDL. Provide diagnostics describing manual recovery, not automatic repair.

Migration permission does not bypass sensitive paths, host grants, SQL extension/
file/privilege restrictions, or TLS. Existing backups and a maintenance window are
operator responsibilities. No automatic destructive down migrations. `serve` rejects
migration grant/API; use a separate authorized `run` before deploying an app.

## 9. Diagnostics and verification gate

Proposed new codes (check for collisions before implementation):

| Code | Meaning |
| --- | --- |
| GRAB-E210 | Invalid static database SQL/contract/migration definition. |
| GRAB-E313 | Database operation failed (safe structured category such as constraint, conflict, unavailable). |
| GRAB-E314 | Unsupported type, column/parameter mismatch, overflow, precision loss. |
| GRAB-E315 | Invalid/closed/rollback-only transaction or resource ownership. |
| GRAB-E316 | Migration history/locking/dirty schema error. |
| GRAB-E317 | Database deadline or configured resource limit exceeded. |
| GRAB-E318 | Commit outcome unknown; application reconciliation required. |
| GRAB-E403 | Database capability denied. |

Keep existing error records (`code`, `category`, `message`, `line`, `column`), catch
behavior, fatal execution guards, and diagnostic modes. Source spans point to the
GRAB call. Logs may include alias, statement fingerprint, operation, duration,
row count and normalized error category; never SQL text, bound values, environment
values, credentials, raw driver messages or server request bodies. Production HTTP
responses remain generic. Fingerprints derive only from static SQL, never values.

Implementation must supply table-driven tests and end-to-end conformance for:

- Literal/arity/contract checks, formatter stability, placeholder quoting/comments/
  repeats/PG casts/dollar quotes, invalid/multi-statement SQL, and injection payloads
  that remain data. No database I/O from check/format or denied calls.
- Every type mapping above: integer extrema/unsigned, exact large decimals and
  currency, fractions/timezones/naive timestamps, invalid dates, Unicode, invalid
  UTF-8, NULL versus absent/empty, bytes, unsupported types and duplicate aliases.
- SQLite TEXT exact-money regression against NUMERIC/REAL coercion; foreign keys
  per reconnect; memory-database persistence; denial of attachment/file/extension
  escapes, side-effecting reads, sensitive paths, symlink and sidecar escapes.
- Capabilities/modes/config validation, secrets redaction, remote DNS/reconnect/TLS
  enforcement, and least-privilege read/write/migration separation.
- Real transaction atomicity, concurrency conflicts, rollback-only behavior,
  cancellation/pool exhaustion, forgotten rollback cleanup, task ownership,
  commit uncertainty without retry, result/statement/byte limits, and no leaks.
- Migration no-op/checksum/gap/ahead/dirty rules, concurrent locks, transactional
  DDL rollback and MariaDB nontransactional failure recovery.
- PreparedApp startup/failure/reload/drain/disposal with overlapping generations,
  isolated simultaneous requests, failed preparation preserving the old generation,
  and shutdown cancellation. HTTP errors must not leak database details.
- Real SQLite tests run offline in ordinary `go test ./...`. MariaDB/PostgreSQL
  have real-service integration suites plus documented setup/CI jobs. An absent
  service may skip locally, but those suites must actually run and pass in the
  milestone acceptance environment; mocks or skips do not establish driver support.

Before claiming 0.7 complete: full `go test ./...`, `go build ./...`, all three real
adapter suites, race tests in a supported toolchain, runnable query/transaction/
migration/server examples, updated guides/map/ADRs, driver security audit, and
documented performance/resource baselines. No implementation begins until the
owner explicitly approves this specification.

## 10. Reference basis

Project authority: Blueprint v1.0 Section 7 / ADR-006, ADR-008/009/011/015,
specifications 004–006. Proposed refinements are ADR-024a–024f in the decision
register, approved with this specification.

Primary technical references consulted for design (not promises that GRAB already
implements their behavior):

- [Go database/sql](https://pkg.go.dev/database/sql)
- [SQLite transactions](https://www.sqlite.org/lang_transaction.html) and
  [foreign-key configuration](https://www.sqlite.org/pragma.html#pragma_foreign_keys)
- [modernc SQLite driver](https://pkg.go.dev/modernc.org/sqlite)
- [MariaDB Go driver configuration](https://github.com/go-sql-driver/mysql)
- [PostgreSQL pgx database/sql adapter](https://pkg.go.dev/github.com/jackc/pgx/v5/stdlib)
- [MariaDB DECIMAL](https://mariadb.com/docs/server/reference/data-types/numeric-data-types/decimal)
- [PostgreSQL exact numeric types](https://www.postgresql.org/docs/current/datatype-numeric.html)

## Owner approval checkpoint

Approve or request changes to: the prepared-call API and explicit contracts;
SQLite → MariaDB → PostgreSQL delivery order; exact-money/timestamp rules; named
configuration with explicit read/read-write/migrate grants; bounded resource
lifecycle; and maintenance-only versioned migrations. Target approval alone does
not approve these proposed decisions or authorize Go database implementation.
