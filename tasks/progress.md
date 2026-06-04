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
| **P02-M9b** loop leftovers | title gen · `json_schema` structured-output tool · MAX_STEPS sentinel · agent-level `maxSteps` wiring (replace hard `const 100`) | `02-agent-engine.md` §M9 | engine | done | P02-M9a | landed #100 |
| **P12-rec** record infra | recording / normalize harness | `12-test-compatibility.md` | conformance | done | P01 | landed |
| **P12-suite** scenarios | full scenario suite + dual-run diff gate | `12-test-compatibility.md` | conformance | done | P12-rec | landed #104 (live dual-run + 5 scenarios, skip-gated; Gemini quota caveat) |
| **P02-M11** conformance | end-to-end SSE conformance pass (Phase B exit gate) | `02-agent-engine.md` §M11 | conformance | done | P02-M9b, P12-suite | **landed #111** — authoritative SSE catalog from opencode src; ambiguity #2 RESOLVED. ⇒ PHASE B EXIT GREEN |
| **P06-P1** sdk pin | clients/server-stubs pinned to `openapi.json` | `06-sdk-generation.md` §Phase 1 | sdk | done | P01 | `make gen` green (verify.md S3) |
| **P06-P2** sdk self-emit | Forge emits its own spec; diff vs frozen | `06-sdk-generation.md` §Phase 2 | sdk | todo | P02-M11, P12-suite | **READY** (deps done) |
| **P03-M3-1a** mcp stdio | MCP config + stdio connect + tool merge/dispatch | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | done | P02-M1..M8 | landed (#59/#62) |
| **P03-M3-1b** mcp remote+watch+gate | StreamableHTTP/SSE transport · `mcp.tools.changed` watcher · MCP-call permission gating | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | done | P03-M3-1a | landed #102 |
| **P03-M3-2** mcp oauth | MCP OAuth flow | `03-ecosystem-mcp-lsp.md` §M3-2 | mcp | todo | P03-M3-1b, P13-oauth | **READY** (deps done) — reuse P13 OAuth surface; add needs_auth/needs_client_registration statuses + mutating /mcp endpoints |
| **P03-M3-3** lsp config | LSP config + built-in server table | `03-ecosystem-mcp-lsp.md` §M3-3 | lsp | done | P02-M1..M8 | landed #99 |
| **P03-M3-4** lsp diagnostics | LSP client — diagnostics | `03-ecosystem-mcp-lsp.md` §M3-4 | lsp | done | P03-M3-3 | landed #103 (client + GET /lsp + lsp.updated) |
| **P03-M3-5** lsp query+tool | LSP query operations + tool | `03-ecosystem-mcp-lsp.md` §M3-5 | lsp | done | P03-M3-4 | landed #106 (9 ops, exp-flag-gated tool) |
| **P03-M3-6** lsp/mcp sse | `lsp.updated` SSE + `mcp.tools.changed` wiring | `03-ecosystem-mcp-lsp.md` §M3-6 | lsp | done | P03-M3-5, P03-M3-1b | landed #107 (+ real @file/@dir/@symbol ResolvePromptParts) |
| **P05** plugin-host | Node/Bun sidecar for opencode TS plugins | `05-plugin-host.md` | plugin | done | P02-M9a | landed #110 (M2–M5 + hooks); Phase-D hook bridges still stubbed |
| **P13-oauth** provider oauth | end-to-end OAuth callback/loopback (assigned owner) | `13-remote-ops.md` | remote | done | P02-M9a | landed #109 (PKCE loopback, xAI; unblocks P03-M3-2) |
| **P13-rest** remote hardening | push notifications · packaging · remote-first hardening | `13-remote-ops.md` | remote | todo | P02-M9a | parallel-safe with P13-oauth? see notes |
| **P07-A** mobile v0 | Android v0 against real opencode daemon | `07-client-mobile.md` §Phase A | mobile | partial | — | UI fidelity passes ongoing (#58–#61) |
| **P07-B** mobile repoint | repoint Android to Forge daemon | `07-client-mobile.md` §Phase B | mobile | todo | P02-M11 | **READY** — conformance green (#111) |
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

As of Wave-3 completion (2026-06-04, main @ e23fe49). **Waves 1+2+3 all merged. 🎯 PHASE B EXIT GATE GREEN (P02-M11, #111).**
Done this wave: P02-M11 (#111), P05 (#110), P13-oauth (#109), P08 slice (#108).
READY now (deps satisfied, distinct tracks → parallelizable) — **Wave 4 candidates**:
- **P07-B** (track mobile) — repoint Android to the Forge daemon. Now unblocked by conformance-green. High value (mobile is the primary client).
- **P06-P2** (track sdk) — Forge emits its own openapi spec; diff vs frozen. Unblocked.
- **P03-M3-2** (track mcp) — MCP OAuth: reuse P13's OAuth surface; add needs_auth/needs_client_registration statuses + mutating /mcp endpoints (POST /mcp add, connect/disconnect/auth, currently 501).
- **P13-rest** (track remote) — push-notifications / packaging / remote-first hardening.
- **P08 / U13** (track tui, partial) — TUI↔Forge dual-run parity now unblocked (P02-M11 done); plus remaining 08c bg-pulse (optional).

Still BLOCKED: P07-C (mobile parity → P07-B).
Distinct tracks → P07-B, P06-P2, P03-M3-2, P13-rest, P08 are parallel-safe (up to 5 workers). **Not yet dispatched — awaiting human go-ahead on Wave-4 scope.**
Pre-existing stale PRs unrelated to these waves: #96 (tui graphics-fineness), #71 (WIP android mDNS) — flag to human, not orchestrator-owned.

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
2026-06-03 Track F (#105) opened + CI-green but agent STOPPED at review without merging (returned 5 should-fix findings: SSE-hang, mislabeled sub-count, p99==max, rps over-report, no ctx backstop). SendMessage unavailable → dispatched continuation agent ab2fc1e70253e9b0c to fix in the existing worktree + self-merge #105.
2026-06-03 Track D MERGED → #104 (a5e170f): live dual-run mode + 5 skip-gated agent-flow scenarios; closed Forge↔Gemini auth gap (builtinBaseURL → Gemini OpenAI-compat endpoint). P12-suite done. Caveat: full 5-scenario green baseline blocked by free-tier Gemini daily quota; 3 pre-existing divergences tracked in known-divergences-live.json (engine info.mode/path.root; /session/:id/summarize 501).
2026-06-03 NOTE: D agent left 2 orphaned uncommitted EventTypes/normalize test files in the MAIN working tree (not in its PR branch). Verified they build+pass against merged main, but direct push of unreviewed test code to main was (correctly) blocked → discarded. Bonus coverage; can be re-added via a future conformance PR if wanted.
2026-06-03 Track F MERGED → #105 (4ae38ac): bench/ W0 baseline harness + head-to-head vs opencode. Continuation agent fixed all 5 should-fix findings (bounded-ctx SSE, honest sub-count, interpolated p99, true-elapsed throughput, run-level deadline) + refreshed measured baseline; review clean, CI green. NOTE: both F agents stopped at review w/o merging — orchestrator drove CI-watch + squash-merge. Plan-11 W0 done.
2026-06-03 B3 MERGED → #106 (425b816): LSP query ops (9 ops, exact opencode enum strings) + lsp engine tool (1→0-based verified, OPENCODE_EXPERIMENTAL_LSP_TOOL-gated). Self-merged cleanly. P03-M3-5 done.
2026-06-03 B4 dispatched (final LSP track, sequential after B3) — agent a43e397c3cccceeee — M3-6 SSE bus wiring (lsp.updated + mcp.tools.changed) + real ResolvePromptParts (@file/@dir/@symbol via WorkspaceSymbol); owns engine.go/resolvePromptParts alone. Prompt hardened with explicit do-not-stop-at-review rule. P03-M3-6.
2026-06-03 B4 MERGED → #107 (2271724): real @file/@dir/@symbol ResolvePromptParts (ported from opencode session/prompt.ts) + verified LSP/MCP SSE envelope (lsp.updated → {}, mcp.tools.changed → {server}). Self-merged after hardened do-not-stop-at-review prompt. P03-M3-6 done. ⇒ WAVE 2 COMPLETE (B2 #103, B3 #106, B4 #107, D #104, F #105).
2026-06-03 WAVE 3 dispatched (parallel, distinct tracks; hardened do-not-stop-at-review prompt; self-merge):
  - P02-M11 conformance exit gate (plan 02 M11) — agent abff06c754797d221 — owns conformance/ (+engine SSE fixes); SSE catalog derived authoritatively from opencode source per human decision.
  - P05 plugin host (plan 05) — agent a9f3610855c94e2b2 — Node/Bun sidecar, flag-gated; seam: cmd/forged/main.go (additive).
  - P13-oauth provider OAuth (plan 13) — agent aaf2ce9c159e069a5 — owns OAuth surface; seam: cmd/forged/main.go + provider auth (additive).
  - P08 TUI polish (plan 08) — agent a03c37e00c2166965 — owns internal/tui/; scoped to the NEXT single phase only.
2026-06-03 P08 MERGED → #108 (5e222da): colored +/- diff sign markers (plan 08c M6 residual). Self-merged clean. P08 stays `partial` — remaining: 08c bg-pulse (optional), 08b §3 workspaces/§8 tags (daemon-gated), 08b §4 auth (P13 lane), U13 TUI↔Forge dual-run parity (gated on P02-M11). Note: U12 PTY/VT pane already shipped (#80) — stale review row corrected.
2026-06-03 P13-oauth MERGED → #109 (1fcbadd): end-to-end provider OAuth (PKCE auth-code, shared 127.0.0.1 loopback callback, CSRF state, token exchange → shared auth.json; xAI as first provider; --oauth-callback-proxy-url for headless). Self-merged; review fixed loopback-port + shutdown nil-deref. P13-oauth done → UNBLOCKS P03-M3-2 (mcp oauth). Deferred: token refresh, more providers, MCP OAuth (stays 501).
  ⚠️ CROSS-TRACK FOLLOWUP: conformance/known-divergences.json still marks provider-oauth/provider-auth as "deferred/501" — now stale. The P02-M11 agent (owns conformance/) should refresh to "implemented for built-in providers (plan 13)"; if it doesn't, orchestrator spins a tiny follow-up after M11 lands.
2026-06-03 P05 MERGED → #110 (cb2cb1b): plugin host — unix-socket JSON-RPC 2.0 sidecar (Bun/Node auto-detect), plugin discovery + tool registration, Go bridge (blocking Trigger hooks + event fan-out + crash isolation), flag-gated off (--plugin-host/FORGE_PLUGIN_HOST=1). Self-merged; rebased 2x over P13 cmd/forged seam (additive); review fixed a startup-hang on pre-ready crash. Phase-D hooks (auth/provider, tool.before/after, permission.ask, compaction) still stubbed. P05 done.
  ⚠️ HARNESS FLAKE (flagged by P13 + P05 independently): scripts/run-conformance.sh self has a nondeterministic opencode-vs-opencode session-LIST ordering flake on base main (CI self-diff passes; local intermittent). Conformance owner (P02-M11) should add order-insensitive normalization (à la E1 permission normalizer) — verify at M11 merge; else tiny follow-up.
2026-06-03 P02-M11 (#111) opened, rebased on main, CI-green; agent handled BOTH cross-track followups (known-divergences refresh for #109; NormalizeSetJSON masks the session-list flake) but STOPPED at review with 1 unaddressed should-fix: summarize `auto` flag parsed (prompt_handlers.go:213) yet dropped (engine.SummarizeInput lacks Auto → hardcoded auto=false) → wire divergence on CompactionPart.auto. Dispatched continuation agent a87628164fbe9cfc4 to fix that one finding + self-merge #111.
2026-06-04 P02-M11 MERGED → #111 (e23fe49): end-to-end SSE conformance — authoritative event catalog enumerated+cited from opencode source (server.heartbeat correctly excluded as transport-injected), session.status/idle ordering, mode/path.root fixes, NormalizeSetJSON masks the session-list flake, known-divergences refreshed for #109. Continuation agent fixed the summarize `auto`-flag drop (engine.SummarizeInput.Auto → createCompaction). Agent stopped at clean review; orchestrator merged. 🎯 PHASE B EXIT GATE GREEN. ⇒ WAVE 3 COMPLETE (#108/#109/#110/#111).
  Both cross-track followups (known-divergences refresh, session-list flake) were handled inside #111 — no separate follow-up needed.
2026-06-04 WAVE 4 dispatched (single track, user-selected — TUI-first): P08/U13 TUI↔Forge dual-run parity (plan 08 U13) — agent a66197823ec439aad — re-point TUI at Forge daemon + parity gate; owns internal/tui/; gemini key throttled so deterministic/mocked flows only. (P07-B/P06-P2/P03-M3-2/P13-rest deferred to a later wave per user.)
