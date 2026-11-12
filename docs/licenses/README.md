# GRAB 0.8 dependency notices

Pinned cache and WebSocket dependencies:

| Module | Version | License notice |
| --- | --- | --- |
| `github.com/redis/go-redis/v9` | `v9.23.0` | [BSD 2-Clause](go-redis-BSD-2-Clause.txt) |
| `github.com/cespare/xxhash/v2` | `v2.3.0` | [MIT](xxhash-MIT.txt) |
| `go.uber.org/atomic` | `v1.12.0` | [MIT](atomic-MIT.txt) |
| `github.com/coder/websocket` | `v1.8.14` | [ISC](coder-websocket-ISC.txt) |

Notices were copied from the downloaded pinned modules. `go.sum` records module
checksums. This lists the new cache dependencies, not every existing GRAB
dependency or a Redis server redistribution license.

The host adapter uses a standalone client with command retries, pipeline pools,
topology/maintenance notifications and identity reporting disabled. Credentials
come from the authorized configuration generation. Source/options and licenses
were inspected; the complete dependency security/release review remains a
milestone-008 acceptance requirement.

Primary references: [go-redis releases](https://github.com/redis/go-redis/releases),
[Redis Go client documentation](https://redis.io/docs/latest/develop/clients/go/).
