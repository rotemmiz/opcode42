# Forge Kotlin SDK

Typed Kotlin client for the Forge / opencode wire contract (plan 06). Generated
from the frozen contract `conformance/openapi-reference.json` with
[openapi-generator](https://openapi-generator.tech) (`kotlin`, `jvm-okhttp4`
library, `kotlinx.serialization`), pinned for deterministic output.

## Layout

- `gen/` — **generated**: request/response models (`dev.forge.sdk.models`), typed
  API classes (`dev.forge.sdk.apis`), and OkHttp infrastructure
  (`dev.forge.sdk.infrastructure`). **Never edit by hand** — regenerate.
- `src/` — **hand-written**: [`ForgeClient`](src/main/kotlin/dev/forge/sdk/ForgeClient.kt),
  a thin wrapper that injects Basic auth + the `X-Opencode-Directory` routing
  header into every request (codegen does not cover these cross-cutting concerns).
- `build.gradle.kts` — a Kotlin/JVM library module compiling `gen/` + `src/`.

Persistent streaming (SSE `/event`, WebSocket `/pty/{id}/connect`) is **not**
generated — codegen cannot model long-lived connections. The Android app ships
hand-written SSE/PTY clients in its `core:network` / `core:sdk` modules (plan 07);
the Go SDK has them in `sdk/go/{sse,pty}.go`.

## Regenerating

```sh
make gen-sdks          # regenerates sdk/kotlin/gen and sdk/swift/gen
# or: scripts/gen-sdks.sh
```

The committed output is pinned to the spec; CI fails on any drift
(`scripts/check-sdk-fresh.sh`). Requires a JVM (>= 11) and Go (for the 3.1→3.0
downconvert step the generator consumes).

## Usage

```kotlin
val forge = ForgeClient("http://localhost:4096", directory = "/work/proj")
val sessions = forge.sessions().sessionList()
```
