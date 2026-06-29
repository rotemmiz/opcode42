# Plan 08a — TUI feature parity: implementable now

> **Scope.** Gaps between the Opcode42 TUI (`internal/tui/`, Go/Bubble Tea) and opencode's TUI
> (`packages/opencode/src/cli/cmd/tui/`, TypeScript/opentui) that need **no new subsystem** — each is
> either an existing wire-compat endpoint or pure client-side UI. The architecturally-heavy gaps
> (diff viewer, PTY pane, workspaces, provider auth, plugin host, stash) live in **plan 08b**.
>
> **Framing.** The TUI is the masterplan's **dogfood / conformance vehicle**, not the primary client
> (Android is). The goal here is **not** to match opencode's ~167 keybinds; it is to (a) close the
> cheap wire-coverage gaps so the TUI exercises more of the frozen contract, and (b) add the handful
> of navigation/ergonomics features that make dogfooding bearable. Items are ordered by
> *conformance value × cheapness*.

## Links
- Parent: `plans/08-client-tui.md` (T1–T12 milestones; T11 PTY = plan 08b).
- Reference TUI: `/Users/rotemmiz/git/opencode/packages/opencode/src/cli/cmd/tui/`.
- Frozen contract: `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json`.
- Sibling: `plans/08b-tui-parity-planned.md`.

## Opcode42 TUI extension points (where everything plugs in)
Verified against the current tree (`internal/tui/`, ~4.7k LOC):
- **`modal.go`** — `modalKind` enum (`modalNone/Palette/Sessions/Models/Agents/Themes/Timeline/Status`),
  `paletteAction` enum, `paletteItems []paletteCmd`. New overlays + palette entries are added here.
- **`model.go`** — Bubble Tea `Update` (key routing) + `View` (compose panes/overlays).
- **`store.go`** — `Reduce(model, event)` SSE switch (`session.*`, `message.*`, `permission.*`,
  `question.*`). New server-pushed state lands here.
- **`conn.go`** — async `tea.Cmd`s wrapping the Go SDK (`opcode42client "github.com/rotemmiz/opcode42/sdk/go"`).
- **`sdk/go/`** — thin client: generic `GetJSON`/`PostJSON`/`Delete` + typed `CreatePTY`/`ConnectPTY`/
  `Events`/`Health`. New session ops are 5-line typed wrappers over `PostJSON`/`Delete`.
- **`slash.go`** — slash-command registry (`/new /sessions /models /agents /themes /timeline /status
  /init /command`). New slash verbs registered here.
- **`chrome.go`** — status bar + right sidebar. **`bootstrap.go`** — parallel startup loads.

Pattern for every endpoint-backed item: **(1)** add a typed method to `sdk/go`, **(2)** wrap it in a
`tea.Cmd` in `conn.go`, **(3)** trigger from a palette entry / slash verb / keybind in `model.go`,
**(4)** reconcile the resulting `session.updated` / `message.*` SSE in `store.go` (most ops already
emit events the reducer handles, so the UI updates for free).

---

## A. Session operations (wire-coverage — highest conformance value)
opencode binds these as `session_*` keybinds (`config/keybind.ts`) and dialogs
(`component/dialog-session-rename.tsx`, etc.); each maps to a frozen endpoint. The Opcode42 **Android**
client already implements all of them (PRs #72/#74) — this is porting the same SDK calls to Go.

| Op | Endpoint (verified in openapi.json) | opencode ref | Opcode42 TUI work |
|---|---|---|---|
| **Rename** | `PATCH /session/{id}` `{title}` → `Session` | `dialog-session-rename.tsx`, keybind `session_rename` | new `dialog-prompt`-style overlay (reuse `modalSelect` text input) → `SDK.RenameSession` → `session.updated` reducer already updates the title |
| **Share / Unshare** | `POST` / `DELETE /session/{id}/share` → `Session` (`share.url`) | `session_share` / `session_unshare` | palette entries; on share, toast the `share.url` + copy to clipboard (`util/clipboard` analog) |
| **Summarize / compact** | `POST /session/{id}/summarize` `{providerID,modelID,auto?}` → bool | `session_compact` | palette entry; reuse the **effective model** (current stream model or the in-TUI model switch). A `message.updated` summary marker arrives via SSE |
| **Abort / interrupt** | `POST /session/{id}/abort` → bool | `session_interrupt`; prompt double-`esc` (`prompt/index.tsx:478-489`) | bind `esc`-while-busy + palette "Interrupt"; mirror opencode's *2× esc within 5s* gesture |
| **Fork** | `POST /session/{id}/fork` → `Session` | `session_fork`, `dialog-fork-from-timeline.tsx` | palette "Fork session" → navigate to new session id. (Fork-*from-timeline* anchored at a message is a stretch — see note) |
| **Delete** | `DELETE /session/{id}` → bool | `session_delete`, `dialog-session-delete-failed.tsx` | palette "Delete session" + confirm overlay; `session.deleted` reducer exists |
| **Message delete** | `DELETE /session/{id}/message/{messageID}` → … | (timeline) | from timeline/message overlay |

**Effort:** ~1–2 days for the whole bundle (each op is a thin wrapper + a palette line; the
reducers already exist). **Conformance value: high** — adds rename/share/summarize/abort/delete to
the endpoints the TUI exercises in a dual-run.

**Fork-from-timeline note:** opencode's `dialog-fork-from-timeline.tsx` forks anchored at a selected
message. Plain fork (no anchor) is trivial now; anchored fork needs the message-navigation cursor
from section C first — do plain fork now, anchored fork after C.

---

## B. `!` bang shell (Claude-Code-style one-shot shell)
**The single highest-value new endpoint.** opencode: pressing `!` at cursor-0 flips the composer to
`mode: "shell"` (`prompt-input.tsx:1160` / TUI `prompt/index.tsx:901-902`); submit calls
`sdk.client.session.shell(...)` (`prompt/index.tsx:1143`) → **`POST /session/{id}/shell`** with
`ShellInput` (`session/prompt.ts:1709`):

```
{ command: string, agent: string, model?: {providerID,modelID}, messageID? }
```

Server (`session/prompt.ts:496` `shellImpl`) writes a synthetic **user** message
("The following tool was executed by the user") + an **assistant** message running the command as a
tool — so the output streams into the conversation via the same `message.part.*` events the TUI
already reduces.

**Opcode42 TUI work:**
1. `composer.go` — add a `mode` field (`normal|shell`); `!` at an empty/at-start buffer toggles
   `shell`; `esc` exits. Render a `!` prefix/indicator (opencode shows a shell glyph + theme accent).
2. `sdk/go` — `ShellCommand(ctx, sessionID string, in ShellInput) (Message, error)` over `PostJSON`.
3. On submit-in-shell-mode, dispatch the cmd with the effective agent + model; **no new render path** —
   output arrives as normal tool parts (Opcode42 already renders `tool` parts in all states).

**Effort:** ~0.5 day. **Conformance value: high** (new endpoint + the synthetic-message shape).
**Neither Opcode42 client (TUI nor Android) has this yet** — see the Android backlog note at the end.

---

## C. Message navigation & yank (pure client UI — ergonomics)
opencode's richest keymap cluster (`messages_*` in `config/keybind.ts`): `messages_next/previous`,
`messages_line_up/down`, `messages_page_up/down`, `messages_half_page_*`, `messages_first/last`,
`messages_last_user`, `messages_copy`, `messages_toggle_conceal`. All **viewport-local — no endpoint**.

**Opcode42 TUI work (in `render.go` + `model.go`):**
1. A **message cursor** (selected message index) over the rendered timeline; vim-style keys
   (`j/k`, `ctrl+d/u`, `g/G`, `n/p`) move it; highlight the selected block.
2. `messages_copy` (`y`) — copy the selected message/part text to the system clipboard
   (add a `util/clipboard` Go helper: OSC-52 for terminals, or `pbcopy`/`wl-copy` fallback;
   opencode uses `util/clipboard.ts`).
3. `messages_last_user` — jump to the last user turn (handy after a long agent run).

**Effort:** ~1–1.5 days (the cursor/scroll model is the bulk). **Conformance value: none** (pure UX),
but it's the difference between a demo and a usable dogfood client.

---

## D. Display toggles (client render flags)
opencode `session_toggle_timestamps`, `session_toggle_generic_tool_output`, `tool_details`,
`display_thinking` (`context/thinking.ts`), `messages_toggle_conceal`, `app_toggle_diffwrap`,
`app_toggle_file_context`. All flip a **client render flag** — no endpoint.

**Opcode42 TUI work:** a `viewState` struct of bools threaded into `render.go`; palette toggles +
keybinds. Opcode42 already renders reasoning/tool parts, so these gate existing render branches:
- **timestamps** on/off per message header,
- **generic tool output** collapse (opencode `util/collapse-tool-output.ts` — collapse noisy
  read/grep/glob output to a one-line summary; Opcode42 has the grouping seed in `render.go`),
- **thinking/reasoning** show/hide,
- **tool details** expand/collapse the selected tool part.

**Effort:** ~1 day. **Conformance value: none** (UX). Bundle with C (both touch `render.go`).

---

## E. Help / which-key overlay
opencode: `ui/dialog-help.tsx` (`help_show`) + `feature-plugins/system/which-key.tsx` (live
leader-key hint popup) + `POST /tui/open-help`. Opcode42 has a `ctrl+x` leader (`model.go`) but no
discoverability.

**Opcode42 TUI work:** a `modalHelp` overlay listing keybinds/commands (static, generated from the
keybind table); optional which-key hint strip after the leader is pressed. **No endpoint** (the
`/tui/open-help` route is for *external* control of the TUI — out of scope here).

**Effort:** ~0.5 day. **Conformance value: none**, but near-zero cost and big usability win.

---

## F. Editor open (`$EDITOR` / `$VISUAL`)
opencode `editor_open` (`context/editor.ts`, `context/editor-zed.ts`) opens the composer buffer (or a
file) in the user's editor and reads it back. Pure client feature.

**Opcode42 TUI work:** `util/editor.go` — write buffer to a tempfile, `exec.Command($EDITOR)`,
Bubble Tea `tea.ExecProcess` (suspends the TUI), read back on exit into the composer. **Effort:**
~0.5 day. **Conformance value: none.** Optional.

---

## G. Read-only resource dialogs (cheap wire-coverage)
opencode dialogs over read endpoints: `dialog-mcp.tsx` (`GET /mcp`, `mcp_list`), `dialog-skill.tsx`
(`GET /skill`, `prompt_skills`). Opcode42 has `/agents`, `/models`, `/themes`, `/status` dialogs — same
pattern, new data sources.

**Opcode42 TUI work:** two `modalSelect`-style list overlays + `/mcp` and `/skills` slash verbs →
`SDK.ListMCP` (`GET /mcp`) and `SDK.ListSkills` (`GET /skill`). Read-only (connect/auth flows are
plan 08b). **Effort:** ~0.5 day. **Conformance value: medium** (adds `GET /mcp`, `GET /skill` to the
dual-run surface).

---

## H. Prompt history + frecency, model favorites/recent, agent cycle (local-state ergonomics)
opencode: prompt **history** (`component/prompt/history.tsx`, `history_next/previous`) + **frecency**
ranking (`component/prompt/frecency.tsx`); **model** `model_cycle_recent`/`model_cycle_favorite` +
`model_favorite_toggle`; **agent** `agent_cycle`/`agent_cycle_reverse`. All backed by a **local KV**
(`context/kv.tsx`) — no endpoint.

**Opcode42 TUI work:** a small on-disk KV (JSON under the config dir; opencode keys e.g.
`diff_viewer_single_patch`, favorites). Then: ↑/↓ history in the composer; `model_cycle_recent` to
flip models without opening the modal; `agent_cycle` likewise; star/recent ordering in the existing
model/agent modals. **Effort:** ~1.5 days (the KV + wiring across composer/model/agent).
**Conformance value: none** (UX), but high daily value for dogfooding.

---

## I. Notifications / attention bell
opencode `feature-plugins/system/notifications.ts` + `attention.ts` + `util/audio.ts`: ring the
terminal bell / desktop notification when the agent finishes or asks a permission while unfocused.
**Opcode42 TUI work:** on `session.idle`-equivalent (turn finished) or `permission.asked`/`question.asked`
while the terminal is unfocused, emit `\a` (BEL) and/or an OSC-9 desktop notification. **Effort:**
~0.5 day. **Conformance value: none.** Optional polish.

---

## Milestones (suggested order)
| # | Deliverable | Bucket | Est |
|---|---|---|---|
| N1 | `sdk/go` typed session ops (rename/share/unshare/summarize/abort/fork/delete/msg-delete) + `conn.go` cmds | A | 1d |
| N2 | Palette entries + confirm/rename overlays wired to N1; reducer check | A | 0.5d |
| N3 | `!` bang shell — composer mode + `session.shell` SDK + indicator | B | 0.5d |
| N4 | Message cursor + vim nav + `messages_copy` (clipboard helper) | C | 1.5d |
| N5 | Display toggles (timestamps/tool-output/thinking/details) | D | 1d |
| N6 | Help / which-key overlay | E | 0.5d |
| N7 | `/mcp` + `/skills` read-only dialogs | G | 0.5d |
| N8 | Local KV + prompt history + model/agent cycle + favorites | H | 1.5d |
| N9 | Editor-open + notifications/bell | F, I | 1d |

**Critical path for conformance:** N1→N2→N3 and N7 (they add endpoints to the dual-run). N4–N9 are
ergonomics and can be deferred or dropped without affecting the conformance mandate.

## Testing
- **Unit (Go, table-driven like existing `*_test.go`):** session-op `tea.Cmd`s return the right
  message on success/failure; shell-mode toggle + submit payload shape; message-cursor movement math;
  toggle-flag rendering; clipboard helper escaping; KV round-trip; history ring.
- **Conformance (plan 12 dual-run):** record each new endpoint (`PATCH /session/{id}`,
  `/share`, `/summarize`, `/abort`, `/shell`, `GET /mcp`, `GET /skill`) against real opencode and
  diff request/response shapes. The `/shell` synthetic-message shape is the key probe.
- **Manual probe:** rename → title updates; share → URL toast + copy; `!ls` → tool output in stream;
  `j/k/y` nav + copy; toggles flip; `/mcp` lists servers.

## Out of scope (→ plan 08b)
Diff viewer, PTY pane (T11), workspace management, provider connect/OAuth, TUI plugin host, stash,
variant, tag, dedicated sub-agent UX, and opencode's pure-flourish items (bg-pulse, logo art,
heap-snapshot/debug console).

## Cross-client note
The **`!` bang shell (section B) is also missing from Opcode42 Android** — same `POST /session/{id}/shell`
endpoint. If built for the TUI, mirror it into the Android composer (an `!`-prefix mode) as a small
follow-up; it's the cheaper, more broadly-useful cousin of the PTY pane.
