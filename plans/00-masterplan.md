# Forge — Master Plan

> Forge is a ground-up, **interop-first alternative to opencode**: a **Go daemon** that is
> wire-compatible with opencode's HTTP+SSE+WebSocket API and ecosystem-compatible with its
> config/resource formats, with **mobile (Android) as the primary client**.

This is the index. Detailed plans live alongside this file:

| # | Plan | Scope |
|---|------|-------|
| 00 | this file | Vision, contract, architecture, sequencing |
| 01 | [daemon-core](01-daemon-core.md) | Go transport, state, auth, instance routing |
| 02 | [agent-engine](02-agent-engine.md) | LLM stream + tool loop, message model, permissions |
| 03 | [ecosystem-mcp-lsp](03-ecosystem-mcp-lsp.md) | MCP + LSP integration |
| 04 | [ecosystem-resources](04-ecosystem-resources.md) | agents/commands/rules/skills/providers loaders |
| 05 | [plugin-host](05-plugin-host.md) | Node/Bun sidecar for opencode-format TS plugins |
| 06 | [sdk-generation](06-sdk-generation.md) | Go + Kotlin/Swift SDKs from the spec |
| 07 | [client-mobile](07-client-mobile.md) | Android/mobile app (primary deliverable) |
| 08 | [client-tui](08-client-tui.md) | Go Bubble Tea TUI (dogfood/test vehicle) |
| 09 | [integration](09-integration.md) | How the components wire together end-to-end |
| 10 | [test-functional](10-test-functional.md) | Functional test strategy |
| 11 | [test-performance](11-test-performance.md) | Performance/load test strategy |
| 12 | [test-compatibility](12-test-compatibility.md) | Conformance harness vs real opencode |
| 13 | [remote-ops](13-remote-ops.md) | Remote-first hardening, push, packaging |

---

## Context & motivation

opencode (the reference repo at `/Users/rotemmiz/git/opencode`) *already is* a daemon +
stateless-clients system: `opencode serve` owns all state (SQLite at `~/.opencode/opencode.db`),
the agent loop, tools, and integrations; thin clients (TUI via `opencode attach <url>`, Electron
desktop, SolidJS web, VSCode) consume **REST + SSE (`/event`, `/global/event`) + WebSocket-PTY
(`/pty/:id/connect`)**, with Basic Auth / `?auth_token=`, directory routing via
`x-opencode-directory`, and mDNS discovery. The missing client is **mobile**; the daemon is
single-user TS/Bun + Effect.

Forge exists for three reasons: **mobile + remote-first**, **ownership/license/control**, and a
**faster Go runtime / single-binary deployment**. We deliberately stay interoperable until interop
becomes a wall worth breaking.

**Strategic leverage of wire-compat:** the mobile client can be built and validated against the
*real opencode daemon from day one*, decoupled from the Go daemon's progress; and opencode's own
*unmodified* clients pointed at the Go daemon become the strongest interop proof. Interop is both
the product goal and the development methodology.

## Reference contract (what we stay compatible with)

- **OpenAPI spec:** `packages/sdk/openapi.json` (~113 path entries) — the frozen contract.
  Generate clients/server-stubs from it; diff the daemon's emitted spec against it (drift = CI failure).
- **Endpoint families:** `/session/*` (CRUD, messages, prompt, prompt_async, shell, command,
  fork/revert, diff), `/event` + `/global/event` (SSE), `/pty/*` + `/pty/:id/connect` (WS),
  `/config`, `/agent`, `/command`, `/provider`(+`/provider/auth`), `/file/*`, `/find/*`, `/lsp`,
  `/mcp`, `/permission`, `/question`, `/project/*`, `/path`, `/skill`, `/tui/*`,
  `/global/{health,config,upgrade,dispose,event}`, plus `/sync/*` & `/experimental/*` (best-effort).
- **SSE payload shape:** `{ id, type, properties }`. Known types: `server.connected`,
  `server.heartbeat`, `session.*`, `message.*`, `part.*`, `permission.asked`,
  `question.{asked,replied,rejected}`, `pty.{created,updated,exited,deleted}`, `lsp.updated`,
  `project.updated`, `workspace.status`, `global.disposed`, `tui.prompt.append`. Full catalog +
  exact field shapes captured empirically (see plan 12).
- **PTY WS framing:** control frame `0x00 + JSON({cursor})`; data frames UTF-8; 2MB buffer/64KB chunks.
- **Auth/routing:** Basic Auth or `?auth_token=base64(user:pass)`; `x-opencode-directory`
  (base64 in v2) / `directory` query param selects the per-directory instance.

## Architecture

```
                ┌─────────────────────────────────────────────┐
   Mobile  ─────┤   Forge Daemon (Go, single static binary)   │── SQLite (sessions/msgs/parts)
   (primary) ───┤   - HTTP/REST + SSE bus + WS PTY            │── repo + built-in tools
   TUI (Go) ────┤   - Auth + directory/instance routing       │── MCP clients (stdio/http/sse)
   opencode's   │   - Agent engine (LLM stream + tool loop)   │── LSP servers (jsonrpc)
   web/desktop  │   - Ecosystem loaders                       │
   (unmodified) │   - Plugin host sidecar (Node/Bun) ◄────────┼── opencode-format TS plugins
                └─────────────────────────────────────────────┘
        all clients speak the SAME opencode wire protocol
```

### Known interop walls (default = stay compatible; decide per-plan)
1. **TS/JS plugins run in-process in opencode's Bun runtime.** Go can't host them natively →
   Node/Bun **plugin-host sidecar** over RPC (plan 05). Highest-risk; flag-gated; deferrable.
2. **Behaviors not in the spec** (SSE semantics, optimistic-update expectations, error envelopes,
   PTY framing) → mitigated by the conformance harness (plan 12).
3. **Provider auth/OAuth** and the **v1↔v2 API split** → pin to v2; v1 best-effort.

## Sequencing (exploits wire-compat to parallelize)

- **Phase A — kickoff (parallel):** plan 12 contract+harness · plan 01 daemon scaffold
  (health/config/session-list/SSE passthrough) · **plan 07 mobile v0 against the real opencode
  daemon**. Mobile advances without the Go engine.
- **Phase B — engine:** plan 02 agent loop on the Go daemon → conformance green → repoint mobile + TUI.
- **Phase C — ecosystem:** plans 03, 04 (MCP, LSP, agents/commands/rules/providers).
- **Phase D — polish:** plan 05 plugin host, plan 13 remote hardening + push, plan 08 TUI, spec self-emission.

## Cross-cutting validation

Every plan's verification ties back to **plan 12 (conformance)**: identical scenarios pass against
opencode and Forge; SSE streams diffed event-for-event; opencode's unmodified clients run against Forge.

## Open decisions (settle in the referenced plans)
- Plugin compat now vs deferred (plan 05).
- Mobile native-Kotlin vs KMP vs cross-platform (plan 07).
- How far to chase v1 / experimental / sync endpoints vs pin to v2 (plans 01, 09).

## Notes on where these plans live
This `forge/` directory is a scratch home for the plan suite, kept separate from the opencode
reference repo. Move it into the real Forge project repo when it's created.
