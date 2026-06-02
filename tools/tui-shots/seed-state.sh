#!/usr/bin/env bash
# seed-state.sh <mode>
# Pins the forge-tui theme (via tui-kv.json) and sets up opencode's config with
# the fixture session imported. Must be run with env.sh already sourced.
#
# mode: "dark" → forge-dark theme
#       "light" → forge-light theme
set -euo pipefail
MODE="${1:-dark}"

case "$MODE" in
  dark)  THEME="forge-dark" ;;
  light) THEME="forge-light" ;;
  *)     echo "seed-state.sh: unknown mode '$MODE' (use dark or light)" >&2; exit 1 ;;
esac

# ── forge-tui KV (theme pin) ────────────────────────────────────────────────
mkdir -p "$FORGE_KV_DIR"
cat > "$FORGE_KV_DIR/tui-kv.json" <<EOF
{
  "theme": "$THEME"
}
EOF

echo "seeded mode=$MODE theme=$THEME"
