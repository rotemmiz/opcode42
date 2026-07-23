#!/usr/bin/env python3
"""Planner agent — reads an issue + repo plans, calls Ollama Cloud, posts a plan comment.

Triggered by .github/workflows/planner.yml on issues.opened.
Runs on the GitHub Actions runner (no E2B sandbox needed for planning — it only reads
files from the checkout and calls the LLM).

Env (set by the workflow):
  ISSUE_NUMBER  — the issue number
  ISSUE_TITLE   — the issue title
  ISSUE_BODY    — the issue body (untrusted text — treated as data, not instructions)
  REPO          — owner/repo, e.g. rotemmiz/opcode42
  OLLAMA_API_KEY — Ollama Cloud key
  GITHUB_TOKEN  — the workflow's auto token (permissions: issues: write)

Posts a structured plan as a comment on the issue. The user then /approves to trigger
the worker.
"""
from __future__ import annotations

import os
import sys
import textwrap

import requests

OLLAMA_URL = os.environ.get("OLLAMA_URL", "https://api.ollama.ai/chat/completions")
OLLAMA_MODEL = os.environ.get("OLLAMA_MODEL", "glm-5.2")


def read_plan_context() -> str:
    """Read the masterplan + any plan file the issue body references."""
    chunks: list[str] = []
    masterplan = "plans/00-masterplan.md"
    if os.path.exists(masterplan):
        with open(masterplan) as f:
            chunks.append(f"# Masterplan (plans/00-masterplan.md)\n\n{f.read()}")
    # If the issue body mentions a plan file (e.g. "plans/07-client-mobile.md"),
    # pull it in too.
    body = os.environ.get("ISSUE_BODY", "")
    for token in body.split():
        token = token.strip("`,()[]\"'")
        if token.startswith("plans/") and token.endswith(".md") and os.path.exists(token):
            with open(token) as f:
                chunks.append(f"# Referenced plan ({token})\n\n{f.read()}")
    return "\n\n---\n\n".join(chunks) if chunks else "(no plan files found)"


def call_ollama(system: str, user: str) -> str:
    resp = requests.post(
        OLLAMA_URL,
        headers={"Authorization": f"Bearer {os.environ['OLLAMA_API_KEY']}"},
        json={
            "model": OLLAMA_MODEL,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
        },
        timeout=120,
    )
    resp.raise_for_status()
    return resp.json()["choices"][0]["message"]["content"]


def post_comment(repo: str, issue_number: str, body: str) -> None:
    requests.post(
        f"https://api.github.com/repos/{repo}/issues/{issue_number}/comments",
        headers={
            "Authorization": f"Bearer {os.environ['GITHUB_TOKEN']}",
            "Accept": "application/vnd.github+json",
        },
        json={"body": body},
        timeout=30,
    ).raise_for_status()


def main() -> int:
    issue_number = os.environ["ISSUE_NUMBER"]
    issue_title = os.environ["ISSUE_TITLE"]
    issue_body = os.environ.get("ISSUE_BODY", "")
    repo = os.environ["REPO"]

    plan_ctx = read_plan_context()

    system = textwrap.dedent("""\
        You are a planning agent for the opcode42 project (a Go daemon, wire-compatible
        with opencode). Given a GitHub issue and the relevant plan section, produce a
        structured implementation plan with:
          - ## Files (list of files to touch)
          - ## Approach (step-by-step)
          - ## Risks (what could break)
          - ## Conformance impact (does this affect wire compat? see plans/12)
        Do NOT re-architect — the plans are the source of truth. Map the issue to the
        plan section and confirm scope. If the issue contradicts the plan, say so and
        stop.""")
    user = f"# Issue #{issue_number}: {issue_title}\n\n{issue_body}\n\n---\n\n# Project plans\n\n{plan_ctx}"

    print(f"planner: calling {OLLAMA_MODEL} for issue #{issue_number}...", flush=True)
    plan_md = call_ollama(system, user)

    comment = (
        f"## Proposed plan\n\n{plan_md}\n\n---\n\n"
        "Reply `/approve` to proceed (repo owner only)."
    )
    post_comment(repo, issue_number, comment)
    print(f"planner: posted plan comment on issue #{issue_number}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())