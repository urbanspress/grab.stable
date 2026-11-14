# GRAB Programming Language & Grisp Application Platform

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/go-%3E%3D%201.26-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="https://github.com/urbanspress/grab.stable/releases"><img src="https://img.shields.io/badge/release-v1.1.0-165caa?style=flat&logo=github" alt="Latest Release"></a>
  <a href="tests/"><img src="https://img.shields.io/badge/tests-100%25%20passing-success?style=flat&logo=checkmarx" alt="Conformance Tests"></a>
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/security-default--deny%20capabilities-006e67?style=flat" alt="Security Model"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg?style=flat" alt="License: MIT"></a>
</p>

---

## Table of Contents

- [Overview](#overview)
- [GRAB & Grisp Runtime](#grab--grisp-runtime)
- [Architecture & Key Highlights](#architecture--key-highlights)
- [Quick Start](#quick-start)
- [CLI Command Reference](#cli-command-reference)
- [Language Tour & Examples](#language-tour--examples)
- [Security & Capability Model](#security--capability-model)
- [Production Deployment](#production-deployment)
- [Milestone Feature Guides](#milestone-feature-guides)
- [Documentation Index](#documentation-index)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

**GRAB** is a fast, deterministic, security-first general-purpose programming language implemented in Go. It blends the clean readability of structured English syntax with high-performance systems engineering, exact arithmetic precision, and strict capability-based security.

GRAB is engineered from first principles to power modern, connected web applications that require rock-solid reliability, zero floating-point rounding errors for currency, and unyielding protection against web vulnerabilities.

---

## GRAB & Grisp Runtime

- **The GRAB Language:** Provides an intuitive, expressive grammar with native support for structured error handling (`try`/`catch`/`raise`), task-group concurrency (`do together`/`wait for`), lossless JSON parsing, unified record/map collection lookups, and exact numerical types.
- **Grisp Application Server:** The persistent application engine powering `grab serve`. Unlike traditional scripting runtimes (such as PHP-FPM or CGI) that re-parse scripts and open fresh database connections on every incoming request, Grisp pre-compiles AST and `.grab.html` templates once at startup (`PreparedApp`). It keeps connection pools warm, isolates execution frames per visitor, sheds load under pressure (503 backpressure), and enables zero-downtime atomic application reloads.

---

## Architecture & Key Highlights

| Highlight | Description |
| :--- | :--- |
| ⚡ **Persistent Server (`PreparedApp`)** | Load-once compiled AST and pre-parsed HTML templates with fresh per-request variable isolation and zero cold starts. |
| 🔢 **Lossless Numeric Precision** | Native arbitrary-precision `integer` and exact `decimal` types prevent currency and ledger calculation errors. |
| 🔒 **Default-Deny Capabilities** | Zero ambient authority. Access to filesystem, outbound HTTP, databases, caches, channels, and metrics requires explicit grants. |
| 🗄️ **Unified Database Engine (`use data`)** | High-performance drivers for pure-Go SQLite, MariaDB, and PostgreSQL with mandatory literal prepared statements. |
| 🚀 **Scale-Out Scaling (`use cache`, `use sockets`)** | Standalone Redis caching, atomic fixed-window rate limits, WebSocket channels, and multi-worker loopback supervision. |
| 🤝 **Typed Contracts & Client Generation** | Runtime-validated JSON API schemas with deterministic TypeScript and React client generator (`grab generate client`). |
| 🛡️ **Zero-XSS Templating (`.grab.html`)** | Context-aware HTML lexer automatically escapes output based on injection placement (body, attribute, or URL). |
| 🌐 **Built-in SSRF Shield** | Outbound HTTP client automatically blocks loopback, private RFC 1918 subnets, and cloud instance metadata addresses. |

---

## Quick Start

### 1. Prerequisites
- [Go 1.26 or newer](https://go.dev/dl/) installed and available on your system `PATH`.
- Git.

### 2. Build the Executable
```sh
# Clone the stable repository (or grab.progress.git for active development)
git clone https://github.com/urbanspress/grab.stable.git
cd grab.stable

# Build the CLI
go build ./cmd/grab
```

### 3. Run Your First Program
```sh
# Run the greeting script
go run ./cmd/grab run index.grab
```

### 4. Launch the Persistent Web Server
```sh
# Start the web preview server on http://127.0.0.1:8080
go run ./cmd/grab serve examples/landing.grab
```
Open [http://127.0.0.1:8080](http://127.0.0.1:8080) in your browser to view the live landing page demo.

---

## CLI Command Reference

The unified `grab` executable provides all runtime, inspection, and operations tooling:

```text
Usage:
  grab <command> [flags] [arguments]

Core Commands:
  run       Execute a GRAB program (.grab) directly
  serve     Run a persistent multi-route application server (Grisp engine)
  check     Validate program syntax, imports, and routes offline
  format    Format GRAB source code canonically (--write to apply)
  migrate   Run database schema migrations against configured databases
  events    Inspect and maintain durable transactional inbox/outbox event queues
  generate  Generate typed client libraries from API contract schemas
  help      Show help for grab or any subcommand
```

### Common CLI Examples

```sh
# 1. Syntax check before deployment
grab check examples/server_app.grab

# 2. Canonical formatting
grab format --write examples/landing.grab

# 3. Persistent server with live auto-reload
grab serve --reload examples/landing.grab

# 4. Multi-worker scaling behind reverse proxy
grab serve --workers 4 --worker-base-port 8765 \
  --platform-config examples/scaling.platform.json \
  --trusted-proxy 127.0.0.1,10.0.0.0/8 \
  --log-format json --diagnostics production \
  examples/realtime_app.grab

# 5. Database schema migration
grab migrate --db-config examples/grab.data.json --allow-db catalog:migrate up

# 6. Generate typed TypeScript/React client from API contract
grab generate client --contract examples/realtime.api.json --out client/src/api
```

---

## Language Tour & Examples

### Persistent Web Service with Exact Decimals
```grab
use server
use math

set store_title to "Acme Marketplace"
set tax_rate to 0.0825

route "GET" "/" using req
  give back server.html using "landing.grab.html", record {
    title: store_title,
    client_ip: req.client_ip,
    request_id: req.request_id
  }
end

route "GET" "/api/quote/{amount}" using req
  set base_amount to req.params.amount
  set tax to math.round using (base_amount times tax_rate), 2
  set total to base_amount plus tax
  
  give back server.json using 200, record {
    store: store_title,
    subtotal: base_amount,
    tax: tax,
    total: total
  }
end
```

### Safe Database Access & Scoped Transactions
```grab
use data

# Open database using authorized connection grant
set db to data.open using "production_db"

# Mandatory literal prepared statement (immune to SQL injection)
set find_user to data.prepare using db, "SELECT id, email, balance FROM users WHERE id = ?"
set user to data.one using find_user, [42]

# Scoped transaction
set tx to data.begin using db
try
  set deduct to data.prepare using tx, "UPDATE accounts SET balance = balance - ? WHERE id = ?"
  call data.execute using deduct, [50.00, 42]
  call data.commit using tx
catch err
  call data.rollback using tx
  show "Transaction failed safely: " joined with err.message
end
```

---

## Security & Capability Model

GRAB implements a strict **default-deny capability architecture**. Applications have zero ambient access to the host machine:

```
┌────────────────────────────────────────────────────────┐
│                   GRAB Application                     │
└──────────────────────────┬─────────────────────────────┘
                           │ Requires Explicit Capability
                           ▼
  [--allow-read]        [--allow-write]       [--allow-net]
   Filesystem Read       Filesystem Write      Outbound HTTP
   (Blocks .env/.git)    (Confined paths)      (Blocks SSRF)
  
  [--allow-db]          [--allow-cache]       [--allow-channel]
   Database Grants       Redis / Memory        WebSocket Channels
   (Prepared SQL only)   (Key TTL & Quotas)    (Origin & Contracts)
```

Read our full [Security Policy & Vulnerability Reporting Guide](SECURITY.md).

---

## Production Deployment

GRAB persistent server (**Grisp**) is designed to run behind a modern edge reverse proxy such as **Caddy** (recommended) or **Nginx**:

- **[deploy/](deploy/)**: Complete production Docker stack.
  - **[deploy/Dockerfile](deploy/Dockerfile)**: Multi-stage, statically compiled Go container running as unprivileged `grab` user.
  - **[deploy/Caddyfile](deploy/Caddyfile)**: Automatic HTTPS (Let's Encrypt / ZeroSSL), HTTP/3, and reverse proxying with client IP forwarding.
  - **[deploy/docker-compose.yml](deploy/docker-compose.yml)**: Single command (`docker compose up -d`) production orchestration.
- **[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)**: Architectural reference and multi-stage VPS hosting guide.

---

## Milestone Feature Guides

Explore our plain-English visual documentation hubs:

- **[Milestone Feature Guides Hub](docs/features/index.html)** — Comprehensive visual roadmap of all completed and active releases.
- **[GRAB 0.9 Visual Guide](docs/features/grab_09_features.html)** — Profile-driven IR and Bytecode VM performance breakdown.
- **[Security & Protection FAQ](docs/features/security_faq.html)** — Plain-English security architecture, DevTools protection, CSP, and printable PDF.
- **[GRAB 0.8 Visual Guide](docs/features/grab_08_features.html)** — Integrations, typed React/TS clients, Redis cache, durable webhooks, and worker scaling.
- **[GRAB 0.7 Visual Guide](docs/features/grab_07_features.html)** — Database access (SQLite, MariaDB, PostgreSQL), prepared queries, and migrations.
- **[GRAB 0.6 Visual Guide](docs/features/grab_06_features.html)** — Persistent multi-route application server (Grisp engine).
- **[GRAB 0.5 Visual Guide](docs/features/grab_05_features.html)** — Universal HTTP client, lossless JSON, and task groups.
- **[GRAB 0.4 Visual Guide](docs/features/grab_04_features.html)** — Standard library, error model, and capability permissions.
- **[Deployment Architecture](docs/features/deployment_architecture.html)** — Traffic flow, proxy topologies, and comparison against PHP-FPM.

---

## Documentation Index

- **[ROADMAP.md](docs/ROADMAP.md):** Complete project roadmap from GRAB 0.1 through 1.0 and Post-1.0.
- **[SECURITY-FAQ.md](docs/SECURITY-FAQ.md):** Built-in security architecture, DevTools isolation, and defense-in-depth protections FAQ.
- **[DATA.md](docs/DATA.md) & [DATA-TESTING.md](docs/DATA-TESTING.md):** Database engine support and real-service verification.
- **[CACHE.md](docs/CACHE.md):** Redis and in-memory cache limits, codec, and rate limiting.
- **[API.md](docs/API.md):** Typed JSON contracts and deterministic TypeScript/React generator.
- **[INTEGRATIONS.md](docs/INTEGRATIONS.md):** Verified connectors and durable transactional event queues.
- **[REALTIME-SCALING.md](docs/REALTIME-SCALING.md) & [SCALE-TESTING.md](docs/SCALE-TESTING.md):** Multi-process supervision, WebSocket channels, and scaling benchmarks.

---

## Contributing

We welcome community participation! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details on our architectural principles, development environment setup, and the mandatory 100% test passing gate.

---

## License

GRAB is open-source software licensed under the [MIT License](LICENSE).
Third-party dependency license notices are documented in [docs/licenses/README.md](docs/licenses/README.md).
