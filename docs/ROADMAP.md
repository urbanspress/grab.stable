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

## GRAB 0.6 — Persistent application server (GRAB Server)

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Section 6; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- Persistent multi-route `grab serve` runtime that loads and validates programs
  once per application load (`ADR-011`, `ADR-012`).
- Strict per-request execution isolation, configurable request/body/time limits,
  and backpressure controls.
- Application supervisor with health checks, structured request logging, and
  graceful reload/shutdown.
- Standalone mode and private loopback reverse-proxy mode behind Nginx
  (`ADR-005`).

---

## GRAB 0.7 — Database access (GRAB Data)

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Section 7; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- SQLite driver first for development and automated testing, followed by
  MariaDB and PostgreSQL support (`ADR-006`).
- Connection pooling, mandatory prepared/parameterized statements, and explicit
  lossless type adapters.
- Transactions, schema migrations, structured result mapping, and durable
  database-backed background jobs.

---

## GRAB 0.8 — Connector SDK, webhooks, WebSockets, and server scaling

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 6, 8, 9; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- Connector SDK for external API providers, plus a secure webhook engine with
  signature verification, deduplication, and durable inbox/outbox retries.
- Optional parallel track for typed API contracts and React/TypeScript client
  adapters (`ADR-014`).
- Advanced server capabilities: WebSockets, multi-process worker scaling, rate
  limiting, metrics/tracing observability, and fault/load testing.

---

## GRAB 0.9 — Profile-driven IR and bytecode VM

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Section 4 (`ADR-003`, `ADR-013`); Blueprint v1.1
roadmap table

Intended scope (requires owner approval before implementation):

- Profile-driven optional intermediate representation (IR) and bytecode
  compiler/virtual machine (`ADR-013`).
- Semantic equivalence gate: the bytecode VM must pass 100% of the interpreter
  conformance suite with identical outputs, side-effect ordering, and
  diagnostics.
- Repeatable performance regression benchmarks comparing AST interpreter vs.
  bytecode VM execution (`ADR-016`).

---

## GRAB 1.0 — Stable, production-ready platform

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 10, 12, 13; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- Frozen GRAB 1.0 language specification with formal compatibility and
  deprecation policy.
- Full security audit: capability enforcement, secret redaction, request
  isolation, and resource budgets (`ADR-009`, `ADR-017`).
- Complete CLI and package ecosystem (`grab new`, `grab test`, `grab build`,
  `grab deploy`, `grab.json`, `grab.lock`).
- Reproducible full-stack benchmark suite and production deployment
  documentation.

---

## Post-1.0 — Future research and downstream products

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 1, 14; Blueprint v1.1 Post-1.0 row

### Native GRAB UI / browser compiler

Research and design for a native GRAB UI framework, selective hydration, or
browser compiler, evaluated only after a formal UI strategy and server/browser
security boundary are approved (`ADR-014`, `ADR-017`, `ADR-018`).

### GrabPress — flagship CMS

A full Content Management System built as an application *on top of* the stable
GRAB 1.0 platform (`ADR-010`), featuring content types, pages/posts, media
library, roles/permissions, themes/templates, plugins, and publishing workflows.
