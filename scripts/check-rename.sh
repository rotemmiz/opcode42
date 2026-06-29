#!/usr/bin/env bash
# Acceptance gate for the Forge -> Opcode42 rename (plans/14-rename-opcode42.md §2.7).
# Fails if the rename is incomplete, over-reached into opencode wire-compat, or the
# new names are missing. Wired into CI (.github/workflows/ci.yml) so it can't regress.
set -euo pipefail
fail=0

# --- Legitimately-remaining "forge", by design -----------------------------
# (0) Meta-files that necessarily name both the old and new brand: the rename
#     plan (before -> after tables) and this gate script itself.
META=(':!plans/14-rename-opcode42.md' ':!scripts/check-rename.sh')
# (2) decision-3 SEAM: the GitHub repo is intentionally NOT renamed yet, so the
#     clone URL / container registry / goreleaser release name stay "forge" until
#     `gh repo rename` (plans/14 §3). These are the ONLY allowed "forge" lines.
# (3) English word "forget" / "fire-and-forget" contains the substring "forge".
# The github.com term is anchored to NOT match a trailing "/path", so a genuinely
# broken module import (github.com/rotemmiz/forge/internal/...) is NOT masked by
# the clone-URL seam and still trips the gate.
SEAM='github\.com/rotemmiz/forge([^/]|$)|ghcr\.io/rotemmiz/forge|^[^:]*:[0-9]+:[[:space:]]*name: forge|forget'

# --- (1) NEGATIVE: no stray "forge" outside meta-files / seam / "forget" -----
stray="$(git grep -in -e forge -- . "${META[@]}" | grep -viE "$SEAM" || true)"
if [[ -n "$stray" ]]; then
  echo "X stray 'forge' remains:"; echo "$stray" | sed 's/^/    /'
  echo "  -> rename it, or (if a new repo-seam line) extend SEAM in this script."; fail=1
else echo "OK no stray 'forge' (only plan doc + repo seam + English 'forget')"; fi

# --- (1b) PATH: no tracked file path contains "forge" ------------------------
if git ls-files | grep -qi forge; then
  echo "X a tracked PATH still contains 'forge':"; git ls-files | grep -i forge | sed 's/^/    /'; fail=1
else echo "OK no tracked path contains 'forge'"; fi

# --- (2) WIRE-COMPAT: opencode identifiers untouched -------------------------
for s in 'x-opencode-directory' '_opencode._tcp' 'openapi-reference.json'; do
  git grep -q -F "$s" -- . || { echo "X protected opencode string vanished: $s"; fail=1; }
done
# Count over code/docs but NOT the meta-files (the plan + this script mention
# "opencode" many times by nature; excluding them keeps the tripwire stable
# against edits to the rename's own documentation).
base="$(cat .rename-opencode-count 2>/dev/null || echo '')"
now="$(git grep -I -i -o -e opencode -- . "${META[@]}" | wc -l | tr -d ' ')"
if [[ -z "$base" ]]; then
  echo "X .rename-opencode-count baseline missing — cannot verify the opencode wire-count tripwire"; fail=1
elif [[ "$now" != "$base" ]]; then
  echo "X opencode token count changed: $base -> $now (did a sweep hit opencode?)"; fail=1
else echo "OK opencode wire identifiers intact ($now)"; fi

# --- (3) POSITIVE: the new Opcode42 names actually landed --------------------
for s in 'github.com/rotemmiz/opcode42' 'dev.opcode42' 'OPCODE_'; do
  git grep -q -F "$s" -- . || { echo "X expected new name missing: $s"; fail=1; }
done
for d in cmd/opcoded cmd/opcode-tui; do
  test -d "$d" || { echo "X expected dir missing: $d"; fail=1; }
done
[[ $fail == 0 ]] && echo "OK new Opcode42 names present"

[[ $fail == 0 ]] && echo "== rename gate PASSED ==" || echo "== rename gate FAILED =="
exit $fail
