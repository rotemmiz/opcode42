# Human-verifiable tasks

Things to sanity-check. Items marked `[x] (automated 2026-05-29)` were verified by a
scripted run; the unchecked `[ ]` items need your judgment (decisions, design confirmations,
"eyeball" reads) and are intentionally left for you.

## S1 — Go module + build/test tooling

- [x] `make build` produces `bin/forged`; `./bin/forged --version` prints `0.0.1`. (automated 2026-05-29)
      (Note: post-S4, `./bin/forged --port N` starts the HTTP server — the original placeholder
      behavior was superseded by S4.)
- [x] `make test` is green. (automated 2026-05-29) (Test files now exist across packages; the
      original `[no test files]` note was pre-test.)
- [x] `golangci-lint run` reports `0 issues`; `golangci-lint config verify` exits 0. (automated 2026-05-29)
- [ ] Eyeball the `internal/*/doc.go` wire-compat citations — they are the contract notes
      future milestones build against; confirm they read correctly against opencode.
- [ ] CI workflow `.github/workflows/ci.yml` is authored but **not yet run** (hosted CI is
      usage-limited). The active gate is a local review subagent before any `git push`.

## S2 — Vetted dependencies

- [x] `go tool oapi-codegen --version` prints `v2.7.0` (pinned via `tool` directive in go.mod). (automated 2026-05-29)
- [ ] `DEPENDENCIES.md` lists the vetted runtime libs; confirm the choices still match plan 01.
      Runtime libs are intentionally NOT in go.mod yet (Go prunes unused requires) — they land on
      first import in plan 01.

## S3 — Spec vendor + codegen

- [x] `./scripts/sync-openapi.sh` reports `vendored 113 paths`; `conformance/openapi-reference.json`
      is byte-identical to `packages/sdk/openapi.json`; provenance file pins the opencode commit. (automated 2026-05-29)
- [x] `make gen` succeeds and prints the transform summary (22 exclusiveMinimum, 28 nullable
      collapses, 53 dup union members dropped, 4 schema renames). The generated
      `internal/api/gen/forge.gen.go` has a `ServerInterface` with **131 methods**, and regeneration
      is byte-stable (no git diff). (automated 2026-05-29)
- [ ] DECISION TO CONFIRM: the generated file is ~1.26 MB / 36k lines and is **committed** (per plan).
      If you'd rather generate-on-build instead of committing it, say so — it's a one-line CI change.
- [ ] DECISION TO CONFIRM: oapi-codegen can't load OpenAPI 3.1, so `internal/tools/downconvert`
      produces a derived 3.0 spec for codegen only. The frozen contract stays 3.1. Confirm this
      derived-spec approach (vs switching codegen tools) is acceptable.
- [ ] The 4 `Event.tui.*` SSE envelope schemas are renamed to `*2` in Go (e.g. `EventTuiCommandExecute2`)
      because opencode ships both dotted and PascalCase variants. Confirm the `*2` names are tolerable
      (they're SSE event types, rarely hand-referenced).

## S4 — forged skeleton

- [x] `./bin/forged --port 4099`, then: `curl /global/health` → `{"healthy":true,"version":"0.0.1"}`;
      `curl /doc` → openapi `3.1.0`, 113 paths; `curl /session` → 501 `{"_tag":"NotImplemented",...}`;
      `curl /nope` → 404; SIGTERM → clean exit (code 0). (automated 2026-05-29)
- [x] Startup log line is `opencode server listening on http://...` (clients scrape this prefix). (automated 2026-05-29)
- [ ] CONFIRM the `/doc` choice: Forge serves the spec at `/doc` (wire-compat with opencode), NOT
      `/openapi.json` as plan 12 §a / plan 01 M7 assumed. (You chose `/doc` + `/openapi.json` alias —
      the plan docs still need correcting.)
- [ ] CONFIRM the 501 envelope `{"_tag","message","operation"}` is acceptable as Forge's Phase-A
      placeholder (opencode never returns 501, so this is an expected conformance divergence).

## C1 — Go cassette package

- [x] `go test ./conformance/cassette/` is green: byte-for-byte golden round-trip, transport
      filters, and the PTY control-frame decode (frame[0]==0x00, payload `{"cursor":0}`). (automated 2026-05-29)
- [ ] NOTE: byte-for-byte round-trip holds for cassettes with sorted map keys (Go sorts map keys;
      JS preserves insertion order). Real recorded cassettes (C2) are compared structurally, not by
      bytes. Confirm this is acceptable.

## C4 + C5 — Normalizer + diff tool

- [x] `go test ./conformance/...` green (normalizer + diff + cassette + suite framework). (automated 2026-05-29)
- [ ] Eyeball the diff CLI output format (run the demo in the session log) — it matches plan 12 §d
      (SCENARIO / STEP / EXPECTED / ACTUAL / DETAIL, blocking vs KNOWN-DIVERGENCE, exit 1 on blocking).
- [ ] DESIGN NOTE: result files are normalized **by the suite as it writes** (it knows its own temp
      dir/paths); the diff tool is a pure structural comparator. Confirm this split is fine (vs
      normalizing at diff time). Volatile fields stripped: ULIDs, epoch-ms/RFC3339 timestamps, paths.

## C0 — Spec-drift gate

- [x] `./bin/forged --port 4099 &` then `bash scripts/check-spec-drift.sh http://127.0.0.1:4099` →
      `131 reference operations, 131 forge operations, 0 breaking` (exit 0). (automated 2026-05-29)
- [x] A seeded missing operation makes the gate report `BREAKING` and exit 1. (automated 2026-05-29)
- [x] `/openapi.json` is served by the skeleton (alias of `/doc`) and logged in
      `conformance/known-additions.json`. (automated 2026-05-29)
- [ ] NOTE: the gate is semantic (missing operations / changed status-code sets = breaking; extra
      ops checked against `conformance/known-additions.json`), so it keeps working when Forge
      self-emits a generated spec in plan 01/06 rather than echoing the reference verbatim.

## C2 + C3 + C6 + C7 — Suite, scenarios, recording, gates (run against live opencode)

- [x] `make selfdiff` (or `bash scripts/run-conformance.sh self`) → `0 blocking difference(s) in 0
      scenario(s); … 7 scenario(s) compared`. The Phase-A correctness gate. (automated 2026-05-29,
      opencode 1.15.11 on PATH)
- [x] `go test ./conformance/ -run TestSSECatalog` passes — locks Finding #2 against the committed
      real-opencode cassette: instance `/event` is BARE `{id,type,properties}`, global `/global/event`
      is WRAPPED `{payload:{…}}`. (automated 2026-05-29)
- [ ] DESIGN NOTE: each suite run uses a fresh temp dir per scenario, and the self-diff runner gives
      each opencode run a fresh `HOME` (fresh SQLite DB) — because `GET /session` returns the GLOBAL
      session list, not per-directory. Confirm fresh-state-per-run is acceptable for the gate.
- [ ] FINDING (plan correction): opencode's `POST /session/{id}/fork` response has **no `parentID`**
      and `GET /session/{id}/children` returns `[]` after a fork. Plan 12 scenario #3 ("assert
      parentID set / children returns both") is wrong; the suite records truth instead of asserting.
- [ ] SCOPE: C2 recorded the SSE catalog via a pure-Go recorder (`conformance/cmd/record`), NOT
      opencode's TS `http-recorder` (no `bun` here; Go is better for CI). PTY WS capture (Finding #3)
      is deferred until forge serves PTY (plan 01 M5 / Phase B); the cassette format already supports
      it and has a synthetic control-frame test.
- [ ] Auth (#20–22) and directory-routing (#23–25) scenarios are NOT yet in the agent-free set —
      they need opencode started with `OPENCODE_SERVER_PASSWORD` / multi-dir setup. Tracked as a
      follow-up within Phase A.
- [ ] CI workflow `.github/workflows/conformance.yml` authored (spec-drift + self-diff, opencode
      pinned to 1.15.11) but NOT run (hosted CI usage-limited). Local gate before push: review subagent.
