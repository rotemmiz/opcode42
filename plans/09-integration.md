# Plan 09 — Integration: How the Components Wire Together

> Scope: end-to-end wiring of plans 01–08 inside the Opcode42 Go daemon and across the
> build/release pipeline. Read this alongside every sibling plan; the sequence diagram
> and milestone table here are the authoritative cross-plan integration contract.

---

## Context

Opcode42 is a single static Go binary (`opcoded`) that owns the full request lifecycle:
HTTP transport → instance/directory router → agent engine → tool registry →
MCP/LSP clients and plugin-host sidecar. Plans 01–08 each own a vertical slice;
this plan defines how those slices are composed, the exact in-process service
boundaries, and what must be true at the end of each delivery phase for the system
to be coherent end-to-end.

The reference for every claim below is the opencode source tree at
`/Users/rotemmiz/git/opencode`. Key files cited throughout:
- Transport + auth: `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts`
- Directory routing: `packages/opencode/src/server/routes/instance/httpapi/middleware/workspace-routing.ts`
- SSE bus: `packages/opencode/src/server/routes/instance/httpapi/handlers/event.ts`
- Prompt entry point: `packages/opencode/src/session/prompt.ts`
- LLM stream processor: `packages/opencode/src/session/processor.ts`
- Wire contract: `packages/sdk/openapi.json` (113 path entries, 22 230 lines)

---

## Component Dependency Graph

```
Plan 07 (Mobile)  ──────────────────────────────────────────────┐
Plan 08 (TUI)     ──────────────────────────────────────────────┤
                                                                 ▼
Plan 06 (SDK)  ─── generated Go server stubs ──► Plan 01 (Daemon-Core)
                └─ generated Kotlin/Swift   ──► Plan 07 (Mobile)
                                                                 │
                              ┌──────────────────────────────────┤
                              ▼                                  │
                    Plan 02 (Agent-Engine)                        │
                       │         │                               │
                    Plan 03    Plan 04                           │
                  (MCP/LSP)  (Resources)                         │
                       │         │                               │
                    Plan 05 (Plugin-Host)                        │
                              (sidecar)                          │
                                                                 │
Plan 12 (Compatibility) ─── test harness drives all ────────────┘
Plan 10 (Functional) ─── tests all plans ──────────────────────┘
Plan 11 (Performance) ─── benchmarks plan 01+02 ───────────────┘
```

**Hard build-time dependencies** (must be satisfied before compilation):

| Consumer | Depends on |
|----------|-----------|
| Plan 01 daemon | Plan 06 Go server stubs (OpenAPI → `oapi-codegen`) |
| Plan 02 agent engine | Plan 01 `SessionStore`, `BusService`, `PermissionService` |
| Plan 03 MCP/LSP | Plan 02 `ToolRegistry` interface |
| Plan 04 resources | Plan 01 config loader, filesystem watcher |
| Plan 05 plugin-host | Plan 04 plugin discovery, Plan 01 sidecar supervisor |
| Plan 07 mobile | Plan 06 Kotlin SDK |
| Plan 08 TUI | Plan 06 Go client or direct HTTP |
| Plans 10/11/12 | All plans, mock LLM provider from Plan 02 |

---

## Internal Service Boundaries

All services live in the same Go process. Boundaries are Go interfaces, not RPC.
The composition root (`cmd/opcoded/main.go`) wires them via constructor injection.

```
┌─────────────────────────────────────────────────────────────────────┐
│  opcoded process                                                      │
│                                                                      │
│  ┌─────────────┐   ┌──────────────────────────────────────────────┐ │
│  │ HTTP layer  │   │  Instance layer (one per directory)          │ │
│  │ (Plan 01)   │   │                                              │ │
│  │  net/http   │   │  ┌────────────┐   ┌──────────────────────┐  │ │
│  │  or fasthttp│──►│  │ BusService │   │  SessionStore        │  │ │
│  │             │   │  │ (pub/sub)  │   │  (SQLite, plan 01)   │  │ │
│  │  Auth MW    │   │  └─────┬──────┘   └──────────┬───────────┘  │ │
│  │  Dir router │   │        │                     │              │ │
│  │  SSE fan-out│   │  ┌─────▼──────────────────────▼──────────┐  │ │
│  └─────────────┘   │  │        AgentEngine  (Plan 02)          │  │ │
│                    │  │  LLM stream → processor → tool loop    │  │ │
│  ┌─────────────┐   │  │  PermissionService                     │  │ │
│  │  PTY layer  │   │  │  QuestionService                       │  │ │
│  │  (Plan 01)  │   │  └──────────┬──────────────┬─────────────┘  │ │
│  │  WS + PTY   │   │             │              │                │ │
│  └─────────────┘   │  ┌──────────▼──┐  ┌───────▼──────────────┐ │ │
│                    │  │ ToolRegistry │  │  ResourceLoader      │ │ │
│  ┌─────────────┐   │  │ (Plan 02)    │  │  (Plan 04)           │ │ │
│  │  Config     │   │  │ built-in     │  │  agents/commands/    │ │ │
│  │  (Plan 01)  │   │  │ + MCP tools  │  │  rules/skills/       │ │ │
│  │  watcher    │   │  └──────┬───────┘  │  providers           │ │ │
│  └─────────────┘   │         │          └──────────────────────┘ │ │
│                    │  ┌──────▼───────────────────────────────┐   │ │
│                    │  │  MCPClient / LSPClient  (Plan 03)     │   │ │
│                    │  │  subprocess lifecycle owned here      │   │ │
│                    │  └──────────────────────────────────────┘   │ │
│                    └──────────────────────────────────────────────┘ │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  PluginHostSidecar  (Plan 05)                               │    │
│  │  Node/Bun child process — supervised by daemon              │    │
│  │  JSON-RPC over stdio or Unix socket                         │    │
│  └─────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

### Key interface contracts (Go)

```go
// Plan 01 → Plan 02
type BusService interface {
    Publish(ctx context.Context, event Event) error
    Subscribe(ctx context.Context) (<-chan Event, CancelFunc)
    SubscribeAll(ctx context.Context) (<-chan Event, CancelFunc)
}

// Plan 01 → Plan 02
type SessionStore interface {
    Create(ctx context.Context, req CreateSessionRequest) (Session, error)
    Get(ctx context.Context, id SessionID) (Session, error)
    UpdateMessage(ctx context.Context, msg Message) (Message, error)
    UpdatePart(ctx context.Context, part Part) (Part, error)
    UpdatePartDelta(ctx context.Context, delta PartDelta) error
}

// Plan 02 → Plan 03
type ToolRegistry interface {
    Register(tool Tool)
    Get(name string) (Tool, bool)
    List() []Tool
    // MCPTools returns tools from all connected MCP servers
    MCPTools(ctx context.Context) ([]Tool, error)
}

// Plan 02 → permission UI gate
type PermissionService interface {
    Ask(ctx context.Context, req PermissionRequest) (PermissionReply, error)
    Reply(ctx context.Context, id PermissionID, reply Reply) error
}
```

---

## End-to-End Request Flow

### POST /session/:id/prompt with tool call and permission gate

Reference: `prompt.ts:286-300`, `processor.ts:305-449`, `event.ts:21-53`,
`authorization.ts:81-87`, `workspace-routing.ts:86-88`.

```
Mobile client                Opcode42 daemon (Go)                  LLM API
     │                              │                              │
     │  POST /session/abc/prompt    │                              │
     │  Authorization: Basic ...    │                              │
     │  x-opencode-directory: /p    │                              │
     │─────────────────────────────►│                              │
     │                              │                              │
     │                         [Auth MW]                           │
     │                         decode Basic / auth_token           │
     │                         validate against ServerAuth.Config  │
     │                              │                              │
     │                         [Dir Router]                        │
     │                         read x-opencode-directory header    │
     │                         (or ?directory= query param)        │
     │                         resolve/start Instance for /p       │
     │                              │                              │
     │                         [HTTP Handler]                      │
     │                         deserialize PromptPayload           │
     │                         call AgentEngine.Prompt(...)        │
     │                              │                              │
     │                         [AgentEngine]                       │
     │                         create UserMessage → store (SQLite) │
     │                         publish message.updated SSE event   │
     │                         build system prompt (Plan 04 rules) │
     │                         build tool list (ToolRegistry)      │
     │                         call LLM.Stream(...)                │
     │                              │─────────────────────────────►│
     │                              │  POST /messages (streaming)  │
     │                              │◄─────────────────────────────│
     │                              │  text-delta events           │
     │                         [Processor]                         │
     │                         on text-delta:                      │
     │                           updatePartDelta → bus.Publish     │
     │◄────────────────────────── SSE: part.updated ──────────────│
     │                              │                              │
     │                         on tool-call event:                 │
     │                           create ToolPart (status=pending)  │
     │                           → bus.Publish part.updated        │
     │◄────────────────────────── SSE: part.updated ──────────────│
     │                              │                              │
     │                         [PermissionService.Ask]             │
     │                         check ruleset (Plan 04 rules)       │
     │                         if not auto-approved:               │
     │                           store Permission record           │
     │                           bus.Publish permission.asked      │
     │◄────────────────────────── SSE: permission.asked ──────────│
     │                              │                              │
     │  POST /session/abc/          │                              │
     │       permissions/xyz        │                              │
     │  {"response":"once"}         │                              │
     │─────────────────────────────►│                              │
     │                              │                              │
     │                         [PermissionService.Reply]           │
     │                         unblock Ask() Deferred              │
     │                              │                              │
     │                         [Tool execution]                    │
     │                         execute tool (read_file, bash, etc) │
     │                         update ToolPart (status=completed)  │
     │                         → bus.Publish part.updated          │
     │◄────────────────────────── SSE: part.updated ──────────────│
     │                              │                              │
     │                         [Processor continues]               │
     │                         send tool result back to LLM        │
     │                              │─────────────────────────────►│
     │                         receive next LLM response           │
     │                              │◄─────────────────────────────│
     │                         on finish-step:                     │
     │                           update AssistantMessage           │
     │                           bus.Publish message.updated       │
     │◄────────────────────────── SSE: message.updated ───────────│
     │                              │                              │
     │  (sync) HTTP 200 body:       │                              │
     │  JSON(MessageWithParts)      │                              │
     │◄─────────────────────────────│                              │
```

**SSE fan-out**: All connected clients for the same directory instance receive
every bus event via `GET /event`. The SSE handler (`event.ts:21-53`) subscribes
eagerly at connection time (before the response body pump starts) to close the
race window where events published during connection handshake could be lost. The
Opcode42 implementation must replicate this: subscribe to BusService inside the
request handler, before returning the streaming response.

**Sync vs async**: `POST /session/:id/prompt` blocks until the agent loop
finishes and returns the final message JSON. `POST /session/:id/prompt_async`
forks immediately and returns 204; all progress arrives via SSE. Both share the
same engine code path.

---

## Subprocess Supervision: MCP, LSP, Plugin-Host

All subprocesses are owned and supervised by the daemon. The supervision contract:

### MCP servers (Plan 03)
- **Startup**: lazy, on first tool call referencing the server, or eagerly at
  instance init if `mcp.startOnInit = true` in config.
- **Transport**: stdio (most common) or HTTP/SSE. For stdio: `os/exec.Cmd` with
  `stdin/stdout` pipes; JSON-RPC 2.0 framing.
- **Health**: no built-in keepalive in MCP spec; Opcode42 pings with
  `initialize` or a no-op custom method on a configurable interval.
- **Restart**: exponential backoff (1s, 2s, 4s, max 30s). After 5 consecutive
  failures the server is marked `error` and a `mcp.updated` event is published.
- **Shutdown**: `SIGTERM` → 5s drain → `SIGKILL`. On daemon shutdown all MCP
  subprocesses are terminated before the HTTP listener closes.

### LSP servers (Plan 03)
- **Startup**: per file-type on first LSP request. One server per language per
  directory instance.
- **Transport**: stdio; JSON-RPC 2.0 with LSP framing (Content-Length headers).
- **Lifecycle**: `initialize` → `initialized` → normal operation →
  `shutdown` + `exit` on daemon shutdown.
- **Events published**: `lsp.updated` SSE event whenever diagnostics change.

### Plugin-host sidecar (Plan 05)
- **Startup**: on demand, when a TS plugin is loaded. One shared sidecar per
  daemon instance (not per directory).
- **Transport**: JSON-RPC over Unix socket (preferred) or stdin/stdout.
- **Restart**: same backoff policy as MCP. Plugin-host failures are non-fatal;
  affected plugin calls return errors, not daemon crashes.
- **Isolation**: sidecar runs as same OS user; no sandbox today. Feature-flagged
  (`OPCODE_PLUGIN_HOST_ENABLED=1`).

All three supervisor implementations share a common `SubprocessSupervisor`
interface in `internal/supervisor/`:

```go
type SubprocessSupervisor interface {
    Start(ctx context.Context, cfg SubprocessConfig) (SubprocessHandle, error)
    Stop(ctx context.Context, handle SubprocessHandle) error
    Status(handle SubprocessHandle) SubprocessStatus
}
```

---

## Config Propagation

Config flows top-down at startup and on hot-reload (file watcher triggers).

```
~/.config/opcode42/config.toml   (global, plan 01)
    └── .opcode42/config.toml    (project, plan 01)  ← merged, project wins
            │
            ├──► AuthConfig       → auth middleware (plan 01)
            ├──► ProviderConfigs  → LLM provider factories (plan 02)
            ├──► MCPConfigs       → MCP supervisor (plan 03)
            ├──► LSPConfigs       → LSP supervisor (plan 03)
            ├──► AgentConfigs     → agent loader (plan 04)
            ├──► CommandConfigs   → command loader (plan 04)
            ├──► RuleConfigs      → permission ruleset (plan 02)
            ├──► SkillConfigs     → skill loader (plan 04)
            └──► PluginConfigs    → plugin-host sidecar (plan 05)
```

Hot-reload behaviour:
- Provider and permission rule changes take effect on next agent call.
- MCP/LSP config changes trigger supervisor reconcile (start/stop servers).
- Auth config changes take effect on next request (no in-flight impact).
- A `config.updated` SSE event is published after every reload so clients
  can refresh their state.

---

## Build and Release Integration

### Single binary
`opcoded` is a single static Go binary embedding:
- All Go packages (plans 01–05, 08 TUI assets if compiled in).
- Default config schema (JSON Schema, embedded via `embed.FS`).
- OpenAPI spec (`packages/sdk/openapi.json`, embedded for self-description).
- No Node/Bun runtime — the plugin-host sidecar is a separately shipped artifact
  (see below).

```
cmd/opcoded/
    main.go          ← composition root
internal/
    transport/       ← plan 01
    session/         ← plan 01 (store, bus)
    agent/           ← plan 02
    tool/            ← plan 02
    permission/      ← plan 02
    mcp/             ← plan 03
    lsp/             ← plan 03
    resource/        ← plan 04
    plugin/          ← plan 05 supervisor
    config/          ← plan 01
    supervisor/      ← shared subprocess supervisor
pkg/
    sdk/             ← plan 06 Go stubs (generated)
```

### Build targets
```makefile
make build          # go build -o opcoded ./cmd/opcoded  (CGO_ENABLED=0)
make generate       # oapi-codegen → pkg/sdk/; buf → proto stubs if any
make test           # go test ./... (plan 10)
make bench          # go test -bench=. ./... (plan 11)
make conformance    # plan 12 harness against real opencode + opcode42
make release        # goreleaser: linux-amd64, linux-arm64, darwin-arm64
```

### Sidecar packaging
The plugin-host sidecar (`plugin-host/`) is a Node/Bun package. It is:
- Shipped as a pre-built JS bundle alongside `opcoded` in the release archive
  (`opcoded`, `plugin-host.js`, `plugin-host-node_modules/` or bundled single file).
- Located by `opcoded` via `OPCODE_PLUGIN_HOST_PATH` env var or a convention path
  (`$OPCODE_HOME/plugin-host/index.js`).
- Not embedded in the binary (too large; version-pinned separately).

### Mobile SDK release
The Kotlin SDK (plan 06) is published as a Maven artifact; the Swift SDK as a
Swift Package. Both are generated from `packages/sdk/openapi.json` via
`openapi-generator-cli` and versioned alongside `opcoded` releases.

---

## Milestone Integration Points

| Phase | End state | Cross-plan handshake |
|-------|-----------|----------------------|
| **A — Kickoff** | `opcoded` starts, serves `/global/health`, `/event` SSE, `/session` CRUD with no-op agent | Plan 01 + Plan 06 stubs must compile; Plan 07 mobile connects to real opencode daemon for development; Plan 12 harness can diff responses |
| **A — Mobile v0** | Mobile app lists sessions, connects to SSE, reads message history — all against opencode daemon | Plan 07 validates Plan 06 Kotlin SDK against real opencode; no Plan 02 dependency |
| **B — Engine** | `POST /session/:id/prompt` drives a real LLM stream through the Go processor; SSE events match opencode's catalog; Plan 12 conformance suite goes green for prompt+tool scenarios | Plan 02 integrates with Plan 01 bus+store; Plan 06 stubs used for request/response types; Plan 08 TUI dogfoods the engine |
| **C — Ecosystem** | MCP tools available in tool registry; LSP diagnostics flow; agents/commands/rules load from `.opencode/` | Plan 03 subprocess supervisors integrated into Plan 02 tool registry; Plan 04 loaders feed Plan 02 system prompt builder |
| **D — Polish** | Plugin-host sidecar loads TS plugins; `/sync/*` SSE reconnect works; Plan 12 full conformance green including PTY and auth; goreleaser publishes binary | Plan 05 integrated; Plan 13 remote hardening applied to Plan 01 transport |

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|-----------|
| SSE fan-out race (subscribe after response starts) | High | Replicate opencode's eager-subscribe pattern (event.ts:27) exactly; add integration test asserting no events lost during connect |
| Permission Deferred unblock across goroutines | High | Use `sync.Cond` or channel-based Deferred; test with concurrent permission replies |
| Plugin-host sidecar crash loop during tool call | Medium | Non-fatal error path; tool returns error result, agent continues; supervisor applies backoff |
| Config hot-reload races with in-flight agent calls | Medium | Per-instance read lock on config; agent snapshots config at call start |
| MCP server slow start blocking first tool call | Medium | Async MCP init with timeout; return tool-not-available error if init exceeds 5s |
| openapi.json drift between opencode versions | Medium | Plan 12 spec-diff CI gate catches this; pin opencode version in go.sum / git submodule |

---

## Review pass (2026-06-03) — corrections to reality + the missing Testing section

This is the cross-plan integration contract, so accuracy matters most here. Several details drifted
from the built daemon; corrected below (per CLAUDE.md's reality rule). The diagrams above are kept
as the design intent — these are the binding overrides.

### Contradictions to fix
1. **Config is JSONC at opencode paths, not TOML at opcode42 paths.** The Config Propagation section
   shows `~/.config/opcode42/config.toml` + `.opcode42/config.toml` (TOML). Reality and the wire-compat
   mandate: **`~/.config/opencode/{config.json,opencode.json,opencode.jsonc}`** global +
   **`opencode.json[c]`** walked from the worktree down (`internal/config/config.go:35-37,50-62,82`).
   Opcode42 reads opencode's own config files — that is the point. Replace all TOML/opcode42-path
   references.
2. **Permission reply endpoint is wrong in the sequence diagram.** It shows
   `POST /session/abc/permissions/xyz {"response":"once"}`. Real contract:
   **`POST /permission/{requestID}/reply`** with body **`{"reply":"once|always|reject"}`**
   (`internal/server/permission_handlers.go:16`; plan 02). Question replies similarly are
   `POST /question/{requestID}/reply`.
3. **Directory layout drift.** Actual `internal/` is `server/` (not `transport/`), `engine/` (not
   `agent/`), `engine/tool` + `engine/permission` (not top-level `tool/`/`permission/`), `sdk/` (not
   `pkg/sdk/`). `lsp/`, `plugin/`, and the shared `supervisor/` package **do not exist yet** — the
   `SubprocessSupervisor` abstraction is still aspirational (MCP spawns its own subprocess directly).
   Treat the layout table as the target, not the current tree.
4. **Binary is `opcoded` (`cmd/opcoded`)** — plan 09 is correct here; note that **plan 01's
   verification block (`opcode42`/`cmd/opcode42`) is the stale one** and should be reconciled to `opcoded`.

### Testing & Validation (this plan had none — add it)
Plan 09's validation was implicit in the phase end-states. Make it explicit as **integration tests
that exercise the composition root**, distinct from per-plan unit tests (plan 10) and conformance
(plan 12):
- **Composition-root wiring test:** boot `cmd/opcoded` in-process against a temp dir + mock LLM;
  assert every interface in "Key interface contracts" is non-nil and the instance graph builds.
- **End-to-end prompt→tool→permission flow:** the sequence diagram above, asserted event-for-event
  over `/event` (user `message.updated` → part deltas → tool `part.updated` pending →
  `permission.asked` → `POST /permission/{id}/reply` → tool completed → assistant `message.updated`),
  with the **corrected** endpoint/payload shapes.
- **Sync vs async parity:** `POST /session/:id/prompt` (blocks, returns final JSON) and
  `/prompt_async` (204 + SSE) drive identical engine paths — assert identical final state.
- **SSE eager-subscribe race:** publish an event during the connect handshake; assert no loss
  (covers Risk #1, the highest-severity integration risk).
- **Config hot-reload:** rewrite the JSONC config; assert `config.updated` SSE fires and a
  subsequent prompt sees the new providers/rules; assert in-flight calls used the snapshot.
- **Subprocess supervision:** kill an MCP child mid-call; assert backoff restart + `mcp.updated`,
  no daemon crash. (When `internal/supervisor/` lands, test the shared contract once for MCP/LSP/
  plugin-host.)
All of the above gate on the repo `CLAUDE.md` local CI mimic + a dual-run for any new endpoint.

## Links to Sibling Plans

- [01-daemon-core](01-daemon-core.md) — transport, auth, instance routing, SQLite, SSE bus
- [02-agent-engine](02-agent-engine.md) — LLM stream, processor, tool loop, permissions
- [03-ecosystem-mcp-lsp](03-ecosystem-mcp-lsp.md) — MCP/LSP subprocess management
- [04-ecosystem-resources](04-ecosystem-resources.md) — agents, commands, rules, skills, providers
- [05-plugin-host](05-plugin-host.md) — Node/Bun sidecar, TS plugin loading
- [06-sdk-generation](06-sdk-generation.md) — Go + Kotlin/Swift SDKs from openapi.json
- [07-client-mobile](07-client-mobile.md) — Android app (primary client)
- [08-client-tui](08-client-tui.md) — Go Bubble Tea TUI
- [10-test-functional](10-test-functional.md) — functional test strategy
- [11-test-performance](11-test-performance.md) — performance/load benchmarks
- [12-test-compatibility](12-test-compatibility.md) — conformance harness
