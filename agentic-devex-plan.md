# Agentic DevEx Learning Lab — End-to-End Plan

Build an agent-owns-PR loop that takes approved GitHub issues → produces merged PRs with
live preview URLs and gate recordings. Runs **off-laptop**, **event-driven** (no polling),
**zero infrastructure to maintain** (GitHub Actions is the orchestrator host). Goal: learn
AI devex by *operating* an agentic pipeline, not by reading about it.

This is a **separate project** from opcode42 the daemon. opcode42 is the *dogfood target* —
the repo the agent works on. Eventually opcode42 replaces `opencode serve` in the sandbox
(Phase 2+ dogfood), but that comes later.

> **Review status (rev 4):** 13 blocking issues fixed across 3 review rounds (7 in rev 2,
> 4 in rev 3, 2 in rev 4). All `worker.py` HTTP API calls verified against
> `packages/sdk/openapi.json`, `packages/server/src/auth.ts`, and
> `packages/opencode/src/session/status.ts`. See §11 for the changelog.

---

## 0. Architecture

```
                         YOU (phone/laptop, ad hoc)
                          │ create issue
                          ▼
                    ┌─────────────┐
                    │   GitHub    │  repo: opcode42_3 (public)
                    │  issues/PRs │  branch protection on main
                    │             │  restrict issue creation to admins
                    └─────┬───────┘
                          │ event: issues.opened
                          ▼
               ┌──────────────────────┐
               │  GitHub Actions       │  .github/workflows/planner.yml
               │  runner (ubuntu)      │  runs planner.py (~80 lines)
               │  ephemeral, free      │  GITHUB_TOKEN (issues:write) posts
               │                       │  the plan comment (no PAT needed)
               │                       │  secrets: E2B_API_KEY,
               │                       │           OLLAMA_API_KEY
               └──┬────────────────────┘
                  │ boots E2B sandbox (planner, read-only)
                  │ reads issue + plans/ from the runner checkout
                  │ calls Ollama Cloud → structured plan
                  │ posts plan as comment via GITHUB_TOKEN
                  ▼
               ┌─────────────┐
               │  YOU        │  read plan, comment /approve
               └─────┬───────┘
                     │ event: issue_comment.created
                     │   if: body == '/approve'
                     │        && issue.pull_request == null
                     │        && comment.author_association == 'OWNER'
                     │   concurrency: worker-${issue.number}, cancel-in-progress
                     ▼
               ┌──────────────────────┐
               │  GitHub Actions       │  .github/workflows/worker.yml
               │  runner (ubuntu)      │  runs worker.py (~150 lines)
               │  ephemeral, free      │  secrets: E2B_API_KEY,
               │  timeout-minutes: 30  │           OLLAMA_API_KEY,
               │                       │           BRANCH_PUSHER_TOKEN
               └──┬────────────────────┘
                  │ boots E2B sandbox (worker, Firecracker microVM, timeout=1500s)
                  │   inside the sandbox:
                  │     git clone opcode42_3 on agent/<issue-n> branch
                  │     opencode serve --port 4096 --hostname 0.0.0.0  (background, blocking)
                  │     worker.py drives the agent via HTTP:
                  │       POST /session  {model: {providerID: "ollama", id: "glm-5.2"}}
                  │       POST /session/{id}/message  {parts: [{type:"text", text: <plan>}]}
                  │       poll GET /session/status (wait busy → wait absence=idle)
                  │     KILL the agent's opencode serve (fuser -k 4096/tcp)
                  │     GATE: scripts/agent-gate.sh (asciinema-recorded)
                  │       make gen && go build && golangci-lint && go test
                  │       && scripts/run-conformance.sh self  (PORT=4096, now free)
                  │     RESTART opencode serve on 4096 for the preview URL
                  │     push branch + gh pr create (BRANCH_PUSHER_TOKEN)
                  │     keep sandbox alive until PR merged/closed OR job timeout
                  ▼
               ┌─────────────┐
               │   GitHub    │  PR with:
               │   PR        │   - diff + CI status
               │             │   - asciinema link (gate recording)
               │             │   - live preview URL (https://<host(4096)>)
               └──────┬──────┘
                      │ you review + merge (GitHub UI — no token, just your click)
                      ▼
               PR-closed event (Phase 2) kills the lingering sandbox;
               in Phase 1 the sandbox dies at job timeout (1500s) if not merged in time
```

### Data flow per issue

1. You create issue (only you — repo setting restricts issue creation to admins)
2. **`issues.opened`** event fires → `planner.yml` workflow runs → `planner.py` boots a
   read-only E2B sandbox, reads the issue (from env) + `plans/` (from the runner checkout),
   calls Ollama Cloud, posts a structured plan as a comment via the workflow's
   `GITHUB_TOKEN` (no PAT needed for posting issue comments)
3. You read the plan comment, comment `/approve` (human gate between planning and execution)
4. **`issue_comment.created`** event fires, filtered to `/approve` on issues (not PRs) **and
   authored by the repo OWNER** (so the public can't trigger the worker) → `worker.yml`
   runs; `concurrency:` ensures only one worker per issue
5. `worker.py` boots an E2B sandbox from `opcode42-builder` (timeout 1500s < Actions 30min,
   so the sandbox self-destructs before the job can be SIGKILLed)
6. Sandbox clones the repo on `agent/<issue-n>` branch
7. `worker.py` starts `opencode serve --port 4096 --hostname 0.0.0.0` in the **background**
   (`serve` is a blocking HTTP server — it does not run an agent on its own)
8. `worker.py` drives the agent via the HTTP API:
   - `POST /session` with `{model: {providerID: "ollama", id: "glm-5.2"}, title: <issue>}`
   - `POST /session/{id}/message` with `{parts: [{type: "text", text: <plan>}]}` (the plan
     is fetched from the issue comments via `GITHUB_TOKEN` and passed directly into the
     request body — no sandbox filesystem round-trip)
   - Poll `GET /session/status` — wait for the session to appear as `busy`, then wait for
     it to disappear (idle sessions are *deleted* from the status map, per
     `session/status.ts:42-44`, so absence = done)
9. `worker.py` kills the agent's `opencode serve` (`fuser -k 4096/tcp`) so the port is free
10. **Gate** (separate process): `scripts/agent-gate.sh` runs `go mod verify && gitleaks &&
    gofmt -l (empty) && make gen && git diff --exit-code internal/api/gen/ && go build &&
    go vet && golangci-lint run && go test && scripts/run-conformance.sh self` (conformance
    uses `PORT=4096`, now free because the agent server was killed), all recorded with
    asciinema. Gate also asserts the diff is non-empty (`git diff --name-only main...HEAD`).
11. Gate passes → `worker.py` restarts `opencode serve` on 4096 (for the preview URL), pushes
    the branch, opens a PR with the asciinema link + preview URL (`https://<sandbox
    get_host(4096)>`)
12. Sandbox stays alive (preview URL works) until the E2B 1500s timeout, OR until you merge
    and a PR-closed workflow kills it (Phase 2). For Phase 1, the preview window is bounded
    by the sandbox timeout — long enough to review + poke during a 30-min session.
13. You review on phone/laptop, merge via the GitHub UI (no `merger` token — merging is a
    UI click under your logged-in session).

### Why this beats a persistent orchestrator (EC2/ECS)

| Concern | GitHub Actions | EC2/ECS orchestrator |
|---|---|---|
| Trigger | **event-driven, instant** | poll 60s or webhook infra |
| Infra to maintain | **zero** (GitHub's runners) | VM, systemd, SSH, patches |
| Cost (public repo) | **$0 forever, unlimited** | $8–150/mo |
| State | **GitHub itself** (issue, comments, PR) | database / checkpoint file |
| Audit trail | **Actions run log** (90 days, free, searchable) | self-managed log store |
| Survives laptop off | **yes** (GitHub's infra) | yes (if your VM stays up) |
| Concurrency | **unlimited** (GitHub scales runners) | bounded by your VM size |
| Secrets | **Actions secrets** (encrypted, masked in logs) | `.env` / Secrets Manager |

The tradeoff: **no long-running process**. Each issue is a fresh, stateless job. State lives
in GitHub — issue body, plan comment, `/approve`, PR, PR comments — that's your durable
store, reconstructable from GitHub alone. This is a feature, not a limitation: it's a better
audit trail than an EC2 log file, and there's nothing to reboot.

### Non-negotiable separation

- **Agent** (opencode `serve` + HTTP API) proposes; **gate** (`agent-gate.sh`) verifies.
  Never the same process — the agent server is killed before the gate runs.
- **Planner** explores (read-only sandbox, `GITHUB_TOKEN` for comments); **worker** mutates
  (write sandbox, `BRANCH_PUSHER_TOKEN` for push/PR). Different workflows, different tokens.
- **`BRANCH_PUSHER_TOKEN`** pushes + opens PRs; **you** merge via the GitHub UI. No
  `merger` token exists — merging is a UI action under your own login, not an API call from
  a machine.

---

## 1. Security model

| Threat | Control |
|---|---|
| Prompt injection via issue body (untrusted text) | Issue text is *data*, only the planner reads it; worker reads the *plan you approved* (a comment), not raw issue. Planner posts via `GITHUB_TOKEN` (issues:write, no push); worker holds `BRANCH_PUSHER_TOKEN` (push + PR-open, no merge, no admin). Injected instructions can't escalate — the token can't do what the injection asks. |
| Anyone comments `/approve` on a public repo | **`if: github.event.comment.author_association == 'OWNER'`** — only the repo owner can trigger the worker. Commenters who aren't the owner are ignored. (Use `COLLABORATOR` instead if you add a collaborator later.) |
| Agent exfiltrates secrets | Sandbox is Firecracker microVM, no path to your laptop or the Actions runner. Keys injected at boot, scoped to session. `BRANCH_PUSHER_TOKEN` never enters the planner; the worker injects it to the sandbox only for the push step. Actions secrets are masked in logs automatically. |
| Agent phones home / DNS exfil (Phase 1, E2B hosted) | **E2B hosted sandboxes have open egress by default.** This is NOT a Phase-1 control. The sandbox dies with the job (1500s cap), so exfil is time-bounded. Network-level egress enforcement is a **Phase 2 (BYOC in your VPC)** control via Security Groups. Don't pretend otherwise. |
| Agent phones home (Phase 2, BYOC) | VPC Security Group allowlist: `proxy.golang.org`, `registry.npmjs.org`, `github.com`, `ollama.ai`, `asciinema.org`. Nothing else. |
| Malicious dependency injection | Gate enforces `go mod verify` (lockfile checksums). gitleaks scans the staged diff in the gate. |
| Anyone opens issues / pushes to main | Branch protection: no direct pushes, CI required, your review required. Repo setting: restrict issue creation to admins (only you). OSS PRs from outsiders are untrusted input — the planner reads them as data if routed through it, never as instructions. |
| Agent loops forever / cost spiral | Actions `timeout-minutes: 30`, E2B `timeout=1500` (self-destructs before the job can SIGKILL it, so `finally:` leak is impossible), LLM spend ceiling enforced in `worker.py`, max 3 retries (re-run workflow). `concurrency:` prevents duplicate runs on the same issue. |
| Sandbox lingers, leaks | E2B `timeout=1500` is a hard cap set *lower* than the Actions timeout — the sandbox self-destructs even if `finally:` doesn't run. No orphan reaper needed. |
| No audit trail | Every action logged in the Actions run log (90 days, searchable, free). Each run is tied to the issue/PR that triggered it. Phase 2: mirror logs to S3 Object Lock. |
| Secret in repo | gitleaks in the gate catches it. Secrets in GitHub Actions secrets (encrypted at rest, masked in logs), never committed. |
| Workflow `pull_request_target` footgun | **Never used.** All workflows trigger on `issues` and `issue_comment` (your actions), not on PRs from outsiders. Phase 3 PR review uses `pull_request` (no secrets) + a follow-up `workflow_dispatch`. |

### Token split (fine-grained PATs or GitHub App)

| Token | Can do | Stored as | Enters sandbox? |
|---|---|---|---|
| `GITHUB_TOKEN` (auto) | post issue comments (planner), read issue comments (worker) | auto-injected per-job | no (runner uses it) |
| `BRANCH_PUSHER_TOKEN` | push to `agent/*`, open PRs — fine-grained PAT with **Contents: write** + **Pull requests: write**, repo-scoped to `opcode42_3` (branch protection on `main` enforces the `agent/*`-only push rule, not the PAT itself) | Actions secret | yes (injected to sandbox for the push/PR step) |
| `E2B_API_KEY` | boot/kill E2B sandboxes | Actions secret | used by runner (not sandbox) |
| `OLLAMA_API_KEY` | call Ollama Cloud | Actions secret | yes (injected to sandbox env) |
| `GIST_TOKEN` | create Gists (for gate recordings) — **classic PAT** with `gist` scope (fine-grained PATs do not support Gists) | Actions secret | used by runner (not sandbox) |
| (merge) | merge to main | **nowhere — you merge via the GitHub UI.** | never |

---

## 2. Phases

### Phase 0 — Feel the loop by hand (1–2 weeks)

**Goal:** internalize the agent-owns-PR workflow before automating. No infra built.

1. Pick 3 small issues from the opcode42 board.
2. For each, run the full `CLAUDE.md` loop manually via opencode on your laptop:
   `git checkout main && git pull` → branch → build → self-gate
   (build/vet/lint/test/conformance) → push → **review the PR yourself** → fix → CI green →
   merge. (No review subagent yet — that's Phase 3. You review, to learn what good agent
   output looks like.)
3. Keep a notes file: what context the agent needed, where it failed, what you nudged. This
   becomes the automation spec for Phase 1.

**Milestone:** 3 merged PRs + a `phase0-notes.md` of pain points.

### Phase 1 — GitHub Actions + E2B, event-driven, live-steering (2–4 weeks)

**Goal:** one approved issue → merged PR with preview URL, zero laptop in the loop, zero
infrastructure to maintain.

**Build order:**

1. **Accounts & keys** (half day)
   - E2B sign-up, `E2B_API_KEY`
   - Ollama Cloud key
   - GitHub fine-grained PAT: `BRANCH_PUSHER_TOKEN` — **Contents: write** + **Pull requests:
     write**, repository-scoped to `opcode42_3`. (Branch protection on `main` + allow
     `agent/*` enforces the branch rule; the PAT itself is scoped by *permission*, not by
     branch.)
   - Add `E2B_API_KEY`, `OLLAMA_API_KEY`, `BRANCH_PUSHER_TOKEN` as Actions secrets in the
     repo. (No `ISSUE_READER_TOKEN` — the planner uses the auto `GITHUB_TOKEN` with
     `permissions: issues: write` to post its plan comment.)

2. **E2B template: `opcode42-builder`** (half day, one-time bake) — see §4
   - V2 fluent `Template()` builder (not the deprecated V1 Dockerfile + `e2b.toml`)
   - Base: Ubuntu 24.04 + Go 1.23+ + Node/Bun (for opencode) + `golangci-lint` + `make` +
     `git` + `gh` CLI + asciinema + gitleaks
   - Bake once → boots in <200ms

3. **Gate harness** (half day — mostly already exists) — see §5
   - `scripts/agent-gate.sh`: `go mod verify && gitleaks && gofmt -l (empty) &&
     git diff --name-only main...HEAD (non-empty) && make gen && git diff --exit-code
     internal/api/gen/ && go build && go vet && golangci-lint run && go test &&
     scripts/run-conformance.sh self`
   - Wrapped in `asciinema rec` → uploaded as a **GitHub Gist** via a separate classic
     `GIST_TOKEN` (fine-grained PATs can't create Gists) —
     not asciinema.org (anonymous uploads are deprecated in v3 and require interactive auth)
   - Conformance uses `PORT=4096` — the gate script kills the agent's `opencode serve`
     before running conformance, so the port is free

4. **`planner.yml` workflow + `planner.py`** (1–2 days) — see §3
   - Triggers on `issues.opened`
   - `planner.py` reads `plans/00-masterplan.md` + the issue-referenced plan section from
     the **runner's checkout** (already there via `actions/checkout` — no sandbox clone
     needed for reading plans). Boots a read-only E2B sandbox only if it needs to run code
     for exploration; otherwise calls Ollama directly from the runner.
   - Produces structured plan (files, approach, risks, conformance impact)
   - Posts as a comment via the workflow's `GITHUB_TOKEN` (`permissions: issues: write`)
   - Honors `plans/00-masterplan.md` — maps issue to plan section, does NOT re-architect
   - **Stop here. Test the plan-posting loop before building the worker.**

5. **`worker.yml` workflow + `worker.py`** (2–4 days) — see §3 for the full shape
   - Triggers on `issue_comment.created` where `body == '/approve'`,
     `issue.pull_request == null`, **and `comment.author_association == 'OWNER'`**
   - `concurrency: { group: worker-${{ github.event.issue.number }}, cancel-in-progress: true }`
   - Boots E2B sandbox from `opcode42-builder` (`timeout=1500` < Actions 30min)
   - Fetches the approved plan from the issue comments via `GITHUB_TOKEN` (GET
     `/issues/{n}/comments`, find the `github-actions[bot]` comment, pass `plan_comment`
     directly into the message body — no sandbox filesystem round-trip)
   - Starts `opencode serve --port 4096 --hostname 0.0.0.0` in the **background**
   - Drives the agent via HTTP: `POST /session` → `POST /session/{id}/message` (with the
     plan as a text part) → poll `GET /session/status` (wait for `busy`, then wait for
     absence = idle)
   - Kills the agent's `opencode serve` (`fuser -k 4096/tcp`)
   - Runs `scripts/agent-gate.sh` (asciinema-recorded, conformance on `PORT=4096` now free)
   - Gate passes → restarts `opencode serve` on 4096 (for preview), pushes branch, opens PR
     with asciinema Gist link + preview URL (`https://<sandbox.get_host(4096)>`)
   - `finally:` kills the sandbox (belt-and-suspenders; the E2B timeout is the real guarantee)

6. **First "hello-issue"** (the milestone)
   - Trivial issue: "add `--version` flag to the daemon"
   - While the worker runs, connect **live** to the sandbox via
     `https://<sandbox.get_host(4096)>` (opencode TUI → that URL) — the agent server is up
     during the agent step, so you can watch it work and steer it
   - After the PR opens, the preview URL points at the restarted server — poke the daemon API
   - Merge the PR from your phone

7. **Tenth issue** — let it run headless, review PR only, merge from phone. You've learned
   the loop.

**Milestone:** 10 merged PRs, zero infra maintained, preview URLs work, cost $0/mo (public repo).

### Phase 2 — only if you hit a Phase-1 limit (later)

Phase 1 works forever for a solo OSS project. Promote only when you hit a concrete wall:
network-level egress enforcement, >90-day audit retention, or parallelism beyond GitHub
Actions free tier (unlikely for one repo). Keep GitHub Actions as the trigger; only move the
sandbox host to E2B BYOC in your VPC. No ECS, no API Gateway — the simplicity of Phase 1 is
preserved.

### Phase 3 — Review-subagent automation (later)

A third workflow (`pr_review.yml`) triggered on `pull_request.opened` (uses the
`pull_request` event — **no secrets** in that job) → posts findings as PR comments → if
blocking findings, comments `/retry` on the issue to re-trigger the worker. Plus a
known-divergence registry (per `plans/12-test-compatibility.md`). Not in scope for the
learning goal.

---

## 3. Workflows + scripts

### State (all in GitHub — no database)

| State | Where it lives |
|---|---|
| Original request | issue body |
| Plan | comment on issue (planner posts it via `GITHUB_TOKEN`) |
| Human approval | `/approve` comment (worker trigger; owner-only) |
| Branch + code | `agent/<issue-n>` branch |
| Gate recording | asciinema Gist link in PR description |
| Live preview | E2B URL (`https://<sandbox.get_host(4096)>`) in PR description |
| Review findings | PR comments (review subagent, Phase 3) |
| Done | PR merged/closed |

Reconstruct any issue's full history from GitHub alone. That's the audit trail.

### `.github/workflows/planner.yml`

```yaml
name: Planner
on:
  issues:
    types: [opened]
jobs:
  plan:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    permissions:
      issues: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with: { python-version: "3.12" }
      - run: pip install e2b requests
      - env:
          E2B_API_KEY: ${{ secrets.E2B_API_KEY }}
          OLLAMA_API_KEY: ${{ secrets.OLLAMA_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}   # auto, issues:write via permissions:
          ISSUE_NUMBER: ${{ github.event.issue.number }}
          ISSUE_BODY: ${{ github.event.issue.body }}
          ISSUE_TITLE: ${{ github.event.issue.title }}
          REPO: ${{ github.repository }}
        run: python scripts/planner.py
```

### `.github/workflows/worker.yml`

```yaml
name: Worker
on:
  issue_comment:
    types: [created]
jobs:
  work:
    if: >-
      github.event.comment.body == '/approve' &&
      github.event.issue.pull_request == null &&
      github.event.comment.author_association == 'OWNER'
    concurrency:
      group: worker-${{ github.event.issue.number }}
      cancel-in-progress: true
    runs-on: ubuntu-latest
    timeout-minutes: 30
    permissions:
      issues: write
      pull-requests: write
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with: { python-version: "3.12" }
      - run: pip install e2b requests
      - env:
          E2B_API_KEY: ${{ secrets.E2B_API_KEY }}
          OLLAMA_API_KEY: ${{ secrets.OLLAMA_API_KEY }}
          BRANCH_PUSHER_TOKEN: ${{ secrets.BRANCH_PUSHER_TOKEN }}
          GIST_TOKEN: ${{ secrets.GIST_TOKEN }}           # classic PAT with gist scope
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}       # auto, used to read issue comments
          ISSUE_NUMBER: ${{ github.event.issue.number }}
          REPO: ${{ github.repository }}
        run: python scripts/worker.py
```

### `scripts/planner.py` (shape, ~80 lines)

```python
import os, requests

# 1. Read issue (from env) + plans/ (from the runner checkout — already on disk)
issue_title = os.environ["ISSUE_TITLE"]
issue_body  = os.environ["ISSUE_BODY"]
# Read plans/00-masterplan.md + the plan section the issue references, from the cwd
with open("plans/00-masterplan.md") as f: masterplan = f.read()
# ... select relevant plan section based on issue keywords ...

# 2. Call Ollama Cloud with issue + plan context → structured plan
resp = requests.post(
    "https://api.ollama.ai/chat/completions",   # adjust to your Ollama Cloud endpoint
    headers={"Authorization": f"Bearer {os.environ['OLLAMA_API_KEY']}"},
    json={"model": "glm-5.2", "messages": [
        {"role": "system", "content": "You are a planning agent. Given an issue and a plan section, produce a structured implementation plan (files, approach, risks, conformance impact). Do NOT re-architect."},
        {"role": "user", "content": f"# Issue\n{issue_title}\n\n{issue_body}\n\n# Relevant plan\n{masterplan}"},
    ]},
)
plan_md = resp.json()["choices"][0]["message"]["content"]

# 3. Post plan as a comment on the issue via GITHUB_TOKEN
requests.post(
    f"https://api.github.com/repos/{os.environ['REPO']}/issues/{os.environ['ISSUE_NUMBER']}/comments",
    headers={"Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}"},
    json={"body": f"## Proposed plan\n\n{plan_md}\n\n---\nReply `/approve` to proceed."},
)
```

### `scripts/worker.py` (shape, ~150 lines)

```python
import os, time, requests
from e2b import Sandbox

sandbox = Sandbox.create(template="opcode42-builder", timeout=1500)  # < Actions 30min
try:
    branch = f"agent/{os.environ['ISSUE_NUMBER']}"

    # 1. Clone repo on agent/<issue-n> branch
    sandbox.commands.run(
        f"git clone https://x-access-token:{os.environ['BRANCH_PUSHER_TOKEN']}@github.com/{os.environ['REPO']}.git repo"
        f" && cd repo && git checkout -b {branch}"
    )

    # 2. Fetch the approved plan from the issue comments (find the planner's comment)
    comments = requests.get(
        f"https://api.github.com/repos/{os.environ['REPO']}/issues/{os.environ['ISSUE_NUMBER']}/comments",
        headers={"Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}"},
    ).json()
    plan_comment = next((c["body"] for c in comments if c["user"]["login"] == "github-actions[bot]"), None)
    if plan_comment is None:
        raise SystemExit("no planner comment found on the issue — run the planner first")

    # 3. Start opencode serve in the BACKGROUND (it blocks forever — that's its job).
    #    No OPENCODE_SERVER_PASSWORD is set, so auth is DISABLED (auth.ts:40-42: required()
    #    returns false when password is unset). The sandbox is isolated — no auth needed.
    #    Do NOT send an Authorization header (it would be ignored anyway).
    sandbox.commands.run(
        "cd repo && opencode serve --port 4096 --hostname 0.0.0.0",
        background=True,                       # returns immediately, server keeps running
    )
    # wait for health (no auth header — server is unauthenticated)
    for _ in range(60):
        try:
            if sandbox.commands.run("curl -fsS http://127.0.0.1:4096/global/health").exit_code == 0: break
        except Exception: pass
        time.sleep(0.5)

    # 4. Drive the agent via the HTTP API (NOT via CLI flags — serve has none of those).
    #    No auth header — server is unauthenticated inside the sandbox.
    sess = requests.post("http://127.0.0.1:4096/session",
        json={"model": {"providerID": "ollama", "id": "glm-5.2"}, "title": f"issue-{os.environ['ISSUE_NUMBER']}"}).json()
    sid = sess["id"]
    # Pass the plan text directly into the message body (no /tmp/plan.md round-trip —
    # worker.py runs on the runner, not in the sandbox).
    requests.post(f"http://127.0.0.1:4096/session/{sid}/message",
        json={"parts": [{"type": "text", "text": plan_comment}]})

    # Poll GET /session/status until the agent finishes.
    # IMPORTANT: status.ts:42-44 DELETES a session from the map when it goes idle
    # (data.delete(sessionID)), so a finished session is ABSENT from the response,
    # NOT present with type:"idle". The correct sequence is:
    #   1. wait for the session to appear as "busy" (confirms the agent started)
    #   2. wait for the session to disappear (confirms it went idle)
    base = "http://127.0.0.1:4096/session/status"
    # Step 1: wait for busy (race: POST /message returns before status.set(busy))
    for _ in range(150):
        if requests.get(base).json().get(sid, {}).get("type") == "busy": break
        time.sleep(1)
    # Step 2: wait for absence = idle (status.ts:44 deletes on idle)
    for _ in range(750):  # 25 min cap on the agent step
        if sid not in requests.get(base).json(): break
        time.sleep(2)

    # 5. Kill the agent's opencode serve so port 4096 is free for conformance
    sandbox.commands.run("fuser -k 4096/tcp", timeout=10)

    # 6. Run the gate (asciinema-recorded, separate process from the agent)
    sandbox.commands.run(
        "cd repo && asciinema rec /tmp/gate.cast --command 'bash scripts/agent-gate.sh'"
    )

    # 7. Upload the gate recording as a GitHub Gist.
    #    NOTE: fine-grained PATs do NOT support Gist scope (Gists are classic-PAT-only).
    #    BRANCH_PUSHER_TOKEN is fine-grained → use a separate classic PAT GIST_TOKEN,
    #    or fall back to attaching the .cast as a PR artifact via the Contents API.
    cast_bytes = sandbox.files.read("/tmp/gate.cast")   # returns bytes — decode for JSON
    cast = cast_bytes.decode() if isinstance(cast_bytes, bytes) else cast_bytes
    gist_token = os.environ.get("GIST_TOKEN") or os.environ["BRANCH_PUSHER_TOKEN"]
    gist = requests.post("https://api.github.com/gists",
        headers={"Authorization": f"Bearer {gist_token}"},
        json={"public": False, "files": {"gate.cast": {"content": cast}}}).json()
    asciinema_url = gist["html_url"]

    # 8. Restart opencode serve for the preview URL (so the reviewer can poke the daemon).
    #    Then WAIT for health before capturing the URL — get_host(4096) returns a
    #    deterministic hostname the moment the sandbox boots, but nothing listens on 4096
    #    until opencode serve is up (~2-5s). Opening a PR with a dead preview URL breaks M1.
    sandbox.commands.run("cd repo && opencode serve --port 4096 --hostname 0.0.0.0", background=True)
    for _ in range(60):
        try:
            if sandbox.commands.run("curl -fsS http://127.0.0.1:4096/global/health").exit_code == 0: break
        except Exception: pass
        time.sleep(0.5)
    preview_url = f"https://{sandbox.get_host(4096)}"   # get_host, NOT forward_port (which doesn't exist)

    # 9. Push + open PR
    sandbox.commands.run(f"cd repo && git push origin {branch}")
    pr = requests.post(
        f"https://api.github.com/repos/{os.environ['REPO']}/pulls",
        headers={"Authorization": f"Bearer {os.environ['BRANCH_PUSHER_TOKEN']}"},
        json={"head": branch, "base": "main",
              "title": f"[agent] issue #{os.environ['ISSUE_NUMBER']}",
              "body": f"## Gate recording\n{asciinema_url}\n\n## Live preview\n{preview_url}\n\nCloses #{os.environ['ISSUE_NUMBER']}"},
    ).json()

    # 10. Sandbox stays alive until timeout (1500s) — reviewer can poke the preview URL
    #     For longer-lived previews, see Phase 2 (PR-closed webhook kills the sandbox)
finally:
    sandbox.kill()   # belt-and-suspenders; the E2B timeout is the real guarantee
```

### Why no LangGraph in Phase 1

The outer loop (issue → plan → approve → PR) is **two YAML workflows + two Python scripts**.
The workflows *are* the graph — event-driven, stateless, each step a fresh job. LangGraph
would add a stateful graph runtime for no benefit when GitHub is already the state store and
event router. `opencode serve` is the inner agent loop (driven via HTTP). Don't build a graph
around a graph.

---

## 4. E2B template spec — `opcode42-builder` (V2 fluent builder)

E2B's V1 (`e2b.toml` + Dockerfile) is deprecated. Use the V2 `Template()` builder:

```python
# template.py
from e2b.templates import Template

t = (
    Template(name="opcode42-builder")
    .from_base_image("ubuntu:24.04")
    .install_packages([
        "ca-certificates", "curl", "git", "make", "build-essential",
        "asciinema", "gitleaks", "fuser",
    ])
    # Go toolchain
    .run_commands([
        "curl -sSL https://go.dev/dl/go1.23.0.linux-amd64.tar.gz | tar -Cz - -C /usr/local",
    ])
    .set_envs({"PATH": "/usr/local/go/bin:$PATH"})
    # gh CLI (for branch-pusher to open PRs inside the sandbox)
    .run_commands([
        "curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg "
        "-o /usr/share/keyrings/githubcli-archive-keyring.gpg "
        "&& echo 'deb [arch=amd64 signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] "
        "https://cli.github.com/packages stable main' > /etc/apt/sources.list.d/github-cli.list "
        "&& apt-get update && apt-get install -y gh",
    ])
    # golangci-lint
    .run_commands([
        "curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh "
        "| sh -s -- -b /usr/local/bin",
    ])
    # Node + Bun (opencode runtime deps — opencode is a Bun/Node app)
    .run_commands([
        "curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt-get install -y nodejs",
        "curl -fsSL https://bun.sh/install | bash",
    ])
    .set_envs({"PATH": "/root/.bun/bin:$PATH"})
    # opencode (the agent runtime)
    .run_commands(["curl -fsSL https://opencode.ai/install.sh | bash"])
    # Verify opencode actually runs (fails the bake if Bun/opencode is broken)
    .run_commands(["opencode --version"])
)
```

Bake: `python template.py` (or `e2b template build` via the V2 CLI). Verify
`opencode --version` succeeds in the bake — if it doesn't, the template won't work.

**Decide once:** pin opencode to a version (reproducible, re-bake weekly) or install at boot
(current, +2s per run). For a learning lab, **install at boot** is simpler — drop the last
`run_commands` from the template and run the install script in `worker.py` before
`opencode serve`. Tradeoff: +2s per sandbox boot.

---

## 5. Gate harness — `scripts/agent-gate.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

# Supply chain
go mod verify
gitleaks detect --source . --no-banner --redact --exit-code 1

# Non-empty diff (reject no-op PRs — the repo is green on main, so an empty diff passes
# every other check trivially; force the agent to have actually changed something)
test -n "$(git diff --name-only main...HEAD)"

# Format check (must be empty)
test -z "$(gofmt -l .)"

# Generate + diff check (frozen contract — plans/06)
make gen
git diff --exit-code internal/api/gen/

# Build + vet + lint + test
go build ./...
go vet ./...
golangci-lint run
go test ./...

# Conformance (plans/12) — the correctness gate against real opencode.
# Uses PORT=4096. The gate script is run AFTER worker.py kills the agent's opencode serve
# on 4096, so the port is free. If you forget to kill it, this will fail with "port in use".
PORT=4096 scripts/run-conformance.sh self

echo "GATE PASSED"
```

Run inside the sandbox as `asciinema rec /tmp/gate.cast --command 'bash scripts/agent-gate.sh'`,
then upload `/tmp/gate.cast` as a **GitHub Gist** (via `gh gist create` or the Gist API) —
not asciinema.org (anonymous uploads deprecated in v3; requires interactive auth that a
sandbox can't do). Put the Gist raw URL in the PR description.

**This script is the single source of truth for "correct."** The agent's job is to make this
script exit 0. The worker's job is to run this script *after* the agent claims done, and only
open a PR if it passes. Agent and gate are separate processes by construction (the agent
server is killed before the gate runs).

---

## 6. Cost model

### Phase 1 (GitHub Actions + E2B hosted + Ollama Cloud)

| Item | Cost/mo |
|---|---|
| GitHub Actions (public repo) | **$0 unlimited** |
| GitHub Actions (private repo, 2k min/mo free) | $0 → $0.008/min after |
| E2B sandboxes (~10 issues × 5 min) | ~$0–2 |
| Ollama Cloud | your subscription (fixed) |
| GitHub Gists (gate recordings) | $0 |
| **Total** | **~$0–2/mo forever** (no 12-month expiry) |

### Phase 2 (E2B BYOC in your VPC + audit) — only if needed

| Item | Cost/mo |
|---|---|
| EC2 for E2B microVM host (scaled) | ~$30–80 |
| NAT Gateway | ~$32 + data |
| Secrets Manager (~5 secrets) | ~$2 |
| S3 audit log (Object Lock) | ~$1 |
| CloudTrail | free / ~$2 |
| E2B BYOC control plane | free (pay AWS for compute) |
| Ollama Cloud | your subscription |
| GitHub Actions | still $0 (public) |
| **Total** | **~$65–120/mo** |

Phase 2 keeps GitHub Actions as the trigger — only the sandbox host moves to your VPC. No
ECS, no API Gateway, no Lambda. The simplicity of Phase 1 is preserved.

---

## 7. Milestones & exit criteria

| Milestone | Done when |
|---|---|
| M0 — hand loop | 3 PRs merged manually; `phase0-notes.md` written |
| M1 — first auto PR | 1 issue: created → planned → approved → sandbox → gate → PR → preview URL → merged, with laptop only used for `/approve` and merge |
| M2 — live steering | connected to the sandbox via `https://<get_host(4096)>` *during the agent step* (server is up while the agent runs), watched it work, steered it out of a loop at least once |
| M3 — headless | 10th PR merged without live-connect; reviewed PR + preview URL only |
| M4 — off-laptop durability | merged a PR with laptop closed overnight (workflow ran on GitHub's infra, not yours) |
| M5 — Phase 2 promoted | sandboxes moved to BYOC in VPC, network-level egress, >90d audit; 50th PR merged |
| M6 — dogfood | `opcode42 serve` replaces `opencode serve` in the sandbox; the daemon runs its own dev loop |

---

## 8. Tech choices (frozen for Phase 1)

| Concern | Choice | Why |
|---|---|---|
| Orchestrator | **GitHub Actions** (two workflows) | event-driven, $0 public, zero infra, state in GitHub, free audit log |
| Sandbox | E2B hosted | Firecracker isolation, agent-native SDK, <200ms boot, first-class Actions integration |
| LLM | Ollama Cloud | your subscription, fixed cost |
| Agent runtime (in sandbox) | opencode `serve` (background) + HTTP API (`/session`, `/session/{id}/message`) | `serve` only starts an HTTP server (verified: `commands.ts:43-50`); the agent is driven via HTTP, not CLI flags |
| Gate | `scripts/agent-gate.sh` | single source of truth, already specified in CLAUDE.md |
| Board integration | GitHub issues + `GITHUB_TOKEN` (planner) | native, no PAT needed for posting comments |
| Proof (CLI) | asciinema → **GitHub Gist** | asciinema.org anonymous deprecated; Gist is free, auth via token, embeddable |
| Proof (live) | `sandbox.get_host(port)` → public URL | `forward_port` doesn't exist; `get_host(4096)` returns the public hostname |
| Secrets | GitHub Actions secrets | encrypted at rest, masked in logs, scoped per-workflow |
| Trigger | GitHub events (`issues.opened`, `issue_comment.created` filtered to OWNER) | instant, no polling, no inbound infra, public-repo-safe |
| `/approve` guard | `comment.author_association == 'OWNER'` | only the repo owner can trigger the worker on a public repo |
| Concurrency | `concurrency: worker-${issue.number}, cancel-in-progress` | prevents duplicate runs on the same issue |
| Sandbox timeout | `timeout=1500` (< Actions 30min) | sandbox self-destructs before the job can SIGKILL it — no leak even if `finally:` doesn't run |
| LangGraph | **not used** | workflows are the graph; `opencode serve` is the inner loop. Re-evaluate in Phase 3. |

---

## 9. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Over-build before feeling the loop | Phase 0 is mandatory; no Phase 1 code until 3 manual PRs merged |
| Agent loops / cost spiral | Actions `timeout-minutes: 30`, E2B `timeout=1500` (self-destructs before SIGKILL), LLM spend ceiling in `worker.py`, max 3 retries, `concurrency:` prevents duplicate runs, sandbox dies with the job |
| Prompt injection from issue text | planner reads issue (data) + posts via `GITHUB_TOKEN` (issues:write only); worker reads *your-approved plan* (a comment); `BRANCH_PUSHER_TOKEN` can't merge; merge is a UI click (no token to steal) |
| `pull_request_target` footgun | never used; all workflows trigger on `issues` / `issue_comment` (your actions). Phase 3 PR review uses `pull_request` (no secrets) + dispatch. |
| `/approve` from a non-owner | `if: ... && github.event.comment.author_association == 'OWNER'` — only you can trigger the worker |
| E2B outage | workflow fails → Actions retries 3x → issue stays open, you re-trigger manually. No data loss (state is GitHub). |
| Ollama Cloud latency / limits | `worker.py` checks the agent produced session output before running the gate; retry the `POST /session/{id}/message` on 429/5xx with backoff |
| Gate flakiness (conformance) | full asciinema recording; if gate fails, PR is opened as *draft* with the failure recording + log for your diagnosis |
| Agent edits nothing → empty PR | gate asserts `test -n "$(git diff --name-only main...HEAD)"` — empty diffs fail |
| Port collision (agent serve vs conformance) | `worker.py` kills the agent's `opencode serve` (`fuser -k 4096/tcp`) before running the gate; conformance uses `PORT=4096` (now free) |
| Preview URL dies before review | sandbox `timeout=1500` gives a ~25-min window after the PR opens; for longer, Phase 2 adds a PR-closed webhook to kill the sandbox explicitly |
| Secret leak in logs | Actions secrets auto-masked; `BRANCH_PUSHER_TOKEN` scoped to push+PR only |
| Actions 6h job cap | `timeout-minutes: 30` (Phase 1 issues are small); if you outgrow this, you're in Phase 2 territory |
| asciinema upload auth | use GitHub Gists (token-auth, non-interactive), not asciinema.org (anonymous deprecated) |
| E2B template V1 deprecation | use the V2 `Template()` fluent builder, not `e2b.toml` + Dockerfile |
| opencode runtime deps missing | template installs Node + Bun + opencode; bake verifies `opencode --version` |

---

## 10. First concrete steps (this week)

1. Create E2B account, get `E2B_API_KEY`
2. Get Ollama Cloud key
3. Create the GitHub fine-grained PAT `BRANCH_PUSHER_TOKEN` — **Contents: write** + **Pull
   requests: write**, repo-scoped to `opcode42_3`. (No `ISSUE_READER_TOKEN` — the planner
   uses the auto `GITHUB_TOKEN`.) Also create a **classic PAT `GIST_TOKEN`** with `gist`
   scope (fine-grained PATs can't create Gists — needed for gate recording uploads). Add
   `E2B_API_KEY`, `OLLAMA_API_KEY`, `BRANCH_PUSHER_TOKEN`, `GIST_TOKEN` as Actions secrets.
4. Bake the `opcode42-builder` E2B template (V2 `Template()` builder, one-time). Verify
   `opencode --version` runs in the baked template.
5. Write `scripts/agent-gate.sh` in the repo; run it locally once to verify it passes on
   `main`
6. Write `.github/workflows/planner.yml` + `scripts/planner.py`; **stop**, test by creating
   one issue — verify a plan appears as a comment
7. `/approve` that issue, then write `.github/workflows/worker.yml` + `scripts/worker.py`
   against that single issue. The worker must: start `opencode serve` in background, drive
   via `POST /session` + `POST /session/{id}/message`, kill the server, run the gate, restart
   the server, push, open PR with Gist + preview URL.
8. Connect live to the sandbox via `https://<get_host(4096)>` during the agent step, watch
   M1 happen, merge from your phone

---

## 11. Review fixes (rev 4 changelog)

### Rev 3 → rev 4 (2 new blockings found by rev-3 review)

1. **Poll loop never terminated: idle sessions are *deleted* from the status map, not
   present with `type:"idle"`.** Verified against `packages/opencode/src/session/status.ts:42-44`:
   `set()` calls `data.delete(sessionID)` when `status.type === "idle"`. So
   `GET /session/status` omits idle sessions — `statuses.get(sid, {}).get("type") == "idle"`
   is always `False` once the agent finishes (the session is absent, not idle-tagged). The
   rev-3 fix was wrong for the same root cause as the rev-2 bug it replaced: an unverified
   assumption about the API's data model. Fixed: the poll now does a two-phase wait —
   (a) wait for the session to appear as `busy` (confirms the agent started, handles the
   race where `POST /message` returns before `status.set(busy)`), then (b) wait for the
   session to disappear from the map (absence = idle = done). Both phases have iteration
   caps (150 × 1s for busy, 750 × 2s for the agent step = 25 min). (§0, §3)

2. **Preview URL captured with no health check after the restart.** `get_host(4096)`
   returns a deterministic hostname the moment the sandbox boots, but nothing listens on
   4096 until `opencode serve` is up (~2-5s). The PR would open with a dead preview URL.
   Fixed: added a health-check loop (60 × 0.5s) after restarting the server, before
   `get_host` and `gh pr create`. (§3)

Also applied (should-fix from rev-3 review):

- **`StopIteration` guard on the planner-comment fetch.** `next(..., None)` + explicit
  `SystemExit("no planner comment found")` if `/approve` is sent before the planner posts.
  (§3)
- **Dead `sandbox.files.write("/tmp/plan.md", ...)` removed.** The file was never read
  (worker.py runs on the runner; the message body uses `plan_comment` directly). (§3)
- **Stale narrative in §0 and §11 fixed** to match the rev-4 code: poll is
  `GET /session/status` (wait-for-busy-then-absence), plan is passed directly (no
  `/tmp/plan.md` round-trip), Gist upload uses `GIST_TOKEN` (not `BRANCH_PUSHER_TOKEN`).

### Rev 2 → rev 3 (4 new blockings found by rev-2 review, all in `worker.py`)

1. **`GET /session/{id}` has no `status` field — polling loop never terminated.** Verified
   against `openapi.json`: the `Session` schema has properties `id, slug, projectID,
   directory, title, version, time, ...` — **no `status`**. Status lives at a separate
   endpoint `GET /session/status` which returns a map `{[sessionID]: {type:
   "idle"|"busy"|"retry", ...}}` (`SessionStatus` schema). Fixed: `worker.py` now polls
   `GET /session/status`. (§3 — further refined in rev 4, see above)

2. **`_basic_auth()` was undefined + scheme was Bearer-not-Basic + server was started with
   no password.** Verified against `auth.ts:40-42`: `required()` returns false when
   `OPENCODE_SERVER_PASSWORD` is unset, and `authorization.ts:42` passes through
   unauthenticated requests. The middleware expects `Basic` (authorization.ts:33), not
   Bearer. Fixed: dropped the auth header entirely — `worker.py` starts `opencode serve`
   without `OPENCODE_SERVER_PASSWORD`, so auth is disabled (acceptable: the sandbox is
   isolated, no auth needed inside it). No `_basic_auth()` function, no `Authorization`
   header. (§3)

3. **`sandbox.files.read()` returns bytes (JSON crash) + `open("/tmp/plan.md")` ran on the
   runner, not the sandbox (FileNotFoundError).** Fixed: `cast = sandbox.files.read(...)`
   is now decoded (`cast_bytes.decode()`) before JSON serialization; the plan text is
   passed directly into the message body (`plan_comment` variable, already in scope from
   the comment-fetch step) — no `/tmp/plan.md` round-trip on the runner. (§3)

4. **Gist upload used a fine-grained PAT — fine-grained PATs do not support Gist scope.**
   Fixed: added a separate **classic PAT `GIST_TOKEN`** with `gist` scope. `worker.py` uses
   `GIST_TOKEN` (falling back to `BRANCH_PUSHER_TOKEN` only if unset). Added `GIST_TOKEN` to
   the worker workflow env, the token table, and step 10. (§1, §3, §10)

### Rev 1 → rev 2 (original 7 blockings + simplicity/robustness)

1. **`/approve` had no author check (public repo).** Fixed: `worker.yml` `if:` now includes
   `&& github.event.comment.author_association == 'OWNER'`. Only the repo owner can trigger
   the worker. (§0, §1, §3, §8)

2. **`opencode serve --model ... --prompt-file ...` is fabricated CLI.** Verified against
   `packages/cli/src/commands/commands.ts:43-50`: `serve` accepts only `--hostname`,
   `--port`, `--register`. It starts a blocking HTTP server (`serve.ts:24` returns
   `Effect.never`). Fixed: `worker.py` now starts `opencode serve --port 4096 --hostname
   0.0.0.0` in the **background** and drives the agent via the HTTP API — `POST /session`
   (with `{model: {providerID, id}}`) then `POST /session/{id}/message` (with `{parts:
   [{type:"text", text: <plan>}]}`), polling `GET /session/status` (wait for `busy`, then
   wait for absence = idle — see rev 4 fix).
   Verified against `openapi.json`: `session.create`, `session.prompt`, and
   `session.status` operations exist with those request/response shapes. (§0, §3, §8)

3. **`sandbox.forward_port(1337)` doesn't exist.** Fixed: replaced with
   `sandbox.get_host(4096)` — the public URL `https://<host(4096)>` is live the moment a
   process listens on 4096; there's no "forward" step. (§3, §8)

4. **Preview URL died before the PR existed (synchronous `worker.py`).** Fixed: `worker.py`
   is restructured. The agent server runs in the background during the agent step (so
   live-steering works *while the agent runs* — M2 is achievable). After the gate, the
   server is restarted for the preview URL, the PR is opened, and the sandbox stays alive
   until the E2B `timeout=1500` (a ~25-min review window). `finally:` kills the sandbox as
   belt-and-suspenders. (§0, §3, §7-M2)

5. **Conformance port collision (agent's `opencode serve` on 4096 vs conformance's
   `PORT=4096`).** Verified: `scripts/run-conformance.sh:29` defaults `PORT=4096` and starts
   `opencode serve --port $PORT` (line 61). Fixed: `worker.py` kills the agent's server
   (`fuser -k 4096/tcp`) before running `agent-gate.sh`, so conformance's `PORT=4096` is
   free. Documented in `agent-gate.sh`. (§0, §5)

6. **`ISSUE_READER_TOKEN` can't post comments (described as read-only).** Fixed: removed
   `ISSUE_READER_TOKEN` entirely. The planner posts its plan comment via the workflow's
   auto `GITHUB_TOKEN` with `permissions: issues: write` (verified: `issues: write` permits
   commenting on issues). The worker reads issue comments via the same `GITHUB_TOKEN`. No
   read-only PAT is needed in Phase 1. (§1, §3)

7. **`BRANCH_PUSHER_TOKEN` scope conflated branch protection with PAT permissions.** Fixed:
   the PAT is now specified as **Contents: write** + **Pull requests: write**, repo-scoped
   to `opcode42_3`. Branch protection on `main` (no direct push) + allow `agent/*` enforces
   the branch rule — the PAT itself is scoped by *permission*, not by branch. (§1, §3, §10)

Also applied (should-fix / simplicity):

- **Phantom `merger` token stripped.** Merging is a GitHub UI click under your login — no
  token is involved. All mentions removed; the token table now says "merge → nowhere, you
  merge via the GitHub UI." (§1, §3)
- **Phase 2/3 collapsed to "later" pointers.** They were given equal weight with full cost
  tables and milestones, making the doc read like you must design for BYOC now. Now Phase 2
  is 5 lines ("only if you hit a limit"), Phase 3 is 4 lines ("later"). (§2, §6, §7)
- **MCP deferred from Phase 0 to Phase 1.** Phase 0 is now "paste issue text, run gate,
  review yourself, merge" — no MCP, no review subagent. Both belong to Phase 1/3. (§2)
- **Review subagent deferred from Phase 0 to Phase 3.** Phase 0 says "review the PR
  yourself." (§2)
- **E2B egress allowlist moved to Phase 2.** E2B hosted sandboxes have open egress — the
  allowlist is a BYOC/VPC control, not a Phase-1 control. §1 now says this explicitly.
  (§1)
- **`concurrency:` group added** to `worker.yml` — `worker-${issue.number},
  cancel-in-progress: true` prevents duplicate runs on the same issue. (§3, §8)
- **Empty-diff check added** to `agent-gate.sh` — `test -n "$(git diff --name-only
  main...HEAD)"` rejects no-op PRs. (§5)
- **E2B `timeout=1500` < Actions `timeout-minutes: 30`** — the sandbox self-destructs
  before the job can SIGKILL it, so `finally:` leak is impossible. (§0, §3, §8, §9)
- **asciinema upload → GitHub Gist**, not asciinema.org (anonymous deprecated in v3,
  requires interactive auth a sandbox can't do). (§3, §5, §8)
- **Plan → worker data handoff specified.** `worker.py` fetches the planner's comment via
  `GET /issues/{n}/comments` using `GITHUB_TOKEN`, finds the `github-actions[bot]` comment,
   passes `plan_comment` directly into the message body (no `/tmp/plan.md` round-trip).
   (§3)
- **E2B template V2 `Template()` builder**, not the deprecated V1 `e2b.toml` + Dockerfile.
  (§4)
- **opencode runtime deps in the template** — Node + Bun installed; bake verifies
  `opencode --version`. (§4)
- **Planner reads `plans/` from the runner checkout** (already there via
  `actions/checkout`), not from a sandbox clone. (§3)
- **`/approve` must be a new comment, not an edit** — `issue_comment` triggers on `created`
  only. Documented in the risk table. (§9)