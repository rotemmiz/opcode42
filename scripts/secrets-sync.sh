#!/usr/bin/env bash
# Sync agentic-devex secrets from .env.local to GitHub Actions secrets.
#
# Usage:
#   scripts/secrets-sync.sh --init       # generate .env.local with empty values
#   # fill in .env.local (it's gitignored — never committed)
#   scripts/secrets-sync.sh --dry-run    # shows what would be set, doesn't push
#   scripts/secrets-sync.sh              # pushes each key to the repo
#
# .env.local is gitignored — never committed. This script reads it locally and
# pushes each value to GitHub via `gh secret set`. The values never touch the
# repo, git history, or CI logs (Actions secrets are encrypted at rest and
# masked in logs).
#
# Requirements: gh CLI, authenticated (`gh auth status`).
set -euo pipefail

ENV_FILE="${ENV_FILE:-.env.local}"

# The keys to sync (must match the workflow env: blocks in planner.yml + worker.yml)
KEYS=(
  E2B_API_KEY
  E2B_TEMPLATE
  OLLAMA_API_KEY
  OLLAMA_URL
  OLLAMA_MODEL
  BRANCH_PUSHER_TOKEN
  GIST_TOKEN
)

ACTION="sync"
[ "${1:-}" = "--dry-run" ] && ACTION="dry-run"
[ "${1:-}" = "--init" ] && ACTION="init"

if [ "$ACTION" = "init" ]; then
  if [ -f "$ENV_FILE" ]; then
    echo "error: $ENV_FILE already exists — not overwriting."
    exit 1
  fi
  for key in "${KEYS[@]}"; do
    echo "${key}="
  done > "$ENV_FILE"
  echo "created $ENV_FILE with empty values. Fill them in, then run: scripts/secrets-sync.sh"
  exit 0
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "error: $ENV_FILE not found. Run: scripts/secrets-sync.sh --init"
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "error: gh CLI not installed. Install: https://cli.github.com/"
  exit 1
fi

if ! gh auth status >/dev/null 2>&1; then
  echo "error: gh not authenticated. Run: gh auth login"
  exit 1
fi

REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)
if [ -z "$REPO" ]; then
  echo "error: could not determine the repo. Run this from the repo root."
  exit 1
fi

# The keys to sync (must match the workflow env: blocks in planner.yml + worker.yml)
KEYS=(
  E2B_API_KEY
  E2B_TEMPLATE
  OLLAMA_API_KEY
  OLLAMA_URL
  OLLAMA_MODEL
  BRANCH_PUSHER_TOKEN
  GIST_TOKEN
)

echo "repo: $REPO"
echo "source: $ENV_FILE"
[ "$ACTION" = "dry-run" ] && echo "mode: DRY-RUN (no secrets will be pushed)"
echo

set +e
pushed=0
skipped=0
for key in "${KEYS[@]}"; do
  # Read the value from .env.local (strip optional surrounding quotes)
  value=$(grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -1 | sed 's/^[^=]*=//' | sed 's/^"//' | sed 's/"$//' || true)
  if [ -z "$value" ]; then
    printf "  %-22s SKIP (empty or not in %s)\n" "$key" "$ENV_FILE"
    skipped=$((skipped + 1))
    continue
  fi
  if [ "$ACTION" = "dry-run" ]; then
    printf "  %-22s WOULD SET (%d chars)\n" "$key" "${#value}"
  else
    printf "  %-22s setting... " "$key"
    if printf '%s' "$value" | gh secret set "$key" --repo "$REPO" 2>/dev/null; then
      echo "OK"
      pushed=$((pushed + 1))
    else
      echo "FAIL"
      exit 1
    fi
  fi
done
set -e

echo
if [ "$ACTION" = "dry-run" ]; then
  echo "dry-run complete: $pushed would be set, $skipped skipped."
else
  echo "done: $pushed secrets pushed to $REPO, $skipped skipped."
fi

if [ "$skipped" -gt 0 ]; then
  echo
  echo "warning: $skipped secret(s) were empty or missing in $ENV_FILE."
  echo "  The workflows will fail on the first missing key they try to use."
  echo "  Fill them in and re-run this script."
fi