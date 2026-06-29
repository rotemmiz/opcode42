#!/usr/bin/env bash
# Spec-drift gate (task C0 / plan 06 Phase 2). Compares the spec the Opcode42 daemon
# SELF-EMITS at GET /openapi.json against the frozen reference
# (conformance/openapi-reference.json).
#
# /openapi.json is derived from the daemon's registered route table (internal/api/
# spec.Emit), NOT served verbatim like /doc — so this gate has teeth: dropping a
# handler/route or adding an unspec'd one changes the emitted spec and trips here.
# (Fetching /doc instead would compare the frozen reference to itself.)
#
# Failure (exit 1) classes, per the locked conformance policy:
#   - an operation (method+path) in the reference is MISSING from Opcode42   -> FAIL
#   - a matched operation's set of response status codes changed          -> FAIL
#   - an EXTRA operation Opcode42 adds that is NOT in known-additions.json    -> FAIL
# Extra operations listed in known-additions.json are reported as WARN.
#
# Note: opencode serves its live spec at /doc (NOT /openapi.json) —
# server/routes/instance/httpapi/server.ts:162-167. Opcode42 serves the verbatim
# reference at /doc for parity, and additionally self-emits at /openapi.json (a
# known-addition) for this gate.
#
# Usage: scripts/check-spec-drift.sh [opcode42_url]   (default http://127.0.0.1:4096)
set -euo pipefail

OPCODE_URL="${1:-http://127.0.0.1:4096}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REFERENCE="$REPO_ROOT/conformance/openapi-reference.json"
ADDITIONS="$REPO_ROOT/conformance/known-additions.json"
OPCODE_DOC="$(mktemp)"
trap 'rm -f "$OPCODE_DOC"' EXIT

if [[ ! -f "$REFERENCE" ]]; then
  echo "error: reference not found at $REFERENCE (run scripts/sync-openapi.sh)" >&2
  exit 2
fi

if ! curl -fsS "$OPCODE_URL/openapi.json" -o "$OPCODE_DOC"; then
  echo "error: could not fetch $OPCODE_URL/openapi.json — is opcoded running?" >&2
  exit 2
fi

python3 - "$REFERENCE" "$OPCODE_DOC" "$ADDITIONS" <<'PY'
import json, sys

reference, opcode42, additions_path = sys.argv[1], sys.argv[2], sys.argv[3]

def ops(path):
    spec = json.load(open(path))
    out = {}
    for p, item in spec.get("paths", {}).items():
        for method, op in item.items():
            if method.lower() in ("get", "post", "put", "delete", "patch"):
                out[(method.upper(), p)] = set((op.get("responses") or {}).keys())
    return out

ref = ops(reference)
fge = ops(opcode42)
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
        warnings.append(f"known-addition {key[0]} {key[1]}")
        continue
    breaking.append(f"EXTRA operation not in reference or known-additions: {key[0]} {key[1]}")

for w in warnings:
    print("WARN:", w)
for b in breaking:
    print("BREAKING:", b)

print(f"\nspec-drift: {len(ref)} reference operations, {len(fge)} opcode42 operations, "
      f"{len(breaking)} breaking, {len(warnings)} warning(s).")
sys.exit(1 if breaking else 0)
PY