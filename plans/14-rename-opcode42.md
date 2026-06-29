# Plan 14 — Rename: Forge → Opcode42

A complete, source-grounded rename of the project brand from **Forge** to **Opcode42**,
across every layer (Go daemon, Android app, generated SDKs, build/release, packaging, docs).

This is a **branding** rename only. It must not touch any **opencode wire-compat** identifier
(see "Do NOT touch" below). The two tokens `forge` and `opencode` are lexically distinct and never
overlap, which makes the sweep safe: we only ever rewrite `forge`, never `opencode`.

Scope baseline (measured 2026-06-29): **1063 tracked files**, **6221 occurrences** of `forge`
(case-insensitive) — `forge` ×3963, `Forge` ×2089, `FORGE` ×169. Of the ~1288 forge-named *paths*,
**1159 are generated SDK output** (`sdk/kotlin/gen`, `sdk/swift/gen`) — renamed by changing two
generator variables and regenerating, **not** by hand.

---

## 0. Naming convention (the contract)

The rename deliberately uses **two families**. Document this in `CLAUDE.md` so future contributors
don't "fix" the inconsistency.

### Brand family — keeps the `42` (code namespaces & identifiers)

| Context | Old | New |
| --- | --- | --- |
| Display / brand name | `Forge` | `Opcode42` |
| Go module path | `github.com/rotemmiz/forge` | `github.com/rotemmiz/opcode42` |
| Go exported identifiers | `Forge*` | `Opcode42*` (`ForgeClient`→`Opcode42Client`, `ForgeFile`→`Opcode42File`) |
| Go SDK package / file | `forgeclient` / `forgeclient.go` | `opcode42client` / `opcode42client.go` |
| Android source package | `dev.forge.*` | `dev.opcode42.*` |
| Android `applicationId` | `dev.forge.app` | `dev.opcode42.app` |
| Kotlin SDK package | `dev.forge.sdk` | `dev.opcode42.sdk` |
| Swift project | `ForgeClient` | `Opcode42Client` |
| npm scope / dir | `@forge/plugin-host`, `packages/forge-plugin-host` | `@opcode42/plugin-host`, `packages/opcode42-plugin-host` |
| Emitted-spec op-id prefix | `forgeAddition_` | `opcode42Addition_` |
| Private config/state dir | `.forge` | `.opcode42` (keeps `42` to stay visually distinct from opencode's `.opencode`) |
| HTTP `User-Agent` | `forge/0.0.1`, `forge` | `opcode42/0.0.1`, `opcode42` |
| mDNS host/domain default | `forge.local` | `opcode42.local` |
| System prompt brand token | `Forge` | `Opcode42` |

### Ops family — drops the `42` (CLI ergonomics)

| Context | Old | New |
| --- | --- | --- |
| Daemon binary / `cmd/` dir | `forged`, `cmd/forged` | `opcoded`, `cmd/opcoded` |
| TUI binary / `cmd/` dir | `forge-tui`, `cmd/forge-tui` | `opcode-tui`, `cmd/opcode-tui` |
| Env var prefix (~40 vars) | `FORGE_` | `OPCODE_` |

### Do NOT touch — opencode wire-compat (branding stops here)

- HTTP headers: `x-opencode-directory` (×84), `x-opencode-ticket` (×5), `x-opencode-workspace` (×4).
- `conformance/openapi-reference.json` + `.provenance.txt` — opencode's **frozen contract** (the SDK source spec).
- mDNS **wire strings**: `_http._tcp`, `_opencode._tcp`, instance name `opencode-<port>`
  (`internal/mdns/mdns.go:19-23`). Only the **Go variable name** `forgeServiceType` → `opcode42ServiceType` changes; the string value `"_opencode._tcp"` stays.
- SSE envelope `{ id, type, properties }`, PTY framing (`0x00` + UTF-8 JSON `{cursor}`).
- Every literal `opencode` token, ecosystem paths (`.opencode/agent`, `AGENTS.md`), opencode config keys, `FORGE_LIVE_OPENCODE` keeps its `OPENCODE` *suffix* (only the prefix flips → `OPCODE_LIVE_OPENCODE`).

---

## 1. Ordered replacement recipe (why order matters)

Lowercase `forge` has **two** targets, so a blanket `s/forge/.../` is wrong. Apply these as
**ordered, anchored** sweeps — specific compound strings first, generic word last — excluding
`.git/`, `conformance/openapi-reference.json*`, and `go.sum`:

1. `github.com/rotemmiz/forge` → `github.com/rotemmiz/opcode42`  (module path; .go, scripts, `//go:generate`)
2. `dev.forge` → `dev.opcode42`  (Android + Kotlin SDK package)
3. `ForgeClient` → `Opcode42Client`; `ForgeFile` → `Opcode42File`; `ForgeApp`/`ForgeTheme`/`ForgeNavGraph`/`ForgeMessagingService` → `Opcode42*`
4. `@forge/` → `@opcode42/`  (npm scope)
5. `forgeAddition_` → `opcode42Addition_`; `forgeServiceType` → `opcode42ServiceType`
6. `\bforge-tui\b` → `opcode-tui`; `\bforged\b` → `opcoded`  (binaries — **word-boundary anchored**)
7. `FORGE_` → `OPCODE_`  (env prefix)
8. `\.forge\b` (config dir) → `.opcode42`  — **must not match `dev.forge`** (already handled in step 2, so the bare-dot form is now unambiguous)
9. Remaining standalone `Forge` → `Opcode42`, `forge` → `opcode42`  (prose, comments, `User-Agent`, `forge.local`, `app_name`, `rootProject.name = "forge-android"` → `"opcode42-android"`)

After each phase, re-run `git grep -in '\bforge'` and eyeball that only intentional residue remains.

---

## 2. Execution phases

> Git workflow per `CLAUDE.md`: branch `rename-opcode42` off fresh `main`; full local gate; separate
> review subagent; CI green; squash-merge. The rename is one feature/PR.

### Phase 1 — Go: module path, identifiers, env, config dir
- `go mod edit -module github.com/rotemmiz/opcode42`; sweep step 1 over all `*.go` + `//go:generate` + scripts.
- `git mv cmd/forged cmd/opcoded`; `git mv cmd/forge-tui cmd/opcode-tui`; fix `binary`/`main` refs.
- `git mv sdk/go/forgeclient.go sdk/go/opcode42client.go` (+ `_test`); `package forgeclient`→`opcode42client`; type/identifier sweeps (steps 3,5).
- Env sweep (step 7); config-dir sweep (step 8) incl. `internal/engine/tool/grep.go:21`, `internal/server/find_handlers.go:24` (`skipDirs`), `internal/engine/catalog/source.go` cache path `"forge"`.
- System prompt + `User-Agent` strings (`internal/engine/catalog/source.go:105`, `internal/engine/tool/webfetch.go:55`, `internal/oauth/provider_xai.go:164`), emitted-spec text (`internal/api/spec/*.go`), mDNS var name.
- **Gate:** `go build ./... && go vet ./... && gofmt -l (empty) && golangci-lint run && go test ./...`.
- **Regen Go:** `make gen` then `git diff --exit-code internal/api/gen/` (the `downconvert` tool's import path moved). Update any golden/snapshot tests asserting `forgeAddition_`, the system-prompt `"Forge"` check (`internal/engine/registry/registry_test.go:203-206`), and the openapi emit tests.

### Phase 2 — Generated SDKs (regenerate, don't hand-edit)
- `scripts/gen-sdks.sh`: `KOTLIN_PACKAGE="dev.forge.sdk"`→`dev.opcode42.sdk`; `SWIFT_PROJECT="ForgeClient"`→`Opcode42Client`; `--model-name-mappings 'File=ForgeFile'`→`File=Opcode42File`; the `go run github.com/rotemmiz/forge/internal/tools/downconvert` path; cache dir `~/.cache/forge-sdk`→`opcode42-sdk`.
- **Prereq:** Java ≥11 (+ Go); Swift toolchain for the compile-gate. Must run on a machine that has them — otherwise `check-sdk-fresh` will diff.
- `make gen-sdks` (the script `rm -rf`s the out dirs, so the stale `dev/forge` tree is removed and a fresh `dev/opcode42` tree + `Sources/Opcode42Client/` is produced). Then `scripts/check-sdk-fresh.sh` (and `make check-sdk-fresh`) green.

### Phase 3 — Android app (hand source)
- `android/settings.gradle.kts`: `rootProject.name = "opcode42-android"`.
- `android/app/build.gradle.kts:10,14`: `namespace` + `applicationId` → `dev.opcode42.app`.
- `git mv` every `.../kotlin/dev/forge/...` → `.../kotlin/dev/opcode42/...` across `app`, `core/{model,network,store,sdk}`, `feature/{connections,sessions,chat,settings,terminal,notifications}` (113 source paths). Rewrite `package dev.forge.*` + imports (steps 2,3).
- Identifiers/files: `ForgeApp`→`Opcode42App`, `ForgeTheme`→`Opcode42Theme`, `ForgeNavGraph`→`Opcode42NavGraph`, `ForgeMessagingService`→`Opcode42MessagingService`, `core/sdk/.../ForgeClient.kt`→`Opcode42Client.kt`.
- Manifests: `.ForgeApp`, `@style/Theme.Forge`→`Theme.Opcode42` (and the style def), `.ForgeMessagingService`, meta-data value `forge_agent_activity`→`opcode42_agent_activity`.
- `res/values/strings.xml`: `app_name` `Forge`→`Opcode42`.
- Android SDK module now imports `dev.opcode42.sdk` (covered by the package sweep + Phase 2 regen).
- **Gate:** project's Android build + unit tests (per the install-on-all-emulators memory).

### Phase 4 — Build / release / packaging / ops
- `Makefile`: `DAEMON := bin/opcoded`, `./cmd/opcoded`, target help text.
- `.goreleaser.yaml`: `project_name: opcode42`; build `id`/`binary` `forged`→`opcoded`; `main: ./cmd/opcoded`; archive `id`/`name_template`. **Seam (decision: module-path-only):** `release.github.{owner,name}` and `ghcr.io/rotemmiz/forge:*` reference the **GitHub repo**, which is **not** renamed yet — leave them as `forge` for now, or they will break publishing. Flip them in lockstep with the eventual `gh repo rename` (§3).
- `packaging/systemd/forge.service` → `git mv` to `opcoded.service`; update `ExecStart`/`Description`.
- `packaging/launchd/dev.forge.daemon.plist` → `git mv` to `dev.opcode42.daemon.plist`; update `Label` + program path.
- `Dockerfile`: build/copy `forged`→`opcoded`.
- `.github/workflows/{ci,conformance}.yml`: binary paths, artifact names, env vars.
- `.env.example`: `FORGE_`→`OPCODE_`.

### Phase 5 — Docs, plans, scripts, conformance, bench, design
- `README.md`, `CONTRIBUTING.md`, `DEPENDENCIES.md`, `CLAUDE.md` (rebrand prose; add the §0 convention note + a one-line "renamed from Forge" history).
- `plans/*.md`: `Forge`→`Opcode42` in prose (keep every `opencode`). Include `00-masterplan.md`.
- `scripts/*.sh`: cache-dir names, `FORGE_` env, bin paths, module-path `go run` invocations.
- `conformance/*`, `bench/*`: `FORGE_` env + identifiers.
- `design/android/tokens.css`, `docs/design/*`, `tasks/*.md`, `tools/tui-shots/*`: branding.
- `@forge/plugin-host`: `git mv packages/forge-plugin-host packages/opcode42-plugin-host`; `package.json` `name`/`description`; `FORGE_PLUGIN_*` env.

### Phase 6 — Full verification gate
- `go build ./... · go vet ./... · gofmt -l (empty) · golangci-lint run · go test ./...`.
- `make gen` + `git diff --exit-code internal/api/gen/`; `make gen-sdks` + `check-sdk-fresh`.
- Android build + tests.
- **`scripts/run-conformance.sh self`** — the opencode-vs-opencode self-diff must still pass: proof the rename did **not** disturb wire-compat.
- **The final `forge` search — a hard acceptance gate (§2.7 below), not an eyeball.**

### 2.7. Acceptance gate — `scripts/check-rename.sh` (commit it; wire into CI)

The rename is **not done** until this script exits `0`. It enforces three invariants — no stray
`forge`, opencode wire-compat untouched, and the new names actually present — and is the literal
"search for forge after it's done" check, made into a gate with an explicit allowlist.

```bash
#!/usr/bin/env bash
# Fails if the Forge→Opcode42 rename is incomplete or over-reached.
set -euo pipefail
fail=0

# Paths legitimately allowed to still contain "forge" (history/this plan).
# Anything else with "forge" is a miss. Keep this list TINY and justified.
ALLOW='^(plans/14-rename-opcode42\.md|CHANGELOG\.md)$'

# (1) NEGATIVE: no stray "forge" (any case), outside the allowlist.
stray="$(git grep -I -i -l -e forge -- . ':!go.sum' | grep -Ev "$ALLOW" || true)"
if [[ -n "$stray" ]]; then
  echo "✗ stray 'forge' remains in:"; echo "$stray" | sed 's/^/    /'
  echo "  → rename them, or justify + add to ALLOW."; fail=1
else echo "✓ no stray 'forge' outside allowlist"; fi

# (2) WIRE-COMPAT INVARIANT: opencode identifiers must be untouched.
#     Compare the opencode-token count against main; it must not drop.
base="$(git show main:.rename-opencode-count 2>/dev/null || echo '')"   # optional pinned baseline
now="$(git grep -I -i -o -e opencode -- . | wc -l | tr -d ' ')"
for s in 'x-opencode-directory' '_opencode._tcp' 'openapi-reference.json'; do
  if ! git grep -q -F "$s" -- .; then echo "✗ protected string vanished: $s"; fail=1; fi
done
[[ -z "$base" || "$now" == "$base" ]] && echo "✓ opencode wire identifiers intact ($now)" \
  || { echo "✗ opencode count changed: $base → $now (did a sweep hit opencode?)"; fail=1; }

# (3) POSITIVE: the new names actually landed.
need=(
  'github.com/rotemmiz/opcode42'   # module path
  'dev.opcode42'                   # android / kotlin sdk pkg
  'OPCODE_'                        # env prefix
)
for s in "${need[@]}"; do
  git grep -q -F "$s" -- . || { echo "✗ expected new name missing: $s"; fail=1; }
done
[[ $fail == 0 ]] && echo "✓ new Opcode42 names present"
test -d cmd/opcoded     || { echo "✗ cmd/opcoded missing";   fail=1; }
test -d cmd/opcode-tui  || { echo "✗ cmd/opcode-tui missing"; fail=1; }
! git ls-files | grep -qi forge || { echo "✗ a tracked PATH still contains 'forge'"; git ls-files | grep -i forge | sed 's/^/    /'; fail=1; }

exit $fail
```

Notes:
- `-I` skips binary files; `:!go.sum` excludes the dependency lockfile (a transitive dep could
  legitimately contain `forge` in its name — if `git grep forge -- go.sum` is non-empty, confirm
  it's a third-party module, not ours).
- Invariant (2) is the safety net against an over-broad sweep clobbering `opencode`. Pin a baseline
  once (`git grep -i -o opencode -- . | wc -l > .rename-opencode-count` on `main`) for an exact equality
  check; without it the script still guards the three named protected strings.
- Run it as the **last** step of Phase 6 and add it to `.github/workflows/ci.yml` so the rename can't
  silently regress later. The path check (no tracked file path contains `forge`) catches missed
  `git mv`s that a content grep alone would pass.

---

## 3. Explicitly deferred (decision: "module path only")

The Go module is renamed to `github.com/rotemmiz/opcode42`, but the **GitHub repo** and **local dir**
are not. Until you do the steps below, `github.com/rotemmiz/opcode42` resolves **locally only**
(fine for `go build`; remote `go install`/CI publish would 404). Do later, then flip the §Phase-4 seam:

1. `gh repo rename opcode42` (or via web).
2. `git remote set-url origin git@github.com:rotemmiz/opcode42.git`.
3. `mv ~/git/forge ~/git/opcode42`.
4. Flip `.goreleaser.yaml` `release.github.name`/`owner` + `ghcr.io/rotemmiz/forge` → `opcode42`, and any workflow/badge URLs.

---

## 4. Risk register

- **Two-family naming is intentional** (`opcode42` namespaces vs `opcode` binaries/env). Documented in §0; note it in `CLAUDE.md` so it isn't "corrected".
- **No blanket sed.** Lowercase `forge` → two targets; use the §1 ordered recipe. Anchor `\bforged\b`/`\bforge-tui\b` so they don't catch `forged` inside other words or `forge` inside `forgeclient`.
- **Never touch `opencode`.** The §0 "Do NOT touch" list + the §Phase-6 `opencode` count-unchanged guard are the safety net. The conformance self-diff is the functional proof.
- **SDK regen needs toolchains** (Java + Swift). Regenerate on a capable machine; `check-sdk-fresh` is the gate. Don't hand-edit `sdk/{kotlin,swift}/gen`.
- **Android applicationId change = new app identity** (existing installs won't upgrade) — accepted per the rename decision.
- **`.forge` → `.opcode42` is a state-dir migration:** existing local daemons/users keep state under `~/.forge`. If continuity matters, add a one-time read-fallback to the old path; otherwise users start fresh.
- **Golden/snapshot churn:** system-prompt test, `forgeAddition_` op-ids, TUI snapshots, openapi emit tests — expect and update these in Phase 1/6.
