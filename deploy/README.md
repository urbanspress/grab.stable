# GRAB Production Deployment Starter

This directory contains a production-ready deployment stack for the GRAB persistent application server behind Caddy reverse proxy with automated SSL/TLS certificates and HTTP/3 support.

## Architecture

```
Internet (Port 80/443)
       │
       ▼
┌──────────────┐
│    Caddy     │  (Automatic Let's Encrypt / ZeroSSL, HTTP/3, gzip/zstd)
└──────┬───────┘
       │ internal docker network (bridge)
       ▼
┌──────────────┐
│  GRAB Server │  (grab serve --allow-public --trusted-proxy ...)
└──────────────┘
```

## Quick Start (Local Docker Test)

From the `deploy/` directory:

```sh
# Build and start the containers in the background
docker compose up -d --build

# Inspect running containers
docker compose ps

# View live application logs
docker compose logs -f
```

Open `http://localhost/` in your browser to view the live landing page, or run:

```sh
curl http://localhost/api/info
curl http://localhost/_health
```

To stop:

```sh
docker compose down
```

---

## Live VPS Deployment (Hetzner, DigitalOcean, Linode, AWS, etc.)

### 1. DNS Setup
Point your domain's **A** (and optionally **AAAA**) record to your VPS public IPv4/IPv6 address:
- `example.com` &rarr; `YOUR_VPS_IP`
- `www.example.com` &rarr; `YOUR_VPS_IP`

### 2. Configure Domain in Caddyfile
Open `deploy/Caddyfile` and replace `:80` with your actual domain name:

```caddyfile
example.com {
    encode zstd gzip

    reverse_proxy grab:8080 {
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}

        health_uri /_health
        health_interval 10s
        health_timeout 2s
    }

    log {
        output stdout
        format json
    }
}
```

*Caddy automatically requests, installs, and renews trusted SSL/TLS certificates from Let's Encrypt / ZeroSSL.*

### 3. Deploy
On your VPS:

```sh
git clone https://github.com/urbanspress/grab.git
cd grab/deploy
docker compose up -d --build
```

### 4. Updating Application
When you push updates to your `.grab` files:

```sh
cd grab
git pull
docker compose build grab
docker compose up -d grab
```
