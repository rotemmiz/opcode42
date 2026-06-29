# Opcode42 — Finish Phase B (plan 02: agent engine) + API/SSE confirm

Continuation handoff for a fresh context. **Do not touch `android/`** (a parallel agent owns it).

## Orientation

- **Repo:** `/Users/rotemmiz/git/opcode42`, branch `main`, clean, at `b546037`.
  (If you were pointed at a different clone like `opcode42-2`, first confirm it is synced to this
  `main` — PRs #32/#34/#35 below must be present, or you'll rebuild merged work.)
- **Plans are the source of truth.** Read `plans/00-masterplan.md` (phases), then
  `plans/02-agent-engine.md` (this is Phase B), and `plans/12-test-compatibility.md` (the
  conformance harness). Don't re-architect; update a plan only if it contradicts reality.
- **Reference codebase:** `/Users/rotemmiz/git/opencode`. Validate every wire claim against it
  (cite `file:line`). Frozen contract: `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json`.
- **Phases (masterplan §sequencing, line 88-93):** A = harness+daemon scaffold + mobile;
  **B = plan 02 agent loop → conformance green → repoint TUI**; C = plans 03/04 (MCP/LSP/
  resources); D = plugin host / remote / TUI polish.

## What is ALREADY DONE — do not redo

The TUI gap-closing track is merged; Opcode42's daemon now serves (was 501 before):
- **#32:** `POST /permission/{id}/reply`, `POST /question/{id}/reply|reject`,
  `GET /session/{id}/todo`. Reshaped `question.Manager` to opencode's multi-question model;
  registered the `question` tool; shared `tool.TodoStore`.
- **#34:** `GET /find/file` (fuzzy file/dir search; `internal/server/find_handlers.go` +
  `fuzzy.go`).
- **#35:** `GET /provider`, `GET /agent`, `GET /command` — the `internal/resource` package
  (built-in agents + `.opencode/agent(s)|command(s)` markdown loaders + models.dev provider
  list with auth.json/env `connected` detection).

Also already done: daemon core (HTTP/SSE/WS transport, SQLite session+message stores, auth,
per-directory routing, SSE bus) in `internal/`; the agent loop itself (`internal/engine/`:
streaming Anthropic+OpenAI, processor, run-state, compaction, core tools); the conformance
harness (`conformance/`, ~16 scenarios, normalizer that scrubs temp-dir/HOME paths).

### Conformance harness — READ THIS, it's a common trap
- `scripts/run-conformance.sh self` = **opencode-vs-opencode** (two fresh opencode runs diffed
  for determinism). It does **NOT** exercise Opcode42 at all. This is the CI gate
  (`.github/workflows/conformance.yml`).
- `scripts/run-conformance.sh dual <opcode42URL>` = **opencode (truth) vs Opcode42**. This is how you
  validate Opcode42. It is NOT in CI; run it manually with a `opcoded` instance up.
- Known divergences (provider/agent/command/find, plus the temp-path notes) are logged in
  `conformance/known-divergences.json`. `/agent`,`/command`,`/provider` are intentionally not
  byte-parity with opencode and are NOT gated by a scenario.
- CI: 4 checks (`build-test-lint`, `codegen-fresh`, `self-diff`, `spec-drift`), all green on
  `main`. (`golangci-lint-action` is pinned to `@v7` for golangci-lint v2 — don't downgrade.)

## The actual remaining Phase-B (plan 02) gaps

The headline Phase-B goal (working agent loop + TUI repointed) is met, but plan 02 has concrete
holes. The DoD "conformance self passes + a prompt round-trips" is already true and is **too weak**
— it does not catch any of these:

1. **Engine ignores the agent registry (highest value).** `engine.go:90` hardcodes
   `orDefault(in.Agent, "build")` and never resolves the named agent. The `internal/resource`
   agent registry (built in #35, `resource.LoadAgents`) feeds the `/agent` *endpoint only*. The
   engine applies neither the agent's model, system prompt (`Agent.Prompt`), nor permission
   rulesets (`Agent.Permission`). The TUI already sends `agent` in the prompt body
   (`promptBody.Agent`, `internal/server/prompt_handlers.go:60`).
2. **HTTP prompt path is allow-all → permission overlay never fires.**
   `prompt_handlers.go:23` `allowAllRulesets` and `:79` `Rulesets: allowAllRulesets` (in
   `buildEngine`). Because every tool is pre-allowed, `permission.Manager.Ask` returns
   immediately and no `permission.asked` SSE is published — so the U10 permission overlay can't
   be exercised end-to-end. Replacing this with the resolved agent's rulesets is what unblocks it.
3. **Subagent `task` tool unwired.** `internal/engine/tool/agentic.go` `Task{Runner SubagentRunner}`
   exists but is NOT registered in `builtinRegistry` (`cmd/opcoded/main.go:220`) and has a nil
   runner → "task: not available". Spawning nested agent runs is core plan-02 M6.
4. **`websearch`/`skill` tools stubbed** — these lean Phase C (web-search provider is plan 03;
   skill loader is plan 04). Optional for "finish Phase B"; note and defer.
5. **M11: end-to-end agent-loop SSE conformance pass** — a dual-run/scenario gate for the
   prompt→stream round-trip. Not yet done.

## Proposed work (one feature per PR, full review gate each)

### PR-4 — Engine consumes the agent registry (closes gaps #1 + #2)
- Resolve the named agent: in `buildEngine` (`prompt_handlers.go`), call
  `resource.LoadAgents(directory, config.Load(directory))`, find the agent named by the prompt
  body (default `"build"`), and feed the engine:
  - the agent's permission ruleset (`Agent.Permission`) instead of `allowAllRulesets` (merge
    with any config rulesets); a missing/empty ruleset should fall back to a sensible default
    (opencode's build agent is effectively allow-all, so keep behavior identical for `build`).
  - the agent's system prompt (`Agent.Prompt`) — thread into `engine.PromptInput.System` /
    `buildSystem` override (`loop.go:126`).
  - the agent's model (`Agent.Model`) when the request doesn't override it.
- Validate against opencode: a `build` prompt still runs tools without prompting; a tool not
  allowed by a restrictive agent publishes `permission.asked` and blocks until
  `POST /permission/:id/reply`. Cite `permission/index.ts` + `agent/agent.ts`.
- **This makes the U10 permission overlay live against Opcode42** — the deferred item in
  `tasks/verify.md`.

### PR-5 — Wire the subagent `task` tool (gap #3)
- Implement a `SubagentRunner` that, given `TaskRequest{Description,Prompt,Agent,ParentSessionID}`,
  creates a child session, runs `engine.Prompt` under the requested agent, and returns the final
  assistant text. Register `tool.Task{Runner: …}` in `builtinRegistry` (`cmd/opcoded/main.go`).
- Mind the run lock (`runstate`): a subagent runs on a *child* session id, not the parent's, so
  it must not deadlock against the parent's lock.
- Validate against opencode's `task` tool semantics (`packages/opencode/src/tool/task.ts`).

### PR-6 (or fold into PR-4/5) — M11 agent-loop conformance
- Add a dual-run-capable check for prompt→stream. Because real LLM calls cost the user tokens,
  use the **mock provider** (`internal/engine/enginetest/mock_provider.go`) or a scripted
  fixture for the deterministic gate; only do a single real-key smoke if explicitly asked.

## Non-negotiable workflow (CLAUDE.md + project memory)
- Branch per feature off `main`; build; spin a **review subagent**; run the full CI-mimic gate
  (`go build ./...`, `go vet ./...`, `gofmt -l`, `golangci-lint run`, `go test ./... -race`,
  `make gen` + `git diff --exit-code internal/api/gen/`, `scripts/run-conformance.sh self`) until
  clean; squash-merge via `gh pr merge`; sync `main`.
- **Never** `git add -A` — a daemon-generated `AGENTS.md`/`.claude/worktrees/` can slip in; use
  targeted `git add <paths>`. No `Co-Authored-By`.
- **Don't auto-fire real LLM prompts** (costs the user tokens) without need — use the mock
  provider/fixtures. The live opencode daemon is on `127.0.0.1:4096` for parity smoke.
- Append human-verify items to `tasks/verify.md`.
- **Do not touch `android/`.**

## Definition of done (stronger than the old one)
- A prompt under the `build` agent runs tools un-prompted (parity with opencode); a prompt under
  a restrictive agent publishes `permission.asked`, blocks, and is unblocked by
  `POST /permission/:id/reply` — verified via a test (mock provider) and a `dual` run.
- The `task` tool spawns a subagent and returns its result (test with mock provider).
- `scripts/run-conformance.sh self` stays green; new endpoints/behaviors that can be compared get
  a `dual` check or a logged divergence.
- Full CI-mimic gate green; PRs merged; `main` synced. `tasks/verify.md` updated (the permission
  overlay item flips from "deferred" to verifiable).
