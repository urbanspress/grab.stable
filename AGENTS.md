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
GRAB 0.1 and **GRAB 0.2 (Localhost browser preview)** are complete and verified
by `go test ./...`. Milestones GRAB 0.3 through 1.0 in `docs/ROADMAP.md` are
marked `Status: Planned (Not Yet Approved for Implementation)` and await
project-owner approval before work begins.

Do not implement the production server, database, VM, UI framework, or CMS yet.

## Live GRAB 0.1 Status & Repository Map

Status below is based on the source, tests, and documentation currently in this repository. The full Go test suite and build were run successfully on November 11, 2026. A passing package test means the tests present for that package passed; packages without test files have not been directly tested.

### GRAB 0.1 components and language features

| Component or feature | Current status |
| --- | --- |
| Language specification | The approved 0.1 specification is in `specifications/GRAB-LANGUAGE-SPEC-001.md`. It defines the intended behavior; approval is not itself proof of implementation. |
| Source reader and lexer | Implemented and tested: UTF-8 validation, LF/CRLF positions, comments and whitespace, ASCII identifiers and keywords, text and byte escapes, numeric and byte literal tokens, punctuation, and source-located lexical errors. Bug fixed: `nextRuneIsDigit` now correctly peeks one character ahead after a consumed `-`, fixing single-digit and all negative decimal/float literals. Conformance fixes reject raw carriage returns inside text and locate unterminated text at its opening quote. Lexer and end-to-end tests pass. |
| Parser | Implemented and tested: `set` and `show`, identifiers and literals, left-associative `joined with`, single-line lists/maps/records, optional trailing commas, duplicate map-key/record-field errors, and source spans. Parser tests pass. |
| AST | Implemented: program, statement, literal, identifier, joined-expression, list, map, and record nodes. Parser tests exercise AST creation; the AST package has no separate test files. |
| Runtime values and display | Implemented and tested: text, Boolean, `nothing`, arbitrary-precision integer, exact decimal, binary64 float, bytes, lists, ordered maps, and ordered records, with canonical display. Runtime tests pass. |
| Interpreter | Implemented and tested: ordered `set`/rebind and `show`, pre-execution name resolution, left-to-right evaluation, text-only joining, deterministic output with LF, and source-located name/type errors. Interpreter tests pass. |
| CLI | Implemented and tested: `grab run <file.grab>` and `grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]`. `cmd/grab/main_test.go` provides 42 table-driven CLI tests covering `run` and `serve` valid usage, flags, loopback enforcement, preflight diagnostics, and runtime errors. All pass. |
| Tests and conformance | GRAB 0.1 and GRAB 0.2 specification conformance milestones complete. `go test ./...` passes 100% across all packages (`cmd/grab`, `internal/lexer`, `internal/parser`, `internal/runtime`, `internal/interpreter`, `internal/template`, `internal/server`, and `tests`). |
| Example programs | Five runnable `.grab` programs (`hello.grab`, `values.grab`, `product.grab`, `greeting.grab`, `collections.grab`) plus two companion `.grab.html` templates (`hello.grab.html`, `product.grab.html`) in `examples/`. All verified with `grab run` and `grab serve`. |
| GRAB 0.2 templates and localhost server | Implemented and tested in `internal/template` and `internal/server` per `specifications/GRAB-LOCALHOST-SPEC-002.md`: `.grab.html` `{<identifier>}` interpolation (e.g. `{title}`), `{{`/`}}` literal braces, companion and reusable template lookup, value binding, context-aware HTML escaping, source-located diagnostics, and loopback-only `grab serve` with security headers and graceful shutdown. |

### Not implemented

- GRAB 0.1/0.2 does not include numeric operators, equality, or implicit coercions; conditions, loops, functions, user-defined types, modules/imports, exceptions, or concurrency; or collection lookup and mutation.
- Database connections and vendor-specific values, production multi-route server/browser/UI/CMS features, IR, bytecode, and a VM remain unimplemented and planned for later milestones (`docs/ROADMAP.md`, GRAB 0.3–1.0).
- The approved 0.1 and 0.2 conformance milestones are complete; this is rule-based test coverage, not a proof over every possible program. Float overflow/non-finite and underflow policy are not explicitly specified; current behavior and this boundary are documented in `tests/README.md`.

### Repository directory and package map

| Path | Purpose |
| --- | --- |
| `cmd/grab/` | `main` package for `grab run` and `grab serve`; `main_test.go` contains 42 table-driven CLI tests. |
| `docs/` | Project documentation, including `docs/ROADMAP.md` (GRAB 0.1 through 1.0 + Post-1.0). |
| `docs/adr/` | Architecture decision register (`ADR-DECISION-REGISTER.md`) and recorded design boundaries. |
| `docs/blueprints/` | GRAB architecture blueprint PDFs; v1.0 is the approved baseline and v1.1 is not approved wholesale. |
| `examples/` | Five runnable `.grab` programs and two `.grab.html` templates (`hello.grab.html`, `product.grab.html`). |
| `internal/ast/` | Abstract syntax tree node types used by the parser and interpreter. |
| `internal/interpreter/` | Name resolution, expression evaluation, statement execution, environment snapshots, and output. |
| `internal/lexer/` | Source reader, positions/spans, tokens, lexical analysis, and lexical diagnostics. |
| `internal/parser/` | Parser for GRAB 0.1 tokens and construction of AST nodes. |
| `internal/runtime/` | Portable runtime values and their canonical display forms. |
| `internal/server/` | Loopback-only `grab serve` development server, path confinement, preflight validation, and HTTP security headers (`server_test.go`). |
| `internal/template/` | `.grab.html` `{<identifier>}` parser, HTML lexical context analyzer, context-aware HTML/URL escaper, and renderer (`template_test.go`). |
| `specifications/` | `GRAB-LANGUAGE-SPEC-001.md` (approved 0.1 spec) and `GRAB-LOCALHOST-SPEC-002.md` (approved 0.2 localhost/template spec). |
| `tests/` | `conformance_test.go` (0.1 conformance), `localhost_conformance_test.go` (0.2 conformance), `structure_test.go` (AST spans), and `README.md`. |
| Repository root | `go.mod` declares module `grab` and Go 1.22.2; `index.grab` is a runnable sample; `.gitignore` lists local build artifacts and editor files. |

### Build, run, and test commands

Run these commands from the repository root with Go 1.22.2 or newer available on `PATH`:

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
