# Plan 05 — Plugin Host (Node/Bun Sidecar for opencode-format TS/JS Plugins)

> **Status:** Design / flag-gated / deferrable to Phase D.
> **Risk level:** HIGHEST of all plans — see Risks section.

---

## Context

opencode-format plugins are TypeScript/JavaScript modules that export a `server` function
(or a legacy bare function) returning a `Hooks` object. opencode runs them in-process
inside Bun using `await import(entry)`. A Go daemon cannot host them natively. This plan
describes a **Node/Bun sidecar process** ("plugin-host") that loads opencode-format plugins
and bridges their hooks to the Go daemon over a local RPC channel, plus the full lifecycle,
failure isolation, and deferral story.

Everything in this plan is **flag-gated** (`--plugin-host` / env `FORGE_PLUGIN_HOST=1`).
When the flag is off (the default in early phases), Forge behaves as a zero-plugin daemon.

---

## opencode References Validated

All citations are from the reference source at `/Users/rotemmiz/git/opencode`.

### Plugin API / Hooks

**File:** `packages/plugin/src/index.ts`

- **`PluginInput`** (lines 56–66): `{ client, project, directory, worktree, experimental_workspace, serverUrl, $ }`.
  `client` is `ReturnType<typeof createOpencodeClient>` — the SDK v2 client. `$` is `BunShell`.
- **`Plugin`** type (line 74): `(input: PluginInput, options?: PluginOptions) => Promise<Hooks>`
- **`PluginModule`** (lines 76–80): `{ id?, server: Plugin, tui?: never }` — the new module shape.
- **`Hooks`** interface (lines 222–334) — complete hook table (see bridge table below).

**File:** `packages/plugin/src/tool.ts`

- **`ToolDefinition`** (line 54): `{ description, args: ZodRawShape, execute(args, context): Promise<ToolResult> }`.
  The `execute` context includes `sessionID`, `messageID`, `agent`, `directory`, `worktree`, `abort`, `metadata(...)`, `ask(...)`.

### Plugin Loading

**File:** `packages/opencode/src/config/plugin.ts`

- **Glob scan** (lines 29–37): `Glob.scan("{plugin,plugins}/*.{ts,js}", { cwd: dir, absolute: true, dot: true, symlink: true })`.
  Discovered paths are pushed as `file://` URL specs.
- **`Spec`** type (line 12): `string | [string, Options]` — either bare identifier or `[spec, options]`.
- **`resolvePluginSpec`** (lines 50–65): path-like specs resolved relative to the config file that declared them;
  `file://` URLs kept as-is.

**File:** `packages/opencode/src/plugin/loader.ts`

- **`PluginLoader.loadExternal`** (lines 207–235): resolves and imports plugins in parallel; retries
  file plugins once if pre-import setup failed (lines 215–228). After dynamic import succeeds (or fails),
  no further retry in the same process (Bun caches failed resolution).
- **`PluginLoader.resolve`** (lines 85–131): stages `install → entry → compatibility → load`.

**File:** `packages/opencode/src/plugin/index.ts`

- **`applyPlugin`** (lines 110–121): calls `readV1Plugin` to detect new module shape; falls back to
  iterating all exports as legacy bare functions. `hooks.push(await plugin.server(input, options))`.
- **Plugin trigger** (lines 288–300): iterates `state.hooks`, calls `hook[name](input, output)` for each,
  in serial to preserve determinism.
- **Trigger call sites** (verified by grep across opencode source):
  - `"shell.env"` — `packages/opencode/src/pty/index.ts:196`, `packages/opencode/src/tool/shell.ts:414`,
    `packages/opencode/src/session/prompt.ts:615`
  - `"experimental.chat.system.transform"` — `packages/opencode/src/agent/agent.ts:397`
  - `"tool.definition"` — `packages/opencode/src/tool/registry.ts:339`
  - `"experimental.chat.messages.transform"` — `packages/opencode/src/session/compaction.ts:405`,
    `packages/opencode/src/session/prompt.ts:1433`
  - `"chat.message"` — `packages/opencode/src/session/prompt.ts:1073`
  - `"command.execute.before"` — `packages/opencode/src/session/prompt.ts:1607`
  - `"tool.execute.before"` / `"tool.execute.after"` — `packages/opencode/src/session/tools.ts:88–147`
  - `"experimental.session.compacting"` — `packages/opencode/src/session/compaction.ts:398`
  - `"experimental.compaction.autocontinue"` — `packages/opencode/src/session/compaction.ts:510`
  - `"experimental.text.complete"` — `packages/opencode/src/session/processor.ts:658`
  - `"chat.params"` / `"chat.headers"` — `packages/opencode/src/session/llm/request.ts:105,125`
  - `"tool"` (plugin-registered tools) — `packages/opencode/src/tool/registry.ts:215–220`
  - `"auth"` (plugin auth hook) — `packages/opencode/src/provider/provider.ts:1417–1433`
  - `"provider"` (plugin provider hook) — `packages/opencode/src/provider/provider.ts:1255`
  - `"dispose"` — `packages/opencode/src/plugin/index.ts:258–270`
  - `"event"` (bus broadcast) — `packages/opencode/src/plugin/index.ts:272–280`
  - `"config"` (config notify) — `packages/opencode/src/plugin/index.ts:249–256`

- **Plugin `client`** construction (lines 141–146): `createOpencodeClient({ baseUrl, directory, headers: ServerAuth.headers(), fetch: ... })` — the plugin talks back to the same daemon instance via HTTP.

### Auth

**File:** `packages/opencode/src/server/routes/instance/httpapi/middleware/authorization.ts`

- Basic Auth (line 84): `Authorization: Basic <base64(user:pass)>`
- Auth token query param (line 82): `?auth_token=<base64(user:pass)>`
- Both paths decode the same credential structure.

---

## Architecture

### Overview

```
┌─────────────────────── Forge Go Daemon ───────────────────────────┐
│  HTTP/SSE/WS server    Agent engine      Tool registry             │
│                                                                     │
│  PluginBridge (Go)                                                  │
│    - RPC client over unix socket / stdio JSON-RPC 2.0              │
│    - Hook call stubs: call → marshal → wait → unmarshal            │
│    - Tool registry entries for plugin-registered tools             │
│    - Shutdown: SIGTERM → wait 5s → SIGKILL                         │
└────────────┬────────────────────────────────────────────────────────┘
             │ JSON-RPC 2.0 over unix socket (or stdin/stdout pipe)
             │  - request: { jsonrpc, id, method, params }
             │  - response: { jsonrpc, id, result | error }
             │  - notification: { jsonrpc, method, params }  (no id)
             ▼
┌──────────────────── Plugin Host (Bun/Node process) ──────────────────┐
│  plugin-host.ts                                                        │
│    - Starts: bun run plugin-host.ts --socket /tmp/forge-ph-<pid>.sock │
│    - Loads plugins via PluginLoader.loadExternal (same code opencode  │
│      uses at packages/opencode/src/plugin/loader.ts)                  │
│    - Creates PluginInput.client → createOpencodeClient({              │
│        baseUrl: FORGE_URL, headers: Basic Auth                        │
│      })    (calls back into the Go daemon over HTTP — neat reuse)     │
│    - Exposes JSON-RPC methods: plugin.trigger, plugin.list            │
│    - Handles: tool.execute (async, returns ToolResult)               │
│    - Broadcasts: event notifications from Go → plugin hooks           │
└──────────────────────────────────────────────────────────────────────┘
```

### RPC Transport Choice

**Decision: JSON-RPC 2.0 over Unix domain socket.**

Rationale:
- Unix socket avoids port conflicts, is local-only (no network exposure), and is faster
  than stdio piping for large payloads (e.g. `messages.transform` with full chat history).
- JSON-RPC 2.0 is a minimal, well-understood protocol; implementations exist in both
  Go (`github.com/creachadair/jrpc2`) and Bun/Node (bare `net` + manual framing or
  `vscode-jsonrpc`).
- gRPC was considered and rejected: adds Protobuf compilation step, schema-versioning
  burden, and is overkill for the call volume (plugin hooks fire at most once per LLM turn).
- stdio JSON-RPC was considered and rejected: stdout multiplexing with plugin log output
  requires a framing convention; unix socket gives clean separation.

**Framing:** length-prefixed JSON over the socket: 4-byte big-endian uint32 length header
followed by UTF-8 JSON body. Simpler than HTTP chunked; avoids delimiter scanning.

**Fallback:** if unix sockets are unavailable (Windows), fall back to stdio with the same
length-prefix framing.

---

## Hook Bridge Table

Each hook is analyzed for: bridgeable? call direction? synchrony constraint?
Go must await the hook result before continuing in all cases marked **blocking**.

| Hook | Bridged? | Direction | Sync constraint | Notes |
|------|----------|-----------|-----------------|-------|
| `dispose` | Yes | Go→Host | Blocking (drain before exit) | Called on daemon shutdown; Go waits up to 5s. |
| `event` | Yes | Go→Host | **Non-blocking** (fire-and-forget) | Bus events forwarded as JSON-RPC notification; host fans out to hooks. Go does not await. |
| `config` | Yes | Go→Host | Blocking | Called once after plugins load with current config. |
| `tool` (registered tools) | Yes | Host→Go reg, Go→Host exec | Async | At load time, host sends `plugin.tools` list; Go registers stubs. On exec, Go calls `tool.execute` RPC (blocking, with per-call timeout). |
| `auth` | Partial | Host→Go (announce), Go→Host (oauth callbacks) | Complex | `auth.methods` and `auth.loader` require UI interaction flows. Bridge the `loader` (pure data transform); OAuth callback flows require additional `/auth/*` HTTP endpoints — may be deferred. |
| `provider` | Partial | Host→Go | Blocking | `provider.models` returns model list; bridge as synchronous RPC call during provider init. |
| `chat.message` | Yes | Go→Host | Non-blocking (observe only) | Called after user message is stored. Host observes; Go does not await result. |
| `chat.params` | Yes | Go→Host | **Blocking** (output mutation) | Go sends current params, awaits mutated params back. Timeout: 5s. |
| `chat.headers` | Yes | Go→Host | **Blocking** (output mutation) | Same pattern as chat.params. Timeout: 5s. |
| `permission.ask` | Yes | Go→Host | **Blocking** (output mutation) | Plugin can change status from "ask" to "allow" or "deny". Go calls this before asking the user. Timeout: 5s. |
| `command.execute.before` | Yes | Go→Host | **Blocking** (output mutation) | Plugin can inject additional parts into the command. Timeout: 5s. |
| `tool.execute.before` | Yes | Go→Host | **Blocking** (output mutation) | Plugin can rewrite tool args. Timeout: 5s. |
| `tool.execute.after` | Yes | Go→Host | **Blocking** (output mutation) | Plugin can rewrite tool output title/output/metadata. Timeout: 5s. |
| `shell.env` | Yes | Go→Host | **Blocking** (output mutation) | Plugin injects env vars into shell/PTY. Timeout: 3s. |
| `experimental.chat.messages.transform` | Yes | Go→Host | **Blocking** | Full message list serialized as JSON; can be large. Timeout: 30s. |
| `experimental.chat.system.transform` | Yes | Go→Host | **Blocking** | System prompt array. Timeout: 10s. |
| `experimental.session.compacting` | Yes | Go→Host | **Blocking** | Returns `{ context, prompt }`. Timeout: 10s. |
| `experimental.compaction.autocontinue` | Yes | Go→Host | **Blocking** | Returns `{ enabled }`. Timeout: 5s. |
| `experimental.text.complete` | Yes | Go→Host | **Blocking** | Returns mutated text. Timeout: 10s. |
| `tool.definition` | Yes | Go→Host | **Blocking** | Go queries once per tool per session setup; plugin can rewrite description/parameters. Timeout: 5s. |

**Hooks not bridged (structural incompatibility):**

- `auth.methods[].authorize(...)` — OAuth flows require a browser/callback server that
  Forge's daemon arch handles differently. Deferred; partial-compat fallback: log a warning
  and skip the auth method.
- `experimental_workspace.register` — workspace adapters are a deep opencode-specific
  concept tied to Bun-hosted lifecycle. Deferred.
- `$` (BunShell) — exposed on PluginInput but not bridgeable to Go without running Bun.
  Plugin host receives it natively (it runs in Bun); plugins that use `$` work. Plugins
  that call `$` and expect the result to affect Go-side state must go through the SDK client.

---

## How the Go Agent Loop Calls Hooks

Each hook call in Go follows this pattern:

```go
// Pseudocode — packages/forge/pluginbridge/bridge.go
func (b *Bridge) Trigger(ctx context.Context, name string, input, output any) (any, error) {
    if !b.enabled || b.proc == nil {
        return output, nil   // flag off or host not running: no-op
    }
    req := jrpc2.NewRequest(name, map[string]any{"input": input, "output": output})
    resp, err := b.client.CallWithTimeout(ctx, hookTimeout(name), req)
    if err != nil {
        b.log.Warn("plugin hook failed", "hook", name, "err", err)
        return output, nil   // non-fatal: return original output
    }
    return resp.Result, nil
}
```

Key design decisions:
1. **Hook failures are non-fatal.** If the plugin host crashes or times out, Go logs a
   warning and uses the unmodified output. This matches opencode's approach of catching
   errors per hook (lines 292–300 in `packages/opencode/src/plugin/index.ts`).
2. **Serial execution per hook name.** opencode triggers hooks serially (line 296 in plugin/index.ts).
   The bridge preserves this: Go sends one RPC call for a given hook; the host fans to all
   registered plugin hooks serially and returns the final output.
3. **Tool execution** uses a separate `tool.execute` RPC method that carries `toolID`,
   `args`, and `ToolContext` (sessionID, messageID, etc.). The host looks up the registered
   `ToolDefinition.execute` function and calls it. The ToolContext.`ask` and `metadata`
   callbacks each call back into Go over HTTP (via the plugin's SDK client) — no additional
   RPC channel needed.

### Plugin-Registered Tools in the Go Registry

At plugin load time, the host sends a `plugin.tools` notification listing all tools:

```json
{
  "jsonrpc": "2.0",
  "method": "plugin.tools",
  "params": {
    "tools": [
      { "id": "my_tool", "description": "...", "parameters": { /* JSONSchema7 */ } }
    ]
  }
}
```

Go registers a stub `Tool.Def` for each. When the agent loop calls the tool:

```
Go agent → RPC call: { method: "tool.execute", params: { id, args, context } }
          ← RPC response: { result: { title, output, metadata, attachments? } }
```

The PluginInput.client that plugins receive is a `createOpencodeClient` pointed at Forge's
own HTTP server (same Basic Auth credentials). This means plugin code like
`input.client.session.list()` works out of the box — zero extra Go code needed.

---

## Plugin Host Implementation

### Process: `packages/forge-plugin-host/src/index.ts`

The plugin host is a standalone Bun/Node script, shipped as part of Forge (embedded in the
binary via `bun build --compile`, or invoked from a pre-installed location):

```typescript
// Simplified sketch
import { createOpencodeClient } from "@opencode-ai/sdk/v2/client"
import { PluginLoader } from "@opencode-ai/opencode/plugin/loader"  // reused directly
import net from "net"

const socketPath = process.env.FORGE_PLUGIN_SOCKET!
const forgeUrl   = process.env.FORGE_URL!
const authHeader = process.env.FORGE_AUTH_HEADER!  // "Basic <b64>"
const directory  = process.env.FORGE_DIRECTORY!

const client = createOpencodeClient({ baseUrl: forgeUrl, headers: { authorization: authHeader }, directory })
const input: PluginInput = { client, project: ..., directory, worktree, serverUrl: new URL(forgeUrl), $: Bun.$ }

// Load plugins (same path as opencode)
const hooks = await loadPlugins(input, pluginSpecs)  // PluginLoader.loadExternal

// Start JSON-RPC server over unix socket
const server = net.createServer(handleConnection)
server.listen(socketPath)
```

The host **does not import Effect** or any opencode daemon internals. It reuses only:
- `@opencode-ai/plugin` (hook types, ToolDefinition)
- `@opencode-ai/sdk` (createOpencodeClient — talks back to Forge over HTTP)
- `@opencode-ai/opencode/config/plugin` (Glob scan for local plugin files)
- `@opencode-ai/opencode/plugin/loader` (PluginLoader — resolve/install/load)

This keeps the host lean (~15 MB Bun bundle) and avoids pulling in Effect's full runtime.

### Bun vs Node

Default runtime: **Bun**. Rationale: opencode plugins are written for Bun (`Bun.$`, Bun file APIs);
running in Bun maximizes compatibility. Fallback: Node.js (without `Bun.$`; plugins that use
BunShell get a stub that throws `Error("BunShell not available in Node mode")`).

Auto-detect: if `bun` is on `$PATH`, use it. Otherwise fall back to `node`. Configurable via
`FORGE_PLUGIN_RUNTIME=bun|node`.

---

## Lifecycle & Failure Isolation

### Startup

1. Forge daemon starts with `--plugin-host` flag.
2. Forge writes a temp socket path (`/tmp/forge-ph-<instanceID>.sock`).
3. Forge spawns `bun run /path/to/plugin-host.ts` with env vars:
   `FORGE_PLUGIN_SOCKET`, `FORGE_URL`, `FORGE_AUTH_HEADER`, `FORGE_DIRECTORY`,
   `FORGE_PLUGIN_SPECS` (JSON array of plugin specs from config).
4. Plugin host connects to socket, sends `{"jsonrpc":"2.0","method":"host.ready","params":{}}`.
5. Forge sets `bridgeReady = true`; subsequent hook calls are forwarded.
6. **Timeout:** if `host.ready` is not received within 30s, Forge logs a warning,
   sets `bridgeReady = false`, and continues without plugins.

### Shutdown

1. Forge sends `{"jsonrpc":"2.0","method":"host.shutdown","params":{}}` notification.
2. Host calls `hook.dispose?.()` for all hooks, then exits with code 0.
3. Forge waits up to 5s for the process to exit, then sends SIGKILL.

### Crash isolation

- If the plugin host process exits unexpectedly, Forge's `Bridge` detects the closed socket,
  logs `plugin host crashed`, sets `bridgeReady = false`, and continues without plugins.
- Forge does **not** auto-restart the plugin host (a crashed plugin likely crashes again).
  An admin can restart Forge to retry.
- Per-hook RPC timeouts (5–30s depending on hook) prevent a misbehaving plugin from stalling
  the agent loop. On timeout, the original unmodified output is used.

### Plugin isolation within the host

- Plugins share the same Node/Bun process (same as opencode). No per-plugin sandboxing.
- If one plugin's hook throws, the host catches it (try/catch per hook call), logs it, and
  returns the unmodified output for that hook. Other plugins' hooks for the same call still run.

---

## Implementation Milestones

| Milestone | Deliverable | Phase |
|-----------|-------------|-------|
| M1 | `FORGE_PLUGIN_HOST=0` path — no-op bridge, all hooks return unmodified output | Phase A (now) |
| M2 | Plugin host scaffold: unix socket JSON-RPC server, `host.ready` / `host.shutdown`, Bun/Node detection | Phase D |
| M3 | Plugin loading: reuse `PluginLoader.loadExternal`, `config/plugin.ts` Glob scan, send `plugin.tools` to Go | Phase D |
| M4 | Go bridge: `Trigger` for all blocking hooks, non-blocking `event` forwarding | Phase D |
| M5 | Tool execution bridge: `tool.execute` RPC, stub registration in Go tool registry | Phase D |
| M6 | Auth hook partial bridge: `auth.loader` (data transform only); skip OAuth method | Phase D |
| M7 | Provider hook bridge: `provider.models` | Phase D |
| M8 | End-to-end: run a real opencode plugin (e.g. `opencode-poe-auth`) against Forge | Phase D |

---

## Testing

### Functional

- **Null bridge test:** with `FORGE_PLUGIN_HOST=0`, all hooks return unmodified output;
  no subprocess spawned.
- **Hook round-trip tests (unit):** mock Go bridge ↔ host over a socketpair; verify each
  hook method serializes and deserializes correctly, including large payloads
  (`messages.transform` with 200 messages).
- **Timeout test:** plugin hook that sleeps 60s → verify Go returns unmodified output
  after 5s and logs a warning.
- **Crash test:** kill the plugin host mid-request → verify Go does not hang, returns
  unmodified output, sets `bridgeReady = false`.

### Compatibility

- **Real plugin smoke test:** run `opencode-poe-auth` and/or a local fixture plugin
  (uses `client`, `tool`, `chat.params`) against Forge. Verify:
  - Plugin tools appear in Forge's tool list.
  - `chat.params` hook mutates model params (verify via LLM request log).
  - `tool.execute.before` / `after` hooks fire (verify via test plugin that logs).
- **Conformance:** add a plugin-host scenario to plan 12's conformance harness —
  run the same plugin against opencode and Forge, compare SSE event streams.

### Performance

- **Hook latency benchmark:** measure round-trip latency for a no-op hook over unix socket
  vs stdio. Target: < 2ms p99 for simple hooks (chat.params, tool.execute.before).
- **Large payload test:** `messages.transform` with 500 messages (≈ 1 MB JSON) should
  complete in < 200ms.

---

## Verification

A milestone is "done" when:

1. `go test ./pluginbridge/...` passes all unit tests including timeout and crash scenarios.
2. The real-plugin smoke test (M8) passes: a published opencode plugin loads and its hooks
   fire correctly against a Forge daemon.
3. Conformance harness (plan 12) shows no regressions on SSE event streams when plugin host
   is enabled vs disabled.
4. Memory: plugin host process stays under 200 MB RSS for a typical plugin set (3–5 plugins).

---

## Risks

This is the highest-risk plan in the suite. Honest assessment:

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Plugins that use `Bun.$` for side-effects won't work correctly if the shell env differs from the daemon's env | High | Medium | Forward `FORGE_DIRECTORY` and all daemon env vars to the host process. Accept partial compat. |
| `auth.methods` OAuth flows (browser redirect, callback server) are structurally incompatible with the RPC bridge | High | Medium | Skip OAuth method bridge; log warning. Users must configure API keys manually for such providers. |
| `experimental_workspace.register` adapter — workspace adapters have async lifecycle bound to Bun | High | Medium | Defer entirely. Log warning if plugin calls it. |
| Large `messages.transform` payloads cause latency spikes | Medium | Medium | Apply 30s timeout; compress payload with msgpack if > 64 KB. |
| Plugin host crashes on first bad plugin, taking all plugins down | Medium | Medium | Add per-plugin error boundary in host; isolate crashes to individual hooks. |
| Plugin expects `PluginInput.serverUrl` to be the opencode server and calls non-standard endpoints | Low | Low | Forge implements the same endpoints; wire-compat is the invariant. |
| npm plugin install in plugin host (Bun's `bun add` in a temp dir) race conditions at startup | Medium | Low | Serialize installs; pre-warm on first run and cache in `~/.forge/plugin-cache`. |
| BunShell (`$`) used inside plugin `execute` function for tool implementation | High | Low | BunShell runs inside the plugin host (Bun process), so it works. Side effects go through the filesystem, not the Go daemon. Acceptable. |

**Partial-compat fallback strategy:** if a plugin fails to load or a hook RPC fails,
Forge logs the error, skips that plugin/hook, and continues. The user sees a `session.error`
SSE event. This matches opencode's behavior (lines 224–236 in `packages/opencode/src/plugin/index.ts`).

**Deferral story:** if plugin support is not worth the engineering cost in Phase D,
the Go daemon ships as a zero-plugin daemon indefinitely. The flag-gate means no code
paths change; the only impact is that opencode-format plugins don't work with Forge.
This is acceptable given Forge's primary target (mobile + remote) rarely uses custom plugins.

---

## Links

- [00 — Masterplan](00-masterplan.md) — sequencing, phase D
- [01 — Daemon Core](01-daemon-core.md) — auth, routing, instance lifecycle
- [02 — Agent Engine](02-agent-engine.md) — hook call sites in the agent loop
- [06 — SDK Generation](06-sdk-generation.md) — the SDK client the plugin host uses to call back into Forge
- [12 — Conformance](12-test-compatibility.md) — conformance harness for plugin hook parity
