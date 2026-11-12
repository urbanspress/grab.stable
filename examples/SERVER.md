# GRAB 0.6 example application

From the repository root, start the sample:

```sh
go run ./cmd/grab check examples/server_app.grab
go run ./cmd/grab serve --reload examples/server_app.grab
```

Open http://127.0.0.1:8080/ for HTML, `/api/products/A-17` for exact-number
JSON, `/start` for a redirect, or use the page's form to submit a name.
POST JSON such as `{"sku":"A-17"}` to `/api/orders` to receive a 201 response.
The reserved probes are `/_health` and `/_ready`.

Each visitor gets fresh variable bindings. Settings outside routes run once
at startup. Template paths are relative to the program's directory. Literal
templates are checked before the server opens its port. An invalid reload
keeps the previous working app running; fix the file to try again.

Behind a local Nginx proxy, use an explicit trust list:

```sh
go run ./cmd/grab serve --addr 127.0.0.1:8765 --trusted-proxy 127.0.0.1,::1 --log-format json --diagnostics production examples/server_app.grab
```

Keep the listener private when Nginx is the public entry point. Direct public
binding requires `--allow-public`; terminate TLS at a reverse proxy. Static
files are disabled unless `--static public` or `--static /assets/=public` is
supplied. Source changes are watched with `--reload`; Unix also supports
SIGHUP. On Windows use `--reload`. Stop with Ctrl+C for a graceful drain.

Limits: `--max-concurrency 64`, `--max-queue 128`, `--queue-timeout 1s`,
`--request-timeout 10s`, `--max-body-bytes 1048576`, and
`--shutdown-timeout 10s`. Queue overload returns 503, execution timeout 504,
oversized bodies 413, and invalid UTF-8 bodies 400.

Run the repeatable performance harness with:

```sh
go test ./internal/server -run '^$' -bench . -benchmem
```
