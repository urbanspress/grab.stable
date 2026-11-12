# GRAB HTTP Client, JSON & Structured Concurrency Specification 005

## Status and authority

**Status:** APPROVED for GRAB 0.5. Project-owner approval recorded November 11, 2026.
**Target milestone:** GRAB 0.5 — HTTP client, JSON, and async task groups
**Owner approval date:** November 11, 2026
**Prerequisite baseline:** `specifications/GRAB-LANGUAGE-SPEC-001.md` (GRAB 0.1), `specifications/GRAB-LOCALHOST-SPEC-002.md` (GRAB 0.2), `specifications/GRAB-LANGUAGE-SPEC-003.md` (GRAB 0.3), `specifications/GRAB-STDLIB-SPEC-004.md` (GRAB 0.4), and `docs/adr/ADR-DECISION-REGISTER.md`

This specification defines the language, standard-library, security, and CLI expansion for **GRAB 0.5**, settling the design questions in `docs/ROADMAP.md` and Blueprint v1.0 Sections 5, 8, and 9 (`use http` universal HTTP client, `use json` lossless JSON encoding/decoding, `--allow-net` capability-based network permissions with SSRF protection, and `do together` / `wait for` structured concurrency).

It does not claim that any GRAB 0.5 feature is implemented yet. Implementation begins only after explicit project-owner approval of this specification.

---

## 1. Purpose and scope

GRAB 0.5 equips the GRAB platform with safe external API connectivity, lossless JSON serialization, and deterministic structured concurrency while preserving 100% backward compatibility with GRAB 0.1–0.4 programs:

- **Universal HTTP client (`use http`, GRAB Connect foundation):** Built-in `http.get`, `http.get_json`, `http.post`, `http.put`, `http.patch`, `http.delete`, and `http.request` functions supporting custom headers, automatic JSON request body encoding, bounded timeouts, and safe bounded retries.
- **Lossless JSON codec (`use json`):** `json.parse`, `json.stringify`, and `json.stringify_pretty` converting between RFC 8259 JSON text and portable GRAB values (`record`, `map`, `list`, `text`, `boolean`, `nothing`, and exact `integer` / `decimal` numbers without floating-point precision loss).
- **Default-deny network permissions (`--allow-net`, `ADR-009`):** Outbound network requests are denied by default (`GRAB-E402`) unless explicitly permitted via `--allow-net <host[,host...]>` on `grab run` or `grab serve`, with mandatory TLS verification and automatic SSRF blocking of private, loopback, and cloud-metadata IP ranges unless explicitly allowlisted.
- **Structured concurrency (`do together` / `wait for`, `ADR-007` & `ADR-015`):** Explicit task-group blocks that run independent operations concurrently with snapshot isolation, deterministic top-to-bottom result and output ordering, automatic sibling cancellation on failure, and deterministic error propagation inside `try` / `catch`.

### Explicitly outside GRAB 0.5

GRAB 0.5 does **not** include:
- Provider-specific connector packages (such as Stripe or Lulu SDKs), inbound webhook signature engines, or WebSockets (planned for **GRAB 0.8**);
- Persistent multi-route production HTTP server supervision or reverse-proxy worker pools (planned for **GRAB 0.6**);
- SQLite, MariaDB, or PostgreSQL database drivers (planned for **GRAB 0.7**);
- Detached background threads/goroutines that outlive their lexical `do together` block;
- Bytecode VM or intermediate representation (`ADR-013`, planned for **GRAB 0.9**).

---

## 2. Lexical and grammar additions

### 2.1 New reserved keywords in GRAB 0.5

In addition to the GRAB 0.1–0.4 reserved keywords, GRAB 0.5 reserves three words from Blueprint v1.0's candidate registry for structured concurrency:
- **Task groups:** `do`, `together`, `wait` (`for` is already reserved in GRAB 0.3)

*(Built-in module names `http` and `json` and their function names are not globally reserved keywords; a module name becomes bound in the file's module namespace only when `use <module>` is declared at the top level.)*

### 2.2 Formal EBNF grammar extensions

```ebnf
statement          = set_statement
                   | show_statement
                   | if_statement
                   | repeat_statement
                   | for_each_statement
                   | while_statement
                   | try_statement
                   | raise_statement
                   | do_together_stmt
                   | give_back_statement
                   | call_statement ;

do_together_stmt   = "do", "together", newline,
                     task_statement, { newline, task_statement }, newline,
                     "end" ;

task_statement     = "set", identifier, "to", [ "wait", "for" ], expression
                   | [ "wait", "for" ], call_statement ;
```

### 2.3 Canonical formatting of `do together` blocks (`grab format`)

`grab format` formats every `do together ... end` block with 2-space indentation and normalizes task assignments to the explicit `set <identifier> to wait for <expression>` (and `wait for call <target> ...`) form so concurrent waiting is visually unmistakable in source code.

---

## 3. Universal HTTP client (`use http`) and JSON codec (`use json`)

### 3.1 `json` module (`use json`)

| Function & signature | Behavior and return value |
| --- | --- |
| `json.parse using s` | Requires `text` `s` containing valid RFC 8259 JSON (up to `10 MiB`, maximum nesting depth `64`). Returns the decoded portable GRAB value. Raises `GRAB-E309` (`standard library error`) if `s` is not valid JSON or exceeds nesting limits. |
| `json.stringify using value` | Converts any JSON-serializable portable GRAB value into compact canonical RFC 8259 JSON `text`. Raises `GRAB-E309` if `value` contains raw `bytes` or non-finite numbers. |
| `json.stringify_pretty using value` | Converts any JSON-serializable portable GRAB value into 2-space indented RFC 8259 JSON `text`. |

#### Lossless JSON-to-GRAB and GRAB-to-JSON mapping rules

1. **Primitives:**
   - JSON `"string"` $\leftrightarrow$ GRAB `text`
   - JSON `true` / `false` $\leftrightarrow$ GRAB `boolean` (`yes` / `no`)
   - JSON `null` $\leftrightarrow$ GRAB `nothing`
2. **Exact numbers (no silent float rounding):**
   - JSON integer tokens (`-?[0-9]+`, e.g. `42`, `-100`) decode into arbitrary-precision GRAB `integer`.
   - JSON fractional tokens without an exponent (`-?[0-9]+\.[0-9]+`, e.g. `19.95`, `0.0125`) decode into exact base-10 GRAB `decimal`, preserving exact scale.
   - JSON numbers with scientific notation (`e` or `E`, e.g. `1.5e4`) decode into GRAB `float` (`binary64`).
   - When encoding back to JSON (`json.stringify`), `integer`, `decimal`, and finite `float` values emit exact JSON numeric literals without quotes.
3. **Arrays and Objects (`list`, `record`, and `map`):**
   - JSON arrays `[ ... ]` $\leftrightarrow$ GRAB `list`.
   - JSON objects `{ ... }`:
     - During `json.parse`, if a JSON object's keys are all non-empty valid GRAB identifier names (`[A-Za-z_][A-Za-z0-9_]*`, non-reserved) with no duplicate keys, it decodes into an ordered GRAB `record` (so `item.price` works directly). Otherwise (for example, keys containing hyphens like `"content-type"` or reserved words), it decodes into an ordered GRAB `map`.
     - **Unified lookup interoperability:** To ensure developers never have to guess whether decoded JSON is a `record` or a `map`, GRAB 0.5 allows `obj at "key"` and `obj has "key"` on **both** `record` and `map` values, and `obj.field` / `obj has field` on **both** `record` and `map` values!
   - During `json.stringify`, both GRAB `record` and GRAB `map` encode as JSON objects `{ ... }` in their exact field/entry order.

---

### 3.2 `http` module (`use http`)

| Function & signature | Behavior and return value |
| --- | --- |
| `http.get using url` <br> `http.get using url, options` | Performs an HTTP `GET` request to `text` `url` with optional `options` (`record` or `map`). Returns an HTTP response `record`. |
| `http.get_json using url` <br> `http.get_json using url, options` | Performs an HTTP `GET` request with default `Accept: application/json`, verifies that the response status is `2xx` (raising `GRAB-E310` otherwise), and returns the decoded JSON value via `json.parse`. |
| `http.post using url, body` <br> `http.post using url, body, options` | Performs an HTTP `POST` request with `body` and optional `options`. Returns an HTTP response `record`. |
| `http.put using url, body` <br> `http.put using url, body, options` | Performs an HTTP `PUT` request with `body` and optional `options`. Returns an HTTP response `record`. |
| `http.patch using url, body` <br> `http.patch using url, body, options` | Performs an HTTP `PATCH` request with `body` and optional `options`. Returns an HTTP response `record`. |
| `http.delete using url` <br> `http.delete using url, options` | Performs an HTTP `DELETE` request to `url` with optional `options`. Returns an HTTP response `record`. |
| `http.request using spec` | Performs an HTTP request configured by `spec` (`record` or `map` with required `method` and `url`, plus optional `headers`, `body`, `timeout_ms`, and `retries`). Returns an HTTP response `record`. |

#### Request `body` encoding rules

- If `body` is `nothing`, an empty request body is sent.
- If `body` is `text`, its UTF-8 bytes are sent (default `Content-Type: text/plain; charset=utf-8` if not set in `headers`).
- If `body` is `bytes`, the raw bytes are sent (default `Content-Type: application/octet-stream` if not set in `headers`).
- If `body` is a `record`, `map`, or `list`, it is automatically encoded with `json.stringify` and sent with default `Content-Type: application/json` (unless overridden in `headers`).

#### Request `options` / `spec` fields

An `options` `record` or `map` may contain any of the following optional keys (unknown keys raise `GRAB-E309`):
- `headers`: `map` or `record` of header names to `text` values.
- `timeout_ms`: `integer` timeout in milliseconds for the total operation (including retries). Must be between `1` and `60000` (`60 seconds`). Defaults to `10000` (`10 seconds`).
- `retries`: `integer` maximum retry count on transient failures. Must be between `0` and `3`. Defaults to `0`.
- *(For `http.request` only)* `method`: `text` (`"GET"`, `"POST"`, `"PUT"`, `"PATCH"`, `"DELETE"`, or `"HEAD"`, case-insensitive) and `url`: `text` (`http://` or `https://`), plus optional `body`.

#### HTTP response `record` shape

Every `http.get`, `http.post`, `http.put`, `http.patch`, `http.delete`, and `http.request` call returns an immutable `record` with five fields:

```grab
record {
  status: 200,
  ok: yes,
  headers: {"content-type": "application/json"},
  body: "{\"sku\":\"A-17\"}",
  url: "https://api.example.com/products/A-17"
}
```

- `status`: `integer` HTTP status code (e.g. `200`, `404`, `500`). Non-`2xx` HTTP responses do **not** automatically raise an exception in `http.get`/`post`/`put`/`patch`/`delete`—instead, `response.ok` is `no` and `response.status` holds the status code so the program can inspect it cleanly (whereas `http.get_json` requires `2xx` and raises `GRAB-E310` on non-`2xx`).
- `ok`: `boolean` (`yes` when `status` is at least `200` and at most `299`, otherwise `no`).
- `headers`: `map` with lowercase `text` header names mapped to `text` values (multiple values for the same header joined with `", "`).
- `body`: `text` response body (validated as UTF-8, bounded at `10 MiB` / `10,485,760` bytes; responses exceeding `10 MiB` fail with `GRAB-E310`).
- `url`: `text` final URL after any redirects.

#### Bounded retry contract

When `retries` is set to `1`, `2`, or `3`:
1. For `GET`, `HEAD`, `PUT`, and `DELETE`, retries occur on transient connection/transport errors or HTTP status `429`, `502`, `503`, or `504`.
2. For non-idempotent methods (`POST`, `PATCH`), retries occur **only** when `retries` is explicitly set in `options` and only on connection reset/refused before a response is received or HTTP `429`/`503`.
3. Each retry waits a bounded deterministic backoff (`100 ms`, `200 ms`, `400 ms`) while respecting the overall `timeout_ms` deadline.

---

## 4. Capability-based network security (`--allow-net`, SSRF & TLS)

Per `ADR-009` (Security by default and least privilege):

### 4.1 Default-deny outbound network policy

1. Every GRAB program starts with **zero** outbound network permissions.
2. Calling any `http.*` function without matching `--allow-net` permission raises `GRAB-E402` (`network permission denied`).
3. Network capabilities are granted at CLI invocation time on `grab run` and `grab serve`:
   - `--allow-net <host[,host...]>` (may be passed multiple times or as a comma-separated list, e.g. `--allow-net api.example.com,127.0.0.1:9000`).
   - Wildcard `*` (e.g. `--allow-net *` or `*.example.com`) is rejected by the CLI before program execution so outbound host allowlists remain explicit.

### 4.2 Host matching rules

1. An allowlist entry without a port (e.g. `api.example.com`) permits `http://` (port `80`) and `https://` (port `443`) to `api.example.com` (case-insensitive ASCII hostname match).
2. An allowlist entry with an explicit port (e.g. `127.0.0.1:8081` or `api.example.com:8443`) permits requests only to that exact `host:port`.
3. Only `http://` and `https://` URL schemes are allowed; any other scheme (`file://`, `ftp://`, `ws://`, `javascript:`, etc.) fails with `GRAB-E310` / `GRAB-E402`.

### 4.3 Mandatory SSRF (Server-Side Request Forgery) protection

To prevent a public hostname or redirect from reaching private internal services or cloud metadata endpoints:

1. Before opening a socket (and on every redirect hop, up to a maximum of `5` redirects), the runtime checks both the hostname allowlist and the resolved IP address(es).
2. Resolved IP addresses in any of the following ranges are **blocked by default** with `GRAB-E402` (`network permission denied`):
   - **Loopback:** `127.0.0.0/8`, `::1`, `localhost`
   - **Link-local & Cloud Metadata (`169.254.169.254`):** `169.254.0.0/16`, `fe80::/10`
   - **Private RFC 1918 IPv4:** `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
   - **Unique Local IPv6 (ULA):** `fc00::/7`
   - **Unspecified / Broadcast:** `0.0.0.0/8`, `::`
3. **Explicit local/private exception for development and testing:** A loopback or private IP address (or `localhost`) is permitted **only** if that exact literal host (e.g. `127.0.0.1`, `127.0.0.1:8081`, or `localhost:8081`) was explicitly listed in `--allow-net`. Even then, cloud metadata IP `169.254.169.254` is **always** blocked unconditionally.
4. **Redirect validation:** Up to `5` HTTP redirects (`301`, `302`, `303`, `307`, `308`) are followed automatically, and every redirect target URL is re-validated against `--allow-net` and the SSRF rules before connecting. Sensitive headers (`Authorization`, `Cookie`, `X-API-Key`) are stripped if a redirect crosses to a different host.

### 4.4 TLS verification and secret redaction

1. All `https://` connections enforce TLS 1.2+ certificate chain and hostname verification using the system root store.
2. Values of sensitive request/response headers (`Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `X-API-Key`) are never included in error messages or diagnostic outputs.

---

## 5. Structured concurrency (`do together` / `wait for`)

Per `ADR-007` and `ADR-015`, GRAB never runs arbitrary statements concurrently behind the scenes. Concurrent execution occurs **only** inside an explicit `do together ... end` block.

### 5.1 Example syntax

```grab
use http
use json

do together
  set profile_resp to wait for http.get using "https://api.example.com/profile"
  set orders_resp to wait for http.get using "https://api.example.com/orders"
end

set profile to json.parse using profile_resp.body
set orders to json.parse using orders_resp.body
show profile.name
```

### 5.2 Static validation rules (`grab check` and pre-execution)

1. A `do together ... end` block must contain between `1` and `16` task statements (`GRAB-E208` if more than `16` tasks are declared in a single block).
2. Each task statement must be either `set <identifier> to [wait for] <expression>` or `[wait for] <call_statement>`.
3. **Distinct target bindings:** Two tasks inside the same `do together` block must not `set` the same variable `<identifier>`. Duplicate target variable bindings inside a `do together` block are rejected before execution with `GRAB-E208` (`invalid task group`).
4. **Snapshot independence:** Within a `do together` block, a task expression may read variables bound *before* the `do together` block, and cannot read a variable first bound by a sibling task in the same `do together` block.

### 5.3 Execution, isolation, and determinism contract

1. **Snapshot isolation (zero data races):** When entering `do together`, the interpreter takes an immutable snapshot of the current lexical environment for each task. Because all GRAB runtime values are immutable and each task writes to its own isolated frame and its own isolated output buffer, concurrent tasks never share mutable state.
2. **Bounded execution:** All tasks in a `do together` block run concurrently under a shared cancellable Go `context.Context`.
3. **Deterministic commit order:** After all tasks in the `do together` block finish:
   - Each task's `show` output (if a called user function executed `show`) is flushed to the parent output writer in **exact top-to-bottom source order** (`Task 1`, then `Task 2`, etc.).
   - Each task's bound variable (`set <identifier> to ...`) is written to the enclosing scope in **exact top-to-bottom source order**.
4. **Deterministic failure and sibling cancellation (`try` / `catch`):**
   - If any task fails with a runtime, network, or permission error, the runtime immediately cancels the shared context (aborting in-flight HTTP requests in sibling tasks) and waits for all sibling tasks in the block to terminate before leaving `do together`.
   - If multiple tasks fail, the `do together` block propagates the error from the **first failing task in top-to-bottom source order**, ensuring 100% deterministic behavior inside `try ... catch err ... end`.
   - Non-catchable fatal safety limits (`GRAB-E306` loop step limit and `GRAB-E307` recursion depth limit) inside any task remain non-catchable and abort the program immediately.

---

## 6. New error codes in GRAB 0.5

GRAB 0.5 preserves all `GRAB-E001`–`GRAB-E401` error codes from GRAB 0.4 and adds:

| Code | Stage | Category | Meaning |
| --- | --- | --- | --- |
| `GRAB-E208` | Validation | `invalid task group` | A `do together` block has duplicate target variable bindings or exceeds the maximum of `16` concurrent tasks per block. |
| `GRAB-E310` | Runtime | `network error` | HTTP request failed (invalid URL, unsupported scheme, connection failure, non-`2xx` status in `http.get_json`, redirect loop, or response exceeding `10 MiB`). |
| `GRAB-E311` | Runtime | `request timeout` | HTTP request exceeded its `timeout_ms` deadline. |
| `GRAB-E402` | Capability | `network permission denied` | `http.*` operation attempted to connect or redirect to a host not granted via `--allow-net` or blocked by the SSRF private/loopback/metadata protection policy. |

All new error codes work with `try` / `catch` (`err.code`, `err.category`, `err.message`, `err.line`, `err.column`) and include plain-English `Hint:` suggestions in `--diagnostics beginner` mode.

---

## 7. CLI flag additions (`--allow-net`)

```text
grab run [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] [--allow-net <host[,host...]>] <file.grab>
grab serve [--addr 127.0.0.1:8080] [--diagnostics beginner|developer|production] [--allow-read <dir>] [--allow-write <dir>] [--allow-net <host[,host...]>] <file.grab> [template.grab.html]
```

---

## 8. Conformance examples

### 8.1 Fetching JSON and formatting prices (`use http`, `use json`, `use text`)

```grab
use http
use json
use text

set item to http.get_json using "http://127.0.0.1:9000/api/product"
set badge to "$" joined with (text.from using item.price)
show item.name joined with " — " joined with badge
```

### 8.2 Concurrent API requests with `do together` / `wait for` and `try` / `catch`

```grab
use http
use json

try
  do together
    set stock_resp to wait for http.get using "http://127.0.0.1:9000/api/stock"
    set shipping_resp to wait for http.get using "http://127.0.0.1:9000/api/shipping"
  end
  set stock to json.parse using stock_resp.body
  set shipping to json.parse using shipping_resp.body
  show "Available: " joined with stock.status joined with ", Ships in: " joined with shipping.window
catch err
  show "Service unavailable [" joined with err.code joined with "]: " joined with err.message
end
```

---

## 9. Owner approval checklist

Before Go implementation of GRAB 0.5 begins, the project owner approves:

- [x] Reserved keywords `do`, `together`, and `wait` for structured concurrency (`do together ... end`).
- [x] Built-in modules `use http` (`get`, `get_json`, `post`, `put`, `patch`, `delete`, `request`) and `use json` (`parse`, `stringify`, `stringify_pretty`) with exact `integer` and `decimal` number preservation and unified `record`/`map` lookup.
- [x] Default-deny network permissions (`--allow-net <host[,host...]>`), mandatory TLS verification, and automatic SSRF blocking of private/loopback/cloud-metadata IP ranges (`GRAB-E402`).
- [x] Deterministic `do together` / `wait for` execution with snapshot isolation, top-to-bottom result/output/error ordering, and automatic sibling cancellation on failure.
