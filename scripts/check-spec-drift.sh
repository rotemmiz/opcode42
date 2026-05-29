#!/usr/bin/env bash
# Spec-drift gate (task C0). Compares the spec the Forge daemon serves at GET /doc
# against the frozen reference (conformance/openapi-reference.json).
#
# A breaking difference fails the build:
#   - an operation (method+path) in the reference is MISSING from Forge
#   - a matched operation's set of response status codes changed
# Extra operations Forge adds are allowed only if listed in known-additions.json;
# otherwise they are reported as warnings.
#
# Note: opencode serves its live spec at /doc (NOT /openapi.json) —
# server/routes/instance/httpapi/server.ts:162-167. Forge matches that.
#
# Usage: scripts/check-spec-drift.sh [forge_url]   (default http://127.0.0.1:4096)
set -euo pipefail

FORGE_URL="${1:-http://127.0.0.1:4096}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO_ROOT/conformance/openapi-reference.json"
ADDITIONS="$REPO_ROOT/conformance/known-additions.json"
FORGE_DOC="$(mktemp)"
trap 'rm -f "$FORGE_DOC"' EXIT

if [[ ! -f "$REFERENCE" ]]; then
  echo "error: reference not found at $REFERENCE (run scripts/sync-openapi.sh)" >&2
  exit 2
fi

if ! curl -fsS "$FORGE_URL/doc" -o "$FORGE_DOC"; then
  echo "error: could not fetch $FORGE_URL/doc — is forged running?" >&2
  exit 2
fi

python3 - "$REFERENCE" "$FORGE_DOC" "$ADDITIONS" <<'PY'
import json, sys

reference, forge, additions_path = sys.argv[1], sys.argv[2], sys.argv[3]

def ops(path):
    spec = json.load(open(path))
    out = {}
    for p, item in spec.get("paths", {}).items():
        for method, op in item.items():
            if method.lower() in ("get", "post", "put", "delete", "patch"):
                out[(method.upper(), p)] = set((op.get("responses") or {}).keys())
    return out

ref = ops(reference)
fge = ops(forge)
try:
    additions = set(tuple(a) for a in json.load(open(additions_path)))
except Exception:
    additions = set()

breaking, warnings = [], []

for key, codes in ref.items():
    if key not in fge:
        breaking.append(f"MISSING operation {key[0]} {key[1]}")
        continue
    if codes != fge[key]:
        breaking.append(f"STATUS CODES changed for {key[0]} {key[1]}: {sorted(codes)} != {sorted(fge[key])}")

for key in fge.keys() - ref.keys():
    if key in additions:
        continue
    warnings.append(f"EXTRA operation not in reference or known-additions: {key[0]} {key[1]}")

for w in warnings:
    print("WARN:", w)
for b in breaking:
    print("BREAKING:", b)

print(f"\nspec-drift: {len(ref)} reference operations, {len(fge)} forge operations, "
      f"{len(breaking)} breaking, {len(warnings)} warning(s).")
sys.exit(1 if breaking else 0)
PY