#!/usr/bin/env python3
"""Worker agent — boots an E2B sandbox, drives opencode via HTTP, runs the gate, opens a PR.

Triggered by .github/workflows/worker.yml on issue_comment.created where body == /approve
and the commenter is the repo OWNER.

Runs on the GitHub Actions runner. The E2B sandbox is where the agent (opencode serve)
and the gate (agent-gate.sh) actually run.

Env (set by the workflow):
  ISSUE_NUMBER       — the issue number
  REPO               — owner/repo, e.g. rotemmiz/opcode42
  E2B_API_KEY        — E2B cloud key
  OLLAMA_API_KEY     — Ollama Cloud key (injected to the sandbox)
  BRANCH_PUSHER_TOKEN — fine-grained PAT (Contents:write + Pull requests:write)
  GIST_TOKEN         — classic PAT with gist scope (for gate recording upload)
  GITHUB_TOKEN       — auto token (read issue comments)

Sequence:
  1. boot E2B sandbox (timeout=1500s < Actions 30min — self-destructs before SIGKILL)
  2. clone repo on agent/<issue-n> branch
  3. fetch the approved plan from the issue comments (github-actions[bot] comment)
  4. start opencode serve --port 4096 --hostname 0.0.0.0 in the background (no auth)
  5. drive the agent: POST /session, POST /session/{id}/message
  6. poll GET /session/status — wait for busy, then wait for absence (idle = deleted)
  7. kill the agent's opencode serve (fuser -k 4096/tcp) so PORT=4096 is free
  8. run scripts/agent-gate.sh (asciinema-recorded)
  9. upload the gate recording to a GitHub Gist
 10. restart opencode serve for the preview URL (wait for health)
 11. push branch + open PR with Gist link + preview URL
 12. sandbox stays alive until the E2B timeout (1500s) — reviewer can poke the preview
 13. finally: kill the sandbox (belt-and-suspenders; the timeout is the real guarantee)
"""
from __future__ import annotations

import json
import os
import sys
import time

import requests
from e2b import Sandbox

E2B_TEMPLATE = os.environ.get("E2B_TEMPLATE", "opcode42-builder")
E2B_TIMEOUT = 1500  # seconds (< Actions timeout-minutes: 30 = 1800s)
AGENT_PORT = 4096
OLLAMA_MODEL_PROVIDER = "ollama-cloud"
OLLAMA_MODEL_ID = os.environ.get("OLLAMA_MODEL", "glm-5.2")


def main() -> int:
    issue_number = os.environ["ISSUE_NUMBER"]
    repo = os.environ["REPO"]
    branch = f"agent/{issue_number}"

    print(f"worker: issue #{issue_number}, branch {branch}", flush=True)

    # 2. Fetch the approved plan from the issue comments (find the planner's comment)
    print("worker: fetching plan comment...", flush=True)
    comments = requests.get(
        f"https://api.github.com/repos/{repo}/issues/{issue_number}/comments",
        headers={
            "Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}",
            "Accept": "application/vnd.github+json",
        },
        timeout=30,
    )
    comments.raise_for_status()
    plan_comment = next(
        (c["body"] for c in comments.json() if c["user"]["login"] == "github-actions[bot]"),
        None,
    )
    if plan_comment is None:
        raise SystemExit("no planner comment found on the issue — run the planner first")

    # 1. Boot E2B sandbox
    print(f"worker: booting E2B sandbox (template={E2B_TEMPLATE}, timeout={E2B_TIMEOUT}s)...", flush=True)
    sandbox = Sandbox.create(template=E2B_TEMPLATE, timeout=E2B_TIMEOUT)
    print(f"worker: sandbox={sandbox.sandbox_id}", flush=True)

    try:
        # 3. Clone repo on agent/<issue-n> branch
        clone_url = (
            f"https://x-access-token:{os.environ['BRANCH_PUSHER_TOKEN']}"
            f"@github.com/{repo}.git"
        )
        print("worker: cloning repo...", flush=True)
        sandbox.commands.run(
            f"git clone {clone_url} repo && cd repo && git checkout -b {branch}",
            timeout=120,
        )

        # 4. Start opencode serve in the BACKGROUND (no auth — sandbox is isolated).
        #    opencode serve blocks forever; background=True returns immediately.
        #    Pass OLLAMA_API_KEY inline so opencode auto-detects the ollama-cloud
        #    provider (provider.ts:1488-1499). Redirect output to a log file so
        #    we can diagnose crashes.
        print(f"worker: starting opencode serve on port {AGENT_PORT} (background)...", flush=True)
        ollama_key = os.environ["OLLAMA_API_KEY"]
        sandbox.commands.run(
            f"cd repo && OLLAMA_API_KEY={ollama_key} "
            f"PATH=/usr/local/go/bin:/usr/local/.bun/bin:/usr/local/bin:$PATH "
            f"opencode serve --port {AGENT_PORT} --hostname 0.0.0.0 "
            f"> /tmp/opencode.log 2>&1",
            background=True,
        )
        _wait_health(sandbox, AGENT_PORT)

        # 5. Drive the agent via the HTTP API (NOT via CLI flags — serve has none).
        #    All HTTP calls run INSIDE the sandbox via curl (the Actions runner
        #    can't reach 127.0.0.1:4096 — that's localhost inside the sandbox).
        base = f"http://127.0.0.1:{AGENT_PORT}"
        print(f"worker: creating session (model={OLLAMA_MODEL_PROVIDER}/{OLLAMA_MODEL_ID})...", flush=True)
        session_body = json.dumps({
            "model": {"providerID": OLLAMA_MODEL_PROVIDER, "id": OLLAMA_MODEL_ID},
            "title": f"issue-{issue_number}",
        })
        sess_resp = sandbox.commands.run(
            f"curl -s -w '\\n%{{http_code}}' -X POST {base}/session "
            f"-H 'Content-Type: application/json' "
            f"-d '{session_body}'",
            timeout=30,
        )
        if sess_resp.exit_code != 0:
            raise SystemExit(f"failed to create session: {sess_resp.stderr}")
        resp_lines = sess_resp.stdout.strip().rsplit("\n", 1)
        http_code = resp_lines[-1] if len(resp_lines) > 1 else "???"
        if http_code != "200":
            _dump_opencode_logs(sandbox)
            raise SystemExit(f"session create returned HTTP {http_code} (expected 200) — check opencode logs above")
        sid = json.loads(resp_lines[0])["id"]
        print(f"worker: session={sid}, sending plan as prompt...", flush=True)

        # Send the plan as the prompt. Use prompt_async (returns 204 immediately,
        # agent runs in background) instead of the synchronous /message endpoint
        # (which blocks until the agent loop completes). Use a temp file to
        # avoid shell-escaping the potentially large plan text.
        plan_path = "/tmp/plan.json"
        plan_body = json.dumps({"parts": [{"type": "text", "text": plan_comment}]})
        sandbox.files.write(plan_path, plan_body.encode())
        msg_resp = sandbox.commands.run(
            f"curl -s -o /dev/null -w '%{{http_code}}' -X POST {base}/session/{sid}/prompt_async "
            f"-H 'Content-Type: application/json' "
            f"-d @{plan_path}",
            timeout=30,
        )
        if msg_resp.exit_code != 0:
            raise SystemExit(f"failed to send message: {msg_resp.stderr}")
        http_code = msg_resp.stdout.strip()
        if http_code != "204":
            _dump_opencode_logs(sandbox)
            raise SystemExit(f"prompt_async returned HTTP {http_code} (expected 204) — check opencode logs above")

        # 6. Poll GET /session/status — two-phase wait.
        #    status.ts:42-44 DELETES a session from the map when it goes idle,
        #    so a finished session is ABSENT from the response, not present with
        #    type:"idle". Correct sequence:
        #      (a) wait for "busy" (confirms the agent started)
        #      (b) wait for absence (confirms it went idle = done)
        print("worker: waiting for agent to start (busy)...", flush=True)
        for _ in range(150):  # 150 × 1s = 2.5 min
            try:
                status_resp = sandbox.commands.run(
                    f"curl -sf {base}/session/status", timeout=10
                )
                if status_resp.exit_code == 0:
                    statuses = json.loads(status_resp.stdout)
                    if statuses.get(sid, {}).get("type") == "busy":
                        print("worker: agent is busy, waiting for completion (absence=idle)...", flush=True)
                        break
            except Exception:
                pass
            time.sleep(1)
        else:
            _dump_opencode_logs(sandbox)
            raise SystemExit("agent never entered busy state — check opencode serve logs above")

        for _ in range(750):  # 750 × 2s = 25 min cap on the agent step
            try:
                status_resp = sandbox.commands.run(
                    f"curl -sf {base}/session/status", timeout=10
                )
                if status_resp.exit_code == 0:
                    statuses = json.loads(status_resp.stdout)
                    if sid not in statuses:
                        print("worker: agent finished (session absent from status map = idle)", flush=True)
                        break
            except Exception:
                pass
            time.sleep(2)
        else:
            _dump_opencode_logs(sandbox)
            raise SystemExit("agent did not finish within 25 min — killing sandbox")

        # 6b. After the agent finishes, check what it actually did
        print("worker: checking agent output...", flush=True)
        try:
            msgs_resp = sandbox.commands.run(
                f"curl -sf {base}/session/{sid}/messages", timeout=10
            )
            if msgs_resp.exit_code == 0:
                msgs = json.loads(msgs_resp.stdout)
                msg_count = len(msgs) if isinstance(msgs, list) else 0
                print(f"worker: session has {msg_count} messages", flush=True)
        except Exception:
            pass

        # Kill the agent's opencode serve so PORT=4096 is free for conformance
        print("worker: killing agent opencode serve (free port for conformance)...", flush=True)
        sandbox.commands.run(f"fuser -k {AGENT_PORT}/tcp", timeout=10)
        time.sleep(1)

        # Check that the agent actually produced changes
        diff_check = sandbox.commands.run("cd repo && git diff --exit-code", timeout=10)
        if diff_check.exit_code == 0:
            _dump_opencode_logs(sandbox)
            raise SystemExit("agent produced no changes — git diff is empty. Check opencode logs above.")

        # 8. Run the gate (asciinema-recorded, separate process from the agent)
        print("worker: running agent-gate.sh (asciinema-recorded)...", flush=True)
        sandbox.commands.run(
            "cd repo && asciinema rec /tmp/gate.cast --command 'bash scripts/agent-gate.sh'",
            timeout=600,
        )

        # 9. Upload the gate recording as a GitHub Gist.
        #    Requires GIST_TOKEN — a classic PAT with `gist` scope. Fine-grained
        #    PATs (like BRANCH_PUSHER_TOKEN) cannot create Gists at all, so there
        #    is no fallback. If GIST_TOKEN is unset, fail loudly here.
        print("worker: uploading gate recording to Gist...", flush=True)
        cast_bytes = sandbox.files.read("/tmp/gate.cast")
        cast = cast_bytes.decode() if isinstance(cast_bytes, bytes) else cast_bytes
        gist_token = os.environ["GIST_TOKEN"]  # required — no fallback (fine-grained PATs can't do Gists)
        gist = requests.post(
            "https://api.github.com/gists",
            headers={
                "Authorization": f"Bearer {gist_token}",
                "Accept": "application/vnd.github+json",
            },
            json={"public": False, "files": {"gate.cast": {"content": cast}}},
            timeout=30,
        )
        gist.raise_for_status()
        asciinema_url = gist.json()["html_url"]

        # 10. Restart opencode serve for the preview URL, wait for health
        print("worker: restarting opencode serve for preview URL...", flush=True)
        sandbox.commands.run(
            f"cd repo && OLLAMA_API_KEY={ollama_key} "
            f"PATH=/usr/local/go/bin:/usr/local/.bun/bin:/usr/local/bin:$PATH "
            f"opencode serve --port {AGENT_PORT} --hostname 0.0.0.0",
            background=True,
        )
        _wait_health(sandbox, AGENT_PORT)
        preview_url = f"https://{sandbox.get_host(AGENT_PORT)}"

        # 11. Push branch + open PR
        print(f"worker: pushing {branch}...", flush=True)
        sandbox.commands.run(f"cd repo && git push origin {branch}", timeout=60)

        pr_body = (
            f"## Gate recording\n{asciinema_url}\n\n"
            f"## Live preview\n{preview_url}\n\n"
            f"Closes #{issue_number}\n\n"
            "This PR was produced by the agentic-devex worker. "
            "Review the gate recording + poke the preview URL, then merge."
        )
        print(f"worker: opening PR for issue #{issue_number}...", flush=True)
        pr = requests.post(
            f"https://api.github.com/repos/{repo}/pulls",
            headers={
                "Authorization": f"Bearer {os.environ['BRANCH_PUSHER_TOKEN']}",
                "Accept": "application/vnd.github+json",
            },
            json={
                "head": branch,
                "base": "main",
                "title": f"[agent] issue #{issue_number}",
                "body": pr_body,
            },
            timeout=30,
        )
        pr.raise_for_status()
        pr_url = pr.json()["html_url"]
        print(f"worker: PR opened: {pr_url}", flush=True)
        print(f"worker: preview URL: {preview_url} (live until sandbox timeout)", flush=True)
        print(f"worker: gate recording: {asciinema_url}", flush=True)

        # 12. Sandbox stays alive until the E2B timeout (1500s) — reviewer can poke
        #     the preview URL. The worker exits here; the sandbox self-destructs.

    finally:
        # 13. Belt-and-suspenders. The E2B timeout is the real guarantee — even if
        #     this finally: doesn't run (SIGKILL on cancel), the sandbox dies at 1500s.
        print("worker: killing sandbox (finally:)...", flush=True)
        try:
            sandbox.kill()
        except Exception:
            pass

    return 0


def _dump_opencode_logs(sandbox: Sandbox) -> None:
    """Dump opencode serve logs to help diagnose crashes."""
    try:
        r = sandbox.commands.run("cat /tmp/opencode.log 2>&1 | tail -50", timeout=5)
        print(f"worker: opencode.log (last 50 lines):\n{r.stdout}", flush=True)
    except Exception as e:
        print(f"worker: could not read opencode.log: {e}", flush=True)


def _wait_health(sandbox: Sandbox, port: int, tries: int = 60, delay: float = 0.5) -> None:
    """Wait for opencode serve to respond on /global/health."""
    for i in range(tries):
        try:
            r = sandbox.commands.run(
                f"curl -fsS http://127.0.0.1:{port}/global/health", timeout=5
            )
            if r.exit_code == 0:
                return
        except Exception:
            pass
        time.sleep(delay)
    # Diagnose why it failed before raising
    print("worker: health check failed — diagnosing...", flush=True)
    try:
        diag = sandbox.commands.run("which opencode; echo '---'; ps aux | head -20", timeout=5)
        print(f"worker: diag: {diag.stdout}", flush=True)
    except Exception as e:
        print(f"worker: diag failed: {e}", flush=True)
    raise SystemExit(f"opencode serve did not become healthy on port {port}")


if __name__ == "__main__":
    sys.exit(main())