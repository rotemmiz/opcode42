#!/usr/bin/env bash
# env.sh — isolate forge-tui's config/state so the real ~/Library/Application Support/forge
# is never touched, and opencode's XDG dirs are sandboxed to state/ as well.
#
# Source this before running tapes or capture.sh:
#   source ./env.sh
#
# Two isolation layers:
#   1. HOME override: forge-tui uses os.UserConfigDir() → ~/Library/Application Support on macOS.
#      We point HOME at state/fakehome/ so the KV file lands at
#      state/fakehome/Library/Application Support/forge/tui-kv.json.
#   2. XDG override: opencode (the daemon we attach to) uses XDG dirs.
#      We point them at state/ so opencode's DB, sessions, and config never
#      touch ~/.local/share/opencode.

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"

# opencode daemon isolation (XDG)
export XDG_DATA_HOME="$HARNESS_DIR/state/data"
export XDG_CONFIG_HOME="$HARNESS_DIR/state/config"
export XDG_CACHE_HOME="$HARNESS_DIR/state/cache"
export XDG_STATE_HOME="$HARNESS_DIR/state/xstate"

# forge-tui isolation: override HOME so os.UserConfigDir() → sandboxed dir on macOS
FAKE_HOME="$HARNESS_DIR/state/fakehome"
export HOME="$FAKE_HOME"
export FORGE_TUI_HOME="$FAKE_HOME"   # informational; not used by code

# forge-tui KV path (for reference and direct writes in seed-state.sh)
export FORGE_KV_DIR="$FAKE_HOME/Library/Application Support/forge"
export HARNESS_DIR
