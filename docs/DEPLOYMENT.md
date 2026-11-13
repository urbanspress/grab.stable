# GRAB Production Deployment & Reverse-Proxy Architecture

This reference summarizes GRAB's standard local development and production hosting architecture for engineers and AI agents. See [`docs/features/deployment_architecture.html`](features/deployment_architecture.html) for the visual guide.

## Status Boundaries

- **Available now (GRAB 0.6 – 0.8):** Persistent `PreparedApp` server, loopback default (`127.0.0.1:8080`), private proxy listeners (`--addr 127.0.0.1:8765`), explicit `--trusted-proxy` allowlists, `--allow-public` opt-in, opt-in `--static`, `/_health` and `/_ready` probes, `--log-format json`, `--diagnostics production`, graceful drain, SQLite/MariaDB/PostgreSQL via `--db-config` and `--allow-db`, supervised multi-process workers (`--workers N --worker-base-port P`), strict `--platform-config`, Redis caching/rate limiting (`use cache`), authorized WebSocket channels (`use sockets`), and production Docker/Caddy stack in `deploy/`.
- **Planned (GRAB 0.9 – 1.0 & GrabLab):** Profile-driven IR/bytecode VM (0.9), and GrabLab automated application packaging.

## Core Architectural Rules (`ADR-005` & `ADR-017`)

1. **Private loopbacks behind an edge proxy (`ADR-005`):**
   - Never expose GRAB worker ports directly to the public internet in production.
   - Terminate TLS (`80`/`443`), enforce edge rate/body shields, and offload static media at the reverse-proxy edge, forwarding clean HTTP/WebSocket traffic to GRAB on private loopback/internal ports (`127.0.0.1:8765` single-worker today; `127.0.0.1:8765–8772` multi-worker in GRAB 0.8).
   - Always pass explicit `--trusted-proxy` IP/CIDR allowlists so `X-Forwarded-*` headers from untrusted clients cannot be spoofed.
2. **Automatic SSL/TLS Termination at the Edge (`ADR-005` & `ADR-017`):**
   - The core GRAB runtime (`grab serve`) never runs as `root` and never manages SSL certificates directly.
   - Instead, automatic zero-config HTTPS (Let's Encrypt / ZeroSSL and on-demand custom-domain TLS) is handled at the reverse-proxy edge using one of three supported patterns, forwarding clean HTTP/WebSocket traffic to GRAB's private worker ports (`127.0.0.1:8765–8772`):
     - **Caddy (Recommended):** Automatic in-memory Let's Encrypt / ZeroSSL issuance, zero-cron renewal, and on-demand TLS for custom user domains out of the box.
     - **Nginx + Certbot:** Classic battle-tested edge proxy paired with automated ACME certificate renewal and fine-grained static asset/rate-limit tuning.
     - **Cloudflare Tunnel (`cloudflared`):** Outbound-only encrypted tunnel with edge TLS managed by Cloudflare, requiring zero open inbound ports on the host.
3. **Server/browser security separation (`ADR-017`):**
   - Keep host manifests (`--db-config`, proposed `--platform-config`), environment secrets, and raw stack traces strictly server-side (`--diagnostics production` in live deployments).
   - Rely on default-deny capability flags (`--allow-db`, `--allow-read`, `--allow-write`, `--allow-net`, and proposed GRAB 0.8 grants).
4. **Hardened non-root container execution (Production Gold Standard):**
   - Run GRAB (**Grisp**) inside a non-root container (`read_only: true`, `cap_drop: ["ALL"]`, minimal writable mounts only where required) on a private backend network with Redis (0.8) and PostgreSQL/MariaDB (0.7).
5. **Zero-cold-start performance & rolling reloads:**
   - GRAB lexes, parses, validates routes/templates, and opens bounded DB pools once per generation (`PreparedApp`)—no per-request bootstrap like PHP-FPM/FastCGI.
   - Use `/_health` (liveness) and `/_ready` (readiness, returning `503` while draining) to coordinate zero-downtime / rolling proxy upstream updates.

## Two-Stage Deployment Path

### Stage 1 — Local Development on Windows (`127.0.0.1:8080`, Live Today)

```powershell
# Single-file or multi-route dev preview with auto-reload:
go run ./cmd/grab serve --reload examples/server_app.grab

# Database-backed catalog preview:
go run ./cmd/grab serve --db-config examples/grab.data.json --allow-db shop:read examples/catalog_app.grab

# Local Nginx reverse-proxy mode (single worker today):
go run ./cmd/grab serve --addr 127.0.0.1:8765 --trusted-proxy 127.0.0.1,::1 --log-format json --diagnostics production examples/server_app.grab
```

### Stage 2 — Production Container Stack (GRAB 1.0 & GrabLab Target)

```text
Public Internet (80/443)
  -> Edge Reverse Proxy: Caddy (recommended zero-cron HTTPS), Nginx + Certbot, or Cloudflare Tunnel
       (Let's Encrypt / ZeroSSL + on-demand custom-domain TLS -> forwards clean HTTP/WebSocket traffic)
  -> Hardened Non-Root Container (non-root UID, read-only rootfs, dropped capabilities)
  -> Grisp / GRAB Private Worker Ports (127.0.0.1:8765-8772 via GRAB 0.8 --workers; never root, never manages certs)
  -> Private Redis (0.8) + PostgreSQL / MariaDB (0.7)
```

Do not modify production infrastructure or implement unapproved GRAB 0.8/1.0 flags until their specifications are explicitly approved by the project owner.
