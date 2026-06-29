#!/usr/bin/env bash
# SDK freshness + compile gate (plan 06 M8/M9). Regenerates the Kotlin + Swift SDKs
# into a throwaway tree and asserts they match what's committed under
# sdk/{kotlin,swift}/gen — the same "generated output is pinned to the spec"
# guarantee the Go codegen has (`make gen` + `git diff --exit-code internal/api/gen/`).
# It then compile-gates the Swift SDK (`swift build`) when a Swift toolchain is
# present, proving the committed output actually compiles.
#
# This catches: a hand-edit to generated sources, a spec change that wasn't
# re-generated, a generator/version drift, or a spec shape the generator renders
# into non-compiling Swift. Run it after `scripts/sync-openapi.sh` bumps the
# contract to prove the SDKs were regenerated.
#
# Skips (exit 0 with a notice) when the Java toolchain is unavailable, so the gate
# is a no-op on machines/CI lanes without a JDK rather than a hard failure. The
# Swift compile step likewise skip-gates when `swift` is absent (e.g. CI without a
# Swift toolchain) — but the freshness diff above still asserts the committed Swift
# tree matches the generated output regardless. The CI job that enforces this
# provisions Java (and Swift) explicitly.
#
# Usage: scripts/check-sdk-fresh.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if ! command -v java >/dev/null 2>&1; then
  echo "check-sdk-fresh: java not found — skipping SDK freshness check." >&2
  exit 0
fi

TMP="$(mktemp -d -t opcode42-sdk-fresh.XXXXXX)"
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

# --- compile-gate the Swift SDK ---------------------------------------------
# Build the FRESHLY-generated scratch tree (never the committed tree) so the
# build's .build/ artifacts can't pollute `git diff` against sdk/swift/gen. If
# the freshness diff above already proved the trees match, this also proves the
# committed Swift compiles. Skip-gate when no Swift toolchain is present.
if command -v swift >/dev/null 2>&1; then
  echo "check-sdk-fresh: compiling the Swift SDK ($(swift --version 2>/dev/null | head -1)) ..." >&2
  if ! (cd "$TMP/sdk/swift/gen" && swift build >/dev/null); then
    echo "::error::sdk/swift/gen does not compile (swift build failed)." >&2
    (cd "$TMP/sdk/swift/gen" && swift build 2>&1 | tail -30) >&2 || true
    status=1
  else
    echo "check-sdk-fresh: Swift SDK compiles." >&2
  fi
else
  echo "check-sdk-fresh: swift not found — skipping Swift compile gate (freshness diff still enforced)." >&2
fi

exit $status
