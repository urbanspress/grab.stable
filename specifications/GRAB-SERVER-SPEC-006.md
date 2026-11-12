# GRAB Persistent Application Server Specification 006

## Status and authority

**Status:** Approved for implementation by the project owner's explicit Codex takeover request on November 11, 2026.
**Target milestone:** GRAB 0.6 — Persistent application server (`GRAB Server`)
**Prerequisite baseline:** `specifications/GRAB-LANGUAGE-SPEC-001.md` (GRAB 0.1), `specifications/GRAB-LOCALHOST-SPEC-002.md` (GRAB 0.2), `specifications/GRAB-LANGUAGE-SPEC-003.md` (GRAB 0.3), `specifications/GRAB-STDLIB-SPEC-004.md` (GRAB 0.4), `specifications/GRAB-CONNECT-SPEC-005.md` (GRAB 0.5), and `docs/adr/ADR-DECISION-REGISTER.md`

This specification defines the language, server runtime, routing, isolation, backpressure, supervision, logging, and CLI extensions for **GRAB 0.6**, settling the open architecture questions in `docs/ROADMAP.md`, `docs/adr/ADR-DECISION-REGISTER.md` (`ADR-005`, `ADR-011`, `ADR-012`, `ADR-015`, `ADR-016`, `ADR-017`), and Blueprint v1.0/v1.1 Section 6.

The project owner authorized completion of this specification in the Codex takeover request. Implementation and verification status are recorded in `AGENTS.md`, `HANDOFF.md`, and `docs/ROADMAP.md`.

---

## 1. Purpose and scope

GRAB 0.6 upgrades `grab serve` from a single-page localhost previewer into a **persistent, high-throughput, isolated application server** (`GRAB Server`) while preserving 100% backward compatibility with existing GRAB 0.1–0.5 programs and GRAB 0.2 `.grab.html` templates:

- **Load-once immutable `PreparedApp` (`ADR-011`, `ADR-012`):** Lexing, parsing, static validation, route compilation, and template context validation run once when starting or reloading the application—never repeated on every HTTP request.
- **Multi-route web & JSON API applications (`route` + `use server`):** Readable top-level `route "<METHOD>" "<path>" [using <req>] ... end` declarations plus the built-in `use server` module for rendering escaped `.grab.html` pages, returning lossless JSON API responses, safe redirects, and parsing JSON request payloads.
- **Strict per-request execution isolation (`ADR-011`):** Every incoming HTTP request executes in a fresh, isolated scope cloned from an immutable startup baseline, with defensive fault/panic isolation so one failed request cannot corrupt other visitors or crash the server process.
- **Standalone & Nginx reverse-proxy deployment (`ADR-005`):** Safe loopback binding (`127.0.0.1:8080`) by default (working seamlessly directly or behind Nginx reverse proxy), explicit `--allow-public` opt-in for non-loopback listeners, and explicit `--trusted-proxy` allowlists so `X-Forwarded-*` headers are never blindly trusted.
- **Operational controls & supervision:** Bounded concurrency and request queues (`503` load shedding), request/body/header/read/write/idle timeouts, built-in `/_health` and `/_ready` probes, optional path-confined `--static` serving, structured JSON/text logging with secret redaction, atomic zero-downtime reload (`--reload` / `SIGHUP`), and graceful `SIGINT`/`SIGTERM` shutdown.
- **Repeatable server benchmarks (`ADR-016`):** Go `testing.B` HTTP handler and routing benchmarks in `internal/server/benchmark_test.go` measuring throughput and allocations per request for `PreparedApp` serving.

### Explicitly outside GRAB 0.6

GRAB 0.6 does **not** include:
- SQLite, MariaDB, or PostgreSQL database drivers and query builders (`GRAB Data`, planned for **GRAB 0.7**);
- Provider-specific connector SDKs, inbound webhook signature engines, WebSockets, multi-process worker clustering, or rate-limiter middleware (planned for **GRAB 0.8**);
- Intermediate representation (IR) or bytecode VM (`ADR-013`, planned for **GRAB 0.9**);
- Client-side JavaScript compilation, React/TypeScript adapters, or CMS features (`ADR-014`, `ADR-018`).

---

## 2. Language and grammar additions (`route` & `use server`)

### 2.1 New reserved keyword in GRAB 0.6

GRAB 0.6 reserves **one** top-level declaration keyword:
- **`route`**

*(Existing keywords `using`, `give`, `back`, and `end` are reused cleanly without reserving HTTP method names like `get`/`post` or common variable names like `req`, `server`, `status`, or `headers`. The built-in module name `server` becomes active only when `use server` is declared at the top level.)*

### 2.2 Formal EBNF grammar for route blocks

```ebnf
top_level_stmt     = use_decl
                   | function_def
                   | route_def
                   | statement ;

route_def          = "route", text_literal, text_literal, [ "using", identifier ], newline,
                     { statement, newline },
                     "end" ;
```

1. **Placement constraint:** `route` declarations may appear only at top level (not nested inside `if`, loops, `try`, functions, `do together`, or another `route`). Placing `route` inside another block is a syntax error (`GRAB-E101`).
2. **Method literal:** The first text literal MUST be one of the uppercase HTTP methods `"GET"`, `"HEAD"`, `"POST"`, `"PUT"`, `"PATCH"`, or `"DELETE"`. Any other value is rejected statically at `grab check` / startup preflight with `GRAB-E209` (`invalid route definition`).
3. **Path pattern literal:** The second text literal MUST start with `"/"` and contain well-formed static segments and `{param}` placeholders (for example `"/"`, `"/products"`, `"/products/{id}"`, `"/users/{user_id}/orders/{order_id}"`). Duplicate placeholder names within the same route or malformed braces fail preflight with `GRAB-E209`.
4. **Optional request parameter (`using <ident>`):** When present, `<ident>` (commonly `req`) is bound inside the `route` block to the immutable incoming HTTP request `record`. Inside a `route` body, `give back <expr>` exits the route handler and supplies the HTTP response value.
5. **Canonical formatting (`grab format`):** `grab format` indents `route` bodies by 2 spaces between `route ...` and `end`, matching `make`, `if`, and `do together`.

---

## 3. Built-in `server` module (`use server`) and Request/Response contract

### 3.1 Request `record` shape (`using req`)

When a route declares `using req`, GRAB binds `req` to an immutable `record` whose fields display in canonical GRAB form as:

```text
record {method: "GET", path: "/products/A-17", params: record {id: "A-17"}, query: {"sort": "price"}, headers: {"accept": "application/json", "user-agent": "..."}, body: "", client_ip: "127.0.0.1", scheme: "http", host: "127.0.0.1:8080", request_id: "req_01hf8z9k2p"}
```

- `params`: A `record` (or `map` if parameter names include non-identifier characters) containing URL-unescaped UTF-8 path parameter strings. Because of GRAB 0.5's unified lookup rules, `req.params.id`, `req.params at "id"`, and `req.params has id` all work uniformly.
- `query`: A `map` from query parameter name (`text`) to first decoded query value (`text`).
- `headers`: A `map` with lowercase `text` header names mapped to trimmed `text` values.
- `body`: UTF-8 `text` request body bounded by `--max-body-bytes` (default `1 MiB`). Requests exceeding `--max-body-bytes` are rejected before invoking the handler with `413 Payload Too Large`; invalid UTF-8 request bodies are rejected with `400 Bad Request`.
- `form`: A map of first decoded values for `application/x-www-form-urlencoded` bodies, added to fulfill the owner's takeover request for form binding; empty for other content types. The original `body` text remains available. Malformed URL-encoded form bodies return `400`.
- `client_ip`, `scheme`, and `host`: Derived exclusively from direct TCP socket metadata unless the immediate peer matches `--trusted-proxy` (see Section 5.2).

### 3.2 `use server` functions

| Function & signature | Behavior and return value |
| --- | --- |
| `server.html using template_path, data` <br> `server.html using template_path, data, status` | Renders the `.grab.html` template at `template_path` with fields/entries from `data` (`record` or `map`) using GRAB 0.2 context-aware HTML escaping (`internal/template`), returning an HTML response `record` (default status `200`, or explicit integer status `100`–`599`). |
| `server.json using status, value` | Encodes `value` using GRAB 0.5's lossless `json.stringify` (`integer`/`decimal` precision preserved) and returns an HTTP response `record` with `Content-Type: application/json; charset=utf-8` and HTTP `status`. |
| `server.text using status, content` | Returns a plain UTF-8 HTTP response `record` with `Content-Type: text/plain; charset=utf-8` and `status`. |
| `server.redirect using location` <br> `server.redirect using location, status` | Returns an HTTP redirect response (`Location: <location>`) with status `302` (or explicit `301`/`302`/`303`/`307`/`308`). Validates `<location>` is a safe relative path (`/`, `?`, `#`), `http://`, or `https://` without CR/LF header injection; raises `GRAB-E312` on unsafe schemes (`javascript:`, `data:`, etc.) or invalid status codes. |
| `server.response using spec` | Builds a full custom response `record` from a `record` or `map` containing `status` (`integer`), optional `headers` (`map`/`record` of `text`), and `body` (`text` or `bytes`). |
| `server.parse_json using req` | Validates and decodes `req.body` using GRAB 0.5's lossless JSON parser (`integer`/`decimal` precision preserved). Raises `GRAB-E312` (`invalid server response` / bad request payload) if `req.body` is empty or invalid JSON so `try`/`catch` (or HTTP `400` handling) can respond cleanly. |

### 3.3 Route return rules & static template pre-validation

1. A `route` handler completes by executing `give back <val>`:
   - If `<val>` is a response `record` created by `server.html`, `server.json`, `server.text`, `server.redirect`, or `server.response`, GRAB Server writes that exact status, headers, and buffered body.
   - Shorthand: `give back <text>` returns `200 OK` with `Content-Type: text/plain; charset=utf-8`.
   - Returning `nothing` (including falling off the end of a `route` without `give back`) or an unserializable non-response value raises `GRAB-E312` (`500 Internal Server Error` safe error response; logged and counted without crashing the server).
2. **Startup template compilation (`ADR-012`):** Whenever `server.html` is called with a literal text template path (e.g. `server.html using "views/product.grab.html", data`), the loader pre-resolves (under workspace confinement rules identical to GRAB 0.2), parses, and HTML-context-validates that `.grab.html` template **once during startup / reload**. Invalid templates or path escapes abort startup/reload immediately with exit code `1` before listening (`GRAB-E209`).
3. **Header safety:** Response headers reject CR (`\r`), LF (`\n`), and NUL bytes (`\x00`), raising `GRAB-E312`.

---

## 4. Persistent `PreparedApp`, Routing & Request Isolation (`ADR-011`, `ADR-012`)

### 4.1 Execution modes (`grab serve`)

GRAB 0.6 automatically selects the appropriate mode from the `.grab` file without breaking any GRAB 0.2 command:

1. **Legacy Single-Page Mode (zero `route` blocks in `<file.grab>`):**
   - Fully compatible with `grab serve <file.grab> [template.grab.html]` from GRAB 0.2.
   - `PreparedApp` lexes, parses, and validates both `<file.grab>` and `<template.grab.html>` **once at load time** (no re-parsing on every request in persistent mode!).
   - Serves `GET /` and `HEAD /` by executing the pre-validated AST on a fresh isolated scope and rendering the pre-parsed `.grab.html` template.
   - When `--reload` is passed for local development, file mtimes are checked cleanly before serving so edits still appear on browser refresh.
2. **Multi-Route Application Mode (one or more `route` blocks in `<file.grab>`):**
   - Top-level statements outside `route` and `make` blocks (e.g. `set store_name to "Northbound Goods"`, `set tax_rate to 0.08`) execute **once at startup/reload** to produce an immutable `BaseEnv` snapshot (`map[string]grabrt.Value`).
   - Any top-level runtime or permission failure aborts startup/reload before serving traffic.
   - On each HTTP request matching a compiled route, the server creates a fresh child scope initialized from `PreparedApp.BaseEnv` (plus `req` if declared), executes ONLY the matched route handler in an isolated execution context, buffers `show` output to the server console/log stream (or discards in quiet mode), buffers the complete HTTP response, and discards all request-local state when the request completes.

### 4.2 Deterministic route matching & conflict detection

1. **Pattern matching:** Routes match path segments split on `"/"`:
   - Static literal segment matches that exact path segment (`"products"` == `"products"`).
   - Parameter segment `{name}` matches exactly one non-empty path segment (URL-decoded UTF-8 text).
2. **Deterministic priority:** If multiple routes could match a path (for example `"/products/featured"` and `"/products/{id}"`), priority runs left-to-right per segment with **exact static segments strictly higher priority than `{param}` segments** (regardless of declaration order).
3. **Preflight conflict rejection (`GRAB-E209`):** Two routes with the same HTTP method and structurally equivalent patterns (same segment count and static vs. `{param}` positions, e.g. `route "GET" "/items/{a}"` and `route "GET" "/items/{b}"`) are rejected statically during `grab check` and `PreparedApp` load.
4. **Automatic `HEAD`, `405`, and `404`:**
   - A `HEAD` request falls back to a matching `GET` route if no explicit `HEAD` route exists, emitting status and headers with an empty body.
   - If the path matches one or more routes for *other* methods, respond `405 Method Not Allowed` with sorted `Allow: ...` header.
   - Otherwise respond `404 Not Found` (`text/plain; charset=utf-8` or JSON error envelope when `Accept: application/json` is requested).

---

## 5. Production Controls, Security & Supervisor Lifecycle

### 5.1 Listener binding & Nginx reverse-proxy policy (`ADR-005`)

1. **Safe loopback default:** `grab serve` continues to default `--addr` to `127.0.0.1:8080`. Binding to loopback addresses (`127.0.0.1:<port>`, `::1:<port>`, `localhost:<port>`, e.g. `127.0.0.1:8765` behind Nginx) requires no special override.
2. **Explicit opt-in for public interfaces:** Binding `--addr` to a non-loopback address (`0.0.0.0:8080`, `[::]:8080`, LAN/WAN IP) is rejected unless `--allow-public` is explicitly passed. This preserves `ADR-009` secure-by-default behavior so local dev apps are never accidentally exposed to a public network.
3. **TLS notice:** When `--allow-public` binds a non-loopback address without an external reverse proxy (`--trusted-proxy`), GRAB Server logs an explicit startup advisory recommending TLS termination at Nginx or a reverse proxy (`ADR-005`). *(Native ACME/TLS termination and HTTP/2 server listener options remain optional future extensions cleanly layered in `GRAB Server`.)*

### 5.2 Explicit trusted-proxy policy (`--trusted-proxy`)

1. By default (`--trusted-proxy` omitted), forwarded headers (`X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`, `Forwarded`) are **ignored** when computing `req.client_ip`, `req.scheme`, `req.host`, and request logs.
2. When `--trusted-proxy <ip_or_cidr[,...]>` is passed (for example `--trusted-proxy 127.0.0.1,::1` behind Nginx):
   - Only when the actual TCP remote address belongs to an allowlisted proxy IP/CIDR does GRAB Server inspect `X-Forwarded-For` (walking right-to-left past trusted proxy hops to pick the true client IP), `X-Forwarded-Proto` (`"http"` or `"https"` only), and validated `X-Forwarded-Host`.
   - Invalid or spoofed header values fall back safely to TCP connection values.

### 5.3 Static file handling (`--static`)

Static file serving is **disabled by default** (`ADR-009`) and enabled explicitly via `--static <[url_prefix=]dir>` (default prefix `"/static/"` when only a directory path is supplied, e.g. `--static public`).

- **Strict confinement:** Canonical path resolution (`filepath.EvalSymlinks`) rejects `..` traversal, NUL bytes, symlinks escaping `<dir>`, directories without explicit index listing (no auto-directory browsing), and all sensitive filenames matching GRAB 0.4's blocklist (`.env`, `.env.*`, `.git`, `.ssh`, `id_rsa`, `id_ed25519`, `*.pem`, `*.key`).
- **Headers & HTTP behavior:** Serves only `GET` and `HEAD`, enforces `X-Content-Type-Options: nosniff`, computes safe `Content-Type`, supports conditional `ETag` / `If-None-Match` (`304 Not Modified`), and returns `404` for non-matching files. Explicit GRAB routes take precedence unless under the static prefix; static assets never spawn an interpreter frame.

### 5.4 Backpressure, concurrency, and resource limits (`ADR-015`)

Every request passes through a bounded execution gate protecting CPU and memory:

| Limit / Flag | Default | Behavior when exceeded |
| --- | --- | --- |
| `--max-concurrency` | `64` | Maximum concurrent active GRAB request executions (`do together` blocks inside a request remain capped at 16 tasks per GRAB 0.5). |
| `--max-queue` | `128` | Maximum requests waiting for an execution slot when `--max-concurrency` is saturated. Exceeding `--max-queue` immediately sheds load with **`503 Service Unavailable`** and `Retry-After: 1` (zero unbounded goroutine growth). |
| `--queue-timeout` | `1s` | Maximum wait time inside the queue before shedding with **`503 Service Unavailable`** (`Retry-After: 1`). |
| `--request-timeout` | `10s` | Per-request execution context deadline (`context.WithTimeout`). Cancels loops/in-flight `http.*` calls immediately and returns **`504 Gateway Timeout`** safely without emitting partial HTML. |
| `--max-body-bytes` | `1048576` (`1 MiB`) | Enforced via `http.MaxBytesReader`; returns **`413 Payload Too Large`**. |
| Server socket timeouts | `ReadHeaderTimeout: 5s` <br> `ReadTimeout: 15s` <br> `WriteTimeout: 30s` <br> `IdleTimeout: 60s` <br> `MaxHeaderBytes: 65536` | Mitigates slow-loris and oversized header attacks before hitting route handlers. |

### 5.5 Operational endpoints, logging, reload & shutdown

1. **Health & Readiness probes (reserved server endpoints):**
   - `GET /_health` (and `HEAD /_health`): Bypass execution queue so orchestrators/Nginx always get a fast liveness check (`200 OK`, `application/json`, `{"status":"ok"}`).
   - `GET /_ready`: Returns `200 OK` `{"status":"ready","in_flight":<n>,"max_concurrency":<m>}` when a valid `PreparedApp` is active and not draining; returns `503 Service Unavailable` `{"status":"draining"}` during graceful shutdown/drain or when the queue is saturated.
2. **Structured request logging (`--log-format json|text`):**
   - Configurable `--log-format` (`text` default in local terminal, `json` recommended for production).
   - Records `timestamp`, `level`, `request_id`, `method`, `path`, `route`, `status`, `duration_ms`, `client_ip`, and `error_code` (when errored).
   - **Mandatory secret redaction:** Never logs `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, or `X-API-Key` header values or raw request/response bodies.
3. **Panic & fault isolation:**
   - Each request handler wraps execution in defensive panic recovery; an unexpected Go panic or GRAB runtime error (`GRAB-E3xx`) produces a clean `500 Internal Server Error` with `request_id` correlation—**never** tearing down the persistent server process.
   - Under `--diagnostics production`, HTTP `500` responses omit internal file paths, source snippets, and stack traces (returning only safe message + `request_id` while details go to server logs).
4. **Zero-downtime atomic reload (`--reload` / `SIGHUP`):**
   - Builds and fully validates a new `PreparedApp` off the request path. Only if lexing, parsing, preflight checks, top-level initialization, and template checks **all pass** does the supervisor atomically swap the active `PreparedApp` pointer (`atomic.Pointer`). If reload validation fails, the server logs the error and continues serving the previous last-known-good `PreparedApp`.
5. **Graceful shutdown (`SIGINT` / `SIGTERM`):**
   - Immediately flips `/_ready` to `503 draining`, closes the listening socket, waits up to `--shutdown-timeout` (default `10s`) for in-flight requests to finish cleanly via `http.Server.Shutdown`, and exits with code `0`.

### 5.6 Mandatory security headers

All responses from `grab serve` continue to include `X-Content-Type-Options: nosniff` and `X-Frame-Options: DENY`. HTML responses (`server.html` and GRAB 0.2 single-file templates) also include `Content-Security-Policy: default-src 'self'; script-src 'none'; object-src 'none'` and `Cache-Control: no-store` by default.

---

## 6. New error codes in GRAB 0.6

GRAB 0.6 preserves all `GRAB-E001`–`GRAB-E402` error codes from GRAB 0.4–0.5 and adds:

| Code | Stage | Category | Meaning |
| --- | --- | --- | --- |
| `GRAB-E209` | Validation | `invalid route definition` | Malformed HTTP method, invalid route pattern syntax, duplicate/ambiguous route pattern, `route` nested inside a block, missing `server.html` static template, or unsafe template context inside a route. |
| `GRAB-E312` | Runtime | `invalid server response` | Route handler returned `nothing` or invalid response type, used CR/LF in a response header, attempted an unsafe redirect scheme (`javascript:`/`data:`), or failed `server.parse_json` on invalid/empty request JSON. |

Both codes integrate with `try` / `catch` (`err.code`, `err.category`, `err.message`, `err.line`, `err.column`) and include plain-English `Hint:` suggestions in `--diagnostics beginner` mode.

---

## 7. Expanded `grab serve` CLI surface

```text
grab serve [flags] <file.grab> [template.grab.html]

Server & Security Flags (all backward-compatible with GRAB 0.2–0.5):
  --addr <host:port>            Bind address (default 127.0.0.1:8080)
  --allow-public                Permit binding --addr to non-loopback interfaces
  --trusted-proxy <ip[,...]>    Allowlisted reverse-proxy IPs/CIDRs for X-Forwarded-*
  --static <[prefix=]dir>       Serve static files from dir (default prefix /static/)
  --max-concurrency <N>         Max active request executions (default 64)
  --max-queue <N>               Max queued requests before 503 load shedding (default 128)
  --queue-timeout <duration>    Max wait time in queue (default 1s)
  --request-timeout <duration>  Max execution duration per request (default 10s)
  --max-body-bytes <N>          Max incoming request body size in bytes (default 1048576)
  --shutdown-timeout <duration> Graceful drain timeout on SIGINT/SIGTERM (default 10s)
  --reload                      Enable dev hot-reload on file modification timestamps
  --log-format <text|json>      Request log format (default text)
  --diagnostics <mode>          beginner | developer | production (default developer)
  --allow-read <dir>            Filesystem read capability (GRAB 0.4)
  --allow-write <dir>           Filesystem write capability (GRAB 0.4)
  --allow-net <host[,host...]>  Outbound HTTP capability allowlist (GRAB 0.5)
```

*(Invalid flag values print usage to `stderr` and exit with code `2`. Preflight failures on startup print source-located diagnostics and exit with code `1` before binding any port.)*

---

## 8. Conformance examples

### 8.1 Multi-route HTML & JSON store application (`app.grab`)

```grab
use server
use text
use math

set store_name to "Northbound Goods"
set tax_rate to 0.08

route "GET" "/" using req
  give back server.html using "home.grab.html", record {title: store_name, visitor_ip: req.client_ip}
end

route "GET" "/api/products/{id}" using req
  set price to 19.95
  set taxed to math.round using (price times (1 plus tax_rate)), 2
  set badge to "$" joined with (text.from using taxed)
  give back server.json using 200, record {store: store_name, sku: req.params.id, price: price, price_with_tax: taxed, display_price: badge}
end

route "POST" "/api/orders" using req
  try
    set order to server.parse_json using req
    give back server.json using 201, record {accepted: yes, sku: order.sku, request_id: req.request_id}
  catch err
    give back server.json using 400, record {error: err.message, code: err.code}
  end
end
```

---

## 9. Owner approval checklist

The owner authorized all items below in the explicit Codex takeover request:

- [x] Reserved top-level `route "<METHOD>" "<path>" [using <req>] ... end` syntax with priority route matching and static conflict detection (`GRAB-E209`).
- [x] Built-in `use server` module (`server.html`, `server.json`, `server.text`, `server.redirect`, `server.response`, `server.parse_json`) preserving context-aware HTML escaping and lossless exact numbers.
- [x] Immutable `PreparedApp` parse/validate-once model (`ADR-011`, `ADR-012`) with strict per-request execution isolation, defensive panic isolation, atomic reload (retaining last-known-good on invalid reload), and 100% backward compatibility with GRAB 0.2 single-page serving.
- [x] Deployment and proxy safety (`ADR-005`, `ADR-009`): safe `127.0.0.1:8080` default, `--allow-public` opt-in for non-loopback interfaces, `--trusted-proxy` CIDR/IP allowlist for `X-Forwarded-*` headers, and path-confined `--static` file serving blocking sensitive files/symlinks.
- [x] Operational controls (`ADR-015`, `ADR-016`): bounded `--max-concurrency`, `--max-queue` (`503` load shedding), `--request-timeout`, `--max-body-bytes`, `/_health` & `/_ready`, structured secret-redacted logging, graceful shutdown, and repeatable `internal/server/benchmark_test.go` throughput/allocation benchmarks.
