# GRAB Data (0.7)

Import `data` to use an explicitly authorized named database. See the complete
contract in [specification 007](../specifications/GRAB-DATA-SPEC-007.md) and the
[runnable catalog](../examples/DATA.md). Go 1.26 or newer is required; use a
current patched Go release. SQLite builds without CGo or a Windows C compiler.

The interpreter provides `open`, `prepare`, `one`, `query`, `execute`, `begin`,
`commit`, `rollback`, `migrate`, and `close`. SQL, contracts, connection aliases,
and migration manifests must be direct literals. Values are separate named
parameters; there is no raw execution API. Results are ordered records/lists.

## Operator configuration

Pass `--db-config manifest.json` and repeat `--allow-db alias:mode` as needed.
Modes are `read`, `read-write`, and maintenance-only `migrate`. No grant means
no access. Filesystem and HTTP grants confer no SQL access. `grab migrate`
executes an explicit `.grab` maintenance program, using the same language and
capabilities as `grab run`; it does not discover or automatically run migrations.
`grab check` and `grab format` do not open databases or read their credentials.

```json
{
  "connections": {
    "shop": {"driver": "sqlite", "path": "storage/shop.db"},
    "warehouse": {
      "driver": "postgres", "host": "db.example.com", "port": 5432,
      "database": "warehouse", "user": "grab_reader",
      "password_env": "WAREHOUSE_DB_PASSWORD", "tls": "verify"
    }
  }
}
```

Remote driver names are `mariadb` and `postgres`. Passwords come only from the
named host environment variable after permission checking. Use separate limited
database accounts for readers, writers, and schema maintainers. Database-side
account permissions remain an additional boundary. Never use a superuser or a
MariaDB account with global FILE privileges for an application.

Remote TLS verifies certificates and hostnames, with TLS 1.2 minimum and no
plaintext fallback. An optional `tls_ca` selects a PEM CA bundle relative to the
manifest; only that bounded certificate file is read. The default uses system
roots. TLS may be disabled only for an explicitly configured literal loopback IP.
Every physical reconnect validates and pins the destination address. DNS names
resolving to private/loopback addresses, and all link-local/metadata targets, fail.
There are no Unix sockets, arbitrary DSNs, or driver option passthrough.
Inherited PostgreSQL service/passfile/client-certificate file selectors are
rejected; configure the approved alias and optional CA explicitly instead.

Optional `pool` and `limits` objects use the exact names/ranges in specification
007, section 6. SQLite has one physical connection. Memory databases keep that
connection alive and reject eviction settings. Default query timeout is 5 seconds;
transactions last at most 10 seconds. Defaults cap rows at 10,000, result payloads
at 10 MiB, individual values at 1 MiB, and prepared handles at 128 per pool.
Pool waits and queries honor deadlines. Native SQLite value/SQL/parameter limits
also bound engine allocation. Database-server resource limits are still needed
for expensive remote computations; transport budgets are not an SQL cost planner.

## Types and transactions

Contracts reject missing/extra fields and unsupported conversions. Integers stay
exact; SQLite native integers are signed 64-bit. Currency uses exact decimals
with declared precision/scale: SQLite TEXT storage, remote DECIMAL/NUMERIC.
Approximate SQL arithmetic cannot become an exact-money promise. Boolean values
are strictly true/false or 0/1. Bytes are copied; empty bytes remain distinct from
NULL. Timestamps are text with explicit UTC/naive semantics and fractional digits.
Remote timestamp precision is limited to six digits; SQLite text supports nine.
Only explicitly nullable fields accept `nothing`.

Prepare statements before beginning a transaction. Pass the transaction as the
final argument to its `one`/`query`/`execute` calls. Commit explicitly. Errors make
it rollback-only; leaving its block, request, task, or run rolls it back. Handles
cannot be printed, compared, converted to text, serialized, or used by another
execution's transaction. Writes are never automatically retried. An uncertain
commit reports `GRAB-E318` and requires business-level reconciliation.

## Supported SQL and files

This release deliberately accepts a conservative SQL subset: single SELECT,
INSERT, UPDATE, or DELETE statements with explicit result columns, named
parameters, and allowed built-in functions. CTEs (`WITH`), quoted function calls,
arbitrary functions, SELECT INTO, file-loading commands, and SELECT/RETURNING
wildcards fail closed. Ordinary multiplication and `count(*)` are supported.
SQL dialects remain explicit. Schema migrations accept table/index DDL; views,
triggers, virtual tables, procedures, and data-seeding statements are excluded.
Seed data through prepared statements in a separate explicit transaction.

SQLite additionally uses its engine authorizer on every connection, disables
extension/attachment escapes, enables foreign keys, and stores temporary data in
memory. Disk SQLite currently supports Windows/amd64 and Linux/amd64, with a
rooted custom VFS permitting only the database and rollback journal. WAL/SHM and
disk temporary files are refused. Other platforms reject disk access rather than
falling back to an unconfined VFS; memory databases and remote adapters remain
available. Symlink and hardlink sidecars are rejected. Keep database directories
under operator control; filesystem grants do not expand their scope.

Migrations lock the database and compare all applied checksums/names/versions.
SQLite and PostgreSQL apply the pending manifest atomically. MariaDB records a
durable dirty entry before DDL because its schema changes can commit independently.
A failed MariaDB migration requires reviewed repair/restore; retry is refused.
No automatic down migration, repair, or request-time migration exists.

Server pools and prepared handles belong to one prepared application generation.
Requests own isolated transaction scopes. Reload disposes failed candidates and
keeps old pools alive until old requests finish; shutdown cancels/rolls back
remaining transactions. Public HTTP failures stay generic. Driver errors are
normalized without SQL, parameter values, paths, or credentials.

See [database verification](DATA-TESTING.md) for real-service setup, CI, pinned
dependencies, security review, and reproducible performance measurements.
