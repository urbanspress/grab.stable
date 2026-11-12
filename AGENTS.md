# GRAB Engineering Instructions

GRAB is a Go-implemented, general-purpose
programming language and application platform.

## Architecture
- Follow approved GRAB blueprints and ADRs.
- Build the AST interpreter before a bytecode VM.
- Preserve simple, deterministic language semantics.
- Keep runtime, server, data, and UI modular.
- Treat future features as unimplemented until verified.

## Workflow
- Inspect existing code before editing.
- Make small, reviewable changes.
- Use idiomatic Go and table-driven tests.
- Run the full Go test suite (`go test ./...`) after implementation.
- Update the `## Live GRAB 0.1 Status & Repository Map` section whenever you add a new feature, package, or command.
- Whenever a new milestone (such as GRAB 0.4, 0.5, etc.) is designed or completed, generate or update a visual `docs/features/grab_<version>_features.html` guide (and update `docs/features/index.html`) explaining the new features, developer benefits, visitor/user benefits, and visual examples in plain English.
- Do not overwrite uncommitted work.
- Do not modify production infrastructure.
- Do not commit, push, or deploy without approval.

## User Communication
- The project owner is a non-coder directing development in plain English. Always explain changes, errors, and design choices simply.
- Handle all file edits and terminal commands directly; do not ask the project owner to perform routine coding or command-line work.

## Mandatory Test Completion Gate
- NEVER mark a task complete or ask the user for approval until you have written automated tests covering the new language feature or bug fix AND executed the full test suite with 100% passing results.

## Tandem Handoff Protocol
- **Google Antigravity -> Codex:** If Google Antigravity fails to fix a bug or pass `go test ./...` after 2 attempts, or encounters complex spec/architecture work, it must stop editing and revert only the specific files it edited during that failed attempt. If any pre-existing uncommitted work would be affected, it must stop and ask the project owner for approval before reverting. It must log what it tried in `HANDOFF.md` and say: "Ready to hand off to Codex. Type `use-codex` to continue."
- **Codex -> Google Antigravity:** Whenever Codex finishes an architectural task or core test suite, it must update `AGENTS.md`, write the next routine follow-up task into `HANDOFF.md`, and say: "Core task complete—ready to hand off to Google Antigravity. Type `use-antigravity` to continue."
- **Automatic Pickup:** Whenever either agent starts via `use-antigravity` or `use-codex`, it must immediately read `AGENTS.md` and `HANDOFF.md`, run `go test ./...` to confirm a clean baseline, and begin the next pending task without asking unnecessary setup questions.
- **Owner Update:** Whenever handing off work, the outgoing agent must explicitly update the `Current Owner` line in `HANDOFF.md` to the incoming agent (`Google Antigravity` or `OpenAI Codex`) before ending its turn.

### Internal Antigravity Model Guidance
- Before ending a turn or starting a new phase, always advise the project owner which Antigravity model and effort level to select for the next step so AI Pro quota is used efficiently:
  - Recommend **Gemini 3.8 Flash (High)** for routine tasks (sample `.grab` / `.grab.html` files, straightforward table-driven tests, documentation updates, and simple fixes).
  - Recommend **Gemini 4 Argon (Medium)** for specification drafting, roadmap updates, and moderate feature planning.
  - Recommend **Opus 5.5 (High)** or **Gemini 4 Argon (High)** for core Go server/compiler implementation, security-critical HTML escaping, and complex bug fixes.
  - If an issue remains unresolved after 2 attempts on **Opus 5.5 (High)** or **Gemini 4 Argon (High)**, trigger the **Tandem Handoff Protocol** and hand off to **OpenAI Codex**.

## Current Priority
GRAB 0.1, **GRAB 0.2 (Localhost browser preview)**, **GRAB 0.3 (Control
flow, functions, and richer language)**, **GRAB 0.4 (Core standard library,
error model, and permissions)**, and **GRAB 0.5 (HTTP client, JSON, and async
task groups)** are complete and verified by `go test ./...` (commit `927564f` on `origin/main`).
**GRAB 0.6 (Persistent application server / GRAB Server)** is implemented,
following the owner's explicit Codex takeover authorization of
`specifications/GRAB-SERVER-SPEC-006.md` on November 11, 2026.
GRAB 0.6 is checkpointed and pushed to `origin/main` as `c4e9856` after a
100% passing full test suite. **GRAB 0.7 — Database access (GRAB Data)** is
complete and verified by `go test ./...` under the owner's explicit approval of
specification 007 and its design on November 11, 2026. The owner approved Go
1.26 minimum, dependency network access, and disposable MariaDB/PostgreSQL
containers, and handed ownership back to **Google Antigravity** in `HANDOFF.md`
for final wrap-up and checkpointing. See `docs/DATA.md` and
`docs/DATA-TESTING.md` for supported boundaries and verification evidence.

**GRAB 0.8 — Integrations, client adapters, and server scaling** specification
and design were explicitly approved by the owner on November 12, 2026.
**Implementation is in progress, not release-complete.** The first slice adds
strict platform/cache configuration, independent cache grants, secret lookup,
memory/standalone Redis adapters, exact-value codec, atomic fixed-window limits,
and generation lifecycle/reload integration. See `docs/CACHE.md` for boundaries.
Typed clients, connectors, durable events, channels, worker supervision, telemetry
and the remaining Section 12 gates are pending. Follow the approved
`specifications/GRAB-SCALE-SPEC-008.md` and `ADR-025a`–`ADR-025h`.
**Current Owner stays OpenAI Codex**, by the owner's explicit instruction;
do not transfer ownership during this implementation.

GRAB 0.9–1.0 and Post-1.0 remain
`Planned (Not Yet Approved for Implementation)`; VM, native GRAB UI/browser
compiler, and CMS are not authorized for implementation.

## Live GRAB 0.1 Status & Repository Map

Status below is based on the source, tests, and documentation currently in this repository. The full Go test suite and build passed on Windows with Go 1.27.4 on November 12, 2026, including the new cache slice with real Redis enabled. Linux minimum-Go 1.26 full race tests also passed. Optional remote database tests were not enabled in these runs; full 0.8 acceptance remains pending. A passing package test means the tests present passed; packages without test files have not been directly tested.

### GRAB 0.1–0.7 components and initial GRAB 0.8 implementation

| Component or feature | Current status |
| --- | --- |
| GRAB 0.8 platform/cache slice (in progress) | `internal/platform` strict private version-1 configuration, independent cache grants and environment secret lookup; `internal/cache` generation-owned memory/standalone Redis stores, bounded tagged exact-value codec, TTLs, quotas, separate atomic fixed-window counters and no write retries; `use cache`, `--platform-config`, `--allow-cache`, offline checking, readiness and last-known-good platform reload. New tests include `tests/grab08_conformance_test.go` (cache slice only) and real Redis vectors; example `examples/cache_app.grab`. Other nonempty subsystem declarations/grants fail closed. This is not full GRAB 0.8 acceptance; see `docs/CACHE.md`. |
| Language specification | The approved 0.1, 0.2, 0.3, 0.4, and 0.5 specifications are in `specifications/GRAB-LANGUAGE-SPEC-001.md`, `specifications/GRAB-LOCALHOST-SPEC-002.md`, `specifications/GRAB-LANGUAGE-SPEC-003.md`, `specifications/GRAB-STDLIB-SPEC-004.md`, and `specifications/GRAB-CONNECT-SPEC-005.md`. |
| Source reader and lexer | Implemented and tested: UTF-8 validation, LF/CRLF positions, comments and whitespace, ASCII identifiers and GRAB 0.1/0.3/0.4/0.5 keywords (`use`, `try`, `catch`, `raise`, `do`, `together`, `wait`), text and byte escapes, numeric and byte literal tokens, punctuation (`[ ] { } , : ( ) .`), and source-located lexical errors (`GRAB-E001`–`GRAB-E007`). Bug fixed: `nextRuneIsDigit` now correctly peeks one character ahead after a consumed `-`, fixing single-digit and all negative decimal/float literals. Conformance fixes reject raw carriage returns inside text and locate unterminated text at its opening quote. Lexer and end-to-end tests pass. |
| Parser and formatter | Implemented and tested: top-level `use <module>`, `set`, `show`, `if`/`otherwise if`/`otherwise`/`end`, `repeat <count> times`, `for each <item> in <coll>`, `while <cond>`, `try`/`catch`/`end`, `raise`, `do together`/`wait for`/`end`, `make`/`using`/`give back`/`call` (including module-qualified `<module>.<function>`), parenthesized grouping, full GRAB 0.3 operator precedence (`not`, `times`/`divided by`/`modulo`, `plus`/`minus`, `joined with`, comparisons, `is`/`is not`, `and`, `or`), collection lookup (`.field`, `at`, `has`), single-line lists/maps/records, optional trailing commas, duplicate map-key/record-field errors, source spans, syntax error codes (`GRAB-E101`–`GRAB-E103`), and canonical source formatter (`parser.Format`). Parser tests pass. |
| AST | Implemented: program, `UseDecl`, statements (`set`, `show`, `if`, `repeat`, `for each`, `while`, `try`/`catch`, `raise`, `do together`/`TaskStatement`, `make`, `give back`, `call`), literals, identifiers, parenthesized/unary/binary/joined expressions, `.field`/`at`/`has` lookup nodes, function and module-qualified call expressions, and list/map/record nodes. |
| Runtime values and display | Implemented and tested: text, Boolean, `nothing`, arbitrary-precision integer, exact decimal, binary64 float, bytes, lists, ordered maps, and ordered records, with canonical display, exact integer/decimal arithmetic (`Add`, `Sub`, `Mul`, `Div`, `Mod`), deep equality (`Equal`), and ordered comparison (`Compare`). Runtime tests pass. |
| Interpreter, standard library, concurrency, and permissions | Implemented and tested: ordered `set`/rebind and `show`, pre-execution validation (module imports, name resolution, exhaustive `if` and `try`/`catch` branch bindings, `do together` task group validation `GRAB-E208`, function registration, and arity checking), control flow (`if`, `repeat`, `for each`, `while` with `1,000,000`-step guard), functions (`make`/`using`/`give back`/`call` with isolated call frames and `256`-frame recursion limit), structured concurrency (`do together`/`wait for` with snapshot isolation, deterministic top-to-bottom output/binding commit order, and sibling cancellation), structured error handling (`try`/`catch`/`raise`, error records, non-catchable fatal safety limits `GRAB-E306`/`GRAB-E307`, and `beginner`/`developer`/`production` diagnostic modes), 10 built-in standard library modules (`text`, `math`, `collections`, `dates`, `files`, `json`, `http`, `server`, `data`, and the initial 0.8 `cache` module), unified `.field`/`at`/`has` lookup across records and maps, default-deny filesystem permissions (`--allow-read`/`--allow-write` with sensitive-path blocklist `GRAB-E401`), default-deny network permissions (`--allow-net` with SSRF blocking `GRAB-E402`), deterministic output with LF, and Go benchmark/allocation suite (`benchmark_test.go`). Interpreter tests pass. |
| CLI | Implemented and tested: `grab run`, `grab migrate`, `grab serve`, `grab check`, and `grab format [--write]`; database manifest and separate read/read-write/migrate `--allow-db` grants alongside filesystem/network permissions and diagnostics. Initial 0.8 `--platform-config` and separate read/read-write/limit `--allow-cache` work in run/serve/check; other platform grants fail closed. Check/format perform no database/cache network I/O. |
| Tests and conformance | GRAB 0.1–0.7 automated suites cover language, templates, HTTP/server, databases, CLI, permissions, and lifecycle. Real-service adapter tests and race verification are documented in `docs/DATA-TESTING.md`; offline runs may skip the remote services. |
| Example programs | Existing 0.1–0.6 examples plus `catalog_migrate.grab`, `catalog_seed.grab`, `catalog_app.grab`, two catalog templates, and `grab.data.json`. Run instructions are in `examples/DATA.md`; HTTP tests exercise the catalog. |
| GRAB 0.2 templates and localhost server | Implemented and tested in `internal/template` and `internal/server` per `specifications/GRAB-LOCALHOST-SPEC-002.md`: `.grab.html` `{<identifier>}` interpolation (e.g. `{title}`), `{{`/`}}` literal braces, companion and reusable template lookup, value binding, context-aware HTML escaping, source-located diagnostics, and loopback-only `grab serve` with security headers and graceful shutdown. |
| GRAB 0.6 persistent application server | Implemented and tested: `route` lexing/AST/parsing/formatting, conflict checks (`GRAB-E209`); prepared code and literal templates loaded once; immutable startup baseline and per-request bindings; `use server` HTML/JSON/text/redirect/custom-response/JSON-body helpers (`GRAB-E312`); request params/query/headers/body/form/proxy metadata; static-first routes, HEAD, 404/405; bounded concurrency/queue/body/deadline controls; public-bind opt-in, trusted proxies, confined static files and ETags; probes, redacted request/console logs, panic containment, atomic reload and graceful shutdown. See `examples/SERVER.md`, `internal/server/application_test.go`, and `tests/grab06_conformance_test.go`. |
| GRAB 0.7 database access | Implemented: `internal/data` adapters and interpreter `use data`, strict literal prepared SQL/type contracts, default-deny database grants/configuration, bounded pools/results/deadlines, explicit scoped transactions, versioned maintenance migrations, and prepared server generation cleanup. Real SQLite/MariaDB/PostgreSQL tests, CLI/HTTP/end-to-end coverage, and runnable catalog. Disk SQLite is confined rollback-journal mode on Windows/amd64 and Linux/amd64; unsupported constructs/platforms fail closed. See `docs/DATA.md` and `docs/DATA-TESTING.md`. |

### Not implemented

- GRAB 0.1–0.7 does not include user-defined types, user-authored file imports, or in-place collection mutation.
- GRAB 0.8 `use api`, `use connect`, `use events`, `use sockets`, `grab generate client`, `grab events`, `--workers`, typed `grab.api.json`, telemetry and complete platform schemas beyond caches remain authorized but unimplemented. GRAB 0.9 IR/bytecode/VM and GRAB 1.0/Post-1.0 remain planned and unapproved (`docs/ROADMAP.md`). Database support boundaries are in `docs/DATA.md`.
- The approved 0.1 through 0.7 conformance milestones are complete; this is rule-based test coverage, not a proof over every possible program.

### Repository directory and package map

| Path | Purpose |
| --- | --- |
| `internal/platform/`, `internal/cache/` | GRAB 0.8 work in progress: strict cache configuration/capabilities/secrets and generation-owned temporary memory/standalone Redis adapters with exact codec and atomic counters. Tests and limits in `docs/CACHE.md`; other 0.8 packages remain pending. |
| `cmd/grab/` | CLI run, migrate, serve, check, format; table-driven CLI/database capability tests. |
| `docs/` | Project documentation, including `docs/ROADMAP.md` (GRAB 0.1 through 1.0 + Post-1.0). |
| `docs/features/` | Self-contained HTML milestone feature guides (`index.html` hub, completed guides `grab_04_features.html`–`grab_07_features.html`, and approved-design/in-progress guide `grab_08_features.html`) explaining features, developer benefits, visitor/user benefits, and visual examples in plain English. |
| `docs/adr/` | Architecture decision register (`ADR-DECISION-REGISTER.md`) and recorded design boundaries. |
| `docs/blueprints/` | GRAB architecture blueprint PDFs; v1.0 is the approved baseline and v1.1 is not approved wholesale. |
| `examples/` | Runnable `.grab` programs and `.grab.html` templates, including SQLite catalog migration/seed/server examples plus `cache_app.grab` and `grab.platform.json`. See `DATA.md`, `SERVER.md` and `CACHE.md`. |
| `internal/ast/` | Abstract syntax tree node types used by the parser, formatter, and interpreter. |
| `internal/interpreter/` | Validation and AST execution, 10 standard modules including `data.go` and the initial 0.8 `cache.go`, scoped database resources/transactions, generation-owned cache resources, capabilities, diagnostics, concurrency, server helpers, and benchmarks. |
| `internal/data/` | Host-owned database/sql adapters, strict SQL/contracts/configuration, engine authorization, confined rollback-journal SQLite VFS, remote TLS/DNS pinning, migrations, transaction/pool lifecycles, tests, and benchmarks. |
| `internal/lexer/` | Source reader, positions/spans, tokens, lexical analysis, and lexical diagnostics (`GRAB-E001`–`GRAB-E007`). |
| `internal/parser/` | Parser for GRAB 0.1–0.6 tokens, AST construction, and canonical code formatter (`format.go`). |
| `internal/runtime/` | Portable runtime values, arithmetic/comparison operations, and canonical display forms. |
| `internal/server/` | Persistent server: `prepared.go` (load-once code/templates), `application.go` (routing, limits, proxies, static files, logging, atomic reload), `supervisor.go` (listeners, signals, drain), platform signal files, table-driven tests, and `benchmark_test.go`. Legacy single-page mode is preserved. |
| `internal/template/` | `.grab.html` `{<identifier>}` parser, HTML lexical context analyzer, context-aware HTML/URL escaper, and renderer (`template_test.go`). |
| `specifications/` | Approved/implemented specifications 001–007, plus owner-approved GRAB 0.8 specification `GRAB-SCALE-SPEC-008.md` (implementation in progress; release acceptance pending). Later milestones remain unapproved. |
| `tests/` | `conformance_test.go` (0.1 conformance), `localhost_conformance_test.go` (0.2 conformance), `grab03_conformance_test.go` (0.3 conformance), `grab04_conformance_test.go` (0.4 conformance), `grab05_conformance_test.go` (0.5 conformance), `grab06_conformance_test.go` (0.6 HTTP conformance), `grab07_conformance_test.go` (0.7 database conformance), `structure_test.go` (AST spans), and `README.md`. |
| Repository root | `go.mod` declares module `grab`, Go 1.26 minimum, and pinned pure-Go SQLite/MariaDB/PostgreSQL dependencies; `go.sum` locks downloads. `index.grab` is runnable; `.gitignore` excludes local catalog files/build artifacts. |

### Build, run, and test commands

Run these commands from the repository root with Go 1.26 or newer available on `PATH`:

```sh
# Build all Go packages, including the CLI.
go build ./...

# Run the checked-in sample using the CLI.
go run ./cmd/grab run index.grab

# Start the localhost browser preview server on http://127.0.0.1:8080/
go run ./cmd/grab serve examples/hello.grab

# Run the full Go test suite.
go test ./...
```
