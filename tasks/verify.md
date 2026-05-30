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
- [x] `DEPENDENCIES.md` lists the vetted runtime libs; confirm the choices still match plan 01.
      Runtime libs are intentionally NOT in go.mod yet (Go prunes unused requires) — they land on
      first import in plan 01.

## S3 — Spec vendor + codegen

- [x] `./scripts/sync-openapi.sh` reports `vendored 113 paths`; `conformance/openapi-reference.json`
      is byte-identical to `packages/sdk/openapi.json`; provenance file pins the opencode commit. (automated 2026-05-29)
- [x] `make gen` succeeds and prints the transform summary (22 exclusiveMinimum, 28 nullable
      collapses, 53 dup union members dropped, 4 schema renames). The generated
      `internal/api/gen/forge.gen.go` has a `ServerInterface` with **131 methods**, and regeneration
      is byte-stable (no git diff). (automated 2026-05-29)
- [x] DECISION CONFIRMED (2026-05-29): the generated file (~1.26 MB / 36k lines) stays **committed**.
      Golden path completed — CI job `codegen-fresh` regenerates and runs `git diff --exit-code
      internal/api/gen/`, so a stale/hand-edited commit fails CI. Regenerate with `make gen`.
- [x] DECISION CONFIRMED (2026-05-29): keep oapi-codegen + `downconvert` (derived 3.0 spec for
      codegen only; frozen contract stays 3.1). Spiked ogen (the only 3.1-native Go generator):
      it fails on the SAME `exclusiveMinimum` number→bool issue (so a shim is unavoidable for ANY
      Go generator), and even on the downconverted spec it can't handle opencode's "complex anyOf"
      (would skip those ops/schemas, dropping the Event union). oapi-codegen handles all 131 ops +
      unions, so it stays.
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
- [x] Auth scenarios (#20–22) ADDED (2026-05-29): the runner now starts opencode auth-enabled and
      the suite sends Basic creds. auth-basic-ok→200, auth-missing-401→401 + captured
      `www-authenticate: Basic realm="Secure Area"`, auth-token-query (`?auth_token=base64(user:pass)`)→200.
      Self-diff green at 10 scenarios. (automated)
- [ ] Directory-routing scenarios (#23–25) DEFERRED — FINDING: in opencode 1.15.x, `GET /session`
      relative to `x-opencode-directory` (header) vs `?directory` (query) behaved inconsistently
      across probes (the list looks global/accumulating; header vs query filtering didn't agree:
      header-list returned multiple sessions in-suite but 1 in isolation; `?directory` returned 0
      for a `/var/folders` dir but 1 for `/tmp/dirA` earlier). Plan 01's "header≡query equivalent"
      (older `workspace-routing.ts:87`) needs re-validation against 1.15.x's workspace/control-plane
      routing before a clean scenario can be written. The Client already supports DirHeader/DirQuery/
      DirNone for when it's pinned down.
- [ ] CI workflow `.github/workflows/conformance.yml` authored (spec-drift + self-diff, opencode
      pinned to 1.15.11) but NOT run (hosted CI usage-limited). Local gate before push: review subagent.

## Interop demonstration (2026-05-29) — SUCCESS, both directions

- [x] Forge-authored client (the conformance suite) drives **real opencode**: all 7 agent-free
      scenarios run green. (automated 2026-05-29)
- [x] opencode's **own** `@opencode-ai/sdk` against `forged`: `session.list()` → `HTTP 501`
      `{"_tag":"NotImplemented",...}` (wire contract present; behavior is Phase B). The identical SDK
      call against real opencode → `HTTP 200` + session array. Same client, same request, two daemons
      — only "implemented vs 501" differs. (automated 2026-05-29)

## Plan-01 M1 (config + GET /config) & M2 (SQLite + session CRUD) — 2026-05-29

Implemented: `internal/id` (opencode id.ts port), `internal/config` (JSONC loader + opencode
merge), `internal/auth` (Basic + `?auth_token`), `internal/worktree` (realpath + git-root/`/`
fallback), `internal/storage` (modernc.org/sqlite + embedded idempotent schema),
`internal/session` (CRUD + wire shape). Wired into `internal/server` behind an `auth → directory`
middleware chain; `cmd/forged` opens storage + passes options.

- [x] Dual gate GREEN: fresh `forged` on :4097 (fresh `HOME`/`FORGE_DB`, auth on) vs live opencode
      → `0 blocking difference(s); 11 known-divergence warning(s); 10 scenarios`. The implemented
      scenarios — `session-create-list`, `session-get-delete`, `session-fork-children`, `config-get`,
      `auth-basic-ok`, `auth-missing-401`, `auth-token-query` — diff to **zero**. (automated)
- [x] Self-diff still GREEN after adding the `version` strip to the normalizer (0 blocking). (automated)
- [x] CI mimic GREEN locally: `go build/vet`, `gofmt -l` empty, `golangci-lint` 0 issues,
      `go test ./...`, `make gen` + `git diff --exit-code internal/api/gen/` unchanged. (automated)
- [ ] DECISION (user-approved): the session `version` field is a **configurable opencode-compat
      constant** (`session.DefaultCompatVersion = "1.15.11"`, the frozen-contract target), NOT
      Forge's own build version (`/global/health` still reports `0.0.1`). The conformance normalizer
      now collapses any `"version"` string to `<ver>` so the dual diff is build-independent. Confirm
      this split reads correctly.
- [ ] GATE SCOPING: the dual gate is currently scoped to the implemented endpoints by **temporary
      Phase-A known-divergence entries** in `conformance/known-divergences.json`: `provider-list`
      (needs the models.dev catalog — plan 04) and `sse-*` (event bus — plan-01 M4). REMOVE these two
      entries when M4 / plan-04 land so the scenarios become blocking again.
- [ ] FRESH-STATE REQUIREMENT: because `GET /session` is a GLOBAL list, the dual gate only matches
      when `forged` starts with a fresh DB (the runner already gives opencode a fresh `HOME`). Start
      forge for the gate with `HOME=$(mktemp -d) FORGE_DB=$HOME/forge.db ./bin/forged --port 4097`
      and do not hit it with smoke requests first (a lingering session makes `session-create-list`
      report `len 1 != len 2`).
- [ ] WIRE-SHAPE eyeball: `POST /session` emits exactly
      `{cost,directory,id,path,projectID:"global",slug,time:{created,updated},title:"New session - <iso>",tokens:{...},version}`
      with optionals omitted. `directory` is the symlink-resolved path; `path` is it relative to the
      worktree (`/` for non-git → leading slash stripped, hence the recorded `private<path>`).
      Confirm against `internal/session/session.go` + opencode `session.ts:61-158,208-224`.
- [ ] FORK QUIRK replicated: `POST /session/{id}/fork` returns a new session with title `… (fork #1)`
      and **no `parentID`**; `GET /session/{id}/children` returns `[]` (matches observed opencode
      1.15.x). Revisit when prompt/agent flows need real parent tracking (plan 02).
- [ ] SCHEMA + MIGRATOR (DECISION, "cleanest" per user 2026-05-29): `internal/storage` uses a
      dependency-free **versioned** migrator gated on SQLite's `PRAGMA user_version` (migration files
      `NNNN_name.sql`; each applies once in a tx, then bumps user_version) — NOT golang-migrate/sqlx
      (plan 01 §5) and NOT bare `CREATE IF NOT EXISTS` (which can't carry future ALTERs). PRAGMAs
      (WAL/foreign_keys) via the modernc DSN; `SetMaxOpenConns(1)` serializes writes for Phase A.
      Confirm this deviation from the plan's golang-migrate/sqlx mention is acceptable.
- [ ] New runtime deps landed (per DEPENDENCIES.md / plan 01): `github.com/tailscale/hujson`,
      `modernc.org/sqlite`. Confirm `go.mod`/`go.sum` additions are acceptable.

### Review-subagent findings (2026-05-29) — addressed / noted

- [x] FIXED (blocking-ish): `?workspace` was wrongly used as a directory fallback. opencode's
      `defaultDirectory` is strictly `?directory → x-opencode-directory → cwd`
      (workspace-routing.ts:86-87); `?workspace` only selects a workspaceID (70-83). Removed the
      branch in `internal/server/directory.go`.
- [x] FIXED: `GET /session` now orders by `time_updated DESC, id DESC LIMIT 100` to match
      opencode's default page (session.ts:927,934). (The reviewer also claimed a `time_archived IS
      NULL` filter — VERIFIED FALSE: `listByProject` has no archived filter, so none was added.)
      Aligned `session_time_idx` to `(time_updated, id)`.
- [x] FIXED: the conformance `version` normalizer now only collapses **semver-shaped** values
      (`^\d+\.\d+\.\d+`), so an unrelated `"version"` field carrying arbitrary data is still
      compared rather than masked. Added a regression test.
- [x] FIXED (2nd review): session `path` at a git worktree ROOT now matches opencode —
      `RelPath(root, root)` returns `""` (was Go's `"."`), and `Info.Path` no longer uses
      `omitempty` (opencode always emits `path`, dropping only `undefined`). Added worktree tests.
- [ ] NOTED (acceptable for M1/M2, revisit later): worktree detection uses nearest-ancestor-`.git`
      (`internal/worktree/worktree.go`) rather than `git rev-parse --show-toplevel`. Agrees for
      normal repos and non-git dirs (`/`); may diverge for linked worktrees / gitlinks. Consider
      shelling to git for full parity when git-project sessions are exercised.
- [ ] NOTED (deferred with #23-25): `GET /session` is a GLOBAL list; opencode scopes by
      project_id + directory (session.ts:897,911-915). Pending the directory-routing investigation
      before a clean per-directory scenario can be written.
- [ ] NOTED (edge case, deferred with #23-25): opencode decodes a `?directory` query value twice
      (URLSearchParams + instance-context `decode`); Forge decodes it once. Only matters for paths
      containing literal `%` sequences. The SDK uses the header path, which Forge handles correctly
      (PathUnescape, no `+`→space).

## Plan-01 M3 (instance routing/cache) & M4 (SSE event bus) — 2026-05-29

Implemented: `internal/bus` (Event{id,type,properties}; per-instance Bus + process Global
fan-out, non-blocking drop, publish forwards to global wrapped {payload,directory}),
`internal/instance` (directory→Context cache, get-or-create), `internal/server/sse.go`
(`GET /event` bare stream, `GET /global/event` {payload}-wrapped stream; server.connected first +
10s heartbeat; instance stream stops on server.instance.disposed). Wired global bus + manager in
`cmd/forged`.

- [x] Dual gate GREEN and TIGHTER: `sse-instance-connected` and `sse-global-connected` now match
      opencode FOR REAL (removed from known-divergences.json) — 0 blocking, and only `provider-list`
      remains a known-divergence (7 warnings, all provider-list). 9 of 10 scenarios genuinely green.
      (automated)
- [x] SSE shapes verified against opencode source (handlers/event.ts, handlers/global.ts) and the
      recorded cassette: instance `/event` = bare `{id,type,properties}`; global `/global/event` =
      `{payload:{id,type,properties}}`; SSE event name `message`; headers Cache-Control
      `no-cache, no-transform`, X-Accel-Buffering `no`, X-Content-Type-Options `nosniff`. (automated)
- [x] CI-mimic GREEN: build/vet/gofmt/golangci-lint(0)/`go test ./...`(incl. new bus + SSE handler
      tests)/`make gen`+gen-diff/self-diff. (automated)
- [ ] EYEBALL: instance event subscribes to the bus BEFORE writing server.connected (closes the
      subscribe-before-publish race, handlers/event.ts:23-27); slow subscribers drop (256-buffer,
      non-blocking publish) rather than stalling the bus. Confirm this is acceptable.
- [ ] NOTED: instance cache (`internal/instance`) currently creates a bus-only Context with a
      simple mutexed get-or-create (init is trivial). The plan's full single-flight (Deferred-style
      ready-channel) + dispose/`server.instance.disposed` emission land when init grows
      (config/LSP/PTY) or instances are torn down — heartbeat + dispose-termination are wired in the
      stream but no disposal is triggered yet (no TTL; matches opencode keeping instances for life).

## Plan-01 M5 (PTY over WebSocket) — 2026-05-29

Implemented `internal/pty` (spawn via creack/pty, UTF-16-code-unit ring buffer + cursor, subscribers,
connect/replay, single-use connect tickets, shell helpers) and the server endpoints:
GET /pty/shells, GET /pty, POST /pty, GET/PUT/DELETE /pty/{ptyID}, POST /pty/{ptyID}/connect-token,
and the WebSocket GET /pty/{ptyID}/connect (coder/websocket). A per-instance `pty.Manager` hangs off
`instance.Context`. Auth middleware skips Basic for `/pty/{id}/connect?ticket=` (the handler burns the
ticket instead).

- [x] Validated by UNIT tests (cursor counts UTF-16 code units incl. a 😀 surrogate pair; partial
      UTF-8 held across reads; 2MB ring trim advances bufCur; replay offset/`-1`/chunking; control
      frame `0x00`+`{cursor}`) and an END-TO-END WebSocket test (real shell → replay text frames +
      binary control frame; ticket mint → ticketed WS bypasses Basic auth; single-use ticket
      rejected on reuse). `go test -race ./internal/pty ./internal/server` clean. (automated)
- [x] spec-drift 131/131 0 breaking; dual gate still 0 blocking (PTY has no scenario yet). (automated)
- [ ] WIRE FORMAT (source-grounded, not yet live-diffed): data = TEXT frames, control = BINARY
      `0x00`+UTF-8 `{"cursor":n}` (pty/index.ts:44-51 string-vs-Uint8Array send). cursor/buffer are
      UTF-16 code units (recorded Finding). `?cursor=-1`=current end, `>=0`=absolute offset,
      missing/invalid=0. A live opencode-vs-forge PTY WS diff is still DEFERRED (harness PTY capture
      not built — no bun; verify.md C2 note). Confirm a live interop before claiming PTY done-done.
- [x] FIXED (review): on process exit Forge now closes the ptmx fd AND removes the session from the
      manager, matching opencode (pty/index.ts:264-270) — no fd/session leak; post-exit GET 404s and
      LIST omits it. (Earlier draft retained exited sessions; that was a leak and a divergence.)
- [x] FIXED (review): `splitValidUTF8` now holds back ONLY a genuine incomplete trailing multibyte
      rune and emits everything else as valid UTF-8 (U+FFFD for invalid bytes), so text WS frames are
      always valid UTF-8 (RFC 6455) and invalid bytes never stall the stream. Per-write 30s timeout
      added so a non-reading client can't block the pump goroutines. Ticket consume no longer burns on
      a ptyID mismatch. (race-clean; new boundary tests added.)
- [ ] NOTED: pty lifecycle bus events (pty.created/updated/exited/deleted) are NOT yet published to
      the event bus — wire when the agent engine needs them (plan 02). Confirm acceptable for now.
- [ ] DEFERRED (pre-existing gap, tracked): no CORS/origin check on connect-token issuance or ticket
      consume; opencode gates both on validOrigin (handlers/pty.ts:98,146). Forge has no CORS layer
      yet anywhere (config field only) — wire when config/CORS lands; add to the divergence registry.
- [ ] NOTED: malformed JSON on POST/PUT /pty is ignored (proceeds with zero-value input) rather than
      400 like opencode. Minor; revisit with request-validation pass.

## Plan-01 M6 (mDNS + graceful shutdown) — 2026-05-29 — COMPLETES PLAN 01

Implemented `internal/mdns` (grandcat/zeroconf publish/withdraw, loopback gating), config→server
settings wiring (`config.Server`, config-over-flags via `flag.Visit`, mDNS forces 0.0.0.0), a base
context threaded into SSE/PTY streams so shutdown unblocks them, `instance.Manager.DisposeAll`
(emits `server.instance.disposed`, kills PTYs, clears cache), and the full graceful-shutdown
sequence in `cmd/forged` (withdraw mDNS → dispose instances → cancel streams → drain HTTP 10s →
close DB). New flags: `--mdns`, `--mdns-domain`.

- [x] LIVE E2E verified: `--host 0.0.0.0 --mdns` → `dns-sd -B _http._tcp local.` shows
      `opencode-4096` (discoverable like opencode). SIGTERM with an OPEN `/event` SSE stream → clean
      exit code 0 in ~0.1s (base-context cancel unblocks the stream so HTTP drains immediately
      instead of waiting the 10s timeout). (manual 2026-05-29)
- [x] Unit/integration tests: mDNS loopback gating; `config.Server` extraction + config-over-flags;
      `DisposeAll` emits `server.instance.disposed` and clears the cache; SSE stream closes when
      BaseCtx is cancelled (graceful-shutdown unblock). `go test -race` clean. (automated)
- [x] CI-mimic + gates GREEN: build/vet/gofmt/golangci-lint(0)/`go test ./...`/gen-diff/spec-drift
      131-131-0/self-diff(0)/dual(0 blocking, 7 provider-list warnings). (automated)
- [ ] NOTED divergence: mDNS host A-record — Forge advertises via zeroconf RegisterProxy with host
      derived from `--mdns-domain` (default "opencode"); opencode's bonjour advertises host
      "opencode.local". The browse record (instance `opencode-<port>`, `_http._tcp`, txt `path=/`)
      matches, which is what clients discover; the host target differs cosmetically. Confirm OK, or
      align the host string if a client resolves it.
- [ ] NOTED: network settings are read from GLOBAL config only (`config.Load("")`, skipping the
      project layer), matching opencode's `getGlobal()` for network (cli/network.ts:40). CORS config
      field is parsed but not yet enforced (no CORS layer — same deferral as the PTY connect-token
      note above).

## PLAN 01 STATUS — DONE (Phase A core)
M1 (config) · M2 (session CRUD) · M3 (instance cache) · M4 (SSE bus) · M5 (PTY/WS) · M6 (mDNS +
graceful shutdown) all merged to main via PRs #1-#? with review gates. M7 (OpenAPI spec emission)
was satisfied earlier (spec served at /doc, 501 fan-out, spec-drift gate). Remaining open items
are the `[ ]` notes above (deferred edges) + the deferred live PTY WS conformance capture and the
`provider-list` divergence (plan 04).
- [ ] NOTED: for a login-capable shell (sh/bash/zsh/...) Forge appends `-l` to args exactly as
      opencode does (pty/index.ts:191-193), even when an explicit `-c` command is given — matches
      opencode, including the quirk that `-l` then lands after the `-c` script.

## M1 (plan 02) — message/part model + storage + serializers

- [x] `go test -race ./internal/engine/...` green; `golangci-lint run` 0 issues; `gofmt -l` clean. (automated 2026-05-29)
- [x] `make gen` byte-stable (M1 touched no endpoints). (automated 2026-05-29)
- [x] `toModelMessages` + `filterCompacted`/`latest` ported from opencode's message-v2.test.ts;
      a local review subagent confirmed 1:1 fidelity (no blocking findings). (automated 2026-05-29)
- [ ] DESIGN CONFIRM: the provider-neutral `llm.ModelMessage` shape (vs mirroring the AI SDK's
      ModelMessage exactly). Forge produces its own neutral form; the OpenAI/Anthropic *wire* JSON
      is rendered from it in M2. Confirm this is the intended boundary.
- [ ] DIVERGENCE CONFIRM (serialize.go header): (a) tool-result media is uniformly promoted to a
      trailing user message; (b) `providerExecuted` tools are not yet modeled (deferred to the
      Anthropic/server-tool path). Both are documented; confirm they belong in the plan-12
      known-divergence registry.

## M2–M9 (plan 02) — engine end-to-end

- [x] `go test -race ./...` green; `golangci-lint run` 0 issues; `gofmt -l` clean; `make gen` byte-stable. (automated 2026-05-29)
- [x] Deterministic integration suite green: `internal/engine/enginetest` `TestE2E_TextOnly`
      (streamed text → SSE deltas + persisted parts) and `TestE2E_ToolCall` (tool executed, result
      fed into the 2nd request, file actually written, 2 provider calls). (automated 2026-05-29)
- [x] Two local review subagents (M1; M2–M9) returned no blocking findings; should-fix items
      applied (rune truncation, lock-safe mock, usage clamp, overflow formula, interrupt shape,
      patch context verify). (automated 2026-05-29)
- [ ] LIVE PROOF (needs your free-tier key): run one real prompt end-to-end against an
      OpenAI-compatible provider. Example (Groq free tier):
      `FORGE_TEST_BASE_URL=https://api.groq.com/openai/v1 FORGE_TEST_MODEL=llama-3.3-70b-versatile \
       FORGE_TEST_API_KEY=$GROQ_API_KEY go test ./internal/engine/enginetest -run TestLive -v`
      Expect a non-empty reply logged with finish/tokens/cost. (Works with Cerebras/OpenRouter/Ollama too.)
- [ ] DEFERRED (tracked): Anthropic provider (was M2, now post-M9); M10 compaction (overflow
      threshold to be finalized there); server /session/:id/prompt endpoint wiring (plan 09) so the
      live path is reachable over HTTP, not just via the engine API.
- [ ] DESIGN CONFIRM: M6 task/websearch/skill take injected collaborators with stub defaults;
      doom-loop scope counts tool calls (not arbitrary parts); websearch is flag-gated only.

## Post-M9 follow-ups (a/b/c) — merged 2026-05-30

- [x] (a) HTTP prompt endpoints wired to the engine (PR #6): POST /session/:id/message (sync→{info,parts}),
      prompt_async (204), GET /message, POST /abort; 404 on unknown session, 409 on busy.
      httptest end-to-end through middleware→handler→engine→DB. (automated)
- [x] (b) M10 compaction (PR #7): overflow→summary(summary:true)→session.compacted→resume; prune
      protects recent turns. Unit (selectTail, prune) + E2E. (automated)
- [x] (c) Anthropic provider (PR #8): hand-rolled /v1/messages client, content_block SSE→llm.Event,
      thinking-signature passthrough; provider factory routes anthropic-native vs openai-compatible
      (Bedrock/Vertex excluded). httptest event-sequence + request-render tests. (automated)
- [ ] LIVE PROOF over HTTP (needs your free-tier key + a running daemon): start `./bin/forged --port 4096`,
      then with the provider env set (FORGE_PROVIDER_BASE_URL / FORGE_PROVIDER_API_KEY), create a session
      and `curl -XPOST localhost:4096/session/$ID/message?directory=$PWD -d '{"model":{"providerID":"openai-compatible","modelID":"<model>"},"parts":[{"type":"text","text":"ping"}]}'`.
      (The engine-level TestLive already proves the model wire; this proves the HTTP surface.)
- [ ] DESIGN CONFIRM: HTTP default permission policy is allow-all until plan-04 config/agent rules;
      the prompt loop runs on context.WithoutCancel(request) so a disconnect doesn't abort (only /abort does).

## TUI: model switcher + multi-line composer (2026-05-30)
- [ ] EYEBALL (model switcher, PR #15): `ctrl+p` → Switch model opens pre-highlighted (●) on the
      current model, lists only connected providers' models, windows with `↑/↓ more` when long
      (~80 in the live opencode catalog). Pick another → status line shows `model · <prov>/<model>`
      and the next prompt runs on it. No `--provider/--model` flags needed.
- [ ] EYEBALL (composer): the input is now a real multi-line editor. `ctrl+j` (and `shift+enter`
      in terminals that distinguish it) inserts a newline; `enter` submits; the box auto-grows with
      content (cap 8 rows) and collapses after sending. Long lines wrap to the content column.
      Placeholder reads "Ask anything…" on splash, "Reply, or / for commands" in a session.

## Pre-existing (NOT from this work) — flagged
- [ ] BUG: `scripts/run-conformance.sh self` fails on `session-create-list` step 2
      (`body.(root): len 22 != len 25`) — recorded fixtures are stale vs the current Forge session
      JSON (3 extra fields). Present on `main` before the TUI work. Re-record the session fixtures
      to make the self-conformance gate green.

## Phase 2 complete — TUI chrome + navigation (2026-05-30, PRs #19–#22)
Eyeball these against a daemon (`pnpm --filter @forge/tui start` or the forge-tui binary):
- [ ] STATUS BAR (#19): bottom bar shows `mode · model` left, connection dot + tokens/cost + `ctrl+p commands` right.
- [ ] SIDEBAR (#19): on a ≥80-col session screen, right sidebar shows title + CONTEXT (tokens, cost) + dir + Forge tag; `ctrl+x b` toggles it; the composer never bleeds into it.
- [ ] MODELS/SESSIONS (#14/#15): `ctrl+p` palette + `/models` `/sessions`, windowed lists.
- [ ] AGENTS (#20): `/agents` or `ctrl+x a` → pick build/plan/explore/general (● current); status mode updates; next prompt runs under it. Internal agents (compaction/summary/title) are hidden.
- [ ] THEMES (#20): `/themes` → forge-dark / forge-light / monochrome; the WHOLE screen (incl. composer + background) repaints legibly — verify forge-light is readable on a dark terminal.
- [ ] TIMELINE (#21): `/timeline` or `ctrl+x g` → lists your turns; enter reverts the session to before that turn (reversible via opencode /unrevert — not yet UI-exposed).
- [ ] STATUS MODAL (#21): `/status` or `ctrl+x s` → daemon/state/dir/model/agent/theme/events/sessions/session-id.
- [ ] SLASH (#18): `/` opens command popup; tab completes; enter runs builtin or daemon command.
- [ ] @-MENTION (#22): type `@mod` → file picker (GET /find/file); tab/enter inserts `@path `; daemon resolves it to a file part in the prompt.
- [ ] LEADER (#22): `ctrl+x` shows the chord hint; l/n/m/a/g/s/p/b dispatch.

## Phase 2 known follow-ups (not blockers)
- [ ] No-confirm revert matches opencode but a UI `unrevert`/undo affordance would be safer (Phase 3).
- [ ] @-mention has no debounce (one GET /find/file per keystroke) — fine for local daemon, revisit for remote.
- [ ] Sidebar has no context-% bar / LSP block yet (needs model context limits wired).

## Phase 3 complete — interactive + board (2026-05-31, PRs #24–#27 + conformance)
- [ ] PERMISSION (#24): run a tool that needs approval against opencode → a centered card blocks
      with allow-once/always/reject (a/s/r/↑↓+enter); the agent proceeds/halts accordingly. A failed
      reply keeps the card (retry), not a silent hang.
- [ ] QUESTION (#25): trigger an AskUserQuestion → step through single/multi-select; answers reach
      the agent. Free-text-only questions show "press r to reject".
- [ ] TASKS (#26): `ctrl+x t` shows the session todos; they update live as the agent runs todowrite.
- [ ] PTY (#27, transport only): SDK client create/connect/echo is live-smoked; the interactive
      in-TUI VT pane is deferred stretch (needs a VT emulator dependency).
- [ ] CONFORMANCE (#U13): `scripts/run-conformance.sh self` now covers agent-list / session-todo /
      session-message-list (deterministic). `GET /command` excluded (opencode order non-deterministic).
      Dual-run TUI parity vs Forge is blocked until Forge implements /agent, /provider,
      permission/question replies, /find/file, /pty (gap-closing track).

## Pre-existing conformance note (NOT Phase 3)
- [ ] `session-create-list` still self-diffs (GET /session returns a project-scoped, accumulating
      session list — len differs between two fresh runs because sessions persist in the repo's
      storage, not isolated by the fresh HOME). Orthogonal to the TUI work; needs the runner to
      isolate per-run project storage or the normalizer to count rather than compare the list.
