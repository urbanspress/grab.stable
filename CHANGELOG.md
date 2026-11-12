# Changelog

All notable changes to **GRAB** are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- **Persistent Multi-Route Web Server (Griss Foundation):** Natural routing syntax (`route "<METHOD>" "<path>" using req ... end`) with path parameter extraction (`{id}`).
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
