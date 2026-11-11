# GRAB Localhost & HTML Template Specification 002

## Status and authority

**Status:** APPROVED for GRAB 0.2. Project-owner approval recorded November 11, 2026.
**Target milestone:** GRAB 0.2 — Localhost browser preview
**Owner approval date:** November 11, 2026
**Prerequisite baseline:** `specifications/GRAB-LANGUAGE-SPEC-001.md` (GRAB 0.1) and `docs/adr/ADR-DECISION-REGISTER.md`

This specification defines the approved contract for **GRAB 0.2**, settling the design questions listed in `docs/ROADMAP.md` and `docs/adr/ADR-DECISION-REGISTER.md` (Open Question #9 and ADR-011, ADR-012, ADR-017 principles as applied to local preview).

---

## 1. Purpose and scope

GRAB 0.2 enables the first browser-based test of GRAB on `localhost` while keeping the core GRAB 0.1 language clean, deterministic, and modular:

- **Program logic stays in `.grab` files:** Core GRAB 0.1 syntax, lexical rules, parser, AST, and portable runtime values remain 100% unchanged.
- **Reusable HTML lives in `.grab.html` files:** Multiple `.grab` programs can bind their own values and render the same `.grab.html` template.
- **Simple `{identifier}` interpolation:** Templates reference bound variables directly by name inside single curly braces (such as `{title}`), without requiring `show` or introducing template-side logic, loops, or arbitrary expressions.
- **Mandatory context-aware HTML escaping:** Every interpolated value is escaped according to its HTML lexical position to prevent Cross-Site Scripting (XSS) and broken markup.
- **Loopback-only development server (`grab serve`):** A minimal, secure local HTTP server binds strictly to `127.0.0.1` / `localhost`, isolates every request in a fresh execution environment, enforces timeouts, and shuts down gracefully.

### Explicitly outside GRAB 0.2

GRAB 0.2 does **not** include:
- A public/production internet server, TLS termination, multi-route dynamic URL routers, or `POST` form processing;
- Template control flow (loops, conditionals, partials/includes) or expression evaluation inside `.grab.html` files;
- Client-side JavaScript generation, a GRAB-to-browser compiler, React/TypeScript adapters, incremental rendering, or DOM hydration (`ADR-014`, `ADR-018`);
- Database connections (`GRAB Data`), external HTTP calls (`GRAB Connect`), or a bytecode VM (`ADR-013`).

---

## 2. `.grab.html` lexical and interpolation grammar

### 2.1 Encoding and line endings

1. A `.grab.html` file MUST be valid UTF-8. Invalid UTF-8 is a template lexical error reported before rendering.
2. Both `LF` (`\n`) and `CRLF` (`\r\n`) are accepted in template source files. Rendered output normalizes all template line endings to `LF` (`\n`) for deterministic cross-platform output.
3. Diagnostic positions use 1-based line numbers and 1-based Unicode-scalar column numbers with half-open source spans, identical to `GRAB-LANGUAGE-SPEC-001.md` Section 6.

### 2.2 Interpolation syntax and literal braces

1. Templates consist of static HTML text interspersed with `{<identifier>}` interpolation tags (such as `{title}`) and escaped literal braces (`{{` and `}}`).
2. **Formal EBNF grammar for `.grab.html` templates:**

```ebnf
template           = { static_segment | escaped_brace | interpolation_tag } ;

escaped_brace      = "{{" | "}}" ;

interpolation_tag  = "{", [ hspace ], identifier, [ hspace ], "}" ;

hspace             = ( " " | "\t" ), { " " | "\t" } ;
identifier         = letter_or_underscore, { letter | digit | "_" } ;
```

3. **Identifier rules:** `<identifier>` follows the exact GRAB 0.1 rule (`[A-Za-z_][A-Za-z0-9_]*`, case-sensitive, excluding the GRAB 0.1 reserved words `set`, `to`, `show`, `joined`, `with`, `yes`, `no`, `nothing`, `record`).
4. **No expressions, keywords, or newlines inside tags:** Only a single variable identifier is permitted inside `{...}` (with optional surrounding spaces or tabs, e.g., `{title}` or `{ title }`). Complex text composition (such as `joined with`) MUST be computed inside the `.grab` file and bound to a variable via `set`. Newlines (`\n` or `\r`) are not permitted inside `{...}`.
5. **Brace escaping and malformed tags:**
   - `{{` in template source renders as a single literal `{` in output.
   - `}}` in template source renders as a single literal `}` in output.
   - Any single `{` that does not form a valid `{<identifier>}` tag (including unclosed `{`, empty `{}`, reserved words or multi-token forms such as `{show title}`, `{"literal"}`, or `{a joined with b}`) and any single unmatched `}` is a **malformed template interpolation** syntax error with a source span pointing to the offending brace or token.

---

## 3. Template lookup and reuse rules

### 3.1 Pairing `.grab` programs with `.grab.html` templates

1. **CLI invocation forms:**
   - **Default companion lookup:** `grab serve <program.grab>` looks for a template with the same base name and `.grab.html` extension in the same directory as `<program.grab>` (for example, `examples/hello.grab` defaults to `examples/hello.grab.html`).
   - **Explicit reusable template lookup:** `grab serve <program.grab> <template.grab.html>` pairs `<program.grab>` with the specified `<template.grab.html>`. This allows multiple `.grab` programs (such as `product_a.grab` and `product_b.grab`) to reuse a single shared template (such as `templates/product_card.grab.html`).
2. **Language grammar preservation:** Pairing is configured at invocation time so GRAB 0.1 `.grab` syntax and reserved words remain 100% unchanged.

### 3.2 Path safety and confinement

1. Both `<program.grab>` and `<template.grab.html>` MUST have the exact file extensions `.grab` and `.grab.html` respectively (case-sensitive).
2. **Directory confinement:** When a template path is resolved, its canonical cleaned path MUST reside within the working directory (or the directory tree of `<program.grab>`). Path traversal segments (`..`) that escape the root directory, NUL bytes, or non-regular files (directories, device files) MUST be rejected before execution.

---

## 4. Value-binding contract

1. **Execution order:**
   - First, the `.grab` source file is lexed, parsed, name-resolved, and executed using the GRAB 0.1 interpreter.
   - Any lexical, syntax, name-resolution, or runtime error in the `.grab` program aborts rendering immediately; no template output is produced.
2. **Environment snapshot:**
   - Upon successful completion of the `.grab` program, the interpreter's final variable environment (mapping each bound variable name to its immutable GRAB 0.1 `runtime.Value`) is passed as a read-only snapshot to the template engine.
   - If a variable was rebound multiple times via `set` in the `.grab` program, the template receives its final bound value.
3. **Pre-render binding verification:**
   - Every `{<identifier>}` tag in the `.grab.html` template is checked against the final `.grab` environment before any HTML output is written.
   - If `<identifier>` is not present in the `.grab` environment, rendering fails with an **unbound template variable** error located at `<identifier>`'s span inside the `.grab.html` file, and produces no HTML output.
   - A variable bound to `nothing` (`set note to nothing`) IS bound; it formats as its canonical display string `nothing`.
4. **Canonical value formatting prior to escaping:**
   - Before HTML context escaping is applied, each bound `runtime.Value` is converted to its canonical GRAB 0.1 display text (`GRAB-LANGUAGE-SPEC-001.md` Section 5.4):
     - **Text:** The raw Unicode string without surrounding quotes.
     - **Boolean:** `yes` or `no`.
     - **Nothing:** `nothing`.
     - **Numbers:** Base-10 integer, scale-preserving exact decimal, or canonical finite binary64 float.
     - **Bytes:** Canonical `b"..."` representation.
     - **Collections (list, map, record):** Canonical single-line `[...]`, `{...}`, or `record {...}` representation.

---

## 5. Context-aware HTML escaping

To uphold `ADR-017` (Server/browser security separation) and prevent XSS, `.grab.html` templates are analyzed for HTML lexical context before rendering. Raw unescaped HTML injection is not permitted in GRAB 0.2.

### 5.1 Permitted interpolation contexts and escaping rules

| HTML Context | Example | Escaping and validation rule |
| --- | --- | --- |
| **1. HTML Element Body / Text** | `<h1>{title}</h1>` | Escapes `&` $\to$ `&amp;`, `<` $\to$ `&lt;`, `>` $\to$ `&gt;`, `"` $\to$ `&#34;`, `'` $\to$ `&#39;`. |
| **2. Quoted Normal Attribute** | `<div title="{tip}" data-id='{id}'>` | Must be enclosed in `"` or `'`. Escapes `&`, `<`, `>`, `"`, and `'` to HTML entities so the value cannot break out of the attribute. |
| **3. Quoted URL Attribute** (`href`, `src`, `action`, `formaction`, case-insensitive) | `<a href="{link}">` | Must be enclosed in `"` or `'`. If `{id}` is at the start of the URL (after optional whitespace), validates the URL scheme: allows relative URLs (`/`, `./`, `../`, `?`, `#`, or path segments without `:`) and safe schemes `http://`, `https://`, and `mailto:`. Rejects dangerous schemes (`javascript:`, `vbscript:`, `data:`, `file:`, or unknown schemes) with a runtime **unsafe URL scheme** error before emitting output. Then applies attribute entity escaping. |

### 5.2 Disallowed contexts in GRAB 0.2 (Default-Deny)

If a `{<identifier>}` tag appears in any of the following HTML contexts, template validation MUST reject the template prior to execution with a **disallowed interpolation context** diagnostic pointing to the `{...}` span:

1. **Unquoted attribute values:** e.g., `<div class={cls}>` (all attributes containing `{<identifier>}` MUST be double- or single-quoted).
2. **Inside `<script>` or `<style>` elements:** e.g., `<script>var x = {x};</script>` or `<style>body { color: {c}; }</style>`.
3. **Inside HTML comments, DOCTYPE, or processing instructions:** e.g., `<!-- {secret} -->`.
4. **Inside tag names or attribute names:** e.g., `<{tag}>` or `<div {attr}="1">` or `<div onClick="{handler}">` (all `on*` event-handler attributes such as `onclick`, `onload`, `onerror` are rejected even when quoted, preventing inline JavaScript execution).

---

## 6. Output behavior and diagnostics

### 6.1 Standard output vs. rendered HTML output

1. **`.grab` `show` statements:** Any `show <expression>` statement inside the `.grab` file continues to write to the process's standard output stream (or server console log), preserving 100% compatibility with GRAB 0.1. It does **not** inject text into the HTTP response body.
2. **`.grab.html` rendered output:** The rendered HTML template is buffered completely in memory. Only when `.grab` execution, binding verification, and context-aware escaping all succeed is the buffered UTF-8 HTML written as the HTTP response body with status `200 OK` and header `Content-Type: text/html; charset=utf-8`.

### 6.2 Required diagnostic categories for GRAB 0.2

In addition to the 6 GRAB 0.1 diagnostic categories, GRAB 0.2 defines 5 template/server diagnostic categories with 1-based line and column spans:

1. `invalid template encoding` — `.grab.html` file contains invalid UTF-8.
2. `malformed template interpolation` — unclosed `{`, single unmatched `}`, empty `{}`, reserved keyword, or non-identifier inside `{...}`.
3. `disallowed interpolation context` — `{<identifier>}` placed in `<script>`, `<style>`, comment, tag/attribute name, `on*` event handler, or unquoted attribute, or inside an unclosed HTML tag.
4. `unbound template variable` — `{name}` references an identifier not bound by the `.grab` program.
5. `unsafe URL scheme` — value bound to a URL attribute (`href`, `src`, `action`, `formaction`) uses a disallowed scheme such as `javascript:` or `data:`.

---

## 7. Minimum localhost-server lifecycle and security contract

### 7.1 CLI syntax

```text
grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]
```

- If `--addr` is omitted, the server defaults to `127.0.0.1:8080`.
- `127.0.0.1:0` (or `localhost:0`) is permitted so automated tests can bind an ephemeral OS-assigned loopback port.
- Invalid CLI arguments or unknown flags print usage to `stderr` and exit with code `2`.

### 7.2 Strict loopback-only binding

1. The `--addr` host component MUST resolve to a loopback address (`127.0.0.1`, `::1`, or `localhost`).
2. Binding to wildcard or non-loopback addresses (`0.0.0.0`, `::`, `:8080` with empty host, or LAN/public IPs) MUST be rejected before opening a socket with exit code `1` and a clear error message.

### 7.3 Fail-fast startup and request isolation

1. **Startup preflight:** Before starting the TCP listener, `grab serve` reads, parses, and validates both `<file.grab>` (including GRAB 0.1 pre-execution name resolution) and `<template.grab.html>` (including lexical, tag, and HTML context validation). If either file is missing or invalid, `grab serve` reports the diagnostic to `stderr` and exits with code `1` without binding a port.
2. **Startup announcement:** Once listening, `grab serve` writes a single line to `stdout`:
   `grab: serving <file.grab> with <template.grab.html> at http://<actual-ip>:<port>/`
3. **Per-request isolation (`ADR-011`):**
   - Each incoming `GET /` or `HEAD /` request runs the `.grab` program and renders the `.grab.html` template in a fresh, isolated execution environment. No mutable state is shared across requests.
   - To make local editing convenient during browser preview, `grab serve` re-reads and validates `<file.grab>` and `<template.grab.html>` on each request (or serves the validated files cleanly); if an edit introduces an error or a runtime error occurs (`joined with` type error or `unsafe URL scheme`), the server responds with `500 Internal Server Error`, `Content-Type: text/plain; charset=utf-8`, and the source-located diagnostic message without crashing the server process.
4. **HTTP method and path rules:**
   - Only path `/` is served; any other request path (including `/favicon.ico` or subpaths) returns `404 Not Found` (`text/plain; charset=utf-8`).
   - Only `GET` and `HEAD` methods are allowed on `/`; any other HTTP method (`POST`, `PUT`, `DELETE`, etc.) returns `405 Method Not Allowed` with header `Allow: GET, HEAD`.

### 7.4 Mandatory security headers and resource timeouts

Every HTTP response from `grab serve` MUST include:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Content-Security-Policy: default-src 'self'; script-src 'none'; object-src 'none'`
- `Cache-Control: no-store`

The HTTP server MUST enforce bounded resource limits:
- `ReadHeaderTimeout`: 5 seconds
- `ReadTimeout`: 5 seconds
- `WriteTimeout`: 10 seconds
- `IdleTimeout`: 30 seconds
- `MaxHeaderBytes`: 65,536 (64 KiB)
- Per-request execution context timeout: 5 seconds

### 7.5 Graceful shutdown

When the server process receives `SIGINT` (`Ctrl+C`) or `SIGTERM`, it stops accepting new connections, waits up to 5 seconds for in-flight requests to complete via `http.Server.Shutdown`, and exits with code `0`.

---

## 8. Approved conformance cases for GRAB 0.2

### Case 1: Basic template interpolation and HTML escaping

`hello.grab`:
```grab
set title to "GRAB <0.2> & Friends"
set link to "https://example.com/docs?v=0.2&lang=grab"
set summary to "Safe \"quoted\" preview"
```

`hello.grab.html`:
```html
<!doctype html>
<html>
  <head><title>{title}</title></head>
  <body>
    <h1 title="{summary}">{title}</h1>
    <a href="{link}">Documentation {{v0.2}}</a>
  </body>
</html>
```

Expected rendered HTML (`200 OK`):
```html
<!doctype html>
<html>
  <head><title>GRAB &lt;0.2&gt; &amp; Friends</title></head>
  <body>
    <h1 title="Safe &#34;quoted&#34; preview">GRAB &lt;0.2&gt; &amp; Friends</h1>
    <a href="https://example.com/docs?v=0.2&amp;lang=grab">Documentation {v0.2}</a>
  </body>
</html>
```

### Case 2: Unbound template variable (error case)

`missing.grab`:
```grab
set title to "Hello"
```

`missing.grab.html`:
```html
<h1>{title}</h1>
<p>{author}</p>
```

Expected result: `unbound template variable` diagnostic spanning `author` on line 2 of `missing.grab.html`. No partial HTML output is emitted.

### Case 3: Disallowed context — inline script or event handler (error case)

`bad.grab.html`:
```html
<button onclick="{action}">Click</button>
```

Expected result: `disallowed interpolation context` diagnostic on line 1 of `bad.grab.html`. Server startup preflight rejects the template before opening a listener.

### Case 4: Unsafe URL scheme (error case)

`xss.grab`:
```grab
set bad_url to "javascript:alert(1)"
```

`xss.grab.html`:
```html
<a href="{bad_url}">Click</a>
```

Expected result: `unsafe URL scheme` error at render time; no partial HTML is written.

---

## 9. Owner approval record for GRAB 0.2

The project owner approved the following GRAB 0.2 decisions on November 11, 2026:

- [x] Use `{<identifier>}` (such as `{title}`) as the sole interpolation syntax in `.grab.html` files, with `{{` and `}}` for literal braces.
- [x] Keep all expressions and logic inside `.grab` files; require `<identifier>` in `{<identifier>}` to be bound in the `.grab` program's final environment.
- [x] Pair `.grab` and `.grab.html` via `grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]`, defaulting to `<basename>.grab.html` when the template argument is omitted.
- [x] Format non-text portable GRAB values using their canonical GRAB 0.1 display strings before applying HTML escaping.
- [x] Enforce context-aware HTML escaping in text, quoted attributes, and quoted URL attributes (`http:`, `https:`, `mailto:`, and relative URLs only), and reject interpolations in `<script>`, `<style>`, comments, tag/attribute names, `on*` event handlers, and unquoted attributes.
- [x] Restrict `grab serve` strictly to loopback addresses (`127.0.0.1` / `localhost` / `::1`) with per-request isolated execution, `GET`/`HEAD /` routing, security headers, and bounded timeouts.
