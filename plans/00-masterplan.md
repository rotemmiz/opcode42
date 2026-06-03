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

## Review pass (2026-06-03) — status reconciliation, ambiguities, validation

This section is a periodic audit layer. It does **not** re-architect; it records where the plan
text has drifted from the built reality and pins cross-cutting ambiguities that individual plans
keep deferring to each other.

### Status vs reality (the sequencing above reads greenfield; it is not)
Verified against the Go tree + git log: `go build ./...` and `go test ./internal/engine/...` are green.
- **Done:** plan 01 (daemon core), plan 02 **M1–M10**, plan 04 (resource loaders).
- **Partial:** plan 03 — MCP config + **stdio** connect + tool merge/dispatch landed (#59/#62);
  remote (StreamableHTTP/SSE) transport, `mcp.tools.changed` watcher, and MCP-call permission
  gating are **still unbuilt within M3-1**;
  **MCP OAuth (M3-2) and the entire LSP subsystem (M3-3→M3-6) are unbuilt** (`internal/lsp/` absent).
  Plan 12 — recording/normalize infra exists; full scenario suite + dual-run gate incomplete.
- **Not started:** plan 02 **M9 leftovers** (title generation, `json_schema` structured-output tool,
  MAX_STEPS sentinel, agent-level `maxSteps` wiring — currently a hard `const 100`) and **M11**
  (conformance pass); plan 05 (plugin host, flag-gated no-op only), plan 13.
- **Further along than a quick scan suggests:** plan 08 (TUI) — `internal/tui/` is ~35 Go files,
  Phases 0–3 done; its gap-closing endpoints (`/agent`, `/provider`, permission/question replies,
  `/find/file`, `/pty`) have all landed. Not a stub.
- **Action:** treat Phase B as "engine built, not yet conformance-green." The Phase B exit gate
  ("conformance green → repoint mobile + TUI") has **not** been met because M11 has not run.

### Cross-cutting ambiguities (owned here because no single plan settles them)
1. **v1↔v2 split + `/sync/*` + `/experimental/*` "best-effort"** (lines 55, 84, 103). "Best-effort"
   is undefined: does an unimplemented v1/sync/experimental endpoint return `501`, proxy to nothing,
   or 404? Pick one contract and assert it in plan 12. Until then this is the single largest
   unresolved compatibility ambiguity.
2. **Authoritative SSE event catalog.** Lines 56–60 list event types but say "captured empirically
   (plan 12)." Decide which artifact is normative — the recorded cassettes or this list — and make
   the other reference it. A plan should never be the source of truth for a wire shape.
3. **`x-opencode-directory` decode contract.** "base64 in v2" must be pinned exactly (which
   encoding, trailing-slash normalization, empty/missing → default instance). Add an explicit
   routing-conformance assertion (plan 12 already has a Routing section — point here).
4. **Provider auth/OAuth surface** (wall #3). Deferred across plans 03/04/13 with no owner for the
   end-to-end OAuth callback/loopback story. Assign it to one plan (recommend 13 remote-ops, since
   loopback redirects interact with remote hardening).

### Validation strategy (make the gate explicit, not just "ties to plan 12")
Every plan's Verification section must, before a feature is considered done, pass the **local CI
mimic** now codified in the repo `CLAUDE.md` git workflow: `go build/vet`, `gofmt -l`,
`golangci-lint run`, `go test ./...`, `make gen` + `git diff --exit-code internal/api/gen/`, and
`scripts/run-conformance.sh self` — **plus a dual-run diff against real opencode for any new or
changed endpoint/SSE shape.** "Conformance green" = this gate, not a vibe.

### Decisions locked (2026-06-03, user-approved)
These resolve the cross-cutting ambiguities above; the referenced plans inherit them.
1. **Unimplemented endpoints (v1 legacy, `/sync/*`, `/experimental/*`) return `501 NotImplemented`**
   with the standard error envelope (`{"tag":"NotImplemented"}`). No no-op-success, no 404. Add a
   conformance assertion that opencode clients **degrade gracefully** on 501 (don't crash). Closes
   cross-cutting ambiguity #1; supersedes plan 13 open-question #3 (no no-op `true` for `/sync`).
2. **Conformance strictness: missing/changed field = FAIL; extra additive field = WARNING** and must
   be listed in `conformance/known-additions.json`. This is the single policy; plan 02's "extra
   fields = warning" and plan 12's "any divergence = fail" both defer to it.
3. **Single-user, matching opencode** (one password/token namespace). Bearer tokens (plan 13.2) are
   additional credentials for the one user, not per-user isolation. Resolves plan 13 open-question #2.
4. **OAuth is deferred; API keys only for now.** Remote-MCP OAuth (plan 03 M3-2) and provider OAuth
   (`/provider/:id/oauth/authorize|callback`, plan 04) stay **501**; users configure API keys (the
   shared `~/.local/share/opencode/auth.json` path already works). This unblocks LSP/agent/
   conformance first. When OAuth is picked up, it lands in plan 13 (loopback + optional
   `--oauth-callback-proxy-url`). Resolves the OAuth-ownership ambiguity.
5. **Windows is not supported for now** — Linux/macOS only. LSP/MCP spawn code, path handling, and
   server-table `.cmd`/`.bat`/`.exe` variants (plan 03 risk #9) target POSIX; Windows is explicitly
   out of scope, not "best-effort." Revisit only if a Windows client is prioritized.
6. **`GET /command` ordering: Forge sorts by name (deterministic) — a known-addition.** opencode
   returns commands in non-deterministic (map/glob) order; Forge already sorts
   (`internal/resource/command.go:50`). Record this in `known-additions.json` and make the
   conformance differ **order-insensitive** for the `/command` list so it isn't a false failure.
7. **Build plan 06 M10 (handler↔spec conformance).** The served spec must be provably accurate, not
   assumed. Implement (a) route-table-derived `/openapi.json` emission and (b) a per-operation
   **response-schema conformance test** that validates real handler outputs against the reference's
   response schemas (offline, no live opencode). This complements — does not replace — plan 12's
   dual-run. Closes the circular-drift gap (spec served verbatim today).

## Notes on where these plans live
This `forge/` directory is a scratch home for the plan suite, kept separate from the opencode
reference repo. Move it into the real Forge project repo when it's created.
