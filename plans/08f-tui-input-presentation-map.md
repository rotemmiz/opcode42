# Plan 08f — TUI input & presentation map (opencode → opcode42)

> **Purpose.** A single source-grounded reference for **how opencode's TUI handles every
> input from the user and how it presents every piece of data back**, plus the delta to the
> current Opcode42 TUI (`internal/tui/`, ~26.5k LOC, Phases 0–3 + 08a–08e shipped) and the
> concrete closures that bring it to full opencode-TUI parity. This is the map the sibling
> plans (08, 08a–08e) were missing: they name the features but do not enumerate the full
> input/presentation surface. Build from this when implementing any remaining parity item.
>
> **Method.** Every claim below is grounded in the opencode TUI source at
> `/Users/rotemmiz/git/opencode/packages/tui/src/` (file:line cited) and cross-checked
> against the current Opcode42 TUI at `/Users/rotemmiz/git/opcode42_1/internal/tui/`. Where
> Opcode42 already matches, the row says **parity**; where it diverges, the row names the
> gap and points at the closure section (§G).
>
> **Status of the foundation.** The Opcode42 TUI is substantially built — see the 08/08a–e
> status notes. This plan does **not** re-open shipped work; it documents it and fills the
> remaining gaps the sibling plans did not enumerate.

## Links

- **Parent / spec:** `plans/08-client-tui.md` (the TUI spec; cross-reference added there).
- **Feature parity:** `plans/08a-tui-parity-now.md`, `plans/08b-tui-parity-planned.md`
  (both SHIPPED).
- **Visual parity:** `plans/08c-tui-visual-parity.md` (SHIPPED).
- **v2 migration:** `plans/08d-tui-bubbletea-v2-migration.md` (M0+M1 done; M2–M5 are
  08e §A).
- **Finish line:** `plans/08e-tui-finish-line.md` (the consolidated finish plan).
- **Reference TUI:** `/Users/rotemmiz/git/opencode/packages/tui/src/`.
- **Frozen wire contract:** `/Users/rotemmiz/git/opencode/packages/sdk/openapi.json`.

---

## I. Input model — how opencode handles every keystroke, paste, and mouse event

### I.1 The keymap engine (the foundation)

opencode's TUI uses `@opentui/keymap` with a **mode stack**, a **timed leader**, and
**binding layers**. The full registration is in `keymap.tsx:214-244`
(`registerOpencodeKeymap`):

- **Mode stack** (`keymap.tsx:53-100`): a stack of mode names; the top of stack is the
  active mode. `OPENCODE_BASE_MODE = "base"` is the default. Overlays push modes
  (`"question"`, `"autocomplete"`) so their bindings win. `keymap.setData("opencode.mode",
  stack.at(-1)?.mode ?? "base")`.
- **Timed leader** (`keymap.tsx:220-226`): `<leader>` (default `ctrl+x`, `keybind.ts:41`)
  is a token that captures the next key within `leader_timeout` (default 2000ms,
  `config/index.tsx:21`). `registerTimedLeader` from `@opentui/keymap/addons/opentui`.
- **Binding layers / `useBindings`** (`keymap.tsx:29` re-exports): each component
  registers a set of bindings optionally gated by `mode`, `enabled`, `target` (a focused
  renderable), and `priority`. The engine resolves a key by walking registered layers in
  priority order within the active mode.
- **Comma bindings** (`keymap.tsx:216`): `ctrl+x,ctrl+d` style multi-key chords (beyond
  the leader). `registerCommaBindings`.
- **Alias expanders** (`keymap.tsx:119-134`): `enter→return`, `esc→escape`, `pgdown→
  pagedown`, `pgup→pageup`. Applied as a binding expander so config can use either name.
- **Fallbacks:** `registerBaseLayoutFallback` (base-mode catch-all),
  `registerEscapeClearsPendingSequence` (esc aborts a partial chord),
  `registerBackspacePopsPendingSequence` (backspace aborts the leader),
  `registerManagedTextareaLayer` (routes the `input_*` group to the focused textarea
  only — `keymap.tsx:229-232`).

**The full keybind table** is `config/keybind.ts:45-239` (the `Definitions` object) —
**183 named keybinds** across these groups (names → `CommandMap` at `keybind.ts:255-418`):

| Group | Keybind names (opencode `Definitions`) | Default keys |
|---|---|---|
| **App / global** | `app_exit`, `app_debug`, `app_console`, `app_heap_snapshot`, `app_toggle_animations`, `app_toggle_file_context`, `app_toggle_diffwrap`, `app_toggle_paste_summary`, `app_toggle_session_directory_filter`, `command_list`, `help_show`, `docs_open`, `terminal_suspend`, `terminal_title_toggle`, `which_key_*` (**11**: toggle, layout_toggle, pending_toggle, group_previous, group_next, scroll_up, scroll_down, page_up, page_down, home, end) | `ctrl+c,ctrl+d,<leader>q` exit; `ctrl+p` palette; `ctrl+z` suspend; `ctrl+alt+k` which-key |
| **Leader (`<leader>` = `ctrl+x`)** | `leader` itself + every `<leader>X` binding below | `ctrl+x` |
| **Session** | `session_new/list/timeline/fork/rename/delete/share/unshare/interrupt/background/compact/export/copy/move`, `session_toggle_timestamps`, `session_toggle_generic_tool_output`, `session_queued_prompts`, `session_pin_toggle`, `session_quick_switch_1..9`, `session_child_first/cycle/cycle_reverse`, `session_parent`, `session_compact` | `<leader>n/l/g/c`; `ctrl+r` rename; `ctrl+d` delete; `escape` interrupt; `ctrl+b` background; `ctrl+f` pin; `<leader>1..9` quick-switch; `<leader>down` first child; `right/left` child cycle; `up` parent |
| **Sidebar / status** | `sidebar_toggle`, `scrollbar_toggle`, `status_view`, `theme_list`, `theme_switch_mode`, `theme_mode_lock` | `<leader>b/s/t` |
| **Diff viewer** | `diff_open` (default `"none"` — opened by clicking a diff tool row, **not** a keybind), `diff_close/toggle/expand/expand_all/collapse/switch_focus/next_hunk/previous_hunk/next_file/previous_file/toggle_file_tree/single_patch/switch_source/toggle_view/help` | `escape,q` close; `enter,space` toggle; `E` expand-all; `b` file tree; `s` single-patch; `d` switch source; `v` split/unified; `?` help (not `?]`); `n/p` next/prev file; `]/[` hunk |
| **Editor** | `editor_open` | `<leader>e` |
| **Messages (scroll)** | `messages_page_up/down`, `messages_line_up/down`, `messages_half_page_up/down`, `messages_first/last/next/previous/last_user`, `messages_copy`, `messages_undo/redo`, `messages_toggle_conceal` | `pageup/ctrl+alt+b`; `pagedown/ctrl+alt+f`; `ctrl+alt+y/e` line; `ctrl+alt+u/d` half; `ctrl+g,home`/`ctrl+alt+g,end` first/last; `<leader>y` copy; `<leader>u/r` undo/redo; `<leader>h` conceal |
| **Prompt** | `prompt_submit`, `prompt_editor_context_clear`, `prompt_skills`, `prompt_stash`, `prompt_stash_pop`, `prompt_stash_list`, `workspace_set` | (mostly `none` defaults — wired via command palette + slash) |
| **Input (textarea)** | `input_clear`, `input_paste`, `input_submit`, `input_newline`, `input_move_left/right/up/down`, `input_select_left/right/up/down`, `input_line_home/end`, `input_select_line_home/end`, `input_visual_line_home/end`, `input_select_visual_line_home/end`, `input_buffer_home/end`, `input_select_buffer_home/end`, `input_delete_line`, `input_delete_to_line_end/start`, `input_backspace`, `input_delete`, `input_undo/redo`, `input_word_forward/backward`, `input_select_word_forward/backward`, `input_delete_word_forward/backward`, `input_select_all`, `history_previous/next` | `ctrl+c` clear; `ctrl+v` paste; `return` submit; `shift+return,ctrl+return,alt+return,ctrl+j` newline; `left/ctrl+b` `right/ctrl+f` move; `up/down` history; `ctrl+a`/`ctrl+e` line home/end; `ctrl+k`/`ctrl+u` delete to end/start; `ctrl+w`/`ctrl+backspace`/`alt+backspace` delete word back; `alt+f/b`+`alt+right/left`+`ctrl+right/left` word move; `ctrl+-`/`super+z` undo; `ctrl+.`/`super+shift+z` redo; `super+a` select all |
| **Dialog (list modals)** | `dialog.select.prev/next/page_up/page_down/home/end/submit`, `dialog.prompt.submit`, `dialog.mcp.toggle`, `dialog.move_session.new/delete/refresh`, `dialog.plugins.install`, `plugins.toggle` | `up,ctrl+p`/`down,ctrl+n`; `pageup/down`; `home/end`; `return` submit; `space` toggle MCP/plugin; `shift+i` install |
| **Prompt autocomplete** | `prompt.autocomplete.prev/next/hide/select/complete` | `up,ctrl+p`/`down,ctrl+n`; `escape` hide; `return` select; `tab` complete |
| **Permission** | `permission.prompt.fullscreen` | `ctrl+f` fullscreen |
| **Model / agent / variant / mcp / provider** | `model_list`, `model_cycle_recent/_reverse`, `model_cycle_favorite/_reverse`, `model_favorite_toggle`, `model_provider_list`, `agent_list`, `agent_cycle/_reverse`, `variant_cycle`, `variant_list`, `mcp_list`, `provider_connect`, `console_org_switch` | `<leader>m` list; `f2`/`shift+f2` cycle recent; `<leader>a` agents; `tab`/`shift+tab` agent cycle; `ctrl+t` variant cycle; `ctrl+a` provider list; `ctrl+f` favorite toggle |
| **Stash** | `stash_delete` | `ctrl+d` (in stash dialog) |
| **Tips** | `tips_toggle` | `<leader>h` |
| **Tool details** | `tool_details` (default `"none"`) | (no default key — wired via command palette) |
| **Display** | `display_thinking` (default `"none"`) | (no default key — wired via command palette) |
| **Plugins** | `plugin_manager` (default `"none"`), `plugin_install` (default `"none"`) | (no default keys) |

> This is the **complete** keybind name set (183 entries). The Opcode42 TUI implements a
> subset (see §G.1 for the exact delta).
>
> **Key collisions** (same default key, different contexts — resolved by mode/target):
> `ctrl+f` (permission fullscreen + model favorite toggle + session pin toggle);
> `<leader>h` (messages conceal + tips toggle).

### I.2 Key dispatch order (the runtime precedence)

opencode does **not** have a single big switch. The engine resolves keys via the layered
binding system, but there is an observable precedence from the component tree
(`app.tsx:1073-1119` + `routes/session/index.tsx:1145-1347`):

1. **Selection-copy interceptor** (`app.tsx:418-425`, `keymap.intercept` priority 1):
   when `OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT` is on, selection keys are handled
   before normal bindings.
2. **Focused PTY / terminal** — the terminal renderable captures keys when focused
   (opencode's GUI uses `ghostty-web`; the TUI's PTY is via `/pty/{id}/connect` WS).
3. **Pending permission** (`routes/session/permission.tsx:544-626`) — registers
   bindings in `OPENCODE_BASE_MODE` (no mode push — base-mode globals like `ctrl+p`
   palette **still fire** during a pending permission). Captures
   `left/right/h/l/return/escape` + `app.exit` + `permission.prompt.fullscreen`.
   **Blocks the composer** (`routes/session/index.tsx:236` `disabled`).
4. **Pending question** (`routes/session/question.tsx:133-286`) — pushes `QUESTION_MODE`
   (`question.tsx:128-131`) (base-mode bindings are **inert**) and captures
   `up/down/k/j/return/escape/tab/left/right/h/l` + number keys `1..9` (capped at
   `Math.min(total, 9)`, `question.tsx:212`) + `app.exit`. **Blocks the composer.**
5. **Open dialog/modal** (`ui/dialog.tsx` stack) — pushes `"modal"` mode
   (`dialog.tsx:78-82`); the top of the dialog stack captures `dialog.select.*` + the
   dialog's own action bindings (`dialog-select.tsx:369-483`). A global `escape`/`ctrl+c`
   handler (`dialog.tsx:102-134`) pops the top dialog when `store.stack.length > 0 &&
   !selection`. `DialogAlert`/`DialogConfirm` have no `InputRenderable` — focus is null.
6. **Autocomplete popup** (`component/prompt/autocomplete.tsx:581-641`) — when visible,
   captures `prompt.autocomplete.*` (up/down/escape/return/tab). Other keys fall through
   to the textarea so typing keeps filtering it.
7. **The prompt textarea** (`component/prompt/index.tsx:557-845`):
   - `input_paste` (`ctrl+v`) → `prompt.paste` command → clipboard read + smart paste
     (`prompt/index.tsx:1391-1415`, `1178-1217`).
   - `input_clear` (`ctrl+c`) → `prompt.clear` command (clears the buffer; if empty, the
     app-level `app_exit` binding fires → two-press exit guard).
    - `prompt.history.previous/next` (`up`/`down`) — only when cursor at start/end of
      buffer (`prompt/index.tsx:857-923`). **Two-press quirk:** if the cursor is NOT at
      the edge, the first press moves the cursor to offset 0 (for `up`) or end (for
      `down`) and returns `false` (no history move); the second press actually navigates
      history (`prompt/index.tsx:870-873, 902-909`).
   - `!` at visual-cursor offset 0 → shell mode (`prompt/index.tsx:811-836`).
   - `escape` in shell mode → normal mode; `backspace` at offset 0 in shell mode → normal
     (`prompt/index.tsx:838-855`).
   - `input_submit` (`return`) → `submit()` (`prompt/index.tsx:926-1142`):
     - if `exit`/`quit`/`:q` typed → exit the app.
     - if shell mode → `POST /session/{id}/shell`.
     - if `/command` matches a daemon command → `POST /session/{id}/command`.
     - else → `POST /session/{id}/message` (or create-then-prompt on the home route).
8. **App-global bindings** (`app.tsx:948-969`) — `app` and `app.global` groups:
   `command.palette.show`, `model.*`, `agent.*`, `variant.*`, `mcp.list`,
   `provider.connect`, `console.org.switch`, `opencode.status`, `theme.*`, `help.show`,
   `docs.open`, `diff.open`, `workspace.list`, `app.debug/console/heap_snapshot`,
   `terminal.suspend/title.toggle`, `app.toggle.*` (5 toggles),
   `session.list/new/quick_switch.1..9`.
   - `app_exit` is gated (`app.tsx:961-969`) to fire only when the composer is **not**
     focused **or** is empty — so `ctrl+c` with text clears instead of exiting.
9. **Session-scoped bindings** (`routes/session/index.tsx:1094-1117`): `session.*`
   (share/rename/timeline/fork/compact/unshare/undo/redo/sidebar.toggle/toggle.*/copy/
   export/child.*/parent/messages_last_user/page.up/down/line.up/down/half.page.*),
    `session.global` (page/line/half-page scroll — active in **all** modes including
    dialog/question/autocomplete; no `mode` field at `index.tsx:1098-1100`),
   `session.global.unfocused` (first/last — only when no editor focused),
   `session.background` (priority 1, only when foreground subagents run).
10. **The `<leader>` chord** — `ctrl+x` arms the leader; the next key resolves a
    `<leader>X` binding (or times out after 2000ms, or is cancelled by esc/backspace).
11. **Base-layout fallback** — any key not matched by the above goes to the focused
    textarea (typing).

### I.3 Paste (bracketed + clipboard + smart paste)

- **Bracketed paste** (DECSET 2004): the textarea's `onPaste` (`prompt/index.tsx:1391-
  1415`) decodes `event.bytes` via `decodePasteBytes`, normalizes CRLF/CR → LF, and:
  - if the paste is empty (Windows Terminal image clipboard edge case) → dispatches
    `prompt.paste` (the `ctrl+v` command) which reads the system clipboard.
  - else `event.preventDefault()` and calls `pasteInputText` (`prompt/index.tsx:1178-
    1217`): detects a filepath → reads the local file as text or binary attachment;
    detects a URL → falls through to text; **smart paste** — if the paste is ≥3 lines or
    >150 chars and `paste_summary_enabled` (KV, default on) → inserts a virtual
    `[Pasted ~N lines]` extmark linked to a synthetic `text` part (the full text is sent
    on submit, the extmark is the in-composer summary). Else inserts the text verbatim.
- **`ctrl+v` / `prompt.paste`** (`prompt/index.tsx:366-386`): reads the clipboard
  (`context/clipboard.tsx` → `clipboard.ts`); if mime is `image/*` → `pasteAttachment`;
  if `text/plain` → `pasteInputText`.
- **Clipboard I/O** (`clipboard.ts`):
  - **Write** (`clipboard.ts:120-124`): always emits **OSC 52** (`\x1b]52;c;<base64>\x07`,
    tmux/screen-wrapped) **plus** a native command (`osascript` macOS, `wl-copy`/`xclip`/
    `xsel` Linux, `powershell.exe` Win32, `clipboardy` fallback).
  - **Read** (`clipboard.ts:29-74`): tries the platform image clipboard first
    (`osascript` PNG / `powershell.exe` / `wl-paste`/`xclip`), then `clipboardy` for
    text.
- **Selection copy** (`app.tsx:417-441`): when `OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_
  SELECT` is **off** (default), `onMouseUp` on the root box calls
  `Selection.copy(renderer, toast, clipboard)` — selecting text with the mouse and
  releasing copies it. When the flag is **on**, a right-click (`MouseButton.RIGHT`)
  copies instead (`app.tsx:1079-1086`). `renderer.console.onCopySelection`
  (`app.tsx:432-441`) is the opentui-console copy path (`ctrl+y` in the console).

### I.4 Mouse

opencode enables mouse when `config.mouse` is true (default) and
`!Flag.OPENCODE_DISABLE_MOUSE` (`app.tsx:197`). Mouse events used:

- **Selection copy** on `mouseUp` (`app.tsx:1087-1091`) — see §I.3.
- **Dialog/modal row hover + click** (`dialog-select.tsx`: `onMouseOver`/`onMouseDown`/
  `onMouseUp` → `moveTo`/`select`).
- **Prompt composer** `onMouseDown` focuses the textarea (`prompt/index.tsx:1432`).
- **Autocomplete** `onMouseMove`/`onMouseOver`/`onMouseDown`/`onMouseUp` → `moveTo`/
  `select` (`autocomplete.tsx:754-766`); tracks an `input: "keyboard"|"mouse"` flag so
  synthetic mouseovers from layout shifts don't steal keyboard selection
  (`autocomplete.tsx:166-170`).
- **Tool rows** — `InlineTool`/`BlockTool` `onMouseOver`/`onMouseOut`/`onMouseUp` for
  hover highlight + click-to-expand / click-to-open-error (`routes/session/index.tsx:
  1888-1901, 2007-2012`).
- **User message** `onMouseUp` opens `DialogMessage` (edit/copy a past prompt)
  (`routes/session/index.tsx:1256-1265`).
- **Revert bar** `onMouseUp` → confirm redo (`routes/session/index.tsx:1211`).
- **Question tabs / options** `onMouseOver`/`onMouseDown`/`onMouseUp` → `selectTab`/
  `moveTo`/`selectOption` (`question.tsx:315-353, 370-407`).
- **Permission options** `onMouseOver`/`onMouseUp` → select + confirm
  (`permission.tsx:684-688`).
- **Scroll** — the `scrollbox` renderable handles wheel natively (opentui);
  `scrollAcceleration` is configurable (`config/index.tsx:27-29`).

### I.5 IME / composition

The textarea handles IME composition; `submit()` double-defers
(`prompt/index.tsx:1386-1390` — `setTimeout(() => setTimeout(() => submit(), 0), 0)`) so
the last composed character (e.g. Korean hangul) is flushed to `plainText` before the
submit reads it. `onContentChange` syncs `plainText` → `store.prompt.input`.

### I.6 Exit / suspend / terminal title

- **Exit** (`app_exit` = `ctrl+c,ctrl+d,<leader>q`): with composer text, `ctrl+c` clears
  (the `prompt.clear` command); with an empty composer, the app-level binding calls
  `exit()` (`app.tsx:814-820`). The Opcode42 TUI mirrors the two-press guard
  (`model.go:594-626`).
- **Suspend** (`terminal_suspend` = `ctrl+z`): `renderer.suspend()` + `SIGTSTP` +
  `SIGCONT` resume (`app.tsx:854-864`). Disabled on win32; when disabled, `input_undo`
  re-binds to `ctrl+z` (`config/index.tsx:90-98`).
- **Terminal title** (`app.tsx:447-471`): `OpenCode` on home, `OC | <title>` on a named
  session, `OC | <plugin>` on a plugin route. Toggled by `terminal_title_toggle` (KV
  `terminal_title_enabled`).

---

## II. Presentation model — what each part of the app shows and where the data comes from

### II.1 Routes (the top-level layout)

`app.tsx:1073-1119` — a column box filling the terminal, background = `theme.background`:

- **`<Home />`** when `route.data.type === "home"` (`routes/home.tsx`).
- **`<Session />`** when `route.data.type === "session"` (`routes/session/index.tsx`).
- **Plugin route** when `route.data.type === "plugin"` (`app.tsx:1067`; rendered via
  `{plugin()}` at line 1108, outside the `<Switch>`).
- **`<StartupLoading />`** overlay until `ready` (`app.tsx:1115-1117`).
- **`app_bottom` slot** (`app.tsx:1110-1112`) — no plugin registers this slot on the
  session route; `routes/session/footer.tsx` is dead code (see §II.3.f).

### II.2 Home route (`routes/home.tsx`)

A centered column: spacer → 4-row gap → **`<Logo />`** (the block-pixel "opencode"
wordmark with shimmer, `component/logo.tsx`) → 1-row gap → **`<Prompt />`** (max width
`prompt.max_width`, default 75 or `auto` = `max(75, 70% width)`, `home.tsx:33-37`) →
`home_bottom` slot → spacer → **`<Toast />`** → `home_footer` slot.

- **Data:** the prompt's agent/model come from `local` (`context/local.tsx`). No session
  data is shown on home.
- **`--prompt` / `--continue` args** pre-fill / auto-submit / fork-then-navigate
  (`home.tsx:48-68`, `app.tsx:496-533`).

### II.3 Session route (`routes/session/index.tsx`)

A row: **stream column** (flex-grow) + optional **sidebar** (42 cols, `sidebar.tsx:31`).
The stream column is a column: **`scrollbox`** (the conversation) + **prompt column**
(permission/question/subagent-footer/prompt). Overlays (sidebar on narrow widths, dialog
stack, toasts) sit on top.

#### II.3.a The stream (`scrollbox`, `routes/session/index.tsx:1167-1281`)

- Renders `messages()` = `sync.data.message[sessionID]` (≤100, oldest trimmed,
  `sync.tsx:334-352`).
- Per message, a `<Switch>` on `message.role`:
  - **`user`** → `UserMessage` (`index.tsx:1350-1453`): left-bordered box (border color =
    agent color, `local.agent.color(message.agent)`), `text` parts joined by `\n\n`, file
    parts as `File`/`Directory` chips, optional `QUEUED` badge or timestamp.
  - **`assistant`** → `AssistantMessage` (`index.tsx:1455-1562`): maps each part via
    `PART_MAPPING` (`text`→`TextPart`, `tool`→`ToolPart`, `reasoning`→`ReasoningPart`).
    Footer line: `▣ <mode> · <model> · <duration> · interrupted?` (only on last/final/
    aborted). Error box if `message.error`.
  - **Revert bar** when `message.id === revert.messageID` (`index.tsx:1190-1248`):
    `<N> message reverted` + `<shortcut> or /redo to restore` + file list with +/− counts.
- **Sticky scroll** (`stickyScroll={true} stickyStart="bottom"`, `index.tsx:1181-1182`):
  the last messages stay pinned; new content scrolls into view.
- **Scrollbar** optional (`scrollbar_visible` KV, `index.tsx:1171-1180`).

#### II.3.b Part renderers (`routes/session/index.tsx:1564-2646`)

| Part type | Renderer | What it shows |
|---|---|---|
| `text` | `TextPart` (`1679-1698`) | `<markdown>` renderable with syntax style, streaming, conceal, theme `markdownText`. |
| `reasoning` | `ReasoningPart` (`1572-1633`) | `ReasoningHeader` (spinner while thinking, `Thought: <title> · <duration>` when done) + collapsible `<markdown>` body. `thinkingMode` controls collapse. |
| `tool` | `ToolPart` (`1702-1779`) → dispatches by `toolDisplay(tool)`: `bash`/`glob`/`read`/`grep`/`webfetch`/`websearch`/`write`/`edit`/`task`/`apply_patch`/`todowrite`/`question`/`skill`/`generic`. | See below. |

**Tool renderers** (`index.tsx:1788-2523`):
- **`InlineTool`** (`1826-1902`) — a one-line row: `icon` + text, with pending/complete/
  failed/denied states, spinner option, hover highlight, click (expand error / open
  diff / open subagent). `permission` color when a permission is pending for this call.
- **`BlockTool`** (`1984-2034`) — a bordered panel with optional title, body, error; used
  by `Shell`/`Write`/`Edit`/`ApplyPatch`/`TodoWrite`/`Question`/`GenericTool` when there
  is output/diff/diagnostics.
- **`Shell`** (`2036-2093`): `$ <command>` + collapsed output (10 lines, click to
  expand); workdir title.
- **`Write`** (`2095-2130`): `Wrote <path>` + line-numbered code block + diagnostics.
- **`Edit`** (`2325-2376`): `Edit <path>` + `<diff>` renderable (split or unified, theme
  diff tokens) + diagnostics.
- **`ApplyPatch`** (`2378-2452`): per-file `BlockTool` with diff + delete/move/add title.
- **`Read`** (`2145-2178`): `Read <path>` + `↳ Loaded <path>` for each loaded file.
- **`Glob`/`Grep`/`WebFetch`/`WebSearch`** (`2132-2208`): one-liners with count/results.
- **`Task`** (`2210-2306`): sub-agent card — `✓`/`│` icon + `<Kind> Task — <desc>` +
  `↳ <current tool>` / `↳ N toolcalls` / `↳ N toolcalls · <duration>`; click navigates to
  the child session; retry status in red.
- **`TodoWrite`** (`2454-2478`): `# Todos` block with `TodoItem` rows.
- **`Question`** (`2480-2514`): `# Questions` block with Q+A pairs.
- **`Skill`** (`2516-2522`): `Skill "<name>"` one-liner.

#### II.3.c The prompt column (`routes/session/index.tsx:1282-1320`)

A non-shrink column at the bottom of the stream column:

1. **`<PermissionPrompt />`** when `permissions().length > 0` (`routes/session/
   permission.tsx`) — a bordered panel with `△ Permission required`, the tool-specific
   body (edit diff, bash command, etc.), and option chips `Allow once / Allow always /
   Reject` (left/right/return/escape; fullscreen toggle).
2. **`<QuestionPrompt />`** when no permission and `questions().length > 0` (`routes/
   session/question.tsx`) — a bordered panel with tabs (one per question + Confirm),
   numbered options, multi-select checkboxes, custom-answer textarea, `↑↓ select / tab
   next / enter submit / esc dismiss`.
3. **`<SubagentFooter />`** when `session.parentID` (`routes/session/subagent-footer.
   tsx`) — the child-session nav strip.
4. **`<Prompt />`** when `visible()` (no parent, no pending permission/question) — the
   composer (`component/prompt/index.tsx`).

#### II.3.d The composer (`component/prompt/index.tsx`)

- **Textarea** (`prompt/index.tsx:1364-1436`) — placeholder, syntax-highlighted, max
  height `prompt.max_height` (default `max(6, height/3)`), left-bordered with the
  agent-color accent bar. Extmarks render `@mention`/`[Pasted …]`/`[Image N]` virtual
  text inline.
- **Mode line** (`prompt/index.tsx:1437-1477`) — `<agent> · <model> <provider> ·
  <variant>` (normal) or `Shell` (shell mode); `auto` chip when auto-approve is on.
- **Status line** (`prompt/index.tsx:1506-1675`):
  - **Busy** (`status.type !== "idle"`): spinner + retry text (with countdown + click to
    expand) + `esc again to interrupt` (two-press).
  - **Workspace/move notices** when active.
  - **Right side:** editor-context file label, usage (`<tokens> (<pct%) · <cost>`) —
    **mutually exclusive** with the `tab agents` hint: usage is shown when present
    (`prompt/index.tsx:1650-1656`), and the `tab agents` hint is the fallback when usage
    is absent (lines 1657-1661). The `ctrl+p commands` hint is always shown. Usage
    format: `[context, cost].filter(Boolean).join(" · ")` where `context` is
    `${Locale.number(tokens)} (${pct})` only if `model?.limit.context` exists (else just
    `${tokens}`), and `cost` only if `cost > 0` (lines 259-277). The entire right side is
    **hidden** when `status().type === "retry"` (line 1640). Or `esc exit shell mode` in
    shell mode.

#### II.3.e The sidebar (`routes/session/sidebar.tsx`)

42-col panel, `backgroundPanel`: scrollable **title block** (session title, id if non-
`latest` channel, workspace label, share URL) + `sidebar_content` slot + **footer**.
Toggled by `<leader>b` (`sidebar_toggle`); auto-shown on wide terminals
(`width > 120`, `index.tsx:264-269`); overlay (absolute, dim backdrop) on narrow widths
(`index.tsx:1329-1341`).

The `sidebar_content` slot (`sidebar.tsx:85`) renders **five builtin plugins** in
order (by `order` field):
1. **Context** (`feature-plugins/sidebar/context.tsx`, order 100) — tokens/cost
   summary + context bar.
2. **MCP** (`sidebar/mcp.tsx`, order 200) — MCP server status list (connected/
   errored/connecting).
3. **LSP** (`sidebar/lsp.tsx`, order 300) — LSP server status list.
4. **Todo** (`sidebar/todo.tsx`, order 400) — incomplete todo items.
5. **Modified Files** (`sidebar/files.tsx`, order 500) — diff file list (files
   changed in the session).

The sidebar **footer** (`feature-plugins/sidebar/footer.tsx`) renders a "Getting
started" callout (when no paid provider + not dismissed), the cwd path
(`parent/name`), AND `• OpenCode <version>`. The `sidebar.tsx` fallback footer
(lines 89-98) is shadowed by the `single_winner` plugin slot.

#### II.3.f The footer / status bar

**On the session route:** `routes/session/footer.tsx` is **dead code** — it is never
imported or mounted. No plugin registers the `app_bottom` slot, so the session route
has **no footer/status bar at all**. The LSP/MCP/permissions/welcome content described
in `footer.tsx:27-50` is unreachable.

**On the home route:** the `home_footer` slot (plugin-driven via
`feature-plugins/home/footer.tsx`) shows `directory:branch`, MCP count, and version.

### II.4 Overlays (the dialog stack — `ui/dialog.tsx`)

`useDialog()` provides a stack of dialogs; `dialog.replace`/`dialog.clear` manage it.
Only the top is rendered. The workhorse is **`<DialogSelect />`** (`ui/dialog-select.tsx`)
— a bordered, titled, filtered list with:

- a filter `InputRenderable` (`dialog-select.tsx:116`),
- a grouped (`groupBy` category) or flat (when filtering) option list,
- `up/down/pageup/pagedown/home/end/return` nav (`dialog.select.*` bindings),
- per-row hover + click (`dialog-select.tsx` onMouse*),
- **actions** (`dialog-select.tsx:118-148`) — side commands (e.g. `model.dialog.favorite`
  with `ctrl+f`, `model.dialog.provider` with `ctrl+a`); `tab`/`shift+tab` cycles action
  focus (`dialog-select.tsx:462-476`),
- footer hints.

The dialogs built on `DialogSelect` / custom: `DialogSessionList`, `DialogModel`,
`DialogAgent`, `DialogMcp`, `DialogThemeList`, `DialogVariant`, `DialogWorkspaceList`,
`DialogConsoleOrg`, `DialogProvider`, `DialogSkill`, `DialogStash`, `DialogStatus`,
`DialogHelp`, `DialogTimeline`, `DialogForkFromTimeline`, `DialogMessage`,
`DialogSessionRename`, `DialogTag`, `DialogSubagent`, `CommandPaletteDialog`,
`DialogAlert`, `DialogConfirm`, `DialogExportOptions`, `DialogRetryAction`,
`DialogWorkspaceCreate/FileChanges/Unavailable`, `DialogSessionDeleteFailed`,
`DialogMoveSession`.

### II.5 Autocomplete (`component/prompt/autocomplete.tsx`)

An absolute-positioned popover above the composer (`autocomplete.tsx:722-780`), bordered,
`backgroundMenu`. Triggered by `/` at offset 0 (slash commands) or `@` (files/agents/
references/MCP resources). Fuzzy-filtered (`fuzzysort`) with frecency boost on file
paths. `up/down/escape/return/tab` nav; `tab` on a directory expands it. Options:
- **`/`** → slash commands (`useCommandSlashes()` + daemon `GET /command`).
- **`@`** → `GET /v2/fs/find` files (frecency-ranked), `@agent` sub-agents, `@reference`
  aliases, MCP resources.

### II.6 Toasts (`ui/toast.tsx`)

A **single slot** (not a queue): `currentToast` (`toast.tsx:55`); a new toast
**replaces** the previous (`setStore("currentToast", toastOptions)` at line 63).
Position: **top-right** corner (`top={2} right={2}`, lines 27-28). Default duration:
5000ms (line 62). `useToast()` is called everywhere (copy success, share URL copied,
errors, update available). Auto-expire; the `Toast` component is mounted once per route
(`home.tsx:88`, `index.tsx:1322`).

### II.7 Startup loading (`component/startup-loading.tsx`)

Shown until `ready` (`app.tsx:1115-1117`); `ready` = `sync.status !== "loading"` or
`skipInitialLoading` (`sync.tsx:558-561`).

---

## III. Update flow — how data gets in and how the view stays in sync

### III.1 The sync store (`context/sync.tsx`)

A single `createStore` (`sync.tsx:64-138`) holding: `status` (`loading`/`partial`/
`complete`), `provider[]`, `provider_default{}`, `provider_next`, `console_state`,
`capabilities`, `provider_auth{}`, `agent[]`, `command[]`, `permission{sessionID:[]}`,
`question{sessionID:[]}`, `config`, `session[]`, `session_status{}`, `session_diff{}`,
`todo{}`, `message{sessionID:[]}`, `part{messageID:[]}`, `lsp[]`, `mcp{}`,
`mcp_resource{}`, `formatter[]`, `vcs`.

### III.2 Bootstrap (`sync.tsx:445-546`)

`onMount(() => void bootstrap())` (`sync.tsx:548-550`). **Blocking phase** (the
`await Promise.all([...])` at `sync.tsx:464-472`): `config.providers`,
`provider.list`, `experimental.capabilities`, `app.agents`, `config.get`,
`project.sync`, and `session.list` only if `--continue`. Sets `status := "partial"`.
**Non-blocking phase** (parallel): `session.list` (if not continuing),
`experimental.console.get` (fired concurrently at `sync.tsx:458` but NOT in the blocking
gate — first awaited at `sync.tsx:482-489`), `command.list`, `lsp.status`, `mcp.status`,
`experimental.resource.list`, `formatter.status`, `session.status`, `provider.auth`,
`vcs.get`, `workspace.sync`. Sets `status := "complete"`.

### III.3 SSE event → store (`sync.tsx:170-440`)

`event.subscribe((event, {directory, workspace}) => switch(event.type) { … })`. Each
event uses a binary-search (`search`, `sync.tsx:41-52`) for sorted insert/update/remove:

| Event | Store mutation |
|---|---|
| `server.instance.disposed` | re-bootstrap |
| `permission.asked` | if `permission.mode === "auto"` → auto-reply `once`; else sorted-insert into `permission[sessionID]` |
| `permission.replied` | remove by `requestID` |
| `question.asked` | sorted-insert into `question[sessionID]` |
| `question.replied`/`rejected` | remove by `requestID` |
| `todo.updated` | `todo[sessionID] = todos` |
| `session.diff` | `session_diff[sessionID] = diff` |
| `session.deleted` | remove from `session[]` |
| `session.updated` | upsert into `session[]` (sorted by id) |
| `session.next.moved` | patch `directory`/`path`/`workspaceID`/`time.updated` |
| `session.status` | `session_status[sessionID] = status` |
| `message.updated` | upsert into `message[sessionID]` (sorted by id); trim to 100 (drop oldest + its parts) |
| `message.removed` | remove from `message[sessionID]` |
| `message.part.updated` | upsert into `part[messageID]` |
| `message.part.delta` | append `delta` to the part's `field` (string concat) |
| `message.part.removed` | remove from `part[messageID]` |
| `lsp.updated` | re-fetch `lsp.status` |
| `vcs.branch.updated` | `vcs.branch = branch` (if current workspace) |

### III.4 Session hydration (`sync.session.sync`, `sync.tsx:588-660`)

On entering a session: `Promise.all([session.get, session.messages(limit=100),
session.todo, session.diff])`, then reconcile into the store (preserving in-flight
hydrated messages/parts via the `hydratingSessions` tracker). `fullSyncedSessions` Set
prevents re-fetching.

### III.5 The SSE transport (`context/sdk.tsx`)

`startSSE()` (`sdk.tsx:82-117`): `sdk.global.event()` stream, 16ms batch window
(`sdk.tsx:68-80`, constant at `sdk.tsx:75-76`), exponential backoff
`min(1000 * 2^(attempt-1), 30000)` (`sdk.tsx:113`), `sseMaxRetryAttempts: 0` (the outer
loop manages reconnect). The TUI reconnects when the SSE stream closes — there is **no
heartbeat timer** in the TUI (the 15s `: heartbeat` comment is server-side, emitted by
`packages/server/src/handlers/event.ts:37`).

### III.6 Reactive render

opencode uses SolidJS fine-grained reactivity: the store mutations above trigger exactly
the components that read the changed slice. There is no "full re-render" — the `scrollbox`
diffs at the renderable level. The `markdown`/`diff`/`code` renderables cache internally.
Animations run on the opentui render loop (`requestRender`), not Solid.

### III.7 The local state (`context/local.tsx`)

Client-only state not on the wire: `agent` (current + list + color + cycle), `model`
(current per-agent + recent + favorite + variant, persisted to
`$state/model.json`), `session` (pinned slots, persisted to `$state/session.json`),
`mcp` (toggle), `permission` (auto-approve mode). The composer reads these for the mode
line + submit payload.

### III.8 The KV (`context/kv.tsx`)

Persistent client toggles: `sidebar`, `timestamps`, `tool_details_visibility`,
`scrollbar_visible`, `diff_wrap_mode`, `animations_enabled`, `file_context_enabled`,
`paste_summary_enabled`, `session_directory_filter_enabled`, `terminal_title_enabled`,
`share_consent`, `skipped_version`, `theme_mode_lock`, theme name, etc. Used by the
`app.toggle.*` commands and the display toggles.

---

## IV. Opcode42 TUI parity status (current tree, `internal/tui/`)

| opencode behavior (§) | Opcode42 status | Where |
|---|---|---|
| Keymap engine: mode stack, timed leader, binding layers | **Simplified** — single `switch` in `Update` + `handleLeaderKey` (`model.go:576-762, 1778-1988`). No mode stack; overlays dispatch directly. | `model.go` |
| ~183 named keybinds | **Subset** — see §G.1 for the delta. | `model.go` |
| Bracketed paste + smart paste + OSC 52 | **Partial** — `tea.PasteMsg` forwarded (`model.go:542-558`); no smart-paste summary extmark; clipboard write is OSC 52 + pbcopy/wl-copy (`clipboard.go`). | `model.go`, `clipboard.go` |
| Selection-copy on mouseUp | **Missing** — no opentui selection model equivalent; `ctrl+x y` copies the last assistant text instead (`model.go:1829-1839`). | `model.go`, `yank.go` |
| Mouse: row hover/click in dialogs, tools, autocomplete | **Partial** — mouse wheel scrolls the stream (`model.go:560-574`); no row hover/click in modals/autocomplete/tools. | `model.go` |
| Routes: home/session | **parity** (splash + session screens, `model.go:28-32`). | `model.go` |
| Stream: user/assistant/text/reasoning/tool parts | **parity** (`render.go`, `toolrender.go`, `markdown.go`). | render.go et al. |
| Tool renderers: bash/write/edit/read/grep/glob/webfetch/websearch/task/apply_patch/todowrite/question/skill/generic | **parity** (`toolrender.go`). | `toolrender.go` |
| Diff viewer (file tree, split/unified, hunk nav, source switch) | **parity** (`diff.go`, shipped 08b §1). | `diff.go` |
| PTY pane | **parity** (`ptypane.go`, vt10x, shipped 08b §2). | `ptypane.go` |
| Composer: textarea, mode line, status line, shell mode, `!` | **parity** (`composer.go`, `model.go:721-738`). | `composer.go` |
| Composer: agent/model/variant chips, usage, editor-context label | **Partial** — model/agent/variant yes (`chrome.go`); usage (tokens/cost) and editor-context label not shown. | `chrome.go` |
| Autocomplete: `/` slash + `@` files/agents | **Partial** — slash popup yes (`slash.go`); `@`-mention file picker yes (`mention.go`); MCP resources + reference aliases not in the popup. | `slash.go`, `mention.go` |
| Permission prompt: 3-stage (once/always/reject), fullscreen, diff body | **parity** (`permission.go`, `permission_state.go`, plan 17). | `permission.go` |
| Question prompt: tabs, multi-select, custom answer, confirm | **parity** (`question.go`, `question_state.go`, plan 17). | `question.go` |
| Modals: palette/sessions/models/agents/themes/timeline/status/help/connect | **parity** (`modal.go`, `connect.go`). | `modal.go` |
| Modals: mcp/skill/stash/variant/session-rename/fork-from-timeline/message/tag/subagent/console-org/provider | **Partial** — mcp/skill read-only yes; stash/variant yes; session-rename yes; fork-from-timeline/message/tag/subagent/console-org/provider **missing** (08b parked the daemon-gated ones). | `modal.go`, `sessionops.go`, `stash.go`, `variant.go` |
| Sidebar: title, workspace, share URL, context/LSP, footer | **Partial** — title + footer yes (`chrome.go`); workspace/share-URL yes; context gauge yes (08e §E5); LSP/MCP status sections **missing**. | `chrome.go` |
| Footer: directory, permissions, LSP, MCP, /status | **Partial** — directory + status + model yes (`chrome.go`); LSP/MCP counts not shown. | `chrome.go` |
| Toasts | **parity** (`toast.go`, 08c M11). | `toast.go` |
| Spinner (gradient scanner) | **parity** (`spinner.go`, 08c M9). | `spinner.go` |
| Logo shimmer + bg-pulse | **parity** (`logo.go`, 08e §B). | `logo.go` |
| Themes (33 opencode + 3 native, JSON loader) | **parity** (`theme/`, 08c M1-M2). | `theme/` |
| Markdown (glamour) + syntax (chroma) | **parity** (`markdown.go`, `syntax.go`, 08c M4-M5). | `markdown.go` |
| Sub-agent: in-stream card, child nav, sidebar status, subtree | **parity** (`subagent.go`, 08e §C). | `subagent.go` |
| mDNS discover + connect overlay | **parity** (`connect.go`, 08e §D). | `connect.go` |
| Reconcile-on-reconnect for pending permission/question | **parity** (`reconcile.go`, 08e §E3). | `reconcile.go` |
| Image parts (Sixel/iTerm2) | **parity** (`image.go`, 08e §E2). | `image.go` |
| Local KV (history, stash, theme, model, pinned sessions, view toggles) | **parity** (`kv.go`). | `kv.go` |
| Which-key overlay | **parity** (`whichkey.go`, 08e §F2). | `whichkey.go` |
| Help overlay | **parity** (`modal.go` modalHelp, 08e §F3). | `modal.go` |
| Terminal title | **Missing** — `app.tsx:447-471` has no Opcode42 equivalent. | — |
| Suspend (`ctrl+z` / SIGTSTP) | **Missing** — `app.tsx:854-864` has no Opcode42 equivalent. | — |
| Animations toggle, file-context toggle, diffwrap toggle, paste-summary toggle, session-directory-filter toggle | **Partial** — animations yes (`noAnim` flag); the KV-backed `app.toggle.*` commands are not palette-reachable. | `kv.go` |
| Session: share/unshare/summarize/abort/fork/delete/rename/message-delete | **parity** (`sessionops.go`, 08a §A). | `sessionops.go` |
| Session: undo/redo (revert/unrevert) | **Missing** — opencode `session.undo`/`redo` call `session.revert`/`unrevert`; Opcode42 does not. | — |
| Session: quick-switch slots 1-9, pin | **Missing** — opencode `<leader>1..9` + `ctrl+f` pin. | — |
| Session: export/copy transcript, editor-open | **Partial** — `ctrl+x y` copies last assistant text; full transcript export/copy + `$EDITOR` open (`ctrl+x e` exists) but not the `/export` / `/copy` slash commands. | `yank.go`, `editor.go` |
| Session: timeline modal + jump-to-message | **parity** (`timeline.go`). | `timeline.go` |
| Session: queued prompts management | **Missing** — opencode `session.queued_prompts` (`<leader>q`). | — |
| Session: background subagents (`ctrl+b`) | **Missing** — opencode `session.background`. | — |
| Command palette (`ctrl+p`) | **parity** (`modal.go` modalPalette). | `modal.go` |
| `docs.open` | **Missing** — opencode opens `https://opencode.ai/docs`. | — |
| `app.debug`/`app.console`/`app.heap_snapshot` | **Missing** — opentui-specific debug surfaces; N/A for Bubble Tea. | — |
| Which-key group nav / scroll / layout / pending preview (9 binds) | **Missing** — Opcode42 which-key is a static cheat-sheet. | `whichkey.go` |
| Plugin host (client-side) | **Out of scope** (08b §5 — "probably never for a dogfood TUI"). | — |

---

## V. The map → what the plans already cover and what they do not

The sibling plans cover the **features** but not the **input/presentation surface** as a
whole. Concretely, this plan adds:

- **The complete keybind table** (§I.1) — no prior plan enumerates all 183 names.
  08a §C names the `messages_*` cluster; 08a §D names the display toggles; 08b §1 names
  the `diff_*` cluster; but the `input_*`, `dialog.*`, `prompt.autocomplete.*`,
  `which_key_*`, `session_quick_switch_*`, `session_pin_toggle`, `session_queued_
  prompts`, `session_background`, `messages_undo/redo`, `docs_open`, `app.debug/console/
  heap_snapshot`, `terminal_suspend`, `terminal_title_toggle` binds are not enumerated
  anywhere.
- **The key dispatch precedence** (§I.2) — the order in which permission > question >
  dialog > autocomplete > composer > app-global > session > leader resolves. The
  Opcode42 `Update` switch encodes a similar order (`model.go:576-762`) but it is not
  documented.
- **The paste/clipboard/selection model** (§I.3) — 08a §C mentions a clipboard helper
  for `messages_copy`; the smart-paste extmark, OSC 52, selection-copy-on-mouseUp, and
  the `ctrl+v`/`prompt.paste` command are not in any plan.
- **The mouse surface** (§I.4) — not enumerated in any plan.
- **The IME double-defer** (§I.5) — not in any plan.
- **The exit/suspend/title behavior** (§I.6) — the two-press `ctrl+c` guard is in
  `model.go:594-626` but not the plans; suspend and terminal-title are not in any plan.
- **The presentation map** (§II) — the plans describe individual components (sidebar,
  footer, composer, modals) but not the full route→pane→part hierarchy with data
  sources.
- **The update flow** (§III) — plan 08 §"TUI sync store" covers the store shape and
  event switch at a high level; this plan adds the bootstrap phasing, the
  session.hydrate reconcile, the SSE transport details, and the reactive-render model.
- **The parity status table** (§IV) — a single audit of what is shipped vs missing
  across all of 08/08a–e.

---

## P. Performance & rendering (the per-frame pipeline)

> opencode and Opcode42 have **fundamentally different** render architectures. This
> section maps opencode's, names the Opcode42 equivalent, and records the known
> hazards. **Plans 19 (incremental render) and 20 (pre-rendered buffer) own the
> Opcode42 pipeline; this section cross-references them.**

### P.1 opencode's retained-renderable tree

opencode uses SolidJS over a native Zig opentui compositor (`app.tsx:186-208` creates a
`CliRenderer` with `targetFps: 60`). Each message/part is a **persistent native
object** created once, mutated in place via setters, and the terminal output is
cell-diffed per frame. Concretely:

- **Viewport culling** — only visible messages paint cells; off-screen messages skip
  their paint hooks entirely (plan 19 §"What opencode does differently").
- **Scroll = translation** — `content.translateY = -position`; no re-render of message
  content on scroll.
- **Cell-level terminal diffing** — the native renderer diffs the new frame buffer vs
  the last and emits ANSI only for changed cells.
- **`requestRender`** — animations run on the opentui render loop, not Solid. A
  `requestRender()` call schedules a frame; idle = no work.
- **`viewEquals` short-circuit** (Bubble Tea v2 does this too) — if `View.Content` is
  byte-identical to the last frame, no output is emitted.

### P.2 Opcode42's pre-rendered buffer (plans 19 + 20)

Opcode42 renders in `Update`, slices in `View` (plan 20's architecture):

- **`bodyLines []string`** — the body pre-rendered as individual lines in `Update`;
  `View` windows via `frameStreamLines`. Zero rendering in `View` (`model.go:285-287`).
- **`footerRendered` / `footerHeight` / `sidebarRendered`** — footer + sidebar
  pre-rendered in `Update`; `sessionLayers` reads them directly (`model.go:273-284`).
- **`mdCache` / `mdBlockCache`** — rendered-markdown cache keyed by `(text-hash,
  width, theme)` and `(partID, blockIdx, width, theme)` (`model.go:200-217`). Stable
  blocks render once; only the trailing streaming block re-renders per delta (mirrors
  opencode's `commitMarkdownBlocks` + `_stableBlockCount`).
- **`diffCache`** — completed edit/apply_patch diffs cached by `(partID, patchHash,
  width, themeName)` (`model.go:219-227`). Tool rows run every anim tick; without this
  cache a multi-hunk diff would dominate the budget.
- **`childStatusMap` / `childStatusVersion`** — child statuses computed once per store
  change, not per frame (`model.go:254-265`). Fixes the 75%-CPU-with-52-subagents
  hazard (plan 20 §1a).
- **`animatingCache`** — `animating()` computed once per `Update` cycle, not per frame
  (`model.go:267-271`). Fixes the status-bar re-scan hazard.
- **`rerenderFull` / `rerenderChrome`** — the `Update` paths that bump the pre-rendered
  strings. Every store mutation or view toggle routes through one of these
  (`model.go` `rerenderFull`/`rerenderChrome` calls are visible throughout `Update`).

### P.3 Streaming partial-text re-render

opencode's `markdown`/`code` renderables cache internally and only re-paint the
streaming block. Opcode42 mirrors this with `mdBlockCache`: a growing part's text is
split into stable blocks (blank-line-terminated) + a trailing streaming block; stable
blocks render once and serve from cache, only the streaming block re-renders each frame
(`model.go:209-217`). This avoids the O(n²) re-parse a full-text cache would incur on
every `message.part.delta`.

### P.4 Animation tick lifecycle

A single low-frequency `animTick` (~30-60ms) self-stops when `animating()` returns false
(`model.go` `maybeKickAnim`). `animating()` is true when: the splash screen is active
(logo shimmer), a session is streaming (spinner), the bg-pulse is on, or a toast is
live. Idle = no tick = no CPU. `animFrame` is the monotonic counter passed to
`scannerFrame()` and `logoFrame()`.

### P.5 Known hazards (record, do not re-fix)

- **`parseToolState` / `childStatus` per-frame JSON decode** — was 52% CPU / 75% with
  52 subagents. Fixed by `childStatusMap` (plan 20 §1a). Do not regress: any new
  per-child render path must read the map, not re-decode.
- **`composeCanvas` base fill** — was 40% CPU (5600-cell zero-fill per frame). Plan 20
  moves to pre-rendered strings so `View` no longer calls `composeCanvas` for the body.
- **`gitBranch` subprocess per frame** — `sidebarView` spawned `git` every frame. Fixed
  by the `gitBranchCache` (plan 19) / pre-rendered sidebar (plan 20).
- **Bubble Tea v2 does NOT coalesce `View()` calls** — each `MouseWheelMsg` triggers
  `Update → View`. The pre-rendered buffer makes `View` a slice op, so this is fine.

### P.6 Opcode42 parity status

| opencode perf property | Opcode42 status | Where |
|---|---|---|
| Retained renderable tree | **Divergence (architectural)** — Bubble Tea is string-based; the pre-rendered buffer is the Go idiom for the same principle. | plans 19, 20 |
| Viewport culling | **Partial** — `frameStreamLines` windows the body buffer (only visible lines are emitted), but all lines are pre-rendered. | `canvas.go` |
| Cell-level terminal diffing | **Bubble Tea v2 does this** (`viewEquals` + ultraviolet). | — |
| Streaming block cache | **parity** (`mdBlockCache`). | `model.go` |
| Diff cache | **parity** (`diffCache`). | `model.go` |
| Child-status cache | **parity** (`childStatusMap`). | `model.go` |
| Animation tick self-stop | **parity** (`maybeKickAnim` + `animatingCache`). | `model.go` |

---

## L. Layout, sizing & placement (the geometry)

> The precise spacing/padding/border math. Scattered across 08c (M8 chrome), 17 (scroll
> layout), 18 (scroll gutter); this section consolidates it.

### L.1 opencode geometry (source-grounded)

| Element | Size | Source |
|---|---|---|
| Root | `width × height` (terminal), column flex, `backgroundColor = theme.background` | `app.tsx:1074-1078` |
| Home prompt max-width | `prompt.max_width` config (default 75; `auto` = `max(75, 70% width)`) | `home.tsx:33-37` |
| Session row | stream column (flex-grow) + sidebar (42 cols) | `index.tsx:1165` |
| Stream padding | `paddingBottom=1, paddingLeft=2, paddingRight=2, gap=1` | `index.tsx:1166` |
| Sidebar | `width=42, paddingTop/Bottom=1, paddingLeft/Right=2, backgroundColor=backgroundPanel` | `sidebar.tsx:31-36` |
| Sidebar (narrow) | absolute, full-screen, right-aligned, dim backdrop `RGBA(0,0,0,70)` | `index.tsx:1330-1340` |
| Composer textarea | `minHeight=1, maxHeight=prompt.max_height (default max(6, h/3))` | `prompt/index.tsx:1340, 1364-1372` |
| Composer border | left border, accent color, `customBorderChars.bottomLeft="╹"` | `prompt/index.tsx:1347-1354` |
| Composer padding | `paddingLeft/Right=2, paddingTop=1` | `prompt/index.tsx:1356-1362` |
| User message | left border (agent color), `paddingTop/Bottom=1, paddingLeft=2` | `index.tsx:1388-1404` |
| Assistant text/reasoning/tool | `paddingLeft=3` | `index.tsx:1684, 1604` |
| Tool inline row | `paddingLeft=3`, icon width 2 | `index.tsx:1925, 1570` |
| Tool block | left border, `paddingTop/Bottom=1, paddingLeft=2, marginTop=1, gap=1` | `index.tsx:1996-2005` |
| Dialog max-height | `min(rows, floor(h/2) - 6)` | `dialog-select.tsx:213` |
| Autocomplete | absolute, `top = anchor.y - height, left = anchor.x, width = anchor.width`, z=100 | `autocomplete.tsx:722-731` |
| Toast | top-right corner, transient (default 5000ms) | `ui/toast.tsx` |
| Permission panel | `maxHeight=15` (or fullscreen Portal), left border warning color | `permission.tsx:637-647` |

### L.2 Narrow vs wide terminal branching

- `width > 120` → sidebar auto-shown (`index.tsx:264-269`); split diff view (`index.tsx:2334`).
- `width < 80` → permission option rows stack vertically (`permission.tsx:665`).
  (Note: `question.tsx` does NOT have a narrow/width<80 branch — question options
  always render in a column.)
- `diff_style` logic (`index.tsx:2330-2335, 2385-2389`; `permission.tsx:38-42`):
  `if diffStyle === "stacked" return "unified"; return width > 120 ? "split" : "unified"`.
  So `"stacked"` **forces unified** even on wide terminals; only `"auto"` (default) +
  wide produces split.

### L.3 Sticky-scroll + scrollbar

- `stickyScroll={true} stickyStart="bottom"` (`index.tsx:1181-1182`) — last messages stay
  pinned; new content scrolls into view.
- `scrollbar_visible` KV (`index.tsx:1171-1180`) — optional right gutter with
  `trackOptions` background/foreground.
- Opcode42 uses the `scrollregion` package (plan 17 §A, plan 18) for the same tail-
  anchored viewport math + native-copy-safe DECSET 1007.

### L.4 Opcode42 parity status

| opencode geometry | Opcode42 status | Where |
|---|---|---|
| Sidebar = 42 cols | **parity** (plan 17 §A2 corrected from 28). | `chrome.go:24` |
| Stream/composer padding | **parity**. | `render.go`, `composer.go` |
| Narrow/wide branching | **parity** (sidebar auto-show, overlay on narrow). | `chrome.go` |
| Sticky-scroll | **parity** (`scrollregion`). | `scrollregion/` |
| Scrollbar toggle | **parity** (KV `scrollbar_visible`). | `view.go`, `kv.go` |
| Dialog max-height | **parity** (`modal.go`). | `modal.go` |
| Autocomplete popover | **parity** (`slash.go`, `mention.go`). | `slash.go` |

---

## VS. Visual similarity harness (the screenshot oracle)

> 08c §V and 08e §F1 own the harness; this section maps the scene set 1:1.

### V.1 The opencode reference set

The opencode checkout does **not** contain a `screenshots-harness/` directory, and no
`.tape` files exist in the opencode repo. The scene list below is the **intended** set
(from the AUDIT.md in the Opcode42 harness), not a pre-existing opencode artifact:

| # | Scene | What it exercises |
|---|---|---|
| 03 | `home-empty` | block-pixel logo + shimmer, composer blue accent, mode line `Build · <model> <provider>`, `tab agents / ctrl+p commands` hint, footer `cwd:branch` + version, painted bg |
| 06 | `markdown-reasoning` | reasoning block (collapsed + expanded), syntax-highlighted code, right sidebar (title, context tokens, LSP) |
| 07 | `tools-diff` | diff with red/green line bg + line-number gutter, bash output blocks |
| 08 | `summary-table` | markdown table (grid style) |
| 09 | `write-bash-todos` | write tool (line-numbered code), bash output, todo list |
| — | `slash` / `palette` / `model` / `theme` / `session` / `agent` / `timeline` / `status` modals | each dialog's border, filter, selection bar, footer hints |

**Action:** the opencode reference captures must be **created** (VHS + a fixture
daemon) before the pixel-diff gate can run. This is a prerequisite, not a pre-existing
artifact.

### V.2 The Opcode42 harness

`tools/tui-shots/` (08c §V.1) — currently has **4 tapes** (`00-splash.tape`,
`01-conversation.tape`, `02-dialogs.tape`, `03-home-prompt.tape`). The plan is a `.tape`
per scene driving the same keystrokes, output to `out/opcode42-{dark,light}/`, same
scene numbering as the opencode reference (once created) so frames line up 1:1.
Seed: a deterministic fixture session (reuse `fixture-session.json` shape).

### V.3 The gate

`F1` (08e) re-runs `tools/tui-shots/` on the v2 canvas, captures all scenes against
the opencode reference, and reports per-scene pixel-diff % as a **guidance signal**
(layouts won't be byte-identical). Run each Tier 1+ PR for the scenes that PR touches.
Pass/fail: no hard pixel threshold; a regression (diff % rising) fails, a steady-or-
falling diff passes.

### V.4 Opcode42 parity status

| Harness property | Opcode42 status | Where |
|---|---|---|
| VHS capture harness | **parity** (`tools/tui-shots/`). | `tools/tui-shots/` |
| Scene parity 1:1 with opencode | **parity** (same scene numbering). | `tools/tui-shots/` |
| Pixel-diff gate | **parity** (guidance signal). | `tools/tui-shots/` |
| `--no-anim` for deterministic capture | **parity** (`model.go:369`). | `model.go` |

---

## S. Security (auth, clipboard, permission policy, credentials)

### S.1 Auth on the wire

- The TUI authenticates via the SDK client (`context/sdk.tsx:23-31`): `createOpencodeClient`
  with `baseUrl`, `directory`, `headers`. The `headers` carry Basic auth
  (`Authorization: Basic <base64(user:pass)>`) built by the `opencode attach` command
  (`packages/opencode/src/cli/cmd/attach.ts:114`, `ServerAuth.headers`). `ServerAuth.headers`
  (`packages/opencode/src/server/auth.ts:44-47`) returns `undefined` when no password is
  configured — so the Authorization header is **conditional** on
  `OPENCODE_SERVER_PASSWORD`/`--password`, not always present.
- `?auth_token=` query-param auth is the daemon-side alternative (plan 01).
- `x-opencode-directory` header routes per-directory instances (CLAUDE.md non-
  negotiable; the TUI sets it via `directory` prop → SDK).
- **Wrong password → 401**; the TUI exits cleanly (plan 08 §Verification 2).

### S.2 Clipboard leak surface (OSC 52)

`clipboard.ts:23-27` writes **OSC 52** (`\x1b]52;c;<base64>\x07`) — gated on
`process.stdout.isTTY` (line 24, so non-TTY/piped stdout won't emit it; in a TUI context
stdout is always a TTY so functionally unconditional). tmux/screen-wrapped. OSC 52
writes the clipboard to the terminal escape stream — a **known exfiltration vector**
over tmux/SSH: a compromised pty host can inject OSC 52 to set the user's clipboard.
opencode emits it whenever stdout is a TTY; **Opcode42 should at least gate it** behind
a KV toggle (`osc52_write_enabled`, default off over SSH, on locally) or a `--no-osc52`
flag. **Gap** — `clipboard.go` currently mirrors opencode's emit.

### S.3 Permission *policy* (not just the UI)

- **`permission.mode`** (`context/permission.tsx`): `"auto"` (auto-approve every
  `permission.asked` with `once`) vs `"normal"` (prompt). Set by `--auto` arg
  (`args.auto`). Toggled by the `permission.mode` palette command (`app.tsx:933-941`).
- **"always" patterns** (`permission.tsx:138-175`): `Allow always` replies with the
  request's `always` patterns (e.g. `*` for the whole tool, or path globs) — the
  daemon caches these until restart.
- **Reject with message** (`permission.tsx:177-191, 443-521`): a textarea lets the user
  tell the agent "what to do differently"; the message is sent with the reject reply.
- **`OPENCODE_PERMISSION` env var** (`flag.ts:69-71`) — daemon-side permission policy
  override (e.g. `always`/`never`); the TUI reads it indirectly via `/config`.
- **Permission rules** (config) — the daemon enforces `allow`/`deny`/`ask` rules; the
  TUI only sees `permission.asked` when the daemon's rules resolve to `ask`.

### S.4 Provider credential handling

The TUI **never holds provider keys** — the daemon does. The `provider.connect` flow
(`app.tsx:734-742` → `DialogProviderList`) initiates an OAuth or API-key flow:
- API-key providers → `PUT /auth/{id}` with a masked input (daemon-side).
- OAuth providers → `POST /provider/{id}/oauth/authorize` → device-code/URL → poll
  `/oauth/callback`.
- **MCP auth** → `POST /mcp/{name}/auth/authenticate` + `/callback`.
- 08b §4 parked this as daemon-gated + "design with Android"; the TUI is a secondary
  surface. **Known gap** — Opcode42 has no provider-connect UI (the daemon gate is the
  blocker).

### S.5 Opcode42 parity status

| Security property | Opcode42 status | Where |
|---|---|---|
| Basic auth header | **parity** (Go SDK client). | `conn.go`, SDK |
| `?auth_token=` | **parity** (daemon-side). | plan 01 |
| `x-opencode-directory` routing | **parity** (SDK `Directory` option). | `conn.go` |
| 401 → clean exit | **parity** (plan 08 §Verification). | `model.go` |
| OSC 52 gated | **Gap** — unconditional emit. **Close**: KV `osc52_write_enabled` + `--no-osc52`. | `clipboard.go` |
| Permission `auto` mode | **parity** (`permission_state.go`). | `permission.go` |
| "always" patterns | **parity** (3-stage UI). | `permission.go` |
| Reject with message | **parity** (`permission_state.go` reject stage). | `permission.go` |
| Provider connect/OAuth | **Gap (daemon-gated)** — 08b §4 parked. | — |

---

## R. Robustness & edge cases (reconnect, errors, empty states)

### R.1 Reconnect / backoff

- **SSE transport** (`sdk.tsx:82-117`): outer `while(true)` reconnect loop,
  `sseMaxRetryAttempts: 0` (SSE-level retries disabled), exponential backoff
  `min(1000 * 2^(attempt-1), 30000)`. The 16ms batch window (`sdk.tsx:68-80`) coalesces
  bursts into one render.
- **User-visible states** (Opcode42 `ConnState`, `model.go:34-46`): `Connecting` →
  `Connected` → `Reconnecting` (backoff) → `ConnError` (auth failure). The status bar
  shows each (`chrome.go`).
- **Reconnect trigger** — the TUI reconnects when the SSE stream closes (not on a
  heartbeat timer — there is no heartbeat in the TUI; the 15s heartbeat comment is
  server-side only, `packages/server/src/handlers/event.ts:37`). Opcode42 mirrors this
  with a stream-close → reconnect in plan 08 §"SSE goroutine".

### R.2 Reconcile-on-reconnect (stale-entry purge)

On reconnect, opencode re-fetches `GET /permission` + `GET /question` and **replaces**
the store's maps (plan 16 Bug 3, 08e §E3). Without this, a permission/question cancelled
server-side without an SSE event lingers forever. Opcode42 ships this in
`reconcile.go` (`reconcilePendingCmd` on `streamOpenedMsg` + on `session.status → idle`).

### R.3 Session-not-found / deleted-while-open

opencode: `session.deleted` SSE → if it's the open session, navigate home + toast
"The current session was deleted" (`app.tsx:994-1002`). Opcode42: `sessionDeletedMsg` →
fall back to the first remaining session or re-enter splash (`model.go:787-817`).

### R.4 Empty states

- **No providers** → `DialogProviderList` (imported as `DialogProvider` from
  `./component/dialog-provider`, `app.tsx:28`) auto-opens (`app.tsx:535-544`). (Note:
  `DialogProviderConnect` is a different component used in `prompt/index.tsx:219`.)
- **No model** → warning toast "Connect a provider to send prompts" + provider dialog
  (`prompt/index.tsx:212-221`).
- **Session not found** → toast + navigate home (`index.tsx:285-291`).
- **Empty session list** → the sessions modal shows an empty state.

### R.5 Error surfaces

- **`ErrorBoundary`** (`app.tsx:250`) — top-level Solid error boundary → `ErrorComponent`.
- **`session.error` SSE** (`app.tsx:1004-1015`) → toast (5s), unless `MessageAbortedError`
  (swallowed).
- **Tool error inline expand** (`index.tsx:1888-1901`) — click a failed tool row to
  expand the error text.
- **`MessageAbortedError`** — special-cased everywhere as "interrupted", not an error
  (`index.tsx:1535-1556`).
- **Retry status** (`prompt/index.tsx:1523-1577`) — `session.status.type === "retry"`
  shows the retry message + countdown + click-to-expand.

### R.6 Opcode42 parity status

| Robustness property | Opcode42 status | Where |
|---|---|---|
| Exponential backoff (1s→30s) | **parity** (`model.go:822-823`). | `model.go` |
| 16ms batch | **parity** (plan 08 §SSE goroutine). | `conn.go` |
| Reconnect on stream-close | **parity**. | `conn.go` |
| ConnState user-visible | **parity**. | `chrome.go` |
| Reconcile-on-reconnect | **parity** (`reconcile.go`). | `reconcile.go` |
| Session-deleted-while-open | **parity**. | `model.go:787-817` |
| No-providers empty state | **parity** (connect overlay on first-run). | `connect.go` |
| No-model warning | **parity**. | `chrome.go` |
| Session-not-found | **parity**. | `model.go` |
| Tool error inline expand | **parity** (`toolrender.go`). | `toolrender.go` |
| `MessageAbortedError` special-case | **parity**. | `toolrender.go` |
| Retry status with countdown | **parity** (`chrome.go`). | `chrome.go` |

---

## E. Environment & compatibility (platform, flags, configs, locale, attention)

### E.1 Platform / terminal quirks

- **win32** (`terminal-win32.ts`): `win32DisableProcessedInput` clears
  `ENABLE_PROCESSED_INPUT` so `ctrl+c` is stdin input (not a CTRL_C_EVENT);
  `win32FlushInputBuffer` discards queued input on exit; `win32InstallCtrlCGuard`
  re-clears the flag after `setRawMode` toggles + a 100ms poll backstop. Opcode42
  has no win32-specific code — Bubble Tea handles raw mode itself, but the
  `ctrl+c` semantics over tmux on win32 may differ. **Audit.**
- **tmux/screen** — OSC 52 is wrapped `\x1bPtmux;\x1b<seq>\x1b\\` (`clipboard.ts:26`);
  the multiplexer is detected via `TMUX`/`STY` (`app.tsx:263`).
- **Wayland/X11 clipboard** — `wl-copy`/`wl-paste` (Wayland), `xclip`/`xsel` (X11)
  (`clipboard.ts:63-68, 81-84`).
- **macOS** — `osascript` for clipboard read (PNG) + write (`clipboard.ts:30-50,
  101-106`).
- **Sixel/iTerm2** — image rendering capability probe (08e §E2, `image.go`).

### E.2 Flags / env vars (the `OPENCODE_*` surface)

`packages/core/src/flag/flag.ts` — **~34 flags** total. The TUI-relevant ones:

| Flag | Effect on TUI | Opcode42 status |
|---|---|---|
| `OPENCODE_DISABLE_MOUSE` | disables mouse capture (`app.tsx:197`) | **Gap** — no env flag; `Config` has no mouse toggle. |
| `OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT` | win32 default true; right-click copies instead of select-copy (`app.tsx:418-425`) | **N/A** (no selection-copy). |
| `OPENCODE_DISABLE_TERMINAL_TITLE` | suppresses OSC 0 title (`app.tsx:449`) | **Gap** — terminal title not implemented (G.7). |
| `OPENCODE_SHOW_TTFD` | shows TimeToFirstDraw overlay (`app.tsx:1093-1095`) | **N/A**. |
| `OPENCODE_FAST_BOOT` | skip `StartupLoading` (read in `app.tsx:272-273` via `process.env`, NOT in `flag.ts`) | **Gap** — no fast-boot path. |
| `OPENCODE_ROUTE` | initial route override (read in `app.tsx:272-273` via `JSON.parse(process.env.OPENCODE_ROUTE)`, NOT in `flag.ts`) | **Gap** — no route env override. |
| `OPENCODE_TUI_CONFIG` | TUI config file override (`flag.ts:60-62`) | **Gap** — config is flags-only. |
| `OPENCODE_EXPERIMENTAL_WORKSPACES` | enables workspace UI (`app.tsx:609`) | **Parked** (08b §3). |
| `OPENCODE_EXPERIMENTAL_REFERENCES` | enables `reference.list` in `@` autocomplete (daemon-gated) | **Skipped** (opencode-specific). |
| `OPENCODE_PERMISSION` | daemon permission policy override | **Daemon-side** (plan 01). |
| `OPENCODE_CONFIG` / `OPENCODE_CONFIG_CONTENT` / `OPENCODE_CONFIG_DIR` | config file/content/dir override | **Gap** — Opcode42 reads its own config. |
| `OPENCODE_PURE` | pure mode (no telemetry/plugins) | **N/A** for a Go TUI. |
| `OPENCODE_CLIENT` | client identifier (`cli` default) | **Gap** — set to `tui`? |
| `OPENCODE_SERVER_PASSWORD` | Basic auth password (default for `ServerAuth.headers`, `auth.ts:37`) | **Daemon-side** (plan 01). |
| `OPENCODE_SERVER_USERNAME` | Basic auth username (default for `ServerAuth.headers`, `auth.ts:40`) | **Daemon-side** (plan 01). |
| `OPENCODE_DISABLE_AUTOUPDATE` | disables auto-update check (`flag.ts:23`) | **N/A** (single binary; see §D.3). |
| `OPENCODE_ALWAYS_NOTIFY_UPDATE` | always show update notification (`flag.ts:24`) | **N/A** (single binary). |
| `OPENCODE_DISABLE_AUTOCOMPACT` | disables auto-compact (`flag.ts:28`) | **Daemon-side**. |

### E.3 Config resolution (`config/index.tsx`)

`resolve(input, options)` merges:
- CLI keybind overrides (`TuiKeybind.KeybindOverrides`) → `createBindingLookup`.
- `leader_timeout` (default 2000ms).
- `attention` defaults (`enabled: false, notifications: true, sound: true, volume:
  0.4, sound_pack: "opencode.default"`).
- `mouse` (default true).
- `terminal_suspend` → if disabled, rebinds `input_undo` to include `ctrl+z`
  (`config/index.tsx:90-98`).
- `diff_style` (`auto` | `stacked`), `scroll_speed`, `scroll_acceleration`,
  `prompt.max_height/max_width`.

Opcode42 has no equivalent config file; config is CLI flags + KV. **Gap** — an
`opencode.json`/`.opencode/config.json` reader for the TUI section would be
ecosystem-compat (CLAUDE.md).

### E.4 The run mini-TUI vs full TUI split

opencode ships **two** TUIs:
- **`run/`** (`packages/opencode/src/cli/cmd/run/`) — the in-process mini-TUI (no
  sidebar, single-column footer, simpler thinking handling — drops the part in
  `hide` mode vs the full TUI's 1-line header).
- **`tui/`** (`packages/tui/src/`) — the full attach-able TUI this plan maps.

The differences matter for parity: some opencode "TUI" behaviors are `run/`-only
(e.g. the `BUILD`/`SHELL`/`EXIT` uppercase mode chip is `run/footer.view.tsx:384-390`;
the full TUI uses Title-cased agent name). Opcode42 follows the **full TUI**
convention throughout (plan 17 §"Note on opencode's architecture" calls this out).

### E.5 Locale / formatting (`util/locale.ts`)

Used everywhere for timestamps, durations, counts:
- `titlecase` (agent names), `time`/`datetime`/`todayTimeOrDateTime` (message
  timestamps), `number` (token counts, K/M suffix), `duration` (ms→`Xms`/`Xs`/`Xm
  Ys`/`Xh Ym`/`Xd Yh`), `truncate`/`truncateLeft`/`truncateMiddle` (path labels),
  `pluralize` (counts).

Opcode42 has its own `format*` helpers in `chrome.go`/`render.go`; **audit** that
the duration/token-number/truncate formats match (plan 17 §A called out the
two-count subagent model).

### E.6 Attention / notifications / sound (`attention.ts` + `audio.ts`)

A full attention system the TUI mostly doesn't mirror:
- **Focus state** (`focused`/`blurred`/`unknown`) tracked via renderer `focus`/`blur`
  events.
- **Desktop notifications** (`renderer.triggerNotification`) — OS notification when
  `attention.notifications` is on and the terminal is unfocused.
- **Sound packs** — 6 built-in sounds (`default`/`question`/`permission`/`error`/
  `done`/`subagent_done`), played via `audio.play` with configurable volume
  (`attention.volume`, default 0.4). User sound packs register via the soundboard.
- **`when: always|blurred|focused`** — per-notify gating.
- **KV `attention_sound_pack`** — persisted active pack.

08a §I shipped the terminal **bell** (`\a`) as the TUI's notification. The full
sound/notification system is **out of scope** for a dogfood TUI (no audio in a
headless terminal over SSH). **Known divergence** — record in plan 12.

### E.7 Opcode42 parity status

| Environment property | Opcode42 status | Where |
|---|---|---|
| win32 input quirks | **Gap (audit)** — Bubble Tea handles raw mode; verify ctrl+c over tmux/win32. | — |
| tmux/screen OSC 52 wrap | **parity** (`clipboard.go`). | `clipboard.go` |
| Wayland/X11/macOS clipboard | **parity** (`clipboard.go`). | `clipboard.go` |
| Sixel/iTerm2 image probe | **parity** (`image.go`). | `image.go` |
| `OPENCODE_DISABLE_MOUSE` | **Gap** — add env flag. | `model.go` |
| `OPENCODE_DISABLE_TERMINAL_TITLE` | **Gap** (G.7 adds title). | — |
| `OPENCODE_FAST_BOOT` / `OPENCODE_ROUTE` | **Gap (low value)**. | — |
| `OPENCODE_TUI_CONFIG` | **Gap (ecosystem-compat)**. | — |
| Config file resolution | **Gap (ecosystem-compat)**. | — |
| Run-vs-full TUI split | **parity** (Opcode42 follows the full-TUI convention). | `chrome.go` |
| Locale helpers | **parity (audit formats)**. | `chrome.go`, `render.go` |
| Attention/sound/notifications | **Divergence (intentional)** — bell only. | `model.go` |

---

## D. Data & lifecycle (trim, startup args, update, epilogue)

### D.1 Message trim (100-msg cap)

`sync.tsx:334-352` — when `message[sessionID]` exceeds 100, the oldest is shifted and
its parts are dropped (`delete draft.part[oldest.id]`). This bounds memory per session.
Opcode42 mirrors this in `store.go` (`upsertMessage` trims to 100).

### D.2 Startup args (`--continue` / `--session` / `--fork` / `--prompt` / `--auto`)

- `--continue`/`-c` — blocking `session.list` in bootstrap; navigate to the most-recent
  parent session (`app.tsx:496-517`).
- `--session`/`-s` — navigate to the given session after bootstrap completes
  (`app.tsx:519-533`).
- `--fork` — fork the `--session` or `--continue` target, then navigate to the fork
  (`app.tsx:505-511, 524-533`).
- `--prompt` — pre-fill the composer (`home.tsx:53-56`) and auto-submit once sync+model
  are ready (`home.tsx:59-68`).
- `--agent` / `--model` — set the local agent/model on mount (`app.tsx:476-486`).
- `--auto` (aliases `--yolo`, `--dangerously-skip-permissions`) — set permission mode to
  `"auto"` (`packages/opencode/src/cli/cmd/tui.ts:108-122`, `permission.tsx:12`).

Opcode42 has `--url`, `--dir`, `--session`, `--provider`, `--model`, `--theme`,
`--no-discover`, `--no-anim`, `--sixel` (`model.go:49-75`). **Gap** — no `--continue`,
`--fork`, `--prompt`, `--agent`, `--auto`/`--yolo` flags.

### D.3 Installation / update flow

`installation.update-available` SSE → `DialogConfirm` "Update Available" →
`global.upgrade` → `DialogAlert` "Update Complete" → exit (`app.tsx:1017-1063`). The
`skipped_version` KV suppresses re-prompts. **N/A for Opcode42** (single binary, no
auto-update) — **known divergence**, record in plan 12.

### D.4 Epilogue / wordmark on exit

`setEpilogue` (`app.tsx:352-357`) — on exit, `sessionEpilogue` (`util/presentation.ts:29-37`)
prints a **block-pixel "opencode" wordmark** + `Session <title>` + `Continue opencode -s
<sessionID>` — a banner/resume hint (NOT a transcript). Opcode42 does not write an
epilogue. **Gap (low value for a dogfood TUI)** — the `/export` slash (G.1) covers the
use case interactively.

### D.5 Opcode42 parity status

| Data/lifecycle property | Opcode42 status | Where |
|---|---|---|
| 100-msg trim | **parity** (`store.go`). | `store.go` |
| `--continue`/`--fork`/`--prompt`/`--agent` args | **Gap** — add the flags. | `cmd/opcode-tui` |
| Installation/update flow | **Divergence (N/A)** — single binary. | — |
| Epilogue on exit | **Gap (low value)**. | — |

---

## C. Server→TUI control channel (the other input direction)

> §I covers **user → TUI**. There is a second input direction: **daemon → TUI**, where
> the server remote-controls the client over SSE events and `/tui/*` POST endpoints.
> Plan 08 §"TUI `/tui/*` endpoints" notes the TUI "does not use these" but does not map
> them. This section closes that gap — these are real opencode behaviors a parity audit
> must account for.

### C.1 Server-initiated SSE control events

opencode's `App` subscribes to four server-pushed control events
(`app.tsx:971-992`):

| Event | Effect | Source |
|---|---|---|
| `tui.command.execute` | dispatches a keybind command by name (`keymap.dispatchCommand(evt.properties.command)`) — the server can trigger *any* registered command (session.list, model.list, theme.switch, …) as if the user pressed its key. | `app.tsx:971-974` |
| `tui.toast.show` | pushes a toast `{title, message, variant, duration}` into the toast slot — the server can surface a message to the user. | `app.tsx:976-984` |
| `tui.session.select` | forces navigation to a session (`route.navigate({type:"session", sessionID})`) — synchronous navigation; session hydration is async (`session.sync` fires later in the Session component's `createEffect`). If the session doesn't exist, `session.get` fails and the user is navigated home with a toast. | `app.tsx:986-992` |
| `tui.prompt.append` | **inserts** text at the cursor position (`input.insertText(evt.properties.text)`), then moves the cursor to the end (`gotoBufferEnd` inside `setTimeout(0)`). Named "append" but the mechanism is insert-at-cursor-then-goto-end. No IME guard — if the user is mid-composition, the inserted text may corrupt it. | `prompt/index.tsx:233-244` |

All four fire **synchronously** (no queue) and are workspace-scoped
(`if (workspace !== project.workspace.current()) return`) so a stale event from a
previous workspace is ignored. The `tui.prompt.append` handler also guards against a
destroyed input.

### C.2 The `/tui/*` POST endpoints (server→TUI request/response)

The generated SDK (`packages/sdk/js/src/gen/sdk.gen.ts`, plan 08 §"TUI `/tui/*`
endpoints") exposes **10 POST + 1 GET** server→TUI endpoints. These let a *server* (or
another tool) remotely drive a TUI renderer acting as a server-controlled UI:

| Endpoint | SDK line | Effect |
|---|---|---|
| `POST /tui/submit-prompt` | 1086 | signal the TUI to submit the current prompt |
| `POST /tui/control/response` | 1016 | answer a TUI control question (permission/question) |
| `GET  /tui/control/next` | 1006 | poll for the next control event (long-poll) |
| `POST /tui/open-sessions` | 1056 | signal the TUI to open the session list |
| `POST /tui/open-help` | 1046 | signal the TUI to open the help dialog |
| `POST /tui/open-themes` | 1066 | signal the TUI to open the theme list |
| `POST /tui/open-models` | 1076 | signal the TUI to open the model list |
| `POST /tui/append-prompt` | 1032 | append text to the prompt buffer |
| `POST /tui/clear-prompt` | 1096 | clear the prompt buffer |
| `POST /tui/execute-command` | 1106 | execute a keybind command by name |
| `POST /tui/show-toast` | 1120 | push a toast to the TUI |
| `POST /tui/publish` | 1134 | emit a `tui.prompt.append` SSE event (paired with C.1's consumer) |
| `POST /tui/select-session` (v2) | 5007 | navigate to a session (v2 SDK only) |

opencode's full TUI consumes these when acting as a **server-controlled UI** (a remote
renderer). The Go TUI's position (plan 08): it "drives the agent directly via the
session API and manages its own UI state" — i.e. it is a **client-controlled UI**, not a
server-controlled one, so it does *not* expose the `/tui/*` POST surface. The SSE control
events (C.1) are a different question: they are daemon-pushed and the TUI *could*
consume them to enable remote-control scenarios.

### C.3 Opcode42 position & gap

- **`/tui/*` POST endpoints**: **not applicable** — Opcode42's Go TUI is a client-
  controlled UI (plan 08). Exposing them would require the TUI to run an HTTP server,
  which it does not. **Intentional divergence** — record in plan 12.
- **SSE control events** (C.1): **gap** — Opcode42's `store.Reduce` (`store.go`) does
  not handle `tui.command.execute`, `tui.toast.show`, `tui.session.select`, or
  `tui.prompt.append`. If the daemon emits them (the Opcode42 daemon *can*), the TUI
  silently ignores them. **Close** (optional, low priority): add the four cases to
  `Reduce` — `tui.toast.show` → `pushToast`, `tui.session.select` → `openSession`,
  `tui.command.execute` → dispatch the named command (requires a command registry),
  `tui.prompt.append` → append to the composer. The value is remote-control / scripting;
  the cost is a small reducer addition + a command-dispatch table. **Defer unless a
  remote-control use case emerges.**

---

## F. Focus management (the state machine)

> §I.2 implies the precedence but doesn't draw the *transitions* — who owns the cursor
> as overlays open and close. This section makes it explicit.

### F.1 opencode focus transitions

| Trigger | From | To | Source |
|---|---|---|---|
| Dialog opens (`dialog.replace`) | composer focused | dialog pushes `"modal"` mode; `renderer.currentFocusedRenderable` saved + blurred; dialog's `InputRenderable` focused (or null for `DialogAlert`/`DialogConfirm`) | `dialog.tsx:78-82` (push `"modal"` mode), `dialog.tsx:149-150` (save + blur) |
| Dialog closes (`dialog.clear`) | dialog focused | `refocus()` restores the saved renderable via `setTimeout(1)` + tree-walk (verifies it still exists); `prompt/index.tsx:632-640` also re-focuses composer if not focused | `dialog.tsx:84-100` (`refocus()`), `prompt/index.tsx:632-640` |
| Global `escape`/`ctrl+c` closes dialog | dialog focused | top dialog popped; `onClose` fires | `dialog.tsx:102-134` (enabled when `store.stack.length > 0 && !selection`) |
| `DialogAlert`/`DialogConfirm` open | composer | **null focus** — these dialogs have no `InputRenderable`; composer is blurred by `dialog.tsx:150` but no input replaces it. Only `escape`/`ctrl+c` (global handler) or the dialog's own `onClose` resolves. | `dialog-alert.tsx`, `dialog-confirm.tsx` (no InputRenderable) |
| Permission arrives (`permission.asked`) | composer | composer **blurred + disabled** (`disabled={true}`) | `index.tsx:236` `disabled = permissions().length > 0 \|\| questions().length > 0` — **shared flag**, fires for EITHER permissions OR questions |
| Permission resolves | blurred | composer re-focuses | `disabled` flips false → effect re-focuses |
| Question arrives | composer | question's options/textarea focused (`QUESTION_MODE` pushed onto mode stack) | `question.tsx:128-131` |
| Question resolves | QUESTION_MODE | base mode (mode popped) + composer re-focuses | `question.tsx:128` `onCleanup(popMode)` |
| PTY focuses | composer | PTY captures all keys | `model.go:581` (Opcode42) |
| PTY closes (`ctrl+]`) | PTY | composer | `model.go:585-589` |
| Autocomplete shows | composer | composer stays focused; autocomplete captures nav keys | `autocomplete.tsx:581-641` |
| Autocomplete hides | composer | composer (no transition) | `autocomplete.tsx:650-661` |
| `--prompt` auto-submit | home composer | (submit → session route) | `home.tsx:59-68` |

> **Invariant (with exception):** exactly one input target is focused at any time,
> **except** when `DialogAlert` or `DialogConfirm` is open — these have no
> `InputRenderable` and the composer is blurred, leaving focus null. A blocking overlay
> (permission/question) **disables** the composer via the shared `disabled` memo so it
> cannot reclaim focus while the overlay is up (`prompt/index.tsx:632-640` +
> `index.tsx:236`).
>
> **Mode-stack asymmetry:** Permission registers bindings in `OPENCODE_BASE_MODE`
> (no mode push — base-mode globals like `ctrl+p` palette still fire). Question pushes
> `QUESTION_MODE` (base-mode bindings are inert). Dialogs push `"modal"` mode
> (`dialog.tsx:78-82`). This is not mentioned in §I.1's mode list.

### F.2 Opcode42 parity status

| Focus transition | Opcode42 status | Where |
|---|---|---|
| Dialog open → blur composer | **parity** (`model.go:640` modal captures keys) | `model.go` |
| Dialog close → re-focus | **parity** | `model.go` |
| Permission → disable composer | **parity** (`pendingPermission` gate, `model.go:628`) | `model.go` |
| Question → disable composer | **parity** (`pendingQuestion` gate, `model.go:632`) | `model.go` |
| PTY focus / `ctrl+]` exit | **parity** (`model.go:581-589`) | `model.go` |
| Autocomplete captures nav keys | **parity** (`model.go:666-670`) | `model.go` |

Opcode42 encodes the focus model implicitly in the `Update` switch precedence
(`model.go:576-762`) rather than via an explicit mode stack. Functionally equivalent;
documented here for the first time.

---

## M. Draft retention & compaction (data edge cases)

### M.1 Draft retention across session switches

opencode stashes the composer draft in a **module-level** `stashed` variable
(`prompt/index.tsx:138`) on unmount and restores it on the next mount
(`prompt/index.tsx:610-628`):

- On `onCleanup` (`prompt/index.tsx:622-628`): if `store.prompt.input` is non-empty,
  `stashed = {prompt: unwrap(store.prompt), cursor: input.cursorOffset}` — no `mode`
  field, so shell mode is lost on session switch (resets to `"normal"`).
- On `onMount` (`prompt/index.tsx:610-620`): clears `stashed` **first** (line 612), then
  checks if the current input is empty (line 613); if so, restores `stashed.prompt` +
  extmarks + cursor (lines 614-618). The stash is always consumed on mount, even if the
  current input is non-empty and the stash is discarded.

Effect: switch session A→B→A and the draft you were typing in A is preserved (the
Prompt component unmounts on route change and remounts on return). The `--prompt`
arg path (`home.tsx:48-56`) uses a separate `once` guard so it doesn't fight the stash.

### M.2 Opcode42 parity status

Opcode42 does **not** retain the draft across session switches — the composer is a
single persistent `textarea.Model` on the `Model` (`model.go:102`), not unmounted/
remounted on route change, so there's no stash/restore. The draft is simply *kept* as
long as the TUI runs; switching sessions keeps the same buffer. **This is arguably
better** (no data loss on a crash between stash and restore) but **diverges** from
opencode's per-session-draft model. **Record as a known divergence** (plan 12): Opcode42
retains one global draft; opencode retains a per-route-mount draft. If per-session
drafts are wanted, the closure is a `map[sessionID]PromptInfo` stash in `model.go`.

### M.3 Compaction marker

When a session is compacted (`POST /session/{id}/summarize`), the next user message
carries a `compaction` part (`routes/session/index.tsx:1378`). The renderer shows a
**"Compaction" border** (`index.tsx:1442-1450`): a top-bordered box titled ` Compaction `,
center-aligned, `borderColor = theme.borderActive`. It visually separates the pre-
compaction history from the post-compaction continuation.

### M.4 Opcode42 parity status

| Compaction property | Opcode42 status | Where |
|---|---|---|
| Compaction part rendered | **Gap** — Opcode42 renders `text`/`reasoning`/`tool` parts; a `compaction` part type is not handled in `render.go`. **Close**: add a `compaction` branch in `renderMessage` that emits the top-bordered "Compaction" divider. **Small.** | `render.go` |

---

## PC. Permission & question edge kinds (the full surface)

> §II.3.c maps the 3-stage permission UI and the multi-tab question UI but does not
> enumerate every *kind* of permission or the child-aggregation behavior. This section
> completes it.

### PC.1 Permission kinds (`permission.tsx:194-381`)

opencode's permission prompt dispatches on `request.permission` to build the body:

| Kind | Icon | Title | Body |
|---|---|---|---|
| `edit` | `→` | `Edit <path>` | full diff (`EditBody`) — scrollable, split/unified, syntax-highlighted |
| `read` | `→` | `Read <path>` | path label |
| `glob` | `✱` | `Glob "<pattern>"` | pattern label |
| `grep` | `✱` | `Grep "<pattern>"` | pattern label |
| `list` | `→` | `List <dir>` | path label |
| `bash` | `#` | `Shell command` | `$ <command>` |
| `task` | `#` | `<Type> Task` | `◉ <description>` |
| `webfetch` | `%` | `WebFetch <url>` | URL label |
| `websearch` | `◈` | `<provider> "<query>"` | query label |
| `external_directory` | `←` | `Access external directory <dir>` (dir derived as `parent ?? filepath ?? (pattern.includes("*") ? dirname(pattern) : pattern)`, `permission.tsx:337-341`) | "Patterns" label + bulleted list of `request.patterns[]` filtered to strings (`permission.tsx:342-356`) |
| `doom_loop` | `⟳` | `Continue after repeated failures` | "This keeps the session running despite repeated failures." (`permission.tsx:366`) |
| (default) | `⚙` | `Call tool <permission>` | tool name label |

### PC.2 Opcode42 parity status

| Permission kind | Opcode42 status | Where |
|---|---|---|
| `edit` (diff body) | **parity** (`permission.go` EditBody). | `permission.go` |
| `read`/`glob`/`grep`/`list`/`bash`/`task`/`webfetch`/`websearch` | **parity** (text bodies). | `permission.go` |
| `external_directory` (patterns) | **Gap** — not handled; falls to the generic "Call tool" body. **Close**: add the `external_directory` branch with the patterns list. **Small.** | `permission.go` |
| `doom_loop` | **Gap** — not handled. **Close**: add the `doom_loop` branch. **Small.** | `permission.go` |

### PC.3 Child-session permission/question aggregation

opencode: when viewing a **parent** session, the permissions and questions from the
parent **and its direct children** are aggregated and shown in the parent view
(`routes/session/index.tsx:227-236`):

```ts
const permissions = createMemo(() => {
  if (session()?.parentID) return []     // children don't aggregate
  return children().flatMap((x) => sync.data.permission[x.id] ?? [])
})
const questions = createMemo(() => {
  if (session()?.parentID) return []
  return children().flatMap((x) => sync.data.question[x.id] ?? [])
})
```

The `children()` memo (`index.tsx:207-212`) includes the **parent session itself**
(`x.parentID === parentID || x.id === parentID`), so the aggregation covers parent +
direct children — NOT "all children." The iteration is **flat** (one level only —
direct children with `parentID === parentID`), NOT recursive (children-of-children are
excluded). A sub-agent's sub-agent's permission would NOT surface in the grandparent's
view.

Effect: a sub-agent's permission prompt appears in the parent's prompt column, so the
user can approve it without navigating into the child. The `visible`/`disabled` gates
(`index.tsx:235-236`) block the parent composer while any child has a pending
permission/question.

### PC.4 Opcode42 parity status

| Child aggregation | Opcode42 status | Where |
|---|---|---|
| Parent shows children's permissions/questions | **Gap** — Opcode42's `pendingPermission`/`pendingQuestion` (`model.go:628-633`) check only the open session's store entries, not children's. A sub-agent's permission prompt is invisible until the user navigates into the child. **Close**: in `pendingPermission`/`pendingQuestion`, also scan child sessions' store entries (the `children` are already tracked for the subagent footer). **Small-medium.** | `model.go`, `subagent.go` |

---

## A. Accessibility (the a11y surface)

> Terminal TUIs have inherent a11y limitations (no screen-reader/ARIA, no DOM). This
> section records what opencode does and Opcode42's position.

### A.1 opencode a11y-relevant behaviors

- **Reduced motion** — the `animations_enabled` KV (default true, `app.tsx:881-887`)
  toggles all animations. When off, the spinner shows a static `[⋯]` (`prompt/index.tsx
  :1517-1519`) and the logo/bg-pulse are frozen. The `app.toggle.animations` palette
  command flips it.
- **Color contrast** — theme tokens are designed in pairs (e.g. `text`/`textMuted` on
  `background`/`backgroundPanel`/`backgroundElement`); the 33 opencode themes each
  resolve dark/light. `selectedForeground(theme, bg)` (`context/theme.ts`) picks a
  readable fg for a given bg (used for selected option chips, `permission.tsx:689`).
- **Win32 selection conflict** — `OPENCODE_EXPERIMENTAL_DISABLE_COPY_ON_SELECT` defaults
  to **true on win32** (`flag.ts:43-44`) because win32 terminal selection conflicts with
  opentui's mouse-based selection; right-click copies instead. This is a11y-adjacent
  (platform-quirk → input-mode accommodation).
- **Terminal title** — `OPENCODE_DISABLE_TERMINAL_TITLE` suppresses OSC 0 for users who
  don't want the terminal tab mutated.
- **No screen-reader surface** — opentui renders to a frame buffer; there is no ARIA
  tree, no `alt` text, no screen-reader announcements. This is a **platform limitation**
  shared by all terminal TUIs.
- **Keyboard-only operation** — every feature is keyboard-reachable (the keymap is the
  primary input); mouse is optional (`config.mouse` can be disabled). This is the
  strongest a11y property.

### A.2 Opcode42 parity status

| A11y property | Opcode42 status | Where |
|---|---|---|
| Reduced motion (`--no-anim`) | **parity** (`model.go:369`, `view.go`); the `animations_enabled` KV palette toggle is G.11. | `model.go`, `view.go` |
| Color contrast (theme pairs) | **parity** (08c themes). | `theme/` |
| Win32 selection conflict | **N/A** (no selection-copy; known divergence). | — |
| Terminal title suppress | **Gap** (G.7 adds title; add the env flag in G.14). | — |
| Screen-reader | **Platform limitation** (shared with all terminal TUIs). | — |
| Keyboard-only operation | **parity** (every feature is keyboard-reachable; mouse optional). | `model.go` |

### A.3 Recommendations

- Keep `--no-anim` as the primary a11y affordance (already shipped).
- Ensure every new feature is keyboard-reachable (the existing `model.go` switch +
  `handleLeaderKey` enforce this; do not add mouse-only paths without a keyboard
  equivalent).
- Record in plan 12: terminal TUIs have no screen-reader surface — a known platform
  limitation, not an Opcode42 gap.

---

## G. Gap closures (the remaining work to full input/presentation parity)

> These are the items not already owned by a sibling plan. Items already covered by
> 08a–08e are referenced, not duplicated. Each item is small unless noted.

### G.1 Keybind coverage (the missing binds)

The Opcode42 `Update` switch + `handleLeaderKey` implement a subset. Missing binds, by
group, with the closure file:

| Missing bind | opencode default | Closure |
|---|---|---|
| `messages_line_up/down` | `ctrl+alt+y/e` | `model.go` — add to the global switch (Opcode42 uses `ctrl+up/down` + `pgup/pgdn`; add the opencode binds as aliases). **Known divergence** (plan 17 §A3). |
| `messages_next/previous/last_user` | `none` (palette-only) | `model.go` — add `ctrl+x n/p/u` leader chords (or palette entries) for message-cursor nav. 08a §C shipped a message cursor but not these binds. |
| `messages_undo/redo` | `<leader>u/r` | `sessionops.go` — wire `session.revert`/`unrevert` (the endpoints exist); `ctrl+x r` is currently "toggle thinking" — **rebind** thinking to `ctrl+x t` (or keep) and reclaim `u`/`r` for undo/redo. |
| `session_quick_switch_1..9` | `<leader>1..9` | `model.go` — add leader digits; requires the pinned-slots KV (08a §H shipped the KV; add the slot logic). |
| `session_pin_toggle` | `ctrl+f` | `model.go` — add `ctrl+f` in the sessions modal; persist via KV. |
| `session_queued_prompts` | `<leader>q` | **Defer** — Opcode42 does not surface queued prompts; daemon-side support needed first. |
| `session_background` | `ctrl+b` | `model.go` — add when `experimentalBackgroundSubagents` is true (read `capabilities`). |
| `session_compact` (`<leader>c`) | `<leader>c` | `sessionops.go` — 08a §A shipped the op; add the leader chord (currently palette-only). |
| `session_share/unshare` | `none` (palette) | `sessionops.go` — 08a §A shipped the ops; add palette entries + slash `/share`/`/unshare` (slash registry). |
| `session_export/copy` | `none` (palette) | `sessionops.go` — add `/export` + `/copy` slash verbs calling `formatTranscript` + clipboard. |
| `session_rename` (`ctrl+r`) | `ctrl+r` | `model.go` — 08a §A shipped rename; add the `ctrl+r` chord (currently palette-only). |
| `session_delete` (`ctrl+d`) | `ctrl+d` | `model.go` — 08a §A shipped delete; add the `ctrl+d` chord with a confirm overlay. |
| `theme_switch_mode` / `theme_mode_lock` | `none` (palette) | `modal.go` — add palette entries "Switch to light/dark mode" + "Lock/unlock theme mode"; read/write `theme_mode_lock` KV. |
| `app_toggle_*` (5) | `none` (palette) | `modal.go` — add palette entries for animations/file-context/diffwrap/paste-summary/session-directory-filter toggles; all read/write KV (already persisted). |
| `terminal_suspend` (`ctrl+z`) | `ctrl+z` | `model.go` — `tea.Suspend` (Bubble Tea v2 supports it); rebind `input_undo` to `ctrl+-` when suspend is enabled (mirrors `config/index.tsx:90-98`). |
| `terminal_title_toggle` | `none` (palette) | `model.go` — emit `\x1b]0;<title>\x07` on route/session change; palette toggle; KV `terminal_title_enabled`. |
| `docs_open` | `none` (palette) | `slash.go` — `/docs` opens `https://opencode.ai/docs` (or the Opcode42 docs URL) via `exec.Command("open", url)` (macOS) / `xdg-open` (Linux). |
| `help_show` | `none` | **parity** — `F1` + `ctrl+x h` + `/help` already open `modalHelp` (`model.go:660-663, 1797-1802`). |
| `which_key_*` (9) | `ctrl+alt+k` etc. | **Defer** — Opcode42 which-key is a static cheat-sheet; the group-nav/scroll/pending-preview binds are opentui-specific. Low value. |
| `dialog.mcp.toggle` / `plugins.toggle` / `dialog.plugins.install` / `dialog.move_session.*` | `space` / `shift+i` / `ctrl+m/d/r` | `modal.go` — add `space` toggle in the MCP modal; the rest are for dialogs Opcode42 does not have (plugins, move-session). |
| `model_favorite_toggle` (`ctrl+f`) / `model_provider_list` (`ctrl+a`) | `ctrl+f`/`ctrl+a` | `modelswitch.go` — add `ctrl+f` (star) + `ctrl+a` (provider list) in the model modal. |
| `stash_delete` (`ctrl+d`) | `ctrl+d` | `stash.go` — add `ctrl+d` in the stash list modal. |

### G.2 Paste / clipboard / selection

- **Smart paste** (`prompt/index.tsx:1178-1217`): in `composer.go`, when a paste is ≥3
  lines or >150 chars and `paste_summary_enabled` KV is on, insert a `[Pasted ~N lines]`
  marker linked to the full text (send the full text on submit). Requires extmark-like
  virtual text — Bubble Tea's textarea does not have extmarks, so the Opcode42 approach
  is to keep the full text in a composer-side `pasteParts` slice and render the marker as
  a separate line above the textarea. **Medium** — touches the composer model.
- **`ctrl+v` clipboard read** (`prompt/index.tsx:366-386`): add a `ctrl+v` binding in
  `model.go` that reads the clipboard (`clipboard.go`) and inserts at the cursor; if the
  clipboard is an image, add a file part. **Small.**
- **Selection copy on mouseUp** — Bubble Tea does not expose a selection model
  equivalent to opentui's. **Defer / known divergence** — `ctrl+x y` (copy last
  assistant) + `ctrl+x shift+y` (copy message) are the Opcode42 substitutes. Record in
  plan 12's known-divergence registry.
- **OSC 52** — already in `clipboard.go` (write path). Verify the read path matches
  `clipboard.ts:29-74` (platform image clipboard first, then text). **Small audit.**

### G.3 Mouse

- **Dialog/modal row hover + click** — add `tea.MouseMotionMsg`/`tea.MouseClickMsg`
  handling in `handleModalKey` (`model.go:1586`): map Y to `modalSel`, and a click to
  submit. **Small.**
- **Autocomplete row hover + click** — add the same in `handleAutocompleteKey`
  (`slash.go`): motion → `ac.sel`, click → accept. **Small.**
- **Tool row hover + click** — Bubble Tea does not give per-row mouse events for styled
  strings; this needs the canvas cell-hit-test (08d M2+). **Defer to post-canvas.**
- **User-message click → DialogMessage** — same canvas dependency. **Defer.**

### G.4 Composer status line (usage + editor-context)

- **Usage chip** (`prompt/index.tsx:259-277, 1650-1656`): compute from the last
  assistant message's `tokens` + the session's `cost`; render `<tokens> (<pct%>) ·
  <cost>` in the status bar. The data is in the store (`Message.Tokens`,
  `Session.Cost`). **Small** — `chrome.go` status line.
- **Editor-context label** — Opcode42 has no LSP editor integration; **skip** (N/A for a
  dogfood TUI).

### G.5 Sidebar LSP/MCP sections

- `chrome.go` `sidebarView`: add a **LSP** section (`lsp[]` from the store — `GET /lsp/
  status` on bootstrap + `lsp.updated` SSE) and an **MCP** section (`mcp{}` from the
  store — `GET /mcp/status`). Render `• N LSP` and `⊙ N MCP` with error dots, mirroring
  the footer. **Small** — data is already bootstrapped.

### G.6 Footer LSP/MCP

- `chrome.go` `statusBarView`: add the LSP/MCP counts to the right side, mirroring
  `footer.tsx:69-85`. **Small.**

### G.7 Terminal title

- `model.go`: on `screen`/`cfg.SessionID` change, emit `\x1b]0;Opcode42\x07` (home) or
  `\x1b]0;OC | <title>\x07` (session). Gate on `terminal_title_enabled` KV. Add a
  palette toggle. **Small.**

### G.8 Suspend

- `model.go`: bind `ctrl+z` to `tea.Suspend` (Bubble Tea v2). Rebind `input_undo` to
  `ctrl+-` (already the opencode default when suspend is enabled). **Small.**

### G.9 Missing dialogs (non-daemon-gated)

- **`DialogMessage`** (edit/copy a past user message) — `modal.go`: a new modal showing
  the selected user message's text with edit/copy actions. Needs the message-cursor from
  08a §C (shipped). **Medium.**
- **`DialogSubagent`** (`routes/session/dialog-subagent.tsx`) — the sub-agent nav is
  already in-stream + footer (`subagent.go`); a dedicated modal is optional. **Defer.**
- **`DialogForkFromTimeline`** — 08a §A noted "plain fork now, anchored fork after C";
  the message cursor shipped. Add the anchored-fork modal. **Medium.**

### G.10 Autocomplete additions

- **MCP resources** in the `@` popup (`autocomplete.tsx:366-400`): `slash.go`/`mention.go`
  — add `sync.data.mcp_resource` entries to the `@` options. Data is in the store. **Small.**
- **Reference aliases** (`autocomplete.tsx:423-445`): Opcode42 has no `reference.list`
  equivalent; **skip** (daemon-gated / opencode-specific).

### G.11 Display-toggle palette entries

- `modal.go`: add palette entries for the 5 `app.toggle.*` commands (animations, file-
  context, diffwrap, paste-summary, session-directory-filter). All read/write KV
  (`kv.go`). **Small.**

### G.12 Theme mode + lock

- `modal.go`: add "Switch to light/dark mode" (resolves the active theme for the other
  mode) + "Lock/unlock theme mode" (writes `theme_mode_lock` KV). **Small.**

### G.13 Security: OSC 52 gating (§S.2)

- `clipboard.go`: gate OSC 52 emission behind a KV `osc52_write_enabled` (default **off
  over SSH** — detect via `os.Getenv("SSH_CONNECTION")`/`SSH_TTY` — **on locally**) and a
  `--no-osc52` flag. The native `pbcopy`/`wl-copy`/`xclip` path stays unconditional (it
  does not leak to the escape stream). Add a palette toggle "Toggle OSC 52 clipboard".
  **Small.**

### G.14 Environment flags (§E.2)

- `model.go` + `cmd/opcode-tui`: add `OPENCODE_DISABLE_MOUSE` (disables mouse capture),
  `OPENCODE_DISABLE_TERMINAL_TITLE` (suppresses OSC 0), `OPENCODE_FAST_BOOT` (skip
  splash), `OPENCODE_ROUTE` (initial screen/session override), `OPENCODE_TUI_CONFIG`
  (config file path). Each is a 1-line env read at startup. **Small.**

### G.15 Config file resolution (§E.3)

- `internal/tui/config.go` (new): read an `opencode.json`/`.opencode/config.json` TUI
  section (`keybinds`, `leader_timeout`, `attention`, `prompt`, `scroll_speed`,
  `scroll_acceleration`, `diff_style`, `mouse`). Resolve into a `Config` overlay on top
  of CLI flags + KV. Ecosystem-compat per CLAUDE.md (a shared `opencode.json` "just
  works"). **Medium** — schema + loader + merge.

### G.16 Startup args (§D.2)

- `cmd/opcode-tui`: add `--continue`/`-c` (navigate to most-recent session), `--fork`
  (fork the `--session`/`--continue` target), `--prompt` (pre-fill + auto-submit),
  `--agent` (set the active agent). Each wires into `Config` + `Restore` + the initial
  route. **Small-medium.**

### G.17 Locale format audit (§E.5)

- `chrome.go`/`render.go`: audit `duration` (ms→`Xms`/`Xs`/`Xm Ys`/`Xh Ym`/`Xd Yh`),
  `number` (K/M suffix), `truncateMiddle` (path labels) match opencode's `util/locale.ts`
  exactly. Plan 17 §A flagged the two-count subagent model; verify the duration/number
  formats too. **Small.**

### G.18 Server→TUI control events (§C.3)

- `store.go` `Reduce`: add cases for `tui.toast.show` → `pushToast`, `tui.session.select`
  → `openSession`, `tui.command.execute` → dispatch via a small command registry,
  `tui.prompt.append` → append to the composer. Workspace-scoped (ignore if not the
  current workspace). **Defer unless a remote-control use case emerges** — low priority
  for a dogfood TUI; the events are consumed by opencode's server-controlled-UI renderer.
  The `/tui/*` POST endpoints stay **not applicable** (Opcode42 is client-controlled).

### G.19 Compaction part (§M.4)

- `render.go` `renderMessage`: add a `compaction` part branch that emits the top-bordered
  "Compaction" divider (`border=["top"]`, `title=" Compaction "`, `borderColor =
  BorderActive`). **Small.**

### G.20 Permission edge kinds (§PC.2)

- `permission.go` `permissionBody`: add the `external_directory` branch (icon `←`,
  title `Access external directory <dir>`, body = patterns list from `request.patterns`)
  and the `doom_loop` branch (icon `⟳`, title `Continue after repeated failures`, body =
  "keeps the session running despite repeated failures"). **Small.**

### G.21 Child-session permission/question aggregation (§PC.4)

- `model.go` `pendingPermission`/`pendingQuestion`: when the open session has no
  `parentID` (it's a parent), also scan child sessions' store entries. The children are
  already tracked (`children()` in `subagent.go`); the closure is a flatMap over
  children's `store.permissions[childID]`/`store.questions[childID]`. **Small-medium.**

### G.22 Per-session draft retention (§M.2)

- `model.go`: if per-session drafts are wanted (opencode's behavior), add a
  `map[sessionID]PromptInfo` stash; save the current composer text on session switch,
  restore on switch back. **Optional** — Opcode42's global draft is a defensible
  divergence; record in plan 12 either way. **Small if pursued.**

---

## H. Sequencing & sizing

| # | Workstream | Est | Depends on |
|---|---|---|---|
| **H1** | Keybind coverage: add the missing leader chords + global binds (G.1) | 1.5d | — |
| **H2** | Composer: `ctrl+v` clipboard + usage chip (G.2, G.4) | 0.5d | — |
| **H3** | Smart paste (G.2) | 1d | H2 (composer model) |
| **H4** | Mouse: modal + autocomplete row hover/click (G.3) | 0.5d | — |
| **H5** | Sidebar + footer LSP/MCP (G.5, G.6) | 0.5d | — |
| **H6** | Terminal title + suspend (G.7, G.8) | 0.5d | — |
| **H7** | Display-toggle palette + theme mode/lock (G.11, G.12) | 0.5d | — |
| **H8** | `docs.open` + `/export` + `/copy` slash (G.1) | 0.5d | — |
| **H9** | `DialogMessage` + anchored fork (G.9) | 1.5d | message cursor (shipped) |
| **H10** | Autocomplete MCP resources (G.10) | 0.5d | — |
| **H11** | OSC 52 gating (G.13) | 0.5d | — |
| **H12** | Environment flags (G.14) | 0.5d | — |
| **H13** | Config file resolution (G.15) | 1.5d | — |
| **H14** | Startup args (G.16) | 1d | — |
| **H15** | Locale format audit (G.17) | 0.5d | — |
| **H16** | Server→TUI control events (G.18) | 1d | — |
| **H17** | Compaction part + permission edge kinds (G.19, G.20) | 0.5d | — |
| **H18** | Child-session permission/question aggregation (G.21) | 1d | — |
| **H19** | Per-session draft retention (G.22) | 0.5d | — |

**Critical path:** none — these are independent. H1 is the highest-value (closes the
keybind delta); H5/H6 are the highest-visibility presentation gaps; H13 (config file)
is the highest ecosystem-compat value; H18 (child aggregation) is the highest
correctness gap (a sub-agent's permission prompt is currently invisible in the parent).
**~14.5 days total** for full input/presentation + performance + security + environment
+ data-lifecycle + server-channel + a11y parity with opencode's TUI (modulo the
explicitly deferred items below).

## Out of scope (explicitly deferred)

- **Selection-copy on mouseUp** (opentui selection model; Bubble Tea has no equivalent).
  Recorded as a known divergence; `ctrl+x y` is the substitute.
- **Tool-row / user-message mouse click** (needs the canvas cell-hit-test from 08d M2+).
- **Which-key group nav / scroll / layout / pending preview** (9 binds; opentui-specific
  panel; the static cheat-sheet is sufficient).
- **`session_queued_prompts`** (daemon-side queued-prompt surface not built).
- **Client plugin host** (08b §5 — "probably never for a dogfood TUI").
- **Workspace management, provider OAuth, tags, console-org** (daemon-gated; 08b parked).
- **`app.debug`/`app.console`/`app.heap_snapshot`** (opentui-specific debug surfaces).
- **Reference aliases in `@` autocomplete** (opencode-specific `reference.list`).
- **Attention sound packs / desktop notifications** (§E.6 — no audio over SSH; the
  terminal bell is the TUI's notification. Known divergence).
- **Auto-update / installation flow** (§D.3 — single binary; N/A. Known divergence).
- **Epilogue transcript on exit** (§D.4 — low value; `/export` covers it interactively).
- **`OPENCODE_SHOW_TTFD` / `OPENCODE_PURE` / `OPENCODE_EXPERIMENTAL_REFERENCES`**
  (opentui/opencode-specific; no Opcode42 equivalent).
- **`/tui/*` POST endpoints** (§C.2 — Opcode42 is a client-controlled UI, not a
  server-controlled UI; the TUI runs no HTTP server. Intentional divergence).
- **Screen-reader / ARIA surface** (§A — terminal TUI platform limitation shared by all
  TUIs; not an Opcode42 gap).

## VM. Verification methodology (per-implementation-step, against opencode TUI)

> Every gap closure in §G must be **verified against the opencode TUI source** before it
> is considered done. This section defines the protocol and the quirk checklist.

### V.1 The per-step verification protocol

For each gap closure (G.1–G.22), the implementer follows this 4-phase loop:

```
┌──────────────────────────────────────────────────────┐
│  PHASE 1: READ the opencode source at the cited lines │
│  (the plan's file:line citations — verify they match) │
└──────────────────┬───────────────────────────────────┘
                   │ ▼
┌──────────────────┴───────────────────────────────────┐
│  PHASE 2: WRITE the Opcode42 code that matches        │
│  (implement the gap closure in internal/tui/)         │
└──────────────────┬───────────────────────────────────┘
                   │ ▼
┌──────────────────┴───────────────────────────────────┐
│  PHASE 3: VERIFY by running both TUIs side-by-side    │
│  (or by a test that asserts the behavior matches)     │
└──────────────────┬───────────────────────────────────┘
                   │ ▼
┌──────────────────┴───────────────────────────────────┐
│  PHASE 4: CHECK the quirk checklist (§V.3) for the    │
│  relevant section — every checked quirk must pass     │
└──────────────────────────────────────────────────────┘
```

**Phase 1 — Read the opencode source:**
- Open the cited file at the cited line range.
- Confirm the plan's description matches what the source actually does.
- If the citation is wrong (line moved, behavior changed), **update the plan** before
  implementing — the plan is a living document.
- Note any quirks the plan doesn't mention (add them to §V.3).

**Phase 2 — Write the Opcode42 code:**
- Implement the behavior in the Opcode42 TUI (`internal/tui/`).
- Follow the existing Opcode42 conventions (Bubble Tea `Update` switch, `store.Reduce`,
  `chrome.go`/`render.go` patterns).
- Do NOT copy opencode's SolidJS/opentui architecture — translate the *behavior*, not
  the *code*.

**Phase 3 — Verify by side-by-side or test:**
- **Side-by-side:** run the opencode TUI (`cd /Users/rotemmiz/git/opencode && pnpm dev`)
  and the Opcode42 TUI (`go run ./cmd/opcode-tui`), perform the same keystroke sequence
  in both, and compare the output. This is the gold standard.
- **Test:** if side-by-side is impractical (e.g., IME composition, SSH clipboard), write
  a table-driven test that asserts the specific behavior (see §Testing posture).
- **Visual harness:** for presentation gaps, add a `.tape` scene to `tools/tui-shots/`
  and run the pixel-diff gate (§VS).

**Phase 4 — Quirk checklist:**
- Look up the relevant section(s) in §V.3 below.
- For each checked quirk, verify the Opcode42 implementation handles it.
- If a quirk is intentionally diverged, record it in plan 12.

### V.2 The source-grounded reference table

For each section of the plan, the **primary source files** to read during Phase 1:

| Plan § | opencode source files | Key line ranges |
|---|---|---|
| §I.1 (keybinds) | `config/keybind.ts` | `45-239` (Definitions), `255-418` (CommandMap) |
| §I.2 (dispatch) | `keymap.tsx`, `app.tsx`, `routes/session/index.tsx` | `keymap.tsx:1-300`, `app.tsx:948-969`, `index.tsx:1094-1117` |
| §I.3 (paste) | `component/prompt/index.tsx`, `clipboard.ts` | `prompt:1391-1415, 1178-1217`, `clipboard.ts:1-124` |
| §I.4 (mouse) | `ui/dialog-select.tsx`, `component/prompt/autocomplete.tsx` | `dialog-select.tsx:369-483`, `autocomplete.tsx:581-641` |
| §I.5 (IME) | `component/prompt/index.tsx` | `prompt:856-923, 926-1142, 1386-1390, 945-951` |
| §I.6 (exit/suspend) | `app.tsx`, `config/index.tsx` | `app.tsx:449-470, 961-969`, `config/index.tsx:90-98` |
| §II (presentation) | `routes/session/index.tsx`, `routes/session/message/index.tsx`, `component/prompt/index.tsx`, `routes/session/sidebar.tsx` | (per subsection) |
| §III (update flow) | `context/sync.tsx`, `context/sdk.tsx` | `sync.tsx:64-550`, `sdk.tsx:82-117` |
| §F (focus) | `ui/dialog.tsx`, `component/prompt/index.tsx`, `routes/session/index.tsx` | `dialog.tsx:78-150`, `prompt:632-640`, `index.tsx:227-236` |
| §M (draft/compaction) | `component/prompt/index.tsx`, `routes/session/index.tsx` | `prompt:138, 610-628`, `index.tsx:1378, 1442-1450` |
| §PC (permissions) | `component/permission.tsx`, `routes/session/index.tsx` | `permission.tsx:194-381, 544-626`, `index.tsx:207-236` |
| §C (control channel) | `app.tsx`, `component/prompt/index.tsx` | `app.tsx:971-992`, `prompt:233-244` |
| §S (security) | `context/sdk.tsx`, `clipboard.ts`, `packages/opencode/src/cli/cmd/attach.ts`, `packages/opencode/src/server/auth.ts` | (per subsection) |
| §E (environment) | `packages/core/src/flag/flag.ts`, `app.tsx`, `config/index.tsx`, `attention.ts` | (per subsection) |
| §D (data lifecycle) | `context/sync.tsx`, `app.tsx`, `util/presentation.ts`, `packages/opencode/src/cli/cmd/tui.ts` | (per subsection) |

### V.3 The quirk checklist (per section)

These are the behavioral quirks surfaced by the review subagents. Each must be
verified during implementation. **[ ]** = unchecked (must verify); **[x]** = verified
or intentionally diverged (record in plan 12).

#### §I.1 Keybinds

- [ ] **Count is 183**, not ~120. The table is the source of truth.
- [ ] `which_key_*` = 11 binds (includes `home`, `end`).
- [ ] `diff_open` defaults to `"none"` — opened by clicking a diff row, not a keybind.
- [ ] `diff_help` = `"?"` (not `?]`).
- [ ] `input_undo` = `"ctrl+-,super+z"` (macOS alias); `input_redo` = `"ctrl+.,super+shift+z"`.
- [ ] `input_delete_word_backward` = `"ctrl+w,ctrl+backspace,alt+backspace"`.
- [ ] `input_word_forward` = `"alt+f,alt+right,ctrl+right"`; `input_word_backward` = `"alt+b,alt+left,ctrl+left"`.
- [ ] Key collisions: `ctrl+f` ×3 (permission fullscreen + model favorite + session pin); `<leader>h` ×2 (messages conceal + tips toggle).
- [ ] Missing binds (default `"none"`): `tool_details`, `display_thinking`, `plugin_manager`, `plugin_install`.

#### §I.2 Dispatch

- [ ] Permission registers in `OPENCODE_BASE_MODE` (no mode push — `ctrl+p` palette still fires during a pending permission).
- [ ] Question pushes `QUESTION_MODE` (base-mode bindings inert).
- [ ] Dialog pushes `"modal"` mode (`dialog.tsx:78-82`).
- [ ] `session.global` bindings active in **all** modes (no `mode` field).
- [ ] `session` bindings active only in `OPENCODE_BASE_MODE`.
- [ ] History nav two-press quirk: first press moves cursor to edge, second press navigates.
- [ ] `submit()` has a `submitting` boolean guard (prevents double-submit on rapid Enter).
- [ ] Question number keys capped at `Math.min(total, 9)`.

#### §I.3 Paste / clipboard

- [ ] OSC 52 gated on `process.stdout.isTTY` (not fully unconditional).
- [ ] Empty clipboard paste is silently dropped (no user feedback).
- [ ] SVG file paste gets `[SVG: filename]` extmark (not `[Pasted ...]`).
- [ ] `pastedFilepath` platform normalization: strips quotes, handles `file://`, non-win32 unescapes backslashes.

#### §I.4 Mouse

- [ ] `onMouseUp` selection guard: question tabs/options and user-message check `renderer.getSelection()?.getSelectedText()` before acting. Permission options do NOT have this guard (inconsistency).

#### §I.5 IME / submit

- [ ] `submitInner` has a SECOND IME re-sync guard (`prompt/index.tsx:945-951`): re-syncs `store.prompt.input` from `input.plainText` before downstream reads.

#### §I.6 Exit / title

- [ ] Terminal title truncated to 37 chars + `"..."` when >40 chars.
- [ ] Default-title sessions show `"OpenCode"`, not `"OC | <title>"`.

#### §II Presentation

- [ ] Sidebar `sidebar_content` renders 5 builtin plugins: Context, MCP, LSP, Todo, Modified Files.
- [ ] Sidebar footer: "Getting started" callout + cwd path + version (not just version).
- [ ] `routes/session/footer.tsx` is dead code — no footer on the session route.
- [ ] Toasts: single slot (not queue), top-right (not bottom-corner), default 5000ms.
- [ ] Status line: `tab agents` hint and usage chip are mutually exclusive; right side hidden on retry.
- [ ] Task tool: title includes `(background)` suffix for background tasks.
- [ ] ApplyPatch: titles are `# Deleted` / `# Created` / `# Moved <from> → <to>` / `← Patched <path>`.
- [ ] WebFetch: shows only `WebFetch <url>` (no count).
- [ ] Write: line-numbered code block only renders when `diagnostics !== undefined`.
- [ ] Revert: messages with `id >= revert.messageID` are hidden entirely (not just a bar inserted).
- [ ] `StartupLoading.ready` is the local plugin-host signal, not `sync.ready`.
- [ ] Missing `DialogPrompt` (generic text-input dialog) in the dialog list.

#### §III Update flow

- [ ] `experimental.console.get` is NOT in the blocking `Promise.all` gate (fired concurrently, first awaited later).
- [ ] `lsp.updated` re-fetch uses `project.workspace.current()`, not the event's workspace param.

#### §F Focus

- [ ] Dialog has its own focus save/restore (`dialog.tsx:149-150, 84-100`), separate from the prompt effect.
- [ ] `DialogAlert`/`DialogConfirm` have no InputRenderable — focus is null (invariant exception).
- [ ] Global `escape`/`ctrl+c` closes the top dialog (`dialog.tsx:102-134`).
- [ ] `disabled` memo = `permissions.length > 0 || questions.length > 0` (shared flag).

#### §M Draft / compaction

- [ ] Stash cleared BEFORE the restore check (line 612 before 613).
- [ ] Stash does not preserve `mode` (shell mode lost on session switch).
- [ ] `DRAFT_RETENTION_MIN_CHARS = 20` — short drafts cleared with `ctrl+c` are NOT added to history.
- [ ] Compaction part is purely visual (no interactive elements).

#### §PC Permissions

- [ ] Child aggregation includes the parent itself (`x.id === parentID`); flat (one level), not recursive.
- [ ] `external_directory` title: `parent ?? filepath ?? (pattern.includes("*") ? dirname(pattern) : pattern)`.
- [ ] `doom_loop` body: "This keeps the session running despite repeated failures." (leading "This").

#### §C Control channel

- [ ] `tui.prompt.append` inserts at cursor, then `gotoBufferEnd` (not "append").
- [ ] Control events fire synchronously, no queue, no IME guard.
- [ ] `tui.session.select` is synchronous navigation; hydration is async (may navigate home + toast if session missing).

#### §S Security

- [ ] Auth path: `packages/opencode/src/cli/cmd/attach.ts:114` (not `tui/attach.ts:68-69`).
- [ ] Auth is conditional (returns `undefined` when no password).
- [ ] `OPENCODE_SERVER_PASSWORD` / `OPENCODE_SERVER_USERNAME` are the auth env defaults.
- [ ] MCP auth endpoints exist in SDK but TUI does not wire them (only a status hint).

#### §R Robustness

- [ ] No heartbeat timer in the TUI (15s heartbeat is server-side only).
- [ ] Retry status has a gemini quota easter egg: "gemini is way too hot right now" (`prompt/index.tsx:1531-1532`).

#### §E Environment

- [ ] `OPENCODE_FAST_BOOT` / `OPENCODE_ROUTE` are in `app.tsx:272-273` (process.env), not `flag.ts`.
- [ ] Flag count is ~34, not ~25.
- [ ] `OPENCODE_DISABLE_AUTOUPDATE` / `OPENCODE_ALWAYS_NOTIFY_UPDATE` affect §D.3 update flow.
- [ ] Duration format uses `.toFixed(1)` for seconds (e.g. `1.5s`, not `Xs`).

#### §D Data lifecycle

- [ ] `--auto` / `--yolo` / `--dangerously-skip-permissions` startup arg (sets permission mode to `"auto"`).
- [ ] Epilogue is a wordmark + resume command, NOT a transcript.

### V.4 The review cadence

- **Per gap closure:** the implementer runs the 4-phase loop above. No gap is "done"
  until Phase 3 (verify) passes.
- **Per workstream (H1–H19):** after all gap closures in the workstream are done, spin a
  review subagent to cross-check the diff against the opencode source (same protocol as
  the review that produced this section). Fix all blockers/should-fixes before opening a PR.
- **Per PR:** the CLAUDE.md git workflow applies — local pre-push gate, independent
  review subagent, CI green, merge.


## Testing posture

> The per-step verification protocol is in §VM. The tests below are the **automated**
> assertions that back the manual side-by-side verification.

- **Keybind unit tests:** table-driven `*_test.go` for each new chord (leader digits,
  `ctrl+r` rename, `ctrl+d` delete, `ctrl+z` suspend, etc.) — assert the right `tea.Cmd`
  / modal / store mutation fires.
- **Composer tests:** `ctrl+v` clipboard read; smart-paste marker insertion + submit
  payload (full text sent); usage chip render.
- **Mouse tests:** `tea.MouseMotionMsg`/`tea.MouseClickMsg` → modal/autocomplete
  selection.
- **Sidebar/footer tests:** LSP/MCP sections render from the store; counts match.
- **Terminal title test:** assert the OSC 0 sequence is emitted on route change.
- **Security tests (§S):** OSC 52 gating — off over `SSH_CONNECTION`, on locally;
  `--no-osc52` suppresses; KV toggle flips. Permission `auto` mode auto-replies `once`.
- **Performance regression tests (§P):** a scroll-injection profile (plan 19's method)
  asserts CPU stays <10% on an idle session; `childStatusMap` is read, not re-decoded
  (a canary test that fails if `parseToolState` is called per-frame).
- **Harness tests (§V):** `tools/tui-shots/` per-scene pixel-diff vs opencode refs; a
  regression (diff % rising) fails.
- **Config tests (G.15):** `opencode.json` TUI section parses + merges over CLI/KV;
  unknown keys are rejected (mirrors `TuiKeybind.unknownKeys`).
- **Startup-args tests (G.16):** `--continue` navigates to the most-recent session;
  `--fork` forks + navigates; `--prompt` pre-fills + auto-submits.
- **Conformance (plan 12):** no new endpoints; the `self` gate stays green. The
  `/export` / `/copy` slash verbs are client-only (transcript formatting).
- **Known-divergence registry (plan 12):** update for — selection-copy, OSC 52
  unconditional (pre-fix), attention/sound, auto-update, epilogue, `OPENCODE_SHOW_TTFD`.

## Decisions baked in (flag if reality contradicts)

1. **This is a map + gap-closure plan, not a rebuild.** The Opcode42 TUI is substantially
   built (08/08a–e); this plan documents the full opencode input/presentation surface
   and fills the remaining gaps.
2. **The keybind table is the source of truth** for "what opencode does." Any future
   parity work should cross-reference §I.1 before claiming "done."
3. **Selection-copy is a known divergence.** Bubble Tea has no selection model; `ctrl+x
   y` is the Opcode42 substitute. Record in plan 12.
4. **Suspend (`ctrl+z`) is feasible** in Bubble Tea v2 (`tea.Suspend`); rebind
   `input_undo` to `ctrl+-` per opencode's `config/index.tsx:90-98`.
5. **Daemon-gated items stay parked.** Queued prompts, workspaces, provider OAuth, tags,
   console-org, reference aliases — all depend on daemon surface not yet built; they are
   not in this plan's scope.
6. **OSC 52 must be gated** (§S.2, G.13). Unconditional clipboard-to-escape-stream
   writes are a known exfiltration vector over SSH/tmux; default off over SSH.
7. **Performance is owned by plans 19 + 20.** §P maps the architecture but does not
   re-open the pre-rendered-buffer work; cross-reference those plans for the pipeline.
8. **Attention/sound is intentionally a divergence.** The terminal bell is the TUI's
   notification; sound packs + desktop notifications are out of scope for a dogfood TUI.
9. **Auto-update is N/A.** Opcode42 is a single binary; the update flow is a known
   divergence (plan 12).
10. **The full TUI (not the run mini-TUI) is the parity target.** Opcode42 follows the
    `tui/` convention throughout (plan 17 §"Note on opencode's architecture").
11. **The `/tui/*` POST endpoints are not applicable.** Opcode42 is a client-controlled
    UI (plan 08); the TUI runs no HTTP server. The SSE control events (§C.1) are a
    *deferred optional* (G.18) — only if a remote-control use case emerges.
12. **Per-session draft retention is optional.** Opcode42's global draft is a defensible
    divergence (no stash/restore race); record in plan 12 either way.
13. **Screen-reader is a terminal-TUI platform limitation**, not an Opcode42 gap.
14. **Child-session permission/question aggregation (G.21) is a correctness gap, not
    cosmetic** — a sub-agent's permission prompt is currently invisible in the parent
    view. High priority despite the small-medium size.
15. **Every gap closure is verified against opencode source before it is "done."** The
    4-phase protocol (read → write → verify → quirk-check) in §VM is mandatory. The
    quirk checklist (§V.3) is the regression surface — each quirk is a test case.