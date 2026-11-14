# GRAB Architecture Decision Register

## Purpose

This register records the project owner's architecture planning decisions for proposed ADR-011 through ADR-018. It captures approved principles, limits their scope, and identifies work that remains deferred or requires a later decision. It does not claim that any described capability is implemented.

**Approval date:** November 10, 2026

## Document authority

- GRAB Master Engineering Blueprint v1.0 remains the approved architectural baseline.
- Blueprint v1.1 remains a proposed update and is not approved wholesale.
- Existing baseline decisions ADR-001 through ADR-010 are preserved as recorded in Blueprint v1.0, Section 15. This register does not amend them.
- The corrected repository structure on the final page of each blueprint is authoritative. The initial language-engine packages use the existing Go `internal/` tree: `internal/lexer`, `internal/parser`, `internal/ast`, `internal/interpreter`, and `internal/runtime`. Future package locations remain open unless separately decided.
- ADR-011 through ADR-018 below are approved only at the planning or principle level stated in each entry. This register does not approve unresolved details or authorize implementation of deferred features.

## Decision register

| ADR | Decision and rationale | Scope and future dependencies | Explicit deferrals |
|---|---|---|---|
| **ADR-011 — Prepared applications and request isolation** | **MODIFY — approved principle.** Treat immutable prepared application definitions and request-owned execution isolation as design goals. This supports reuse while reducing cross-request state leakage. | A future server contract will define execution frames, ownership and lifecycle. It depends on defined runtime value/state behavior and server lifecycle requirements. | No request machinery or server lifecycle implementation in GRAB 0.1. Immutability boundaries, frame contents, state sharing and reload behavior remain open. |
| **ADR-012 — Parse-on-load and bytecode caching** | **MODIFY — approved principle.** Retain parsing and validation once per application load as a future persistent-server goal, avoiding repeated frontend work. | The parse-on-load policy belongs to future server loading/reload design and depends on stable parsing and validation behavior. | Defer bytecode caching until a VM exists and measurements justify it. Cache format, keys, invalidation, dependency tracking and integrity rules are not approved. |
| **ADR-013 — Optional intermediate representation** | **APPROVE.** An intermediate representation (IR) is optional, not a required pipeline stage. GRAB 0.1 uses an AST interpreter without a mandatory IR, keeping the first implementation direct and testable. | Reconsider an IR only in a later optimization/VM milestone, based on profiling and stabilized language semantics. Any later VM must preserve the conformance behavior of the interpreter. | No IR, bytecode VM, optimizer or native/JIT backend is part of GRAB 0.1. Their designs and implementation remain deferred. |
| **ADR-014 — React/TypeScript interoperability and browser compiler** | **DEFER (narrowly updated for GRAB 0.8 planning).** A GRAB-to-browser compiler and general UI/component model remain deferred. For GRAB 0.8 (`GRAB-SCALE-SPEC-008.md`, in review), a narrowly scoped typed HTTP/JSON contract and offline TypeScript/React API client generator (`ADR-025c`) is proposed for existing server routes. | Reconsider broader browser integration only under a future UI strategy with defined module and browser/server security boundaries. The narrow GRAB 0.8 client generator awaits owner approval of `GRAB-SCALE-SPEC-008.md`. | No GRAB-to-browser compiler, component model, SSR/hydration framework, or wholesale Blueprint v1.1 approval is authorized. |
| **ADR-015 — Language concurrency and server resource policy** | **MODIFY — approved principle.** Preserve explicit language-level concurrency goals, consistent with baseline ADR-007, while separating language semantics from server backpressure and resource-management policy. This keeps language behavior distinct from deployment controls. | Future language/concurrency specifications will define task-group behavior. A separate server contract will define admission control, queueing, backpressure and resource budgets. These designs must coordinate without making Go implementation details the language semantics. | Exact task-group syntax and behavior, cancellation, error propagation, result ordering, fairness, shared state, server limits and overload handling remain open. Concurrency constructs are outside GRAB 0.1. |
| **ADR-016 — Evidence-based performance goals** | **APPROVE.** Use reproducible, workload-specific benchmarks and evidence to set performance goals. This makes comparisons actionable and avoids unsupported performance claims. | Define workloads, environment, methodology and repeatability for the milestones where performance is relevant. Set numeric targets only after representative baselines exist. | No unsupported numeric targets or blanket performance claims are approved. Benchmark criteria do not replace GRAB 0.1 correctness and conformance requirements. |
| **ADR-017 — Server/browser security separation** | **MODIFY — approved principle.** Preserve a clear security boundary between server-side capabilities and browser-delivered code. This reduces accidental exposure of secrets and privileged APIs. | Define detailed contracts at the relevant module, server, security and UI milestones, coordinating imports, data transfer and trust boundaries. | Module rules, serialization formats, output escaping behavior and authorization contracts remain deferred and unapproved. No browser architecture or implementation is part of GRAB 0.1. |
| **ADR-018 — Incremental server rendering and browser interactivity** | **DEFER.** Incremental server rendering and selective browser interactivity remain future implementation proposals, not approved architecture. Their value and design depend on a chosen UI integration model. | Reconsider only after owner approval of a UI strategy and relevant server/browser contracts. Dependencies include ADR-014's integration decision and ADR-017's security boundaries. | No server-rendering model, hydration behavior, client compiler, component syntax or React compatibility is approved. This is not part of GRAB 0.1. |

## GRAB 0.1 scope

GRAB 0.1 remains limited to the formal language specification, lexer, parser, AST, basic interpreter/runtime, CLI, and conformance tests. Its purpose is a small, deterministic language foundation. It does not include a bytecode VM, mandatory IR, request machinery, server, UI, browser compiler, language-level concurrency implementation, or CMS.

## Open design questions requiring future owner approval

The following questions remain open; this register does not decide them:

1. **Later language semantics:** The GRAB 0.1 value scope, syntax, semantics, diagnostics and conformance cases in `specifications/GRAB-LANGUAGE-SPEC-001.md` were approved on November 10, 2026. Numeric operators, equality, coercion, additional value kinds, modules and other features listed as deferred in that specification require future design and owner approval.
2. **Runtime state and server lifecycle:** What is immutable in a prepared application, what state belongs to each request, how are shared values handled, and what are the load, reload, cancellation and cleanup rules?
3. **Concurrency contract:** What syntax and semantics govern task creation and joining, result order, cancellation, failures, fairness, timeouts and shared mutable state? Which resource controls belong only to the server?
4. **Server preparation and caching:** What constitutes an application load or reload, and what evidence would justify bytecode caching after a VM exists? If justified, what cache compatibility, invalidation and integrity rules are required?
5. **Performance evidence:** Which workloads and environments represent GRAB's goals, and what numeric thresholds, if any, should be set after baselines are measured?
6. **Module and browser security boundary:** How should server-only and browser-safe modules be identified, and what serialization, escaping and authorization contracts are required?
7. **Frontend strategy:** What specific React/TypeScript interoperability use case is intended, if any? Is a GRAB browser compiler wanted, and what target and compatibility obligations would it carry?
8. **Rendering model:** Is incremental server rendering with selective client interactivity needed? If so, what rendering, hydration and component model should it use?
9. **HTML templates:** The owner approved a separate future `.grab.html` template milestone on November 10, 2026, and approved its full specification (`specifications/GRAB-LOCALHOST-SPEC-002.md`) on November 11, 2026.

A future proposal must resolve the relevant questions and receive owner approval before these deferred details become architectural commitments or implementation work.

## Future roadmap proposals and approved future milestones

### HTML template files and localhost preview (GRAB 0.2) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-LOCALHOST-SPEC-002.md`, approved November 11, 2026).** On November 10, 2026, the project owner approved a separate milestone for reusable HTML templates and a minimal localhost browser preview (`GRAB 0.2`), with `.grab.html` as the preferred direction. Program logic remains in `.grab` files, and a `.grab.html` template is reusable by multiple `.grab` programs.

On November 11, 2026, the project owner approved the GRAB 0.2 specification in `specifications/GRAB-LOCALHOST-SPEC-002.md`:
- **Interpolation grammar (`ADR-019a` approved):** `{<identifier>}` (such as `{title}`) with `{{` and `}}` for literal braces; no `show` keyword, expressions, or control flow inside `.grab.html`.
- **Lookup and reuse (`ADR-019b` approved):** `grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]`, defaulting to `<basename>.grab.html` in the same directory and rejecting path traversal outside the workspace.
- **Value binding (`ADR-019c` approved):** `.grab` executes first; its final variable environment is passed as a read-only snapshot to the template; unbound template variables fail pre-render verification with a source-located diagnostic; bound values format via GRAB 0.1 canonical display before escaping.
- **Context-aware HTML escaping (`ADR-019d` approved):** Mandatory escaping in HTML element text, quoted attributes, and quoted URL attributes (`http:`, `https:`, `mailto:`, and relative URLs only); default-deny rejection of interpolations in `<script>`, `<style>`, comments, tag/attribute names, `on*` event attributes, and unquoted attributes.
- **Localhost server security and lifecycle (`ADR-019e` approved):** Loopback-only binding (`127.0.0.1` / `localhost` / `::1`), `GET /` and `HEAD /` only, per-request isolated execution frames (`ADR-011`), mandatory security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`), bounded request timeouts, and graceful `SIGINT`/`SIGTERM` shutdown.

This approval authorizes GRAB 0.2 implementation only. It does not approve a production server, browser compiler, React/TypeScript interoperability, or general client-side UI framework.

### Control flow, functions, and richer language (GRAB 0.3) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-LANGUAGE-SPEC-003.md`, approved and implemented November 11, 2026).** On November 11, 2026, the project owner approved **GRAB 0.3 — Control flow, functions, and richer language** and its formal specification in `specifications/GRAB-LANGUAGE-SPEC-003.md`:
- **Expression grammar and operator precedence (`ADR-020a` approved):** Parenthesized grouping `( ... )`; postfix lookup (`.field`, `at`, `has`); unary `not`; multiplicative (`times`, `divided by`, `modulo`); additive (`plus`, `minus`); text concatenation (`joined with`); relational (`[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most`); equality (`is`, `is not`); and short-circuit `and` / `or`.
- **Exact numeric and comparison semantics (`ADR-020b` approved):** Preserve GRAB 0.1 portable numeric rules: `integer` combined with `decimal` promotes losslessly to exact `decimal`; `float` (`binary64`) never mixes implicitly with `integer` or `decimal`; `divided by` or `modulo` by zero raises a source-located `division by zero` runtime error; conditions and logical operators require strict `boolean` (`yes`/`no`) values without truthy/falsy coercion.
- **Collection lookup and membership (`ADR-020c` approved):** `record.field` accesses record fields; `list at index` (0-based integer) and `map at key` (`text` key) access list and map elements; `collection has key_or_field` tests key/field presence and returns `yes`/`no`, preserving the GRAB 0.1 distinction between an absent key/field and `nothing`.
- **Control flow and block scoping (`ADR-020d` approved):** `if` / `otherwise if` / `otherwise` / `end`, `repeat <count> times ... end`, `for each <item> in <collection> ... end`, and `while <condition> ... end` execute with deterministic lexical scoping and a configurable loop iteration budget (default `1,000,000`) guarding against accidental infinite loops.
- **Functions and isolated call frames (`ADR-020e` approved):** Top-level `make <name> [using <params>] ... end` declarations, `give back [expr]`, and calls via `<name> using <args>` or `call <name> [using <args>]`. Functions execute in isolated lexical call frames, enforce arity at pre-execution validation, and bound recursion at 256 frames.
- **Initial profiling harness (`ADR-020f` approved, applying `ADR-016`):** Reproducible Go `testing.B` benchmarks with `b.ReportAllocs()` (`internal/interpreter/benchmark_test.go`) across straight-line, arithmetic/branching, collection-loop, and function-call workloads.

### Core standard library, error model, and permissions (GRAB 0.4) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-STDLIB-SPEC-004.md`, approved and implemented November 11, 2026).** On November 11, 2026, the project owner approved **GRAB 0.4 — Core standard library, error model, and permissions** and its formal specification in `specifications/GRAB-STDLIB-SPEC-004.md`:
- **Standard library module system (`ADR-021a` approved):** Explicit top-level `use <module>` declarations for built-in modules (`text`, `math`, `collections`, `dates`, `files`), invoked via `<module>.<func> using <args>` or `call <module>.<func> [using <args>]` with pre-execution module and arity validation. Includes `text.from` for converting numbers and portable values into canonical text.
- **Structured error model and diagnostic modes (`ADR-021b` approved):** `try ... catch <err> ... end` and `raise <expr>` constructs binding an immutable error record (`code`, `category`, `message`, `line`, `column`), stable `GRAB-E...` error codes across lexical/syntax/validation/runtime/capability failures, non-catchable fatal safety limits (`call stack overflow` and `execution limit exceeded`), and `beginner`, `developer`, and `production` diagnostic modes.
- **Capability-based filesystem permissions (`ADR-021c` approved, applying `ADR-009`):** Default-deny filesystem access for `files.*` unless explicitly granted via `--allow-read <dir>` and/or `--allow-write <dir>`, enforcing strict directory confinement, 10 MiB read size limits, and an unconditional blocklist for sensitive paths (`.git`, `.env`, `.ssh`, private keys).
- **CLI validation and formatting tools (`ADR-021d` approved):** `grab check <file.grab> [template.grab.html]` for static validation without execution, and `grab format [--write] <file.grab>` for deterministic 2-space canonical source formatting.

### HTTP client, JSON, and async task groups (GRAB 0.5) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-CONNECT-SPEC-005.md`, approved and implemented November 11, 2026).** On November 11, 2026, the project owner approved **GRAB 0.5 — HTTP client, JSON, and async task groups** and its formal specification in `specifications/GRAB-CONNECT-SPEC-005.md`:
- **Universal HTTP client module (`ADR-022a` approved, applying `ADR-008`):** Built-in `use http` module (`http.get`, `http.get_json`, `http.post`, `http.put`, `http.patch`, `http.delete`, `http.request`) returning an immutable response `record` (`status`, `ok`, `headers`, `body`, `url`), with automatic JSON request serialization for records/maps/lists, bounded timeouts (`timeout_ms` default `10,000 ms`, max `60,000 ms`), and bounded safe retries (`0`–`3`).
- **Lossless JSON codec and unified record/map lookup (`ADR-022b` approved):** Built-in `use json` module (`json.parse`, `json.stringify`, `json.stringify_pretty`) preserving exact arbitrary-precision `integer` and base-10 `decimal` numbers without binary floating-point rounding, decoding identifier-keyed JSON objects into GRAB `record` values (and arbitrary-keyed objects into `map` values), and permitting both `.field` and `at "key"` / `has` lookup across `record` and `map`.
- **Capability-based network permissions and SSRF defense (`ADR-022c` approved, applying `ADR-009`):** Default-deny outbound network access (`GRAB-E402`) unless granted via explicit `--allow-net <host[,host...]>`, mandatory TLS 1.2+ certificate verification, header secret redaction in diagnostics, and automatic SSRF blocking of loopback, private RFC 1918, link-local, and cloud-metadata (`169.254.169.254`) IP ranges across initial connections and redirects unless a literal local/private host is explicitly allowlisted.
- **Deterministic structured concurrency (`ADR-022d` approved, applying `ADR-007` and `ADR-015`):** Explicit `do together ... end` blocks containing `set <name> to [wait for] <expr>` (or `[wait for] <call_stmt>`) task statements (up to 16 tasks per block) with immutable snapshot isolation, top-to-bottom source-order commit of bindings and `show` output, automatic cancellation of sibling tasks on error, and deterministic earliest-in-source-order error propagation inside `try` / `catch`.

### Persistent application server (`GRAB Server`, GRAB 0.6) — approved and implemented

**Status: SPECIFICATION APPROVED; IMPLEMENTED (`specifications/GRAB-SERVER-SPEC-006.md`, drafted November 11, 2026).** On November 11, 2026, the project owner approved the GRAB 0.6 target and later explicitly authorized Codex to complete specification 006 and the partial implementation.

Approved GRAB 0.6 architecture and server decisions in `specifications/GRAB-SERVER-SPEC-006.md`:
- **Load-once immutable `PreparedApp` and request isolation (`ADR-023a` approved, applying `ADR-011` and `ADR-012`):** Lexing, parsing, preflight name/arity/route checks, top-level startup initialization, and static template validation execute once per application load or atomic reload (`PreparedApp`), never repeated per HTTP request. Every request runs in a fresh isolated scope cloned from the immutable baseline (`BaseEnv`), preserving 100% compatibility with GRAB 0.2 single-file serving (`grab serve <file.grab> [template.grab.html]`).
- **Declarative multi-route syntax & `use server` module (`ADR-023b` approved):** Single new reserved keyword `route "<METHOD>" "<path>" [using <req>] ... end` with `{param}` path segments, deterministic static-before-param matching priority, static route conflict rejection (`GRAB-E209`), and built-in `use server` helpers (`server.html`, `server.json`, `server.text`, `server.redirect`, `server.response`, `server.parse_json`).
- **Deployment modes and explicit trusted-proxy policy (`ADR-023c` approved, applying `ADR-005` and `ADR-009`):** Safe `127.0.0.1:8080` loopback default (supporting direct standalone use and private loopback Nginx reverse proxying without extra flags), explicit `--allow-public` opt-in for non-loopback listeners, explicit `--trusted-proxy` IP/CIDR allowlists so `X-Forwarded-*` headers are never blindly trusted, and opt-in path-confined `--static` file serving blocking sensitive paths and escaping symlinks.
- **Backpressure, bounded limits, supervision & secret-safe logging (`ADR-023d` approved, applying `ADR-015`):** Bounded `--max-concurrency`, `--max-queue` / `--queue-timeout` shedding load cleanly with `503 Service Unavailable`, `--request-timeout`, `--max-body-bytes`, `/_health` and `/_ready` probes, defensive per-request panic isolation, atomic zero-downtime reload retaining last-known-good apps on error, graceful `SIGINT`/`SIGTERM` shutdown, and structured logging with mandatory header/body secret redaction.
- **Server performance scorecard harness (`ADR-023e` approved, applying `ADR-016`):** Repeatable Go HTTP throughput and allocation benchmarks (`internal/server/benchmark_test.go`) measuring `PreparedApp` static, dynamic JSON, and templated HTML serving latency and allocations.

### Database access (`GRAB Data`, GRAB 0.7) — approved and implemented

**Status: Approved for implementation.** The owner explicitly approved
specification 007 and its design on November 11, 2026. The following decisions
are implemented; their normative details and acceptance tests are in
`specifications/GRAB-DATA-SPEC-007.md`. The owner separately approved Go 1.26
minimum and disposable real database containers. See `docs/DATA-TESTING.md`.

- **ADR-024a — Driver order and module boundary (approved; refines ADR-006):**
  SQLite first with the pure-Go modernc adapter, then MariaDB via go-sql-driver/mysql,
  then PostgreSQL via pgx/stdlib, behind `internal/data` and database/sql. Same
  GRAB API, explicit dialect SQL; no automatic dialect translation. All three real
  adapter suites must pass before 0.7 is complete. Dependency/toolchain versions
  require compatibility and security review before implementation.
- **ADR-024b — Prepared module calls and portable results (approved):** Existing
  `use data` / `using` grammar; `open`, `prepare`, `query`, `one`, `execute`,
  `begin`, `commit`, `rollback`, `migrate`, `close`. Direct literal SQL and type
  contracts, named value placeholders, mandatory driver/server preparation,
  exact parameter keys, no concatenation, dynamic identifiers, multi-statements,
  or unsafe execution escape hatch. Result rows are records; query results are
  lists. New opaque host handles cannot be forged or serialized.
- **ADR-024c — Lossless type contracts (approved):** Exact integer and decimal
  adapters, bounded precision/scale validation, explicit nullable fields, strict
  booleans, copied bytes, UTF-8 text, and timestamp text preserving fractional
  precision with explicit UTC versus naive semantics. SQLite currency uses
  TEXT-affinity columns, not NUMERIC/REAL; remote currency uses DECIMAL/NUMERIC.
  Reject unsupported/coerced values rather than silently rounding. SQL arithmetic
  and schema-side conversions remain explicit database semantics.
- **ADR-024d — Database capabilities (approved; applies ADR-009):** Strict named
  operator manifest selected with `--db-config`; repeated `--allow-db alias:mode`
  with read/read-write/migrate grants, default deny (`GRAB-E403`). Grants bind
  immutable destinations; file and HTTP grants confer no database access.
  Confine SQLite files/sidecars and disable extension/attachment escapes; verify
  remote TLS/destinations and least-privilege accounts. Host environment credentials
  stay outside GRAB source/logs; no arbitrary DSNs or driver-option passthrough.
- **ADR-024e — Pool and transaction lifecycle (approved; extends ADR-011/015):**
  Bounded pool/statement/result/deadline budgets; per-execution transaction
  ownership, rollback-only after errors, automatic rollback on scope exit,
  no implicit commit or retry. Report ambiguous commit outcomes explicitly.
  Host-owned server pools/statements span requests within a prepared generation;
  reload/drain closes resources after old requests finish and disposes failed
  candidate resources without disturbing the live generation.
- **ADR-024f — Versioned maintenance migrations (approved):** Explicit
  `data.migrate` in CLI `run` with migrate grant only, never server startup/reload
  or routes. Literal ordered DDL manifests, exclusive locks, checksum/history
  verification, transactional versions where supported. MariaDB implicit DDL
  commits require persistent dirty-state recovery; no false rollback guarantee,
  automatic down migration, or automatic repair. Durable job scheduling is deferred.

ADR-024a–024f were approved with specification 007. The pinned adapters are
modernc SQLite v1.62.0, go-sql-driver/mysql v1.10.1, and pgx v5.13.0. Disk SQLite
uses a confined rollback-journal VFS on Windows/amd64 and Linux/amd64; unsupported
disk platforms and SQL constructs fail closed. See `docs/DATA.md` for boundaries.

### Integrations, client adapters, and server scaling (GRAB 0.8) — approved design

**Status: Specification/design approved by the owner on November 12, 2026; implementation in progress under OpenAI Codex.** The following decisions are authorized design requirements. The platform/cache slice has local implementation; other subsystems and full Section 12 acceptance remain pending. See `docs/CACHE.md`. The original proposed labels below describe the design record, not completion claims:

- **ADR-025a — Platform manifest & capability isolation (`grab.platform.json`, proposed; applies ADR-009):** Private version-1 host configuration (`--platform-config`) alongside `--db-config`, repeatable default-deny capability flags (`--allow-cache`, `--allow-connector`, `--allow-events`, `--allow-channel`, `--allow-telemetry`), host environment-variable secret references only, verified TLS/DNS-pinning/SSRF rules (standalone Redis only; plaintext restricted to explicit dev setting on literal loopback IP), and zero implicit cross-subsystem privileges.
- **ADR-025b — Redis caching & atomic fixed-window rate limiting (`use cache`, proposed):** Explicit `redis` or `memory` drivers (`memory` is never an automatic Redis outage fallback), versioned tagged canonical value codec preserving exact integers/decimals without float rounding, mandatory TTLs, a single host-owned atomic Redis counter script for `cache.limit` (no separate INCR/EXPIRE or blind write retries), and fail-closed `429`/`503` rate limiting.
- **ADR-025c — Typed JSON contracts & generated React/TypeScript clients (`grab.api.json`, `use api`, proposed; narrow ADR-014 update):** Strict contract binding to existing routes (`api.input`, `api.respond`), exact base-10 string wire serialization for integers/decimals (`GrabInteger`, `GrabDecimal`), offline deterministic client generation (`grab generate client --api-contract ... --out ... [--react]`) writing manifest-tracked `types.ts`, `client.ts`, `validators.ts`, `contract.json`, README, and optional `react.ts` with `AbortSignal`, Strict Mode-safe mutations, same-origin defaults, and explicit CSRF/CORS boundaries—excluding any browser compiler, GRAB UI framework, or SSR/hydration.
- **ADR-025d — Vendored Connector SDK & lockfile integrity (`use connect`, proposed; extends ADR-008):** Data-only vendored connector packages (`connector.json`, static request templates, declarative field mappings, test vectors) locked via `grab.connect.lock` SHA-256 hashes; `connect.call` constrained to declared origin/method/path templates and host secrets; HTTPS-only outside explicit loopback test mode; retries off by default for unsafe side effects; no general plugin execution or online package registry.
- **ADR-025e — Verify-first webhooks & durable inbox/outbox (`use events`, proposed):** Reserved exact POST webhook routes verifying raw request bytes and constant-time HMAC signatures (including the `Grab-Event-Id`/`Grab-Event-Time`/`Grab-Signature` reference profile with <=300s skew) before persisting deduplicated inbox rows to SQLite/MariaDB/PostgreSQL (multi-worker requires MariaDB or PostgreSQL); atomic business + outbox commit with incrementing lease fencing tokens, at-least-once delivery (`uncertain` reconciliation state), and audited `grab events inspect|retry|resolve` maintenance commands.
- **ADR-025f — Authorized WebSocket channels (`use sockets`, proposed):** Reserved GET paths with explicit `authorize` and `receive` callbacks, strict `Origin` allowlists, `wss` TLS edge or trusted-proxy policy, contract-validated text JSON frames (binary/compression deferred), bounded connection/message queues with RFC 6455 close codes, and transient memory or standalone Redis pub/sub (`sockets.publish` as a hint; authoritative catch-up via typed HTTP/DB).
- **ADR-025g — Supervised multi-process scaling & rolling reload (proposed; extends ADR-011/015):** `grab serve --workers N --worker-base-port P` supervising 1–32 processes on consecutive loopback ports behind an external reverse proxy; verified hash parity and aggregate DB/Redis pool budgets across workers; bounded crash restart backoff; rolling replacement with 30s drain deadline; no shared mutable globals, automatic cluster election, or schema migration during reload.
- **ADR-025h — Bounded telemetry & mandatory release gate (proposed; extends ADR-009/016/017):** Disabled-by-default loopback OpenMetrics listener with token auth and opt-in TLS OTLP/HTTP traces (`--allow-telemetry`) using low-cardinality labels, W3C trace context without baggage/remote sampling override, and non-blocking drop-on-failure buffers; plus mandatory compatibility, real-Redis, three-engine DB, React/TS fixture, race, and 30-minute sustained fault/load acceptance gates prior to release approval.

### Profile-driven IR and bytecode VM (GRAB 0.9) — approved & implemented

**Status: Complete (`specifications/GRAB-VM-SPEC-009.md`, implemented and verified November 12, 2026 under Google Antigravity).** On November 12, 2026, the project owner approved **GRAB 0.9 — Profile-driven IR and bytecode VM** and authorized implementation under **Google Antigravity**:

- **ADR-026a — Two-tier profile-driven execution (`--engine=auto|ast|vm`, approved & implemented; applies ADR-003 and ADR-013):** Cold startup code and one-shot CLI scripts execute on the AST reference interpreter (`internal/interpreter`), while hot `make` functions (16 calls), loops (64 back-edges triggering enclosing unit compilation on next entry), and `PreparedApp` routes (8 requests) lower once per unit via `sync.Once` to `internal/ir` and `internal/vm`. `--engine=ast` preserves the pure reference oracle; `--engine=vm` compiles eagerly for conformance and benchmark parity. No fragile mid-loop OSR or native JIT compilation.
- **ADR-026b — Generation-owned immutable IR & bytecode (`internal/ir`, `internal/vm`, approved & implemented; extends ADR-011 and ADR-012):** Compiled `vm.Chunk` artifacts (resolved lexical slots, constant pools, PC-range `try`/`catch` handler tables, and run-length source-span tables) are immutable and owned by the `PreparedProgram` / `PreparedApp` generation, retired atomically on reload. Runtime `vm.machine` scratch slices are pooled via `sync.Pool` and scrubbed between runs. On-disk persistent bytecode cache files remain deferred.
- **ADR-026c — Strict semantic equivalence & host-bridge parity (approved & implemented; applies ADR-003 and ADR-013):** The VM reuses `internal/runtime` exact `integer`/`decimal`/`float` arithmetic (with inline `int64` fast path and overflow promotion to `big.Int`) and `internal/interpreter` capability-checked host bridges for all 14 standard library modules, `do together` task groups, and `beginner`/`developer`/`production` diagnostics. `tests/grab09_vm_equivalence_test.go` enforces parity across `--engine=ast`, `--engine=vm`, and `--engine=auto`.
- **ADR-026d — Static bytecode verification & VM safety guards (approved & implemented; extends ADR-009 and ADR-015):** Every compiled chunk passes a fail-closed static bytecode verifier (stack bounds, jump targets, constant/slot/handler indices) before execution. The VM enforces `DefaultMaxLoopSteps` (`1,000,000` → non-catchable `GRAB-E307`), `DefaultMaxCallDepth` (`256` → non-catchable `GRAB-E306`), bounded operand stacks, and `ctx.Done()` preemption at back-edges, calls, and host boundaries.
- **ADR-026e — Evidence-based VM benchmark & acceptance gates (approved & verified; applies ADR-016):** `go test -count=1 ./...` and `go build ./...` pass 100%, differential parity passes across `--engine=ast`/`vm`/`auto`, prepared function/loop/`try-catch` workloads measure 2.33×–3.14× speedup and up to 95% allocation reduction, compute JSON routes measure ~2.0× faster, `BenchmarkPreparedApp` (`text`, `json`, `html`) shows zero regression, and cold-start overhead in `auto` mode measures ~2–4% (< 10%).

### Stable platform & native MCP server (GRAB 1.0) — approved for implementation

**Status: Approved target; specification approved (`specifications/GRAB-PLATFORM-SPEC-100.md`, approved November 13, 2026 under Google Antigravity).** The owner approved GRAB 1.0 and `ADR-027a`–`ADR-027d` for implementation under **Google Antigravity**:

- **ADR-027a — Native GRAB MCP Server (`grab mcp`, approved; applies ADR-009):** Built-in `stdio` JSON-RPC 2.0 MCP server exposing five capability-bounded, workspace-confined tools (`grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, `grab_scaffold`) so external and local AI agents (**Groxy AI**, **OpenCode**, **Antigravity**, **Codex**) inspect, validate, format, and scaffold GRAB apps without hallucinating syntax or escaping `--workspace`. Strict read-only default (`--allow-write` required for disk writes), sensitive-path blocklist (`GRAB-E401`), symlink confinement, and zero shell/network/DB execution.
- **ADR-027b — In-memory asset fingerprinting & smart `Cache-Control` (`server.asset`, approved; extends ADR-011):** `PreparedApp` hashes `--static` assets in memory at startup/reload (supporting `--read-only` containers) and exposes `server.asset` returning `/static/<file>?v=<12-char-sha256>`. `Application.static` serves `Cache-Control: public, max-age=31536000, immutable` when `?v=` matches the served file hash and falls back to `Cache-Control: no-cache` on mismatch or missing `?v=` during rolling multi-worker updates.
- **ADR-027c — Core `v1.0.0` stability & SemVer compatibility freeze (approved):** Freezes GRAB 0.1–0.9 syntax, exact numeric semantics, `.grab.html` context-aware escaping, error codes (`GRAB-E001`–`GRAB-E403`), all 14 standard library modules, `v1` manifest schemas, and `--engine=auto` AST/VM differential parity for the entire `v1.x` release line (additive-only minor updates; deprecation warnings until `v2.0.0`).
- **ADR-027d — Production release, security & two-repository promotion gates (approved; applies ADR-017):** Requires 100% passing Windows/Linux test & Linux `-race` suites, GRAB 0.8 Section 12 30-minute sustained fault/load evidence (`docs/SCALE-TESTING.md`), `govulncheck ./...` + `go mod verify` clean scans, cross-platform binary builds, `--read-only` container verification, and synchronized `v1.0.0` tags/releases on both `origin` (`urbanspress/grab.progress`) and `stable` (`urbanspress/grab.stable`).

### Security Primitives, Capability Env & Opt-In Script CSP (GRAB 1.1) — Approved & Implemented

**Status: Approved & Implemented (`specifications/GRAB-PLATFORM-SPEC-110.md`, November 14, 2026 under Google Antigravity).** Additive `v1.1.0` capabilities under `ADR-027c`:

- **ADR-028a — Native `use crypto` standard library module (approved & implemented):** Implements `crypto.hash_password` / `crypto.verify_password` (`argon2id` `m=19456,t=2,p=1`, 16-byte random salt, PHC format, bounded parser, `crypto/subtle`), `crypto.random_urlsafe` / `crypto.random_hex` (`crypto/rand`, `16..64` bytes), `crypto.sha256_hex` / `crypto.sha256_base64url` (PKCS/OAuth2 PKCE `S256`), and `crypto.encrypt_aead` / `crypto.decrypt_aead` (AES-256-GCM `"v1:"` envelope with explicit 32-byte key decoding and key buffer zeroing), with exact AST/VM parity via `stdlibModules`.
- **ADR-028b — Capability-gated `use env` module & `--allow-env` (approved & implemented; extends ADR-009):** Default-deny `env.get` / `env.has` failing closed with `GRAB-E401` on any variable outside `--allow-env <VAR[,VAR...]>`, contextual `env.has` member parsing preserving operator `has`, unset handling (`nothing`/`no`), propagation across `do together` task groups, and forwarding in `restrictedWorkerEnv`.
- **ADR-028c — Opt-in strict `--allow-scripts` CSP policy on `grab serve` (approved & implemented; extends ADR-011):** Preserves strict default `script-src 'none'` (`object-src 'none'`) and emits `script-src 'self'` (zero `unsafe-eval`/`unsafe-inline` scripts) only when `--allow-scripts` is passed on `grab serve`, enabling local **Grex JS v1.0.0** bundles via `server.asset`.
