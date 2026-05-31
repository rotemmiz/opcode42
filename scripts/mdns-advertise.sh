#!/usr/bin/env bash
# Advertise a running opencode daemon over mDNS so the Android app can auto-discover it.
#
# opencode does NOT advertise itself yet (plan 07), so this sidecar publishes an
# `_opencode._tcp` record next to it — zero changes to opencode. Keep it running; the
# record disappears when this process exits.
#
# Usage:
#   scripts/mdns-advertise.sh [PORT] [NAME]
#     PORT  daemon port            (default 4096)
#     NAME  advertised service name (default "opencode")
#
# Env (folded into TXT records):
#   FORGE_MDNS_PATH       base path                       (default /)
#   FORGE_MDNS_VERSION    version string                  (default dev)
#   FORGE_MDNS_AUTH       none|basic|token                (default none)
#   FORGE_MDNS_DIRECTORY  suggested x-opencode-directory  (default unset)
#
# Requires: macOS `dns-sd` (built in) or Linux `avahi-publish-service` (avahi-utils).
set -euo pipefail

PORT="${1:-4096}"
NAME="${2:-opencode}"
TYPE="_opencode._tcp"

PATH_TXT="path=${FORGE_MDNS_PATH:-/}"
VERSION_TXT="version=${FORGE_MDNS_VERSION:-dev}"
AUTH_TXT="auth=${FORGE_MDNS_AUTH:-none}"

txt_records=("$PATH_TXT" "$VERSION_TXT" "$AUTH_TXT")
[ -n "${FORGE_MDNS_DIRECTORY:-}" ] && txt_records+=("directory=${FORGE_MDNS_DIRECTORY}")

echo "Advertising '$NAME' $TYPE on port $PORT  [${txt_records[*]}]"
echo "Press Ctrl-C to stop."

if command -v dns-sd >/dev/null 2>&1; then
    # macOS Bonjour. -R "register"; TXT records are trailing key=value args.
    exec dns-sd -R "$NAME" "$TYPE" local "$PORT" "${txt_records[@]}"
elif command -v avahi-publish-service >/dev/null 2>&1; then
    # Linux avahi. Service type wants no trailing dot here.
    exec avahi-publish-service "$NAME" "$TYPE" "$PORT" "${txt_records[@]}"
else
    echo "error: need 'dns-sd' (macOS) or 'avahi-publish-service' (Linux, install avahi-utils)." >&2
    echo "       For CI/containers use a zeroconf sidecar instead (see plan 07)." >&2
    exit 1
fi
