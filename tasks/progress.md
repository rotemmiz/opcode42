# Opcode42 — Orchestration Ledger

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
| **P06-P2** sdk self-emit | Opcode42 emits its own spec; diff vs frozen | `06-sdk-generation.md` §Phase 2 | sdk | done | P02-M11, P12-suite | landed #113 (self-emit /openapi.json from route table + 2-way drift gate; /doc+/openapi.json are known-additions) |
| **P03-M3-1a** mcp stdio | MCP config + stdio connect + tool merge/dispatch | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | done | P02-M1..M8 | landed (#59/#62) |
| **P03-M3-1b** mcp remote+watch+gate | StreamableHTTP/SSE transport · `mcp.tools.changed` watcher · MCP-call permission gating | `03-ecosystem-mcp-lsp.md` §M3-1 | mcp | done | P03-M3-1a | landed #102 |
| **P03-M3-2** mcp oauth | MCP OAuth flow | `03-ecosystem-mcp-lsp.md` §M3-2 | mcp | done | P03-M3-1b, P13-oauth | landed #116 (DCR+PKCE via mcp-go, tokens→shared mcp-auth.json, needs_auth/needs_client_registration, all mutating /mcp 501→real). Deferred: token-refresh dual-run gate |
| **P03-M3-3** lsp config | LSP config + built-in server table | `03-ecosystem-mcp-lsp.md` §M3-3 | lsp | done | P02-M1..M8 | landed #99 |
| **P03-M3-4** lsp diagnostics | LSP client — diagnostics | `03-ecosystem-mcp-lsp.md` §M3-4 | lsp | done | P03-M3-3 | landed #103 (client + GET /lsp + lsp.updated) |
| **P03-M3-5** lsp query+tool | LSP query operations + tool | `03-ecosystem-mcp-lsp.md` §M3-5 | lsp | done | P03-M3-4 | landed #106 (9 ops, exp-flag-gated tool) |
| **P03-M3-6** lsp/mcp sse | `lsp.updated` SSE + `mcp.tools.changed` wiring | `03-ecosystem-mcp-lsp.md` §M3-6 | lsp | done | P03-M3-5, P03-M3-1b | landed #107 (+ real @file/@dir/@symbol ResolvePromptParts) |
| **P05** plugin-host | Node/Bun sidecar for opencode TS plugins | `05-plugin-host.md` | plugin | done | P02-M9a | landed #110 (M2–M5 + hooks); Phase-D hook bridges still stubbed |
| **P13-oauth** provider oauth | end-to-end OAuth callback/loopback (assigned owner) | `13-remote-ops.md` | remote | done | P02-M9a | landed #109 (PKCE loopback, xAI; unblocks P03-M3-2) |
| **P13-rest** remote hardening | push notifications · packaging · remote-first hardening | `13-remote-ops.md` | remote | done | P02-M9a | landed #114 (auth hardening + CheckBindExposure, mDNS dual-advertise, goreleaser/Docker/systemd/launchd packaging). Deferred→verify.md: push/FCM §13.8, install-service §13.13, Windows release (blocked by internal/lsp unguarded Unix syscalls) |
| **P07-A** mobile v0 | Android v0 against real opencode daemon | `07-client-mobile.md` §Phase A | mobile | partial | — | UI fidelity passes ongoing (#58–#61) |
| **P07-B** mobile repoint | repoint Android to Opcode42 daemon | `07-client-mobile.md` §Phase B | mobile | done | P02-M11 | landed #115 (fixed fully-broken SSE parse; added `android` CI job). Surfaced daemon SSE gap → see followup |
| **P07-C** mobile parity | full feature parity | `07-client-mobile.md` §Phase C | mobile | done | P07-B | landed: PTY terminal #118, archive/rename UI #126, push client #129, KMP extraction #130. Followup: actual iOS app target |
| **P08** tui | Phases 0–3 done; remaining polish phases | `08-client-tui.md` | tui | done | P02-M9a | VT pane (#80)+attrs (#120), diff markers (#108), /command parity (#120). Followup: VT scrollback, /command source expansion |

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

As of "finish the plan" completion (2026-06-04, main @ 2768f35). **ALL clearly-scoped plan tracks DONE — 33 feature PRs merged (#98–#130).** Every ledger row above is `done` except P07-A (partial, ongoing UI-fidelity passes — superseded in practice by P07-B/C against Opcode42). Phase B conformance-green; daemon + full ecosystem (MCP/LSP/plugins/provider+MCP OAuth incl. refresh) + SDKs (Kotlin/Swift/Go) + both clients (TUI + Android, fully repointed w/ PTY/archive/push/KMP) all merged. Most earlier "open followups" were CLOSED this effort: daemon session-lifecycle SSE gap → #119; Windows build → #117; plugin Phase-D → #123; provider-OAuth-wiring → #125; FCM → #127; OAuth refresh → #121.

REMAINING = POST-V1 BACKLOG ONLY (nothing on the v1 critical path):
- **~18 auto-install LSP servers** — the one substantial item; needs per-language download/npm/dotnet-tool installer machinery (eslint, vue, biome, elixir-ls, zls, csharp, jdtls, kotlin-ls, yaml-ls, lua-ls, etc.). The 14 PATH-resolved built-ins landed in #117. **HELD for explicit human go/no-go** (kickoff parked the full ~32 table as post-v1).
- Minor polish (small, mop-up-able): VT scrollback; GET /command source expansion (skill/MCP/built-in sources, plan 04 M6) for opcode42-vs-opencode set parity; per-session permission ruleset persistence (#124); Swift Opcode42Client auth-wrapper parity with Kotlin; an actual iOS app target consuming the new KMP commonMain.

Pre-existing stale PRs unrelated to these waves: #96 (tui graphics-fineness), #71 (WIP android mDNS) — flag to human, not orchestrator-owned.
**Nothing in flight. Plan complete bar post-v1 backlog. Awaiting human decision on the ~18-server LSP auto-install effort + whether to mop up the minor polish.**

## Run log
<!-- Append one line per dispatch/merge: `2026-06-03 P03-M3-3 dispatched (worktree wt-lsp)` etc. -->
2026-06-03 Wave-1 fully merged & synced to main @ 674ad79 (#98 #99 #100 #101 #102); stale worktrees pruned.
2026-06-03 docs: committed agent-owns-PR git workflow + Wave-2 scaffolding directly to main @ 3fa7b3f (user-approved, no PR).
2026-06-03 Wave-2 dispatched in parallel (own-PR-until-merged lifecycle, self-merge):
  - B2 LSP client/diagnostics (plan 03 M3-4) — agent ad34fce615fc99e3c — owns internal/lsp/, adds go.lsp.dev deps.
  - Track D dual-run + scenarios (plan 12 / 02 M11) — agent aeca38799e1407058 — owns conformance/, verifies Opcode42↔Gemini auth gap first.
  - Track F perf W0 (plan 11) — agent ac6050bd4aa832f21 — new bench harness, measures opencode baseline first.
2026-06-03 B2 MERGED → #103 (a9ef55b): LSP JSON-RPC client + diagnostics, GET /lsp + lsp.updated SSE. Deps added: go.lsp.dev/{jsonrpc2,protocol,uri}. P03-M3-4 done.
2026-06-03 B3 dispatched (sequential after B2) — agent a2a9cd5b66cb74d87 — LSP query ops + internal/engine/tool/lsp.go (operation enum, 1→0-based, HasClients). P03-M3-5.
2026-06-03 Track F (#105) opened + CI-green but agent STOPPED at review without merging (returned 5 should-fix findings: SSE-hang, mislabeled sub-count, p99==max, rps over-report, no ctx backstop). SendMessage unavailable → dispatched continuation agent ab2fc1e70253e9b0c to fix in the existing worktree + self-merge #105.
2026-06-03 Track D MERGED → #104 (a5e170f): live dual-run mode + 5 skip-gated agent-flow scenarios; closed Opcode42↔Gemini auth gap (builtinBaseURL → Gemini OpenAI-compat endpoint). P12-suite done. Caveat: full 5-scenario green baseline blocked by free-tier Gemini daily quota; 3 pre-existing divergences tracked in known-divergences-live.json (engine info.mode/path.root; /session/:id/summarize 501).
2026-06-03 NOTE: D agent left 2 orphaned uncommitted EventTypes/normalize test files in the MAIN working tree (not in its PR branch). Verified they build+pass against merged main, but direct push of unreviewed test code to main was (correctly) blocked → discarded. Bonus coverage; can be re-added via a future conformance PR if wanted.
2026-06-03 Track F MERGED → #105 (4ae38ac): bench/ W0 baseline harness + head-to-head vs opencode. Continuation agent fixed all 5 should-fix findings (bounded-ctx SSE, honest sub-count, interpolated p99, true-elapsed throughput, run-level deadline) + refreshed measured baseline; review clean, CI green. NOTE: both F agents stopped at review w/o merging — orchestrator drove CI-watch + squash-merge. Plan-11 W0 done.
2026-06-03 B3 MERGED → #106 (425b816): LSP query ops (9 ops, exact opencode enum strings) + lsp engine tool (1→0-based verified, OPENCODE_EXPERIMENTAL_LSP_TOOL-gated). Self-merged cleanly. P03-M3-5 done.
2026-06-03 B4 dispatched (final LSP track, sequential after B3) — agent a43e397c3cccceeee — M3-6 SSE bus wiring (lsp.updated + mcp.tools.changed) + real ResolvePromptParts (@file/@dir/@symbol via WorkspaceSymbol); owns engine.go/resolvePromptParts alone. Prompt hardened with explicit do-not-stop-at-review rule. P03-M3-6.
2026-06-03 B4 MERGED → #107 (2271724): real @file/@dir/@symbol ResolvePromptParts (ported from opencode session/prompt.ts) + verified LSP/MCP SSE envelope (lsp.updated → {}, mcp.tools.changed → {server}). Self-merged after hardened do-not-stop-at-review prompt. P03-M3-6 done. ⇒ WAVE 2 COMPLETE (B2 #103, B3 #106, B4 #107, D #104, F #105).
2026-06-03 WAVE 3 dispatched (parallel, distinct tracks; hardened do-not-stop-at-review prompt; self-merge):
  - P02-M11 conformance exit gate (plan 02 M11) — agent abff06c754797d221 — owns conformance/ (+engine SSE fixes); SSE catalog derived authoritatively from opencode source per human decision.
  - P05 plugin host (plan 05) — agent a9f3610855c94e2b2 — Node/Bun sidecar, flag-gated; seam: cmd/opcoded/main.go (additive).
  - P13-oauth provider OAuth (plan 13) — agent aaf2ce9c159e069a5 — owns OAuth surface; seam: cmd/opcoded/main.go + provider auth (additive).
  - P08 TUI polish (plan 08) — agent a03c37e00c2166965 — owns internal/tui/; scoped to the NEXT single phase only.
2026-06-03 P08 MERGED → #108 (5e222da): colored +/- diff sign markers (plan 08c M6 residual). Self-merged clean. P08 stays `partial` — remaining: 08c bg-pulse (optional), 08b §3 workspaces/§8 tags (daemon-gated), 08b §4 auth (P13 lane), U13 TUI↔Opcode42 dual-run parity (gated on P02-M11). Note: U12 PTY/VT pane already shipped (#80) — stale review row corrected.
2026-06-03 P13-oauth MERGED → #109 (1fcbadd): end-to-end provider OAuth (PKCE auth-code, shared 127.0.0.1 loopback callback, CSRF state, token exchange → shared auth.json; xAI as first provider; --oauth-callback-proxy-url for headless). Self-merged; review fixed loopback-port + shutdown nil-deref. P13-oauth done → UNBLOCKS P03-M3-2 (mcp oauth). Deferred: token refresh, more providers, MCP OAuth (stays 501).
  ⚠️ CROSS-TRACK FOLLOWUP: conformance/known-divergences.json still marks provider-oauth/provider-auth as "deferred/501" — now stale. The P02-M11 agent (owns conformance/) should refresh to "implemented for built-in providers (plan 13)"; if it doesn't, orchestrator spins a tiny follow-up after M11 lands.
2026-06-03 P05 MERGED → #110 (cb2cb1b): plugin host — unix-socket JSON-RPC 2.0 sidecar (Bun/Node auto-detect), plugin discovery + tool registration, Go bridge (blocking Trigger hooks + event fan-out + crash isolation), flag-gated off (--plugin-host/OPCODE_PLUGIN_HOST=1). Self-merged; rebased 2x over P13 cmd/opcoded seam (additive); review fixed a startup-hang on pre-ready crash. Phase-D hooks (auth/provider, tool.before/after, permission.ask, compaction) still stubbed. P05 done.
  ⚠️ HARNESS FLAKE (flagged by P13 + P05 independently): scripts/run-conformance.sh self has a nondeterministic opencode-vs-opencode session-LIST ordering flake on base main (CI self-diff passes; local intermittent). Conformance owner (P02-M11) should add order-insensitive normalization (à la E1 permission normalizer) — verify at M11 merge; else tiny follow-up.
2026-06-03 P02-M11 (#111) opened, rebased on main, CI-green; agent handled BOTH cross-track followups (known-divergences refresh for #109; NormalizeSetJSON masks the session-list flake) but STOPPED at review with 1 unaddressed should-fix: summarize `auto` flag parsed (prompt_handlers.go:213) yet dropped (engine.SummarizeInput lacks Auto → hardcoded auto=false) → wire divergence on CompactionPart.auto. Dispatched continuation agent a87628164fbe9cfc4 to fix that one finding + self-merge #111.
2026-06-04 P02-M11 MERGED → #111 (e23fe49): end-to-end SSE conformance — authoritative event catalog enumerated+cited from opencode source (server.heartbeat correctly excluded as transport-injected), session.status/idle ordering, mode/path.root fixes, NormalizeSetJSON masks the session-list flake, known-divergences refreshed for #109. Continuation agent fixed the summarize `auto`-flag drop (engine.SummarizeInput.Auto → createCompaction). Agent stopped at clean review; orchestrator merged. 🎯 PHASE B EXIT GATE GREEN. ⇒ WAVE 3 COMPLETE (#108/#109/#110/#111).
  Both cross-track followups (known-divergences refresh, session-list flake) were handled inside #111 — no separate follow-up needed.
2026-06-04 WAVE 4 dispatched (single track, user-selected — TUI-first): P08/U13 TUI↔Opcode42 dual-run parity (plan 08 U13) — agent a66197823ec439aad — re-point TUI at Opcode42 daemon + parity gate; owns internal/tui/; gemini key throttled so deterministic/mocked flows only. (P07-B/P06-P2/P03-M3-2/P13-rest deferred to a later wave per user.)
2026-06-04 P08/U13 MERGED → #112 (8a93f0b): TUI↔Opcode42 dual-run parity gate (deterministic, key-free httptest harness driving real Model.Update against real internal/server + engine + mock provider; covers health/SSE, prompt stream, permission round-trip, abort). TUI was already wire-generic — NO wiring gaps, NO daemon followups. Self-merged (hardened prompt held). P08 stays `partial`: U12 in-TUI VT pane (needs VT-emulator dep; WS-PTY transport exists) + GET /command ordering divergence remain.
  ⇒ WAVE 4 COMPLETE. Remaining READY parked per user: P07-B (mobile repoint), P06-P2 (sdk self-emit), P03-M3-2 (mcp oauth), P13-rest (remote hardening). Project status: Phase B conformance-green; 13 PRs merged this run (#98–#112; #103/#106/#107 LSP, #104 conformance, #105 perf, #108 tui-diff, #109 oauth, #110 plugin, #111 M11, #112 U13).
2026-06-04 WAVE 5 dispatched (parallel, distinct tracks; hardened carry-to-merge prompt; self-merge):
  - P07-B Android repoint (plan 07 Phase B) — agent a8c50304f6cc3ac7b — owns ./android; live-daemon/LLM steps → tasks/verify.md.
  - P06-P2 SDK self-emit (plan 06 Phase 2) — agent a2c9976d4b8e94d34 — Opcode42 emits own openapi + diff gate vs frozen; owns codegen/spec path.
  - P03-M3-2 MCP OAuth + mutating /mcp (plan 03 M3-2) — agent af0aeb400606085c5 — reuse internal/oauth (#109); needs_auth/needs_client_registration + POST /mcp add/connect/disconnect/auth; seam: internal/server (additive, w/ P13-rest).
  - P13-rest remote hardening (plan 13 non-oauth) — agent ae3b97a4cfd3706c1 — push notif/packaging/remote hardening; seam: internal/server+cmd/opcoded (additive, w/ P03-M3-2).
2026-06-04 P06-P2 MERGED → #113 (2038b62): Opcode42 self-emits /openapi.json from its route table (info/components reused; paths rebuilt; non-reference ops tagged x-opcode42-addition); 2-way drift gate (offline Go test + CI spec-drift via check-spec-drift.sh) classifying missing/changed=FAIL, additive=FAIL-unless-known-addition. Drift: GET /doc + /openapi.json → known-additions.json (WARN). Review fixed a path-item parse edge case. Self-merged.
2026-06-04 P13-rest MERGED → #114 (1f1baf2): auth hardening (constant-time compare; CheckBindExposure refuses non-loopback bind w/o password — stricter than opencode's warn), mDNS dual-advertise (_http._tcp + _opencode._tcp), packaging (goreleaser static CGO-free multi-arch + Docker/ghcr + systemd/launchd + release-on-tag CI w/ <40MB gate, ~15MB actual). Self-merged; review clean. DEFERRED→verify.md: push/FCM §13.8, install-service generator §13.13, Windows release. ⚠️ FOLLOWUP: internal/lsp/service.go uses unguarded Unix syscalls → blocks Windows target (out of P13 scope; for an lsp-track fix).
2026-06-04 P07-B MERGED → #115 (a11a1bc): Android repointed to Opcode42. App SSE path was fully broken (all events → Unknown); fixed envelope unwrap (/global/event {payload,directory}), nested field reads (part-under-part, partID vs id, info-nested updates), coalesce keys; added `android` CI job (client never built in CI before). 19 unit tests; review clean. Self-merged. P07-C now READY.
  ⚠️ DAEMON FOLLOWUP (conformance/engine gap — NOT caught by P02-M11 gate): Opcode42 does NOT emit session.created/updated/deleted or message.removed from its session handlers (only engine message/part events). App works around via REST refresh on navigation, but live SSE session-list auto-refresh needs the daemon to publish these. M11's catalog/Track-D scenarios focused on the agent message flow, not session-lifecycle CRUD events → real coverage hole in the "Phase B green" claim for those event types. Owner: conformance/engine track.
2026-06-04 P03-M3-2 MERGED → #116 (8e5f1f9): MCP OAuth (DCR+PKCE via mark3labs/mcp-go) + persistent token store → shared $DATA/opencode/mcp-auth.json; needs_auth/needs_client_registration on GET /mcp; mutating /mcp 501→real (add/connect/disconnect/auth/auth/callback/authenticate, DELETE auth) w/ opencode error shapes. Self-merged; rebased onto #113's new spec-drift gate (routes match frozen contract); review bounded the DCR probe by server timeout. Deferred: token-refresh dual-run gate. ⇒ WAVE 5 COMPLETE (#113/#114/#115/#116).
2026-06-04 ===== SESSION SUMMARY: Waves 2–5 dispatched & all merged — 14 PRs (#103–#116, excl. the pre-existing #96/#71). Phase B conformance-green. Whole planned v1 backend+ecosystem+clients-repointed done. Remaining: mobile Phase C parity (P07-C), TUI U12 VT pane, + the OPEN FOLLOWUPS in the Ready-set block. Stop-at-review pattern recurred ~5×; hardened carry-to-merge prompt fixed it for later agents; orchestrator drove the finish whenever an agent stalled review-clean+CI-green. =====
2026-06-04 "go, finish all tasks in plan" → WAVE 6 dispatched (5 parallel distinct-tree tracks; hardened carry-to-merge):
  - SSE-lifecycle (engine/conformance) — agent acbf69ef4f1b7ff77 — emit session.created/updated/deleted + message.removed + conformance coverage (closes the #115 gap). OWNS internal/engine+internal/server+conformance this wave.
  - P08-finish U12 (tui) — agent a5e48403ac7f1a0c5 — in-TUI VT pane (consumes WS-PTY #80) + GET /command deterministic ordering.
  - P07-C (mobile) — agent a2ebbac020b52b29d — Android Phase-C parity; one coherent slice/PR, rest → same-track re-dispatch.
  - LSP-expand (lsp) — agent ae32b6caac13ac81b — build-tag-split Unix syscalls (unblock GOOS=windows) + expand built-in server table.
  - P06-SDKs (sdk) — agent a3f3db02bfc95fa79 — Kotlin (priority, for android) + Swift SDK gen + finish Go SDK; wire freshness gate.
  SEQUENCED AFTER the engine track merges (both edit internal/engine → seam): plugin Phase-D hook bridges (#110 follow-on) + provider/MCP OAuth token-refresh (#109/#116 follow-on). Not yet launched.
2026-06-04 LSP-expand MERGED → #117 (e4174d8): Windows build unblocked (procgroup_unix.go/procgroup_windows.go build-tag split; GOOS=windows go build ✓) + 14 PATH-resolved built-in LSP servers added (deno/ruby/rust/clangd/dart/php/prisma/ocaml/bash/terraform/dockerfile/gleam/clojure/nixd). Self-merged; review clean. NOTE: this CLEARS the P13-rest Windows blocker → goreleaser Windows target can be re-enabled (small packaging followup). FOLLOWUP: ~18 more LSP servers need download/npm/dotnet auto-install machinery (separate work item, still post-v1 backlog).
2026-06-04 P07-C slice MERGED → #118 (df86b1d): WS-PTY terminal made functional (text-frame handling via Kotlin TerminalEmulator: CR/LF/BS/TAB+CSI/SGR/OSC/charset; 0x00+{cursor} reconnect-resume; resizePty→PUT /pty/{id}). Self-merged; review fixed charset-escape leak + resize spam. P07-C stays `partial`.
  Remaining Phase-C (mobile track, sequential, MOSTLY DAEMON-GATED): KMP extraction (independent, doable now); session archive UI (gated ↓); push/FCM client (gated ↓).
  ⚠️ NEW DAEMON FOLLOWUPS (surfaced by #118):
   (a) No PATCH /session/{id} route in internal/server/session_handlers.go → renameSession + archive 404. Needs PATCH route + time.archived persistence (internal/session SetTitle only writes title). [server/engine task]
   (b) No push-notification/FCM relay infra (plan 13 §13.8, deferred). [remote task]
2026-06-04 SSE-lifecycle MERGED → #119 (819d329): session.created/updated/deleted + message.removed now emit (opencode-matched shapes via session.Store WithBus→instance bus); new DELETE /session/:id/message/:messageID; conformance catalog now gates session-lifecycle CRUD (catalog_test + sse_lifecycle_test). Self-merged; review clean; no divergences. Engine track CLEAR.
2026-06-04 SEQUENCED engine-seam wave dispatched (3 disjoint-tree tracks, after #119):
  - plugin Phase-D hooks — agent ab758b6e5d623141d — tool.before/after, permission.ask, messages.transform, compaction hook bridges (internal/engine + pluginbridge); flag-gated, no-op when host off.
  - OAuth token-refresh — agent aaf2901a20709cd14 — at-request refresh_token grant for provider (#109 internal/oauth) + MCP (#116 internal/mcp); persist back to shared stores; failure→needs_auth.
  - PATCH /session/{id} + archive — agent a5c15927dcfb514f7 — missing route (rename+archive, time.archived persist) surfaced by #118; owns internal/server(session)+internal/session; regen spec for #113 drift gate. Unblocks android archive UI.
  Still running from Wave 6: P08-U12 (tui) a5e48403, P06-SDKs (sdk) a3f3db02. (FCM relay §13.8 + remaining mobile slices sequenced next.)
2026-06-04 P08-U12 MERGED → #120 (8205c63): VT-pane text-attribute rendering (bold/underline/italic + SGR-reverse via Lipgloss decoding vt10x Glyph.Mode; no new dep — vt10x already vendored by #80). CLARIFICATION: the interactive embedded VT terminal already shipped in #80; only this attribute follow-up remained. /command ordering RESOLVED → order-insensitive `command-list` parity scenario (NormalizeSetJSON), removed from exclusions (now 17 conformance scenarios, 0 blocking). Self-merged; review clean. P08 effectively COMPLETE; followups: VT scrollback (#80 tail), GET /command source expansion (skill/MCP/built-in — plan 04 M6) for opcode42-vs-opencode set parity.
2026-06-04 plugin Phase-D MERGED → #123 (1646016): hook bridges wired — tool.execute.before/after (before fires pre-permission-gate; after only on success), chat.message (observe), messages.transform, compaction hooks; flag-gated, no-op when host off; host-off path unchanged. Agent stopped at clean review; orchestrator watched CI (6 green) + squash-merged. NON-BLOCKING followup: tool.before arg-rewrite not persisted to stored tool part (opencode persists it) — pre-existing processor seam, observability-only. Plugin track DONE (M2–M7 hooks).
2026-06-04 OAuth token-refresh MERGED → #121 (1e50b6f): provider refresh via Service.Access() (refresh_token grant, 120s skew + unsigned-JWT exp check, per-provider single-flight, retains non-rotated refresh token; failure→ErrNeedsReauth, stored record never clobbered) + MCP refresh live through mcp-go persistentTokenStore→mcp-auth.json. Self-merged; review fixed single-flight panic-safety. ⚠️ FOLLOWUP: provider Access() not yet wired into engine LLM request path (no consumer today) — when provider-client construction resolves credstore creds, call oauth.Service.Access() + map ErrNeedsReauth to re-auth prompt.
2026-06-04 PATCH /session MERGED → #124 (83dde39): PATCH /session/{id} (title + time.archived partial update; full Session response; 400 body-validation parity incl. empty/unknown-field/wrong-type; matches frozen openapi → spec-drift green; one logged divergence: malformed-JSON 400-vs-opencode-500). session.Store.Update publishes session.updated (reuses #119). Self-merged; 2 review rounds fixed 3 body-validation divergences; dual-run 0-blocking. UNBLOCKS android rename/archive UI. Deferred: per-session permission ruleset persistence (accepted-and-ignored).
2026-06-04 FOLLOW-ON wave dispatched (3 tracks; #122 SDKs still running):
  - P07-C archive/rename UI (mobile) — agent ab0dcb9ba401721dd — wire rename+archive to PATCH /session/{id} (#124); archived filtering; owns ./android.
  - provider-oauth-wire (engine) — agent ab7e680782135822e — wire oauth.Service.Access() (#121) into LLM provider-client construction (credential precedence: oauth token else api-key); cmd/opcoded seam w/ FCM (additive).
  - FCM-relay (remote/server) — agent a3a7f11fe01d4bcd3 — daemon push relay (plan 13 §13.8): device-token registration + event→notification mapping + FCM v1 send (no-op without creds; live=manual-verify); new internal/push + server endpoints + spec regen.
2026-06-04 provider-oauth-wire MERGED → #125 (54ce46a): oauth.Service.Access() now consumed at provider-client construction via internal/engine/provider/credresolve (precedence: oauth access token w/ refresh else api-key; matches opencode xai.ts:596/657); ErrNeedsReauth surfaced+handled at all 3 call sites (loop/compaction/title) w/o crash; api-key path unchanged; no token logging. Agent stopped at clean review; orchestrator watched CI (6 green) + merged. Closes #121's "not yet wired" followup → provider OAuth is now end-to-end usable.
2026-06-04 P07-C archive/rename UI MERGED → #126 (467668d): long-press rename(dialog)+archive(active rows); client-side active/archived partition; "Archived (n)" badge → archived-only view (no FAB/archive, set-only); Opcode42Client.archiveSession→PATCH /session/{id} {time:{archived:now}}; SessionTime.archived:Long?; live session.updated reflection; 2 wire tests. Self-merged (NOTE: self-reviewed — Agent spawn unavailable in its context; landed CI-green). No daemon followups. Remaining Phase-C: KMP extraction + push/FCM client.
2026-06-04 P06-SDKs MERGED → #122 (cf91695): openapi-generator-cli 7.10.0 (pinned). Kotlin SDK FULLY landed (typed REST client jvm-okhttp4+kotlinx.serialization + Opcode42Client wrapper w/ Basic auth + X-Opencode-Directory; gradle build CI-green). Swift scaffolded (committed+drift-checked, NOT compile-gated — swift5 mis-renders one array-of-array schema → followup). Go SDK already complete. New `sdk-fresh` CI job (regen + git-diff + Kotlin compile). Self-merged; fixed gradle-CLI-on-CI + arch-dependent codegen non-determinism (orphan EventTui schemas dropped in normalization, byte-identical across arches). FOLLOWUP: Swift QuestionAnswer array-of-array fix + compile gate; optional android→sdk/kotlin/gen migration.
2026-06-04 FCM-relay MERGED → #127 (f5eb42d): daemon-side push relay (plan 13 §13.8). Self-merged. (Agent finishing report; worktree pruned on notify.) Unblocks android push client.
2026-06-04 FINAL-TAIL wave dispatched (2 distinct tracks):
  - P07-C push client (mobile) — agent a3533ce1afe4cbbcf — Android FCM: token register→POST /push/register (#127), notification + tap deep-link; gated to build without google-services.json; live=manual-verify.
  - swift-sdk (sdk) — agent ab5c691f6e112606f — fix Swift array-of-array (QuestionAnswer) render so SDK compiles + enable sdk-fresh Swift compile gate (#122 followup).
  REMAINING after this: KMP extraction (mobile, after push client) · ~18 auto-install LSP servers (BIG post-v1 machinery: download/npm/dotnet-tool) · minor polish (VT scrollback, GET /command source expansion, per-session permission persistence #124).
2026-06-04 11:33 RESUMED after user pause. Nothing merged during pause (main still 7c6b11a). Both final-tail agents were cut off by the session usage limit (reset 11:30) BEFORE committing/PR — partial work left uncommitted in their worktrees: push-client had substantial progress (new android/feature/notifications/ + 9-file wiring), swift-sdk had only scripts/gen-sdks.sh touched. Dispatched CONTINUATION agents into the existing worktrees:
  - finish push client — agent ad29c90976e0830ae (worktree agent-a3533ce1afe4cbbcf, picks up partial FCM module).
  - finish swift SDK — agent aa8c19b742aee4cda (worktree agent-ab5c691f6e112606f).
  KMP extraction (mobile) sequenced AFTER push client merges (same ./android tree).
2026-06-04 P07-C push client MERGED → #129 (c4dfa21): continuation finished the cut-off agent's :feature:notifications module — FCM token→POST /push/register ({device_id,fcm_token,platform}), onNewToken re-register, DELETE on server removal; Opcode42MessagingService→NotificationPublisher→tap deep-links to Chat via {event_type,session_id}; gms not applied + Firebase runtime-gated on PushConfig so CI no-config build is clean; 11 JVM tests. Review fixed a dup-tap bug. ⇒ FCM chain end-to-end. Manual-verify appended.
2026-06-04 swift-sdk #128: sdk-fresh RED — generated URLSessionImplementations.swift imports MobileCoreServices under `#if !os(macOS)` (Apple-only; absent on Linux CI) → swift build fails. NOT my doc edit. Continuation agent a025fed122528eac5 fixing (patch import guard to #if canImport(...) in gen-sdks.sh normalization, deterministic). [orchestrator had fixed 3 misleading doc comments in fb43f49 first]
2026-06-04 KMP extraction dispatched (final mobile slice) — agent a66ade4e2d7f71b31 — :core:network/:core:store → Kotlin Multiplatform (commonMain + expect/actual), app still builds; owns ./android.
2026-06-04 swift-sdk MERGED → #128 (ab9e73b): Swift SDK now compiles on Linux — continuation patched generated infra (canImport guards replacing #if !os(macOS); FoundationNetworking import) deterministically in gen-sdks.sh (rm -rf→regen→patch, count!=1 drift-guard); verified built in swift:6.0 Linux container = CI env; regen byte-identical. sdk-fresh now compile-gates Swift. Orchestrator merged after agent stopped at clean review (CI 7/7 green). Swift SDK DONE. [3 prior misleading doc comments also corrected]
2026-06-04 KMP extraction MERGED → #130 (2768f35): :core:model + :core:store + :core:network → Kotlin Multiplatform (kotlin("multiplatform")+androidTarget; commonMain shared, androidMain for Hilt/OkHttp/lifecycle; expect/actual for currentTimeMillis/randomIdSuffix; SSE tests→commonTest 21/21). App builds unchanged (Hilt singleton scoping preserved); commonMain now iOS-ready. Self-merged; review clean. P07-C mobile Phase-C DONE (PTY #118, archive/rename #126, push #129, KMP #130).
2026-06-04 ===== "finish the plan" COMPLETE for all clearly-scoped tracks. Merged this effort: #117 LSP-windows+servers, #118 PTY, #119 SSE-lifecycle, #120 TUI-VT-attrs, #121 oauth-refresh, #122 SDKs, #123 plugin-PhaseD, #124 PATCH-session, #125 provider-oauth-wire, #126 archive-UI, #127 FCM-relay, #128 swift-linux, #129 push-client, #130 KMP. (33 feature PRs total #98–#130.)
  REMAINING = post-v1 backlog only: (1) ~18 auto-install LSP servers (download/npm/dotnet-tool machinery — needs human go/no-go); (2) minor polish: VT scrollback, GET /command source expansion (skill/MCP/built-in), per-session permission persistence (#124), Swift Opcode42Client wrapper parity, actual iOS app target. =====
