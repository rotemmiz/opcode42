# Opcode42 — Orchestrator Runbook

How to execute the plan suite through subagents without blowing up the orchestrator's
context window. **You (the session reading this) are the orchestrator.** Stay thin: you
hold *state*, not *work*. Workers hold work and are discarded.

Invoke with: **"run the orchestrator"** (process the whole READY set) or
**"run the orchestrator for <id>"** (single task). The ledger is `tasks/progress.md`.

---

## Roles
- **Orchestrator (you):** never read plan *bodies* or source files. Read `tasks/progress.md`,
  compute the READY set, dispatch workers, collect their ≤15-line summaries, run the gate,
  write status back. Your window grows ~15 lines per task — disposable & resumable from the ledger.
- **Implementer worker (subagent, fresh each time):** reads only its assigned plan *slice*,
  writes code, runs local checks, returns a compact report. Its big context dies with it.
- **Reviewer worker (separate subagent):** reviews the implementer's diff against the same
  plan slice. Independent agent → honest review. This is the "agents verify each other" gate.

---

## The loop

```
1. READ tasks/progress.md. Compute READY = { t : status∈{todo,partial} AND all t.deps==done }.
2. Group READY by `track`. Pick at most one task per track (distinct tracks ⇒ parallel-safe).
   Cap fan-out at N (start N=2–3; raise once proven).
3. CHECK ambiguities section: if a chosen task is listed as blocked by an unresolved
   ambiguity, do NOT dispatch it — surface the question to the human (AskUserQuestion) and
   pick a different READY task instead.
4. For each chosen task, in ONE message, spawn implementer subagents in parallel:
     Agent(subagent_type="general-purpose", isolation="worktree",
           run_in_background=true,   # so they run concurrently
           prompt=<IMPLEMENTER TEMPLATE>)
   Log each dispatch to the ledger Run log.
5. As each implementer returns: spawn a Reviewer subagent on its worktree/diff.
   - Review clean (no blocking/should-fix) → go to gate (step 6).
   - Findings → spawn implementer again (same worktree) to fix; re-review. Repeat until clean.
6. GATE (run inside the worktree, see §Gate). Green → merge the worktree branch:
     - commit (no Co-Authored-By), push, open PR per repo CLAUDE.md git workflow,
       OR fast-forward into the integration branch if batching. (Ask the human which on
       first run; default to PR-per-task.)
   - Red → back to step 5 with the failures as the fix brief.
7. UPDATE tasks/progress.md: set the task `status: done`, recompute & overwrite the
   "Ready set" block, append to Run log. Newly-unblocked tasks become READY.
8. If any READY remain (and human hasn't said stop) → go to 1. Else report and stop.
```

**Context hygiene (the whole point):**
- Dispatch tasks, don't do them. If you find yourself Reading a `.go` file or a plan body,
  stop — that work belongs in a subagent.
- Demand compact summaries. The only thing that should re-enter your window is the report.
- State lives in `progress.md`. If your context is compacted mid-run, re-read it and continue.

---

## Parallelization rules (what makes this safe)
- **Distinct track ⇒ parallel OK.** Same track ⇒ sequential, even if both READY (avoids
  two workers editing the same package → merge hell).
- Each parallel worker gets its **own worktree** (`isolation: "worktree"`). They never share
  a working tree.
- Cross-track file overlap is rare here because tracks map to disjoint package trees
  (`internal/engine`, `internal/mcp`, `internal/lsp`, `conformance/`, plugin sidecar, android).
  If a task's `notes` warns of shared files, treat it as same-track for scheduling.
- Merge serially even if work was parallel: bring worktrees back one at a time, each through
  its own gate, so a conformance regression is attributable.

---

## IMPLEMENTER TEMPLATE (fill the <…> and pass as the worker prompt)

```
You are implementing ONE milestone of the Opcode42 project. Work only within your scope.

SCOPE: <task id> — <task title>
PLAN:  Read ONLY the section <plan ref> of /Users/rotemmiz/git/opcode42/<plan file>.
       Do NOT read other plan files. Read the referenced opencode source for wire-compat
       claims (cite file:line) — opencode is at /Users/rotemmiz/git/opencode.

RULES:
- Follow /Users/rotemmiz/git/opcode42/CLAUDE.md (wire-compat non-negotiables, Go-only, single binary).
- Match surrounding code style. No Co-Authored-By in any commit.
- If you hit one of the unresolved ambiguities in tasks/progress.md, STOP and report it as
  BLOCKED rather than guessing a wire contract.
- Before reporting done, run the gate locally: go build ./... && go vet ./... && gofmt -l .
  && golangci-lint run && go test ./... && make gen && git diff --exit-code internal/api/gen/
  && scripts/run-conformance.sh self  (report any you could not run and why).

RETURN (≤15 lines, this is all the orchestrator sees):
- STATUS: done | blocked | partial
- FILES: list of paths touched
- GATE: pass/fail per check (build/vet/fmt/lint/test/gen-diff/conformance)
- DEVIATIONS: anything you did differently from the plan + why
- BLOCKED-ON: (if blocked) the exact question/ambiguity
- FOLLOWUPS: anything out of scope you noticed (do NOT do it)
```

## REVIEWER TEMPLATE

```
You are reviewing ONE milestone's diff. You did NOT write it. Be a skeptical reviewer.

TASK:  <task id> — <task title>
PLAN:  Read ONLY <plan ref> of /Users/rotemmiz/git/opcode42/<plan file>.
DIFF:  Review the uncommitted changes in this worktree (git diff / git status).

CHECK:
- Correctness vs the plan slice and the wire-compat non-negotiables in CLAUDE.md
  (SSE shape, PTY framing, auth/routing, spec drift). Cite opencode file:line where relevant.
- Bugs, missing error handling, untested paths.
- Reuse/simplification only if it also affects correctness or clarity (don't bikeshed).

RETURN (≤12 lines):
- VERDICT: clean | findings
- BLOCKING: numbered must-fix items (empty if none)
- SHOULD-FIX: numbered items
- NOTES: optional
Do not edit code. Report only.
```

---

## Gate (the definition of "done", from masterplan §143)
Every task must pass, inside its worktree, before merge:
1. `go build ./...`  2. `go vet ./...`  3. `gofmt -l .` (empty)  4. `golangci-lint run`
5. `go test ./...`  6. `make gen` + `git diff --exit-code internal/api/gen/`
7. `scripts/run-conformance.sh self`
8. **+ dual-run diff vs real opencode for any new/changed endpoint or SSE shape.**

"Conformance green" = this gate passing, not a vibe.

---

## When to involve the human
- An ambiguity in `progress.md` blocks a task → `AskUserQuestion`, don't guess.
- A worker returns `blocked` → relay it, get a decision, re-dispatch.
- Human-verify `[ ]` items in `tasks/verify.md` → never self-check judgment calls; list them
  for the human (per the verify-tasks convention) and continue with other work.
- First run only: confirm merge strategy (PR-per-task vs batch into an integration branch)
  and fan-out N.
