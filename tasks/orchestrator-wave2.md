# Opcode42 v1 — ORCHESTRATOR kickoff (Wave 2)

Paste this to start the next orchestrator session. You are the ORCHESTRATOR for Opcode42 v1 parallel
execution. Opcode42 is a Go daemon, wire-compatible with opencode (reference: `/Users/rotemmiz/git/opencode`).
**Read `CLAUDE.md` first.** The generic mechanics (roles, dispatch loop, context hygiene, gate, worker
templates) live in `tasks/orchestrator.md` — follow them, with the POLICY OVERRIDE below.

## READ FIRST (source of truth)
1. `~/.claude/plans/plan-execution-of-all-lively-kazoo.md` — the parallelized v1 roadmap (tracks A–F).
2. `plans/00-masterplan.md` → "Decisions locked (2026-06-03)" — binding; do NOT re-litigate.
3. Each `plans/*.md` "Review pass (2026-06-03)" section.
4. Memories: `[[opcode42-v1-orchestration]]`, `[[orchestrator-lean-and-seams]]`, `[[feedback-mimic-ci-before-push]]`.

## POLICY OVERRIDE vs tasks/orchestrator.md (changed 2026-06-03)
- **Hosted CI is AVAILABLE again — rely on it.** (The runbook/old memory said it was exhausted; ignore that.)
- **Each track agent owns its PR end-to-end and SELF-MERGES** — this replaces the runbook's model where
  the orchestrator spawns the reviewer and merges. Per the `CLAUDE.md` "Git workflow", the agent must:
  implement → local pre-push gate → push + `gh pr create` → **spin a SEPARATE review subagent**, fix all
  blocking/should-fix, push, re-review until clean → **wait for green CI** (`gh pr checks <pr> --watch`)
  → **`gh pr merge --squash`** (rebase + re-gate if main moved) → sync. The agent exits ONLY after merge.
  NO `Co-Authored-By`. So your job shrinks to: dispatch, track PR numbers, resolve cross-track conflicts.
- **Stay lean** (Wave 1 burned 130K in the orchestrator): delegate every rebase/conflict-resolution to a
  subagent; never `Read` whole files here (targeted `grep`/`sed -n` only); pipe gates to `tail`.

## STATE (main @ 674ad79; verify with `git log`)
**Wave 1 fully MERGED & green:** #98 (E2 /provider Model wire shape + un-skipped M10 test), #99 (B1 LSP
foundation — subset gopls/typescript/pyright, no client yet, 0 new deps), #100 (A agent M9: MaxSteps +
MAX_STEPS sentinel, structured output, title-gen), #101 (E1 order-insensitive permission normalizer →
self-conformance deterministically green), #102 (C MCP remote transport + tools.changed + permission-gated
calls).
- **Uncommitted on main:** `CLAUDE.md` (the new Git-workflow lifecycle) + `README.md` (pre-existing).
  Resolve with the user: PR it through the new lifecycle, or commit the governance doc directly.
- **Prune stale worktrees:** `.claude/worktrees/agent-*` (`git worktree prune`).

## CONFLICTS CLUSTER AT WIRING SEAMS (Wave 1 lesson)
Multiple tracks editing the same constructors caused every Wave-1 conflict:
`internal/instance/instance.go`, `internal/server/prompt_handlers.go`, `internal/engine/engine.go`,
and shared `conformance/known-divergences.json`. **This wave: ONE track owns each wiring file**; others
add only their field via the owner or land sequentially. Merge low-conflict PRs first, doubly-conflicted last.

## LIVE-RUN ENVIRONMENT (for LLM dual-run + perf)
- `opencode` is on PATH (a server also runs at `http://127.0.0.1:4096`, but the harness
  `scripts/run-conformance.sh` **spawns its own pristine opencode** with a fresh temp HOME, so it does
  NOT inherit that server's auth/config).
- **Gemini key:** `~/.opcode42/conformance.env` (600, outside repo) — `export GOOGLE_GENERATIVE_AI_API_KEY=…`.
  Shared `~/.local/share/opencode/auth.json` has only an `opencode` entry (no google). Any agent/harness
  needing the model must `set -a; . ~/.opcode42/conformance.env; set +a` first.
- **Pin scenarios to model `google/gemini-2.5-flash`** (confirm with user if auth fails). LLM output is
  non-deterministic → assert SHAPE, not text.
- **VERIFY FIRST in Track D:** Opcode42's `internal/` reads no Google env var (opencode reads
  GEMINI_API_KEY/GOOGLE_API_KEY/GOOGLE_GENERATIVE_AI_API_KEY) — Opcode42 may not yet authenticate to Gemini.
  Close that gap (env-var read or shared-auth.json google entry) before live dual-run.
- The EXISTING conformance gate is keyless and already gates every PR; the key is ONLY for new LLM
  scenarios (D2) and real-load perf (F).

## WAVE 2 — launch in parallel (disjoint subsystems)
- **B2 — LSP client/diagnostics (plan 03 M3-4).** Owns `internal/lsp/`. `client.go` on
  `go.lsp.dev/jsonrpc2`+`protocol` (ADDS go.mod deps — only dep-touching track; if it merges first, the
  others rebase); initialize handshake; push+pull diagnostics (dedup + 150ms debounce);
  `Service.{TouchFile,Diagnostics,Status}`; `GET /lsp`; `lsp.updated` SSE. Live gopls integration test
  (skip-gate if binary absent). Then **B3 (M3-5)** query ops + `internal/engine/tool/lsp.go` (opencode
  `operation` enum, 1-based→0-based, HasClients pre-check) and **B4 (M3-6)** bus wiring +
  real `ResolvePromptParts` (@file/@dir/@symbol via WorkspaceSymbol). **B3→B4 SEQUENTIAL after B2; B4
  owns `engine.go`/`resolvePromptParts` alone.**
- **Track D — dual-run + scenarios (plan 12 / 02 M11).** Owns `conformance/`. Verify the Opcode42-Gemini
  auth gap first. D1: live dual-run mode in the runner (loosen model-output fields — E1 already did the
  order-insensitive permission normalizer). D2: scenario suite (prompt text-only, one tool-call,
  permission round-trip, compaction, abort) replayed vs both daemons → the green baseline (Phase-B
  "conformance-green" exit).
- **Track F — perf W0 (plan 11).** New bench harness. **Measure opencode FIRST** (cold start, idle RSS,
  SSE fan-out, throughput) on this machine; SQLite fixed at pure-Go `modernc`. **No "Nx" claim until both
  daemons are measured head-to-head.**

## LOCKED DECISIONS (don't reopen)
Unimplemented endpoints (v1/sync/experimental) → 501. Strictness: missing/changed FAILS, extra additive
WARNS (`known-additions.json`). Single-user. OAuth deferred — API keys only (MCP & provider OAuth stay
501). Windows out of scope. `/command` sorted (known-addition). M10 built.

## POST-V1 BACKLOG (deferred)
Full LSP table (~32 servers), OAuth (remote-MCP + provider), plugin host (plan 05), TUI VT pane (plan 08
U12), remote-ops hardening (plan 13), Kotlin/Swift SDKs (plan 06), finish Go SDK.

## START
1. `git checkout main && git pull`; prune stale worktrees; resolve the uncommitted `CLAUDE.md` with the user.
2. Launch Wave-2 agents (B2 + D + F) in background worktrees under the own-it-until-merged lifecycle.
3. Report PR numbers as they open; resolve cross-track conflicts via subagents; stay lean.
