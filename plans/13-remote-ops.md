# Forge Plan 13 — Remote-First Hardening & Ops

> **Status:** design-ready, implementation in Phase D (after plan 02 agent loop is green)
>
> **Audience:** Go engineers building the Forge daemon; Android/mobile client engineers (plan 07)
>
> **Motivation:** opencode is loopback-first and TS/Bun-native. Forge is explicitly
> remote-first and mobile-primary. This plan hardens the daemon for that posture:
> TLS, stronger auth, push notifications for mobile, resilient reconnection/replay,
> LAN discovery, and single-binary packaging. Nothing here breaks wire-compat with
> existing opencode clients.

---

## Context

Forge's core transport (REST+SSE+WS PTY) is specified in plan 01 and must remain
wire-compatible with the opencode v2 API (frozen in `packages/sdk/openapi.json`).
This plan layers security, reliability, and operational concerns on top of that
transport without changing any endpoint shapes.

Remote access is not an afterthought. The primary client is an Android phone; the
daemon often runs on a developer's workstation or a cloud VM. Every default and
every dial must be chosen with that topology in mind.

---

## opencode References Validated (file:line + takeaways)

### Network options

`packages/opencode/src/cli/network.ts:5-32`  
Default bind `127.0.0.1` (line 14); default port `0` resolves to 4096 then any free
port; `--hostname 0.0.0.0` opt-in; `--mdns` forces hostname to `0.0.0.0` when not
explicitly set (line 54); `--mdns-domain` (default `opencode.local`); `--cors` accepts
multiple origins. All flags fall back to `config.server.*` if the flag was not
explicitly passed (lines 49-59). Forge must replicate this precedence:
`flag > config-file > env > built-in default`.

`packages/opencode/src/cli/cmd/serve.ts:14-16`  
No password emits a console warning: `"Warning: OPENCODE_SERVER_PASSWORD is not set;
server is unsecured."` Forge must emit the same warning and block `--hostname 0.0.0.0`
without a password (opencode warns; Forge must refuse/block to be more secure).

### Auth model

`packages/opencode/src/server/auth.ts:17-33`  
Config reads `OPENCODE_SERVER_PASSWORD` (Option) and `OPENCODE_SERVER_USERNAME`
(default `"opencode"`). `required()` is true when password Option is Some and non-empty.
`authorized()` does plain string compare — no timing-safe compare. Forge must use
`subtle.ConstantTimeCompare` / `hmac.Equal`.

`packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts:9-86`  
Auth is Basic (header) or `?auth_token=base64(user:pass)` query param. Both paths
use `decodeCredential()` (base64 → `user:pass` split). `authorizationRouterMiddleware`
skips public UI paths (line 114); `ptyConnectAuthorizationLayer` skips Basic check
when a PTY ticket is present (line 147). Forge must honour the same skip rules for
wire-compat.

`packages/opencode/src/server/shared/pty-ticket.ts:1-15`  
Path pattern `/pty/:id/connect` skips Basic Auth when `?ticket=` query param is present.
Ticket is a `crypto.randomUUID()` with 60-second TTL, single-use, capacity 10 000
(line 9-10 of `packages/opencode/src/pty/ticket.ts`). Forge must replicate: issue
ticket via `POST /pty/:id/ticket`, consume-and-delete on WebSocket upgrade.

### Desktop sidecar security model

`packages/desktop/src/main/sidecar.ts:54-84`  
Desktop spawns the daemon as a utility process bound to `127.0.0.1` on a dynamic port
with a random password injected via `OPENCODE_SERVER_PASSWORD` env var. `cors` is set
to `["oc://renderer"]` (the Electron renderer origin). The parent-process IPC channel
(`postMessage`) carries start/stop commands; the password never appears in a command
line argument.

`packages/desktop/src/main/server.ts:204-228`  
`checkHealth()` polls `GET /global/health` with Basic auth before marking the sidecar
ready. Forge's local CLI mode must do the same: spawn daemon, poll `/global/health`,
then hand off the URL to the TUI/shell.

### Client reconnection & heartbeat

`packages/app/src/context/server-sdk.tsx:41-44`  
Constants: `FLUSH_FRAME_MS=16`, `STREAM_YIELD_MS=8`, `RECONNECT_DELAY_MS=250`.
Heartbeat timeout is `HEARTBEAT_TIMEOUT_MS=15_000` ms (line 103). The client aborts
the current SSE attempt on heartbeat timeout and retries after 250 ms (lines 107-115,
193-194). The `visibilitychange` handler triggers reconnect when the page becomes
visible after a timeout (lines 208-215).

`packages/opencode/src/server/routes/instance/httpapi/handlers/global.ts:45-52`  
Server emits `server.heartbeat` every 10 seconds. Forge must do the same; mobile
clients set HEARTBEAT_TIMEOUT_MS=15s so a 10s server interval gives comfortable
headroom.

### /sync/* endpoint semantics

`packages/opencode/src/server/routes/instance/httpapi/groups/sync.ts:11-112`  
Four endpoints:
- `POST /sync/start` — triggers workspace sync loops for active sessions.
- `POST /sync/replay` — accepts `{ directory, events: NonEmptyArray<ReplayEvent> }`;
  events must have contiguous `.seq` values for one `aggregateID`; replays into local
  state (lines 34-57 of `handlers/sync.ts`).
- `POST /sync/steal` — reassigns a session to the current workspace ID.
- `POST /sync/history` — body is `Record<aggregateID, lastKnownSeq>`; returns events
  with `seq > lastKnownSeq` for known aggregates, full history for unknown (lines 79-93
  of `handlers/sync.ts`).

`packages/opencode/src/sync/index.ts:86-101`  
`replay()` enforces `event.seq === latest + 1` (strict monotone), owner check, and
idempotency (seq <= latest → skip). `replayAll()` enforces same-aggregate constraint
and contiguous seq range.

These semantics are the durability contract. Forge's implementation must match exactly
for plan-12 conformance.

### mDNS

`packages/opencode/src/server/mdns.ts:1-58`  
Uses `bonjour-service` npm package. Publishes service type `_http._tcp`, name
`opencode-<port>`, host `opencode.local` (or custom domain), TXT `{ path: "/" }`.
Forge uses `github.com/grandcat/zeroconf` (pure Go, no CGO, `_opencode._tcp`
service type with the same TXT).

---

## Threat Model & Security Posture

### Assets
1. Agent sessions and their full message/file history (SQLite).
2. PTY access — full shell on the host machine.
3. LLM provider API keys (stored in config, passed to providers).
4. Source code in watched directories.

### Threats

| Threat | opencode handling | Forge target |
|--------|-------------------|--------------|
| Unauthenticated remote access | Warn if no password; loopback default | Block `0.0.0.0` bind without password; warn on loopback |
| Credential sniffing (HTTP) | None — no TLS | TLS opt-in; Tailscale/proxy recommended for remote |
| Timing attacks on password compare | Plain string compare (`auth.ts:30`) | `subtle.ConstantTimeCompare` |
| Token reuse / session hijack | Basic creds are long-lived | Bearer tokens with revocation; PTY tickets are single-use 60s |
| CSRF | CORS allowlist | Same CORS allowlist; SameSite cookie flag N/A (not cookies) |
| PTY ticket replay | Single-use invalidation | Same — consume-and-delete on WS upgrade |
| Compromised mobile device | N/A (no mobile) | Token revocation endpoint; device pairing per device |
| Log leakage | N/A | Structured logs must not emit passwords or tokens |

### Defaults
- Bind `127.0.0.1` always unless `--hostname` or `--mdns` is explicitly set.
- `0.0.0.0` bind **requires** a password; daemon refuses to start otherwise (unlike
  opencode which only warns).
- No TLS by default; TLS opt-in via `--tls-cert`/`--tls-key` or `--tls-acme`.
- Recommended remote posture: Tailscale (mutual WireGuard, no open port), SSH tunnel,
  or reverse proxy (Caddy/nginx) with TLS termination.

---

## Remote Access Topologies

### 1. Loopback-only (default, sidecar mode)

```
Mobile client (ADB forward / local) → 127.0.0.1:4096
```

Desktop app and TUI spawn the daemon on loopback. No TLS needed (loopback is
encrypted by the OS on modern kernels). Random password generated by the launcher,
passed via env (mirror `packages/desktop/src/main/sidecar.ts:54-84`).

**Tradeoff:** Secure by default; useless for remote access.

### 2. Direct TLS (self-signed or ACME)

```
Mobile → forge.example.com:4096 (TLS)
Daemon  → listen 0.0.0.0 --tls-acme forge.example.com
```

Forge embeds `golang.org/x/crypto/acme/autocert` for Let's Encrypt, and
`crypto/tls` self-signed generation for LAN use. Self-signed certs are pinned
on first connect (TOFU — Trust On First Use) on the mobile client (plan 07).

**Tradeoff:** Requires open port; no VPN overhead; ACME needs port 80/443 or DNS
challenge.

**Implementation:** `--tls-cert path --tls-key path` for BYO cert; `--tls-acme domain`
for Let's Encrypt (uses `autocert.Manager`); `--tls-self-signed` for dev TOFU. When
any TLS flag is set, the HTTP listener is replaced by `tls.NewListener`.

### 3. Tailscale (recommended for remote)

```
Mobile (Tailscale app) → forge-machine.tailnet:4096 (WireGuard)
Daemon  → bind on tailscale0 IP or 0.0.0.0, password required
```

No daemon changes needed beyond `--hostname <tailscale-ip>`. Tailscale provides
mutual auth (user/device certificates), encrypted tunnel, and MagicDNS. This is
the recommended topology for mobile remote access:

- Zero open ports on the daemon host.
- No TLS cert management.
- Daemon logs `tailscale` as the detected interface type.

Document: `forge serve --hostname $(tailscale ip -4) --port 4096`.

**Tradeoff:** Requires Tailscale on both ends; not available on all networks.

### 4. Cloudflare Tunnel (zero-config public URL)

```
Mobile → https://forge.example.workers.dev (Cloudflare edge)
         ← cloudflared tunnel → 127.0.0.1:4096
Daemon → loopback (no open port)
```

`cloudflared tunnel --url http://localhost:4096` creates a public HTTPS URL with
Cloudflare TLS. The daemon stays on loopback; the Forge daemon needs no changes.
Forge docs should include a one-liner setup.

**Tradeoff:** Traffic transits Cloudflare; free for personal use. No offline access.

### 5. SSH Tunnel (mirrors opencode's `ssh` connection type)

`packages/app/src/context/server.tsx:99-104` defines `ServerConnection.Ssh` — the
desktop app can SSH into a remote host and expose a local proxy port. Forge documents
the equivalent:

```
ssh -L 4096:127.0.0.1:4096 user@dev-machine -N
# Then connect mobile via localhost:4096 with adb forward
```

Or the reverse tunnel pattern:
```
ssh -R 4096:127.0.0.1:4096 user@mobile-gateway -N
```

**Tradeoff:** Requires SSH access; works everywhere; no extra software on daemon host.

### 6. Reverse Proxy (Caddy or nginx, production)

```
Mobile → https://forge.example.com (Caddy, TLS via Let's Encrypt)
          → proxy_pass http://127.0.0.1:4096
```

Caddy example `Caddyfile`:
```
forge.example.com {
    reverse_proxy localhost:4096
    header {
        X-Accel-Buffering no    # required for SSE
    }
}
```

nginx requires `proxy_buffering off; proxy_read_timeout 3600s;` for SSE.

**Key:** SSE streams must not be buffered. Forge emits `X-Accel-Buffering: no` and
`Cache-Control: no-cache, no-transform` on the event endpoint (matching
`handlers/global.ts:58-64`).

---

## Auth — Stronger Than Basic for Remote

### Compatibility layer (keep for wire-compat)

Forge keeps Basic Auth + `?auth_token=base64(user:pass)` exactly as opencode does
(`authorization.ts:83-86`). PTY ticket flow (issue + consume) is also preserved. These
let unmodified opencode clients (TUI, web, desktop) connect to Forge.

### Bearer token layer (new for mobile)

Mobile clients use a long-lived bearer token rather than a password on every request.
This avoids embedding the raw password in every HTTP header and enables revocation.

**Flow:**
```
POST /auth/token
  Authorization: Basic base64(user:pass)
  Body: { "device_name": "Pixel 9", "device_id": "uuid" }

→ 200 { "token": "fgt_...", "token_id": "uuid", "expires_at": null }
```

Tokens are stored in daemon's SQLite: `(token_id, token_hash, device_name, device_id,
created_at, last_used_at, revoked_at)`. Token value is `fgt_` + 32 random bytes
base64url. Only the SHA-256 hash is stored.

Middleware: `Authorization: Bearer fgt_...` is accepted alongside Basic. Timing-safe
compare via `subtle.ConstantTimeCompare(storedHash, sha256(presented))`.

**Revocation:**
```
DELETE /auth/token/:token_id
  Authorization: Bearer <any valid token>

GET /auth/tokens
  → [{ token_id, device_name, last_used_at, created_at }]
```

### Device-pairing flow for mobile (QR code)

Mobile first-launch: user does not know the password. Desktop TUI / web UI generates a
one-time pairing code (6 digits, 5-minute TTL):

```
POST /auth/pair/start   (requires Basic or existing Bearer)
→ { "code": "847291", "expires_in": 300 }

POST /auth/pair/complete  (no auth required — uses the code)
  Body: { "code": "847291", "device_name": "Pixel 9", "device_id": "uuid" }
→ { "token": "fgt_...", "token_id": "uuid" }
```

Mobile scans a QR code encoding the daemon URL + pairing code, completes the exchange,
and stores the bearer token in Android Keystore (plan 07). The pairing code is
single-use and invalidated on first use or expiry.

**Implementation:** pairing codes in an in-memory `sync.Map` (no persistence needed —
codes are ephemeral). The QR code encodes `{ "url": "https://...", "code": "847291" }`.

### opencode-compat auth_token query param

The `?auth_token=` param continues to work — it is used by the TUI and web clients
(`authorization.ts:83`). Forge accepts both Basic-encoded and bearer-encoded values
here for the transition period.

---

## Push Notifications Design

### Motivation

A mobile client on a flaky network (subway, flight mode) needs to know when:
- The agent finishes a task (`session.status` → `idle`).
- The agent asks for permission (`permission.asked`).
- The agent asks a question (`question.asked`).
- The daemon needs user input to continue.

SSE covers this when the client is connected. Push covers it when the client is
backgrounded or offline.

### Architecture

```
Daemon SSE event bus
       │
       ▼
[Push filter goroutine]  ─── evaluates event type + user preferences
       │
       ▼
[Notification queue]     ─── SQLite: (id, token, payload, created_at, sent_at, acked_at)
       │
       ▼
[Push dispatcher]        ─── calls FCM HTTP v1 API
       │
       ▼
Firebase Cloud Messaging ──► Android device (plan 07)
```

### Event → Notification Mapping

| SSE event type | Push title | Push body |
|----------------|-----------|-----------|
| `session.status` where status transitions to `idle` | "Agent finished" | Session title or first 60 chars of last assistant message |
| `permission.asked` | "Permission needed" | Tool name + description |
| `question.asked` | "Agent has a question" | Question text truncated to 120 chars |
| `global.disposed` | "Forge daemon stopped" | "Reconnect when ready" |

Only events for sessions the device is registered to follow are dispatched (default:
all sessions).

### Device Registration

```
POST /push/register
  Authorization: Bearer <token>
  Body: {
    "device_id": "uuid",
    "fcm_token": "fcm-registration-token",
    "platform": "android",
    "session_filter": ["all"]  // or ["sessionID", ...]
  }

DELETE /push/register/:device_id   // unregister
```

Device registrations stored in SQLite: `(device_id, bearer_token_id, fcm_token,
platform, session_filter, registered_at, last_refreshed_at)`.

### Dispatcher

The push dispatcher runs as a background goroutine started at daemon boot. It polls
the notification queue every 5 seconds (or wakes on `NOTIFY`-style channel signal
from the filter goroutine).

FCM HTTP v1 API:
```
POST https://fcm.googleapis.com/v1/projects/{project_id}/messages:send
Authorization: Bearer <google-oauth2-token>
Content-Type: application/json

{
  "message": {
    "token": "<device-fcm-token>",
    "notification": { "title": "...", "body": "..." },
    "data": { "event_type": "session.status", "session_id": "..." },
    "android": { "priority": "high" }
  }
}
```

Google OAuth2 token obtained via `golang.org/x/oauth2/google` with a service-account
JSON key (`FORGE_FCM_SERVICE_ACCOUNT` env var or `--fcm-service-account` flag).

**Offline delivery:** FCM queues messages for up to 4 weeks for offline Android devices.
The Forge notification queue marks `sent_at` once FCM returns 200; `acked_at` is set
when the mobile client calls `POST /push/ack/:notification_id`. Unacked notifications
older than 30 days are pruned.

**No FCM configured:** if `FORGE_FCM_SERVICE_ACCOUNT` is not set, the push subsystem
is disabled and the `/push/*` endpoints return 503. Log a startup notice.

**Rate limiting:** max 1 notification per device per session per minute to avoid
flooding during rapid agent loops.

---

## Reconnection & Replay (/sync/* Parity)

### SSE reconnection (matches client behaviour)

`packages/app/src/context/server-sdk.tsx:100-199` implements:
- On SSE stream failure: wait `RECONNECT_DELAY_MS=250ms`, retry indefinitely.
- If no heartbeat within 15 000ms: abort current attempt and reconnect.

Forge must emit `server.heartbeat` every 10 seconds (matching `handlers/global.ts:45`).
The heartbeat payload is `{ id, type: "server.heartbeat", properties: {} }`.

### Event cursor / buffering

Forge's SSE stream does not replay missed events on reconnect today (opencode has the
same gap — `server.connected` is the only reconnect signal). The `/sync/*` endpoints
fill this gap for session state, but raw SSE events are not buffered in opencode.

Forge adds a lightweight in-memory ring buffer per active SSE stream:
- Ring buffer capacity: 1 000 events, 5-minute retention.
- Client sends `Last-Event-ID` HTTP header (standard SSE field) on reconnect.
- Forge replays buffered events with `id ≤ Last-Event-ID` skipped, ids > that value
  replayed in order, then live events resume.
- If the `Last-Event-ID` has aged out of the buffer, Forge emits `server.connected`
  (triggering full client re-sync, same behaviour as today).

This is an **additive, non-breaking extension** — opencode clients that ignore
`Last-Event-ID` continue to work; mobile clients that send it get lossless reconnect.

### /sync/* implementation

Forge implements all four sync endpoints with identical semantics:

**`POST /sync/start`** — triggers workspace sync loops. In Forge, this is a no-op
returning `true` in single-workspace mode; extended in multi-workspace mode.

**`POST /sync/replay`** — validate contiguous seq range for single aggregateID; apply
each event via the projector registry; publish to SSE bus. Strict monotone check
(`expected = latest + 1`), idempotency (seq ≤ latest → skip).

**`POST /sync/steal`** — reassign session's `workspaceID` field via a sync event.

**`POST /sync/history`** — query SQLite `event_table` for `seq > lastKnownSeq` per
aggregateID; return full history for unknown aggregates.

SQLite schema mirrors `packages/opencode/src/sync/event.sql.ts`:
```sql
CREATE TABLE event_sequence (
    aggregate_id TEXT PRIMARY KEY,
    seq          INTEGER NOT NULL,
    owner_id     TEXT
);
CREATE TABLE event (
    id           TEXT PRIMARY KEY,
    seq          INTEGER NOT NULL,
    aggregate_id TEXT NOT NULL REFERENCES event_sequence(aggregate_id),
    type         TEXT NOT NULL,
    data         JSONB NOT NULL
);
CREATE INDEX event_aggregate_seq ON event(aggregate_id, seq);
```

Go struct:
```go
type SyncEvent struct {
    ID          string         `db:"id"`
    Seq         int64          `db:"seq"`
    AggregateID string         `db:"aggregate_id"`
    Type        string         `db:"type"`
    Data        map[string]any `db:"data"`
}
```

---

## Discovery

### LAN: mDNS

**opencode:** `bonjour-service` publishes `_http._tcp`, name `opencode-<port>`, host
`opencode.local`, TXT `{ path: "/" }` (mdns.ts:17-23).

**Forge:** `github.com/grandcat/zeroconf` (pure Go, no CGO). Service type
`_opencode._tcp` (keep same for client compat; also advertise `_http._tcp` as an alias
for non-Forge browsers). Instance name `forge-<port>`. TXT records:
```
path=/
auth=required   // or "open"
version=1
```

Mobile client (plan 07) uses `android.net.nsd.NsdManager` to browse `_opencode._tcp`
and `_http._tcp`. On discovery, stores `{ url, txtVersion }` and prompts the user.

**CLI flag parity:**
```
forge serve --mdns                      # enables mDNS, forces 0.0.0.0
forge serve --mdns-domain mydev.local   # custom host announced in mDNS
```

`--mdns` with no explicit `--hostname` defaults to `0.0.0.0` (same as opencode,
`network.ts:53-55`). Requires password or daemon refuses.

### WAN: Manual URL entry

For remote (non-LAN) access, the mobile app provides a URL entry screen:
1. User enters `http://192.168.1.100:4096` or `https://forge.example.com`.
2. App normalizes (adds `http://` if missing, strips trailing slash) — mirrors
   `packages/app/src/context/server.tsx:10-15`.
3. App calls `GET /global/health` with the user's credentials to validate before saving.
4. Stores `{ url, token_id, encrypted_token }` in Android EncryptedSharedPreferences.

QR-code shortcut: desktop TUI / web UI generates a QR encoding
`{ "url": "...", "code": "<pairing-code>" }`. Mobile scans, enters the pairing flow.

---

## Packaging & Ops

### Single static Go binary

```
goreleaser release --clean
```

`goreleaser` config produces:
- `linux/amd64`, `linux/arm64` (primary targets for server + Raspberry Pi)
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64`

Build flags: `CGO_ENABLED=0 GOFLAGS="-trimpath"`. Embed `FORGE_VERSION` via
`-ldflags "-X main.version=$(git describe --tags)"`.

Binary size target: < 40 MB uncompressed (no CGO, no embedded JS/TS runtime). SQLite
via `modernc.org/sqlite` (pure Go, no CGO).

Artifact naming: `forge_linux_amd64.tar.gz`, etc. Checksums in `checksums.txt`.
GitHub Releases via goreleaser + `gh release`.

Install one-liner:
```sh
curl -fsSL https://forge.dev/install.sh | sh
```

### Container image

```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot
COPY forge /forge
ENTRYPOINT ["/forge"]
```

Multi-arch manifest via `goreleaser`'s `dockers` + `docker_manifests` config.
Published to `ghcr.io/forge-dev/forge:{version}` and `:latest`.

Usage:
```sh
docker run -d \
  -p 4096:4096 \
  -v ~/.config/forge:/home/nonroot/.config/forge \
  -v ~/projects:/projects \
  -e FORGE_SERVER_PASSWORD=secret \
  ghcr.io/forge-dev/forge:latest \
  serve --hostname 0.0.0.0 --port 4096
```

### systemd unit (Linux)

`/etc/systemd/system/forge.service`:
```ini
[Unit]
Description=Forge AI coding daemon
After=network.target

[Service]
Type=simple
User=forge
ExecStart=/usr/local/bin/forge serve --hostname 127.0.0.1 --port 4096
Restart=on-failure
RestartSec=5s
Environment=FORGE_SERVER_PASSWORD_FILE=/etc/forge/password
EnvironmentFile=-/etc/forge/env

# Hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/forge

[Install]
WantedBy=multi-user.target
```

`forge install-service` command generates and installs the unit, writes a random
password to `/etc/forge/password` (0600), and runs `systemctl enable --now forge`.

### launchd plist (macOS)

`~/Library/LaunchAgents/dev.forge.daemon.plist` generated by `forge install-service`.
Uses `KeepAlive` + `StandardOutPath`/`StandardErrorPath` for logs.

### Windows Service

`golang.org/x/sys/windows/svc` wraps the daemon. `forge install-service` calls
`sc create` and starts the service. Password stored in Windows Credential Manager
via `github.com/danieljoos/wincred`.

### Configuration: flags → env → file

Priority: CLI flag > env var > config file > built-in default (same as opencode's
precedence in `network.ts:45-61`).

Config file location: `$FORGE_CONFIG` env > `$XDG_CONFIG_HOME/forge/config.yaml` >
`~/.config/forge/config.yaml` (Linux/macOS) > `%APPDATA%\forge\config.yaml` (Windows).

YAML structure (subset):
```yaml
server:
  hostname: "127.0.0.1"
  port: 4096
  mdns: false
  mdns_domain: "opencode.local"
  cors: []
  tls:
    cert: ""
    key: ""
    acme_domain: ""
  password: ""          # prefer env FORGE_SERVER_PASSWORD
  username: "opencode"

logging:
  level: "info"         # debug|info|warn|error
  format: "json"        # json|text

push:
  fcm_service_account: ""  # prefer env FORGE_FCM_SERVICE_ACCOUNT
```

Env vars: `FORGE_SERVER_PASSWORD`, `FORGE_SERVER_USERNAME`, `FORGE_SERVER_PORT`,
`FORGE_SERVER_HOSTNAME`, `FORGE_FCM_SERVICE_ACCOUNT`, `FORGE_LOG_LEVEL`,
`FORGE_LOG_FORMAT`, `FORGE_DB` (path to SQLite).

### Structured logging

`log/slog` (stdlib, Go 1.21+) with `slog.JSONHandler` in production and
`slog.TextHandler` for TTY-attached terminals. Log levels: `debug`, `info`, `warn`,
`error`. Never log: passwords, tokens, token hashes, FCM tokens, user message content.

Request logging: method, path, status, latency. Auth failures logged at `warn` with IP
but not the presented credential.

### Health & metrics endpoints

**`GET /global/health`** — already in the opencode spec (`global.ts:37`). Returns:
```json
{ "healthy": true, "version": "0.1.0" }
```
Forge adds: no auth required on this endpoint (matches opencode; the desktop sidecar
polls it without auth in some paths — `server.ts:204-228` passes password optionally).

**`GET /global/metrics`** (Forge extension, not in opencode spec, not surfaced in
generated OpenAPI spec to avoid drift). Returns Prometheus text format. Protected by
the same auth middleware. Metrics:

```
forge_http_requests_total{method,path,status} counter
forge_http_request_duration_seconds{method,path} histogram
forge_sse_connections_active gauge
forge_sse_events_emitted_total counter
forge_push_notifications_sent_total{status} counter
forge_agent_sessions_active gauge
forge_sync_replays_total counter
```

Use `github.com/prometheus/client_golang/prometheus` and `promhttp.Handler()`.

Scrape config for Prometheus:
```yaml
- job_name: forge
  static_configs:
    - targets: ["localhost:4096"]
  metrics_path: /global/metrics
  basic_auth:
    username: opencode
    password_file: /etc/forge/password
```

### Graceful shutdown

On `SIGINT` / `SIGTERM`:
1. Stop accepting new HTTP connections (`net/http.Server.Shutdown` with 30s context).
2. Signal all active agent loops to flush and stop (context cancellation).
3. Flush the notification queue (attempt to send pending push notifications, 5s timeout).
4. Close all active PTY sessions with `server.heartbeat` drain.
5. Close SQLite with WAL checkpoint.
6. Exit 0.

Emit `global.disposed` SSE event before step 1 so connected clients know to reconnect.

---

## Implementation Milestones (Ordered)

> Phase D — after plan 02 agent loop conformance is green.

| # | Milestone | Description | Depends on |
|---|-----------|-------------|------------|
| 13.1 | Auth hardening | `subtle.ConstantTimeCompare`, block `0.0.0.0` without password, PTY ticket flow | plan 01 |
| 13.2 | Bearer tokens | `POST /auth/token`, `GET /auth/tokens`, `DELETE /auth/token/:id`, SQLite token table | 13.1 |
| 13.3 | Device pairing | `POST /auth/pair/start`, `POST /auth/pair/complete`, QR payload | 13.2 |
| 13.4 | TLS support | `--tls-cert/key`, `--tls-self-signed`, `--tls-acme` via `autocert` | 13.1 |
| 13.5 | SSE cursor / ring buffer | `Last-Event-ID` replay, 1000-event ring buffer per stream | plan 01 |
| 13.6 | /sync/* endpoints | All four endpoints with exact opencode semantics | plan 02 |
| 13.7 | mDNS | `zeroconf` publish on `--mdns`, `_opencode._tcp` + `_http._tcp` | 13.1 |
| 13.8 | Push notifications | Device registration, notification queue, FCM dispatcher | 13.2 |
| 13.9 | Heartbeat | 10s `server.heartbeat` on SSE streams | plan 01 |
| 13.10 | Prometheus metrics | `/global/metrics`, all counters/gauges/histograms | plan 01 |
| 13.11 | Graceful shutdown | `SIGTERM` handler, `global.disposed` SSE event | plan 02 |
| 13.12 | goreleaser | Multi-arch binary + container, `goreleaser.yaml`, GitHub Actions | — |
| 13.13 | Service installation | `forge install-service` for systemd / launchd / Windows | 13.12 |
| 13.14 | Config file | YAML config loader with flag > env > file precedence | 13.1 |

---

## Testing

### Functional

| Test | Method |
|------|--------|
| Auth bypass attempt (no creds) | `curl -s http://127.0.0.1:4096/global/health` against auth-required server; expect 401 |
| Basic auth correct | `Authorization: Basic base64(opencode:password)`; expect 200 |
| Basic auth wrong password | timing-safe: measure response time vs correct; delta < 1ms |
| `?auth_token=` compat | Token query param accepted same as Basic header |
| Bearer token lifecycle | Issue token, use token, revoke token, verify 401 post-revoke |
| Device pairing happy path | Start pairing, complete with code, use returned token |
| Device pairing code expiry | Complete pairing after 5 minutes; expect 410 Gone |
| PTY ticket single-use | Issue ticket, connect WS (succeeds), reconnect with same ticket (fails) |
| Block `0.0.0.0` without password | `forge serve --hostname 0.0.0.0`; expect startup error |
| TLS self-signed TOFU | `--tls-self-signed`; client pins cert; reconnect succeeds |
| mDNS advertise | Browse `_opencode._tcp` on LAN; find forge instance |
| CORS allowlist | Request from unlisted origin; expect 403 |

### Performance

| Test | Method | Target |
|------|--------|--------|
| SSE fan-out | 100 concurrent SSE clients; emit 10 events/s; measure latency p99 | p99 < 50ms |
| Auth middleware overhead | 10 000 req/s with Basic auth; measure additional latency | < 0.5ms/req |
| Push dispatcher throughput | Enqueue 1 000 notifications; measure time to FCM dispatch | < 5s |
| Sync replay 10k events | `POST /sync/replay` with 10 000 events; measure time | < 2s |
| Ring buffer under pressure | 1 000-event buffer, 1 000 events/s burst; oldest events expired correctly | no OOM |

### Compatibility (plan 12 conformance)

| Scenario | How |
|----------|-----|
| opencode unmodified TUI connects to Forge | `opencode attach http://localhost:4096` with Basic auth; check all sessions visible |
| opencode web client connects to Forge | Point browser at `http://localhost:4096`; SSE stream runs; no JS errors |
| Forge mobile client connects to real opencode | Use bearer token on opencode... it falls back to Basic (token not supported there); confirm graceful fallback |
| Simulate network drop + resume | Drop loopback route for 30s; restore; verify SSE reconnects and `Last-Event-ID` replay delivers missed events |
| `sync/history` after disconnect | Disconnect for 5 min, reconnect, call `POST /sync/history`; verify no missed events |

---

## Verification (Concrete)

1. **Binary size:** `go build -o forge . && ls -lh forge` → must be < 40 MB.
2. **Auth regression:** `go test ./internal/auth/...` with subtests for constant-time
   compare, token lifecycle, and pairing flow.
3. **`0.0.0.0` block:** `forge serve --hostname 0.0.0.0` exits non-zero when no
   password is configured; CI catches regressions.
4. **mDNS round-trip:** integration test starts daemon with `--mdns`, browses via
   `zeroconf.Browse`, asserts service discovered within 5s.
5. **SSE heartbeat:** integration test connects to `/global/event`, waits 12s, asserts
   `server.heartbeat` received (must arrive within 11s of connect or last heartbeat).
6. **`Last-Event-ID` replay:** integration test: subscribe SSE → disconnect after 3
   events → reconnect with `Last-Event-ID: <id-of-event-2>` → assert event 3 replayed.
7. **`/sync/history` parity:** record golden fixture from real opencode `/sync/history`
   response; replay identical body against Forge; diff responses (plan 12 pattern).
8. **goreleaser dry run:** `goreleaser release --snapshot --clean` in CI; assert all
   platform binaries produced.
9. **Container health:** `docker run --rm forge serve &` → `curl /global/health` → 200.
10. **Graceful shutdown timing:** send `SIGTERM`, measure time to `Exit 0` ≤ 35s.

---

## Risks & Open Questions

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| FCM service account key management in self-hosted deployments | Medium | Push subsystem is opt-in; daemon runs fine without it |
| ACME Let's Encrypt rate limits in dev environments | Low | `--tls-self-signed` for dev; ACME only for prod |
| Ring buffer memory pressure with many concurrent SSE connections | Medium | Cap at 1 000 events per stream; evict by age (5 min); add metric `forge_sse_buffer_evictions_total` |
| `zeroconf` on Linux without mDNS daemon (no avahi) | Medium | `zeroconf` uses raw multicast sockets; test on minimal Docker; document that `avahi-daemon` is not required |
| Windows service credential storage | Low | `wincred` is well-maintained; fallback to env var |
| opencode clients expect `_http._tcp` not `_opencode._tcp` | High | Advertise both; `_http._tcp` is the fallback |
| Mobile bearer token exposed if device storage compromised | Medium | Android Keystore protects token; revocation available; instruct users to revoke on lost device |
| Prometheus metrics leaking session metadata | Low | Metrics are aggregate (counts/histograms); no session IDs in label values |

**Open questions:**

1. **APNs:** Should the push subsystem also support Apple Push Notification service for
   a future iOS client? Design should keep the dispatcher interface abstract
   (`Dispatcher` interface with `FCM` and `APNS` implementations). Decision: defer to
   plan 07 iOS follow-up, but design the interface now.

2. **Multi-user:** opencode is single-user (one `OPENCODE_SERVER_PASSWORD`). Should
   Forge support multiple named users with per-user session isolation? Decision needed
   before 13.2 (token schema assumes single user today).

3. **Sync semantics for Forge-only features:** the `/sync/*` endpoints were designed for
   workspace sync (experimental in opencode). Should Forge support multi-workspace? If
   yes, `sync/start` must do real work. Decision: implement no-op returning `true` for
   now; flag-gate multi-workspace for later.

4. **`goreleaser` vs manual Makefile:** goreleaser adds a tool dependency. Alternative:
   hand-rolled `Makefile` + `xgo`. Decision: goreleaser is the standard; accept the
   dependency.

---

## Review pass (2026-06-03) — milestone overlap, config format, OAuth ownership

Phase D, not started — but several milestones are **already built in plan 01**, and a couple of
specs contradict reality.

**Already done elsewhere — do not rebuild, reframe as "harden":**
- **13.7 mDNS** — built (`internal/mdns/`, plan 01 M6). Remaining delta is only advertising **both**
  `_opencode._tcp` and `_http._tcp` (risk row) — scope 13.7 down to that.
- **13.9 Heartbeat** — built (10s `server.heartbeat`, plan 01 M4).
- **13.11 Graceful shutdown** — built (`SIGTERM` + dispose, plan 01 M6).
- **13.1 Auth (partial)** — Basic + `?auth_token=` already work (plan 01); the *new* work here is
  constant-time compare, `0.0.0.0`-without-password block, and the PTY ticket flow.
Update the milestone table to mark these as "harden/extend," not greenfield, so they aren't
re-implemented.

**Config format contradiction.** 13.14 specifies a **YAML** config loader. Reality and the
wire-compat mandate are **JSONC at opencode paths** (`internal/config/config.go`). A separate YAML
config would fork the config story. Keep the flag > env > file **precedence**, but the file format is
JSONC — not YAML. (Same correction as plans 09/10.)

**This plan now owns the OAuth loopback/callback story.** Per the masterplan review, the ownerless
OAuth callback problem (MCP M3-2 remote auth + provider-auth `oauth/authorize`+`/callback`) is
assigned here, because the loopback-redirect server interacts directly with remote hardening
(reachability, TLS, port-forwarding). Add an explicit milestone: a shared OAuth loopback callback
server that both MCP and provider auth use, with the remote-reachability caveat (plan 03 risk #3)
addressed (e.g. `--oauth-callback-proxy-url`).

**`/sync/*` — DECIDED: return `501 NotImplemented`** until 13.5/13.6 implement real semantics (no
no-op `true`). Per masterplan "Decisions locked" #1; resolves open-question #3 below.

**Multi-user — DECIDED: single-user, matching opencode** (masterplan "Decisions locked" #3). The
13.2 token schema is a single namespace of bearer tokens for the one user; no per-user isolation.
Resolves open-question #2 below.

**OAuth — DECIDED: deferred, API keys only for now** (masterplan "Decisions locked" #4). When picked
up, the shared loopback callback server lives here (plan 13), with optional `--oauth-callback-proxy-url`
for remote reachability. **Note:** verification uses
`go build -o forge` but the binary is **`forged`** (`cmd/forged`) — align with plan 09.

## Links to Sibling Plans

- **Plan 00** (`00-masterplan.md`) — sequencing context; this plan is Phase D.
- **Plan 01** (`01-daemon-core.md`) — the HTTP/SSE/WS transport this plan hardens.
  Auth middleware, CORS, and heartbeat are implemented there; this plan adds the token
  layer and ring buffer on top.
- **Plan 07** (`07-client-mobile.md`) — Android client; consumes bearer tokens, device
  pairing QR flow, push notification registration, `Last-Event-ID` reconnect, mDNS
  browsing, and Tailscale topology.
- **Plan 12** (`12-test-compatibility.md`) — conformance harness; auth bypass tests,
  `sync/history` golden fixtures, SSE replay scenarios, and the check that opencode
  unmodified clients connect to Forge are all hooks for plan 12's test runner.
