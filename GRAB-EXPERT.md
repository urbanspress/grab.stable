# GRAB-EXPERT: Non-Negotiable Engineering Invariants

Read this checklist **before** editing any Go package in GRAB. These invariants apply across AST execution (`internal/interpreter`), IR/bytecode (`internal/ir`, `internal/vm`), server/realtime (`internal/server`, `internal/realtime`), data/caching (`internal/data`, `internal/cache`), and integrations (`internal/contracts`, `internal/connect`, `internal/events`).

---

## 1. Exact Numbers (No Silent Float Coercion)
- **Never convert GRAB integers, decimals, or currency values to IEEE-754 `float64`.**
- Always represent values with `internal/runtime` (`grabrt.Value`): arbitrary-precision `integer` (`smallInt int64` fast path with automatic promotion to `big.Int`), exact base-10 `Decimal`, and strict `FloatKind` separation (`FloatKind` must never silently mix with `IntegerKind` or `DecimalKind` in arithmetic or equality).
- Preserve exact base-10 string wire encodings in JSON contracts (`GrabInteger` / `GrabDecimal`) and cache serialization codecs; use `TEXT`-affinity columns for SQLite currency and `DECIMAL`/`NUMERIC` for MariaDB/PostgreSQL.

## 2. Concurrency & Context Cancellation
- **Propagate `context.Context` everywhere:** check `ctx.Done()` in loops, interpreter/VM back-edges, HTTP calls, database queries, Redis operations, WebSocket reads/writes, and `do together` sibling tasks.
- **No leaked goroutines:** every background worker, supervisor process, `do together` task group, ticker, and socket loop must terminate cleanly on `ctx.Done()`, parent error, reload drain, or `Close()`.
- **Deterministic task-group commits:** `do together` / `wait for` must isolate task snapshots and commit output/bindings strictly in top-to-bottom source order (identical under `--engine=ast`, `--engine=vm`, and `--engine=auto`).

## 3. Resource, Runtime & VM Bounds
- **Loop guard (`GRAB-E307`):** enforce `DefaultMaxLoopSteps = 1,000,000` inside both AST and VM execution; never make `GRAB-E307` catchable by `try` / `catch`.
- **Call-depth guard (`GRAB-E306`):** enforce `DefaultMaxCallDepth = 256` inside both AST and VM call frames; never make `GRAB-E306` catchable by `try` / `catch`.
- **Fail-closed bytecode verifier (`internal/vm/verify.go`):** every compiled `vm.Chunk` must pass static verification (operand stack bounds, jump targets, handler ranges, constant/local/up-to-date slot indices) before execution.
- **Bounded server/transport budgets:** preserve request concurrency limits, bounded queues, request/body/header size limits, database pool/result/deadline caps, cache TTL/size bounds, and WebSocket queue/rate limits.
- **Immutable server generations (`ADR-011`):** never share mutable request state across requests or leave generation resources (DB pools, prepared statements, Redis clients, sockets) unclosed on reload/retire.

## 4. Security & Default-Deny Capabilities
- **Never bypass capability flags (`ADR-009`):** all access must be explicitly authorized via default-deny grants:
  - `--allow-read`, `--allow-write` (with sensitive-path blocklist `GRAB-E401` and symlink/path confinement)
  - `--allow-net` (with SSRF IP/DNS pinning `GRAB-E402`)
  - `--allow-db` (`read`, `read-write`, `migrate`-only for `grab migrate`, `GRAB-E403`)
  - `--allow-cache`, `--allow-connector`, `--allow-events`, `--allow-channel`, `--allow-telemetry`
- **No raw dynamic SQL or unescaped HTML:** always use strict literal prepared SQL statements (`internal/data`), `TEXT`-safe encoding, context-aware HTML/attribute/URL escaping (`internal/template`), and secret redaction in logs and error diagnostics.
- **Fail closed on malformed/untrusted inputs:** keep signature verification (`grab-hmac-sha256-v1`), `Origin` / TLS / trusted-proxy checks, and `grab.connect.lock` SHA-256 verification strict—never fall back automatically from Redis to memory cache or downgrade TLS/security defaults.

---

## Pre-Finish Sanity Gate
Before concluding any task, verify:
1. Changes match this checklist with zero capability, precision, or concurrency regressions.
2. Tests pass (`go test -race ./<package>` on CGO-enabled Unix/Linux, or `go test -count=1 ./<package>` on Windows without CGO, plus `go test -count=1 ./...` when required by `AGENTS.md`).
3. If execution, IR, VM, or stdlib behavior changed, differential parity passes across `--engine=ast`, `--engine=vm`, and `--engine=auto`.
