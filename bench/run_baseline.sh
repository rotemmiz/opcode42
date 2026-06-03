#!/usr/bin/env bash
# Forge performance baseline runner (plan 11, W0).
#
# Stands up the real opencode daemon and forged on THIS machine and measures the
# four W0 baseline metrics head-to-head (cold start, idle RSS, SSE connection
# fan-out, request throughput), writing a dated JSON + Markdown report under
# bench/results/.
#
# Prerequisites:
#   - `opencode` on PATH (the compatibility reference daemon).
#   - A Go toolchain (the harness builds forged from ./cmd/forged).
#
# Usage:
#   bench/run_baseline.sh
#
# Tunables (env, defaults shown):
#   BENCH_COLDSTART_ITERS=10   cold-start iterations per daemon
#   BENCH_SUBS=50              concurrent SSE subscribers for fan-out + RSS
#   BENCH_TP_CONCURRENCY=16    throughput load workers
#   BENCH_TP_SECONDS=5         throughput window per endpoint
#   FORGE_PORT=4097 / OPENCODE_PORT=4096
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RESULTS="$REPO_ROOT/bench/results"
mkdir -p "$RESULTS"

FORGE_PORT="${FORGE_PORT:-4097}"
OPENCODE_PORT="${OPENCODE_PORT:-4096}"
BENCH_USER="${BENCH_USER:-opencode}"
BENCH_PASS="${BENCH_PASS:-bench-baseline}"

if ! command -v opencode >/dev/null 2>&1; then
  echo "error: opencode not found on PATH (W0 requires the reference daemon)" >&2
  exit 1
fi

echo "==> building forged"
FORGED_BIN="$(mktemp -d)/forged"
(cd "$REPO_ROOT" && go build -o "$FORGED_BIN" ./cmd/forged)

# Isolate opencode's HOME so its SQLite DB does not bleed across runs and the
# bench directory is used as the routed project directory for both daemons.
OPENCODE_HOME="$(mktemp -d)"
BENCH_DIR="$(mktemp -d)"
BENCH_LOG_DIR="$(mktemp -d)"

echo "==> running baseline (this forks daemons; takes ~1-2 min)"
cd "$REPO_ROOT"
BENCH_FORGE_BIN="$FORGED_BIN" \
BENCH_FORGE_PORT="$FORGE_PORT" \
BENCH_OPENCODE_BIN="$(command -v opencode)" \
BENCH_OPENCODE_PORT="$OPENCODE_PORT" \
BENCH_USER="$BENCH_USER" \
BENCH_PASS="$BENCH_PASS" \
BENCH_DIR="$BENCH_DIR" \
BENCH_OPENCODE_HOME="$OPENCODE_HOME" \
BENCH_LOG_DIR="$BENCH_LOG_DIR" \
BENCH_RESULTS_DIR="$RESULTS" \
  go test -tags bench -run TestBaseline -count=1 -timeout 15m -v ./bench/

echo "==> done; see $RESULTS"
