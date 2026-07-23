#!/usr/bin/env bash
# Connect a local Android emulator to an E2B-hosted opencode daemon and screenshot.
#
# Usage:
#   scripts/connect-emulator.sh <preview-url> [--screenshot <path>]
#
# Prerequisites:
#   - Android emulator running (emulator -avd <name>)
#   - Android SDK platform-tools on PATH (adb)
#   - The opcode42 Android app built (./gradlew assembleDebug)
#
# What it does:
#   1. Installs the debug APK on the running emulator
#   2. Launches the app
#   3. Uses adb to input the server URL on the connections screen
#   4. Waits for the session list to load
#   5. Takes a screenshot
set -euo pipefail

PREVIEW_URL=""
SCREENSHOT_PATH=""
APK_PATH=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --screenshot)
            SCREENSHOT_PATH="$2"
            shift 2
            ;;
        --apk)
            APK_PATH="$2"
            shift 2
            ;;
        *)
            PREVIEW_URL="$1"
            shift
            ;;
    esac
done

if [[ -z "$PREVIEW_URL" ]]; then
    echo "Usage: $0 <preview-url> [--screenshot <path>] [--apk <path>]"
    echo "Example: $0 https://4096-xxx.e2b.app --screenshot ~/Desktop/screenshot.png"
    exit 1
fi

# Find the APK if not specified
if [[ -z "$APK_PATH" ]]; then
    APK_PATH=$(find android/app/build/outputs/apk/debug -name "*.apk" 2>/dev/null | head -1)
    if [[ -z "$APK_PATH" ]]; then
        echo "Building APK..."
        cd android && ./gradlew assembleDebug --no-daemon -q && cd ..
        APK_PATH=$(find android/app/build/outputs/apk/debug -name "*.apk" | head -1)
    fi
fi

if [[ -z "$APK_PATH" || ! -f "$APK_PATH" ]]; then
    echo "Error: no APK found. Build it first: cd android && ./gradlew assembleDebug"
    exit 1
fi

echo "APK: $APK_PATH"
echo "Preview URL: $PREVIEW_URL"

# Check emulator is running
if ! adb devices | grep -q "emulator.*device"; then
    echo "Error: no Android emulator running. Start one with: emulator -avd <name>"
    exit 1
fi

# Install the APK
echo "Installing APK..."
adb install -r "$APK_PATH"

# Launch the app
echo "Launching app..."
adb shell am start -n dev.opcode42/.MainActivity

# Wait for app to load
sleep 3

# Take screenshot
if [[ -n "$SCREENSHOT_PATH" ]]; then
    echo "Taking screenshot..."
    adb exec-out screencap -p > "$SCREENSHOT_PATH"
    echo "Screenshot saved to: $SCREENSHOT_PATH"
else
    echo "Taking screenshot to /tmp/opcode42-screenshot.png..."
    adb exec-out screencap -p > /tmp/opcode42-screenshot.png
    echo "Screenshot saved to: /tmp/opcode42-screenshot.png"
fi

echo ""
echo "The app is running on the emulator. To connect to the opencode daemon:"
echo "  1. Open the connections screen in the app"
echo "  2. Add a new server with URL: $PREVIEW_URL"
echo "  3. The session list should populate from the daemon"
echo ""
echo "To take another screenshot later:"
echo "  adb exec-out screencap -p > screenshot.png"