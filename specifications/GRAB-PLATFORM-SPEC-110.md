# GRAB Platform Specification 1.1 (`GRAB-PLATFORM-SPEC-110`)

- **Version:** `1.1.0`
- **Date:** November 14, 2026
- **Status:** Approved & Implemented (`ADR-028a`–`ADR-028c`, additive-only under `ADR-027c`)
- **Scope:** Additive standard library security primitives (`use crypto`), default-deny host environment access (`use env` + `--allow-env`), and opt-in strict script Content-Security-Policy (`grab serve --allow-scripts`) across the AST interpreter, Bytecode VM, Grisp application server, MCP checker, and CLI.

---

## 1. Compatibility Contract (`ADR-027c`)

GRAB `v1.1.0` is strictly **additive-only** within the `v1.x` compatibility line:
1. All `v1.0.0` / `v1.0.1` syntax, exact numbers, error codes (`GRAB-E001`–`GRAB-E403`), existing 14 stdlib modules, `.grab.html` context-aware escaping, and strict default HTTP headers remain unchanged.
2. Both `--engine=ast`, `--engine=vm`, and `--engine=auto` execute every new `crypto.*` and `env.*` builtin identically via `stdlibModules` and `HostBridge.CallModule`.
3. `grab mcp` (`grab_check` and `grab_format`) validates `use crypto`, `use env`, and `env.has` syntax offline without evaluating or exposing runtime secrets.

---

## 2. Native `use crypto` Standard Library Module (`ADR-028a`)

Built into `internal/interpreter/crypto.go` and registered in `stdlibModules`:

| Function | Arguments | Returns | Guarantees & Limits |
| --- | --- | --- | --- |
| `crypto.hash_password` | `(password text)` | `text` (PHC `$argon2id$...`) | `golang.org/x/crypto/argon2` (`m=19456, t=2, p=1`, keyLen `32`), fresh 16-byte `crypto/rand` salt per call, valid UTF-8 max `4096` bytes (`GRAB-E309` on overflow). |
| `crypto.verify_password` | `(password text, encoded_hash text)` | `boolean` (`yes`/`no`) | Bounded PHC parser (`8<=m<=524288`, `1<=t<=32`, `1<=p<=16`, `8<=salt<=64`, `16<=hash<=64`) + `crypto/subtle.ConstantTimeCompare`. Malformed hash -> `GRAB-E309`. |
| `crypto.random_urlsafe` | `(byte_len integer)` | `text` (unpadded base64url) | `crypto/rand`, bounded `16 <= byte_len <= 64` (`GRAB-E309` out of range, `GRAB-E301` non-integer). |
| `crypto.random_hex` | `(byte_len integer)` | `text` (lowercase hex) | `crypto/rand`, bounded `16 <= byte_len <= 64`. |
| `crypto.sha256_hex` | `(input text\|bytes)` | `text` (64 hex chars) | Deterministic SHA-256 digest (`GRAB-E301` otherwise). |
| `crypto.sha256_base64url` | `(input text\|bytes)` | `text` (RFC 7636 S256) | Unpadded RFC 4648 base64url SHA-256 digest (OAuth2 PKCE `S256` challenge). |
| `crypto.encrypt_aead` | `(plaintext text, key text\|bytes)` | `text` (`v1:<base64url>`) | AES-256-GCM with fresh 12-byte `crypto/rand` nonce. `key` accepts 32 raw `bytes`, 64-char hex, 43/44-char base64/base64url, or 32-byte raw ASCII `text`; key buffer zeroed after use. Max `10 MiB` UTF-8 plaintext. |
| `crypto.decrypt_aead` | `(ciphertext text, key text\|bytes)` | `text` | Verifies `"v1:"` envelope, AES-256-GCM tag authentication, and UTF-8 validity; wrong key or tampered payload fails cleanly with `GRAB-E309`. |

---

## 3. Capability-Gated `use env` Module & `--allow-env` (`ADR-028b`)

- **CLI Flag:** `--allow-env <VAR[,VAR...]>` supported on `grab run` and `grab serve` (repeatable and comma-separated).
- **Name Validation:** Every allowlisted name must match `^[A-Za-z_][A-Za-z0-9_]*$` (`1..128` chars); empty segments, wildcards (`*`), `=`, spaces, or malformed tokens return CLI usage exit code `2`.
- **Functions:**
  - `env.get(name)` — returns `text` if present, or `nothing` if explicitly allowlisted and unset.
  - `env.has(name)` — contextual member call (`env.has` alongside `map has`) returning `boolean` (`yes`/`no`).
- **Default-Deny Security:** Zero grants allow zero variables. Both `env.get` and `env.has` fail closed with `PermissionDenied` (`GRAB-E401`) on any ungranted variable name (case-sensitive match), preventing host environment probing and never exposing environment values in diagnostics or logs.
- **Propagation:** Grants propagate to `do together` task groups, VM callbacks, and child worker processes via `restrictedWorkerEnv`.

---

## 4. Opt-In Strict Script CSP (`--allow-scripts`, `ADR-028c`)

- **Default Policy (`Config.AllowScripts == false`):**
  `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'`
- **Opt-In Policy (`grab serve --allow-scripts`):**
  `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'`
- Applies uniformly to HTML routes, static assets (`--static`), custom responses, and error responses, overwriting any weaker script directives without permitting `unsafe-eval`, inline scripts, or wildcards (`*`).
- `--allow-scripts` is restricted exclusively to `grab serve`; passing it to `grab run` or `grab check` exits with usage error code `2`.
