# Forge — Orchestration Ledger

> **This file is the source of truth for "what's next", not the conversation.**
> The orchestrator session reads this, dispatches work, and writes status back here.
> A fresh/compacted session resumes by re-reading this file — nothing lives only in chat.
>
> Read `tasks/orchestrator.md` for the loop that drives this ledger.

## Status legend
- `done`    — built + passed the local CI-mimic gate (see orchestrator.md §Gate).
- `partial` — some sub-work landed; remaining sub-items are split into their own rows.
- `todo`    — not started, all deps satisfied → **eligible to dispatch**.
- `blocked` — not started, ≥1 dep not `done` → **do not dispatch**.

A task is **READY** iff `status ∈ {todo, partial}` AND every id in `deps` is `done`.
The orchestrator dispatches **all READY tasks whose `track` differs, in parallel.**

## Parallelization model
- `track` = the code area a task owns. **Two tasks may run in parallel only if their
  tracks differ** (disjoint package trees → no merge conflicts). Same-track tasks run
  sequentially even when both are READY.
- Each parallel worker runs in its **own git worktree** (`Agent` tool, `isolation: "worktree"`).
- Within a plan, milestones are sequential **unless** a row says otherwise in `notes`.

Tracks in play: `engine` (internal/engine) · `mcp` (internal/mcp) · `lsp` (internal/lsp) ·
`conformance` (conformance/) · `plugin` (plugin host sidecar) · `remote` (remote-ops) ·
`tui` (internal/tui) · `mobile` (android) · `sdk` (codegen).

---

## Ledger

Baseline status taken from `plans/00-masterplan.md` §"Review pass (2026-06-03)" and
`tasks/verify.md`. Deps derived from masterplan §Sequencing.

| id | task | plan ref | track | status | deps | notes |
|----|------|----------|-------|--------|------|-------|
| **P01** daemon-core | M1–M7 transport/SQLite/routing/SSE/PTY/mDNS/stubs | `01-daemon-core.md` | engine | done | — | landed |
| **P04** resources | M1–M8 frontmatter/agents/commands/skills/providers | `04-ecosystem-resources.md` | engine | done | — | landed |
| **P02-M1..M8** engine | message model → tools → permissions → registry | `02-agent-engine.md` §M1–M8 | engine | done | P01 | landed |
| **P02-M9a** agent loop | core agent loop integration | `02-agent-engine.md` §M9 | engine | done | P02-M1..M8 | landed |
| **P02-M10** compaction | context compaction | `02-agent-engine.md` §M10 | engine | done | P02-M9a | landed |
| **P02-M9b** loop leftovers | title gen · `json_schema` structured-output tool · MAX_STEPS sentinel · agent-level `maxSteps` wiring (replace hard `const 100`) | `02-agent-engine.md` §M9 | engine | todo | P02-M9a | small, self-contained |
| **P12-rec** record infra | recording / normalize harness | `12-test-compatibility.md` | conformance | done | P01 | landed |
| **P12-suite** scenarios | full scenario suite + dual-run diff gate | `12-test-compatibility.md` | conformance | todo | P12-rec | **unblocks P02-M11**; parallel-safe now |
| **P02-M11** conformance | end-to-end SSE conformance pass (Phase B exit gate) | `02-agent-engine.md` §M11 | conformance | blocked | P02-M9b, P12-suite | gates mobile/TUI repoint |
| **P06-P1** sdk pin | clients/server-stubs pinned to `openapi.json` | `06-sdk-generation.md` §Phase 1 | sdk | done | P01 | `make gen` green (verify.md S3) |
| **P06-P2** sdk self-emit | Forge emits its own spec; diff vs frozen | `06-sdk-generation.md` §Phase 2 | sdk | blocked | P02-M11, P12-suite | ties to conformance |
| **P03-M3-1a** mcp stdio | MCP config + stdio connect + tool merge/dispatch | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | done | P02-M1..M8 | landed (#59/#62) |
| **P03-M3-1b** mcp remote+watch+gate | StreamableHTTP/SSE transport · `mcp.tools.changed` watcher · MCP-call permission gating | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | todo | P03-M3-1a | uses P02-M7 permissions (done) |
| **P03-M3-2** mcp oauth | MCP OAuth flow | `03-ecosystem-mcp-lsp.md` §M3-2 | mcp | blocked | P03-M3-1b, P13-oauth | OAuth surface owned by P13 |
| **P03-M3-3** lsp config | LSP config + built-in server table | `03-ecosystem-mcp-lsp.md` §M3-3 | lsp | todo | P02-M1..M8 | `internal/lsp/` greenfield |
| **P03-M3-4** lsp diagnostics | LSP client — diagnostics | `03-ecosystem-mcp-lsp.md` §M3-4 | lsp | blocked | P03-M3-3 | |
| **P03-M3-5** lsp query+tool | LSP query operations + tool | `03-ecosystem-mcp-lsp.md` §M3-5 | lsp | blocked | P03-M3-4 | |
| **P03-M3-6** lsp/mcp sse | `lsp.updated` SSE + `mcp.tools.changed` wiring | `03-ecosystem-mcp-lsp.md` §M3-6 | lsp | blocked | P03-M3-5, P03-M3-1b | cross-track dep on mcp |
| **P05** plugin-host | Node/Bun sidecar for opencode TS plugins | `05-plugin-host.md` | plugin | todo | P02-M9a | flag-gated; currently no-op stub |
| **P13-oauth** provider oauth | end-to-end OAuth callback/loopback (assigned owner) | `13-remote-ops.md` | remote | todo | P02-M9a | masterplan §128.4 assigns OAuth here |
| **P13-rest** remote hardening | push notifications · packaging · remote-first hardening | `13-remote-ops.md` | remote | todo | P02-M9a | parallel-safe with P13-oauth? see notes |
| **P07-A** mobile v0 | Android v0 against real opencode daemon | `07-client-mobile.md` §Phase A | mobile | partial | — | UI fidelity passes ongoing (#58–#61) |
| **P07-B** mobile repoint | repoint Android to Forge daemon | `07-client-mobile.md` §Phase B | mobile | blocked | P02-M11 | needs conformance green |
| **P07-C** mobile parity | full feature parity | `07-client-mobile.md` §Phase C | mobile | blocked | P07-B | |
| **P08** tui | Phases 0–3 done; remaining polish phases | `08-client-tui.md` | tui | partial | P02-M9a | ~35 files; gap endpoints landed |

---

## Ambiguities that block clean dispatch (resolve with human before the dependent task)
These are from masterplan §"Cross-cutting ambiguities" — a worker should **escalate, not guess**:
1. **v1↔v2 / `/sync` / `/experimental` "best-effort" contract** (501 vs 404 vs proxy). Blocks parts of **P12-suite** and **P06-P2**.
2. **Authoritative SSE event catalog** — cassettes vs masterplan list. Blocks **P12-suite**, **P02-M11**.
3. **`x-opencode-directory` decode contract** (encoding, trailing slash, empty→default). Blocks routing assertions in **P12-suite**.
4. **Provider OAuth surface owner** — masterplan recommends P13. Confirmed here as **P13-oauth**; blocks **P03-M3-2**.

---

## Ready set (orchestrator maintains this — recompute each cycle)
<!-- ORCHESTRATOR: overwrite this block each cycle with the current READY tasks and their tracks. -->

As of scaffold (2026-06-03), READY (deps satisfied, distinct tracks → all parallelizable):
- **P02-M9b** (track engine)
- **P12-suite** (track conformance) — *gated by ambiguities #1/#2; confirm contract first*
- **P03-M3-1b** (track mcp)
- **P03-M3-3** (track lsp)
- **P05** (track plugin)
- **P13-oauth** (track remote)
- **P13-rest** (track remote) — *same track as P13-oauth → runs after it unless split*
- **P08** (track tui)

That's up to **6 workers in parallel** across distinct tracks (engine, conformance, mcp, lsp,
plugin, remote/tui). Start with a smaller fan-out (2–3) until the loop is proven.

## Run log
<!-- Append one line per dispatch/merge: `2026-06-03 P03-M3-3 dispatched (worktree wt-lsp)` etc. -->
2026-06-03 Wave-1 fully merged & synced to main @ 674ad79 (#98 #99 #100 #101 #102); stale worktrees pruned.
2026-06-03 docs: committed agent-owns-PR git workflow + Wave-2 scaffolding directly to main @ 3fa7b3f (user-approved, no PR).
2026-06-03 Wave-2 dispatched in parallel (own-PR-until-merged lifecycle, self-merge):
  - B2 LSP client/diagnostics (plan 03 M3-4) — agent ad34fce615fc99e3c — owns internal/lsp/, adds go.lsp.dev deps.
  - Track D dual-run + scenarios (plan 12 / 02 M11) — agent aeca38799e1407058 — owns conformance/, verifies Forge↔Gemini auth gap first.
  - Track F perf W0 (plan 11) — agent ac6050bd4aa832f21 — new bench harness, measures opencode baseline first.
2026-06-03 B2 MERGED → #103 (a9ef55b): LSP JSON-RPC client + diagnostics, GET /lsp + lsp.updated SSE. Deps added: go.lsp.dev/{jsonrpc2,protocol,uri}. P03-M3-4 done.
2026-06-03 B3 dispatched (sequential after B2) — agent a2a9cd5b66cb74d87 — LSP query ops + internal/engine/tool/lsp.go (operation enum, 1→0-based, HasClients). P03-M3-5.
