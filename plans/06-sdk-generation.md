# Plan 06 — SDK Generation

> Generating client SDKs from the wire contract for Opcode42's own clients.

---

## Context

Opcode42 needs three client SDKs:

1. **Go SDK** — for the Go TUI (plan 08) and integration tests.
2. **Kotlin SDK** — for the Android mobile client (plan 07, primary deliverable).
3. **Swift SDK** — for a future iOS client (plan 07 stretch goal).

The strategy is to derive all three from a single OpenAPI contract source, then layer
**hand-written SSE and WebSocket-PTY clients** on top — because codegen cannot handle
persistent streaming connections. Auth and directory header injection are cross-cutting
concerns built into each generated client wrapper.

---

## opencode References Validated

All citations are from the reference source at `/Users/rotemmiz/git/opencode`.

### SDK Generation Pipeline

**File:** `packages/sdk/js/script/build.ts`

- **Toolchain** (lines 10–41): uses `@hey-api/openapi-ts` (version `0.90.10` per
  `packages/sdk/js/package.json:24`).
- **Contract source** (line 14): `bun dev generate > ${dir}/openapi.json` — the opencode
  daemon emits its spec at runtime; the output is committed as `packages/sdk/openapi.json`.
- **v2 output** (lines 18–20): generated into `src/v2/gen` with `@hey-api/typescript`,
  `@hey-api/sdk`, and `@hey-api/client-fetch` plugins.
- **SSE codegen bug patch** (lines 43–59): `@hey-api/openapi-ts` incorrectly passes the
  endpoint's `TError` into the second generic of `ServerSentEventsResult`. The build script
  post-processes `src/v2/gen/client/types.gen.ts` to drop the second generic arg so
  `TReturn` defaults to `void`. This is a known upstream defect that Opcode42 must work around
  in its own codegen pipeline.
- **v1 output** (implicit `src/gen`): legacy generated path kept for backward compat;
  Opcode42 pins to v2 only.

**File:** `packages/sdk/js/src/v2/client.ts`

- **`createOpencodeClient`** (lines 47–90): wraps the `@hey-api` generated client with:
  - `x-opencode-directory: encodeURIComponent(directory)` header injection (lines 62–65).
  - `x-opencode-workspace` header injection (lines 67–70).
  - Disabled timeout via custom `fetch` (lines 49–54: `req.timeout = false`).
  - Request interceptor: for GET/HEAD, rewrites `x-opencode-directory` / `x-opencode-workspace`
    headers into `directory` / `workspace` query params (lines 17–45).
  - Response interceptor: rejects `text/html` responses (lines 81–87) — server version mismatch guard.
  - Error interceptor: wraps errors via `wrapClientError` (line 88).

**File:** `packages/sdk/js/src/v2/server.ts`

- **`createOpencodeServer`** (lines 22–100): spawns `opencode serve --hostname --port` as a child
  process, polls stdout for `"opencode server listening on <url>"` (lines 55–61), resolves with
  the URL. Timeout: configurable, default 5000ms (lines 29–30).
- **Relevance to Opcode42:** Opcode42's own `createOpcode42Server` wrapper will mirror this pattern —
  spawning `opcode42 serve` and polling for the same ready line. Wire-compat means the same
  SDK server bootstrap works for both daemons.

**File:** `packages/sdk/openapi.json`

- 131 `operationId` entries covering all endpoint families listed in plan 00.
- The SSE endpoints (`/event`, `/global/event`) appear with `x-sse` or equivalent annotations
  that trigger the `@hey-api/client-fetch` SSE codegen path (`client.sse.get` in the generated SDK,
  e.g. `packages/sdk/js/src/v2/gen/sdk.gen.ts:553,625`).
- PTY WebSocket (`/pty/{ptyID}/connect`) is present as a regular POST/GET endpoint in the spec
  but the actual persistent WS connection is **not representable in OpenAPI 3.1** — it is
  hand-written (see below).
- `x-opencode-directory` and `x-opencode-workspace` routing headers are **not** in the spec as
  security schemes; they are injected by the client wrapper layer.

### Auth

**File:** `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts`

- Basic Auth (line 84): `Authorization: Basic <base64(user:pass)>`
- Auth token query param (line 82): `?auth_token=<base64(user:pass)>` — useful for WebSocket
  connections where headers are restricted by the browser/Android WS client.

---

## Contract Source & Drift Strategy

### Phase 1: Pin to `packages/sdk/openapi.json`

During Phases A–C, `packages/sdk/openapi.json` from the opencode repo is the **frozen
contract**. All three SDKs are generated from this file. It is copied into
`opcode42/sdk/openapi.json` and committed. Changes are made only via deliberate bumps.

### Phase 2: Opcode42 emits its own spec (ties to Plan 12)

Once the Opcode42 daemon implements `GET /openapi.json` (or `bun dev generate` equivalent —
`opcode42 generate` in Go using `github.com/swaggest/rest` or `kin-openapi`), a CI job diffs
the emitted spec against the frozen contract:

```
opcode42 generate > /tmp/opcode42-openapi.json
diff <(jq -S . packages/sdk/openapi.json) <(jq -S . /tmp/opcode42-openapi.json)
```

Any path or schema drift is a CI failure. Opcode42 is not allowed to add or remove operations
without updating the frozen contract and all three SDKs simultaneously.

### Scope of code generation

Code generation covers: request/response types, request functions, and error envelopes.
It does **not** cover:
- SSE streaming (persistent connections, event re-delivery, reconnect backoff).
- PTY WebSocket framing.
- Auth header injection (always hand-written in the wrapper layer).
- Directory / workspace routing header injection.

---

## Per-Language Generation

### Go SDK

**Tool:** `oapi-codegen` (`github.com/deepmap/oapi-codegen` v2, or the maintained fork
`github.com/oapi-codegen/oapi-codegen`).

**Config file:** `opcode42/sdk/go/oapi-codegen.yaml`

```yaml
package: opcode42client
generate:
  chi-server: false
  iris-server: false
  echo-server: false
  fiber-server: false
  models: true
  client: true
  embedded-spec: false
output: opcode42/sdk/go/gen/client.gen.go
output-options:
  skip-prune: false
  nullable-type: true
```

**Wrapper:** `opcode42/sdk/go/client.go` — hand-written wrapper around the generated client:

```go
type Opcode42Client struct {
    inner     *gen.ClientWithResponses
    baseURL   string
    directory string
    auth      string  // "Basic <b64>" or ""
}

func NewOpcode42Client(baseURL, directory, username, password string) *Opcode42Client {
    auth := ""
    if username != "" {
        auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
    }
    inner, _ := gen.NewClientWithResponses(baseURL,
        gen.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
            if auth != "" {
                req.Header.Set("Authorization", auth)
            }
            if directory != "" {
                req.Header.Set("X-Opencode-Directory", url.QueryEscape(directory))
            }
            return nil
        }),
    )
    return &Opcode42Client{inner: inner, baseURL: baseURL, directory: directory, auth: auth}
}
```

The Go SDK is used by:
- The TUI client (plan 08): REST calls for session CRUD, config, etc.
- Integration tests (plan 10): roundtrip tests against both opencode and Opcode42 daemons.
- The conformance harness (plan 12): REST calls for setup/teardown of test scenarios.

### Kotlin SDK

**Tool:** `openapi-generator` CLI (`org.openapitools:openapi-generator-cli`), generator `kotlin`.

```bash
openapi-generator generate \
  -i opcode42/sdk/openapi.json \
  -g kotlin \
  -o opcode42/sdk/kotlin \
  --library jvm-okhttp4 \
  --additional-properties=packageName=dev.opcode42.client,dateLibrary=java8,serializationLibrary=kotlinx_serialization
```

**Why openapi-generator over hey-api for Kotlin:** hey-api is TypeScript-only. openapi-generator
is the de facto standard for JVM/Kotlin; `jvm-okhttp4` + `kotlinx.serialization` is idiomatic
for Android.

**Wrapper:** `opcode42/sdk/kotlin/src/main/kotlin/Opcode42Client.kt`

```kotlin
class Opcode42Client(
    private val baseUrl: String,
    private val directory: String = "",
    private val username: String = "",
    private val password: String = "",
) {
    private val httpClient: OkHttpClient = OkHttpClient.Builder()
        .addInterceptor { chain ->
            val req = chain.request().newBuilder().apply {
                if (username.isNotEmpty()) {
                    val creds = Credentials.basic(username, password)
                    header("Authorization", creds)
                }
                if (directory.isNotEmpty()) {
                    header("X-Opencode-Directory", URLEncoder.encode(directory, "UTF-8"))
                }
            }.build()
            chain.proceed(req)
        }
        .build()

    // SSE and WS-PTY clients are hand-written — see below
    val sse: Opcode42SseClient = Opcode42SseClient(baseUrl, httpClient)
    val pty: Opcode42PtyClient = Opcode42PtyClient(baseUrl, httpClient)
}
```

**Alternative considered:** `@hey-api` has no Kotlin support. KMP (Kotlin Multiplatform)
with `ktor-client` was considered for sharing code with a future Swift SDK via KMP, but the
added complexity is premature — address in plan 07 when Swift is scoped.

### Swift SDK (Future / Phase D+)

**Tool:** `openapi-generator` CLI, generator `swift5`.

```bash
openapi-generator generate \
  -i opcode42/sdk/openapi.json \
  -g swift5 \
  -o opcode42/sdk/swift \
  --additional-properties=projectName=Opcode42Client,responseAs=AsyncAwait
```

Swift SSE and WS-PTY layers are hand-written (same design as Kotlin, see below). Deferred
until plan 07 scopes iOS. For now, a stub `opcode42/sdk/swift/README.md` placeholder suffices.

---

## Hand-Written SSE + WS-PTY Layers

These are the most critical non-generated components. Codegen cannot model persistent
streaming connections.

### SSE Contract

Events have shape `{ id: string, type: string, properties: object }` (from plan 00 / plan 12).
Known event types: `server.connected`, `server.heartbeat`, `session.*`, `message.*`, `part.*`,
`permission.asked`, `question.{asked,replied,rejected}`, `pty.{created,updated,exited,deleted}`,
`lsp.updated`, `project.updated`, `workspace.status`, `global.disposed`, `tui.prompt.append`.

The SSE endpoints are:
- `GET /event?directory=<dir>&workspace=<ws>` — per-directory instance events.
- `GET /global/event` — global events (server lifecycle, config changes).

Both require the same auth headers. Both are long-lived connections.

### Go SSE Client (`opcode42/sdk/go/sse/client.go`)

```go
type SSEClient struct {
    baseURL   string
    headers   http.Header
    reconnect time.Duration  // initial backoff, doubles on failure, max 30s
}

func (c *SSEClient) Subscribe(ctx context.Context, path string, params url.Values) (<-chan Event, <-chan error) {
    events := make(chan Event, 64)
    errs   := make(chan error, 1)
    go func() {
        backoff := c.reconnect
        for {
            if err := c.connect(ctx, path, params, events); err != nil {
                if ctx.Err() != nil { errs <- ctx.Err(); return }
                select {
                case <-time.After(backoff): backoff = min(backoff*2, 30*time.Second)
                case <-ctx.Done(): errs <- ctx.Err(); return
                }
            }
        }
    }()
    return events, errs
}

// connect opens one SSE connection, reads events until EOF or error, returns.
// Decodes "data: <json>\n\n" lines into Event structs.
// Heartbeat timeout: if no event received in 60s, close connection (reconnect loop handles restart).
```

**Heartbeat timeout:** opencode sends `server.heartbeat` every ~30s. If 60s pass with no
event (including heartbeat), the client closes and reconnects. This handles silent server drops.

### Kotlin SSE Client (`opcode42/sdk/kotlin/src/main/kotlin/Opcode42SseClient.kt`)

Uses OkHttp's `EventSource` API (`com.squareup.okhttp3:okhttp-sse`):

```kotlin
class Opcode42SseClient(private val baseUrl: String, private val client: OkHttpClient) {
    fun subscribe(path: String, params: Map<String, String> = emptyMap()): Flow<Opcode42Event> = callbackFlow {
        val url = HttpUrl.parse(baseUrl + path)!!.newBuilder()
            .apply { params.forEach { (k, v) -> addQueryParameter(k, v) } }
            .build()
        val request = Request.Builder().url(url).build()
        val listener = object : EventSourceListener() {
            override fun onEvent(es: EventSource, id: String?, type: String?, data: String) {
                // data is the JSON body; type is the SSE event type field (or use id field)
                val event = Json.decodeFromString<Opcode42Event>(data)
                trySend(event)
            }
            override fun onFailure(es: EventSource, t: Throwable?, response: Response?) {
                // Reconnect handled by RealEventSource internally; or close and restart manually
                if (currentCoroutineContext()[Job]?.isActive == false) close()
            }
        }
        val es = OkHttpClient.newEventSource(request, listener)
        awaitClose { es.cancel() }
    }.retryWithExponentialBackoff(initial = 1.seconds, max = 30.seconds)
}
```

**Note on OkHttp SSE:** `okhttp-sse`'s `RealEventSource` handles reconnect automatically
(following the SSE spec `retry:` field). Opcode42 emits `retry: 3000` on connect; callers
can also wrap in a coroutine retry loop for extra resilience.

### PTY WebSocket Contract

From `packages/opencode/src/pty/index.ts:44`:

```
// WebSocket control frame: 0x00 + UTF-8 JSON.
const meta = (cursor: number) => {
  const json = JSON.stringify({ cursor })
  const bytes = encoder.encode(json)
  const out = new Uint8Array(bytes.length + 1)
  out[0] = 0
  out.set(bytes, 1)
  return out
}
```

**Framing rules:**
- **Control frame:** first byte `0x00`, remaining bytes = UTF-8 JSON (`{ cursor: number }`).
  Sent by the server to update the client's scroll position.
- **Data frame:** all other binary or text frames = raw PTY output (UTF-8 terminal data).
- **Buffer limits** (`packages/opencode/src/pty/index.ts:17–18`): `BUFFER_LIMIT = 2 MB`,
  `BUFFER_CHUNK = 64 KB`.
- **Connect:** `GET /pty/{ptyID}/connect` upgraded to WS.
  Auth: `?auth_token=<b64>` (since WS headers are restricted) or
  short-lived ticket via `POST /pty/{ptyID}/connect-token`.
- **Input:** client sends UTF-8 keystrokes as text frames.
- **Resize:** client sends `PUT /pty/{ptyID}` with `{ size: { rows, cols } }` (REST, not WS).

### Go WS-PTY Client (`opcode42/sdk/go/pty/client.go`)

```go
import "github.com/gorilla/websocket"

type PTYClient struct {
    conn      *websocket.Conn
    Data      chan []byte      // raw terminal output
    Meta      chan PTYMeta     // { cursor }
    done      chan struct{}
}

func Connect(ctx context.Context, baseURL, ptyID, authToken string) (*PTYClient, error) {
    wsURL := strings.Replace(baseURL, "http", "ws", 1) + "/pty/" + ptyID + "/connect"
    if authToken != "" { wsURL += "?auth_token=" + authToken }
    conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
    // ...
    c := &PTYClient{conn: conn, Data: make(chan []byte, 256), Meta: make(chan PTYMeta, 16), done: make(chan struct{})}
    go c.readLoop()
    return c, err
}

func (c *PTYClient) readLoop() {
    for {
        _, msg, err := c.conn.ReadMessage()
        if err != nil { close(c.done); return }
        if len(msg) > 0 && msg[0] == 0x00 {
            var meta PTYMeta
            json.Unmarshal(msg[1:], &meta)
            c.Meta <- meta
        } else {
            c.Data <- msg
        }
    }
}
```

### Kotlin WS-PTY Client (`opcode42/sdk/kotlin/src/main/kotlin/Opcode42PtyClient.kt`)

Uses OkHttp WebSocket:

```kotlin
class Opcode42PtyClient(private val baseUrl: String, private val client: OkHttpClient) {
    fun connect(ptyID: String, authToken: String? = null): PtyConnection {
        val wsUrl = baseUrl.replace("http", "ws") + "/pty/$ptyID/connect" +
            (if (authToken != null) "?auth_token=$authToken" else "")
        val request = Request.Builder().url(wsUrl).build()
        val incoming = Channel<PtyFrame>(256)
        val listener = object : WebSocketListener() {
            override fun onMessage(ws: WebSocket, bytes: ByteString) {
                val frame = if (bytes.size > 0 && bytes[0] == 0x00.toByte()) {
                    val meta = Json.decodeFromString<PtyMeta>(bytes.substring(1).utf8())
                    PtyFrame.Control(meta)
                } else {
                    PtyFrame.Data(bytes.toByteArray())
                }
                incoming.trySend(frame)
            }
            override fun onFailure(ws: WebSocket, t: Throwable, response: Response?) {
                incoming.close(t)
            }
        }
        val ws = client.newWebSocket(request, listener)
        return PtyConnection(ws, incoming)
    }
}

sealed class PtyFrame {
    data class Data(val bytes: ByteArray) : PtyFrame()
    data class Control(val meta: PtyMeta) : PtyFrame()
}

data class PtyMeta(val cursor: Int)
```

---

## Auth & Directory Header Injection

**Unified injection contract** (all three SDKs):

| Header / Param | When | Value |
|----------------|------|-------|
| `Authorization: Basic <b64>` | All requests (if auth configured) | `base64(username:password)` |
| `?auth_token=<b64>` | WS/PTY connections | Same base64 credential |
| `X-Opencode-Directory: <encoded>` | All REST requests | `encodeURIComponent(directory)` |
| `?directory=<encoded>` | GET/HEAD requests (fallback rewrite) | Same as header — see `client.ts:23–44` |
| `X-Opencode-Workspace: <id>` | Optional workspace routing | workspace ID string |

The request-interceptor pattern in `packages/sdk/js/src/v2/client.ts:75–79` (rewriting
directory headers to query params for GET/HEAD) must be replicated in all three SDKs.
Reason: some reverse proxies strip custom headers from GET requests.

**SSE connections:** must include `Authorization` header on the initial SSE request. OkHttp
EventSource and Go's `http.NewRequest` both support this. For cross-origin browser contexts
(not applicable for Go/Kotlin), the `?auth_token=` fallback is used.

**PTY WebSocket:** use `?auth_token=` param (not `Authorization` header) because browser
`WebSocket` constructor does not support custom headers. Alternatively, use the short-lived
ticket flow (`POST /pty/{ptyID}/connect-token`) as described in
`packages/opencode/src/server/routes/instance/httpapi/handlers/pty.ts:96–100`.

---

## Implementation Milestones

| Milestone | Deliverable | Phase |
|-----------|-------------|-------|
| M1 | Commit `opcode42/sdk/openapi.json` (copy from opencode, frozen) | Phase A |
| M2 | Go SDK: `oapi-codegen` config + generated `client.gen.go` + wrapper with auth/directory injection | Phase A |
| M3 | Go SSE client: `subscribe`, reconnect/backoff, heartbeat timeout, event decoding | Phase A |
| M4 | Go WS-PTY client: connect, frame decoding, `Data`/`Meta` channels | Phase A |
| M5 | Kotlin SDK: `openapi-generator` config + generated client + wrapper | Phase A (parallel with M2) |
| M6 | Kotlin SSE client: OkHttp EventSource + coroutine Flow | Phase A |
| M7 | Kotlin WS-PTY client: OkHttp WebSocket + `PtyFrame` decoding | Phase A |
| M8 | CI drift check: `opcode42 generate` vs frozen spec (ties to plan 12) | Phase B |
| M9 | Swift SDK scaffold (openapi-generator `swift5`, stubs only) | Phase D |
| M10 | Opcode42 daemon emits its own `GET /openapi.json`; drift gate goes live | Phase D |

---

## Testing

### Generated Client Round-Trips

Run the same test suite against both opencode and Opcode42 daemons (conformance approach from plan 12):

**Go:**
```go
func TestSessionCRUD(t *testing.T) {
    for _, daemon := range []string{"opencode", "opcode42"} {
        t.Run(daemon, func(t *testing.T) {
            client := opcode42client.New(daemonURL(daemon), "/tmp/test-dir", "", "")
            sess, err := client.Session.Create(ctx, opcode42client.CreateSessionParams{})
            require.NoError(t, err)
            require.NotEmpty(t, sess.ID)
            err = client.Session.Delete(ctx, sess.ID)
            require.NoError(t, err)
        })
    }
}
```

**SSE round-trip:**
```go
func TestSSESubscribe(t *testing.T) {
    events, _ := sseClient.Subscribe(ctx, "/event", url.Values{"directory": {"/tmp/test-dir"}})
    // send a prompt, expect session.* and message.* events
    select {
    case ev := <-events:
        assert.Equal(t, "server.connected", ev.Type)
    case <-time.After(5 * time.Second):
        t.Fatal("no SSE event within 5s")
    }
}
```

**Kotlin (Android instrumented or JVM unit test):**
```kotlin
@Test fun `session list round-trip against opencode`() = runBlocking {
    val client = Opcode42Client(opencodeUrl, "/tmp/test-dir")
    val sessions = client.session.list()
    assertNotNull(sessions)
}
```

**PTY WS round-trip:**
```go
func TestPTYWebSocket(t *testing.T) {
    // Create PTY, connect WS, send "echo hello\n", expect "hello" in Data channel
    pty, _ := client.Pty.Create(ctx, opcode42client.CreatePtyParams{Command: "bash"})
    conn, _ := ptyClient.Connect(ctx, pty.ID, authToken)
    conn.Send([]byte("echo hello\n"))
    select {
    case frame := <-conn.Data:
        assert.Contains(t, string(frame), "hello")
    case <-time.After(5 * time.Second):
        t.Fatal("PTY no output")
    }
}
```

### Drift Tests

```bash
# CI job (Phase B+)
./opcode42 generate > /tmp/opcode42-openapi.json
diff <(jq -S . opcode42/sdk/openapi.json) <(jq -S . /tmp/opcode42-openapi.json) \
  || (echo "DRIFT DETECTED — update opcode42/sdk/openapi.json and regenerate SDKs" && exit 1)
```

---

## Verification

A milestone is "done" when:

1. `go test ./sdk/go/...` passes all round-trip tests against a live opencode daemon.
2. `./gradlew :sdk-kotlin:test` passes all round-trip tests against a live opencode daemon.
3. SSE heartbeat timeout test: client reconnects within 90s of server going silent.
4. PTY framing test: control frame (`0x00` + JSON) and data frames decoded correctly.
5. Drift CI check passes (Phase B+): `opcode42 generate` output matches frozen spec.

---

## Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `oapi-codegen` v2 generates incorrect nullable types for some schema shapes | Medium | Low | Pin version; add unit tests for known nullable fields; file upstream issues. |
| `openapi-generator` Kotlin codegen produces non-idiomatic types requiring heavy manual override | Medium | Medium | Use `--import-mappings` and `--type-mappings` to override problem types; fall back to pure hand-written data classes for the 10–20 most-used types. |
| SSE spec drift between opencode and Opcode42 causes event type mismatches | Medium | High | Conformance harness (plan 12) catches this; freeze the event type catalog. |
| PTY WS framing change in opencode (e.g. 2-byte control header) | Low | Medium | Pin opencode version; add framing conformance test. |
| Android OkHttp EventSource does not support custom reconnect intervals | Low | Low | Implement manual reconnect loop (cancel + restart on failure); ignore `retry:` field from server. |
| WS auth via `?auth_token=` leaks credentials in server logs | Low | Low | Use the connect-token flow (`POST /pty/{ptyID}/connect-token`) for production; auth_token is fine for dev. |
| Swift openapi-generator codegen lags behind spec (common issue) | High | Low | Deferred to Phase D; use `swift-openapi-generator` (Apple's tool) as alternative when scoped. |

---

## Review pass (2026-06-03) — drift gate direction + stale paths

**Status:** M1–M4 (Go SDK + SSE + PTY clients) are built (`sdk/go/opcode42client.go`, `sse.go`,
`pty.go`, `gen/`). M5–M7 (Kotlin) and M9 (Swift) not started. M8 spec-drift gate exists
(`scripts/check-spec-drift.sh`). **M10: the response-schema conformance half is now BUILT** —
`internal/api/gen/conformance.go` (kin-openapi loader over the 3.0 derived spec, additionalProperties
relaxed per the strictness policy) + `internal/server/openapi_conformance_test.go` (validates live
GET responses against the contract; offline). It immediately caught a real divergence
(`/provider` cost shape) now logged in `known-divergences.json`. Still open: route-table-derived
`/openapi.json` emission (the daemon still serves the reference verbatim) and broadening the
validated endpoint set.

**The drift gate runs spec→code, not code→spec — be explicit about what it proves.**
- `/openapi.json` is an alias of `/doc` and serves the **embedded frozen reference verbatim**
  (`internal/server/server.go:86-88`). Codegen (`internal/api/gen/gen.go`) generates Go interfaces
  *from* `conformance/openapi-reference.json` via `oapi-codegen`. Both directions flow **out of** the
  frozen spec.
- Consequence: the spec-drift check cannot catch a handler whose request/response **shape or
  behavior** diverges from the spec — it only catches (a) generated-interface drift (`make gen` +
  `git diff --exit-code internal/api/gen/`) and (b) missing/extra **registered operations**. True
  request/response conformance is owned by **plan 12's dual-run**, not by this gate. Update the
  "Drift Tests" section so "drift gate" does not imply behavioral conformance.

**M10 — DECIDED (2026-06-03): BUILD it** (masterplan "Decisions locked" #7). Close the loop so the
gate catches per-handler shape divergence **offline, without a running opencode**. Concrete approach
for this repo (avoid a full reverse spec-generator / swaggo):
- **Route-table emission:** emit the served spec from the registered `reg(method, path, handler)`
  table rather than a static blob, so `/openapi.json` reflects the operations the daemon *actually*
  serves (upgrades the coverage check from "assumed" to "derived").
- **Per-operation response-schema conformance test (the part with teeth):** for each operation,
  take a representative handler response (from the existing handler tests) and validate it against
  that operation's response JSON Schema in `conformance/openapi-reference.json` using a schema
  validator (e.g. `santhosh-tekuri/jsonschema`). Reuse the oapi-codegen-generated models in
  `internal/api/gen/` as the handler response types so the types already track the spec. A handler
  that adds an unspec'd field or drops a required one **fails the test**.
- **Gate wiring:** run both in CI alongside `make gen` + `git diff --exit-code internal/api/gen/`.
  Complementary to — not a replacement for — plan 12's dual-run (the behavioral oracle vs live
  opencode).

**Stale path references.** The plan names the frozen file `opcode42/sdk/openapi.json` and a
`./opcode42 generate` command. The repo's canonical reference is **`conformance/openapi-reference.json`**
(synced by `scripts/sync-openapi.sh` from opencode's `packages/sdk/openapi.json`, with a provenance
file), regenerated via `make gen` (`go generate ./...` → `downconvert` to 3.0 → `oapi-codegen`).
Update M1/M8 and the Drift Tests block to these real paths/commands.

**Note:** opencode serves its live spec at `/doc` (not `/openapi.json`); Opcode42's `/openapi.json` is a
known-addition (`conformance/known-additions.json`). Keep that divergence recorded.

## Phase 2 update (2026-06-04) — route-table emission + offline drift gate BUILT

The **route-table-emission** half of M10 is now built (the per-operation response-schema half remains
as `internal/api/gen/conformance.go` + `openapi_conformance_test.go` from the prior pass).

- **Self-emitted spec.** `internal/api/spec/emit.go` (`spec.Emit`) builds the served OpenAPI doc from
  the operations the daemon **actually registered** (`server.New` now collects `regOps` as it wires
  each route). The frozen reference's `info`/`components`/etc. are reused verbatim (so request/response
  schemas stay identical to the contract); only `paths` is rebuilt from the route table. Operations
  not in the reference are emitted tagged `x-opcode42-addition: true`. Output is deterministic.
- **`/openapi.json` is no longer a verbatim alias of `/doc`.** `/doc` still serves the frozen reference
  byte-for-byte (opencode parity); `/openapi.json` now serves the **route-table-derived** self-emitted
  spec. This is what gives the drift gate teeth: dropping a `reg(...)` for a real handler whose path is
  not in the reference, or adding an unspec'd route, changes the emitted spec and trips the gate.
- **Offline drift gate (the teeth).** `internal/api/spec/drift.go` (`spec.CompareOps`/`Drift`) classifies
  per the locked policy — missing operation = FAIL, changed response status codes = FAIL, additive op =
  FAIL unless in `known-additions.json` (then WARN). `internal/server/openapi_emit_test.go` fetches the
  live `GET /openapi.json` from a fully-wired in-memory daemon and asserts it diffs clean against the
  frozen reference; `internal/api/spec/drift_test.go` proves the classifier actually catches
  missing/changed/extra (not trivially passing). Both run under `go test ./...` — no running opencode.
- **CI gate aligned.** `scripts/check-spec-drift.sh` now fetches `/openapi.json` (not `/doc`, which
  compared the reference to itself) and FAILs on unsanctioned extras. Unimplemented operations stay
  present (Opcode42 registers a 501 stub for every reference op), so 501 is not a spec-absence failure —
  matching the locked v1/sync/experimental decision.
- **Registry.** `GET /doc` and `GET /openapi.json` are recorded in `conformance/known-additions.json`
  (both are absent from opencode's own spec `paths`, so both surface as additive and are WARN'd).

## M9 follow-up (2026-06-04) — Swift SDK now COMPILES and is compile-gated

M9 is closed: the Swift SDK is generated, committed, and **compile-gated** (no longer a
not-built scaffold). Two changes, both kept deterministic across arches:

- **Generator `swift5` → `swift6`** (`scripts/gen-sdks.sh`). The `swift5` generator mis-rendered
  the array-of-array `answers` field (`QuestionReplied`, `QuestionReplyRequest`) as the
  non-compiling `[Array]`. `swift6` renders it as the correct `[[String]]` and emits a
  self-contained SwiftPM `Sources/` layout with **no external `AnyCodable` dependency**, so
  `swift build` compiles offline.
- **Normalizer step (b2)** inlines a `$ref` whose target is a *bare-array* component schema at
  array-`items` use sites. This leaves the `QuestionAnswer` bare-array schema unreferenced; the
  swift6/kotlin generator skips unreferenced bare-array schemas, so it never reaches the generated
  output (the orphan-collection pass (c) only removes `EventTui*` schemas by name — the same
  drop-the-orphan discipline the Kotlin fix used to avoid amd64/arm64 casing diffs).
  Representation-only; the wire bytes are unchanged.
- **Gating.** `scripts/check-sdk-fresh.sh` asserts `git diff`-clean `sdk/swift/gen` **and** runs
  `swift build` on the freshly-generated scratch tree when a Swift toolchain is present (skip-gated
  otherwise — the freshness diff still runs). The `sdk-fresh` CI job provisions Swift 6
  (`swift-actions/setup-swift`) and compiles `sdk/swift/gen`, so a non-compiling Swift SDK fails CI.
  `.build/` is gitignored so the compile can't dirty the committed tree.

The hand-written SSE / WS-PTY Swift layers remain deferred to plan 07's iOS scope (codegen can't
model persistent streams); the generated REST/types layer is the M9 deliverable.

## Links

- [00 — Masterplan](00-masterplan.md) — contract source, wire-compat strategy
- [07 — Client Mobile](07-client-mobile.md) — Kotlin/Swift SDK consumers
- [08 — Client TUI](08-client-tui.md) — Go SDK consumer
- [12 — Conformance](12-test-compatibility.md) — drift gate, round-trip tests against opencode
