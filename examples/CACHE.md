# Temporary catalog cache (GRAB 0.8 work in progress)

From the repository root:

```powershell
go run ./cmd/grab check --platform-config examples/grab.platform.json examples/cache_app.grab
go run ./cmd/grab serve --platform-config examples/grab.platform.json --allow-cache catalog:read-write --allow-cache catalog:limit examples/cache_app.grab
```

Open `http://127.0.0.1:8080/catalog`. The response preserves the demonstration
price as text. Three requests are allowed per ten-second window, then HTTP 429
includes a rounded-up Retry-After header. Store failures return 503. The bucket
is a fixed, shared demonstration bucket, not an authenticated per-user limit.
An application must derive real buckets from authenticated identity or trusted
server context, never an arbitrary request header.

The startup price expires after one minute; restart to seed it again. This
example intentionally uses temporary memory and has no authoritative database.
Restart/reload resets memory values and limits. Redis shares temporary state
across processes but is still not a durable ledger. See [CACHE.md](../docs/CACHE.md)
for the standalone Redis configuration and current implementation boundaries.

Other GRAB 0.8 integrations and release acceptance remain pending. This example
does not demonstrate typed clients, durable work, channels or worker supervision.
