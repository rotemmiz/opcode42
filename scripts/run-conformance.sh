#!/usr/bin/env bash
# Conformance runner (task C7).
#
#   scripts/run-conformance.sh self            # opencode-vs-opencode self-diff (Phase-A gate)
#   scripts/run-conformance.sh dual <opcode42URL> # opencode (truth) vs a opcode42 daemon
#   scripts/run-conformance.sh live            # LIVE dual-run: opencode vs a freshly-built
#                                              #   opcoded, both prompting a real model
#                                              #   (google/gemini-2.5-flash). Skip-gated:
#                                              #   needs a provider key (see below).
#   scripts/run-conformance.sh live-self       # opencode-vs-opencode live self-diff
#                                              #   (proves the live harness/normalizer is
#                                              #   deterministic before trusting the dual diff)
#
# The self-diff is the Phase-A correctness gate: it proves the harness + normalizer
# are deterministic before opcode42 implements anything. Each opencode run gets a
# fresh HOME (hence a fresh SQLite DB + config) so global state — e.g. the session
# list — does not bleed between runs.
#
# The LIVE modes drive a real LLM, so output is non-deterministic; the live
# normalizer masks model-output-dependent fields (text/cost/tokens) and the diff
# asserts response SHAPE + the shared SSE event-type sequence, not exact text.
# The provider key must NOT live in the repo: load it from ~/.opcode42/conformance.env
# (chmod 600) which exports GOOGLE_GENERATIVE_AI_API_KEY (or set the env var
# yourself). When no key is present the live modes SKIP cleanly (exit 0) so CI
# without the key stays green.
set -euo pipefail

MODE="${1:-self}"
PORT="${PORT:-4096}"
OPCODE_PORT="${OPCODE_PORT:-4097}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RESULTS="$REPO_ROOT/conformance/results"
mkdir -p "$RESULTS"

# Auth is enabled so the auth-conformance scenarios (#20-22) are exercised; the
# suite sends these Basic credentials on every request.
OC_USER="${OPENCODE_SERVER_USERNAME:-opencode}"
OC_PASS="${OPENCODE_SERVER_PASSWORD:-conformance-test}"

# Live runs need a real provider key. Source the out-of-repo env file if present;
# never commit the key.
LIVE_ENV="${OPCODE_CONFORMANCE_ENV:-$HOME/.opcode42/conformance.env}"

wait_health() {
  local port="$1"
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 1 -u "$OC_USER:$OC_PASS" "http://127.0.0.1:$port/global/health" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "error: daemon did not become healthy on port $port" >&2
  return 1
}

# run_opencode_suite <test> <out.json>: start a pristine, auth-enabled opencode,
# run the given suite test with credentials, stop it. Any GOOGLE_*/GEMINI_* key
# in the current environment is forwarded so live scenarios can reach the model.
run_opencode_suite() {
  local test="$1" out="$2"
  local home; home="$(mktemp -d)"
  HOME="$home" OPENCODE_SERVER_USERNAME="$OC_USER" OPENCODE_SERVER_PASSWORD="$OC_PASS" \
    opencode serve --port "$PORT" --hostname 127.0.0.1 >"$home/serve.log" 2>&1 &
  local pid=$!
  if ! wait_health "$PORT"; then cat "$home/serve.log" >&2; kill -9 "$pid" 2>/dev/null || true; rm -rf "$home"; return 1; fi
  ( cd "$REPO_ROOT" && go test ./conformance/ -run "$test" \
      -target="http://127.0.0.1:$PORT" -user="$OC_USER" -pass="$OC_PASS" -out="$out" >/dev/null )
  kill -9 "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -rf "$home"
  sleep 1 # let the port free up before the next run
}

# run_opcode42_suite <test> <out.json>: build + start a pristine, auth-enabled
# opcoded on OPCODE_PORT, run the given suite test against it, stop it. Forwards the
# provider key the same way as opencode.
run_opcode42_suite() {
  local test="$1" out="$2"
  local home; home="$(mktemp -d)"
  local bin="$home/opcoded"
  ( cd "$REPO_ROOT" && go build -o "$bin" ./cmd/opcoded )
  HOME="$home" OPENCODE_SERVER_USERNAME="$OC_USER" OPENCODE_SERVER_PASSWORD="$OC_PASS" \
    "$bin" --port "$OPCODE_PORT" --host 127.0.0.1 >"$home/opcoded.log" 2>&1 &
  local pid=$!
  wait_health "$OPCODE_PORT" || { cat "$home/opcoded.log" >&2; kill -9 "$pid" 2>/dev/null || true; rm -rf "$home"; return 1; }
  ( cd "$REPO_ROOT" && go test ./conformance/ -run "$test" \
      -target="http://127.0.0.1:$OPCODE_PORT" -user="$OC_USER" -pass="$OC_PASS" -out="$out" >/dev/null )
  kill -9 "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -rf "$home"
  sleep 1
}

# load_live_key sources the out-of-repo env file (if any) and returns non-zero
# when no provider key is available, so the caller can SKIP cleanly.
load_live_key() {
  if [ -f "$LIVE_ENV" ]; then
    set -a; . "$LIVE_ENV"; set +a
  fi
  [ -n "${GOOGLE_GENERATIVE_AI_API_KEY:-}${GOOGLE_API_KEY:-}${GEMINI_API_KEY:-}" ]
}

case "$MODE" in
  self)
    echo "self-diff: opencode vs opencode (fresh state each run)"
    run_opencode_suite TestSuite "$RESULTS/opencode-1.json"
    run_opencode_suite TestSuite "$RESULTS/opencode-2.json"
    echo "=== diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" "$RESULTS/opencode-1.json" "$RESULTS/opencode-2.json"
    ;;
  dual)
    OPCODE_URL="${2:-http://127.0.0.1:$OPCODE_PORT}"
    echo "dual-run: opencode (truth) vs opcode42 at $OPCODE_URL"
    run_opencode_suite TestSuite "$RESULTS/opencode.json"
    ( cd "$REPO_ROOT" && go test ./conformance/ -run TestSuite \
        -target="$OPCODE_URL" -user="$OC_USER" -pass="$OC_PASS" -out="$RESULTS/opcode42.json" >/dev/null )
    echo "=== diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" "$RESULTS/opencode.json" "$RESULTS/opcode42.json"
    ;;
  live-self)
    if ! load_live_key; then
      echo "SKIP live-self: no provider key (set GOOGLE_GENERATIVE_AI_API_KEY or $LIVE_ENV)"; exit 0
    fi
    echo "live self-diff: opencode vs opencode (real model: google/gemini-2.5-flash)"
    run_opencode_suite TestLiveSuite "$RESULTS/live-opencode-1.json"
    run_opencode_suite TestLiveSuite "$RESULTS/live-opencode-2.json"
    echo "=== live diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" \
      -divergences "$REPO_ROOT/conformance/known-divergences-live.json" \
      "$RESULTS/live-opencode-1.json" "$RESULTS/live-opencode-2.json"
    ;;
  live)
    if ! load_live_key; then
      echo "SKIP live: no provider key (set GOOGLE_GENERATIVE_AI_API_KEY or $LIVE_ENV)"; exit 0
    fi
    echo "live dual-run: opencode (truth) vs opcode42 (real model: google/gemini-2.5-flash)"
    run_opencode_suite TestLiveSuite "$RESULTS/live-opencode.json"
    run_opcode42_suite TestLiveSuite "$RESULTS/live-opcode42.json"
    echo "=== live diff ==="
    go run "$REPO_ROOT/conformance/cmd/diff" \
      -divergences "$REPO_ROOT/conformance/known-divergences-live.json" \
      "$RESULTS/live-opencode.json" "$RESULTS/live-opcode42.json"
    ;;
  *)
    echo "usage: $0 [self|dual <opcode42URL>|live|live-self]" >&2
    exit 2
    ;;
esac
