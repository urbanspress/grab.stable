# Changelog

All notable changes to **GRAB** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.1.0] - 2026-11-14

### Added
- **Native `use crypto` Standard Library Module (`internal/interpreter/crypto.go`, `specifications/GRAB-PLATFORM-SPEC-110.md`, `ADR-028a`):**
  - `crypto.hash_password(password)` and `crypto.verify_password(password, encoded_hash)` using `golang.org/x/crypto/argon2` (`argon2id`, 16-byte random salt, PHC encoding, `19 MiB` / `t=2` / `p=1` baseline, `crypto/subtle` constant-time verify).
  - `crypto.random_urlsafe(byte_len)` and `crypto.random_hex(byte_len)` backed by `crypto/rand`, bounded `16..64` bytes.
  - `crypto.sha256_hex(input)` and `crypto.sha256_base64url(input)` supporting RFC 7636 PKCE `S256` challenges and session token hashing across both `text` and `bytes`.
  - `crypto.encrypt_aead(plaintext, key)` and `crypto.decrypt_aead(ciphertext, key)` using AES-256-GCM authenticated encryption at rest (`"v1:"` envelope, 12-byte random nonce, strict 32-byte raw/hex/base64 key decoding).
- **Capability-Gated `use env` Module & `--allow-env` Flag (`internal/interpreter/env.go`, `ADR-028b`):**
  - Default-deny `env.get(name)` and `env.has(name)` across `grab run` and `grab serve`, failing closed with `PermissionDenied` (`GRAB-E401`) for any variable not explicitly allowlisted via `--allow-env <VAR[,VAR...]>`.
  - Added contextual `env.has` parser support alongside the existing `has` map/record operator and forwarded allowed env names through `internal/server/workers.go` (`restrictedWorkerEnv`).
- **Opt-In `--allow-scripts` Flag on `grab serve` (`internal/server/supervisor.go`, `application.go`, `cmd/grab/main.go`, `ADR-028c`):**
  - Preserves `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'` by default.
  - Passing `--allow-scripts` to `grab serve` emits `script-src 'self'` (keeping `object-src 'none'` and forbidding `unsafe-eval`/`unsafe-inline` scripts) so local **Grex JS v1.0.0** bundles run natively under strict CSP.
- **Verification & Tooling:**
  - Added `grab init` CLI subcommand (`cmd/grab/init.go`, `cmd/grab/init_test.go`) delegating via structured `exec.CommandContext` argv to `grabman init` with forwarded arguments and a plain-English install hint when `grabman` is absent from `PATH`.
  - Added unit, capability denial, AST/VM/Auto differential parity, live HTTP CSP, CLI flag, and MCP validation tests (`internal/interpreter/crypto_env_test.go`, `internal/parser/cache_test.go`, `internal/server/scripts_env_test.go`, `cmd/grab/v11_cli_test.go`, `tests/grab11_conformance_test.go`).
  - Added `specifications/GRAB-PLATFORM-SPEC-110.md`, `docs/features/grab_11_features.html`, `docs/releases/pdf/GRAB-v1.1.0-Guide.pdf`, and `golang.org/x/crypto` BSD-3-Clause notice in `docs/licenses/`.

---

## [1.0.1] - 2026-11-13

### Added
- **CLI Version Reporting (`grab -v`, `grab --version`, `grab version`):**
  - Added clean 4-line PHP-style CLI version banner branded for GRAB & Urbanspress, displaying the build version, `MMM DD YYYY HH:MM:SS` build timestamp, target `GOOS`/`GOARCH`, and Go toolchain version.
  - Added `-ldflags` build-time metadata support (`main.Version`, defaulting to `1.0.1`, and `main.BuildDate`, defaulting to `unknown`), returning exit code `0` cleanly.
  - Added `TestVersion_CLI` unit tests in `cmd/grab/main_test.go` covering default metadata, custom `-ldflags` metadata, and invalid extra argument rejection.

---

## [1.0.0] - 2026-11-13

### Added
- **Native GRAB MCP Server (`grab mcp`):** Standard I/O (`stdio`) JSON-RPC 2.0 Model Context Protocol server built directly into the GRAB CLI, enabling AI coding and design agents (**Groxy AI**, **OpenCode**, **Antigravity**, **Codex**) to safely inspect, validate, format, and scaffold GRAB applications without syntax hallucination.
  - Five capability-bounded tools: `grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, and `grab_scaffold`.
  - Strict `--workspace` path confinement blocking `.git`, `.env`, `.ssh`, and path escapes with `GRAB-E401`.
  - Default-deny read-only execution: `--allow-write` CLI flag required to persist scaffolded files or formatted code to disk.
- **Forward-Compatible Asset Cache-Busting (`server.asset` + Smart `Cache-Control`):**
  - Built-in `server.asset` helper in `use server` generating fingerprinted URLs (`/static/app.css?v=<12-char-sha256>`).
  - In-memory SHA-256 asset hash manifest computed once at `PreparedApp` startup and dynamically refreshed on `--reload` / `SIGHUP` without writing temporary files to disk (100% compatible with `--read-only` container filesystems).
  - Smart `Cache-Control: public, max-age=31536000, immutable` headers served on matching asset version hashes, with safe `no-cache` fallback on hash mismatch during rolling multi-worker restarts.
- **Core Platform Stability & SemVer Freeze:**
  - Formal freeze of GRAB language syntax, grammar, error registry (`GRAB-E001`–`GRAB-E403`), context-aware HTML template escaping, and all 14 standard library modules for the entire `v1.x` release line.
  - Two-tier VM differential parity: 100% semantic equivalence between AST and Bytecode VM engines verified across all language features.
- **Sustained Scale & Fault Acceptance Gate (`docs/SCALE-TESTING.md`):**
  - 30-minute 1/2/4-worker sustained fault/load benchmark executed against real PostgreSQL 18 and Redis 8.2.2 services (`tests/grab08_sustained_load_test.go`).
  - Zero data loss across 103,894+ sustained requests (`committed >= acknowledged` webhook reconciliation).
  - Automated worker fault recovery: supervisor cleanly detected and restarted SIGKILL-terminated workers in under 200 ms with zero connection pool corruption.
  - Bounded memory footprint (4–28 MB heap) and zero file descriptor or port leaks.
- **Production Supply Chain & Security Audit:**
  - Zero vulnerabilities reported by `govulncheck v1.9.0` across the entire codebase and dependencies.
  - Clean `go mod verify` cryptographic hash validation.
  - Reproducible static `linux/amd64` and `windows/amd64` release binary builds.

---

## [0.9.0] - 2026-11-12

### Added
- **Two-Tier Profile-Driven Virtual Machine (`--engine=auto|ast|vm`):** Automatic transparent tiering: cold one-off scripts start immediately on AST; hot functions (16 calls), hot loops (64 back-edge iterations), and hot routes (8 hits) promote dynamically to bytecode via thread-safe `sync.Once`.
- **Intermediate Representation (IR) & Optimizer (`internal/ir`):** Pre-indexed lexical slot allocations (`OpGetLocal`, `OpSetLocal`), jump-threading branch optimization, and scoped transaction tracking (`OpTxEnter`, `OpTxExit`).
- **Compact Bytecode & Fail-Closed Static Verifier (`internal/vm`):** 16-byte fixed-size instruction stream with static validation of stack bounds, constant pools, and jump targets prior to execution.
- **Structured PC-Range Exception Tables:** Fast happy-path `try`/`catch` without allocation; handler lookup via binary search over program-counter tables only when an error is raised (3.08× faster, 58.8% fewer allocs).
- **Inline Small-Integer Fast Path (`internal/runtime`):** Inline `int64` storage for small integers, automatically promoting to arbitrary-precision `big.Int` on overflow.
- **Pooled Machine Recycling:** `sync.Pool` allocation reuse for execution machine frames, eliminating memory churn under high concurrency.
- **Route Snapshot Map Bypass (`ExecuteRoute`):** High-throughput HTTP route handler path avoiding per-request variable map allocations (1.91× faster on compute routes).
- **Strict Conformance Parity:** 100% byte-for-byte semantic equivalence across AST, VM, and Auto execution engines verified by `tests/grab09_vm_equivalence_test.go`.

---

## [0.8.0] - 2026-11-12

### Added
- **Cache & Rate Limiting (`use cache`):** In-memory and standalone Redis cache adapters, exact-value codec, namespace isolation, TTL expiration, and atomic fixed-window counters for rate limiting without race conditions.
- **Typed API Contracts (`use api`):** Strict JSON schema validation for requests and responses, plus deterministic TypeScript client and React hook generator via `grab generate client`.
- **Integrity Connectors (`use connect`):** SHA-256 locked connector packages, destination validation, and SSRF-safe outbound transport.
- **Durable Event Queues (`use events`):** Signed HMAC-SHA256 webhook ingestion and transactional inbox/outbox event queues with worker fencing across SQLite, MariaDB, and PostgreSQL, accompanied by the `grab events` maintenance CLI.
- **Authorized Realtime Channels (`use sockets`):** Bounded WebSocket pub/sub with application authorization, exact Origin allowlists, typed JSON contracts, message rate limiting, and Redis backplane federation.
- **Multi-Worker Server Scaling:** Private loopback worker process supervision (`--workers`, `--worker-base-port`), aggregate pool budgeting, crash-loop containment, and zero-downtime rolling drains.
- **Observability & Telemetry:** Opt-in OpenMetrics endpoint (`/_metrics`) and verified-TLS OTLP/HTTP distributed tracing with durable transaction correlation and automatic PII/secret scrubbing.
- **Production VPS Deployment Starter:** Multi-stage `Dockerfile`, Caddy reverse proxy with automated TLS/HTTP-3 in `Caddyfile`, `docker-compose.yml`, and `landing.grab` demo in `deploy/`.

---

## [0.7.0] - 2026-11-11

### Added
- **Unified Database Access (`use data`):** High-performance drivers for pure-Go SQLite (Windows/amd64 and Linux/amd64 rollback journal), MariaDB 11.8+, and PostgreSQL 18+.
- **Prepared Statements by Construction:** Enforced literal SQL statements and typed parameter bindings to eliminate SQL injection vulnerabilities.
- **Lossless Database Types:** Exact `decimal` and `integer` database mapping; explicit representation of SQL `NULL` as `nothing`.
- **Scoped Transactions:** Atomic unit of work with explicit `begin`, `commit`, and `rollback` lifecycle.
- **Database Migrations (`grab migrate`):** Forward and backward schema migrations tracking applied versions in `_grab_migrations`.
- **Default-Deny Database Security:** Granular `--allow-db <name>:<mode>` capability grants (`read`, `read-write`, `migrate`).
- **Persistent Server Integration:** Connection pool management with automatic cleanup across request cycles and atomic reloads.

---

## [0.6.0] - 2026-11-11

### Added
- **Persistent Multi-Route Web Server (Grisp Foundation):** Natural routing syntax (`route "<METHOD>" "<path>" using req ... end`) with path parameter extraction (`{id}`).
- **Load-Once App Architecture (`PreparedApp`):** Pre-compiles and validates AST and `.grab.html` templates at boot for zero cold starts.
- **Strict Per-Request Isolation:** Fresh variable bindings per request, eliminating shared-state leakage between visitors.
- **Server Standard Module (`use server`):** Built-in response builders `server.html`, `server.json`, `server.text`, `server.redirect`, `server.response`, and `server.parse_json`.
- **Load Shedding & Traffic Controls:** Configurable concurrency limits (`--max-concurrency`), queue bounds (`--max-queue`), and graceful 503 backpressure.
- **Production Reverse Proxy Readiness:** `--allow-public` opt-in, `--trusted-proxy` CIDR allowlists for Nginx/Caddy client IP extraction, and path-confined `--static` asset serving with ETag caching.
- **Supervisor & Probes:** Built-in `/_health` and `/_ready` endpoints, JSON structured logging with credential redaction, and atomic reload (`--reload` on Windows; SIGHUP on Unix).

---

## [0.5.0] - 2026-11-11

### Added
- **Universal HTTP Client (`use http`):** Outbound HTTP requests (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `get_json`), custom headers, timeouts, and bounded retries.
- **Lossless JSON Codec (`use json`):** `json.parse`, `json.stringify`, and `json.stringify_pretty` preserving exact `integer` and `decimal` values without floating-point rounding errors.
- **Default-Deny Network Capabilities:** `--allow-net <host>` allowlists with mandatory TLS 1.2+ verification.
- **Built-in SSRF Protection:** Automatic blocking of loopback (`127.0.0.1`), RFC 1918 private subnets, and cloud instance metadata addresses (`GRAB-E402`).
- **Structured Concurrency:** `do together` / `wait for` task groups with snapshot isolation, deterministic commit ordering, and sibling cancellation.

---

## [0.4.0] - 2026-11-11

### Added
- **Standard Library Modules:**
  - `text`: String manipulation, casing, trimming, padding, searching, and template formatting.
  - `math`: Exact rounding, absolute value, minimum, maximum, clamp, and power functions.
  - `collections`: List/map filtering, mapping, folding, sorting, and keys/values extraction.
  - `dates`: ISO 8601 parsing, formatting, timestamp generation, and calendar arithmetic.
  - `files`: File reading, writing, appending, directory traversal, and existence checks.
- **Structured Error Model:** `try` / `catch` / `raise` with error records containing message, code, source location, and stack trace.
- **Diagnostic Modes:** Configurable error output modes (`beginner`, `developer`, `production`).
- **Default-Deny Filesystem Capabilities:** `--allow-read` and `--allow-write` with mandatory blocking of sensitive paths (`.git`, `.env`, `.ssh`, private keys) via `GRAB-E401`.
- **CLI Development Tools:** `grab check` for offline validation and `grab format` for canonical code formatting.

---

## [0.3.0] - 2026-11-11

### Added
- **Control Flow:** `if` / `otherwise if` / `otherwise` branching, `repeat <n> times` counted loops, `for each <item> in <collection>` iteration, and `while <cond>` guarded loops.
- **User-Defined Functions:** `make <fn> using <params> ... give back <val> ... end` and `call <fn> using <args>` with lexical scoping and recursion limit (256 frames).
- **Collection Access:** Unified dot notation (`.field`), indexing (`at`), and membership checking (`has`) for records and maps.
- **Full Operator Precedence:** Mathematical (`times`, `divided by`, `modulo`, `plus`, `minus`), text (`joined with`), comparisons, equality, and logical operators (`and`, `or`, `not`).

---

## [0.2.0] - 2026-11-11

### Added
- **Localhost Preview Server:** Fast development HTTP server launched with `grab serve <file.grab>`.
- **Context-Aware HTML Templates (`.grab.html`):** Interpolation with `{identifier}` and literal brace escaping `{{` / `}}`.
- **Automatic XSS Prevention:** Lexical HTML parser analyzing injection contexts (HTML body, attributes, URLs) to apply appropriate escaping automatically.
- **Companion Template Auto-Discovery:** Seamlessly discovers `.grab.html` files matching script names.

---

## [0.1.0] - 2026-11-10

### Added
- **Core Language Foundation:** Lexer, parser, and Abstract Syntax Tree (AST) evaluator.
- **First-Class Value Types:** Text, Boolean, `nothing`, arbitrary-precision `integer`, exact `decimal`, `float`, `bytes`, `list`, `map`, and `record`.
- **Exact Decimal Arithmetic:** Currency-safe calculations without IEEE 754 floating-point inaccuracies.
- **Base Statements:** Module import (`use <module>`), variable assignment (`set <id> to <expr>`), and output (`show <expr>`).
