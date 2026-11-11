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

## Current Priority
GRAB 0.1: Formal language specification,
lexer, parser, AST, interpreter, and CLI.

Do not implement the VM, server, UI, or CMS yet.

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
| CLI | Implemented and tested: `grab run <file.grab>` reads, parses, and runs a source file, reporting errors and exit status. `cmd/grab/main_test.go` provides 33 table-driven CLI tests covering valid programs, missing files, usage errors, syntax errors, name-resolution errors, and runtime errors. All pass. |
| Tests and conformance | GRAB 0.1 specification conformance milestone complete. `go test ./...` passes for all packages with test files, including `tests/`. The table-driven suite runs each source through the real CLI and parser/interpreter, checks exact output and diagnostic categories/spans, and covers all 256 byte values. Fixed AST span and left-association checks supplement executable cases. `tests/README.md` maps coverage to every specification section and records limits and unspecified behavior. |
| Example programs | Five runnable `.grab` programs in `examples/`: `hello.grab` (vertical slice), `values.grab` (all value kinds), `product.grab` (spec conformance case), `greeting.grab` (joined-with chaining, rebinding), `collections.grab` (lists, maps, records, nesting). All verified with `grab run`. |

### Not implemented

- GRAB 0.1 does not include numeric operators, equality, or implicit coercions; conditions, loops, functions, user-defined types, modules/imports, exceptions, or concurrency; or collection lookup and mutation.
- Database connections and vendor-specific values, HTML templates, server/browser/UI/CMS features, IR, bytecode, and a VM are outside 0.1 and remain unimplemented. A separate future localhost/template milestone is recorded in `docs/ROADMAP.md`; it is not part of 0.1.
- The approved 0.1 conformance milestone is complete; this is rule-based test coverage, not a proof over every possible program. Float overflow/non-finite and underflow policy are not explicitly specified; current behavior and this boundary are documented in `tests/README.md`.

### Repository directory and package map

| Path | Purpose |
| --- | --- |
| `cmd/grab/` | `main` package for the `grab run <file.grab>` command; `main_test.go` contains 33 table-driven CLI tests. |
| `docs/` | Project documentation, including the roadmap. |
| `docs/adr/` | Architecture decision register and recorded design boundaries. |
| `docs/blueprints/` | GRAB architecture blueprint PDFs; v1.0 is the approved baseline and v1.1 is not approved wholesale. |
| `examples/` | Five runnable GRAB 0.1 sample programs: `hello.grab`, `values.grab`, `product.grab`, `greeting.grab`, `collections.grab`. |
| `internal/ast/` | Abstract syntax tree node types used by the parser and interpreter. |
| `internal/interpreter/` | Name resolution, expression evaluation, statement execution, and output. |
| `internal/lexer/` | Source reader, positions/spans, tokens, lexical analysis, and lexical diagnostics. |
| `internal/parser/` | Parser for GRAB 0.1 tokens and construction of AST nodes. |
| `internal/runtime/` | Portable runtime values and their canonical display forms. |
| `specifications/` | Approved language specification and conformance examples. |
| `tests/` | `conformance_test.go`: table-driven CLI and pipeline conformance; `structure_test.go`: exact AST spans and left association; `README.md`: specification coverage and limits. |
| Repository root | `go.mod` declares module `grab` and Go 1.22.2; `index.grab` is a runnable sample; `.gitignore` lists local build artifacts and editor files. |

### Build, run, and test commands

Run these commands from the repository root with Go 1.22.2 or newer available on `PATH`:

```sh
# Build all Go packages, including the CLI.
go build ./...

# Run the checked-in sample using the CLI.
go run ./cmd/grab run index.grab

# Run the full Go test suite.
go test ./...
```
