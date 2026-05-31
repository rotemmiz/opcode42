# Plan 07 — Client: Mobile (Android) — PRIMARY DELIVERABLE

> **Primary deliverable.** The Android app is built and validated against the **real opencode
> daemon from day one** (Phase A), then repointed to the Forge daemon (Phase B+). Because
> clients are wire-compatible, mobile progress is completely decoupled from Go daemon progress.

---

## Links

- [00 — Master plan](00-masterplan.md): wire-compat contract, sequencing, architecture
- [06 — SDK generation](06-sdk-generation.md): Kotlin SDK generated from the OpenAPI spec
- [12 — Compatibility](12-test-compatibility.md): conformance harness, cross-daemon validation
- [13 — Remote ops](13-remote-ops.md): push notification infrastructure, remote hardening
- [Design reference](../design/android/): Claude design handoff — "Terminal-Material" direction, tokens, screens (`design/android/README.md`)

---

## Context

opencode already is a server-as-source-of-truth system with thin clients. The web app
(`packages/app/`) demonstrates every pattern the Android client needs: connection management,
SSE consumption with batching and reconnect, optimistic updates, and permission/question prompts.
This plan mirrors those proven patterns idiomatically in Kotlin/Jetpack Compose.

**KMP note:** the architecture is modular enough that core networking/state layers (all pure Kotlin,
no Android framework dependencies) can be extracted into a Kotlin Multiplatform module for an iOS
client later. The Android-specific layers (Keystore, WorkManager, Compose UI) remain Android-only.
This option is noted but not required for the initial delivery.

---

## opencode references validated (file:line)

### Connection abstraction and types

`packages/app/src/context/server.tsx`:

- **Line 71–76:** `ServerConnection.HttpBase = { url: string; username?: string; password?: string }`
- **Lines 78–83:** `ServerConnection.Http = { type: "http"; http: HttpBase; authToken?: boolean }`
- **Lines 86–97:** `ServerConnection.Sidecar` (variant: "base" | "wsl") and `Ssh` types — desktop-only, not needed on mobile
- **Lines 111–122:** `ServerConnection.key(conn): Key` — switch on `conn.type`; for `"http"` returns `Key.make(conn.http.url)`
- **Lines 154–168:** `add()` deduplicates by URL, normalizes, sets active
- **Lines 10–15:** `normalizeServerUrl()` — strips trailing slash, adds `http://` prefix when missing

The Android `ServerConnectionManager` mirrors this: `ServerConnection` sealed class with an `Http`
variant holding url, optional username, optional password; a `key()` function; add/remove/setActive
operations persisted to `EncryptedSharedPreferences`.

### Auth mechanism

`packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts`:

- **Line 9:** `const AUTH_TOKEN_QUERY = "auth_token"` — query-param auth
- **Lines 82–86:** credential resolution: first checks `?auth_token=<base64(user:pass)>` query
  param, then falls back to `Authorization: Basic <base64(user:pass)>` header
- **Line 84:** regex `^Basic\s+(.+)$/i` — case-insensitive Basic scheme

`packages/opencode/src/server/auth.ts`:

- **Line 41:** `Basic ${Buffer.from(`${username}:${password}`).toString("base64")}`
- **Line 47:** `headers = { Authorization: authorization }`

The Android client uses the `Authorization: Basic` header for all REST and SSE calls; for WS-PTY
it appends `?auth_token=<base64(user:pass)>` to the upgrade URL (matches the PTY ticket path in
the server's authorization middleware).

### SSE consumption, batching, reconnect, heartbeat

`packages/app/src/context/server-sdk.tsx`:

- **Line 103:** `const HEARTBEAT_TIMEOUT_MS = 15_000` — abort and reconnect if no event for 15 s
- **Line 41:** `const FLUSH_FRAME_MS = 16` — batch flush target (one animation frame)
- **Line 42:** `const STREAM_YIELD_MS = 8` — cooperative-yield interval within the stream loop
- **Line 43:** `const RECONNECT_DELAY_MS = 250` — fixed reconnect delay (no backoff in this layer)
- **Lines 106–112:** `resetHeartbeat()` — clears and resets the 15 s timeout on every received event
- **Lines 119–199:** `start()` — outer while loop, inner for-await over SSE stream, catches AbortError,
  waits `RECONNECT_DELAY_MS` before retry; the `abort` controller is the lifecycle controller,
  `attempt` is per-connection
- **Lines 208–215:** visibility-change handler — on `"visible"`, if stale (`Date.now() - lastEventAt >= 15_000`),
  aborts the current attempt to force reconnect
- **Lines 53–59:** `key()` coalesces `session.status`, `lsp.updated`, `message.part.updated`
  events: only the latest version of a coalesced key is emitted, older ones are dropped
- **Lines 62–88:** `flush()` drains the queue under a single `batch()` call; skips stale delta
  events for parts that have already received a `message.part.updated`

The TUI SDK context (`packages/opencode/src/cli/cmd/tui/context/sdk.tsx`) uses the same pattern
with explicit exponential backoff:

- **Line 44:** `retryDelay = 1000`, **line 45:** `maxRetryDelay = 30000`
- **Lines 105–106:** `backoff = Math.min(retryDelay * 2 ** (attempt - 1), maxRetryDelay)`

The Android `SseManager` implements both: the 16 ms batch window, the 15 s heartbeat abort, the
visibility/foreground trigger, and exponential backoff on reconnect (1 s → 2 s → 4 s → … capped
at 30 s).

### Store / sync / optimistic updates

`packages/app/src/context/server-sync.tsx`:

- **Lines 353–394:** `serverSDK.event.listen()` drives `applyGlobalEvent` and `applyDirectoryEvent`
- **Lines 406–420:** `onMount` defers `serverSDK.event.start()` to the next animation frame

`packages/app/src/context/directory-sync.ts`:

- **Lines 49–73:** `OptimisticStore`, `OptimisticAddInput`, `OptimisticRemoveInput`, `OptimisticItem`
- **Lines 126–135:** `applyOptimisticAdd()` — binary-search insertion into sorted message list
- **Lines 137–144:** `applyOptimisticRemove()` — binary-search removal
- **Lines 96–124:** `mergeOptimisticPage()` — reconciles server-fetched page with pending optimistic
  items; collects `confirmed` IDs for cleanup
- **Lines 209–230:** `setOptimistic` / `clearOptimistic` — local pending-message map keyed by
  `directory\nsessionID`

`packages/app/src/context/global-sync/event-reducer.ts`:

- **Lines 21–48:** `applyGlobalEvent()` — handles `global.disposed`, `server.connected`,
  `project.updated`
- **Lines 93–382:** `applyDirectoryEvent()` — full switch on event type; Binary.search-based
  sorted insert/update/remove for sessions, messages, parts, permissions, questions

The Android `AppStore` is a unidirectional store (MVI / Redux-style) with a single `reduce(state,
event)` pure function that mirrors `applyDirectoryEvent`; a `Channel<SseEvent>` feeds it from the
`SseManager` coroutine.

### Message and part shapes

`packages/opencode/src/session/message-v2.ts`:

- **Lines 97–111:** `TextPart` — `{ type: "text"; text: string; synthetic?; ignored?; time?: {start, end?}; metadata? }`
- **Lines 113–123:** `ReasoningPart` — `{ type: "reasoning"; text: string; metadata?; time: {start, end?} }`
- **Lines 160–168:** `FilePart` — `{ type: "file"; mime: string; filename?; url: string; source? }`
- **Lines 310–320:** `ToolPart` — `{ type: "tool"; callID; tool; state: ToolState; metadata? }`
- **Lines 248–308:** `ToolState` discriminated union: `pending | running | completed | error`
  - `pending` (line 249): `{ status: "pending"; input; raw }`
  - `running` (line 255): `{ status: "running"; input; title?; metadata?; time: {start} }`
  - `completed` (line 266): `{ status: "completed"; input; output; title; metadata; time: {start, end, compacted?}; attachments? }`
  - `error` (line 287): `{ status: "error"; input; error; metadata?; time: {start, end} }`
- **Lines 352–378:** `Part` union: `TextPart | SubtaskPart | ReasoningPart | FilePart | ToolPart | StepStartPart | StepFinishPart | SnapshotPart | PatchPart | AgentPart | RetryPart | CompactionPart`
- **Lines 327–349:** `User` message shape; **lines 452–490:** `Assistant` message shape

SSE event types driving part updates:
- `message.part.updated` — full part replace (coalesced by part ID)
- `message.part.delta` — incremental field append (e.g. streaming text)
- `message.part.removed`
- `message.updated`, `message.removed`
- `permission.asked` (line 304 of event-reducer.ts), `question.asked` (line 338)

### Mobile responsiveness already present in opencode web

`packages/app/src/context/layout.tsx`:

- **Line 256:** `mobileSidebar: { opened: false }` — state for the mobile sidebar overlay
- **Lines 671–680:** `mobileSidebar.opened` computed accessor + `open()`, `close()`, `toggle()` actions

This confirms the web client already has a mobile-sidebar abstraction; the Android app makes this
a native bottom-sheet / nav-drawer pattern rather than a CSS overlay.

---

## App architecture

### Technology choices

| Layer | Choice | Rationale |
|---|---|---|
| UI | Jetpack Compose (Material 3) | Modern Android declarative UI; Google-supported |
| Language | Kotlin | First-class Coroutines + Flow; KMP-ready |
| State | MVI with Kotlin StateFlow | Mirrors opencode's unidirectional event-reducer pattern |
| Networking | OkHttp + custom SSE parser | Reliable on Android; fine-grained lifecycle control |
| REST/SDK | Generated Kotlin SDK (plan 06) | Stays in sync with OpenAPI spec |
| Auth storage | EncryptedSharedPreferences (AES-256-GCM via Jetpack Security) | Android Keystore-backed |
| DI | Hilt | Standard Android DI; integrates with ViewModel |
| Background | WorkManager | Handles Doze, background restrictions for notification checks |
| Push | Firebase Cloud Messaging | See plan 13 |
| Navigation | Compose Navigation (type-safe routes, Kotlin 2.x) | |
| Image loading | Coil | Compose-native; handles data-URI and HTTP images |
| Diff rendering | Custom Compose component with syntax highlighting | For file diff parts |

### Module structure

```
:app                    — Android application module, Hilt entry, NavHost
:core:network           — OkHttp client factory, auth interceptor, SSE consumer, WS-PTY client
:core:store             — AppState, AppEvent sealed class, reduce() pure function, StateFlow wrapper
:core:sdk               — Generated Kotlin SDK (from plan 06); REST calls only
:core:model             — Data classes mirroring the OpenAPI schema (Part, Message, Session, …)
:feature:connections    — ServerConnectionManager, EncryptedSharedPreferences persistence
:feature:sessions       — Session list screen + ViewModel
:feature:chat           — Chat screen + ViewModel: message list, prompt input, part renderers
:feature:terminal       — PTY terminal pane (WS-PTY, rendered with a custom terminal view)
:feature:settings       — App settings, server management UI
:feature:notifications  — FCM integration (plan 13)
```

### Unidirectional data flow (mirrors event-reducer)

```
SseManager (coroutine)
    │  emits SseEvent
    ▼
EventChannel (Channel<SseEvent>)
    │
    ▼
StoreReducer.reduce(currentState, event): AppState
    │  pure function — mirrors applyDirectoryEvent / applyGlobalEvent
    ▼
StateFlow<AppState>  ──► Compose UI (collectAsStateWithLifecycle)
```

`AppState` top-level shape (mirrors `server-sync.tsx` GlobalStore + TUI sync store):

```kotlin
data class AppState(
    val sessions: List<Session> = emptyList(),
    val messages: Map<String, List<Message>> = emptyMap(),      // sessionID → messages
    val parts: Map<String, List<Part>> = emptyMap(),            // messageID → parts
    val permissions: Map<String, List<PermissionRequest>> = emptyMap(),
    val questions: Map<String, List<QuestionRequest>> = emptyMap(),
    val sessionStatus: Map<String, SessionStatus> = emptyMap(),
    val optimisticMessages: Map<String, List<OptimisticMessage>> = emptyMap(),
    val connectionState: ConnectionState = ConnectionState.Disconnected,
)
```

---

## Connection and auth and secure storage

### ServerConnection sealed class

```kotlin
sealed class ServerConnection {
    abstract val http: HttpConfig
    abstract val displayName: String?
    fun key(): String = when (this) {
        is Http -> http.url
    }

    data class Http(
        override val http: HttpConfig,
        override val displayName: String? = null,
    ) : ServerConnection()

    data class HttpConfig(val url: String, val username: String? = null, val password: String? = null)
}
```

This directly mirrors `ServerConnection.Http` and `HttpBase` from
`packages/app/src/context/server.tsx:71-83`.

### ServerConnectionManager

- Persists `List<ServerConnection>` to `EncryptedSharedPreferences` (AES-256-GCM key stored in
  Android Keystore hardware-backed key store on API 23+).
- `add(conn)`: normalize URL (strip trailing slash, add `http://` prefix if missing — mirrors
  `normalizeServerUrl` at `server.tsx:10-15`), deduplicate by key, persist.
- `remove(key)`: remove, select next.
- `setActive(key)`: update active server, trigger SSE reconnect.
- Exposes `StateFlow<List<ServerConnection>>` and `StateFlow<ServerConnection?>` (active).

### Auth interceptor (OkHttp)

```kotlin
class AuthInterceptor(private val connectionManager: ServerConnectionManager) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val conn = connectionManager.active.value ?: return chain.proceed(chain.request())
        val cfg = conn.http
        val request = if (cfg.username != null && cfg.password != null) {
            val credential = Credentials.basic(cfg.username, cfg.password)
            chain.request().newBuilder()
                .header("Authorization", credential)
                .build()
        } else chain.request()
        return chain.proceed(request)
    }
}
```

For WS-PTY upgrades, append `?auth_token=<base64(user:pass)>` (matches the server's
`AUTH_TOKEN_QUERY` at `authorization.ts:9` and `credentialFromURL` at `authorization.ts:82-83`).

### Directory header

All API calls include `x-opencode-directory: <base64(path)>` (v2 format) per the master plan
contract. The Kotlin SDK (plan 06) handles this transparently via a request factory that accepts
`directory: String?`.

### mDNS / LAN auto-discovery (zero-config connect)

**Goal.** Let a user connect to a daemon on the same Wi-Fi **without typing an address**. The app
browses the local network for advertised daemons and offers them as one-tap "suggested
connections" that feed the existing `ServerConnectionManager.add(...)` flow above. Manual entry
stays as the fallback.

**Service type.** Browse for **`_opencode._tcp.`** (preferred — distinguishes a real daemon from
any other HTTP service); optionally also `_http._tcp.` as a secondary. TXT-record keys read when
present:

| TXT key     | Meaning                                           |
|-------------|---------------------------------------------------|
| `path`      | base path (default `/`)                           |
| `directory` | suggested `x-opencode-directory`                  |
| `version`   | daemon version (display + compat hint)            |
| `auth`      | `basic` / `token` / `none` (drives the auth form) |

A resolved record yields `scheme://host:port` → run through `normalizeServerUrl` (same path as
manual add) → probe with `GET /app` before showing it as "found on your network".

#### Android implementation

Use the platform **`android.net.nsd.NsdManager`** — no third-party dependency:

```kotlin
// :feature:connections — DiscoveryManager.kt
class DiscoveryManager(private val nsd: NsdManager, private val wifi: WifiManager) {
    private var lock: WifiManager.MulticastLock? = null
    private val listener = object : NsdManager.DiscoveryListener { /* onServiceFound -> resolve */ }

    fun start(serviceType: String = "_opencode._tcp.") {
        lock = wifi.createMulticastLock("forge-mdns").apply { setReferenceCounted(true); acquire() }
        nsd.discoverServices(serviceType, NsdManager.PROTOCOL_DNS_SD, listener)
    }
    fun stop() { runCatching { nsd.stopServiceDiscovery(listener) }; lock?.release(); lock = null }
}
```

- **Resolve serially.** Pre-API-34, `NsdManager.resolveService` can throw if called concurrently —
  queue resolves, or use `registerServiceInfoCallback` on API 34+.
- **Lifecycle.** Browse only while the connections / add-server screen is `STARTED`; `stop()` on
  `onStop` to drop the multicast lock and save battery.
- **De-dupe** by `host:port` and merge with already-saved connections (mark "added" vs "new").

**Manifest.** `INTERNET` is already declared; add multicast reception:

```xml
<uses-permission android:name="android.permission.CHANGE_WIFI_MULTICAST_STATE" />
<uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />
```

**UI.** A "Servers on your network" section at the top of the Add-Server screen: a live list of
discovered daemons (name, host:port, version), each tappable to prefill the form and — if
`auth=none` — add immediately. Spinner while browsing; empty-state explains "make sure you're on
the same Wi-Fi."

#### Bonjour on top of opencode (for testing — no opencode code changes)

opencode does **not** advertise itself over mDNS today, so during development we run a tiny
**sidecar publisher** next to the daemon. This needs **zero changes to opencode** and lets us
build/test the Android discovery path immediately. Ship these as `scripts/mdns-advertise.*`.

- **macOS (built-in Bonjour):**
  ```bash
  dns-sd -R "opencode" _opencode._tcp local 4096 path=/ version=dev
  # keep running; advertises only while alive
  ```
- **Linux (avahi static service — survives reboot, no process to babysit):**
  ```xml
  <!-- /etc/avahi/services/opencode.service -->
  <service-group>
    <name>opencode</name>
    <service><type>_opencode._tcp</type><port>4096</port>
      <txt-record>path=/</txt-record></service>
  </service-group>
  ```
  or one-shot: `avahi-publish-service "opencode" _opencode._tcp 4096 path=/`
- **Cross-platform sidecar:** a ~15-line Python (`zeroconf`) or Go script registering the same
  record — for CI/containers where `dns-sd`/`avahi` aren't present.

> **Later (native):** when the Forge daemon matures it should advertise `_opencode._tcp` itself at
> startup (track in plan 01 / 13). The sidecar is a testing scaffold, not the long-term answer.

#### Testing notes & gotchas

- **Use a physical device** on the same Wi-Fi for end-to-end testing. The Android **emulator** sits
  behind a virtual NAT and generally **cannot see host-LAN mDNS**, so emulator + host `dns-sd`
  won't discover each other reliably.
- Unit-test `DiscoveryManager` against a **fake `NsdManager`** (start/stop, multicast lock
  acquire/release, serial resolve, de-dupe). Manual matrix: device finds sidecar, TXT parsing,
  auth-required prompt, two daemons on the LAN, Wi-Fi off → graceful empty state.
- **Build order:** (1) sidecar script + manifest perms, (2) `DiscoveryManager` + fake-based tests,
  (3) Add-Server "found on network" UI wired to `add(...)`, (4) device E2E.

---

## SSE lifecycle on Android (the hard part)

### Why this is hard on Android

Android kills background processes aggressively (Doze mode, App Standby Buckets, background
execution limits). A foreground SSE connection is fine while the app is active; once the app is
backgrounded, the OS may suspend network I/O within seconds. The strategy is:

1. **Foreground:** maintain a live OkHttp SSE connection with a 15 s heartbeat abort/reconnect.
2. **Background:** close the SSE connection; rely on push notifications (plan 13) for wakeup.
3. **Return to foreground:** immediately reconnect SSE and reconcile state.

### SseManager

```kotlin
class SseManager(
    private val client: OkHttpClient,
    private val connectionManager: ServerConnectionManager,
    private val store: AppStore,
    private val scope: CoroutineScope,       // tied to the app's lifecycle, not a screen
) {
    private val HEARTBEAT_TIMEOUT_MS = 15_000L   // mirrors server-sdk.tsx:103
    private val FLUSH_FRAME_MS = 16L              // mirrors server-sdk.tsx:41
    private val RECONNECT_DELAY_BASE_MS = 1_000L  // mirrors tui/context/sdk.tsx:44
    private val RECONNECT_DELAY_MAX_MS = 30_000L  // mirrors tui/context/sdk.tsx:45

    fun start()   // called on app foreground
    fun stop()    // called on app background
    fun reconnect() // called after setActive()
}
```

**SSE connection loop:**

```
while (active) {
    attempt = 0
    connect to GET /global/event (OkHttp streaming)
    start 15 s heartbeat watchdog (abort connection if no event)
    for each raw SSE line:
        reset heartbeat watchdog
        parse { id, type, properties }
        enqueue to batch buffer
        schedule flush after 16 ms (if not already scheduled)
    on connection close / error:
        flush any remaining buffered events
        backoff = min(1000 * 2^attempt, 30000)   // exponential backoff
        delay(backoff)
        attempt++
}
```

**Batch flush (mirrors `flush()` in `server-sdk.tsx:62-88`):**

- Collect all buffered events.
- For coalesced event types (`session.status`, `lsp.updated`, `message.part.updated`): keep only
  the latest per compound key (`directory:messageID:partID`).
- Skip `message.part.delta` events whose part has already received a `message.part.updated` in
  the same batch (stale delta suppression).
- Dispatch the deduplicated batch to the store reducer in a single transaction.

**Foreground/background lifecycle (`ProcessLifecycleOwner`):**

```kotlin
ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
    override fun onStart(owner: LifecycleOwner) { sseManager.start() }
    override fun onStop(owner: LifecycleOwner) { sseManager.stop() }
})
```

On `onStart`, also check `System.currentTimeMillis() - lastEventAt >= HEARTBEAT_TIMEOUT_MS`;
if stale, force reconnect (mirrors the `visibilitychange` handler at `server-sdk.tsx:208-215`).

**Doze / App Standby:** when the system delivers a high-priority FCM message (plan 13), the app
receives a brief CPU window; use it to re-connect SSE for ~10 s (to drain any pending events),
then close again. The WorkManager constraint `NetworkType.CONNECTED` ensures reconnect attempts
are not made without connectivity.

### SSE raw parser

OkHttp's `EventSourceListener` handles SSE line framing. Each event carries:

```
id: <event-id>
data: {"type":"message.part.updated","properties":{...},"directory":"<path>"}
```

The `directory` field in the data JSON is the per-directory routing key (matches
`event.directory` in `server-sdk.tsx:150`). Parse with kotlinx.serialization.

---

## Optimistic updates and reconciliation

Pattern mirrors `directory-sync.ts:49-230` exactly:

1. **On prompt submit:**
   - Generate a client-side `messageID` (UUID v7 — monotonically sortable, same property as
     opencode's `MessageID.ascending()`).
   - Build optimistic `Message` (role: "user", status: pending) and one `TextPart`.
   - Insert into `AppState.optimisticMessages[sessionID]` (binary-search insertion by ID to
     maintain sort order — mirrors `applyOptimisticAdd` at `directory-sync.ts:126-135`).
   - Immediately reflect in UI (no waiting for server).
   - POST `POST /session/{id}/prompt` (or `POST /session` if new session).

2. **On server reconciliation (SSE `message.updated` event):**
   - When server confirms a message whose ID matches an optimistic entry, run
     `mergeOptimisticPage` logic: if the server message and its parts are now present, mark the
     optimistic entry as confirmed and remove it (mirrors `directory-sync.ts:96-124`).

3. **On error:**
   - If the POST fails or the server emits an error, remove the optimistic message (`applyOptimisticRemove`,
     `directory-sync.ts:137-144`) and show a retry affordance.

**Binary-search sorted insertion:** all `session`, `message`, and `part` lists are kept sorted by
ID (IDs are lexicographically monotonic). Use `Collections.binarySearch` with a comparator. This
mirrors `Binary.search` used throughout `event-reducer.ts` and `directory-sync.ts`.

---

## Screens and components

### Navigation graph

```
ServerList (startup if no servers configured)
    └── AddServer (URL + credentials form)

SessionList (home)
    ├── NewSession (sheet: pick agent/model)
    └── Chat (session selected)
            ├── PartRenderer (inline in lazy column)
            │       ├── TextPartView (markdown rendered via Markwon or custom renderer)
            │       ├── ReasoningPartView (collapsible, italic)
            │       ├── ToolPartView (name + status chip + expandable input/output)
            │       ├── FilePartView (inline preview or attachment chip)
            │       └── DiffPartView (unified diff with syntax highlight)
            ├── PermissionPrompt (bottom sheet, blocks input)
            ├── QuestionPrompt (bottom sheet, free-text answer)
            ├── PromptInput (sticky bottom bar: text field + send + attach + model picker)
            └── TerminalPane (optional side panel: WS-PTY, rendered as terminal emulator)

Settings
    ├── ServerManagement (list + add/remove/edit)
    └── AppPreferences
```

### Chat screen — LazyColumn streaming

- Use `LazyColumn` with `reverseLayout = false`; `rememberLazyListState()` auto-scrolls to bottom
  when the last item changes.
- `messages` and `parts` come from `StateFlow<AppState>` collected with
  `collectAsStateWithLifecycle()`.
- Each message is a `LazyColumn` item; its parts are rendered as a `Column` of `PartRenderer`
  composables.
- For `ToolPart` with state `running`, show an animated progress indicator (maps to the `active`
  state in opencode's tool states). For `pending`, show a spinner + "waiting" label. For
  `completed`/`error`, show the collapsed result with expand affordance.
- `message.part.delta` events produce incremental text updates; the store accumulates deltas into
  the part's `text` field; Compose re-renders only the changed leaf.

### Permission / question prompts

Driven by SSE events `permission.asked` and `question.asked` (handled in `applyDirectoryEvent` at
`event-reducer.ts:304` and `event-reducer.ts:338`):

- A non-dismissible `ModalBottomSheet` appears when `AppState.permissions[sessionID]` or
  `AppState.questions[sessionID]` is non-empty.
- Approve/deny calls `POST /permission/{requestID}/reply` or `POST /question/{requestID}/reply`.
- On `permission.replied` / `question.replied` / `question.rejected` SSE events, the store
  removes the entry and the sheet dismisses.

### PTY terminal pane

- WS-PTY over `wss://{host}/pty/{id}/connect` (or `ws://`).
- Auth via `?auth_token=<base64(user:pass)>` appended to the WebSocket URL (matches the server's
  `hasPtyConnectTicketURL` fallback in `authorization.ts`).
- Framing: control frame `0x00 + JSON({cursor})`, data frames as UTF-8 bytes (per master plan
  contract).
- Render with a custom `Canvas`-based terminal emulator or integrate `termux-view` / `Konsole`
  Compose equivalent.
- Input: software keyboard events → binary WS frames.

---

## Push notifications

Full design in plan 13. Summary from mobile perspective:

- The Forge daemon (or a notification relay service) sends FCM high-priority messages when:
  - Agent completes a session (turn finished, no further prompts pending).
  - `permission.asked` or `question.asked` event fires (agent is blocked, waiting for user).
- On FCM receipt, the Android app:
  1. Shows an actionable notification (Approve / Deny for permissions; Open for completions).
  2. If the app is in background, briefly re-connects SSE (via a `CoroutineScope` tied to a
     `ForegroundService` or `BroadcastReceiver` with a short-lived wake lock) to pull the latest
     state before displaying the notification.
  3. Deep-links the notification into the relevant `Chat` screen with the correct session ID.

---

## Phased delivery

### Phase A — v0 against the real opencode daemon (no Forge daemon needed)

Goal: a functional Android app that works with `opencode serve`.

| Milestone | Deliverable |
|---|---|
| A1 | Server connection manager (add/select servers, EncryptedSharedPreferences, URL normalization) |
| A2 | REST client (generated Kotlin SDK from plan 06) + auth interceptor + Basic Auth header |
| A3 | `GET /global/event` SSE consumer: raw parse, heartbeat (15 s), reconnect (250 ms fixed), batch (16 ms) |
| A4 | Session list screen: `GET /session/list`, display, new session creation |
| A5 | Chat screen: load messages + parts for a session, display static history |
| A6 | Live SSE integration: `message.updated`, `message.part.updated`, `message.part.delta` drive real-time updates in Chat |
| A7 | Prompt submit: optimistic add + `POST /session/{id}/prompt` + reconcile on `message.updated` |
| A8 | Permission/question bottom sheets: `permission.asked`, `question.asked`, reply endpoints |
| A9 | Exponential backoff reconnect; foreground/background lifecycle handling |
| A10 | Basic settings: server list management, dark/light theme |

Phase A complete = a usable mobile coding assistant against real opencode.

### Phase B — Repoint to Forge daemon

- Change base URL in ServerConnectionManager to point to Forge daemon.
- Run the conformance harness (plan 12) to verify parity.
- Fix any divergences found; no app code changes expected if the daemon is wire-compatible.

### Phase C — Full feature parity

- PTY terminal pane (WS-PTY).
- Push notifications (plan 13).
- File attachment in prompt input.
- Session forking, archiving.
- mDNS / LAN auto-discovery: browse `_opencode._tcp` so users connect without typing an address (see "mDNS / LAN auto-discovery" under Connection and auth). Includes a Bonjour sidecar to advertise opencode for testing.
- Diff viewer for `session.diff` events.
- KMP extraction of `:core:network` and `:core:store` for future iOS port.

---

## Testing

### Functional tests

- **Unit:** `reduce(state, event)` pure function — property-based tests with
  Kotest + Arb generators. Cover all event types from `applyDirectoryEvent` and
  `applyGlobalEvent`. No Android framework needed; fast.
- **Integration:** `SseManager` against a local mock SSE server (MockWebServer from OkHttp);
  verify batch coalescing, heartbeat abort, reconnect backoff.
- **Optimistic update round-trip:** submit prompt → verify optimistic message appears → inject
  `message.updated` SSE event → verify reconciliation removes optimistic entry.
- **UI (Compose):** `ComposeTestRule` tests for Chat screen: renders `TextPart`, `ToolPart`
  (all four states), `PermissionPrompt` sheet trigger/dismiss.

### Performance tests

- Message list with 1000 messages + 5 parts each: scroll FPS must stay above 55 on a Pixel 5
  (Snapdragon 765G). Use Macrobenchmark.
- SSE burst: inject 500 `message.part.delta` events in < 100 ms; verify all 500 reach the store
  and the UI renders within 200 ms of the last event (batch flush latency).
- Memory: hold 10 open sessions in memory; heap must stay below 150 MB on a 2 GB device.

### Compatibility tests (device matrix)

| Dimension | Values to cover |
|---|---|
| API level | 26 (min), 30, 33, 34, 35 |
| Architecture | arm64-v8a, x86_64 (emulator) |
| Network | WiFi, 4G, flaky (throttled via Android emulator network shaping), VPN |
| Screen | compact phone (360 dp), large phone (420 dp), foldable (700 dp unfolded) |

Run on Firebase Test Lab with real devices for physical sensor / thermal tests.

### Dual-daemon validation

Each Phase A milestone is tested against **both** the real opencode daemon and (once available in
Phase B) the Forge Go daemon. The test fixtures are identical; only the base URL changes. This is
the mobile-side face of plan 12's conformance harness.

---

## Verification (concrete flows)

1. **Add server + auth:** launch app → tap "+" → enter `http://myserver:4096` + password →
   confirm connection → session list loads. Verify `Authorization: Basic …` header present in
   network log (Charles / OkHttp logging interceptor).

2. **Live SSE stream:** open a session that is currently running → observe parts appearing in
   real time without a manual refresh → turn off WiFi → wait 20 s → turn WiFi back on → verify
   auto-reconnect and state catches up (no duplicate or missing messages).

3. **Heartbeat abort:** block SSE data at the network layer for 16 s → verify the app reconnects
   within 2 s of the 15 s timeout firing.

4. **Permission prompt:** trigger a tool that requires permission on the daemon → verify the
   bottom sheet appears within one SSE batch window (≤ 32 ms from server event) → tap Approve →
   verify `permission.replied` SSE event dismisses the sheet and the tool resumes.

5. **Optimistic message:** submit a prompt on a slow network (throttled to 1 kbps) → verify the
   user message appears immediately in the UI → wait for server confirmation → verify optimistic
   entry is replaced by the server-confirmed message.

6. **Background/foreground:** put app in background for 60 s → return to foreground → verify
   SSE reconnects, session list and any new messages are up to date without a manual refresh.

7. **Cross-daemon:** run flow 2–4 against opencode daemon, then identically against Forge daemon;
   assert identical user-visible behavior.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Android kills SSE connection in background | High | Push notifications (plan 13) as the background signal; SSE only for foreground |
| Doze mode delays FCM high-priority messages | Medium | Use `PRIORITY_HIGH` FCM; test in Doze with `adb shell dumpsys deviceidle force-idle` |
| SSE connections dropped by intermediate proxies / load balancers after 30–60 s | High | 15 s heartbeat abort forces reconnect before most proxy timeouts; server emits `server.heartbeat` every ~10 s |
| OkHttp SSE buffering on slow streams | Low | Use `EventSource` with manual byte-level parsing; set `readTimeout(0)` for streaming |
| Binary-search sorted insertion correctness | Medium | Exhaustive unit tests on `reduce()` with edge cases (duplicate IDs, out-of-order delivery) |
| Kotlin SDK drift from OpenAPI spec | Low | Plan 06 regenerates from `packages/sdk/openapi.json` on every spec update; plan 12 catches drift |
| Battery drain from always-on foreground service | Medium | Do not use a foreground service; rely on `ProcessLifecycleOwner` + WorkManager; profile with Android Battery Historian |
