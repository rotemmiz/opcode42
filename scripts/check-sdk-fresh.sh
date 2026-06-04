#!/usr/bin/env bash
# SDK freshness gate (plan 06 M8). Regenerates the Kotlin + Swift SDKs into a
# throwaway tree and asserts they match what's committed under sdk/{kotlin,swift}/gen
# — the same "generated output is pinned to the spec" guarantee the Go codegen has
# (`make gen` + `git diff --exit-code internal/api/gen/`).
#
# This catches: a hand-edit to generated sources, a spec change that wasn't
# re-generated, or a generator/version drift. Run it after `scripts/sync-openapi.sh`
# bumps the contract to prove the SDKs were regenerated.
#
# Skips (exit 0 with a notice) when the Java toolchain is unavailable, so the gate
# is a no-op on machines/CI lanes without a JDK rather than a hard failure. The CI
# job that enforces it provisions Java explicitly.
#
# Usage: scripts/check-sdk-fresh.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if ! command -v java >/dev/null 2>&1; then
  echo "check-sdk-fresh: java not found — skipping SDK freshness check." >&2
  exit 0
fi

TMP="$(mktemp -d -t forge-sdk-fresh.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

echo "check-sdk-fresh: regenerating SDKs into a scratch tree ..." >&2
OUT_DIR="$TMP" "$REPO_ROOT/scripts/gen-sdks.sh"

status=0
for lang in kotlin swift; do
  committed="$REPO_ROOT/sdk/$lang/gen"
  fresh="$TMP/sdk/$lang/gen"
  if ! diff -r "$committed" "$fresh" >/dev/null 2>&1; then
    echo "::error::sdk/$lang/gen is stale — run 'make gen-sdks' and commit the result." >&2
    diff -r "$committed" "$fresh" | head -40 >&2 || true
    status=1
  else
    echo "check-sdk-fresh: sdk/$lang/gen is up to date." >&2
  fi
done

exit $status
