#!/usr/bin/env bash
# capture.sh [mode...]
#   mode: one or more of "dark" "light" (default: both)
#
# For each mode: pins theme, ensures opencode daemon + fixture session are up,
# builds forge-tui, runs every tape in tapes/, and collects PNGs into
# out/forge-<mode>/. The forge-tui attaches to a local opencode daemon seeded
# with the same fixture-session.json that opencode's reference harness uses,
# enabling honest 1:1 visual comparison (same content, two front-ends).
#
# Usage:
#   ./capture.sh                # dark + light
#   ./capture.sh dark           # dark only
#   ./capture.sh dark light     # explicit
#
# Requirements: vhs, ttyd, ffmpeg, opencode (≥1.15.12), go on PATH.
set -euo pipefail
cd "$(dirname "$0")"
source ./env.sh

MODES=("$@")
[ ${#MODES[@]} -eq 0 ] && MODES=(dark light)

DAEMON_PORT=4199
DAEMON_URL="http://127.0.0.1:$DAEMON_PORT"
SESSION_ID="ses_00000000000000000000000000"
REPO_ROOT="$(cd ../.. && pwd)"

# ── 1. Build forge-tui ───────────────────────────────────────────────────────
echo "▶ building forge-tui …"
mkdir -p bin
go build -o bin/forge-tui "$REPO_ROOT/cmd/forge-tui"
echo "  ✓ bin/forge-tui built"

# ── 2. Ensure opencode daemon + fixture session ──────────────────────────────
# Import the fixture session into the isolated opencode state (idempotent).
# opencode serve will pick up the session from its DB.
ensure_fixture() {
  mkdir -p "$XDG_DATA_HOME/opencode"

  # Check if the session is already in the DB (fast path).
  local existing
  existing=$( cd "$HARNESS_DIR/fixture" && opencode session list 2>/dev/null | grep -c "$SESSION_ID" || true )
  if [ "$existing" -gt 0 ]; then
    echo "  ✓ fixture session already imported"
    return
  fi

  echo "  · importing fixture session …"
  # opencode import re-points projectID/directory to the fixture/ dir.
  ( cd "$HARNESS_DIR/fixture" && opencode import "$HARNESS_DIR/fixture-session.json" >/dev/null )
  echo "  ✓ fixture session imported (id=$SESSION_ID)"
}

start_daemon() {
  # Kill any stale daemon on our port.
  lsof -ti tcp:"$DAEMON_PORT" | xargs kill -9 2>/dev/null || true
  sleep 0.5

  echo "  · starting opencode daemon on port $DAEMON_PORT …"
  # Run from within fixture/ directory (opencode serve uses cwd as the project dir).
  ( cd "$HARNESS_DIR/fixture" && opencode serve --port "$DAEMON_PORT" >/dev/null 2>&1 ) &

  # Wait for the daemon to be ready (up to 20s).
  local deadline=$((SECONDS + 20))
  while [ $SECONDS -lt $deadline ]; do
    if curl -s "$DAEMON_URL/global/health" >/dev/null 2>&1; then
      echo "  ✓ daemon ready at $DAEMON_URL"
      return
    fi
    sleep 0.5
  done
  echo "  ! daemon did not become ready in time" >&2
  exit 1
}

echo "▶ setting up opencode daemon + fixture …"
ensure_fixture
start_daemon

# ── 3. Capture each mode ─────────────────────────────────────────────────────
for MODE in "${MODES[@]}"; do
  echo "▶ capturing mode=$MODE"
  bash seed-state.sh "$MODE" >/dev/null

  # Export FORGE_THEME so VHS-spawned bash sessions can use $FORGE_THEME in Type commands.
  case "$MODE" in
    dark)  export FORGE_THEME="forge-dark" ;;
    light) export FORGE_THEME="forge-light" ;;
  esac

  rm -f out/*.png out/_*.gif

  for tape in tapes/[0-9]*.tape; do
    echo "  · $(basename "$tape")"
    vhs "$tape" >/dev/null 2>&1 || echo "    ! $(basename "$tape") failed (non-fatal)"
  done

  DEST="out/forge-${MODE}"
  mkdir -p "$DEST"
  rm -f "$DEST"/*.png
  mv out/*.png "$DEST"/ 2>/dev/null || true
  rm -f out/_*.gif
  echo "  → $(ls "$DEST"/*.png 2>/dev/null | wc -l | tr -d ' ') shots in $DEST/"
done

# ── 4. Shut down daemon ───────────────────────────────────────────────────────
lsof -ti tcp:"$DAEMON_PORT" | xargs kill -9 2>/dev/null || true
echo "done."
