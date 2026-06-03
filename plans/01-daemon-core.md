# Forge Plan 01 — Daemon Core

> Transport, state management, auth, routing, and event bus. Does NOT cover the
> agent engine (plan 02) or ecosystem loaders (plan 04). Everything here must
> be runnable without a working agent.

---

## Context (why, what it unblocks)

The daemon core is the loadbearing foundation: no other plan ships user-visible
functionality until HTTP+SSE+WS is live and sessions can be stored. Critically,
having a wire-compatible skeleton — even one that returns stubs for agent
endpoints — lets the mobile client (plan 07) and the TUI (plan 08) be built and
tested against the **real opencode daemon first** and then seamlessly repointed
at Forge the moment a given endpoint is implemented. This also makes the
conformance harness (plan 12) runnable from day one.

Milestones in this plan gate:
- Plan 02 (agent engine) — needs session/message persistence and SSE bus
- Plan 06 (SDK gen) — needs the OpenAPI spec emitted by the running daemon
- Plan 07 (mobile) — needs auth + directory routing to work correctly
- Plan 09 (integration) — needs graceful shutdown and mDNS
- Plan 12 (conformance) — needs a living HTTP server to target

---

## opencode references validated

All citations are from the repository at `/Users/rotemmiz/git/opencode`.

| File:line | Takeaway |
|-----------|----------|
| `packages/opencode/src/server/auth.ts:17-20` | Auth config is `OPENCODE_SERVER_PASSWORD` + `OPENCODE_SERVER_USERNAME` (default `"opencode"`) from env via Effect Config. |
| `packages/opencode/src/server/auth.ts:24-34` | `required()` is true only when password env var is set and non-empty; `authorized()` does string-equal compare of username + password. |
| `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts:9,83-86` | Query param `auth_token` holds `base64(user:pass)`; header fallback is `Authorization: Basic <base64>`. Both are standard Base64, not URL-safe. |
| `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts:54-58` | On 401, adds `www-authenticate: Basic realm="Secure Area"` header. |
| `packages/opencode/src/server/routes/instance/httpapi/middleware/workspace-routing.ts:87` | Directory resolution order: `?directory` query → `x-opencode-directory` header → `process.cwd()`. |
| `packages/sdk/js/src/client.ts:49` | v1 SDK sends `x-opencode-directory: encodeURIComponent(dir)`, then on GET rewrites to `?directory=<value>`. |
| `packages/sdk/js/src/v2/client.ts:63` | v2 SDK same encoding; additionally handles `x-opencode-workspace`. |
| `packages/opencode/src/server/routes/instance/httpapi/middleware/workspace-routing.ts:23-25` | Both `directory` and `workspace` query params must be accepted; schema is `optional(string)`. |
| `packages/opencode/src/project/instance-store.ts:105-120` | Instance cache is a `Map<string, Entry>` keyed by the resolved absolute directory path; single-flight via a `Deferred`. If boot fails the entry is removed. |
| `packages/opencode/src/project/instance-store.ts:77-89` | On dispose, fires `server.instance.disposed` event via `GlobalBus.emit`. |
| `packages/opencode/src/bus/index.ts:17-22` | `InstanceDisposed` is `BusEvent.define("server.instance.disposed", { directory: string })`. |
| `packages/opencode/src/bus/index.ts:100-119` | Every publish emits to instance-level PubSub AND `GlobalBus`. Payload shape: `{ id, type, properties }`. |
| `packages/opencode/src/bus/global.ts:5-8` | `GlobalEvent` wraps payload with optional `{ directory, project, workspace }`. The SSE handler strips the wrapper and sends only `payload` to clients. |
| `packages/opencode/src/server/routes/instance/httpapi/handlers/event.ts:36-52` | Instance `/event` SSE: emits `server.connected` first, then merges live bus stream with 10s heartbeat, stops on `InstanceDisposed`. Headers: `Cache-Control: no-cache, no-transform`, `X-Accel-Buffering: no`, `X-Content-Type-Options: nosniff`. SSE event name is `"message"`. |
| `packages/opencode/src/server/routes/instance/httpapi/handlers/global.ts:36-66` | Global `/global/event` SSE: same `server.connected` + heartbeat pattern but subscribed to `GlobalBus` node EventEmitter directly; sends `event.payload` not the full GlobalEvent wrapper. |
| `packages/opencode/src/pty/index.ts:17-18` | PTY constants: `BUFFER_LIMIT = 2MB`, `BUFFER_CHUNK = 64KB`. |
| `packages/opencode/src/pty/index.ts:44-51` | Control frame: byte `0x00` followed by UTF-8 JSON `{ cursor }` (character count, not byte offset). |
| `packages/opencode/src/pty/index.ts:239-262` | Live data frames are plain UTF-8 strings sent in ≤ 64KB slices. Buffer is trimmed to 2MB; earlier chars are dropped and `bufferCursor` advanced. |
| `packages/opencode/src/pty/index.ts:301-361` | On WebSocket connect, replays buffer from `cursor` param, then sends control frame `meta(end)`, then registers subscriber. |
| `packages/opencode/src/server/routes/instance/httpapi/handlers/pty.ts:156` | WS `connect` endpoint reads `?cursor=<int>`; `-1` means "start from current end". |
| `packages/opencode/src/storage/db.ts:103-109` | SQLite PRAGMAs: WAL mode, synchronous NORMAL, busy_timeout 5000ms, cache_size 64MB (negative = KiB), foreign_keys ON, WAL checkpoint PASSIVE. |
| `packages/opencode/src/storage/db.ts:32-36` | DB path: `$XDG_DATA_HOME/opencode/opencode.db` (or channel-suffixed variant). Override via `OPENCODE_DB` env var; `:memory:` is accepted. |
| `packages/opencode/src/session/session.sql.ts:16-58` | `session` table: `id`, `project_id` (FK→project), `slug`, `directory`, `title`, `version`, plus cost/token counters, `revert`, `permission`, `agent`, `model`, timestamps. |
| `packages/opencode/src/session/session.sql.ts:61-73` | `message` table: `id`, `session_id` (FK), timestamps, `data` JSON blob. Index on `(session_id, time_created, id)`. |
| `packages/opencode/src/session/session.sql.ts:75-91` | `part` table: `id`, `message_id` (FK), `session_id`, timestamps, `data` JSON blob. Indices on `(message_id, id)` and `(session_id)`. |
| `packages/opencode/src/project/project.sql.ts:5-17` | `project` table: `id`, `worktree`, `name`, `sandboxes` JSON array, `commands`, timestamps. |
| `packages/opencode/src/session/message-v2.ts:563-578` | Cursor pagination: cursor is `base64url(JSON({ id, time }))`. Page query orders by `(time_created DESC, id DESC)`, returns `limit+1` to detect `more`. |
| `packages/opencode/src/session/message-v2.ts:596-610` | `WHERE (time_created < cursor.time) OR (time_created = cursor.time AND id < cursor.id)` — stable pagination over equal timestamps. |
| `packages/opencode/src/config/config.ts:55-61` | `mergeConfigConcatArrays`: `instructions` field is concatenated (de-duplicated via `Set`) across layers; all other fields use deep merge (last wins). |
| `packages/opencode/src/config/config.ts:443-476` | Global config load order: `config.json` → `opencode.json` → `opencode.jsonc` all under `Global.Path.config` (`~/.config/opencode/`). |
| `packages/opencode/src/config/config.ts:596-612` | `OPENCODE_CONFIG` env override loads a named file after global. `OPENCODE_DISABLE_PROJECT_CONFIG` skips project-level files. `OPENCODE_CONFIG_DIR` points to an extra `.opencode`-equivalent dir. |
| `packages/opencode/src/config/config.ts:666-674` | `OPENCODE_CONFIG_CONTENT` is a raw JSONC string injected last (highest priority, local scope). |
| `packages/opencode/src/config/paths.ts:10-21` | Project config files: walks up from `directory` to `worktree`, collecting `opencode.jsonc` / `opencode.json` (reversed so parent-most applies first). |
| `packages/opencode/src/config/paths.ts:23-41` | Config directories: `~/.config/opencode/` + `.opencode` dirs up the tree + `~/.opencode/` + `OPENCODE_CONFIG_DIR`. |
| `packages/opencode/src/server/server.ts:120-125` | Default port: tries 4096 first, falls back to OS-assigned. |
| `packages/opencode/src/server/server.ts:158-171` | mDNS: published only when `mdns=true` AND hostname is NOT loopback. |
| `packages/opencode/src/server/mdns.ts:9-44` | mDNS service type `"http"`, host `opencode.local` (default), name `opencode-<port>`, txt `{ path: "/" }`. |
| `packages/opencode/src/cli/cmd/serve.ts:15` | Warn when `OPENCODE_SERVER_PASSWORD` unset (server is unsecured). |
| `packages/opencode/src/cli/network.ts:49-61` | Config fields `server.port`, `server.hostname`, `server.mdns`, `server.mdnsDomain`, `server.cors` override CLI flags. mDNS forces hostname to `0.0.0.0` unless explicitly set. |

---

## Design

### 1. HTTP server and router

**Choice:** `net/http` stdlib + [`go-chi/chi/v5`](https://github.com/go-chi/chi)

**Rationale:**
- chi is a thin router over `net/http` (zero external allocations in the hot
  path), easy to swap, and works with standard `http.Handler` middleware.
- Alternatives: Echo is heavier and opinionated; Gin bundles too much. gorilla/mux
  is archived.
- `net/http` HTTP/1.1 is sufficient; opencode uses Node HTTP/1.1. HTTP/2 can be
  added later behind a flag.

**Server config:**
```go
srv := &http.Server{
    Addr:              net.JoinHostPort(cfg.Hostname, strconv.Itoa(cfg.Port)),
    ReadHeaderTimeout: 30 * time.Second,
    WriteTimeout:      0,           // SSE streams are long-lived
    IdleTimeout:       120 * time.Second,
    Handler:           router,
}
```

Graceful shutdown: `srv.Shutdown(ctx)` with a 10-second context after `SIGINT`/`SIGTERM`.

### 2. SSE writer

No third-party library. The pattern is:

```go
func sseStream(w http.ResponseWriter, r *http.Request, events <-chan SSEEvent) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache, no-transform")
    w.Header().Set("X-Accel-Buffering", "no")
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.WriteHeader(http.StatusOK)
    flusher := w.(http.Flusher)
    for {
        select {
        case <-r.Context().Done():
            return
        case ev, ok := <-events:
            if !ok { return }
            fmt.Fprintf(w, "event: message\ndata: %s\n\n", ev.JSON)
            flusher.Flush()
        }
    }
}
```

SSE event name is always `"message"` (validated: `handlers/event.ts:12-16`).
Payload shape is always `{ "id": "...", "type": "...", "properties": {...} }`.

**On connect,** immediately enqueue `server.connected` then start the live
stream. Inject `server.heartbeat` every 10 seconds on a separate ticker goroutine
merged with a `select`.

### 3. WebSocket PTY transport

**Choice:** [`coder/websocket`](https://github.com/coder/websocket)

**Rationale:** Pure Go, no CGo, passes `net/http` `http.Handler` directly,
supports both RFC 6455 and Safari's non-standard close frames, actively
maintained, and trivially upgrades existing `http.ResponseWriter`.
gorilla/websocket is an alternative but is in maintenance mode.

**PTY framing protocol** (source: `pty/index.ts:44-51`, `239-262`, `301-361`):

| Frame direction | Byte 0 | Payload |
|----------------|--------|---------|
| Server → Client (control) | `0x00` | UTF-8 JSON `{"cursor":<n>}` |
| Server → Client (data) | any non-`0x00` | UTF-8 string, up to 64 KB per chunk |
| Client → Server (input) | — | UTF-8 string (stdin) |

On connect:
1. Validate `?cursor=<int>` query param; `-1` = start at current end.
2. Replay buffered output from `cursor` position in ≤ 64KB slices.
3. Send control frame `0x00 + {"cursor": <current_end>}`.
4. Register subscriber; forward all new output; accept input frames.

Buffer management: ring buffer of last 2MB of output; `bufferCursor` tracks the
absolute offset of the oldest byte in the ring.

**WS config:**
```go
conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    InsecureSkipVerify: true, // CORS checked separately
})
conn.SetReadLimit(64 * 1024)
```

### 4. Config loader

The Go config loader must match opencode's load order **exactly** to be a
drop-in for its config format. All files are parsed as JSONC (JSON with C-style
comments and trailing commas).

**Load order (high to low priority, later overrides earlier):**

1. `~/.config/opencode/config.json`
2. `~/.config/opencode/opencode.json`
3. `~/.config/opencode/opencode.jsonc`
4. `OPENCODE_CONFIG` env — a named file path
5. Project-level files: walk up from `directory` to git root, collect
   `opencode.jsonc` and `opencode.json` at each level (parent-first so child
   wins)
6. Each `.opencode/` directory in the tree: `opencode.json`, `opencode.jsonc`
7. `OPENCODE_CONFIG_DIR` directory: `opencode.json`, `opencode.jsonc`
8. `OPENCODE_CONFIG_CONTENT` env — raw JSONC string (highest priority)

**Merge rules:**
- All scalar and object fields: deep merge, last source wins.
- `instructions` array: concatenated with de-duplication (preserve order, drop
  exact duplicates).

**Deprecated TUI keys** (`theme`, `keybinds`, `tui`) are silently stripped when
encountered in a config file.

**JSONC parser:** [`github.com/nicholasgasior/gsfmt`](https://github.com/tailscale/hujson)
→ actually use **`tailscale/hujson`** (strips JSONC comments, then `encoding/json`
handles the rest). No other dependency needed.

**Config schema** (Go struct, not code-generated, matches `config.ts:134-306`):

```go
type Config struct {
    Schema           *string                      `json:"$schema,omitempty"`
    Shell            *string                      `json:"shell,omitempty"`
    LogLevel         *string                      `json:"logLevel,omitempty"`
    Server           *ServerConfig                `json:"server,omitempty"`
    Model            *string                      `json:"model,omitempty"`
    SmallModel       *string                      `json:"small_model,omitempty"`
    DefaultAgent     *string                      `json:"default_agent,omitempty"`
    Instructions     []string                     `json:"instructions,omitempty"`
    Agent            map[string]AgentConfig       `json:"agent,omitempty"`
    Provider         map[string]ProviderConfig    `json:"provider,omitempty"`
    MCP              map[string]MCPConfig         `json:"mcp,omitempty"`
    LSP              interface{}                  `json:"lsp,omitempty"`
    Permission       *PermissionConfig            `json:"permission,omitempty"`
    Compaction       *CompactionConfig            `json:"compaction,omitempty"`
    DisabledProviders []string                    `json:"disabled_providers,omitempty"`
    EnabledProviders  []string                    `json:"enabled_providers,omitempty"`
    // ... remaining fields per config.ts schema
}

type ServerConfig struct {
    Port      *int     `json:"port,omitempty"`
    Hostname  *string  `json:"hostname,omitempty"`
    MDNS      *bool    `json:"mdns,omitempty"`
    MDNSDomain *string `json:"mdnsDomain,omitempty"`
    CORS      []string `json:"cors,omitempty"`
}
```

Config is per-instance (one loaded per directory). Global config is cached at
startup; instance config is derived lazily on first request for that directory.

### 5. Persistence (SQLite)

**Choice:** `modernc.org/sqlite` (pure Go, CGo-free, single static binary)

**Rationale:** No CGo dependency means cross-compilation to Android/ARM64 is
trivial. `mattn/go-sqlite3` requires CGo. `modernc.org/sqlite` is the
CGo-free port maintained by the same SQLite team members.

**ORM/query builder:** [`github.com/jmoiron/sqlx`](https://github.com/jmoiron/sqlx)
for lightweight struct scanning. Avoid GORM — too opinionated. Raw `database/sql`
with sqlx is idiomatic Go and keeps SQL visible.

**Schema** (Forge's own; only API contract must match opencode):

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS project (
    id           TEXT PRIMARY KEY,      -- ulid/slug
    worktree     TEXT NOT NULL,
    name         TEXT,
    time_created INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000),
    time_updated INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000)
);

CREATE TABLE IF NOT EXISTS session (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    directory       TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    agent           TEXT,
    model_id        TEXT,
    provider_id     TEXT,
    cost            REAL NOT NULL DEFAULT 0,
    tokens_input    INTEGER NOT NULL DEFAULT 0,
    tokens_output   INTEGER NOT NULL DEFAULT 0,
    tokens_cache_read  INTEGER NOT NULL DEFAULT 0,
    tokens_cache_write INTEGER NOT NULL DEFAULT 0,
    time_created    INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000),
    time_updated    INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000),
    time_archived   INTEGER
);

CREATE INDEX IF NOT EXISTS session_project_idx ON session(project_id);

CREATE TABLE IF NOT EXISTS message (
    id           TEXT PRIMARY KEY,      -- monotonic ascending (same ID scheme as opencode)
    session_id   TEXT NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,         -- 'user' | 'assistant'
    data         TEXT NOT NULL,         -- JSON blob matching MessageV2.Info shape
    time_created INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000),
    time_updated INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS message_session_time_idx ON message(session_id, time_created, id);

CREATE TABLE IF NOT EXISTS part (
    id           TEXT PRIMARY KEY,
    message_id   TEXT NOT NULL REFERENCES message(id) ON DELETE CASCADE,
    session_id   TEXT NOT NULL,
    type         TEXT NOT NULL,         -- 'text'|'tool'|'step-start'|etc.
    data         TEXT NOT NULL,         -- JSON blob matching MessageV2.Part shape
    time_created INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000),
    time_updated INTEGER NOT NULL DEFAULT (unixepoch('now', 'subsec') * 1000)
);

CREATE INDEX IF NOT EXISTS part_message_idx ON part(message_id, id);
CREATE INDEX IF NOT EXISTS part_session_idx ON part(session_id);
```

**Cursor pagination** (matching `message-v2.ts:595-610`):
```go
type Cursor struct {
    ID   string `json:"id"`
    Time int64  `json:"time"`
}
// Encode: base64.RawURLEncoding(json.Marshal(cursor))
// WHERE (time_created < ?) OR (time_created = ? AND id < ?)
```

**Migrations:** use `golang-migrate/migrate` with embedded SQL files
(`embed.FS`). Migrations run on startup before the server accepts connections.

**Connection:** single `*sql.DB` (WAL allows concurrent readers). Write
operations serialize through a dedicated goroutine or a mutex; reads are
concurrent.

### 6. Auth

**Env vars** (source: `server/auth.ts:17-20`):
- `OPENCODE_SERVER_PASSWORD` — required for auth to be active
- `OPENCODE_SERVER_USERNAME` — default `"opencode"`

**Middleware** (`authorization.ts:9,83-86`):

```go
func authMiddleware(username, password string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !checkAuth(r, username, password) {
                w.Header().Set("WWW-Authenticate", `Basic realm="Secure Area"`)
                http.Error(w, "", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}

func checkAuth(r *http.Request, wantUser, wantPass string) bool {
    // 1. ?auth_token=base64(user:pass)
    if token := r.URL.Query().Get("auth_token"); token != "" {
        decoded, err := base64.StdEncoding.DecodeString(token)
        if err == nil { return checkUserPass(string(decoded), wantUser, wantPass) }
    }
    // 2. Authorization: Basic <base64>
    u, p, ok := r.BasicAuth()
    if ok { return u == wantUser && p == wantPass }
    return false
}
```

Auth is skipped when `OPENCODE_SERVER_PASSWORD` is empty (server runs
unauthenticated; log a warning on startup per `serve.ts:15`).

**PTY connect endpoint** has a separate auth path: it accepts a short-lived
one-time ticket via `?pty_connect_ticket=` query param (see
`server/shared/pty-ticket.ts`) in addition to regular auth. Forge implements
this as a `map[string]time.Time` with 60-second TTL.

### 7. Per-directory instance routing and cache

An **instance** is the in-memory state (config, event bus, PTY sessions, LSP
clients) for one project directory.

**Cache:** `sync.Map` keyed by the canonical (absolute, `filepath.Clean`) path.
Each entry is a `*instanceEntry` with a `singleflight`-style init:

```go
type instanceEntry struct {
    mu      sync.Mutex
    ready   chan struct{}  // closed when ctx is initialised or failed
    ctx     *InstanceContext
    err     error
}
```

Equivalent to opencode's `Deferred`-based single-flight (`instance-store.ts:112-117`).

**Init** is triggered by the first request for a directory. On failure the entry
is removed so the next request retries.

**Directory resolution** (source: `workspace-routing.ts:87`):
```
1. ?directory query param
2. x-opencode-directory header (URL-decoded; accept raw or %xx-encoded)
3. fallback: process working directory at startup
```

The v1 SDK sends `x-opencode-directory: encodeURIComponent(dir)` and rewrites
GET requests to `?directory=<value>`. Both forms must be accepted.

**Dispose:** on `server.instance.disposed`, remove from cache and emit the event
to `GlobalBus` subscribers.

**Middleware ordering:**
```
authMiddleware → directoryMiddleware → instanceMiddleware → handler
```

### 8. Event bus and SSE endpoints

**Instance bus:** per-instance publish/subscribe using `github.com/cespare/go-broadcast`
or a custom fan-out over `chan []byte`. Each subscriber gets its own buffered
channel (size 256). Slow subscribers are dropped (non-blocking send with a drop counter).

**Global bus:** a single process-level fan-out carrying `GlobalEvent` (with
optional `directory`/`project`/`workspace` envelope). SSE clients on
`/global/event` subscribe here.

**Event payload** (source: `bus/index.ts:24-28`, `bus/global.ts:5-8`):
```json
{ "id": "evt_01...", "type": "server.connected", "properties": {} }
```
The `GlobalEvent` wrapper (`directory`, `project`, `workspace`) is stripped before
sending to SSE clients. The `id` is auto-assigned if absent (source: `global.ts:15-17`).

**Instance `/event` endpoint** (source: `handlers/event.ts`):
1. Subscribe to instance wildcard bus.
2. Send `server.connected`.
3. Merge live events with 10s heartbeat ticker.
4. On `server.instance.disposed` event type → close stream.

**Global `/global/event` endpoint** (source: `handlers/global.ts:36-66`):
1. Subscribe to GlobalBus.
2. Send `server.connected`.
3. Merge live events (`event.payload`) with 10s heartbeat.
4. No dispose terminator (global stream lives for the server lifetime).

**ID generation:** IDs are prefixed ascending ULIDs (`evt_<ulid>`). Use
`github.com/oklog/ulid/v2` with monotonic entropy.

### 9. Server lifecycle and mDNS

**Startup sequence:**
1. Load global config.
2. Open/migrate SQLite.
3. Bind HTTP listener (prefer port 4096; fall back to OS-assigned if unavailable).
4. If `mdns=true` and hostname is not loopback: publish mDNS via
   `github.com/grandcat/zeroconf` (service type `_http._tcp`, host
   `opencode.local`, TXT `path=/`).
5. Print `opencode server listening on http://<host>:<port>` (exact format for
   client compat).
6. Block on signal.

**Graceful shutdown** (SIGINT / SIGTERM):
1. Unpublish mDNS.
2. Stop accepting new connections (`srv.Shutdown(ctx)` with 10s timeout).
3. Close all active WebSocket connections (send close frame).
4. Flush/close SSE streams.
5. Call `disposeAll` on instance store (waits for in-flight agent ops; has its
   own timeout in plan 02).
6. Close SQLite.

**mDNS library:** `github.com/grandcat/zeroconf` is pure Go, no platform SDK.
Alternative: `github.com/hashicorp/mdns`. Choose zeroconf for richer TXT support.

### 10. OpenAPI spec generation

Rather than hand-writing all route types, generate Go server interfaces from
`packages/sdk/openapi.json` (131 operations) using
[`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) configured to
emit **server interfaces only** (not client code). Keep the generated types in
`internal/api/gen/` and implement each interface in `internal/api/handlers/`.

For drift detection, emit the running daemon's spec at `/openapi.json` (using
`github.com/swaggo/swag` or hand-rolled from the same OpenAPI JSON embedded as
`embed.FS`) and diff it against the reference in CI.

---

## Data model

Primary tables (canonical Forge schema; JSON blob fields match opencode's wire
format to preserve client compat):

```
project     (id PK, worktree, name, time_created, time_updated)
session     (id PK, project_id→project, slug, directory, title, agent,
             model_id, provider_id, cost, tokens_*, time_created,
             time_updated, time_archived)
message     (id PK, session_id→session, role, data JSON, time_created,
             time_updated)
part        (id PK, message_id→message, session_id, type, data JSON,
             time_created, time_updated)
```

The `data` JSON blob in `message` matches `MessageV2.Info` (User | Assistant)
from `message-v2.ts`; in `part` it matches `MessageV2.Part`. The Go structs
mirror these shapes exactly so the HTTP response can be marshalled without
transformation.

Key IDs: 26-char uppercase ULIDs with fixed prefix (`ses_`, `msg_`, `prt_`,
`prj_`) for type-safety and sorted iteration. Same semantic as opencode's
`Identifier.create` approach.

Cursor pagination: `base64url(json({"id": "...", "time": <ms>}))` per
`message-v2.ts:563-578`.

---

## Implementation milestones

Each milestone is independently runnable and testable. Ship in order.

### M1 — Skeleton: health + static config (≈ 2 days)
- `cmd/forge/main.go` with `serve` subcommand.
- HTTP server on port 4096 (fallback to 0).
- `GET /global/health` → `{ "healthy": true, "version": "0.0.1" }`.
- `GET /config` → return parsed global config as JSON.
- Auth middleware (OPENCODE_SERVER_PASSWORD).
- Directory header/query parsing; log which instance would be used.
- JSONC config loader + merge.
- **Test:** `curl /global/health`; point opencode CLI at `http://localhost:4096`.

### M2 — SQLite persistence + project/session CRUD (≈ 3 days)
- `internal/storage/` package: open DB, run migrations, WAL pragmas.
- `internal/session/` package: `Create`, `Get`, `List`, `Update` for sessions.
- HTTP handlers:
  - `POST /session` → create
  - `GET /session` → list (cursor-paginated)
  - `GET /session/{id}` → get
  - `DELETE /session/{id}` → delete
  - `GET /session/{id}/messages` → list messages (stub: empty)
- **Test:** create/list/get/delete sessions; verify cursor pagination correctness.

### M3 — Instance routing + instance cache (≈ 2 days)
- `internal/instance/` package: cache + single-flight init.
- `directoryMiddleware` resolving `x-opencode-directory` / `?directory`.
- `instanceMiddleware` wiring `InstanceContext` into request context.
- **Test:** two concurrent requests for different directories create two
  independent instances; repeated requests reuse cached instance.

### M4 — Event bus + SSE (≈ 2 days)
- `internal/bus/` package: per-instance fan-out + global bus.
- `GET /event` SSE: `server.connected` + heartbeat + live events.
- `GET /global/event` SSE: same on global bus.
- `server.instance.disposed` emitted on instance teardown.
- **Test:** subscribe to `/event`, trigger a publish from a test handler, verify
  event received. Verify heartbeat fires in 10 seconds. Verify stream closes on
  dispose.

### M5 — PTY WebSocket (≈ 3 days)
- `internal/pty/` package: spawn shell via `github.com/creack/pty`, ring buffer,
  subscriber map.
- `POST /pty` create, `GET /pty`, `GET /pty/{id}`, `DELETE /pty/{id}`.
- `GET /pty/{id}/connect` WebSocket upgrade, replay, control frame.
- `?cursor` param handling; `-1` semantics.
- **Test:** connect two WS clients simultaneously; verify both receive output;
  verify cursor-replay correctness; verify 2MB buffer trim.

### M6 — mDNS + graceful shutdown (≈ 1 day)
- mDNS publish/unpublish on start/stop.
- Signal handler + clean shutdown sequence.
- Startup warning when no password set.
- **Test:** `avahi-browse` / `dns-sd` sees the service; SIGINT causes clean exit
  with all connections closed.

### M7 — OpenAPI stubs + spec emission (≈ 2 days)
- Run `oapi-codegen` against `openapi.json`; commit generated interfaces.
- Implement all remaining stub handlers (return `501 Not Implemented` with
  proper error envelope for endpoints not yet covered).
- Embed reference `openapi.json`; serve at `/openapi.json`.
- **Test:** `diff <(curl /openapi.json) packages/sdk/openapi.json` shows only
  expected deviations.

---

## Testing

This plan proves the following claims; deep test harness spec lives in plan 12.

**Functional:**
- Auth: `?auth_token=base64(user:pass)` and `Authorization: Basic ...` both
  accepted; unauthenticated request returns 401 with `WWW-Authenticate` header.
- Directory routing: `x-opencode-directory: %2Ftmp%2Ffoo` and
  `?directory=%2Ftmp%2Ffoo` both resolve to `/tmp/foo`; missing directory falls
  back to cwd.
- Instance cache: 100 concurrent requests for the same directory complete with
  exactly one `loadInstance` call (single-flight verification via counter).
- SSE: `server.connected` is always the first event; heartbeat arrives within
  11s of connection; `server.instance.disposed` closes the stream.
- PTY framing: control frame `bytes[0] == 0x00`; data frames are plain UTF-8;
  cursor replay covers exactly the buffered range; 2MB trim works correctly.
- Cursor pagination: page boundaries stable when new messages arrive during
  iteration.
- Config merge: `instructions` arrays are concatenated; scalar fields are
  overridden; OPENCODE_CONFIG_CONTENT wins over file configs.
- DB pragmas applied: `PRAGMA journal_mode` returns `wal`.

**Performance (defer profiling details to plan 11):**
- SSE fan-out to 50 concurrent subscribers with 1000 events/s: p99 latency < 5ms.
- SQLite cursor pagination of 10k messages: p99 < 20ms.
- Instance cache hot path (cached directory): p99 < 1ms overhead.

**Compatibility (formal conformance in plan 12):**
- opencode's unmodified JS SDK (`createOpencodeClient`) connects to Forge and
  can list sessions and subscribe to events without errors.
- opencode TUI (`opencode attach <forge-url>`) connects and renders.
- `diff /openapi.json reference/openapi.json` produces no unexpected deltas.

---

## Verification

Concrete commands to prove this plan works, in order:

```bash
# 1. Build
go build -o forge ./cmd/forge

# 2. Health check (no auth)
./forge serve &
curl -s http://localhost:4096/global/health | jq .
# → { "healthy": true, "version": "..." }

# 3. Auth check
OPENCODE_SERVER_PASSWORD=secret ./forge serve &
curl -s -u opencode:secret http://localhost:4096/global/health | jq .
curl -s "http://localhost:4096/global/health?auth_token=$(echo -n 'opencode:secret' | base64)" | jq .
curl -s http://localhost:4096/global/health  # → 401

# 4. Session CRUD
curl -s -X POST http://localhost:4096/session \
  -H 'x-opencode-directory: /tmp/test-project' \
  -H 'Content-Type: application/json' \
  -d '{}' | jq .id
curl -s http://localhost:4096/session | jq .

# 5. SSE stream (verify server.connected + heartbeat)
curl -sN http://localhost:4096/event \
  -H 'x-opencode-directory: /tmp/test-project' &
sleep 12; kill %  # wait for one heartbeat

# 6. PTY WebSocket (using websocat or wscat)
SESSION_ID=$(curl -sX POST http://localhost:4096/pty \
  -H 'x-opencode-directory: /tmp/test-project' \
  -d '{}' | jq -r .id)
websocat "ws://localhost:4096/pty/$SESSION_ID/connect?cursor=0"
# → control frame then shell prompt

# 7. Point opencode SDK at Forge (interop proof)
node -e "
const { createOpencodeClient } = require('@opencode-ai/sdk');
const c = createOpencodeClient({ baseUrl: 'http://localhost:4096', directory: '/tmp/test-project' });
c.session.list().then(r => console.log('sessions:', r)).catch(console.error);
"

# 8. Point opencode TUI at Forge (interop proof)
opencode attach http://localhost:4096

# 9. mDNS discovery (macOS)
dns-sd -B _http._tcp local
# → should list opencode-4096 at opencode.local

# 10. OpenAPI drift check
diff <(curl -s http://localhost:4096/openapi.json | jq -S .) \
     <(cat packages/sdk/openapi.json | jq -S .) | head -50
```

---

## Risks and open questions

| # | Risk | Likelihood | Mitigation |
|---|------|-----------|------------|
| 1 | PTY buffer cursor semantics: opencode counts UTF-16 code units (JS string `.length`) not bytes; Go's `len(string)` counts bytes | Medium | Validate against test vectors in plan 12; use rune-count or byte-count consistently and document the choice |
| 2 | `x-opencode-directory` encoding: v1 SDK uses `encodeURIComponent`, v2 uses the same; server must URL-decode both | Low | Tested in M3; decodeURIComponent is idempotent for plain paths |
| 3 | SSE race on subscribe-before-publish: opencode uses eager subscribe (acquires PubSub before sending `server.connected`) to close the window; Go channel approach must do the same | Medium | Implement subscribe-then-send in the handler, not send-then-subscribe |
| 4 | mDNS on Linux: `grandcat/zeroconf` requires root or CAP_NET_BIND_SERVICE for port 5353 | Medium | Document; fall back to dnsmasq or avahi-publish wrapper |
| 5 | SQLite write contention with concurrent agent writes (plan 02) | Low for M1-M7 | WAL + mutex-serialized writes; revisit in plan 11 |
| 6 | `oapi-codegen` schema coverage: complex `anyOf`/`oneOf` in openapi.json may not code-gen cleanly | Medium | Inspect generated output; use `allOf` workarounds; fall back to `interface{}` where needed |
| 7 | Global event SSE wrapper: `GlobalBus.emit` wraps payload in `{ directory, project, workspace, payload }` but SSE sends only `event.payload`; Forge must NOT send the wrapper | Low | Validated against source; add regression test |
| 8 | Port 4096 conflict on mobile (Android) | Low | Mobile connects to a remote Forge; default port only matters on the server host |

**Open questions:**
- Should Forge implement the `/sync/*` and `/experimental/*` endpoints? Default:
  return `501` for now; revisit in plan 09.
- Should the instance cache have a TTL (evict idle instances)? opencode keeps
  them for server lifetime. Decision: match opencode (no TTL) for now; add
  LRU-with-TTL in plan 13 for remote/multi-tenant scenarios.
- Is plan 07 (mobile) blocked on M4 (event bus) or is M1 (health + session list)
  sufficient to start? Answer: M1 + M2 unblocks mobile session listing; M4
  unblocks real-time updates.

---

## Review pass (2026-06-03) — resolved ambiguities & remaining gaps

Plan 01 is **built and green**. Several items above still read as "open" but the implementation has
since settled them; record the resolution so they stop being treated as undecided.

**Resolved in code (update mental model accordingly):**
- **Risk #1 (PTY cursor units) — RESOLVED: UTF-16 code units.** `internal/pty/pty.go:4-6`,
  `internal/pty/doc.go:8-9` count UTF-16 code units via `unicode/utf16` so replay offsets line up
  with opencode (`pty/index.ts:239-262`). The "rune vs byte" question is closed; cite the conformance
  finding, not the open risk.
- **Open question: `/sync/*` + `/experimental/*`** — implemented as a generic **`501 NotImplemented`**
  for any unimplemented operation (`internal/server/server_test.go:71` asserts the `NotImplemented`
  error tag). This is the contract; promote it from "revisit in plan 09" to "decided," and make the
  masterplan cross-cutting ambiguity (v1/sync/experimental) point here.

**Still genuinely open (carry forward, with owners):**
- **Instance-cache TTL** (open question 2): still "match opencode = no TTL." Real for remote/
  multi-tenant; keep owned by plan 13, but add a conformance note that idle instances are never
  evicted so memory growth is expected, not a leak.
- **Risk #7 (global-event wrapper)** says Forge must send only `event.payload`, not the
  `{directory,project,workspace,payload}` wrapper. Confirm a regression test exists for
  `/global/event` payload shape; if not, that is the one missing validation in this plan.
- **`gofmt`/drift gate.** M7's "diff /openapi.json" test should be hardened to the repo workflow's
  `make gen` + `git diff --exit-code internal/api/gen/` so generated-stub drift fails CI, not just a
  manual `diff`.

**Validation:** strong already. The only addition: every endpoint family that returns 501 today
should have a positive conformance assertion that opencode clients degrade gracefully (don't crash)
on 501 — otherwise "best-effort" compatibility is unverified.

## Links to sibling plans

- **Plan 00** (`00-masterplan.md`): Vision, contract, architecture, sequencing.
  This plan implements Phase A of the roadmap.
- **Plan 02** (`02-agent-engine.md`): Plugs into the instance context and event
  bus created here; requires M2 (persistence) and M4 (SSE) to be complete.
- **Plan 06** (`06-sdk-generation.md`): Generates Go + Kotlin/Swift SDKs from
  the `openapi.json` embedded by M7.
- **Plan 09** (`09-integration.md`): Wires all components together; depends on
  graceful shutdown (M6) and the instance lifecycle defined here.
- **Plan 12** (`12-test-compatibility.md`): Conformance harness runs against this
  daemon; needs M1 at minimum, full suite needs M7.
- **Plan 13** (`13-remote-ops.md`): Remote hardening (TLS, reverse proxy, push
  notifications) builds on the server lifecycle from M6.
