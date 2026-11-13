# Security Policy

## Security Philosophy

GRAB is built with a **default-deny, capability-based security model**. Unlike traditional scripting languages that grant ambient authority to the host system, GRAB applications cannot access the filesystem, outbound network, databases, caches, realtime channels, or telemetry collectors unless explicitly authorized by command-line capability flags or strict platform configuration manifests.

Furthermore, GRAB eliminates entire classes of common web vulnerabilities at the language and architecture level:
- **No SQL Injection:** Database queries (`use data`) require fixed literal SQL and parameter binding; dynamic string concatenation in queries fails closed at parse/pre-execution time.
- **No Cross-Site Scripting (XSS):** Web templates (`.grab.html`) parse lexical HTML contexts and automatically escape interpolated values according to their placement (HTML body, attributes, or URLs).
- **No SSRF to Internal Infrastructure:** Outbound HTTP requests (`use http`) automatically block access to loopback (`127.0.0.1`), RFC 1918 private subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), and cloud instance metadata endpoints (`169.254.169.254`).
- **No Path Traversal or Secret Leakage:** Filesystem operations (`use files`) enforce path confinement and unconditionally block access to sensitive directories including `.git`, `.env`, `.ssh`, and private cryptographic key files (`GRAB-E401`).
- **Per-Request Memory Isolation:** In the persistent web server (**Grisp** engine), requests run in isolated call frames with fresh variable bindings. Global mutable state between concurrent visitors is disallowed.

---

## Supported Versions

Security updates are applied to the active release stream:

| Version | Supported | Notes |
| :--- | :---: | :--- |
| `0.8.x` | :white_check_mark: | Current active release (scaling, channels, cache) |
| `0.7.x` | :white_check_mark: | Database access milestone |
| `0.6.x` | :white_check_mark: | Persistent application server milestone |
| `< 0.6` | :x: | Historical development releases |

---

## Capability Security Matrix

| Subsystem | Flag / Grant | Default | Security Guarantees |
| :--- | :--- | :---: | :--- |
| **Filesystem Read** | `--allow-read <path>` | Denied | Path containment; sensitive files (`.env`, `.git`, `.ssh`) blocked (`GRAB-E401`). |
| **Filesystem Write** | `--allow-write <path>` | Denied | Confined writing; sensitive files blocked. |
| **Outbound HTTP** | `--allow-net <host>` | Denied | Strict host allowlist; automatic SSRF filtering (`GRAB-E402`); TLS 1.2+ verification. |
| **Database Access** | `--allow-db <name>:<mode>` | Denied | Modes: `read`, `read-write`, `migrate`. Prepared literal SQL only; connection pooling. |
| **Cache & Limits** | `--allow-cache <name>:<mode>` | Denied | Modes: `read`, `write`, `read-write`. Namespace boundaries; key expiration; memory quotas. |
| **Realtime Channels** | `--allow-channel <name>:<mode>` | Denied | Modes: `connect`, `publish`. Origin verification; typed JSON contracts; rate limits. |
| **Telemetry** | `--allow-telemetry <sink>` | Denied | OpenMetrics and OTLP. Mandatory sanitization; secrets and query values are never exported. |

---

## Reporting a Vulnerability

We take the security of GRAB, its runtime, and applications built upon it seriously. If you discover a vulnerability, please do **NOT** open a public issue.

### Preferred Method
Please use [GitHub Private Vulnerability Reporting](https://github.com/urbanspress/grab/security/advisories/new) on the repository.

### Alternative Method
If Private Vulnerability Reporting is unavailable, send details to:
- **Email:** `security@urbanspress.com` (or project maintainers)
- **Subject:** `[GRAB Security Disclosure] <Component or Description>`

Please include:
1. Description of the vulnerability and its potential impact.
2. Minimal reproduction code (`.grab` script, template, or CLI command).
3. Any proposed mitigations or patches.

### Response Timeline
- **Initial Acknowledgement:** Within 48 hours.
- **Assessment & Status Update:** Within 5 business days.
- **Resolution & Release:** Coordinated disclosure once a patch is verified and release notes are prepared.
