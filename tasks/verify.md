# Human-verifiable tasks

Things to sanity-check. Items marked `[x] (automated 2026-05-29)` were verified by a
scripted run; the unchecked `[ ]` items need your judgment (decisions, design confirmations,
"eyeball" reads) and are intentionally left for you.

## S1 — Go module + build/test tooling

- [x] `make build` produces `bin/opcoded`; `./bin/opcoded --version` prints `0.0.1`. (automated 2026-05-29)
      (Note: post-S4, `./bin/opcoded --port N` starts the HTTP server — the original placeholder
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
      `internal/api/gen/opcode42.gen.go` has a `ServerInterface` with **131 methods**, and regeneration
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

## S4 — opcoded skeleton

- [x] `./bin/opcoded --port 4099`, then: `curl /global/health` → `{"healthy":true,"version":"0.0.1"}`;
      `curl /doc` → openapi `3.1.0`, 113 paths; `curl /session` → 501 `{"_tag":"NotImplemented",...}`;
      `curl /nope` → 404; SIGTERM → clean exit (code 0). (automated 2026-05-29)
- [x] Startup log line is `opencode server listening on http://...` (clients scrape this prefix). (automated 2026-05-29)
- [ ] CONFIRM the `/doc` choice: Opcode42 serves the spec at `/doc` (wire-compat with opencode), NOT
      `/openapi.json` as plan 12 §a / plan 01 M7 assumed. (You chose `/doc` + `/openapi.json` alias —
      the plan docs still need correcting.)
- [ ] CONFIRM the 501 envelope `{"_tag","message","operation"}` is acceptable as Opcode42's Phase-A
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

- [x] `./bin/opcoded --port 4099 &` then `bash scripts/check-spec-drift.sh http://127.0.0.1:4099` →
      `131 reference operations, 131 opcode42 operations, 0 breaking` (exit 0). (automated 2026-05-29)
- [x] A seeded missing operation makes the gate report `BREAKING` and exit 1. (automated 2026-05-29)
- [x] `/openapi.json` is served by the skeleton (alias of `/doc`) and logged in
      `conformance/known-additions.json`. (automated 2026-05-29)
- [ ] NOTE: the gate is semantic (missing operations / changed status-code sets = breaking; extra
      ops checked against `conformance/known-additions.json`), so it keeps working when Opcode42
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
      is deferred until opcode42 serves PTY (plan 01 M5 / Phase B); the cassette format already supports
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

- [x] Opcode42-authored client (the conformance suite) drives **real opencode**: all 7 agent-free
      scenarios run green. (automated 2026-05-29)
- [x] opencode's **own** `@opencode-ai/sdk` against `opcoded`: `session.list()` → `HTTP 501`
      `{"_tag":"NotImplemented",...}` (wire contract present; behavior is Phase B). The identical SDK
      call against real opencode → `HTTP 200` + session array. Same client, same request, two daemons
      — only "implemented vs 501" differs. (automated 2026-05-29)

## Plan-01 M1 (config + GET /config) & M2 (SQLite + session CRUD) — 2026-05-29

Implemented: `internal/id` (opencode id.ts port), `internal/config` (JSONC loader + opencode
merge), `internal/auth` (Basic + `?auth_token`), `internal/worktree` (realpath + git-root/`/`
fallback), `internal/storage` (modernc.org/sqlite + embedded idempotent schema),
`internal/session` (CRUD + wire shape). Wired into `internal/server` behind an `auth → directory`
middleware chain; `cmd/opcoded` opens storage + passes options.

- [x] Dual gate GREEN: fresh `opcoded` on :4097 (fresh `HOME`/`OPCODE_DB`, auth on) vs live opencode
      → `0 blocking difference(s); 11 known-divergence warning(s); 10 scenarios`. The implemented
      scenarios — `session-create-list`, `session-get-delete`, `session-fork-children`, `config-get`,
      `auth-basic-ok`, `auth-missing-401`, `auth-token-query` — diff to **zero**. (automated)
- [x] Self-diff still GREEN after adding the `version` strip to the normalizer (0 blocking). (automated)
- [x] CI mimic GREEN locally: `go build/vet`, `gofmt -l` empty, `golangci-lint` 0 issues,
      `go test ./...`, `make gen` + `git diff --exit-code internal/api/gen/` unchanged. (automated)
- [ ] DECISION (user-approved): the session `version` field is a **configurable opencode-compat
      constant** (`session.DefaultCompatVersion = "1.15.11"`, the frozen-contract target), NOT
      Opcode42's own build version (`/global/health` still reports `0.0.1`). The conformance normalizer
      now collapses any `"version"` string to `<ver>` so the dual diff is build-independent. Confirm
      this split reads correctly.
- [ ] GATE SCOPING: the dual gate is currently scoped to the implemented endpoints by **temporary
      Phase-A known-divergence entries** in `conformance/known-divergences.json`: `provider-list`
      (needs the models.dev catalog — plan 04) and `sse-*` (event bus — plan-01 M4). REMOVE these two
      entries when M4 / plan-04 land so the scenarios become blocking again.
- [ ] FRESH-STATE REQUIREMENT: because `GET /session` is a GLOBAL list, the dual gate only matches
      when `opcoded` starts with a fresh DB (the runner already gives opencode a fresh `HOME`). Start
      opcode42 for the gate with `HOME=$(mktemp -d) OPCODE_DB=$HOME/opcode42.db ./bin/opcoded --port 4097`
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
      (URLSearchParams + instance-context `decode`); Opcode42 decodes it once. Only matters for paths
      containing literal `%` sequences. The SDK uses the header path, which Opcode42 handles correctly
      (PathUnescape, no `+`→space).

## Plan-01 M3 (instance routing/cache) & M4 (SSE event bus) — 2026-05-29

Implemented: `internal/bus` (Event{id,type,properties}; per-instance Bus + process Global
fan-out, non-blocking drop, publish forwards to global wrapped {payload,directory}),
`internal/instance` (directory→Context cache, get-or-create), `internal/server/sse.go`
(`GET /event` bare stream, `GET /global/event` {payload}-wrapped stream; server.connected first +
10s heartbeat; instance stream stops on server.instance.disposed). Wired global bus + manager in
`cmd/opcoded`.

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
      missing/invalid=0. A live opencode-vs-opcode42 PTY WS diff is still DEFERRED (harness PTY capture
      not built — no bun; verify.md C2 note). Confirm a live interop before claiming PTY done-done.
- [x] FIXED (review): on process exit Opcode42 now closes the ptmx fd AND removes the session from the
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
      consume; opencode gates both on validOrigin (handlers/pty.ts:98,146). Opcode42 has no CORS layer
      yet anywhere (config field only) — wire when config/CORS lands; add to the divergence registry.
- [ ] NOTED: malformed JSON on POST/PUT /pty is ignored (proceeds with zero-value input) rather than
      400 like opencode. Minor; revisit with request-validation pass.

## Plan-01 M6 (mDNS + graceful shutdown) — 2026-05-29 — COMPLETES PLAN 01

Implemented `internal/mdns` (grandcat/zeroconf publish/withdraw, loopback gating), config→server
settings wiring (`config.Server`, config-over-flags via `flag.Visit`, mDNS forces 0.0.0.0), a base
context threaded into SSE/PTY streams so shutdown unblocks them, `instance.Manager.DisposeAll`
(emits `server.instance.disposed`, kills PTYs, clears cache), and the full graceful-shutdown
sequence in `cmd/opcoded` (withdraw mDNS → dispose instances → cancel streams → drain HTTP 10s →
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
- [ ] NOTED divergence: mDNS host A-record — Opcode42 advertises via zeroconf RegisterProxy with host
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
- [ ] NOTED: for a login-capable shell (sh/bash/zsh/...) Opcode42 appends `-l` to args exactly as
      opencode does (pty/index.ts:191-193), even when an explicit `-c` command is given — matches
      opencode, including the quirk that `-l` then lands after the `-c` script.

## M1 (plan 02) — message/part model + storage + serializers

- [x] `go test -race ./internal/engine/...` green; `golangci-lint run` 0 issues; `gofmt -l` clean. (automated 2026-05-29)
- [x] `make gen` byte-stable (M1 touched no endpoints). (automated 2026-05-29)
- [x] `toModelMessages` + `filterCompacted`/`latest` ported from opencode's message-v2.test.ts;
      a local review subagent confirmed 1:1 fidelity (no blocking findings). (automated 2026-05-29)
- [ ] DESIGN CONFIRM: the provider-neutral `llm.ModelMessage` shape (vs mirroring the AI SDK's
      ModelMessage exactly). Opcode42 produces its own neutral form; the OpenAI/Anthropic *wire* JSON
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
      `OPCODE_TEST_BASE_URL=https://api.groq.com/openai/v1 OPCODE_TEST_MODEL=llama-3.3-70b-versatile \
       OPCODE_TEST_API_KEY=$GROQ_API_KEY go test ./internal/engine/enginetest -run TestLive -v`
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
- [ ] LIVE PROOF over HTTP (needs your free-tier key + a running daemon): start `./bin/opcoded --port 4096`,
      then with the provider env set (OPCODE_PROVIDER_BASE_URL / OPCODE_PROVIDER_API_KEY), create a session
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
      (`body.(root): len 22 != len 25`) — recorded fixtures are stale vs the current Opcode42 session
      JSON (3 extra fields). Present on `main` before the TUI work. Re-record the session fixtures
      to make the self-conformance gate green.

## Phase 2 complete — TUI chrome + navigation (2026-05-30, PRs #19–#22)
Eyeball these against a daemon (`pnpm --filter @opcode42/tui start` or the opcode-tui binary):
- [ ] STATUS BAR (#19): bottom bar shows `mode · model` left, connection dot + tokens/cost + `ctrl+p commands` right.
- [ ] SIDEBAR (#19): on a ≥80-col session screen, right sidebar shows title + CONTEXT (tokens, cost) + dir + Opcode42 tag; `ctrl+x b` toggles it; the composer never bleeds into it.
- [ ] MODELS/SESSIONS (#14/#15): `ctrl+p` palette + `/models` `/sessions`, windowed lists.
- [ ] AGENTS (#20): `/agents` or `ctrl+x a` → pick build/plan/explore/general (● current); status mode updates; next prompt runs under it. Internal agents (compaction/summary/title) are hidden.
- [ ] THEMES (#20): `/themes` → opcode42-dark / opcode42-light / monochrome; the WHOLE screen (incl. composer + background) repaints legibly — verify opcode42-light is readable on a dark terminal.
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
      Dual-run TUI parity vs Opcode42 is blocked until Opcode42 implements /agent, /provider,
      permission/question replies, /find/file, /pty (gap-closing track).

## Opcode42 gap-closing PR-1 — interactive replies + todo (2026-05-31, branch feat/daemon-interactive-endpoints)
Opcode42's daemon now implements the manager-backed interactive endpoints (plan 02 M6/M7); they
were 501 before. Verified against the live opencode daemon (byte-identical 404 shapes) and via
dual-run conformance.
- [x] (automated 2026-05-31) `POST /permission/:id/reply`, `POST /question/:id/reply`,
      `POST /question/:id/reject`, `GET /session/:id/todo` return opencode-identical shapes/status
      (live-smoke vs 127.0.0.1:4096 + dual-run: all four parity scenarios pass).
- [ ] TUI-AGAINST-OPCODE42 setup. Run a Opcode42 daemon and point the TUI at it (NOT opencode):
      `go build -o /tmp/opcoded ./cmd/opcoded && /tmp/opcoded --port 4097 --host 127.0.0.1`
      then `go run ./cmd/opcode-tui --url http://127.0.0.1:4097 --dir "$PWD" --provider <id> --model <id>`
      (a provider API key must be in the env so the agent can run tools).
- [ ] QUESTION OVERLAY + TODO DOCK (verifiable now): prompt the agent to call the `question` tool
      (multi-question, matching opencode) — confirm the overlay renders header/options/multiple,
      single- and multi-select answers reach the agent, and esc/r rejects. Prompt a multi-step task
      so it calls `todowrite`, then open the tasks dock (`ctrl+x t`) and confirm todos render/update
      live. (Needs a real LLM prompt — left for you so it doesn't auto-spend tokens.)
- [x] PERMISSION OVERLAY now wired (PR #38, 2026-05-31): the prompt path resolves the named agent
      and applies its permission ruleset instead of allow-all, so a restrictive agent fires
      `permission.asked` and blocks until `POST /permission/:id/reply`. Proven by
      `TestPrompt_RestrictiveAgentTriggersPermission`. EYEBALL: in a TUI-against-Opcode42 session, run a
      prompt under a restrictive `.opencode/agent/*.md` (e.g. `permission: {bash: ask}`) and confirm
      the U10 overlay blocks the tool until you allow/reject.

## Opcode42 gap-closing PR-2 — GET /find/file (2026-05-31, branch feat/find-file)
Fuzzy file/dir search backing the TUI's @-mention picker (plan 04 M8); was 501 before.
- [x] (automated 2026-05-31) GET /find/file returns repo-relative paths (dirs with trailing /),
      query required (400 if missing), limit 1..200 (default 100), ?type=directory supported.
      Live-smoke vs opencode: top result for "server.go" is identical; ordering is close.
- [ ] EYEBALL @-MENTION: in the TUI-against-Opcode42 session above, type "@" + a partial filename and
      confirm the picker shows sensible best-first matches and inserts the chosen path. (Opcode42's
      fuzzy ranking is its own scorer, not opencode's fuzzysort — order may differ slightly; that's
      an accepted divergence, see conformance/known-divergences.json "find-file".)
- [ ] NOTE: Opcode42 skips a fixed ignore set (.git/node_modules/vendor/.opcode42 + hidden) rather than
      parsing .gitignore like opencode's `rg --files`, so results may include files .gitignore would
      hide. Confirm acceptable, or flag for the optional .gitignore-aware walker (PR-3/plan 04).

## Opcode42 gap-closing PR-3 — GET /provider, /agent, /command (2026-05-31, branch feat/resource-endpoints)
Resource loaders (plan 04 M3/M4/M7/M8): all three were 501 before. New package internal/resource
(built-in agents + .opencode/agent(s)|command(s) markdown loaders + models.dev provider list with
auth.json/env connected detection). Flips the TUI's model switcher, agents modal, and slash commands
to Opcode42.
- [x] (automated 2026-05-31) Live-smoke vs opencode at its own repo dir: Opcode42 loads the project's
      .opencode agents (duplicate-pr, triage) and all 8 .opencode commands identically; /provider
      `all` count matches opencode (137) and `opencode` shows connected via the shared auth.json.
- [ ] EYEBALL SWITCHERS: in a TUI-against-Opcode42 session, open the model switcher (confirm connected
      providers' models list), the agents modal (`/agents` — build/plan/general/explore + any project
      agents), and slash commands (`/` — project commands). Confirm they populate from Opcode42.
- [ ] NOTE (divergences, see known-divergences.json): /agent built-ins carry a simple allow-all
      permission (not opencode's env-specific patterns + prompts) and omit flag-gated `scout`;
      /command returns only .opencode/config commands (opencode also has built-in/MCP/skill commands);
      /provider `connected` depends on which provider creds are in the daemon's env/auth.json.
- [ ] PERMISSION OVERLAY still needs wiring: PR-3 loads agent permission rulesets but the HTTP prompt
      path (`prompt_handlers.go`) still uses allow-all, so live `permission.asked` doesn't fire yet.
      Consuming the loaded agent's rulesets in the engine is a separate follow-up (engine does not yet
      resolve agents by name). Flag if you want that prioritized next.

## Finish Phase B — PR-4/PR-5/M11 (2026-05-31, PRs #38/#40 + M11)
Plan 02 agent loop completed end-to-end on the Go daemon.
- [x] PR-4 (#38): engine resolves the prompt's agent → applies its model/system-prompt/permission
      rulesets (permission overlay now fires; see above).
- [x] PR-5 (#40): subagent `task` tool wired — child session + nested loop + `<task>`-wrapped result.
- [x] M11: HTTP-level agent-loop SSE baseline (`TestAgentLoopSSESequence`) asserts the documented
      sequence (server.connected → user message → text deltas → completed assistant). Building it
      surfaced and FIXED a real data race: the emit path published live mutable part/message pointers
      that the SSE goroutine marshalled while the processor kept mutating them; now it publishes
      immutable snapshots (`message.ClonePart`/`CloneAssistant`, deep-copying `TextPart.Time`). Full
      `go test ./... -race` is clean.
- [ ] EYEBALL: run the TUI against Opcode42 and drive a multi-step prompt with a subagent (`task`) and a
      restrictive agent; confirm streaming, the tasks dock, the permission overlay, and subagent
      delegation all work end-to-end. (Needs a real LLM key — left for you.)

## Pre-existing conformance note (NOT Phase 3)
- [ ] `session-create-list` still self-diffs (GET /session returns a project-scoped, accumulating
      session list — len differs between two fresh runs because sessions persist in the repo's
      storage, not isolated by the fresh HOME). Orthogonal to the TUI work; needs the runner to
      isolate per-run project storage or the normalizer to count rather than compare the list.

## P13-rest — Remote-ops hardening (2026-06-04, plan 13 §13.1/§13.7/§Packaging)

Auth + mDNS hardening are unit-tested and CI-gated; the items below need real infra /
multiple machines and are left for you.

- [x] Constant-time credential compare (`auth.authorized` via `crypto/subtle`), wrong-username
      and wrong-password both 401, both-fields-checked. (automated: `go test ./internal/auth/...`)
- [x] Non-loopback bind without a password refuses to start (`CheckBindExposure`). (automated:
      `TestCheckBindExposure`) Spot-check live: `OPENCODE_SERVER_PASSWORD= ./bin/opcoded --host 0.0.0.0`
      should exit non-zero with a "refusing to bind" error.
- [x] mDNS advertises BOTH `_http._tcp` and `_opencode._tcp`. (automated: `TestPublishDualRecords`)
- [ ] EYEBALL (LAN): start `./bin/opcoded --mdns` (with a password) on one machine; browse
      `_opencode._tcp.local` and `_http._tcp.local` from another (e.g. `dns-sd -B _opencode._tcp`
      on macOS) — confirm both records appear with TXT `auth=required`/`version=1` on the Opcode42 one.
- [ ] EYEBALL (release): push a `vX.Y.Z` tag (or run `make release-snapshot`) — confirm goreleaser
      produces linux/darwin amd64+arm64 archives + checksums, and (on a real tag) multi-arch
      `ghcr.io/rotemmiz/opcode42` images. Binary stays < 40MB (CI asserts this).
- [ ] EYEBALL (container): `docker run --rm -e OPENCODE_SERVER_PASSWORD=x -p 4096:4096
      ghcr.io/rotemmiz/opcode42:latest --host 0.0.0.0` then `curl -u opencode:x localhost:4096/global/health`
      returns 200.
- [ ] EYEBALL (service units): install `packaging/systemd/opcode42.service` (Linux) or
      `packaging/launchd/dev.opcode42.daemon.plist` (macOS); confirm the daemon starts and restarts.

### Deferred to followups (NOT in this PR)
- [ ] Push notifications (FCM dispatcher, `/push/*` endpoints, notification queue) — plan 13 §13.8.
      Needs an FCM service account + a real Android device; cannot be CI-gated. Left as a followup.
- [ ] `opcode42 install-service` CLI command (systemd/launchd/Windows-svc generators) — plan 13 §13.13.
      Static unit templates ship in `packaging/`; the generator command is deferred.
- [ ] Windows release target — `internal/lsp/service.go` uses Unix-only syscalls without build
      constraints, so windows/amd64 cross-build fails. goreleaser omits windows until the daemon is
      made Windows-portable (out of this track's scope; do not edit internal/lsp here).

## P07-B — Android repointed to Opcode42 daemon (2026-06-04, plan 07 Phase B)
Repointed the Android client's HTTP+SSE wiring at the Opcode42 daemon and fixed the SSE
consumption path (it was parsing the SSE `event:` name as the type and reading the wrong
payload field locations). Deterministic JVM unit tests (19) now pin the wire contract and
run in CI (new `android` job in ci.yml). The flows below need a LIVE Opcode42 daemon + real LLM
key, so they are manual EYEBALL items (the gemini free key is throttled):
- [ ] EYEBALL: `opcode42 serve`, add the server in the app (URL + Basic creds), confirm the session
      list loads (GET /session) and a new session can be created.
- [ ] EYEBALL: open a running session and confirm streaming works end-to-end via SSE — assistant
      text deltas (`message.part.delta`), full part replaces (`message.part.updated` → nested `part`),
      and `message.updated` (`info`-wrapped) all render live with no manual refresh.
- [ ] EYEBALL: trigger a tool that needs permission; confirm the permission bottom sheet appears
      from `permission.asked` and dismisses on `permission.replied`.
- [ ] EYEBALL: background the app ~60s then foreground; confirm SSE reconnects and state catches up.
- [ ] EYEBALL: verify `Authorization: Basic …` is present on REST + SSE calls (OkHttp logging /
      Charles), and that `?auth_token=` is appended on the WS-PTY upgrade URL.

## P07-C — Android WS-PTY terminal made functional (2026-06-04, plan 07 Phase C)
The terminal pane previously dropped all output (it only handled binary WS frames, but the
daemon streams PTY output as TEXT frames) and would have rendered raw ANSI escapes as garbage.
This slice routes text frames through a new pure-Kotlin `TerminalEmulator` (CR/LF/BS/TAB +
CSI cursor/erase + SGR/OSC stripping), parses the `0x00 + {cursor}` control frame for
reconnect-resume, and reports terminal size to the daemon via `PUT /pty/{id}`. JVM unit tests
(`TerminalEmulatorTest`, `PtyClientCursorTest`) pin the rendering + cursor contract in CI.
Live-daemon eyeball items (need `opcode42 serve` + a real shell):
- [ ] EYEBALL: open a session's Terminal pane; run `ls`, `echo`, `vim`/`top` — confirm output is
      readable (colors stripped, no `^[[…m` garbage), progress bars (`\r`) overwrite in place, and
      backspace/tab render correctly.
- [ ] EYEBALL: type a command + Enter; confirm keystrokes reach the shell and output streams back.
- [ ] EYEBALL: rotate the device / show-hide the keyboard; confirm the shell re-wraps to the new
      width (PUT /pty resize took effect — e.g. `tput cols` reflects the visible width).
- [ ] EYEBALL: background+foreground or briefly drop the network during a terminal session and
      confirm reconnect resumes from the last cursor without replaying the entire scrollback.

## P07-C — Android session rename + archive UI (plan 07 Phase C; PATCH /session/{id} #124)
- [ ] EYEBALL: long-press a session row in the list → "Rename session" → change the title → Save;
      confirm the row title updates immediately and persists across app restart (PATCH title).
- [ ] EYEBALL: open a session → overflow (⋮) → "Rename session"; confirm the chat top bar title updates.
- [ ] EYEBALL: long-press a session row → "Archive session"; confirm it disappears from the active
      list and the top-bar "Archived (n)" badge increments.
- [ ] EYEBALL: tap the "Archived (n)" badge; confirm archived sessions are listed, the title shows
      "Archived", the FAB is hidden, and rows offer NO "Archive" action (opencode has no un-archive path).
- [ ] EYEBALL: open a session → overflow → "Archive session"; confirm it navigates back and the
      session is gone from the active list.
- [ ] EYEBALL: with the app open on the session list, archive/rename the SAME session from another
      client (or curl PATCH /session/{id}); confirm the list updates live via the session.updated SSE
      (no manual refresh).

## P13-FCM — Daemon-side push-notification relay (2026-06-04, plan 13 §13.8)
A new `internal/push` package adds FCM push: device-token registration
(`POST/GET/DELETE /push/register[/{deviceID}]`, Opcode42 known-additions, spec-gated and
recorded in `conformance/known-additions.json`), an event→notification mapping
(`session.idle`→"Agent finished", `permission.asked`→"Permission needed",
`question.asked`→"Agent has a question"), and an FCM HTTP v1 dispatcher that fires only when
no SSE client is connected. Without `--fcm-service-account` / `OPCODE_FCM_SERVICE_ACCOUNT` the
relay is a no-op (registration still persists; no send; daemon + CI run clean). The store CRUD,
event mapping, no-client gating, per-device-per-session rate limit, unregistered-token pruning,
and the FCM JWT/send flow (against a stub) are unit-tested. The LIVE FCM send needs real
infra (a Firebase project + service-account key + a physical Android device with an FCM token),
so it is MANUAL-VERIFY:
- [ ] EYEBALL: create a Firebase project, download its service-account JSON, start
      `opcoded --fcm-service-account /path/key.json`; confirm the log says "push relay enabled".
- [ ] EYEBALL: register a real device token via `POST /push/register`, then with NO SSE client
      connected drive an agent to idle (or trigger a permission/question); confirm a push notification
      arrives on the Android device with the mapped title/body and a `data.session_id` that deep-links.
- [ ] EYEBALL: connect an SSE client (`GET /event` or `/global/event`) and repeat; confirm NO push is
      sent while a client is actively connected.
- [ ] EYEBALL: with a stale/invalid FCM token registered, trigger a push; confirm the daemon logs
      "pruning unregistered token" and the device row is removed (`GET /push/register` no longer lists it).

## Android push client (plan 07 Phase C — push)

The Android app now acquires its FCM token, registers it with the daemon relay
(`POST /push/register` with `{device_id, fcm_token, platform:"android"}`, re-register on FCM
token rotation, `DELETE /push/register/{deviceID}` when the active server is removed), renders
received pushes as notifications, and deep-links a notification tap to the relevant Chat
session (`data.session_id`). The whole path is gated on Firebase being configured for the build
(`PushConfig`): the FCM `Opcode42MessagingService` is `android:enabled="false"` in the manifest and
only enabled at runtime when a Firebase config is present, so the app builds and runs on the
no-`google-services.json` path (the CI path) with push as a clean no-op. The token-register body,
dedup/refresh/unregister logic, 404-as-success on DELETE, and the `{event_type, session_id}`
data-key contract are JVM-unit-tested (`:feature:notifications`, MockWebServer + fakes; no live
Firebase). LIVE send/receive needs real infra and is MANUAL-VERIFY:
- [ ] EYEBALL: create a Firebase project; drop its `google-services.json` into `android/app/`
      (NOT committed) and run the gms plugin's resource generation, OR add a private
      `firebase_config.xml` defining `firebase_application_id` / `firebase_api_key` /
      `firebase_project_id` (+ optional `firebase_messaging_sender_id`); build + launch the app and
      confirm push is now active (it logs "Registered push device with daemon").
- [ ] EYEBALL: with the daemon's FCM relay enabled (see the daemon section above) and NO SSE client
      connected, drive an agent to idle / trigger a permission or question; confirm a notification
      arrives on the device with the mapped title/body.
- [ ] EYEBALL: tap the notification; confirm the app opens (or comes to foreground) directly on the
      Chat screen for the pushed `session_id`.
- [ ] EYEBALL: remove the active server in Settings; confirm `DELETE /push/register/{deviceID}` fires
      and the device is no longer listed by `GET /push/register`.
- [ ] EYEBALL: on Android 13+ confirm the app requests the `POST_NOTIFICATIONS` runtime permission on
      first launch (only when push is configured) and that denying it does not crash the app.

## Plan 08d — Bubble Tea v2 migration (branch `track-p08d-tui-v2-spike`)
- [x] M0 spike (2026-06-04): `charm.land/.../v2` paths resolve; canvas compositor renders z-ordered
      layers. Throwaway `cmd/v2spike/` deleted at M1 start.
- [ ] EYEBALL (M1): run the real TUI under v2 (`pnpm --filter @forge/tui start`, or `forge-tui` against
      a daemon) in a true-color terminal. Confirm it's visually identical to pre-migration: composer has
      no trailing dark bar, sidebar/footer width unchanged, modal/permission/question cards are the same
      width (not 2 cols narrower), toasts show full untruncated text, completed tasks-dock todos still
      show struck-through, and splash + light/dark theme auto-pick work. Unit suite is green; this
      catches what goldens can't.
