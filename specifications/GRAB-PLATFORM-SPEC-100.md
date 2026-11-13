# GRAB 1.0 — Stable, Production-Ready Platform & Native MCP Server

**Status:** Approved target; specification in review (Drafted November 13, 2026)  
**Version:** 1.0-draft — November 13, 2026  
**Current Owner:** Google Antigravity  
**Implementation record:** Pre-implementation specification only. No Go code changes are authorized until the project owner approves this specification.

---

## 1. Authority, scope, and compatibility

### 1.1 Architectural authority
This specification defines the **GRAB 1.0** general-availability release target established in:
- **Blueprint v1.0, Sections 9, 10, 11, 12, and 13** (*Security, GRAB Tools, Repository/deployment structure, GRAB 1.0 — General availability, Testing & benchmarking*).
- **Blueprint v1.1 (narrowly scoped)** Sections 9, 10, 12, and 13 (*MCP-ready developer tooling, security gates, compatibility rules*).
- **Existing Approved ADRs:** `ADR-001`–`ADR-010`, `ADR-011`–`ADR-018` (`ADR-017` server/browser security boundary), and `ADR-019a`–`ADR-026e`.
- **Proposed GRAB 1.0 ADRs (`ADR-027a`–`ADR-027d`):** Recorded in `docs/adr/ADR-DECISION-REGISTER.md` for owner review alongside this specification.

### 1.2 Included scope
1. **Native GRAB MCP Server (`grab mcp`):** A standard-input/output (`stdio`) JSON-RPC 2.0 Model Context Protocol (MCP) server built directly into the `grab` CLI (`cmd/grab/` + `internal/mcp/`) with capability-bounded, workspace-confined tools (`grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, `grab_scaffold`).
2. **Forward-Compatible Asset Cache-Busting (`server.asset` + Smart `Cache-Control`):** In-memory SHA-256 asset fingerprinting at `PreparedApp` startup/reload paired with immutable long-lived caching on matching `?v=` hashes and safe `no-cache` fallback during rolling multi-worker updates.
3. **Core `v1.0.0` Stability & Compatibility Freeze:** Formal freeze of GRAB's lexical/parser grammar, error code registry (`GRAB-E001`–`GRAB-E403`), `.grab.html` context-aware escaping rules, all 14 standard library modules, and `--engine=auto` Bytecode VM differential parity guarantees for the `v1.x` release line.
4. **Production Release & Security Gates (`ADR-017`):** Final pre-1.0 security audit, cross-platform Windows/Linux binary verification, `govulncheck ./...`, documentation completeness, and synchronized promotion rules for `v1.0.0` across `origin` (`urbanspress/grab.in.the.making`) and `stable` (`urbanspress/grab.stable`).

### 1.3 Explicit exclusions (deferred to Post-1.0)
- **Not included in GRAB 1.0:** `Grex JS` / browser compiler, `Grex` visual page builder, `GrabLab` CMS, `Groxy AI` standalone agent/Ollama runner, `Gashboard` desktop VPS panel, `Grabtaculous` marketplace, public package registry / `.grabpkg` remote installer, or native machine-code JIT compilation (`ADR-003`, `ADR-010`, `ADR-014`, `ADR-018`).
- **GRAB 0.8 Section 12 30-minute sustained fault/load gate:** Remains tracked in `docs/SCALE-TESTING.md` and is a mandatory prerequisite before `v1.0.0` promotion to `stable`.

---

## 2. Native GRAB MCP Server (`grab mcp`) — `ADR-027a`

### 2.1 Purpose & agent ecosystem
AI coding and design agents (**Groxy AI**, **OpenCode**, **Antigravity**, **Codex**, and any MCP-compatible client) need deterministic, authoritative ways to inspect, validate, format, and scaffold GRAB projects without guessing syntax, hallucinating error codes, or reading/writing outside the project workspace.

`grab mcp` exposes GRAB's own lexer, parser, formatter, static validator, route/schema inspectors, and safe project scaffolder over the standard **Model Context Protocol (MCP)** using newline-delimited JSON-RPC 2.0 over `stdin` / `stdout`. Diagnostic logs from `grab mcp` MUST go exclusively to `stderr` so `stdout` remains a clean JSON-RPC stream.

### 2.2 CLI invocation & default-deny capability flags
```sh
grab mcp --workspace <dir> [--allow-write] [--db-config <path>] [--platform-config <path>]
```

- **`--workspace <dir>` (required, or defaults to current working directory only when explicitly permitted; recommended explicit flag):** Canonicalized via `filepath.Abs` and `filepath.EvalSymlinks`. Every file path argument passed to any MCP tool MUST resolve strictly within this workspace root.
- **Default-deny write mode:** By default, `grab mcp` starts in **strict read-only mode**. Mutating actions (`grab_format` with `write: true`, `grab_scaffold` writing files to disk) MUST fail closed with a capability error (`GRAB-E401`) unless `--allow-write` is explicitly passed on the `grab mcp` command line.
- **No arbitrary shell or network execution:** `grab mcp` exposes **zero** shell-execution tools, **zero** arbitrary file-read/write tools outside `.grab`, `.grab.html`, `grab.data.json`, `grab.api.json`, and `grab.platform.json` project artifacts, and **zero** outbound network/database execution.

### 2.3 Protocol lifecycle & framing
- **Transport:** `stdio` (newline-delimited UTF-8 JSON-RPC 2.0 messages, max single request message size `4 MiB`).
- **Supported MCP methods:**
  - `initialize`: Negotiates protocol version (`2024-11-05` or latest compatible MCP revision), returns `serverInfo` (`name: "grab-mcp"`, `version: "1.0.0"`), and advertises `capabilities.tools`.
  - `notifications/initialized`: Acknowledged silently (no response).
  - `ping`: Returns `{}`.
  - `tools/list`: Returns the five built-in tool definitions with strict JSON Schemas.
  - `tools/call`: Dispatches to the named tool with validated arguments, returning `{ content: [{ type: "text", text: <JSON> }], isError: <bool> }`.
- **Malformed JSON-RPC / unknown methods:** Returns standard JSON-RPC error codes (`-32700` Parse error, `-32600` Invalid Request, `-32601` Method not found, `-32602` Invalid params).

### 2.4 Workspace-root confinement & security rules
Every tool accepting a file or directory path MUST enforce:
1. **Path confinement:** Reject empty paths, null bytes, UNC/drive-escape paths, `..` traversal escaping `--workspace`, and symlinks whose `filepath.EvalSymlinks` target leaves `--workspace`.
2. **Sensitive-path blocklist (`GRAB-E401`):** Unconditionally reject any path matching `.git`, `.env`, `.ssh`, `*.pem`, `*.key`, `id_rsa`, `id_ed25519`, or host secret files, reusing GRAB's existing sensitive-path matcher.
3. **Read size cap:** Reject files exceeding `10 MiB` (`internal/interpreter` read limit).
4. **Offline & side-effect-free inspection:** `grab_check`, `grab_format`, `grab_inspect_routes`, and `grab_inspect_schema` MUST NEVER execute top-level `.grab` runtime statements, connect to databases, or make network calls.

### 2.5 MCP tool specifications

| Tool Name | Mode | Inputs | Output & Behavior |
| --- | --- | --- | --- |
| `grab_check` | Read-only | `file` (workspace-relative `.grab` path), optional `template` (`.grab.html` path), optional `mode` (`beginner` \| `developer`) | Runs lexer, parser, pre-execution static validator, route conflict check (`GRAB-E209`), and `.grab.html` context-aware template check. Returns structured `{ ok, diagnostics: [{ code, category, message, line, column, file }] }`. |
| `grab_format` | Read-only by default (`write: true` requires `--allow-write`) | `file` (workspace-relative `.grab` path) OR `source` (inline `.grab` text), optional `write` (boolean, default `false`) | Runs `parser.Format` for canonical 2-space formatting. When `write` is `false`, returns `{ formatted, changed }` without touching disk. When `write` is `true`, requires `--allow-write` and atomically writes the formatted file. |
| `grab_inspect_routes` | Read-only | `file` (workspace-relative `.grab` path) | Parses the AST and returns all declared `route` blocks (`method`, `path`, `params`, `has_req_binding`, `line`, `column`) plus static vs parameterized priority order and any `use api` / `use sockets` bindings. |
| `grab_inspect_schema` | Read-only | Optional `db_config` (defaults to `grab.data.json` if present), optional `api_contract` (defaults to `grab.api.json` if present) | Reads and validates manifests inside `--workspace` (never reading host env secrets—secret env var names are returned as redacted metadata only, never values). Returns configured database aliases/engines, API contract routes/types, and schema metadata. |
| `grab_scaffold` | Write (`--allow-write` required) unless `dry_run: true` | `kind` (`route` \| `template` \| `app` \| `api_contract`), `target` (workspace-relative path), `name` / `routes` / `fields` options, optional `dry_run` (default `false`) | Generates idiomatic, canonically formatted GRAB `.grab` / `.grab.html` / `grab.api.json` starter files that pass `grab check` before being returned or written. Refuses to overwrite existing files unless `overwrite: true` is explicitly passed. |

---

## 3. Forward-Compatible Asset Cache-Busting (`server.asset` + Smart `Cache-Control`) — `ADR-027b`

### 3.1 Motivation
Modern production web deployments behind Nginx/Caddy and CDNs need aggressive browser caching (`max-age=31536000, immutable`) for static CSS, JS, fonts, and images—without serving stale assets after a deploy or breaking visitors mid-rollout during multi-worker rolling restarts.

### 3.2 `PreparedApp` in-memory asset manifest
At `PreparedApp` load time (and refreshed atomically on `--reload` / `SIGHUP`), if `--static` is configured (`staticRoot`, `staticPrefix`, default `/static/`):
1. `PreparedApp` walks `staticRoot` once in memory (respecting symlink confinement and the sensitive-path blocklist—never writing hash manifests or temp files to disk, ensuring 100% compatibility with `--read-only` containers).
2. For each regular file within `staticRoot` (up to existing bounded static limits), it computes the lowercase hex SHA-256 digest and stores the **12-character lowercase hex prefix** (`digest[:12]`) keyed by normalized URL path (e.g., `/static/app.css` → `a1b2c3d4e5f6`).
3. The manifest is **immutable** for the lifetime of that `PreparedApp` generation (`ADR-011`).

### 3.3 Built-in `server.asset` helper
`use server` gains one additive helper:
```grab
use server

set css_url to server.asset using "/static/app.css"
```
- **Input:** A single `text` path (either starting with `staticPrefix`, e.g. `"/static/app.css"`, or a relative asset path e.g. `"app.css"` resolved against `staticPrefix`). Path traversal (`..`), backslashes, null bytes, or sensitive paths fail deterministically with `GRAB-E312` (invalid server argument).
- **Output:** Canonical URL string with query fingerprint: `"/static/app.css?v=a1b2c3d4e5f6"`.
- **Missing asset or `--static` not configured:** Fails deterministically with `GRAB-E312` (`unknown static asset`) during route execution (and optionally validated at template/startup check when literal), preventing silent broken asset links.
- **AST & VM Parity:** Registered in the shared `use server` host bridge so `--engine=ast`, `--engine=vm`, and `--engine=auto` behave identically.

### 3.4 Smart `Cache-Control` response headers in `Application.static`
When serving `GET` or `HEAD` requests under `staticPrefix`:
1. **Exact `?v=` hash match:** If the request includes `?v=<hash>` and `<hash>` equals the served file's 12-char SHA-256 prefix:
   - Set `Cache-Control: public, max-age=31536000, immutable`
   - Set `ETag: "<full-sha256>"` (preserving existing `If-None-Match` → `304 Not Modified` support).
2. **Mismatch or missing `?v=`:** If `?v=` is absent or `?v=<hash>` does **not** match the file currently on disk/in this worker's generation (for example, during a rolling multi-worker restart where Worker A is on `v1` and Worker B is on `v2`):
   - Set `Cache-Control: no-cache` (forcing revalidation via `ETag` / `If-None-Match` instead of poisoning browser/CDN caches with an immutable wrong version!).
   - Set `ETag: "<full-sha256>"`.

---

## 4. Core `v1.0.0` Stability & Compatibility Freeze — `ADR-027c`

Upon `v1.0.0` release, GRAB enters **Semantic Versioning (`SemVer 2.0.0`)** stability for the entire `v1.x` release line:

### 4.1 Frozen language & template surface
- **Core syntax & grammar:** All GRAB 0.1–0.6 keywords (`set`, `to`, `show`, `if`, `otherwise`, `end`, `repeat`, `times`, `for`, `each`, `in`, `while`, `make`, `using`, `give`, `back`, `call`, `use`, `try`, `catch`, `raise`, `do`, `together`, `wait`, `for`, `route`, `yes`, `no`, `nothing`, `and`, `or`, `not`, `is`, `plus`, `minus`, `times`, `divided`, `by`, `modulo`, `joined`, `with`, `at`, `has`, `greater`, `than`, `less`, `at least`, `at most`), operator precedence, and canonical 2-space formatting (`grab format`).
- **Exact numeric semantics:** Arbitrary-precision `integer`, exact base-10 `decimal`, and isolated `float` (`binary64`) rules; zero silent float coercion.
- **`.grab.html` templates:** `{identifier}` interpolation, `{{` / `}}` literal braces, and context-aware HTML/attribute/URL escaping (`ADR-019d`).
- **Error code registry:** Existing error codes `GRAB-E001`–`GRAB-E007` (lexer), `GRAB-E101`–`GRAB-E103` (parser), `GRAB-E201`–`GRAB-E210` (static validation), `GRAB-E301`–`GRAB-E318` (runtime), and `GRAB-E401`–`GRAB-E403` (capability denials) remain stable in meaning and catchability (`GRAB-E306` and `GRAB-E307` remain non-catchable).
- **14 Standard Library Modules:** `text`, `math`, `collections`, `dates`, `files`, `json`, `http`, `server`, `data`, `cache`, `api`, `connect`, `events`, `sockets` (plus additive `server.asset`).
- **Execution Engine Parity:** `--engine=auto`, `--engine=ast`, and `--engine=vm` MUST maintain 100% differential equivalence across all conformance suites (`tests/`).

### 4.2 `v1.x` Compatibility & Deprecation Policy
- **No breaking changes in `v1.x`:** Existing valid `.grab` and `.grab.html` programs, `grab` CLI flags, and `v1` manifests (`grab.data.json`, `grab.api.json`, `grab.platform.json`, `grab.connect.lock`) MUST continue to work across all `v1.x` minor and patch releases.
- **Additive changes only:** New standard library helpers, optional CLI flags, or MCP tools may be added in `v1.x` minor releases without breaking existing callers.
- **Deprecation window:** Any feature marked deprecated in `v1.x` must emit a clear diagnostic warning and remain functional until at least `v2.0.0`.

---

## 5. Production Release & Security Gates (`ADR-017`, `ADR-027d`)

Before `v1.0.0` may be tagged or promoted to `stable` (`urbanspress/grab.stable`), **all** of the following gates MUST pass with recorded evidence:

1. **Full Automated Test & Race Suite:**
   - `go test -count=1 ./...` passes 100% on Windows (`windows/amd64`) and Linux (`linux/amd64`).
   - `go test -race -count=1 ./...` passes 100% on CGO-enabled Linux (`linux/amd64`), including real SQLite, MariaDB, PostgreSQL, and Redis integration suites.
   - Differential parity (`tests/grab09_vm_equivalence_test.go`) and new GRAB 1.0 MCP (`grab mcp`) + `server.asset` conformance tests pass across `--engine=ast`, `--engine=vm`, and `--engine=auto`.
2. **GRAB 0.8 Section 12 Sustained Fault/Load Gate:**
   - Reproducible 30-minute 1/2/4-worker sustained fault/load evidence recorded in `docs/SCALE-TESTING.md`.
3. **Security & Supply-Chain Audit (`ADR-009`, `ADR-017`):**
   - `govulncheck ./...` reports zero known vulnerabilities on the release Go toolchain.
   - `go mod verify` succeeds with zero modified dependencies.
   - Explicit audit of: default-deny capability flags, `GRAB-E401`–`E403` confinement, secret redaction in logs/diagnostics/MCP responses, `.grab.html` escaping, SSRF IP/DNS pinning, and `grab mcp` workspace-root confinement.
4. **Cross-Platform Binary & Deployment Verification:**
   - Clean `go build ./...` and reproducible release binaries for `linux/amd64` and `windows/amd64`.
   - `deploy/` Docker + Caddy/Nginx starter verified with `--read-only` root filesystem and `server.asset` immutable caching.
5. **Documentation & Two-Repository Promotion:**
   - `docs/ROADMAP.md`, `AGENTS.md`, `HANDOFF.md`, `docs/features/grab_10_features.html`, `docs/features/index.html`, and user/operator docs are 100% aligned with implemented behavior.
   - Annotated tag `v1.0.0` pushed to both `origin` (`urbanspress/grab.in.the.making`) and `stable` (`urbanspress/grab.stable`), with matching GitHub Releases created on both repositories.
