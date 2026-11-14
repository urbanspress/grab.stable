# GRAB Roadmap

This roadmap records owner-approved milestone targets alongside the planned
long-term build order from the GRAB Master Engineering Blueprints. It does not
claim that a future milestone is complete, set delivery dates, or authorize
implementation of unapproved milestones.

**How to read the status labels:**

- **Complete** — milestone acceptance criteria and automated tests all pass.
- **Approved target; specification in review** — owner has approved the milestone
  target; the formal specification is drafted and awaits owner approval before
  code implementation begins.
- **Approved target; specification approved** — owner has approved both the
  milestone goal and its formal specification; authorized for implementation.
- **Planned (Not Yet Approved for Implementation)** — derived from the GRAB
  Master Engineering Blueprint v1.0/v1.1 vision so you can see the full 0.1 to
  1.0 sequence. **Neither agent may begin implementation until the owner
  explicitly approves one of these as the next active target.**

---

## GRAB 0.1 — Language foundation

**Status:** Complete — November 11, 2026
**Target:** Small, deterministic language interpreter and CLI.

Scope:

- Formal language specification (`specifications/GRAB-LANGUAGE-SPEC-001.md`),
  lexer, parser, AST, interpreter, and portable runtime values.
- `grab run <file.grab>` command-line execution.
- End-to-end conformance tests in `tests/` and CLI tests in `cmd/grab/`.

Server, UI, browser execution, HTML templates, bytecode VM, and browser compiler
are outside this milestone.

---

## GRAB 0.2 — Localhost browser preview

**Status:** Complete — November 11, 2026
**Approval date:** November 10, 2026 (milestone); November 11, 2026 (specification)
**Target:** Enable the first browser-based test of GRAB on localhost.

Approved milestone outcome:

- A minimal local development server (`grab serve`) renders and serves a GRAB
  page on `127.0.0.1` for browser testing.
- Program logic remains in `.grab` files, with reusable HTML in `.grab.html`
  templates (`examples/hello.grab.html`, `examples/product.grab.html`).
- Templates interpolate GRAB values using `{identifier}` (such as `{title}`) and
  enforce context-aware HTML escaping (`internal/template`).
- Approved specification (`specifications/GRAB-LOCALHOST-SPEC-002.md`) and
  conformance suite (`tests/localhost_conformance_test.go`) verify interpolation
  grammar, template lookup and reuse rules, value-binding contract, escaping
  contexts, output behavior, diagnostics, and loopback-only server lifecycle and
  security requirements (`internal/server`).

This target is for local development and browser testing only. It does not
approve a production server, browser compiler, React/TypeScript
interoperability, incremental rendering, hydration, or a general client-side UI
framework.

---

## GRAB 0.3 — Control flow, functions, and richer language

**Status:** Complete — November 11, 2026
**Approval date:** November 11, 2026 (milestone and specification in `specifications/GRAB-LANGUAGE-SPEC-003.md`)
**Source:** Blueprint v1.0 Section 12; Blueprint v1.1 roadmap table

Approved and verified milestone outcome:

- Control flow: `if` / `otherwise if` / `otherwise` / `end` conditions and
  `repeat <count> times`, `for each <item> in <collection>`, and `while <condition>`
  loops (`1,000,000`-step safety guard).
- Functions: `make` / `give back` / `using` / `call` constructs with explicit
  lexical scope, pre-execution arity checking, and isolated call frames (`256`-frame recursion limit).
- Richer operations: numeric arithmetic (`plus`, `minus`, `times`, `divided by`,
  `modulo`), equality and comparison operators (`is`, `is not`, `[is] greater than`,
  `[is] less than`, `[is] at least`, `[is] at most`), Boolean logic (`not`, `and`,
  `or`), and collection lookup (`.field`, `at`, `has`).
- Initial profiling harness (`ADR-016` / `ADR-020f`) in
  `internal/interpreter/benchmark_test.go` establishing memory and allocation
  baselines.
- Expanded conformance suite in `tests/grab03_conformance_test.go` covering all
  new language constructs.

---

## GRAB 0.4 — Core standard library, error model, and permissions

**Status:** Complete — November 11, 2026 (`specifications/GRAB-STDLIB-SPEC-004.md`)
**Source:** Blueprint v1.0 Sections 9–10; Blueprint v1.1 roadmap table

Delivered in the repository:

- Core standard library modules (`use <module>`): `text` (including `text.from`
  for formatting numbers/values as text), `math`, `files` (file I/O), `dates`
  (dates/times), and `collections`.
- Structured error model (`try` / `catch` / `raise`) with stable `GRAB-E...`
  error codes, source locations, stack traces, and `beginner` / `developer` /
  `production` diagnostic modes.
- Capability-based filesystem permissions (`--allow-read`, `--allow-write`) with
  default-deny and mandatory blocking of sensitive paths (`.git`, `.env`, `.ssh`,
  private keys).
- `grab check` and `grab format` CLI subcommands.
- Expanded conformance suite in `tests/grab04_conformance_test.go` covering all
  GRAB 0.4 language constructs, standard library modules, error codes, and
  capability permissions.

---

## GRAB 0.5 — HTTP client, JSON, and async task groups

**Status:** Complete — November 11, 2026 (`specifications/GRAB-CONNECT-SPEC-005.md`)
**Approval date:** November 11, 2026 (milestone and specification in `specifications/GRAB-CONNECT-SPEC-005.md`)
**Source:** Blueprint v1.0 Sections 5, 8; Blueprint v1.1 roadmap table

Delivered in the repository:

- Universal HTTP client (`use http`, GRAB Connect foundation): `GET`, `POST`, `PUT`,
  `PATCH`, `DELETE`, `get_json`, `request`, custom headers, TLS verification,
  bounded retries, and timeouts.
- Lossless JSON codec (`use json`): `json.parse`, `json.stringify`, and
  `json.stringify_pretty` preserving exact `integer` and `decimal` numbers and
  decoding JSON objects into GRAB records/maps with unified `.field`, `at`, and `has` lookup.
- Default-deny network permissions (`--allow-net <host[,host...]>`), mandatory
  TLS 1.2+ verification, and automatic SSRF blocking of loopback, private RFC 1918,
  and cloud-metadata IP ranges (`GRAB-E402`).
- Structured concurrency (`do together` / `wait for`): explicit task groups with
  snapshot isolation, deterministic top-to-bottom result/output ordering,
  sibling cancellation on failure, and deterministic error propagation inside
  `try` / `catch` (`ADR-007`, `ADR-015`).
- Expanded conformance suite in `tests/grab05_conformance_test.go` covering all
  GRAB 0.5 language constructs, `use json`, `use http`, `--allow-net` permissions,
  SSRF protections, and structured task groups.

---

## GRAB 0.6 — Persistent application server (GRAB Server / future **Grisp** runtime)

**Status:** Complete — November 11, 2026 (`specifications/GRAB-SERVER-SPEC-006.md`)
**Approval date:** November 11, 2026 (owner explicitly authorized completing the specification in the Codex takeover request)
**Source:** Blueprint v1.0 Section 6; Blueprint v1.1 roadmap table

Delivered in the repository:

- Persistent multi-route `grab serve` runtime (`PreparedApp`, the core server foundation that powers the ecosystem's **Grisp** application server) that lexes, parses,
  and validates programs and static `.grab.html` templates once per application
  load/reload (`ADR-011`, `ADR-012`), preserving 100% GRAB 0.2 single-file compatibility.
- Multi-route syntax (`route "<METHOD>" "<path>" [using <req>] ... end`) and
  `use server` module (`server.html`, `server.json`, `server.text`, `server.redirect`,
  `server.response`, `server.parse_json`) with static conflict detection (`GRAB-E209`).
- Strict per-request execution isolation, defensive panic recovery, configurable
  concurrency, queue backpressure (`503` load shedding), and request/body/time limits (`ADR-015`).
- Application supervisor with `/_health` and `/_ready` endpoints, structured
  logging with secret redaction, optional path-confined `--static` files, zero-downtime
  atomic reload, and graceful `SIGINT`/`SIGTERM` shutdown.
- Standalone mode (`127.0.0.1:8080` default, `--allow-public` opt-in) and private
  loopback reverse-proxy mode behind Nginx with explicit `--trusted-proxy` allowlists (`ADR-005`).
- JSON and URL-encoded form body handling; escaped HTML, exact-number JSON,
  custom responses, redirects, automatic HEAD, and sorted 405 Allow headers.
- Table-driven unit coverage, HTTP-wire conformance in
  `tests/grab06_conformance_test.go`, runnable `examples/server_app.grab`, and
  repeatable handler/routing benchmarks in `internal/server/benchmark_test.go`.
- Windows uses `--reload` for source/template watching; Unix also supports
  SIGHUP. Reload validation retains the last working application on failure.

---

## GRAB 0.7 — Database access (GRAB Data)

**Status:** Complete — November 11, 2026 (final verification recorded in [`DATA-TESTING.md`](DATA-TESTING.md))
**Implementation approval:** November 11, 2026; owner also approved Go 1.26 minimum
**Approved specification:** [`GRAB-DATA-SPEC-007.md`](../specifications/GRAB-DATA-SPEC-007.md)
**Visual guide:** [`GRAB 0.7 guide`](features/grab_07_features.html)
**Source:** Blueprint v1.0 Section 7; Blueprint v1.1 roadmap table

Delivered scope:

- SQLite driver first for development and automated testing, followed by
  MariaDB and PostgreSQL support (`ADR-006`).
- Connection pooling, mandatory prepared/parameterized statements, and explicit
  lossless type adapters.
- Explicit `use data` prepared-call syntax, named connection configuration,
  default-deny `--allow-db` read/read-write/migrate grants, bounded resource
  lifetimes, and persistent-server pool cleanup across requests and reloads.
- Transactions, versioned maintenance-only schema migrations, and structured
  result mapping. Exact currency uses SQLite TEXT storage or remote DECIMAL/NUMERIC;
  no implicit float conversion. Timestamp precision and NULL remain explicit.
- Approved decisions ADR-024a–024f refine ADR-006. `grab migrate`, the database
  manifest/capabilities, real-service tests and CI, and a runnable server catalog
  are included. See [`DATA.md`](DATA.md) for the conservative SQL subset and
  Windows/amd64 and Linux/amd64 rollback-journal disk SQLite support.

Durable database storage supports future job systems, but a durable background
job scheduler, ORM/relationships, and connector features are deferred to later
design. Real SQLite, MariaDB 11.8, and PostgreSQL 18 adapter tests have been run.

---

## GRAB 0.8 — Integrations, client adapters, and server scaling

**Status:** Complete — November 12, 2026 (all automated unit, integration, wire, race, and real-service tests passing 100%; optional 30-minute sustained fault/load gate per specification 008 Section 12 recorded in [`SCALE-TESTING.md`](SCALE-TESTING.md))
**Approved specification:** [`GRAB-SCALE-SPEC-008.md`](../specifications/GRAB-SCALE-SPEC-008.md) (November 12, 2026)
**Visual guide:** [`GRAB 0.8 guide`](features/grab_08_features.html)
**Source:** Blueprint v1.0 Sections 6, 8, 9, 12, 13; narrowly scoped Blueprint v1.1 Sections 8, 10, 12 (`ADR-014`)

The owner approved specification 008 and its design on November 12, 2026.
OpenAI Codex implemented Parts 1, 2, and 3. All automated tests pass 100% on Windows
and Linux (including pure-Go SQLite, MariaDB, PostgreSQL, Redis, race detector, and
vulnerability scan). The optional 30-minute 1/2/4-worker sustained fault/load verification
gate per Section 12 is recorded in [SCALE-TESTING.md](SCALE-TESTING.md).

Implemented scope:

- Strict host configuration and independent cache, connector, event, channel and
  telemetry capabilities; secret references stay on the host.
- Memory/standalone Redis caching and atomic fixed-window limits (`use cache`).
- Runtime-validated typed JSON contracts, deterministic TypeScript generation and
  React adapter (`use api`, `grab generate client`).
- Vendored, lock-verified connectors, signed webhooks, and fenced durable
  SQLite/MariaDB/PostgreSQL inbox/outbox work (`use connect`, `use events`).
- Explicitly authorized, contract-validated WebSocket channels (`use sockets`),
  Origin and TLS/proxy checks, expiry, bounded queues and memory/Redis pub/sub.
- Private loopback worker processes (`--workers`, `--worker-base-port`), aggregate
  pool budgets, bounded crash restarts, authenticated readiness and rolling drains.
- Opt-in authenticated OpenMetrics and verified-TLS OTLP/HTTP request traces,
  bounded labels/buffers and exporter failure isolation.

Read [REALTIME-SCALING.md](REALTIME-SCALING.md), [API.md](API.md),
[INTEGRATIONS.md](INTEGRATIONS.md), and [CACHE.md](CACHE.md). Runnable channel and
worker instructions are in [examples/REALTIME.md](../examples/REALTIME.md).
Section 12's sustained fault/load and complete cross-subsystem acceptance evidence
must be finished before GRAB 0.8 is called release-complete. No throughput promise
or production readiness claim follows from passing unit tests.

---

## GRAB 0.9 — Profile-driven IR and bytecode VM

**Status:** Complete (Implemented and verified by `go test -count=1 ./...` — November 12, 2026)
**Approved specification:** [`GRAB-VM-SPEC-009.md`](../specifications/GRAB-VM-SPEC-009.md)
**Visual guide:** [`GRAB 0.9 guide`](features/grab_09_features.html)
**Source:** Blueprint v1.0 Section 4 (`ADR-003`, `ADR-013`), Section 12, Section 13 (`ADR-016`); Blueprint v1.1 roadmap table

Implemented scope:

- Profile-driven two-tier execution (`--engine=auto|ast|vm`) lowering hot functions (`16` calls), hot loops (`64` iterations triggering enclosing compilation on next entry), and hot `PreparedApp` routes (`8` requests) into `internal/ir` and `internal/vm` bytecode while keeping cold startup code on the AST interpreter (`ADR-026a`, `ADR-026b`).
- Strict semantic equivalence (`ADR-026c`): verified differential parity (`tests/grab09_vm_equivalence_test.go`) between `--engine=ast`, `--engine=vm`, and `--engine=auto` across exact base-10 decimals and inline/promoted `big.Int` integers, `yes`/`no` Booleans, `nothing`, `try`/`catch` error codes/spans, `do together` ordering, SQLite transaction rollbacks, HTTP `PreparedApp` warmups, and capability permissions.
- Safety & determinism guards (`ADR-026d`): fail-closed static bytecode verification (`internal/vm/verify.go`), `1,000,000`-step loop guard (`GRAB-E307`), `256`-frame call depth guard (`GRAB-E306`), pooled slice-bounded VM frames (`sync.Pool`), and `ctx.Done()` preemption.
- Evidence-based benchmark gates (`ADR-016`, `ADR-026e`): measured 2.33×–3.14× speedups and up to 95% fewer allocations on prepared function/loop/`try-catch` workloads, ~2.0× faster compute JSON HTTP routes, zero regression on `BenchmarkPreparedApp` (`text`, `json`, `html`), and ~2–4% cold-start overhead in `auto` mode.

---

## GRAB 1.0 — Stable, production-ready platform & native MCP server (`grab mcp`)

**Status:** Complete (Implemented, verified, and release gates passed — November 13, 2026)
**Approved specification:** [`GRAB-PLATFORM-SPEC-100.md`](../specifications/GRAB-PLATFORM-SPEC-100.md)
**Visual guide:** [`GRAB 1.0 guide`](features/grab_10_features.html)
**Source:** Blueprint v1.0 Sections 9, 10, 11, 12, 13; Blueprint v1.1 roadmap table

Implemented and verified under Google Antigravity (`go build ./...`, Windows `go test -count=1 ./...`, Linux `go1.26.11` full `go test -race -mod=readonly -count=1 ./...`, `govulncheck v1.9.0 ./...` clean on `go1.26.11` and `go1.27.4`, `go mod verify`, static `linux/amd64` build, `--read-only` container `server.asset` check, and the 30-minute 1/2/4-worker sustained fault/load gate in [SCALE-TESTING.md](SCALE-TESTING.md) 100% passed):

- **Native GRAB MCP Server (`grab mcp`, `ADR-027a`):** Built-in `stdio` JSON-RPC 2.0 MCP server with five workspace-confined, capability-bounded tools (`grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, `grab_scaffold`), default-deny read-only mode (`--allow-write` required for writes), and sensitive-path blocking (`GRAB-E401`) for **Groxy AI**, **OpenCode**, **Antigravity**, and **Codex**.
- **Forward-Compatible Asset Cache-Busting (`server.asset` + Smart `Cache-Control`, `ADR-027b`):** In-memory SHA-256 asset hashing at `PreparedApp` startup/reload (compatible with `--read-only` containers), returning `/static/<file>?v=<12-char-sha256>` with `Cache-Control: public, max-age=31536000, immutable` on hash match and safe `no-cache` fallback on mismatch during rolling multi-worker restarts.
- **Core `v1.0.0` Stability & Compatibility Freeze (`ADR-027c`):** Formal SemVer `v1.x` freeze of GRAB syntax, exact numeric rules, error codes (`GRAB-E001`–`GRAB-E403`), `.grab.html` context-aware escaping, all 14 standard library modules, and `--engine=auto` Bytecode VM differential parity.
- **Production Release & Security Gates (`ADR-017`, `ADR-027d`):** Final pre-1.0 security audit, Windows/Linux cross-platform binary checks, `govulncheck ./...`, `go mod verify`, GRAB 0.8 Section 12 sustained fault/load gate (`docs/SCALE-TESTING.md`), documentation completeness, and synchronized `v1.0.0` promotion to both `origin` and `stable`.

### GRAB 1.0.1 — CLI Version Reporting Polish (`v1.0.1`)

**Status:** Complete (Implemented, verified, and released — November 13, 2026)

- Added `grab -v`, `grab --version`, and `grab version` to `cmd/grab/main.go` with `-ldflags` build-time `Version` and `BuildDate` (`MMM DD YYYY HH:MM:SS`) injection, outputting the clean 4-line GRAB / Urbanspress / Grisp CLI banner with exit code `0`, covered by `TestVersion_CLI` and full `go test -count=1 ./...` verification.

---

### GRAB 1.1 — Cryptographic Primitives, Capability Env & Opt-In Script CSP (`v1.1.0`)

**Status:** Complete (Implemented, verified, and released — November 14, 2026)
**Specification:** [`GRAB-PLATFORM-SPEC-110.md`](../specifications/GRAB-PLATFORM-SPEC-110.md)
**Visual guide:** [`GRAB 1.1 guide`](features/grab_11_features.html)

Additive security and runtime capabilities under the `v1.x` compatibility guarantee (`ADR-027c`, `ADR-028a`–`ADR-028c`), implemented identically across AST, Bytecode VM, and `auto` engines:

- **Native `use crypto` module:** Argon2id password hashing and constant-time verification (`crypto.hash_password`, `crypto.verify_password`), bounded `crypto/rand` tokens (`crypto.random_urlsafe`, `crypto.random_hex`), SHA-256 / OAuth2 PKCE `S256` hashing (`crypto.sha256_hex`, `crypto.sha256_base64url`), and AES-256-GCM authenticated encryption at rest (`crypto.encrypt_aead`, `crypto.decrypt_aead`).
- **Capability-gated `use env` module (`--allow-env <VAR[,VAR...]>`):** Default-deny `env.get` / `env.has` failing closed with `GRAB-E401` for ungranted variables, with contextual parser support and child worker env forwarding.
- **Opt-in `--allow-scripts` CSP flag on `grab serve`:** Keeps strict `script-src 'none'` by default and emits `script-src 'self'` (preserving `object-src 'none'` and forbidding `unsafe-eval`/`unsafe-inline` scripts) so local **Grex JS v1.0.0** bundles execute cleanly in GrabLab CMS interactive admin UIs.

---

## Post-1.0 — Future research and downstream products

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 1, 14; Blueprint v1.1 Post-1.0 row

### Native GRAB UI / reactive frontend (**Grex JS**)

Research and design for **Grex JS** (the native reactive GRAB UI runtime, selective
hydration, or browser compiler powering **Grex**), evaluated only after a formal
UI strategy and server/browser security boundary are approved (`ADR-014`, `ADR-017`,
`ADR-018`).

### Post-1.0 Ecosystem Milestones

**Dedicated Multi-Repository Architecture & Build Order:** To keep the core GRAB programming language and Grisp application server repository (`grab.progress` / `grab.stable`) decoupled, lightweight, and clean, each downstream ecosystem layer lives in its own dedicated `.progress` (active development) + `.stable` (verified milestone releases) GitHub repository pair:
- **Step 1 — `grex-js v1.0.0`:** **Complete** ([`urbanspress/grex-js.progress`](https://github.com/urbanspress/grex-js.progress) / [`urbanspress/grex-js.stable`](https://github.com/urbanspress/grex-js.stable)) — Native reactive UI runtime, 20-element zero-wrapper-div catalog, tokens, and preview bridge.
- **Step 2 — `grablab-cms v1.0.0`:** **Complete** ([`urbanspress/grablab-cms.progress`](https://github.com/urbanspress/grablab-cms.progress) / [`urbanspress/grablab-cms.stable`](https://github.com/urbanspress/grablab-cms.stable)) — Flagship Savvy CMS built on a **Zero-Plugin Core + Exclusive Active Theme Architecture**.
- **Step 3 — `grex-builder v1.0.0` (Grex Theme Builder):** **Complete** ([`urbanspress/grex-builder.progress`](https://github.com/urbanspress/grex-builder.progress) / [`urbanspress/grex-builder.stable`](https://github.com/urbanspress/grex-builder.stable)) — Installable `mode: "visual"` GrabLab theme & visual design studio (`GREX-BUILDER-SPEC-100.md` approved & frozen); disables any prior theme and takes full exclusive template control when activated inside GrabLab.
- **Step 4 — `groxy-ai`:** **Next Active Milestone** (`urbanspress/groxy-ai.progress` / `urbanspress/groxy-ai.stable`) — Autonomous design, SEO intelligence & CMS agent tooling combining a curated **SEO Knowledge & Reasoning Playbook** (Semrush, Ahrefs, and Google Search Central best practices) with deterministic on-page DOM/content auditing and `grab mcp` CMS/Builder tools.
- **Step 5 — `gashboard` + `grabtaculous`:** **Planned** (`urbanspress/gashboard.progress` / `urbanspress/gashboard.stable`) — Desktop VPS control panel and 1-click app marketplace (includes 1-click install of GrabLab + Grex Visual Theme).

Each downstream ecosystem repository requires separate owner approval before implementation:

- **Grisp (Snappy Multi-Worker Application Server):** The persistent
  multi-worker GRAB web application server runtime that Nginx or Caddy passes
  the "rock" to on private loopback ports (`127.0.0.1:8765–8772`), eliminating
  cold starts with compiled `PreparedApp` memory generations, warm database/Redis
  pools, and `/_ready`-gated rolling worker reloads.
- **GrabLab (Savvy Flagship CMS — Step 2 Complete in `urbanspress/grablab-cms.progress` / `urbanspress/grablab-cms.stable`):** Built *on top of* the stable GRAB 1.0
  platform (`ADR-010`), featuring a **Zero-Plugin Core + Exclusive Active Theme** architecture,
  dynamic WordPress-style pretty permalinks (`/:slug`, custom permalink patterns, and automatic 301 redirect history),
  content types, custom fields, pages/posts, media management, roles/permissions,
  native hooks/filters, query loops, and exclusive active theme ownership (coded `.grab`/`.grab.html` themes or the Grex Visual Theme).
- **Grex Theme Builder & Grex JS (Responsive-First Visual Page Builder & Reactive UI Engine — Step 1 & Step 3 Complete in `urbanspress/grex-builder.progress` / `urbanspress/grex-builder.stable`):**
  A visual page and theme builder (**Grex Theme Builder**) powered by its completed reactive frontend engine
  (**Grex JS v1.0.0**, `grex-js`), installing as a first-class **Visual Theme** inside GrabLab (disabling any prior theme and taking 100% exclusive template rendering control when activated) and outperforming Bricks, Elementor, and Divi with clean semantic markup and zero wrapper-div bloat:
  1. **Native Responsive-First Element Library:** Clean, semantic drag-and-drop
     elements (Layout Containers/Grid/Flex, Typography, Media, Buttons, Forms,
     Navigation, Tabs/Accordions, Dynamic Query Loops) editable through visual
     settings panels with zero wrapper-div bloat.
  2. **Global Style & Class Manager:** Centralized design tokens (colors,
     typography scales, spacing, shadows) and reusable Bricks-style classes.
  3. **Visual Responsive Breakpoints:** Desktop, Tablet, Mobile, and custom
     breakpoint toggles with per-device style overrides.
  4. **Template Library & Conditional Theme Builder:** Reusable section/page
     templates plus conditional global parts (Headers, Footers, Single
     Templates, Archives, 404s) with display rules (e.g., by route, content
     type, or user state).
  5. **Visual Dynamic Data Picker:** Point-and-click binding from element
     settings directly to GRAB 0.7 database records, GRAB 0.5 APIs, and GRAB
     0.8 cached data with automatic context-aware escaping.
  6. **Live Multi-Device Preview & Draft/Publish Workflow:** Real-time preview
     before publishing, revision history, and one-click rollback.
  7. **AI Designer Guardrails (`GRAB-DESIGN-SYSTEM.md` + `grab mcp`):** Curated
     spacing, typography, and accessibility rules paired with `grab mcp` tools
     so **Groxy AI** composes world-class, validated pages out of native elements.
- **Groxy AI (Autonomous Design, SEO Intelligence & CMS Expert Agent):** Powered by `grab mcp`,
  `GRAB-DESIGN-SYSTEM.md`, a curated **SEO Knowledge & Reasoning Playbook** (modeled on proven Semrush, Ahrefs, and Google Search Central best practices: search intent, semantic entity coverage, CTR meta optimization, `schema.org` JSON-LD, internal linking graphs, and WCAG AA image `alt` rules), and cost-free local models (via Ollama + OpenCode). Combines deterministic on-page DOM/content auditing with `grab mcp` CMS & Grex Theme Builder tools (`query_entries`, `create_entry_draft`, `update_entry_layout`, `list_media`, `list_menus`) to power **GrabLab CMS `v1.1.0`'s Pre-Publish Grade Review Gate** (`A+`–`F` / `0–100` across On-Page SEO, Content & WCAG Accessibility, Performance & Clean DOM, and Off-Page / Internal Linking Readiness) and **🚀 "Let Groxy Boost This"** side-by-side diff + **"Approve & Make This Live"** review workflow with zero recurring API fees.
- **Gashboard & Grabtaculous (Discreet Desktop VPS Control Panel & 1-Click App Marketplace):**
  - **Gashboard (Desktop-Based VPS Control Panel):** Eliminates public
    web-panel login attack surfaces entirely by running as a local desktop
    control panel that manages remote VPS Docker containers, databases
    (PostgreSQL, MariaDB, SQLite), Redis instances, and automatic edge SSL
    (via Caddy or Nginx + Certbot) over encrypted connections—keeping **Grisp**
    unprivileged on private worker ports (`127.0.0.1:8765–8772`).
  - **Grabtaculous (1-Click App Marketplace):** Built-in Softaculous-style
    one-click app marketplace inside **Gashboard** offering curated container
    templates to deploy **GrabLab + Grex** alongside popular self-hosted
    companion apps (such as analytics, automation, object storage, and webmail)
    in seconds.
