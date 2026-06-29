# Plan 08 — Client: Go TUI (dogfood / test vehicle)

> **Purpose:** a lightweight Go TUI that dogfoods the Opcode42 Go SDK (plan 06) and exercises the
> conformance surface. It is a thin client over the opencode wire protocol, not a feature-complete
> product. It validates the SDK and daemon end-to-end and serves as the primary manual conformance
> probe for plan 12.

---

## Links

- [00 — Master plan](00-masterplan.md): wire-compat contract, architecture, sequencing
- [01 — Daemon core](01-daemon-core.md): transport, auth, instance routing
- [06 — SDK generation](06-sdk-generation.md): Go SDK generated from the OpenAPI spec
- [12 — Compatibility](12-test-compatibility.md): conformance harness
- [Design reference](../design/tui/): Claude design handoff — tokens, screens, components (high-fidelity; `design/tui/README.md`)

---

## Context

opencode's own TUI is a thin client: `opencode attach <url>` connects to a running daemon over
HTTP+SSE+WS-PTY. The TUI publishes actions to the daemon via `/tui/*` endpoints and listens for
events over the global SSE stream. It does not own any agent state. The Opcode42 Go TUI mirrors
exactly this pattern but written in Go using Bubble Tea (charmbracelet). Its job is not to be
polished — it is to break the Go SDK and daemon.

---

## opencode references validated (file:line)

### `opencode attach` command

`packages/opencode/src/cli/cmd/tui/attach.ts`:

- **Lines 9–46:** `command: "attach <url>"` — takes a positional URL, plus options:
  `--dir`, `--continue`/`-c`, `--session`/`-s`, `--fork`, `--password`/`-p`, `--username`/`-u`
- **Lines 48–68:** health-check via `validateSession`; resolves `--dir` by calling `process.chdir`
  (if the dir exists locally) or passing it through raw (remote attach case)
- **Lines 68–69:** auth headers built with `ServerAuth.headers({ password, username })`

### TUI SSE context

`packages/opencode/src/cli/cmd/tui/context/sdk.tsx`:

- **Line 34:** `retryDelay = 1000`
- **Line 35:** `maxRetryDelay = 30000`
- **Lines 66–68:** batch window: if last flush was `< 16 ms` ago, schedule a 16 ms timer; else
  flush immediately — identical to the web `server-sdk.tsx` logic
- **Lines 74–108:** `startSSE()` — calls `sdk.global.event()`, iterates the stream, calls
  `handleEvent` per event, catches abort; **lines 105–106:** exponential backoff:
  `Math.min(retryDelay * 2 ** (attempt - 1), maxRetryDelay)`
- **Lines 83–84:** `sseMaxRetryAttempts: 0` — SSE-level retries disabled; the outer while loop
  manages reconnect

### TUI sync store — bootstrap and event handling

`packages/opencode/src/cli/cmd/tui/context/sync.tsx`:

- **Lines 39–108:** store shape — `session[]`, `message[sessionID][]`, `part[messageID][]`,
  `provider[]`, `agent[]`, `config`, `permission[sessionID][]`, `question[sessionID][]`,
  `session_status[sessionID]`, `lsp[]`, `mcp{}`, `vcs`
- **Lines 127–131:** `listSessions()` — `sdk.client.session.list({ start: Date.now() - 30d, ...sessionListQuery() })`
- **Lines 133–373:** event switch — handles `permission.asked`, `question.asked`, `session.updated`,
  `session.deleted`, `message.updated`, `message.part.updated`, `message.part.delta`, `lsp.updated`,
  `vcs.branch.updated` using `Binary.search` sorted insertion / reconcile
- **Lines 378–478:** `bootstrap()` — parallel fetch of `config.providers`, `provider.list`,
  `app.agents`, `config.get`, `project.sync`, optional `session.list`; non-blocking follow-up
  for `command.list`, `lsp.status`, `mcp.status`, `formatter.status`, `session.status`,
  `provider.auth`, `vcs.get`
- **Lines 481–482:** `onMount(() => void bootstrap())` — called once on startup

### TUI `/tui/*` endpoints used by the web/desktop clients

`packages/sdk/js/src/gen/sdk.gen.ts` (verified present):

- `POST /tui/submit-prompt` (line 1086) — submit the current prompt
- `POST /tui/control/response` (line 1016) — answer a TUI control question (permission/question)
- `POST /tui/open-sessions` (line 1056) — signal the TUI to open the session list
- `POST /tui/append-prompt` (line 1032) — append text to the prompt buffer
- `POST /tui/publish` (line 1134) — emit a `tui.prompt.append` SSE event

The Go TUI does **not** use these endpoints — it drives the agent directly via the session API
(`POST /session`, `POST /session/{id}/prompt`) and manages its own UI state. The `/tui/*`
endpoints are noted for completeness; they are used by the TS TUI renderer when acting as a
server-controlled UI.

### Global sync event-reducer (shared logic)

`packages/app/src/context/global-sync/event-reducer.ts`:

- **Lines 21–48:** `applyGlobalEvent()` — handles `global.disposed`, `server.connected`,
  `project.updated`
- **Lines 93–382:** `applyDirectoryEvent()` — full switch on event type with binary-search
  sorted-insert/update/remove semantics

The Go TUI's `Reduce(model Model, event SseEvent) Model` pure function mirrors this switch
statement exactly, using `sort.Search` for sorted insertion.

---

## Design

### Bubble Tea model/update/view

The TUI follows Bubble Tea's canonical architecture:

```
type Model struct {
    // Connection
    ServerURL    string
    Directory    string
    ConnState    ConnState  // Connecting | Connected | Reconnecting | Error

    // Store (mirrors TUI sync.tsx store)
    Sessions     []Session                   // sorted by ID
    Messages     map[string][]Message        // sessionID → sorted
    Parts        map[string][]Part           // messageID → sorted
    Permissions  map[string][]PermReq        // sessionID → sorted
    Questions    map[string][]QuestionReq    // sessionID → sorted
    SessionStatus map[string]SessionStatus

    // UI state
    ActiveScreen Screen     // SessionList | Chat | Terminal
    SelectedSID  string
    Input        textinput.Model
    Viewport     viewport.Model
    List         list.Model
    Terminal     *TerminalModel  // nil if no PTY open
    Spinner      spinner.Model
    Err          error
}

type Screen int
const (
    ScreenSessionList Screen = iota
    ScreenChat
    ScreenTerminal
    ScreenPermission
    ScreenQuestion
)
```

**Message types (Bubble Tea Msg):**

```go
type SseEventMsg     struct { Event SseEvent }
type ConnectedMsg    struct{}
type ReconnectMsg    struct{ Attempt int }
type ErrorMsg        struct{ Err error }
type BootstrapDoneMsg struct {
    Sessions []Session
    // ...
}
type PromptSentMsg   struct{ SessionID string; MessageID string }
type TermDataMsg     struct{ Data []byte }
```

**Update function:**

```go
func Update(msg tea.Msg, m Model) (Model, tea.Cmd) {
    switch msg := msg.(type) {
    case SseEventMsg:
        m = Reduce(m, msg.Event)   // pure reducer
        return m, nil
    case ConnectedMsg:
        m.ConnState = Connected
        return m, bootstrapCmd(m)
    case ReconnectMsg:
        delay := min(1000 * (1 << msg.Attempt), 30000)  // mirrors sdk.tsx:105-106
        return m, reconnectAfterCmd(delay, msg.Attempt)
    case tea.KeyMsg:
        return handleKey(msg, m)
    // ...
    }
}
```

**View function:**

```go
func View(m Model) string {
    switch m.ActiveScreen {
    case ScreenSessionList: return renderSessionList(m)
    case ScreenChat:        return renderChat(m)
    case ScreenPermission:  return renderPermissionOverlay(m)
    case ScreenQuestion:    return renderQuestionOverlay(m)
    case ScreenTerminal:    return renderTerminal(m)
    }
    return ""
}
```

### SSE goroutine → Bubble Tea msgs

Bubble Tea's `Program.Send(msg)` is goroutine-safe. The SSE goroutine runs outside the Bubble Tea
event loop and sends messages into it:

```go
func sseCmd(ctx context.Context, sdk *opcode42client.Client, program *tea.Program) tea.Cmd {
    return func() tea.Msg {
        attempt := 0
        for {
            if ctx.Err() != nil { return nil }
            err := connectAndStream(ctx, sdk, program, &attempt)
            if errors.Is(err, context.Canceled) { return nil }
            delay := min(1000 * (1 << attempt), 30000)
            attempt++
            time.Sleep(time.Duration(delay) * time.Millisecond)
            program.Send(ReconnectMsg{Attempt: attempt})
        }
    }
}

func connectAndStream(ctx context.Context, sdk *opcode42client.Client, program *tea.Program, attempt *int) error {
    stream, err := sdk.Global.Event(ctx)
    if err != nil { return err }
    defer stream.Close()

    *attempt = 0   // reset on successful connection
    program.Send(ConnectedMsg{})

    heartbeat := time.NewTimer(15 * time.Second)
    defer heartbeat.Stop()

    batchTicker := time.NewTicker(16 * time.Millisecond)
    defer batchTicker.Stop()

    var batch []SseEvent
    for {
        select {
        case event, ok := <-stream.Events():
            if !ok { return fmt.Errorf("stream closed") }
            heartbeat.Reset(15 * time.Second)   // mirrors resetHeartbeat() in server-sdk.tsx:106-112
            batch = append(batch, event)
        case <-batchTicker.C:
            if len(batch) > 0 {
                for _, ev := range batch {
                    program.Send(SseEventMsg{Event: ev})
                }
                batch = batch[:0]
            }
        case <-heartbeat.C:
            return fmt.Errorf("heartbeat timeout")  // triggers reconnect
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

### SDK usage

All REST operations go through the generated Go SDK from plan 06:

```go
// Bootstrap (mirrors sync.tsx:378-478)
sdk.Session.List(ctx, &SessionListParams{Start: startMs})
sdk.Config.Providers(ctx, nil)
sdk.Provider.List(ctx, nil)
sdk.App.Agents(ctx, nil)
sdk.Config.Get(ctx, nil)

// Submit prompt
sdk.Session.Prompt(ctx, sessionID, &PromptParams{
    Parts: []PartInput{{Type: "text", Text: inputText}},
})

// Reply to permission
sdk.Permission.Reply(ctx, requestID, &PermissionReply{Allowed: true})

// Reply to question
sdk.Question.Reply(ctx, requestID, &QuestionReply{Answer: answer})
```

---

## Feature scope (minimal viable)

The TUI is explicitly **not** a full product. Scope is limited to what exercises the conformance
surface:

| Feature | In scope | Notes |
|---|---|---|
| Connect by URL + auth (Basic) | Yes | `--url`, `--password`, `--username` flags |
| `--dir` / directory routing | Yes | Sets `x-opencode-directory` header |
| Session list screen | Yes | List, select, create new |
| Chat screen: display messages + parts | Yes | Text, reasoning, tool (all states), file (as name) |
| Streamed part updates (delta + updated) | Yes | Core conformance surface |
| Prompt input + submit | Yes | |
| Permission prompt (blocking overlay) | Yes | Approve/deny |
| Question prompt (blocking overlay) | Yes | Free-text answer |
| Optional PTY pane | Yes (stretch) | WS-PTY; use a terminal widget |
| Session forking / archiving | No | Deferred |
| File attachment in prompt | No | Deferred |
| Diff viewer | No | Deferred |
| Provider/model selector | No | Deferred |
| Push notifications | No | Not applicable for TUI |

---

## Implementation milestones

| Milestone | Deliverable |
|---|---|
| T1 | Project scaffold: Go module, Bubble Tea dependency, flags parsing (`--url`, `--password`, `--dir`, `--session`) |
| T2 | Go SDK client construction + auth interceptor (Basic header); `GET /global/health` health check; exit with error on auth failure |
| T3 | Bootstrap: `session.list`, `config.get`, `provider.list`, `app.agents` in parallel; display loading spinner; store in Model |
| T4 | Session list screen: render with `list.Model` from Bubble Tea; keyboard navigation; select to enter Chat |
| T5 | SSE goroutine: `connectAndStream`, heartbeat (15 s), batch (16 ms), exponential backoff, `ReconnectMsg` |
| T6 | `Reduce(model, event)` — implement full event switch: `session.updated/deleted/created`, `message.updated`, `message.part.updated`, `message.part.delta`, `permission.asked/replied`, `question.asked/replied` |
| T7 | Chat screen: `viewport.Model` rendering of messages + parts (text, reasoning, tool states); auto-scroll on new content |
| T8 | Prompt input: `textinput.Model`; submit with Enter; optimistic message in Model; `POST /session/{id}/prompt` |
| T9 | Permission overlay: non-dismissible view over Chat; `y`/`n` or `Enter`/`Esc` keys; `POST /permission/{id}/reply` |
| T10 | Question overlay: text input; `POST /question/{id}/reply` |
| T11 | (Stretch) PTY pane: WS connection, raw byte I/O, simple ANSI terminal rendering |
| T12 | Integration test: run TUI against real opencode daemon; run identical scenario against Opcode42 daemon; compare outputs |

---

## Testing

The TUI is primarily a **manual conformance probe**. Automated testing is lightweight:

### Unit tests

- `Reduce(model, event)` pure function: table-driven tests covering all event types. No UI or
  network. Run with `go test ./internal/store/...`.
- Auth header construction: verify `Authorization: Basic <base64(user:pass)>`.
- URL normalization: trailing-slash strip, `http://` prefix injection.

### Integration / conformance tests

The TUI's integration test is the main value. A test harness:

1. Starts the real `opencode serve` daemon on a random port.
2. Runs the Go TUI in `--headless` mode (outputs structured JSON instead of terminal UI) against
   that daemon.
3. Injects a prompt via stdin; waits for agent completion (observing `session.status` SSE events).
4. Records the observed SSE event sequence and final state.
5. Repeats steps 1–4 with the Opcode42 Go daemon on the same port.
6. Diffs the event sequences and final states; asserts they are functionally equivalent.

This is the TUI-layer face of plan 12's conformance harness.

### Manual conformance probe checklist

Run by a developer before each daemon milestone:

- [ ] `opencode serve` → TUI connects, session list loads, no errors
- [ ] Submit prompt → message appears optimistically → parts stream in real time
- [ ] Tool call visible with `running` state → transitions to `completed`
- [ ] Permission prompt appears → approve → agent continues
- [ ] Disconnect network → wait 16 s → reconnect → TUI reconnects, state is consistent
- [ ] Opcode42 daemon (same checklist)

---

## Verification

1. **Health check:** `./opcode-tui --url http://localhost:4096 --password secret` — should display
   session list within 2 s, no errors.
2. **Auth failure:** wrong password → should display `401 Unauthorized` and exit cleanly.
3. **Live streaming:** submit a prompt → observe text and tool parts appearing character by
   character (delta events). Confirm heartbeat reconnect fires after 15 s of simulated silence.
4. **Permission flow:** trigger a permission-requiring tool → overlay appears → approve →
   `permission.replied` event arrives → overlay dismisses → agent continues.
5. **Cross-daemon equivalence:** same session + same prompt against both daemons produces the
   same rendered output (modulo timing).
6. **Exponential backoff:** inject repeated connection failures; verify delay sequence is 1 s,
   2 s, 4 s, 8 s, …, capped at 30 s.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Bubble Tea terminal rendering quirks on non-standard terminals | Low | Target xterm/iTerm2; test with `$TERM=dumb` for CI headless mode |
| Go SDK not yet generated (depends on plan 06) | Medium | Milestone T2 can be unblocked with a hand-written HTTP client; swap to generated SDK when available |
| SSE goroutine / Bubble Tea `Send` race conditions | Low | `tea.Program.Send` is documented as goroutine-safe; verify with `-race` flag in CI |
| WS-PTY framing edge cases (control frame 0x00 prefix) | Medium | T11 is stretch; test framing unit against a known-good opencode PTY session before enabling |
| TUI becomes a distraction from the Go daemon | Low | Strictly timeboxed; if T1–T10 are not done in 2 sprints, cut T11 and go straight to conformance |

---

## Build-program addendum — full-design TUI (2026-05-30, user-approved)

This refines the milestone table above. The original T1–T12 describe a *minimal conformance
probe*; the project now builds the **full design handoff** (`design/tui/`, high-fidelity) using the
Bubble Tea architecture above as the engine. Recorded per the "update a plan only if it contradicts
reality, and say so explicitly" rule.

**Decisions locked with the user:**
1. **Scope = the full design, phased.** Splash, rich conversation stream (user/thinking/markdown/
   tool-rows/diff/write/bash/todos/sub-agent/summary blocks + streaming cursor), sidebar, status
   bar, persistent tasks board, slash/@/`ctrl+x`-leader input, and the seven command modals. The
   "Feature scope (minimal viable)" table above is the fallback floor, not the target.
2. **Target opencode now, Opcode42-ready.** Develop against the **real opencode daemon** (full wire
   surface, zero gaps) for fast UI iteration, keeping everything wire-generic so the URL flips to
   Opcode42 anytime. A **parallel Opcode42 gap-closing track** wires the endpoints the design needs that
   Opcode42 currently 501s: `GET /provider`, `GET /agent`, `POST /permission/:id/reply` +
   `GET /permission`, `POST /question/:id/reply` + `GET /question` (the permission/question managers
   already exist; only the HTTP surface is missing).
3. **Consume the generated plan-06 Go SDK.** Build the **Go SDK (plan 06) first** as the prerequisite
   (oapi-codegen REST client + hand-written SSE/WS-PTY clients + a `createOpcode42Client`-style wrapper
   doing auth + `x-opencode-directory` injection). The TUI's client layer (U2) consumes it rather
   than hand-rolling.

**Sequenced program:**
- **Prereq — Plan 06 (Go SDK):** generated REST client + hand-written SSE consumer + WS-PTY + auth/
  directory wrapper; smoke-tested against a running opencode daemon.
- **Phase 0 — foundation:** `U0` scaffold (`cmd/opcode-tui` + `internal/tui/`, Bubble Tea/Lipgloss,
  flags) · `U1` theme (lift `design/tui/styles.css` `:root` tokens → Lipgloss palette + styles,
  density variants, truecolor→256 degrade; unit-tested) · `U2` SDK client wiring + health + auth.
- **Phase 1 — hero conversation stream:** `U3` Model/Update/View + screens + SSE goroutine →
  msgs · `U4` `Reduce(model,event)` pure reducer (table-tested) · `U5` block renderers (all design
  block types) · `U6` composer + submit (optimistic, auto-scroll). → a streaming coding turn renders
  matching the design.
- **Phase 2 — chrome + navigation:** `U7` status bar + sidebar · `U8` the seven command modals
  (palette/model/agent/theme/session/timeline/status; needs `/provider`,`/agent`) · `U9` slash
  autocomplete + @-mention picker + `ctrl+x` leader keys.
- **Phase 3 — interactive + board:** `U10` permission + question overlays (reply endpoints) ·
  `U11` tasks board dock (reads `tasks.md`/issues) · `U12` (stretch) PTY pane · `U13` conformance
  parity: identical scenario vs opencode and Opcode42.
  - **Status (2026-05-31): Phase 3 done.** `U10` permission overlay (#24) + question overlay (#25);
    `U11` tasks dock (#26); `U12` — the WS-PTY *transport* (SDK client, #27) is built + live-smoked,
    the interactive in-TUI VT pane is the remaining stretch (needs a VT emulator dep); `U13` —
    extended the plan-12 conformance suite with the TUI's read surface (`/agent`, `/session/:id/todo`,
    `/session/:id/message`), all recording deterministically. `GET /command` is excluded (opencode
    returns it in non-deterministic order). Full TUI↔Opcode42 dual-run parity is gated on the parallel
    Opcode42 gap-closing track implementing those endpoints (Opcode42 currently 501s `/agent`, `/provider`,
    permission/question replies, `/find/file`, `/pty`).
- **Parallel — Opcode42 gap-closing:** wire the four endpoint families above so the TUI flips from
  opencode to Opcode42 cleanly (each small; the engine managers already exist).

---

## Review pass (2026-06-03) — the gap-closing track has landed; status was stale

User-owned client spec, so this is a light touch — but the Status note above is **out of date** and
the correction is high-value:

- **The "Opcode42 currently 501s `/agent`, `/provider`, permission/question replies, `/find/file`,
  `/pty`" claim is no longer true.** All are wired: `/agent`+`/provider`
  (`internal/server/resource_handlers.go:14,16`), `POST /permission/{id}/reply`
  (`permission_handlers.go:16`), `POST /question/{id}/reply` (`question_handlers.go:15`),
  `GET /find/file` (`find_handlers.go:30`), `GET`/`POST /pty` (`pty_handlers.go:27-28`). The
  parallel "Opcode42 gap-closing track" is effectively **done** — so full TUI↔Opcode42 dual-run parity is
  no longer blocked on the HTTP surface; it is now blocked only on plan 02 reaching conformance-green
  (M11) and the LSP/MCP-mutation endpoints.
- **The TUI is substantially built** (`internal/tui/` ≈ 35 Go files; Phases 0–3 per the addendum),
  **not a stub.** Any roadmap that lists plan 08 as "not started" is wrong.
- **Genuinely remaining:** `U12` the in-TUI VT pane (the WS-PTY *transport* exists; the interactive
  terminal pane needs a VT-emulator dependency) and the `GET /command` conformance exclusion
  (opencode returns commands in non-deterministic order — keep this as a recorded divergence, and
  decide whether Opcode42 sorts deterministically as a known-addition).
- **Validation:** the `--headless` structured-JSON mode + the extended plan-12 read-surface
  recordings are the right approach; just re-point the dual-run now that the endpoints exist.

## U13 landed (2026-06-04) — TUI↔Opcode42 dual-run parity gate

`U13` is done. The TUI was already wire-generic (it spawns `opcoded`/connects to any
HTTP+SSE daemon via the `--url` flag; default `http://127.0.0.1:4096`), so "re-pointing"
needed no client change — every endpoint the TUI calls (`/session`, `/session/:id/message`,
`/permission/:id/reply`, `/question/:id/{reply,reject}`, `/session/:id/{abort,summarize,fork}`,
`/agent`, `/provider`, `/command`, `/find/file`, `/pty`) is now served by Opcode42, and the SSE
event types it reduces (`message.part.{updated,delta}`, `permission.{asked,replied}`,
`question.{asked,replied,rejected}`, `session.{updated,deleted}`) match the daemon's emitter
field-for-field (incl. the `requestID` field on replied events).

The parity gate itself lives TUI-side as `internal/tui/opcode42_e2e_test.go`: it boots the REAL
`internal/server` handler wired to the agent engine + a deterministic mock provider (no LLM key,
CI-safe) behind `httptest`, points the real TUI `Model.Update` loop at it, and asserts the core
flows work end-to-end against **Opcode42** over the real wire — health + global SSE subscribe,
session create, prompt → streamed message/part SSE rendered into the store + view, the blocking
permission round-trip (real `permission.asked` → overlay → `POST /permission/:id/reply` →
daemon `Ask()` unblocks), and abort. This complements the existing plan-12 read-surface
recordings (which dual-run the TUI's GET surface vs opencode). The full-LLM dual-run remains
`scripts/run-conformance.sh live` (skip-gated on a provider key) and is NOT depended on here.

No new known-divergence: the TUI consumes only existing endpoints. `GET /command`'s
non-deterministic ordering exclusion is unchanged.

## U12 finished + `/command` ordering resolved (2026-06-04) — plan-08 polish closed

This closes the two items the 2026-06-03 review flagged as "genuinely remaining".

- **`U12` the in-TUI VT pane is DONE.** The interactive embedded terminal landed in #80
  (`internal/tui/ptypane.go`): a `vt10x` (`github.com/hinshun/vt10x`, the plan's spike — a stable
  cell-grid emulator chosen over the untagged `charmbracelet/x/vt`) virtual screen fed by the PTY
  WebSocket, rendered as a bottom split, opened/focused via `ctrl+x ``` (also the palette
  "Terminal (PTY)" / `/terminal`), keystrokes forwarded as raw bytes, generation-stamped so frames
  from a reopened pane can't corrupt the grid, reflow + `PUT /pty/{id}` on resize. This change adds
  the #80 follow-up — **text-attribute rendering**: `renderGrid` now decodes the vt10x glyph mode
  (`Glyph.Mode`, the fixed `attr*` bit layout from vt10x `state.go`) into bold/underline/italic and
  applies them via Lipgloss, and an SGR-reverse cell swaps fg/bg (composing with the cursor-reverse).
  The run-batching key is now the full cell styling (color + attributes). Scrollback remains a
  follow-up. So **no new TUI dependency is added** — `vt10x` was already vendored by #80.
- **`GET /command` ordering: implemented per masterplan decision #6.** Opcode42 already sorts commands
  by name (`internal/resource/command.go:50`) — a deterministic known-addition. This change turns
  `/command` from a conformance *exclusion* into an **order-insensitive parity scenario**
  (`command-list`): the harness set-normalizes the `/command` body (`orderInsensitiveListPath` in
  `conformance/client.go`, reusing `NormalizeSetJSON`), so opencode's non-deterministic order and
  Opcode42's sorted order compare equal while a genuinely missing/extra command still fails. The
  self-diff gate (opencode-vs-opencode) now covers `/command`. The `command-list` known-divergence
  note is updated: ordering is no longer a divergence; the remaining (opcode42-vs-opencode-only)
  divergence is that opencode's command SET is a superset (built-in/MCP/skill commands Opcode42 doesn't
  surface yet).
  - **Not recorded in `known-additions.json`** by design: that registry lists *additive operations*
    (endpoints absent from the frozen reference, consumed by the OpenAPI drift gate). `/command` is a
    reference endpoint; listing it there would falsely mark it Opcode42-only and weaken the drift gate.
    The deterministic-sort is a *behavioral* known-addition, recorded via the order-insensitive
    scenario + the `command-list` divergence note + masterplan decision #6 itself.
