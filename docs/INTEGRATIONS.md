# Connectors, verified webhooks, and durable events

GRAB 0.8 Part 2 adds `use connect`, `use events`, and SQL-backed incoming and
outgoing work. It does not provide exactly-once delivery, a commercial provider
integration, automatic migrations, or an authentication framework.

## Vendored connectors

A connector is a data-only directory: `connector.json`, JSON contracts/vectors,
and README. The provider-neutral reference is `examples/connectors/reference`.
No package scripts, native libraries, Go plugins, registry, or installer run.

Platform `connectors` maps an alias to `package` (project-relative directory),
exact `version`, `secrets` (credential aliases to environment-variable names),
optional development mode and allowed private-network CIDRs. Generate and review
the SHA-256 lock explicitly:

```text
grab connect lock --platform-config grab.platform.json
grab connect inspect --platform-config grab.platform.json
```

The lock lists every package file and its hash. Startup and offline checking
reject changes, additional files, missing files, symlinks, unsupported executable
files, and version mismatches. Runtime never updates the lock. The SDK includes
the reference fake-provider `connect.Harness`, an in-memory HTTP transport, and
raw-byte signature helpers for sanitized local tests.

Manifest version 1 declares `id`, `package_version`, `provider_version`, documented
`sources`, one exact `origin`, `types`, and named `operations`. Each operation
declares method/path, input/output contracts, timeout/body bounds, credential
requirements, and classification (`read-only`, `idempotent`, or `unsafe`). Path
and query field mappings select named input fields. Body mappings select fields
or contract-checked JSON literals; response mappings select/rename named fields.
No expression evaluator is used. Authentication is Bearer, API-key, or Basic
from host secret references. Idempotent operations declare a stable key header
and documentation for the provider's behavior.

`connect.call using "alias", "operation", input` requires the exact
`--allow-connector alias:operation` grant. Endpoints and credentials never come
from input. Direct calls perform one attempt. Persisted outbox delivery supplies
the stable idempotency key for idempotent operations. Use the outbox for side
effects requiring recovery. HTTP grants and connector grants are independent.
HTTPS verifies certificates; plaintext requires a literal loopback development
origin. Every dial rechecks DNS and uses a checked IP. Redirects, arbitrary paths,
hop-by-hop headers, and credential overrides are refused.

## Durable stores and migration

Platform `events` entries declare `database` (existing data alias), `namespace`,
`max_pending` (default 100,000), `max_bytes` (256 MiB), `concurrency` (4), and
`max_attempts` (8, maximum 20). Event grants are `alias:accept` and `alias:work`;
database read-write grants are separately required. SQLite is a local
single-process development store under the existing confined rollback-journal
boundaries. Use MariaDB or PostgreSQL for multiple processes.

```text
grab migrate --platform-config grab.platform.json --db-config grab.data.json --events-store queue --allow-events queue:work --allow-db shop:migrate
```

This explicit maintenance command applies adapter-owned versioned tables and
initializes the configured namespace. Event migration history is separate from
application history. Checksums and dirty records are checked on startup. Serving
and reload never migrate. MariaDB DDL failure leaves a dirty migration requiring
operator inspection; never clear it blindly. Retain backups and reconcile any
unknown commit before repairing history. SQL language calls cannot manipulate
the internal event tables. Database durability and encrypted backups remain
operator responsibilities.

## Webhook acceptance

Each platform `webhooks` entry declares exact POST `path`, connector alias,
event `store`, trusted `tenant`, `handler`, contract `type`, and one or two `keys`
(environment-variable names). These paths are reserved against route collisions.
Secrets resolve only while preparing the serving generation, never in check.

The reference profile uses `Grab-Event-Id`, `Grab-Event-Time` (Unix seconds), and
`Grab-Signature` (`v1=` and lowercase HMAC-SHA256). It signs
`event_id.timestamp.exact-body-bytes`. Maximum skew is 300 seconds. Duplicate
headers, malformed IDs/MACs, unsupported content encodings, and tampering fail
before inbox admission. Two active keys permit rotation. This profile is not
Stripe compatibility.

A verified payload is contract-validated and committed before 202. Identical
signed ID/body replay returns 200; an ID with different raw bytes returns 409.
The unique identity includes connector instance and trusted tenant. A full queue,
storage outage, or uncertain commit returns 503. Invalid signatures return 401;
oversized bodies return 413. Neither creates work. No payload, secret, signature,
or credential is written to ordinary request logs.

## Handlers and outgoing work

Handlers are `make receive using event`. The event contains
`id,tenant,type,payload,attempt,transaction`. The host owns the transaction.
Prepare and execute literal business SQL on the store's database; enqueue outgoing
work with `events.enqueue using event.transaction, "queue", "reference", "send", input, stable_key`;
then call `events.complete using event.transaction, event`.

The host commits only after successful return, completion, and a live lease/token
check in the same SQL transaction. Business changes, outbox rows, and completion
commit together. Exceptions, missing completion, cancellation, or an expired lease
roll everything back. Handler frames cannot commit/rollback the transaction,
perform HTTP/connector side effects, access another database, share the transaction
with tasks, or return a resource. Preparation inside these host-owned transactions
is bounded and scoped; ordinary application transaction rules are unchanged.

Claims increment fencing tokens, expire after 30 seconds, and use 10-second
handlers. Outbox leases renew every five seconds; the shorter inbox transaction
holds the claim lock through completion. Unsafe outgoing claims abandoned after
a crash become uncertain. Stable keys survive retries and reconciliation.
Read-only/provider-idempotent work retries only transport failures, 408, 429, and
declared transient 5xx responses. Full-jitter exponential delay starts at one
second and caps at 15 minutes; Retry-After cannot extend the 24-hour deadline.
Auth/contract failures and other 4xx are permanent. Attempt limits and deadlines
persist. No global ordering or cross-database transaction is promised.

## Inspection and recovery

```text
grab events inspect --store queue --id ID [platform/database configuration and grants]
grab events retry --store queue --id ID --reason "configuration repaired" [configuration and grants]
grab events resolve --store queue --id ID --outcome delivered --reason "provider confirmed" [configuration and grants]
```

All require work and database read-write grants. Inspect returns only ID, state,
attempts, and next-attempt time. Retry permits dead work and explicitly reconciled
not-delivered work. Uncertain work requires `resolve` first. Resolving as
not-delivered does not send anything: a separate retry is required. Resolving as
delivered completes the item. Maintenance records the reason in an audit table;
do not put secrets in reasons. Retry preserves the original key.

Completed IDs and digests are retained; there is no background deletion of
pending or dead work. Protect and size storage accordingly. Use `grab events purge --store queue --before UNIX_MILLISECONDS --reason TEXT`
with the same configuration/grants for bounded audited retention maintenance.
It removes at most 1,000 completed records per call, only after at least 30 days
from completion. It never removes live, uncertain, or dead work. Configure a longer
retention window by selecting an earlier cutoff; provider replay windows may
require it. Replays after an explicit purge need lasting business-level protection.
Final acceptance still requires long-running fault/load tests, cross-platform
races, and multi-process tests alongside Part 3.

Test evidence and remaining acceptance are recorded in `HANDOFF.md`. A passing
offline suite does not replace real Redis/database/browser and load gates.
