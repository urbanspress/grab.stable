# SQLite product catalog

The database path in `grab.data.json` is relative to that manifest. On the
Windows/amd64 or Linux/amd64 disk adapter, run from the repository root with
Go 1.26 or newer:

```powershell
go run ./cmd/grab migrate --db-config examples/grab.data.json --allow-db shop:migrate examples/catalog_migrate.grab
go run ./cmd/grab run --db-config examples/grab.data.json --allow-db shop:read-write examples/catalog_seed.grab
go run ./cmd/grab serve --db-config examples/grab.data.json --allow-db shop:read examples/catalog_app.grab
```

Open http://127.0.0.1:8080/ . Migrations are an explicit maintenance step;
requests and reloads never run them. Repeating the migration or seed is safe.
`catalog.db` is local example data; it is not committed.

Prices use SQLite TEXT storage and explicit decimal contracts, preserving exact
cents. Each visitor gets separate bindings and rows; the prepared pool and query
handles belong to the server generation. Product names are escaped by templates.

This is a read-only browsing example, not a checkout or payment system.
