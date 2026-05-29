#!/usr/bin/env bash
# Conformance runner (task C7).
#
#   scripts/run-conformance.sh self            # opencode-vs-opencode self-diff (Phase-A gate)
#   scripts/run-conformance.sh dual <forgeURL> # opencode (truth) vs a forge daemon
#
# The self-diff is the Phase-A correctness gate: it proves the harness + normalizer
# are deterministic before forge implements anything. Each opencode run gets a
# fresh HOME (hence a fresh SQLite DB + config) so global state — e.g. the session
# list — does not bleed between runs.
set -euo pipefail

MODE="${1:-self}"
PORT="${PORT:-4096}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RESULTS="$REPO_ROOT/conformance/results"
mkdir -p "$RESULTS"

wait_health() {
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 1 "http://127.0.0.1:$PORT/global/health" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "error: opencode did not become healthy on port $PORT" >&2
  return 1
}

# run_opencode_suite <out.json>: start a pristine opencode, run the suite, stop it.
run_opencode_suite() {
  local out="$1"
  local home; home="$(mktemp -d)"
  HOME="$home" opencode serve --port "$PORT" --hostname 127.0.0.1 >"$home/serve.log" 2>&1 &
  local pid=$!
  if ! wait_health; then cat "$home/serve.log" >&2; kill -9 "$pid" 2>/dev/null || true; rm -rf "$home"; return 1; fi
  ( cd "$REPO_ROOT" && go test ./conformance/ -run TestSuite -target="http://127.0.0.1:$PORT" -out="$out" >/dev/null )
  kill -9 "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -rf "$home"
  sleep 1 # let the port free up before the next run
}

case "$MODE" in
  self)
    echo "self-diff: opencode vs opencode (fresh state each run)"
    run_opencode_suite "$RESULTS/opencode-1.json"
    run_opencode_suite "$RESULTS/opencode-2.json"
    echo "=== diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" "$RESULTS/opencode-1.json" "$RESULTS/opencode-2.json"
    ;;
  dual)
    FORGE_URL="${2:-http://127.0.0.1:4097}"
    echo "dual-run: opencode (truth) vs forge at $FORGE_URL"
    run_opencode_suite "$RESULTS/opencode.json"
    ( cd "$REPO_ROOT" && go test ./conformance/ -run TestSuite -target="$FORGE_URL" -out="$RESULTS/forge.json" >/dev/null )
    echo "=== diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" "$RESULTS/opencode.json" "$RESULTS/forge.json"
    ;;
  *)
    echo "usage: $0 [self|dual <forgeURL>]" >&2
    exit 2
    ;;
esac
