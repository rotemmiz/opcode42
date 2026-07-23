#!/usr/bin/env bash
# Screenshot the opencode web UI using headless Chromium.
#
# Usage: screenshot-opencode.sh <port> <output.png>
#
# Takes a full-page screenshot of the opencode session view at
# http://127.0.0.1:<port> and saves it as <output.png>.
set -euo pipefail

PORT="$1"
OUTPUT="$2"

chromium-browser \
    --headless \
    --no-sandbox \
    --disable-gpu \
    --screenshot="$OUTPUT" \
    --window-size=1280,900 \
    "http://127.0.0.1:$PORT" 2>/dev/null

if [[ -f "$OUTPUT" ]]; then
    echo "Screenshot saved to $OUTPUT ($(stat -c %s "$OUTPUT") bytes)"
else
    echo "Screenshot failed"
    exit 1
fi