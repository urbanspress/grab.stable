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

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 9–10; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- Core standard library modules: text, math, file I/O, dates/times, and
  collections.
- Structured error model (`try` / `catch` / `raise`) with stable internal error
  codes, source locations, stack traces, and beginner/developer/production
  diagnostic modes.
- Capability-based filesystem permissions with default-deny for sensitive paths.
- `grab check` and `grab format` CLI subcommands.

---

## GRAB 0.5 — HTTP client, JSON, and async task groups

**Status:** Planned (Not Yet Approved for Implementation)
**Source:** Blueprint v1.0 Sections 5, 8; Blueprint v1.1 roadmap table

Intended scope (requires owner approval before implementation):

- Universal HTTP client (GRAB Connect foundation): `GET`, `POST`, `PUT`,
  `PATCH`, `DELETE`, JSON encoding/decoding, custom headers, TLS verification,
  bounded retries, and timeouts.
- Explicit cancellation and timeout contracts at the language level.
- Structured concurrency prototype: explicit task groups (`do together` /
  `wait for`) with deterministic result ordering and error propagation
  (ADR-007, ADR-015).

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
