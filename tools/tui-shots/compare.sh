#!/usr/bin/env bash
# compare.sh [mode]
# Side-by-side visual comparison of forge vs opencode reference screenshots.
#
# For each scene that exists in both out/forge-<mode>/ and the opencode reference
# (../../screenshots-harness/out/opencode-<mode>/), creates a montage image in
# out/compare-<mode>/ showing them side by side.
#
# Requires ImageMagick (montage / convert) or ffmpeg. Falls back to a simple
# file-path listing if neither is available.
#
# Usage:
#   ./compare.sh           # compare dark mode
#   ./compare.sh light     # compare light mode
set -euo pipefail
cd "$(dirname "$0")"

MODE="${1:-dark}"
FORGE_DIR="out/forge-${MODE}"
OC_DIR="../../screenshots-harness/out/opencode-${MODE}"
DEST="out/compare-${MODE}"

if [ ! -d "$FORGE_DIR" ]; then
  echo "No forge captures at $FORGE_DIR — run ./capture.sh $MODE first." >&2
  exit 1
fi
if [ ! -d "$OC_DIR" ]; then
  echo "No opencode reference at $OC_DIR — run opencode's capture.sh first." >&2
  exit 1
fi

mkdir -p "$DEST"

# Find matching scenes.
matched=0
unmatched_forge=()
for forge_png in "$FORGE_DIR"/*.png; do
  scene=$(basename "$forge_png")
  oc_png="$OC_DIR/$scene"
  if [ -f "$oc_png" ]; then
    matched=$((matched + 1))
    if command -v montage >/dev/null 2>&1; then
      montage -title "forge vs opencode: $scene" \
        -label "forge-$MODE" "$forge_png" \
        -label "opencode-$MODE" "$oc_png" \
        -geometry +4+4 -tile 2x1 \
        "$DEST/$scene" 2>/dev/null && echo "  ✓ $scene"
    elif command -v ffmpeg >/dev/null 2>&1; then
      # ffmpeg hstack for side-by-side
      ffmpeg -y -loglevel quiet \
        -i "$forge_png" -i "$oc_png" \
        -filter_complex "[0:v]scale=800:-1[l];[1:v]scale=800:-1[r];[l][r]hstack=inputs=2" \
        "$DEST/$scene" 2>/dev/null && echo "  ✓ $scene (ffmpeg)"
    else
      echo "  pair: $forge_png  vs  $oc_png"
    fi
  else
    unmatched_forge+=("$scene")
  fi
done

echo ""
echo "Matched $matched scenes."
if [ ${#unmatched_forge[@]} -gt 0 ]; then
  echo "Forge-only (no opencode reference): ${unmatched_forge[*]}"
fi

# List opencode scenes not yet in forge.
for oc_png in "$OC_DIR"/*.png; do
  scene=$(basename "$oc_png")
  if [ ! -f "$FORGE_DIR/$scene" ]; then
    echo "Opencode-only (not yet captured for forge): $scene"
  fi
done

if command -v montage >/dev/null 2>&1 || command -v ffmpeg >/dev/null 2>&1; then
  echo "Side-by-side images in $DEST/"
else
  echo "Install ImageMagick (montage) or ffmpeg for side-by-side images."
fi
