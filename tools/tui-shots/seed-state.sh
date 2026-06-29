#!/usr/bin/env bash
# seed-state.sh <mode>
# Pins the opcode-tui theme (via tui-kv.json) and sets up opencode's config with
# the fixture session imported. Must be run with env.sh already sourced.
#
# mode: "dark" → opcode42-dark theme
#       "light" → opcode42-light theme
set -euo pipefail
MODE="${1:-dark}"

case "$MODE" in
  dark)  THEME="opcode42-dark" ;;
  light) THEME="opcode42-light" ;;
  *)     echo "seed-state.sh: unknown mode '$MODE' (use dark or light)" >&2; exit 1 ;;
esac

# ── opcode-tui KV (theme pin) ────────────────────────────────────────────────
mkdir -p "$OPCODE_KV_DIR"
cat > "$OPCODE_KV_DIR/tui-kv.json" <<EOF
{
  "theme": "$THEME"
}
EOF

echo "seeded mode=$MODE theme=$THEME"
