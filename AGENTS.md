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

## Two-Repository Workflow & Release Protection
- **Repository Roles:**
  - `origin` (`https://github.com/urbanspress/grab.progress.git`): Active development repository for ongoing work, feature branches, experimental spikes, and in-progress milestone commits.
  - `stable` (`https://github.com/urbanspress/grab.stable.git`): Production release repository powering live VPS auto-deployment and public production releases.
- **Strict `stable` Protection:** All AI agents and models MUST push work-in-progress commits, drafts, and partial milestone checkpoints ONLY to `origin` (`urbanspress/grab.progress`). Pushing to `stable` (`urbanspress/grab.stable`) is strictly forbidden unless the milestone is 100% `Complete` in `docs/ROADMAP.md`, `go build ./...` and `go test -count=1 ./...` pass 100%, and `HANDOFF.md` records no pending milestone tasks.
- **Synchronized Releases:** Whenever a completed milestone is promoted to `stable`, the agent must push `main` and annotated version tags to both `origin` and `stable`, and create the matching GitHub Release on both repositories (`gh release create <tag> --repo urbanspress/grab.progress` and `gh release create <tag> --repo urbanspress/grab.stable`).
- **Release Tag Chronology Rule:** Always create and push annotated release tags strictly in ascending chronological order and never re-tag an older release after newer tags exist (so GitHub's Releases sidebar always stays in exact descending version order).
- **Release Assets & Plain-English Guides:** Every release promotion MUST run `scripts/build-release.ps1 -Version <X.Y.Z>` (which cleanly rebuilds `dist/`, verifies `cmd/grab/resource_windows.syso`, compiles `grab-windows-amd64.exe` and `grab-linux-amd64` with `CGO_ENABLED=0` and UTC `MMM DD YYYY HH:MM:SS` `BuildDate` ldflags, compiles a single flicker-free Inno Setup Windows installer `GRAB-Setup-<X.Y.Z>.exe` from `deploy/windows/grab-setup.iss` via `iscc` — Inno Setup `.exe` only, never `.msi` — and generates `SHA256SUMS.txt`), generate the plain-English e-commerce walkthrough PDF (`docs/releases/pdf/GRAB-v<X.Y.Z>-Guide.pdf` via `docs/releases/generate_release_guides.py`), and attach `GRAB-Setup-<X.Y.Z>.exe`, `grab-windows-amd64.exe`, `grab-linux-amd64`, `SHA256SUMS.txt`, and `GRAB-v<X.Y.Z>-Guide.pdf` to the **Assets** section of both repositories' GitHub Releases (`gh release upload --clobber --repo urbanspress/grab.progress` and `gh release upload --clobber --repo urbanspress/grab.stable`).

### SemVer (MAJOR.MINOR.PATCH) & Release Compatibility Matrix Standard
1. **Strict `MAJOR.MINOR.PATCH` Semantic Versioning (`ADR-027c`):**
   - **`1.0.x` (`PATCH`):** Strictly for non-breaking bug fixes, security hardening, documentation polish, and cross-layer audit patches (`100%` drop-in compatible).
   - **`1.x.0` (`MINOR`):** For backwards-compatible new features, additive standard library/runtime capabilities, or new MCP/CMS/Builder endpoints.
   - **`2.0.0` (`MAJOR`):** Reserved for breaking architectural or contract changes—**never used without explicit project-owner approval (`ADR-027c`)**.
2. **Mandatory Release Compatibility Badge & Matrix Block:**
   - Every GitHub Release (`v1.0.1`, `v1.1.0`, etc.) across both `.progress` and `.stable` repositories MUST display at the top of its release notes:
     1. A **Release Type & Affected Layers** summary callout (`PATCH (v1.0.x)` vs. `MINOR (v1.x.0)`, which layer(s) changed, and whether any neighboring layer requires an upgrade).
     2. An **Ecosystem Compatibility Matrix** Markdown table covering all five layers:

````markdown
### Release Type & Affected Layers
- **Release Version & SemVer Tier:** `v<X.Y.Z>` (`PATCH 1.0.x — Non-Breaking Bug/Security/Audit Patch` | `MINOR 1.x.0 — Backwards-Compatible Feature Release`)
- **Affected Layer(s):** `<Layer Name>` (`urbanspress/<repo>`)
- **Neighboring Layer Upgrade Required?** `No — 100% drop-in compatible` (or explicit version requirement if `Yes`)

| Ecosystem Layer | Repository | Required Version Range | Touched in This Release? | Neighbor Upgrade Required? |
|---|---|---|---|---|
| **Core Language, Bytecode VM & Grisp Server** | `urbanspress/grab` | `>= v1.1.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Reactive Frontend Runtime & 20-Element Library** | `urbanspress/grex-js` | `>= v1.0.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Flagship Zero-Plugin CMS** | `urbanspress/grablab-cms` | `>= v1.0.1 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Exclusive `mode: "visual"` Theme & Studio** | `urbanspress/grex-builder` | `>= v1.0.0 (< v2.0.0)` | `Yes` / `No` | `No` |
| **Autonomous Design, Content & SEO Agent** | `urbanspress/groxy-ai` | `>= v0.1.0` | `Yes` / `No` | `No` |
````

### Mandatory Milestone Snapshot Rule
- **Ecosystem Snapshot Sync:** At the end of every milestone (before closing the milestone or pushing release promotions), every agent MUST run:
  ```powershell
  powershell -ExecutionPolicy Bypass -File C:\Development\GRAB\scripts\sync-notebook-sources.ps1
  ```
  so `C:\Development\ECOSYSTEM-SNAPSHOT.md` always reflects the latest Git branches, commits, tags, `AGENTS.md`, `*-EXPERT.md`, `HANDOFF.md`, `docs\ROADMAP.md`, and `specifications\*.md` across all discovered `C:\Development\*` repositories.

## Tandem Handoff Protocol
- **Google Antigravity -> Codex:** If Google Antigravity fails to fix a bug or pass `go test ./...` after 2 attempts, or encounters complex spec/architecture work, it must stop editing and revert only the specific files it edited during that failed attempt. If any pre-existing uncommitted work would be affected, it must stop and ask the project owner for approval before reverting. It must log what it tried in `HANDOFF.md` and say: "Ready to hand off to Codex. Type `use-codex` to continue."
- **Codex -> Google Antigravity:** Whenever Codex finishes an architectural task or core test suite, it must update `AGENTS.md`, write the next routine follow-up task into `HANDOFF.md`, and say: "Core task complete—ready to hand off to Google Antigravity. Type `use-antigravity` to continue."
- **Automatic Pickup:** Whenever either agent starts via `use-antigravity` or `use-codex`, it must immediately read `AGENTS.md` and `HANDOFF.md`, run `go test ./...` to confirm a clean baseline, and begin the next pending task without asking unnecessary setup questions.
- **Owner Update:** Whenever handing off work, the outgoing agent must explicitly update the `Current Owner` line in `HANDOFF.md` to the incoming agent (`Google Antigravity` or `OpenAI Codex`) before ending its turn.

### Internal Antigravity Model Guidance
- Before ending a turn or starting a new phase, always advise the project owner which Antigravity model and effort level to select for the next step so AI Pro quota is used efficiently:
  - **`Google Hydrogen (Medium)`:** Pre-implementation specifications (`*-SPEC-*.md`), documentation, governance updates, and single-module edits.
  - **`Google Hydrogen (High)`:** Standard feature milestone implementations (`v0.4.0`, `v1.0.0` build & release).
  - **`Orchestrator (Argon)`:** Second-opinion pre-`v1.0.0` security/DOM/contract audits, multi-repo cross-layer integrations, and complex refactors where Argon-level planning + automated delegation saves tokens.
  - **`Google Hydrogen (Extra)`:** Reserved for autonomous AI engines (`Groxy AI`), multi-server orchestration (`Gashboard + Grabtaculous + grabman`), and stubborn cross-layer bugs.
- **Mandatory Pause & Swap Prompt Rule:** Every active AI agent MUST pause at milestone boundaries or before high-risk cross-layer/pre-`v1.0.0` audits and explicitly tell the user when to switch to `Orchestrator (Argon)`, including a clear 1-sentence reason why `Orchestrator (Argon)` is recommended next.
- If an issue remains unresolved after 2 attempts on `Google Hydrogen (Extra)` or `Orchestrator (Argon)`, trigger the **Tandem Handoff Protocol** and hand off to **OpenAI Codex**.

## Tiered Model Routing
- **Tier 1 — Frontier Models (`Google Hydrogen`, `Gemini 4 Argon`, `GPT-6.1 Sol`, `Claude Opus/Sonnet`):**
  - Authorized to execute multi-package architectural changes, compiler/VM features, and cross-cutting refactors using GRAB's standard milestone, ADR, and `go test ./...` verification rules.
- **Tier 2 — Fast Workhorse & Backup Models (`Gemini 3.8 Flash`, `OpenCode`, `Cline`, `Kilo Code`):**
  - Must operate in **Strict Single-Package Mode** for all Go code changes:
    1. **Read `GRAB-EXPERT.md` first** before making any code edit.
    2. **Scope edits to one Go package at a time** instead of multi-package mega-edits.
    3. **Verify every package immediately after editing:** run `go test -race ./<package>` (using `go test -count=1 ./<package>` when on Windows without CGO, plus `-race` inside CGO/Linux Docker when applicable) and differential `--engine=ast` vs. `--engine=vm` checks (`go test ./tests -run TestGRAB09`) whenever touching execution logic before moving to the next package.

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
Parts 1–3 are implemented and verified locally (100% passing automated test
suite across all packages on Windows and Linux, including pure-Go SQLite,
MariaDB, PostgreSQL, Redis integration and race tests). The optional 30-minute
sustained fault/load gate per specification 008 Section 12 is documented in
`docs/SCALE-TESTING.md`. See `docs/REALTIME-SCALING.md`, `docs/API.md`,
`docs/INTEGRATIONS.md`, and `docs/CACHE.md`. Follow `specifications/GRAB-SCALE-SPEC-008.md`
and `ADR-025a`–`ADR-025h`.
**GRAB 0.9 — Profile-driven IR and bytecode VM** is complete and verified by `go test -count=1 ./...` under **Google Antigravity** (following owner approval on November 12, 2026). See `specifications/GRAB-VM-SPEC-009.md` and `ADR-026a`–`ADR-026e`.
**GRAB 1.0 — Stable, production-ready platform & native MCP server (`grab mcp`)** is complete, verified, and released as `v1.0.0` (`go build ./...`, Windows `go test -count=1 ./...` across all 19 testable packages, Linux `go1.26.11` full `go test -race -mod=readonly -count=1 ./...`, `govulncheck v1.9.0 ./...` clean on both `go1.26.11` and `go1.27.4`, `go mod verify`, static `linux/amd64` build, `--read-only` container `server.asset` verification, and the 30-minute 1/2/4-worker sustained fault/load gate in `docs/SCALE-TESTING.md` 100% passing) under **Google Antigravity** (`specifications/GRAB-PLATFORM-SPEC-100.md`, `ADR-027a`–`ADR-027d`, `docs/features/grab_10_features.html`).
**Current Owner:** Google Antigravity.

Post-1.0 ecosystem tracks live in dedicated downstream repositories: **Step 1 (`grex-js v1.0.0`)** is complete in `urbanspress/grex-js`; **Step 2 (`grablab-cms v1.0.1`)** is complete in `urbanspress/grablab-cms` (Zero-Plugin Core + Exclusive Active Theme architecture); **Step 3 (`grex-builder v1.0.0` — Grex Theme Builder)** is complete in `urbanspress/grex-builder` (exclusive `mode: "visual"` GrabLab theme & visual design studio; `GREX-BUILDER-SPEC-100.md` approved & frozen); **Step 4 (`groxy-ai` — Autonomous Design & SEO Agent)** is the **Next Active Milestone** (`urbanspress/groxy-ai.progress` / `urbanspress/groxy-ai.stable`, awaiting spec approval in `specifications/GROXY-AI-SPEC-001.md`); and **Step 5 (`gashboard` + `grabtaculous`)** remains `Planned (Not Yet Approved for Implementation)`. Core GRAB (`v1.1.0`) remains focused exclusively on the language, compiler/VM, and Grisp server (`specifications/GRAB-PLATFORM-SPEC-110.md`, `ADR-028a`–`ADR-028c`, `docs/features/grab_11_features.html`).

## Live GRAB 0.1 Status & Repository Map

Status below reflects source, tests, sustained fault/load benchmarks, and documentation on November 14, 2026.
GRAB 1.0 (`v1.0.0`) release gates and 30-minute sustained load/fault benchmarks are recorded in `docs/SCALE-TESTING.md` with 100% passing results across all platforms.

### GRAB 0.1–0.7 components and GRAB 0.8 implementation

| Component or feature | Current status |
| --- | --- |
| GRAB 0.8 platform and cache | `internal/platform` strict private configuration, independent grants and host secrets; `internal/cache` memory/standalone Redis, bounded exact values, TTLs, quotas and atomic fixed-window counters. See `docs/CACHE.md`. |
| GRAB 0.8 typed APIs and integrations | `internal/contracts` typed JSON validation and deterministic TypeScript/React generation; `internal/connect` integrity-locked connectors and SSRF-safe verified transport; `internal/events` signed ingestion, fenced durable inbox/outbox, atomic business work, retries/reconciliation and explicit maintenance on SQLite/MariaDB/PostgreSQL. See `docs/API.md` and `docs/INTEGRATIONS.md`. |
| GRAB 0.8 channels, workers and telemetry | `internal/realtime`, `use sockets`: explicit authorization/expiry/audiences, Origin and TLS/proxy checks, contract-validated JSON, bounded queues/rates, memory/standalone Redis pub/sub. `internal/server/workers.go`: private consecutive loopback workers, restricted verified spawning, source snapshots, aggregate pools, bounded restarts and rolling drains. `internal/telemetry`: opt-in authenticated OpenMetrics, verified-TLS OTLP/HTTP request and durable-attempt spans, bounded private labels/queues. Operator examples and limits: `docs/REALTIME-SCALING.md`, `examples/REALTIME.md`. Automated test suite passing 100%; sustained 30-minute fault/load gate recorded in `docs/SCALE-TESTING.md`. |
| Language specification | The approved 0.1, 0.2, 0.3, 0.4, and 0.5 specifications are in `specifications/GRAB-LANGUAGE-SPEC-001.md`, `specifications/GRAB-LOCALHOST-SPEC-002.md`, `specifications/GRAB-LANGUAGE-SPEC-003.md`, `specifications/GRAB-STDLIB-SPEC-004.md`, and `specifications/GRAB-CONNECT-SPEC-005.md`. |
| Source reader and lexer | Implemented and tested: UTF-8 validation, LF/CRLF positions, comments and whitespace, ASCII identifiers and GRAB 0.1/0.3/0.4/0.5 keywords (`use`, `try`, `catch`, `raise`, `do`, `together`, `wait`), text and byte escapes, numeric and byte literal tokens, punctuation (`[ ] { } , : ( ) .`), and source-located lexical errors (`GRAB-E001`–`GRAB-E007`). Bug fixed: `nextRuneIsDigit` now correctly peeks one character ahead after a consumed `-`, fixing single-digit and all negative decimal/float literals. Conformance fixes reject raw carriage returns inside text and locate unterminated text at its opening quote. Lexer and end-to-end tests pass. |
| Parser and formatter | Implemented and tested: top-level `use <module>`, `set`, `show`, `if`/`otherwise if`/`otherwise`/`end`, `repeat <count> times`, `for each <item> in <coll>`, `while <cond>`, `try`/`catch`/`end`, `raise`, `do together`/`wait for`/`end`, `make`/`using`/`give back`/`call` (including module-qualified `<module>.<function>`), parenthesized grouping, full GRAB 0.3 operator precedence (`not`, `times`/`divided by`/`modulo`, `plus`/`minus`, `joined with`, comparisons, `is`/`is not`, `and`, `or`), collection lookup (`.field`, `at`, `has`), single-line lists/maps/records, optional trailing commas, duplicate map-key/record-field errors, source spans, syntax error codes (`GRAB-E101`–`GRAB-E103`), and canonical source formatter (`parser.Format`). Parser tests pass. |
| AST | Implemented: program, `UseDecl`, statements (`set`, `show`, `if`, `repeat`, `for each`, `while`, `try`/`catch`, `raise`, `do together`/`TaskStatement`, `make`, `give back`, `call`), literals, identifiers, parenthesized/unary/binary/joined expressions, `.field`/`at`/`has` lookup nodes, function and module-qualified call expressions, and list/map/record nodes. |
| Runtime values and display | Implemented and tested: text, Boolean, `nothing`, arbitrary-precision integer, exact decimal, binary64 float, bytes, lists, ordered maps, and ordered records, with canonical display, exact integer/decimal arithmetic (`Add`, `Sub`, `Mul`, `Div`, `Mod`), deep equality (`Equal`), and ordered comparison (`Compare`). Runtime tests pass. |
| Interpreter, standard library, concurrency, and permissions | Implemented and tested: ordered `set`/rebind and `show`, pre-execution validation (module imports, name resolution, exhaustive `if` and `try`/`catch` branch bindings, `do together` task group validation `GRAB-E208`, function registration, and arity checking), control flow (`if`, `repeat`, `for each`, `while` with `1,000,000`-step guard), functions (`make`/`using`/`give back`/`call` with isolated call frames and `256`-frame recursion limit), structured concurrency (`do together`/`wait for` with snapshot isolation, deterministic top-to-bottom output/binding commit order, and sibling cancellation), structured error handling (`try`/`catch`/`raise`, error records, non-catchable fatal safety limits `GRAB-E306`/`GRAB-E307`, and `beginner`/`developer`/`production` diagnostic modes), 16 built-in standard library modules (`text`, `math`, `collections`, `dates`, `files`, `json`, `http`, `server`, `data`, `cache`, `api`, `connect`, `events`, `sockets`, `crypto`, and `env`), unified `.field`/`at`/`has` lookup across records and maps, default-deny filesystem permissions (`--allow-read`/`--allow-write` with sensitive-path blocklist `GRAB-E401`), default-deny network permissions (`--allow-net` with SSRF blocking `GRAB-E402`), default-deny environment variable permissions (`--allow-env` with `GRAB-E401`), deterministic output with LF, and Go benchmark/allocation suite (`benchmark_test.go`). Interpreter tests pass. |
| CLI | Implemented and tested: `grab run`, `grab migrate`, `grab serve`, `grab check`, and `grab format [--write]`; database manifest and separate read/read-write/migrate `--allow-db` grants alongside filesystem/network permissions and diagnostics. 0.8 adds `--platform-config`, independent cache/connector/events/channel/telemetry grants, typed API contracts, `grab generate client`, connector locking, event maintenance, and supervised `--workers` / `--worker-base-port`. 0.9 adds `--engine=auto|ast|vm`. Check/format remain offline. |
| Tests and conformance | GRAB 0.1–0.7 automated suites cover language, templates, HTTP/server, databases, CLI, permissions, and lifecycle. 0.9 adds full `--engine=ast`/`vm`/`auto` differential parity (`tests/grab09_vm_equivalence_test.go`), IR/VM unit & verifier suites, and `benchmark_test.go` comparisons. Real-service adapter tests and race verification are documented in `docs/DATA-TESTING.md`; offline runs may skip the remote services. |
| Example programs | Existing 0.1–0.6 examples plus `catalog_migrate.grab`, `catalog_seed.grab`, `catalog_app.grab`, two catalog templates, and `grab.data.json`. Run instructions are in `examples/DATA.md`; HTTP tests exercise the catalog. |
| GRAB 0.2 templates and localhost server | Implemented and tested in `internal/template` and `internal/server` per `specifications/GRAB-LOCALHOST-SPEC-002.md`: `.grab.html` `{<identifier>}` interpolation (e.g. `{title}`), `{{`/`}}` literal braces, companion and reusable template lookup, value binding, context-aware HTML escaping, source-located diagnostics, and loopback-only `grab serve` with security headers and graceful shutdown. |
| GRAB 0.6 persistent application server | Implemented and tested: `route` lexing/AST/parsing/formatting, conflict checks (`GRAB-E209`); prepared code and literal templates loaded once; immutable startup baseline and per-request bindings; `use server` HTML/JSON/text/redirect/custom-response/JSON-body helpers (`GRAB-E312`); request params/query/headers/body/form/proxy metadata; static-first routes, HEAD, 404/405; bounded concurrency/queue/body/deadline controls; public-bind opt-in, trusted proxies, confined static files and ETags; probes, redacted request/console logs, panic containment, atomic reload and graceful shutdown. See `examples/SERVER.md`, `internal/server/application_test.go`, and `tests/grab06_conformance_test.go`. |
| GRAB 0.7 database access | Implemented: `internal/data` adapters and interpreter `use data`, strict literal prepared SQL/type contracts, default-deny database grants/configuration, bounded pools/results/deadlines, explicit scoped transactions, versioned maintenance migrations, and prepared server generation cleanup. Real SQLite/MariaDB/PostgreSQL tests, CLI/HTTP/end-to-end coverage, and runnable catalog. Disk SQLite is confined rollback-journal mode on Windows/amd64 and Linux/amd64; unsupported constructs/platforms fail closed. See `docs/DATA.md` and `docs/DATA-TESTING.md`. |

### Not implemented

- GRAB 0.1–0.7 does not include user-defined types, user-authored file imports, or in-place collection mutation.
- GRAB 0.8 and GRAB 1.0 release gates are complete; reproducible 30-minute 1/2/4-worker load/fault benchmarks were verified on November 13, 2026 (`docs/SCALE-TESTING.md`). Redis Cluster/Sentinel, durable WebSocket replay, and automated cluster membership are excluded. Rolling mode conservatively requires unchanged contract bytes, DB manifests, worker policy and Redis pool totals; those changes need a stopped deployment. See `docs/SCALE-TESTING.md`.
- GRAB 0.9 IR/bytecode/VM (`specifications/GRAB-VM-SPEC-009.md`) and GRAB 1.0 native MCP server (`specifications/GRAB-PLATFORM-SPEC-100.md`) are implemented, verified, and released. Native machine-code JIT compilation and on-disk persistent bytecode caches remain deferred (`ADR-026a`, `ADR-026b`). Post-1.0 milestones remain planned and unapproved (`docs/ROADMAP.md`). Database support boundaries are in `docs/DATA.md`.
- The approved 0.1 through 0.7 conformance milestones are complete; this is rule-based test coverage, not a proof over every possible program.

### Repository directory and package map

| Path | Purpose |
| --- | --- |
| `internal/ir/`, `internal/vm/` | GRAB 0.9 AST-to-IR lowering, jump-threading optimizer, compact stack bytecode compiler, fail-closed static bytecode verifier, pooled VM executor (`sync.Pool`), `do together` task-group runner, and PC-range `try`/`catch` handler table. |
| `internal/platform/`, `internal/cache/` | GRAB 0.8 work in progress: strict cache configuration/capabilities/secrets and generation-owned temporary memory/standalone Redis adapters with exact codec and atomic counters. Tests and limits in `docs/CACHE.md`; full acceptance is tracked in `docs/SCALE-TESTING.md`. |
| `cmd/grab/` | CLI run/migrate/serve/check/format/mcp, typed client generation, connector locking and event maintenance; independent host grants, `--allow-env`, `--allow-scripts`, and multi-process flags. |
| `internal/mcp/` | GRAB 1.0 native `stdio` JSON-RPC 2.0 MCP server (`grab mcp`) with strict `--workspace` confinement, `GRAB-E401` sensitive-path blocking, read-only default (`--allow-write` opt-in), and 5 tools (`grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, `grab_scaffold`). |
| `internal/contracts/`, `internal/connect/`, `internal/events/` | 0.8 typed contracts/client generation, verified connector adapters, signed webhooks and durable three-engine work/maintenance. |
| `internal/realtime/`, `internal/telemetry/` | 0.8 bounded authorized WebSocket transport and opt-in private metrics/traces. |
| `deploy/` | Production VPS deployment starter: Dockerfile, Caddyfile (automated TLS & HTTP/3), docker-compose.yml, and deployment guide. |
| `docs/` | Project documentation, including `docs/ROADMAP.md` (GRAB 0.1 through 1.1 + Post-1.0). |
| `docs/features/` | Self-contained HTML milestone feature guides (`index.html` hub and completed milestone guides `grab_04_features.html`–`grab_11_features.html`) explaining features, developer benefits, visitor/user benefits, and visual examples in plain English. |
| `docs/adr/` | Architecture decision register (`ADR-DECISION-REGISTER.md`) and recorded design boundaries. |
| `docs/blueprints/` | GRAB architecture blueprint PDFs; v1.0 is the approved baseline and v1.1 is not approved wholesale. |
| `examples/` | Runnable `.grab` programs and `.grab.html` templates, including SQLite catalog migration/seed/server examples plus `cache_app.grab` and `grab.platform.json`. See `DATA.md`, `SERVER.md` `CACHE.md` and `REALTIME.md`. |
| `internal/ast/` | Abstract syntax tree node types used by the parser, formatter, and interpreter. |
| `internal/interpreter/` | Validation and AST execution, 16 standard modules including crypto/env/data/cache/API/connectors/events/sockets, scoped database resources/transactions, generation-owned cache resources, capabilities (`--allow-read`/`--allow-write`/`--allow-net`/`--allow-env`), diagnostics, concurrency, server helpers, and benchmarks. |
| `internal/data/` | Host-owned database/sql adapters, strict SQL/contracts/configuration, engine authorization, confined rollback-journal SQLite VFS, remote TLS/DNS pinning, migrations, transaction/pool lifecycles, tests, and benchmarks. |
| `internal/lexer/` | Source reader, positions/spans, tokens, lexical analysis, and lexical diagnostics (`GRAB-E001`–`GRAB-E007`). |
| `internal/parser/` | Parser for GRAB 0.1–0.6 tokens, AST construction, and canonical code formatter (`format.go`). |
| `internal/runtime/` | Portable runtime values, arithmetic/comparison operations, and canonical display forms. |
| `internal/server/` | Persistent server: `prepared.go` (load-once code/templates), `application.go` (routing, limits, proxies, static files, logging, atomic reload), `supervisor.go` (listeners, signals, drain), platform signal files, table-driven tests, and `benchmark_test.go`. Legacy single-page mode is preserved. |
| `internal/template/` | `.grab.html` `{<identifier>}` parser, HTML lexical context analyzer, context-aware HTML/URL escaper, and renderer (`template_test.go`). |
| `specifications/` | Approved/implemented specifications 001–007, 009 (`GRAB-VM-SPEC-009.md`), and 100 (`GRAB-PLATFORM-SPEC-100.md`), plus owner-approved GRAB 0.8 specification `GRAB-SCALE-SPEC-008.md`. Post-1.0 milestones remain unapproved. |
| `tests/` | GRAB 0.8 cache, channel, 1/2/4-worker and complete durable-workflow fixtures (including opt-in two-worker Redis/MariaDB/PostgreSQL vectors), plus `conformance_test.go` (0.1), `localhost_conformance_test.go` (0.2), `grab03_conformance_test.go` (0.3), `grab04_conformance_test.go` (0.4), `grab05_conformance_test.go` (0.5), `grab06_conformance_test.go` (0.6 HTTP), `grab07_conformance_test.go` (0.7 DB), `grab09_vm_equivalence_test.go` (0.9 AST/VM/Auto parity), `structure_test.go` (AST spans), and `README.md`. |
| Repository root | `go.mod` declares module `grab`, Go 1.26 minimum, and pinned pure-Go SQLite/MariaDB/PostgreSQL dependencies; `go.sum` locks downloads. `AGENTS.md`, `HANDOFF.md`, and `GRAB-EXPERT.md` hold agent workflow rules, active ownership, and non-negotiable invariants. `index.grab` is runnable; `.gitignore` excludes local catalog files/build artifacts. |

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
