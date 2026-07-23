#!/usr/bin/env bash
# Agent gate — the single source of truth for "correct."
#
# Run inside an E2B sandbox AFTER the agent (opencode serve) has been stopped.
# The worker.py kills the agent's opencode serve (fuser -k 4096/tcp) before
# invoking this script, so PORT=4096 is free for the conformance self-diff.
#
# Usage:
#   asciinema rec /tmp/gate.cast --command 'bash scripts/agent-gate.sh'
#
# Exits 0 only if every check passes. The worker opens a PR only on exit 0.
set -euo pipefail

echo "=== agent-gate: start ==="

# 1. Supply chain — verify module checksums match go.sum
echo "=== go mod verify ==="
go mod verify

# 2. Secret scan — reject leaked tokens/keys in the diff
if command -v gitleaks >/dev/null 2>&1; then
  echo "=== gitleaks ==="
  gitleaks detect --source . --no-banner --redact --exit-code 1
else
  echo "WARN: gitleaks not installed — skipping secret scan"
fi

# 3. Non-empty diff — reject no-op PRs (the repo is green on main, so an empty
#    diff passes every other check trivially; force the agent to have actually
#    changed something)
echo "=== non-empty diff check ==="
CHANGED=$(git diff --name-only main...HEAD || true)
if [ -z "$CHANGED" ]; then
  echo "FAIL: no changes vs main — refusing to open an empty PR"
  exit 1
fi
echo "changed files: $CHANGED"

# 4. Format check — gofmt -l must be empty
echo "=== gofmt ==="
FMT=$(gofmt -l .)
if [ -n "$FMT" ]; then
  echo "FAIL: gofmt would reformat: $FMT"
  exit 1
fi

# 5. Generate + diff check (frozen contract — plans/06)
echo "=== make gen + diff check ==="
make gen
if ! git diff --exit-code internal/api/gen/; then
  echo "FAIL: internal/api/gen/ has uncommitted changes after make gen — regenerate and commit"
  exit 1
fi

# 6. Build
echo "=== go build ==="
go build ./...

# 7. Vet
echo "=== go vet ==="
go vet ./...

# 8. Lint
if command -v golangci-lint >/dev/null 2>&1; then
  echo "=== golangci-lint ==="
  golangci-lint run
else
  echo "WARN: golangci-lint not installed — skipping lint"
fi

# 9. Test
echo "=== go test ==="
go test ./...

# 10. Conformance — the correctness gate against real opencode (plans/12).
#     Uses PORT=4096. The worker kills the agent's opencode serve before
#     running this, so the port is free. If you forget to kill it, this fails
#     with "port in use".
echo "=== conformance self-diff ==="
PORT=4096 scripts/run-conformance.sh self

echo "=== agent-gate: PASSED ==="