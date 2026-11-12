# Run authorized notifications locally

From the repository root, build once:

```powershell
go build -o grab.exe ./cmd/grab
./grab.exe check --platform-config examples/realtime.platform.json --api-contract examples/realtime.api.json examples/realtime_app.grab
./grab.exe serve --platform-config examples/realtime.platform.json --api-contract examples/realtime.api.json --allow-channel updates:connect --allow-channel updates:publish examples/realtime_app.grab
```

The sample accepts native WebSocket connections to `ws://127.0.0.1:8080/live`
with `Origin: http://127.0.0.1:8080` and
`Authorization: Bearer local-demo`. Send
`{"audience":"demo","value":"refresh"}` and receive an authorized echo.
An authenticated `POST /notify` sends a change notice; `GET /` represents the
authoritative HTTP state clients refetch. The automated conformance test runs this
sequence on actual network connections.

These are visible local demo credentials, not a production authentication system.
Replace both authorization callbacks with your session and tenant policy before
deployment. A browser client needs secure cookie/session authentication because
its WebSocket constructor cannot supply an Authorization header.

For two workers, first provide a disposable standalone Redis on loopback port
16388 (or edit the host manifest for your approved service), then:

```powershell
./grab.exe serve --workers 2 --worker-base-port 8765 --reload --platform-config examples/scaling.platform.json --api-contract examples/realtime.api.json --allow-channel updates:connect --allow-channel updates:publish examples/realtime_app.grab
```

The workers listen at `127.0.0.1:8765` and `127.0.0.1:8766`. Redis joins their
notifications. Use an external proxy for a single browser-facing address. This
example explicitly enables plaintext local development; production requires TLS,
trusted proxies, secure origins, and separate Redis ACL credentials. Pool budget:
two workers × (four command connections + one subscription) + five headroom = 15,
within the configured maximum of 32.

Read [the operator guide](../docs/REALTIME-SCALING.md) for authorization, limits,
telemetry, restart/drain behavior, and recovery boundaries.
