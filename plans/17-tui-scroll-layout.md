# Plan 17 — TUI scroll, layout & parity fixes (validated)

> **Scope.** Fix the three most-noticeable TUI bugs reported after plan 08e, plus
> six parity items grounded against opencode's source:
>
> 1. The composer + status bar (bottom chrome) must **stay pinned** at the bottom
>    across all scroll positions — never scroll with the stream.
> 2. The right sidebar must **stay pinned** and be wider (42 cols, matching
>    opencode).
> 3. The chat window must be an **independent scroll viewport** over the full
>    session history.
> 4. Permission/question prompts: footer panel (not centered modal), matching
>    opencode's footer dispatcher.
> 5. In-chat diff: inline unified diff at tool completion.
> 6. Thinking/reasoning: collapse/expand with duration, matching opencode's full
>    TUI (not the run mini-TUI).
> 7. Subagent count: active vs recent, matching opencode's two-count model.
> 8. Input box: defined surface + status bar with mode/model/variant.
> 9. Paste: wire `tea.PasteMsg` (currently broken); add ctrl+enter/alt+enter
>    newline.
>
> **Root cause (bug #1).** Today `frame()` (`internal/tui/render.go:247`) joins
> the body + footer into ONE string and `scrollregion.Window` slices that joined
> string. The footer is a suffix of the body, so the scroll math treats it as
> part of the scrollable content. The composer/status bar ride the scroll.
>
> **Note on opencode's architecture.** opencode has TWO TUIs — a "run" mini-TUI
> (`packages/opencode/src/cli/cmd/run/`, no sidebar) and a full TUI
> (`packages/tui/src/`, with sidebar). opencode never has the footer-rides-scroll
> bug because it uses a **layout tree** (flexbox), not string composition — the
> scrollbox and footer are siblings, pinned by flexbox (`routes/session/index.tsx:1165-1344`).
> Opcode42 uses Go + Bubble Tea + lipgloss (string composition), so the 3-layer
> absolute-position canvas split is the **Go idiom** for faking a layout tree.
> The bug existed because `frame()` joined what should have been siblings.

## Links

- **Parent:** `plans/08e-tui-finish-line.md` (the finish-line plan; §A3 re-seated
  scrollregion but didn't split the layers — this plan completes that).
- **scrollregion:** `scrollregion/scrollregion.go` (the tail-anchored viewport
  math, reused as-is).
- **canvas:** `internal/tui/canvas.go` (the v2 compositor; `sessionLayers` is
  where the split happens).
- **chrome:** `internal/tui/chrome.go` (sidebar width + render).
- **render:** `internal/tui/render.go` (`frame` — the buggy join, to be split).
- **opencode reference:** `/Users/rotemmiz/git/opencode/packages/opencode/src/cli/cmd/run/`
  (run mini-TUI) and `/Users/rotemmiz/git/opencode/packages/tui/src/` (full TUI).

## Source grounding

Every claim below is validated against opencode source with `file:line`
citations. Citations prefixed `run/` are in the run mini-TUI; `tui/` are in the
full TUI. Key reference files:

- `run/footer.ts` (1129 lines) — footer layout, coalescing queue, height math
- `run/footer.view.tsx` (942 lines) — footer panel dispatcher, status bar
- `run/footer.prompt.tsx` (1306 lines) — composer, permission/question panels
- `run/scrollback.writer.tsx` — stream rendering, tool snapshots
- `run/session-data.ts` — the reducer (thinking gate, permissions, questions)
- `run/tool.ts` (1486 lines) — tool snapshot functions (snapEdit, snapQuestion)
- `tui/routes/session/index.tsx` (1632 lines) — full TUI session layout
- `tui/routes/session/sidebar.tsx` — sidebar (42 cols, plugin slots)
- `tui/config/keybind.ts` — full keybind definitions
- `tui/context/thinking.ts` — thinking mode (show/hide toggle)
- `tui/feature-plugins/system/diff-viewer.tsx` — full-screen diff viewer

---

## Workstream A — Scroll & layout split (foundation, ship first)

**Validated against:** `tui/routes/session/index.tsx:1165-1344` (flexbox layout),
`tui/routes/session/sidebar.tsx:31` (`width={42}`), `tui/config/keybind.ts:134-141`
(scroll keybinds), `scrollregion/keys.go:37-40` (Home/End infra).

### A1. Split frame() into frameStream + frameFooter

The root-cause fix. `frame()` at `render.go:247-257` returns
`strings.Join(lines, "\n") + "\n" + footer` — the join is the bug. Split into:
- `frameStream(body string, bodyH int) string` — returns `scrollregion.Window(body, offset, bodyH)`
- `frameFooter(footer string) string` — returns the footer as-is (no join)

`sessionLayers()` (`canvas.go:266-311`) composes 3 layers:
- Stream layer: `NewLayer(frameStream(body, bodyH)).X(0).Y(0).Z(zPane)` — scrollable.
- Footer layer: `NewLayer(frameFooter(footer)).X(0).Y(bodyH).Z(zPane)` — pinned.
- Sidebar layer: already separate at `canvas.go:305-308` — pinned, no change.

**Clean-code requirement:** extract a shared `buildFooter(leftW) string` helper
used by BOTH `sessionLayers()` (canvas path) and `renderSession()` (the 0×0
fallback at `render.go:15`). Currently both paths duplicate the footer-stacking
logic (dock + subagent + pty + composer + statusbar). The split must not create a
third copy.

### A2. Sidebar width 42 (not 36), threshold ≥121

**Correction from original plan:** the original plan proposed 36 cols. opencode
uses **42 cols** — `tui/routes/session/sidebar.tsx:31` (`width={42}`) and
`tui/routes/session/index.tsx:271` (`contentWidth = width - (sidebarVisible ? 42 : 0) - 4`).
42 is the source-grounded number; 36 was fabricated relative to Opcode42's own
28-col constant, violating the "no fabricated numbers" rule.

**Changes:**
- `chrome.go:18`: `sidebarWidth = 28` → `sidebarWidth = 42`.
- `chrome.go:41`: threshold `m.width >= 80` → `m.width >= 121` (matches opencode's
  `width > 120` at `tui/routes/session/index.tsx:263`).
- Preserve the existing `ctrl+x b` toggle (`model.go:1399-1401`) — opencode has
  the same (`keybind.ts:81`, `session.sidebar.toggle`). Document the "auto vs
  hide" semantics: `sidebarHidden=true` forces off; `sidebarHidden=false` follows
  the width threshold. Opcode42's boolean conflates these correctly (the AND
  at `chrome.go:41` handles it).

**Sidebar scroll note:** opencode's sidebar is a `<scrollbox>`
(`tui/routes/session/sidebar.tsx:38-47`) — independently scrollable when content
overflows. Opcode42's `sidebarView()` pads to `m.height` (`chrome.go:277-281`).
The 3-layer split must not bake in "sidebar never scrolls" — note for a future
sidebar-scroll workstream.

### A3. Home/End + half-page scroll keybinds

opencode's full scroll keymap (`tui/config/keybind.ts:134-141`):
- `messages_first`: `ctrl+g, home` → jump to first message
- `messages_last`: `ctrl+alt+g, end` → jump to last message
- `messages_half_page_up`: `ctrl+alt+u` → half-page up
- `messages_half_page_down`: `ctrl+alt+d` → half-page down
- `messages_page_up`: `pageup, ctrl+alt+b`
- `messages_page_down`: `pagedown, ctrl+alt+f`
- `messages_line_up`: `ctrl+alt+y`
- `messages_line_down`: `ctrl+alt+e`

**Infrastructure already exists:** `scrollregion/keys.go:37-40` decodes `"home"` →
`Top` and `"end"` → `Bottom`; `keys.go:51-65` `Apply` handles them. But
`internal/tui/model.go:528-535` only binds `ctrl+up/down/pgup/pgdn` — **Home/End
are NOT wired**.

**Changes:**
- Refactor `model.go:528-535` scroll key handling to use
  `scrollregion.Decode(msg.String())` + `m.scroll.Apply(...)` — picks up Home/End
  for free and is cleaner than the current hand-rolled switch.
- Add `ctrl+alt+u` / `ctrl+alt+d` → half-page scroll (±`bodyH/4`), matching
  opencode's `messages_half_page_up/down` (`tui/config/keybind.ts:138-139`).
- Add `ctrl+g` as alias for Home (opencode's `messages_first: "ctrl+g,home"`).
- Keep `ctrl+up/down` for line scroll (Opcode42 convention; opencode uses
  `ctrl+alt+y/e` — document this divergence in the known-divergence registry).

### A4. Don't skip the body for footer panels

`canvas.go:111-119` skips body layers when `modalClassActive()` is true
(`canvas.go:258-261`). This is correct for centered modals (diff reviewer) but
**wrong for footer panels** (permission/question) — opencode keeps the stream
visible above the footer panel. Split `modalClassActive` into:
- `modalClassActive` — true centered modals (diff, sessions, help, palette) → body hidden.
- `footerPanelActive` — permission/question footer panels → body stays visible.

This is a prerequisite for Workstream B.

### Acceptance

- **Bug #1 fixed:** scrolling the stream never moves the composer/status bar.
  New `TestCanvas_FooterLayerPinned` — set scrollOffset to 0, 3, 1000; assert the
  footer's Y range contains "ctrl+p commands" at the SAME y across all offsets.
- **Bug #2 fixed:** sidebar is 42 cols (`TestSidebarView_WidthMatchesConstant` —
  update the constant). Stays at the SAME x across scroll offsets
  (`TestCanvas_SidebarLayerPinned`).
- **Bug #3 fixed:** Home jumps to top, End to tail. Half-page scroll works.
  `TestKeyScroll_HomeEndJumpsToTopAndTail`, `TestKeyScroll_HalfPageScroll`.
- **A4:** body stays visible when a footer panel is active
  (`TestFooterPanel_StreamStaysVisible`).
- Existing `TestView_NeverExceedsViewport`, `TestView_FooterPinnedAcrossScroll`,
  `TestView_ScrollChangesStreamContent`, `TestKeyScroll_PageAndCtrlArrowsScroll`
  stay green.

### Performance notes

- The 3-layer split is a **correctness** fix, not a perf fix. The real perf cost
  is `composeCanvas()` double-fill (`canvas.go:99-103` + `canvas.go:136-151` —
  2× full-canvas cell writes per frame). Not in scope for A; flag for a future
  perf workstream.
- `sessionStreamBlocks` rebuilds the entire stream string every frame
  (`render.go:60-85`). opencode's retained surface only commits new rows
  (`run/scrollback.surface.ts:225-308`). Not in scope for A; the markdown cache
  (`markdown.go`) partially mitigates this.

### Adjacent features (noted, not implemented in A)

- **Scrollbar toggle** (`tui/routes/session/index.tsx:1170-1180`,
  `keybind.ts:82`) — opencode has a toggleable vertical scrollbar. Opcode42 has
  none. Follow-up workstream H.
- **Sticky scroll** (`tui/routes/session/index.tsx:1181-1182`,
  `stickyScroll={true} stickyStart="bottom"`) — opencode auto-sticks to tail
  when at tail. Opcode42 does this manually on submit. Note the gap.
- **Message-jump keybinds** (`tui/config/keybind.ts:142-144`) — jump to next/prev
  message boundary. Follow-up.
- **Sidebar plugin slots** (`tui/feature-plugins/builtins.ts:21-36`) — 6 sidebar
  plugins (context, LSP, todo, files, MCP, footer). Opcode42's sidebar is
  monolithic. Future plugin work.

---

## Workstream B — Unified footer prompt panel (permission + question)

**Validated against:** `run/footer.view.tsx:667-795` (footer dispatcher),
`run/footer.ts:103-104` (PERMISSION_ROWS/QUESTION_ROWS), `run/footer.ts:697-722`
(applyHeight), `run/permission.shared.ts:22-23` (3-stage state machine),
`run/footer.permission.tsx:214-257` (keyboard shortcuts),
`run/footer.question.tsx:167-248` (question shortcuts),
`run/session-data.ts:219-229` (pickBlockerView: permission > question priority),
`run/tool.ts:607-622` (snapQuestion — answered Q&A in scrollback).

### B1. Replace centered modal with footer panel

opencode renders permission AND question as a **footer-region panel** (bottom of
screen), swapped by a dispatcher based on `active().type`
(`run/footer.view.tsx:778-794`). NOT a centered modal.

**Panel height formula (corrected):** the original plan said "12 for permission,
14 for question" — this is imprecise. opencode's formula is
`base + PERMISSION_ROWS` / `base + QUESTION_ROWS` (`run/footer.ts:700-702`),
where `base = max(1, renderer.footerHeight - TEXTAREA_MIN_ROWS)` (`footer.ts:291`)
and `TEXTAREA_MIN_ROWS = 1` (`footer.prompt.tsx:35`). With the initial footer
height of 4 (`runtime.lifecycle.ts:35`), `base = 3`. So:
- Permission: `base(3) + 12 = 15` rows total.
- Question: `base(3) + 14 = 17` rows total.

For Opcode42: `panelH = base + PERMISSION_ROWS` where `base =
lipgloss.Height(m.statusBarView(leftW))` (Opcode42's non-textarea chrome height).

**Background:** the `surface` bg goes on the **panel body**, not the outer footer
container (`run/footer.permission.tsx:260`, `run/footer.question.tsx:280`). The
outer footer container is `transparent` when a panel is active
(`run/footer.view.tsx:663`), and the left accent border is **removed**
(`run/footer.view.tsx:645`). A 1-row transparent spacer is rendered above the
panel (`run/footer.view.tsx:632-634`).

**Changes:**
1. `permissionView()` / `questionView()` — remove `lipgloss.Place(Center,
   Center, card)`. Return the panel body string (sized `leftW × panelH`),
   styled with `BgElev` background (Opcode42's equivalent of `surface`).
2. `canvas.go overlayLayers()` — replace `NewLayer(p).X(0).Y(0).Z(zModal)` with
   `NewLayer(panel).X(0).Y(m.height - panelH).Z(zModal)`.
3. `canvas.go modalClassActive()` — split into `modalClassActive` (centered
   modals → body hidden) vs `footerPanelActive` (permission/question → body
   stays). See A4.
4. Queueing: permission has **strict priority over question**
   (`run/session-data.ts:219-229` `pickBlockerView`). Both are FIFO within type.
   Opcode42 already handles this (`canvas.go:223-231`). Add a test
   `TestPermissionPriorityOverQuestion`.

### B2. Keyboard shortcuts: match opencode

**Correction:** the original plan didn't specify shortcuts. Opcode42 currently
uses `a`/`s`/`r` for permission and `space` for question multi-select — these
are **diverggences from opencode**. opencode uses:

**Permission** (`run/footer.permission.tsx:214-257`):
- `tab` / `shift+tab` / `left`/`h` / `right`/`l` — shift selection
- `return`/`enter` — confirm selected
- `esc` — escape (→ reject stage, or cancel from always stage)
- **No `y`/`n` shortcuts.** No `1`/`2`/`3` digit shortcuts.** Selection is tab/arrows + enter.

**Question** (`run/footer.question.tsx:167-248`):
- `up`/`k`, `down`/`j` — move selection
- `1`-`9` **digit shortcuts** — directly choose option N (`footer.question.tsx:217-224`)
- `return`/`enter` — select/confirm/submit (verb changes per `verb()` at `:63-77`)
- `esc` — reject/dismiss
- `tab`/`shift+tab` — tab navigation (multi-question only)
- **No `space` for multi-select toggle** — opencode uses `enter` to toggle
  (`footer.question.tsx:238-242`)

**Change:** match opencode shortcuts. Replace Opcode42's `a`/`s`/`r` with
tab/arrows + enter; replace `space` toggle with `enter` toggle. Log the
divergence change in the known-divergence registry.

### B3. 3-stage permission flow

opencode has a **3-stage state machine** (`run/permission.shared.ts:22-23`):
`PermissionStage = "permission" | "always" | "reject"`.

1. **Stage `permission`** (`:80-83`): options = `["once", "always", "reject"]`.
2. **Stage `always`** (`:179-188`): confirmation step showing the patterns that
   will be allowed (`permissionAlwaysLines`, rendered at
   `footer.permission.tsx:401-422`). "Always" is until restart
   (`:127` `"until OpenCode is restarted"`).
3. **Stage `reject`** (`:190-198`): a `RejectField` textarea for the rejection
   message ("Tell OpenCode what to do differently") (`footer.permission.tsx:69-130`).

Opcode42's `permission.go:21-28` has the 3 choices but NO confirmation stage for
"always" and NO rejection-message textarea. The wire side is identical (reply
`once`/`always`/`reject` + optional `message`); the UI flow is the gap.

**Change:** add the 3-stage flow. The "always" confirmation shows the patterns;
the "reject" stage shows a textarea for feedback. This is a UX feature the plan
must implement to match opencode.

### B4. Permission panel shows inline diff (reuse Workstream C)

For `edit`/`apply_patch` permissions, opencode shows the **unified diff inline in
the permission panel** (`run/footer.permission.tsx:373-391`, `<diff view="unified">`).
The diff comes from `PermissionInfo.diff` (`permission.shared.ts:33-39`).

**Change:** reuse Workstream C's `renderUnifiedDiff` helper to render the diff in
the permission panel when `info.diff` is present.

### B5. Question multi-question tabs + confirm review + custom text

**Multi-question tabs** (`run/footer.question.tsx:282-315`): when a question
request has multiple questions, opencode shows tab headers + a final `Confirm`
tab with a review summary (`:319-354`). Opcode42's `question.go:308-310` has a
simple "question N of M" label but no tab navigation.

**Custom text** (`run/footer.question.tsx:425-506`, `question.shared.ts:70-72`):
opencode supports "Type your own answer" — a custom-text field. Opcode42's
`question.go:328-330` says "free-text answers aren't supported — press r to
reject" — this is a gap.

**Changes:**
- Add multi-question tab navigation (tab/shift+tab) + confirm review.
- Add custom-text answer support.

### B6. Answered-question in-stream card (keep, with note)

opencode renders answered Q&A from the **tool's structured snapshot**
(`run/tool.ts:607-622` `snapQuestion` → `run/scrollback.writer.tsx:264-287`).
Opcode42 uses a separate `answeredQuestions` store (`store.go:165`,
`question.go:237-275`). This is a **structural divergence** — acceptable for B,
but when Workstream C's tool-snapshot mechanism lands, migrate the
answered-question rendering to the tool snapshot and retire the
`answeredQuestions` store. **Drop the pending-question in-stream card**
(`render.go:71-75`) — opencode has no pending card (`run/tool.ts:827-829`
`scrollQuestionStart` returns `""`).

### B7. Permission leaves no in-stream trace

Confirmed: no `snapPermission` exists, no permission tool, no scrollback commit.
The permission decision is purely transient. Opcode42 currently has no in-stream
permission card — B7 is a no-op (verify).

### Acceptance

- Permission/question render as footer panels (bottom, not centered) with
  `BgElev` background.
- Panel disappears immediately on answer — composer returns same frame.
- 3-stage permission flow (permission → always confirm → reject with message).
- Question has multi-question tabs, confirm review, and custom-text option.
- Keyboard shortcuts match opencode (tab/arrows/enter + digits for question).
- Permission panel shows inline diff for edit/apply_patch (reuse C's helper).
- Permission has priority over question when both are pending.
- Stream stays visible above the footer panel (body not hidden).
- New tests: `TestPermissionView_FooterPanelNotCentered`,
  `TestQuestionView_FooterPanelNotCentered`,
  `TestPermissionReplied_PanelDisappearsImmediately`,
  `TestPermissionPriorityOverQuestion`,
  `TestPermission_ThreeStageFlow`,
  `TestPermissionPanel_HeightIncludesBase`,
  `TestFooterPanel_StreamStaysVisible`.

### Adjacent features (noted)

- The footer-panel pattern is shared by 7 other panels (command palette, model/
  variant/skill/subagent/queued pickers — `run/footer.view.tsx:667-795`). B
  implements it for permission/question; the same treatment can extend later.
- Subagent permissions/questions bubble up to the parent footer
  (`run/stream.transport.ts:306-307`). Opcode42's `pendingPermission`/
  `pendingQuestion` only check `m.store.permissions`/`m.store.questions` — verify
  subagent blockers reach those slices (plan 08e §E territory).

### Performance & clean code

- opencode separates state (`permission.shared.ts`) from render
  (`footer.permission.tsx`) — pure state machine + render. Opcode42's
  `permission.go` mixes state (`permSel`, `permReplying`) with render.
  **Recommendation:** extract a pure `permissionState` struct + transition
  functions to make the 3-stage flow testable. Same for `question.shared.ts`
  vs `question.go`.

---

## Workstream C — In-chat diff viewer (inline unified diff)

**Validated against:** `run/scrollback.writer.tsx:188-225` (diff snapshot
renderer), `run/tool.ts:515-532` (snapEdit), `run/tool.ts:534-569` (snapPatch),
`run/tool.ts:484-498` (patchTitle), `run/tool.ts:1290-1298` (toolStructuredFinal
gate), `run/scrollback.writer.tsx:52-79` (entryLayout: inline→block flip),
`tui/feature-plugins/system/diff-viewer.tsx:1045-1072` (full-screen viewer).

### C1. Inline unified diff at tool completion

opencode renders the diff INLINE in the chat stream when an edit/apply_patch
tool completes, via a structured snapshot at `phase === "final"`. The diff body
comes from `metadata.diff` (`run/tool.ts:517`).

**Correction from original plan:** the plan claimed "opencode has no separate
full-screen diff reviewer." This is **wrong** — opencode's full TUI has a
full-screen `diff-viewer` plugin (`tui/feature-plugins/system/diff-viewer.tsx`,
1077 lines). The run mini-TUI has only the inline diff. Opcode42's `diff.go`
(ctrl+x d) is the counterpart to the full-TUI plugin; we keep both.

**Per-item snapshot title:** opencode renders `# Edited <file>` (edit) /
`# Created|Deleted|Moved|Patched <file>` (apply_patch) as a muted line ABOVE the
diff (`run/scrollback.writer.tsx:192-194`, `run/tool.ts:484-498` `patchTitle`).
For `apply_patch` with multiple files, one title+diff block per file
(`scrollback.writer.tsx:190` `.map` over items).

**Per-file +N -N stats:** opencode shows `+additions -deletions` per file
(`tui/feature-plugins/system/diff-viewer.tsx:829-834`) and `-{deletions} lines`
when no patch text (`scrollback.writer.tsx:217-221`).

**Snapshot replaces inline progress:** opencode's `entryLayout` flips from
`"inline"` (progress) to `"block"` (final snapshot) at completion
(`run/scrollback.writer.tsx:52-79`). The Go store/render should **replace** the
in-flight inline text with the diff block when `status` flips to `completed`,
not append.

### C2. Shared helper (with filename for syntax)

Extract the diff rendering from `internal/tui/diff.go` into a shared helper:

```go
func renderUnifiedDiff(patch string, filename string, p theme.Palette, width int) string
```

**Correction:** the original plan's signature omitted `filename`. opencode
passes `filetype={toolFiletype(item.file)}` (`scrollback.writer.tsx:200`) for
syntax highlighting. The helper must take `filename` to match the file's
language. The existing `diff.go` functions (`classifyDiffLine`, `advanceDiffLineNumbers`,
`renderGutter`, `renderDiffCodeLine`, `padRow`) are already near-pure (only
depend on `m.styles.P` palette) — extraction is clean.

**Bg-tint divergence:** opencode's run mini-TUI uses **transparent** diff
backgrounds (`run/theme.ts:572-574`); the full-TUI viewer uses real tints.
Opcode42's palette has tints (`theme.DiffPalette`). **Decision: use the tints**
for the inline diff (better readability, colorblind-safe with `+`/`-` sign
coloring). Log in the known-divergence registry.

### C3. Render cache (performance)

Diffs arrive complete at `phase === "final"` (they don't stream). Add a
`map[partID]cachedDiff` (or `(partID, patchHash, width)` key) on `Model` so
`toolRow` doesn't re-render the diff every frame. This is the Go equivalent of
SolidJS's `createMemo` and is critical since `toolRow` runs every animation
tick. Syntax highlighting is the expensive part — cache the fully-rendered
styled string.

### C4. Bypass the 20-line cap

`renderOutputPanel` (`toolrender.go:380-420`) caps generic tool output at
`maxPanelLines = 20`. Workstream C must NOT route the diff through
`renderOutputPanel` — call `renderUnifiedDiff` directly so full hunks render
(matching opencode's no-truncation behavior).

### C5. Preserve the per-tool fold (extension)

opencode's run mini-TUI has no per-tool fold. Opcode42's `toolrender.go:297-305`
has `ctrl+x v` (fold toggle). **Keep it** as an intentional extension — when
collapsed, show only the header (`Edit <file> ▸`); when expanded, show header +
diff. Document as a divergence from opencode's run mini-TUI.

### Acceptance

- An `edit` tool completion renders the unified diff inline in the chat stream,
  below the `# Edited <file>` title, with `+`/`-`/context coloring.
- `apply_patch` renders one title+diff block per file (multi-file support).
- Per-file `+N -N` stats appear in the title row.
- The diff is cached (rendered once at completion, not every frame).
- Full-screen `diff.go` reviewer (ctrl+x d) still works.
- `ctrl+x v` fold toggle still works.
- New tests: `TestToolRow_EditRendersInlineDiff`,
  `TestToolRow_ApplyPatchRendersMultiFileDiff`,
  `TestInlineDiff_CachedAcrossFrames` (assert the diff string is not rebuilt
  when the frame re-renders without the part changing).
- Existing `diff_test.go` stays green.

### Adjacent features (other tool renders)

opencode's `toolEntryBody` (`run/tool.ts:1420-1472`) dispatches to snapshots for
each tool kind. The plan should note these for future parity:

| Tool | Snapshot | Renderer |
|---|---|---|
| `edit` | `diff` | `<diff view="unified">` (C implements this) |
| `apply_patch` | `diff` (multi-file) | same `<diff>` per file (C implements this) |
| `write` | `code` | `<line_number>` + `<code>` block |
| `task` | `task` | `# Task` heading + rows + tail |
| `todowrite` | `todo` | `# Todos` + `[✓]`/`[•]`/`[ ]` items |
| `question` | `question` | `# Questions` + Q/A pairs (B6 addresses this) |
| `bash` | (no snapshot) | inline `$ <cmd>` + streamed progress + `exit N · <time>` |
| `read`/`grep`/`glob` | (no snapshot) | header-only (icon + title) |

**Duration display:** opencode shows duration for `bash` and `patch` finals
(`run/tool.ts:673-685`) but NOT for `edit`/`read`/`write`. Opcode42 matches this.

**Error states:** `toolEntryBody` returns `textBody(toolScroll("final", ctx))`
for `status === "error"` (`tool.ts:1453-1456`). Opcode42's error sub-line
(`toolrender.go:347-351`) is a reasonable equivalent.

### Performance & clean code

- **No virtualization in the inline diff** — opencode renders the full `<diff>`
  for every item (`scrollback.writer.tsx:188-225`). The terminal scrollback is
  the viewport. C matches this (no truncation, full hunks).
- **Memoization:** SolidJS `createMemo` wraps the snapshot selectors. The Go
  equivalent is the render cache (C3) keyed by `(partID, patchHash, width)`.
- **Use `strings.Builder`** for the line join (the shared helper returns a
  string via `strings.Builder` to avoid the final allocation of `strings.Join`).
- **Syntax highlighting is the expensive part** — cache per `(code-line,
  filename, palette-hash)`. For the inline diff, the patch text is immutable
  once the tool completes, so the entire rendered diff string can be cached.

---

## Workstream D — Thinking/reasoning: collapse/expand + duration

**Validated against:** `run/session-data.ts:902-915` (thinking gate),
`run/footer.ts:535-564` (coalescing queue), `run/scrollback.surface.ts:225-308`
(retained surface settle), `run/entry.body.ts:61-75` (reasoning render),
`tui/context/thinking.ts:4,36` (ThinkingMode = show|hide, default "hide"),
`tui/routes/session/index.tsx:1572-1632` (ReasoningPart: header + collapsible body),
`tui/routes/session/index.tsx:1650-1654` (spinner while streaming),
`tui/routes/session/index.tsx:1667-1671` (duration display).

### D1. Thinking mode: collapse/expand (NOT hard drop)

**Major correction from original plan:** the plan proposed "DROP reasoning
parts at the reducer when hidden." This matches opencode's **run mini-TUI**
(`run/session-data.ts:906-908` — when `!input.thinking`, the commit is dropped).
But Opcode42 is a **full TUI**, and opencode's full TUI has a different
behavior:

- `tui/context/thinking.ts:4`: `type ThinkingMode = "show" | "hide"` — a 2-state
  show/hide, NOT a drop.
- `tui/context/thinking.ts:36`: default is **"hide"** (collapsed), not "show".
- `tui/routes/session/index.tsx:254`: `showThinking = createMemo(() => true)` —
  reasoning parts are **always rendered** in the full TUI; "hide" means
  collapsed to a 1-line header, not dropped.
- `tui/routes/session/index.tsx:1572-1632` (ReasoningPart): "hide" mode renders
  a single `ReasoningHeader` line (`+ Thought: <title> · <duration>`); "show"
  mode renders header + full markdown body. An `expanded` signal
  (`index.tsx:1577`) allows click/toggle to expand the body when in hide mode.

**Changes:**
1. **Default `hideThinking = true` (collapsed)**, matching opencode's full-TUI
   default ("hide"). The original plan's "default: show" was wrong — it picked
   the run mini-TUI's default, not the full TUI's.
2. **`ctrl+x r` toggles collapse/expand** (the `ThinkingMode` toggle), NOT a
   hard drop. When `hideThinking=true`, render a 1-line header
   (`▸ Thought: <title> · <duration>`), not nothing. This preserves the
   reasoning *presence* (user knows reasoning happened) while hiding the body.
3. **Keep `ctrl+x f`** (expand the body within hide mode) — this corresponds to
   opencode's `expanded` signal (`index.tsx:1577`). The original plan said "drop
   `ctrl+x f`" — **do not drop it.** It's a real opencode behavior.
   - `ctrl+x r` = toggle hide/show (the `ThinkingMode` toggle)
   - `ctrl+x f` = expand the body when in hide mode (the `expanded` signal)
4. **No "off" (drop) mode** — opencode's full TUI has no "off" mode
   (`showThinking` is always true). If Opcode42 wants a true "off", that's a
   divergence from opencode and must be logged. **Recommendation: match opencode
   (no "off", only collapse).**

### D2. Deferred token write: event-driven, NOT 100ms tick

**Correction from original plan:** the plan claimed "the animTickMsg cadence
(100ms) is the flush interval — tokens accumulate in the store and render in
one batch on the next tick." This is **misleading**.

- In Opcode42, token arrival is NOT gated by animTick. `sseEventMsg`
  (`model.go:962-1018`) calls `m.store.Reduce(ev)` immediately, and Bubble Tea
  schedules a `View()` re-render right after. So streaming updates appear at
  SSE arrival rate (20-50ms typically), **independent of the 100ms animTick**.
- The 100ms animTick (`spinner.go:49, 60-62`) drives only **animation** (spinner,
  logo, toasts), not token flush.
- opencode's coalescing drain is `queueMicrotask` (`run/footer.ts:560`) — sub-
  millisecond. The effective streaming cadence is "as fast as the event loop +
  surface settle allow."
- **The deferral is the event-loop batching between renders** — SSE deltas
  accumulate in `store.Part.Text` between `View()` calls, and each `View()`
  re-renders the full current text. No explicit coalescing queue is needed
  because Bubble Tea's event-driven re-render already batches whatever arrived.

**Fix the plan's wording:** replace "animTickMsg cadence is the coalescing
interval" with "SSE deltas accumulate in `store.Part.Text` between `View()`
calls; Bubble Tea re-renders on each `sseEventMsg` (event-driven), so the
deferral is the event-loop batching between renders, NOT the 100ms animTick."

### D3. Incremental markdown cache (performance)

**New work the original plan omitted.** The current markdown cache
(`markdown.go:392-449`) keys on `(SHA-256(text), width, themeName)`. A growing
`Part.Text` produces a cache miss **every delta** → a full glamour re-parse of
the entire part every frame. For a 5k-token reasoning part streaming over 500
deltas, that's O(n²) over the whole stream.

opencode avoids this via incremental block rendering:
`run/scrollback.surface.ts:287-305` (`commitMarkdownBlocks` +
`_stableBlockCount`) — only new stable blocks are committed; the trailing
streaming block re-settles each frame.

**Change:** extend `mdCache` to key by `(partID, stableBlockCount, width, theme)`
and render only new stable blocks, mirroring opencode's approach. Without this,
long reasoning parts will stutter. This is **new work** the original plan
omitted by claiming "no new mechanism needed."

### D4. Reasoning render: header + collapsible body + spinner + duration

Match opencode's full TUI (`tui/routes/session/index.tsx:1572-1632`):

- **While streaming** (`!isDone`): render a spinner with "Thinking: <title>"
  (`index.tsx:1650-1654`). Reuse the existing `animTick` + `scannerFrame` infra
  (`spinner.go`).
- **When done** (`isDone`): render a static `+ Thought: <title> · <duration>`
  header (`index.tsx:1655-1674`). Duration is computed from `part.time.end -
  part.time.start` (`index.tsx:1587-1590`).
- **Body:** collapsible markdown, shown when "show" mode or `expanded` signal.
  Render as muted markdown (DIM attribute + `theme.reasoning.body` color,
  matching `run/scrollback.shared.ts:53-58`).
- **Prefix:** the `_Thinking:_` italic prefix is a run mini-TUI convention
  (`run/entry.body.ts:61-75`). The full TUI uses the header spinner + duration
  instead. Use the full-TUI style.

**Add `Time { Start, End int64 }` to `Part`** (`store.go:94-112` currently has no
time field). Populate from `message.part.updated` events to enable the duration
display. This is a real adjacent feature the original plan missed.

### D5. [REDACTED] stripping

opencode strips `[REDACTED]` (OpenRouter encrypted-reasoning placeholder) from
reasoning text (`run/entry.body.ts:62`, `run/session-data.ts:532`). Opcode42
does not strip it. **Change:** add `strings.ReplaceAll(text, "[REDACTED]", "")`
in the reasoning render path (`render.go:111-115`).

### Acceptance

- `ctrl+x r` toggles collapse/expand (default: collapsed). When collapsed,
  shows a 1-line header (`▸ Thought: <title> · <duration>`), not nothing.
- `ctrl+x f` expands the body when in collapsed mode.
- Reasoning shows a spinner while streaming, a static header with duration
  when done.
- `[REDACTED]` placeholders are stripped from reasoning output.
- New tests: `TestThinking_HideCollapsesReasoning` (header appears, body
  hidden), `TestThinking_ShowsDurationWhenDone` (header includes `· <duration>`),
  `TestReasoning_RedactedStripped`,
  `BenchmarkRenderMarkdown_StreamingPart` (sub-quadratic scaling after
  incremental cache).
- Existing `TestRenderSession_ShowsAllBlockKinds` stays green with
  `hideThinking=false` (expanded mode shows "Thought").

### Divergences to log (plan 12)

- **`ctrl+x r` / `ctrl+x f` keybinds** — opencode's full TUI leaves
  `display_thinking` unbound (`keybind("none")`, `tui/config/keybind.ts:150`),
  reachable only via `/thinking` command palette. Opcode42 binding it to
  `ctrl+x r` is a convenience divergence.
- **Default `hideThinking = true`** — matches opencode's full-TUI "hide"
  default. If changed to "show", log the divergence.

### Performance & clean code

- opencode's coalescing is O(1) per append (`run/footer.ts:540-553` — peek
  `queue.at(-1)`, string concat). The microtask drain flushes the whole queue in
  one pass. The expensive part (markdown render) happens in the retained
  surface, which settles row-by-row (`run/scrollback.surface.ts:287-305`) — only
  new stable blocks are committed. This is the **incremental** optimization
  Opcode42 must adopt (D3).
- Opcode42 currently re-parses the full text on every cache miss, and the cache
  misses every delta. For typical parts (<2k tokens) this is fine; for long
  reasoning it will stutter. D3 is the fix.

---

## Workstream E — Subagent count: two-count model (active vs recent)

**Validated against:** `run/footer.command.tsx:356,374` (activeSubagentCount +
"N active"/"N recent" labels), `run/subagent-data.ts:295-309` (taskStatus —
derives from parent's task tool part), `run/footer.subagent.tsx:29-43,127-135`
(status icons — spinner for running, NOT ◔), `internal/tui/subagent.go:188-189`
(double childrenOf call).

### E1. Two-count model (corrected)

**Correction from original plan:** the plan said "filter by childStatus ==
running." This only yields the running count. opencode uses **two counts**:
- `activeCount` = `tabs.filter(t => t.status == "running").length`
  (`run/footer.command.tsx:356`)
- `totalCount` = `props.subagents().length` (ALL tabs, running or not)
  (`footer.command.tsx:374`)
- Label: `activeCount > 0` → `"${activeCount} active"`; else `totalCount > 0`
  → `"${totalCount} recent"`; else hide.

**Change:** compute both counts:
```
kids := m.childrenOf(cur.ID)
activeCount := 0
for _, k := range kids {
    if m.childStatus(k.ID) == "running" {
        activeCount++
    }
}
totalCount := len(kids)
label := ""
if activeCount > 0 {
    label = fmt.Sprintf("%d active", activeCount)
} else if totalCount > 0 {
    label = fmt.Sprintf("%d recent", totalCount)
}
```

### E2. Fix the childStatus == "" gap

**Critical bug the original plan missed:** `childStatus` (`subagent.go:228-258`)
scans `m.store.messages[childID]`. Children's messages are loaded **lazily** by
`loadChildMessagesCmd` ("on first expand of a task card"). Before the user
expands any task card, **all children have `childStatus == ""`**, and filtering
by `== "running"` would show **"0 active"** even while subagents are running.

opencode avoids this by deriving status from the **parent's task tool part**
(`run/subagent-data.ts:295-309` `taskStatus`), which is in the parent's message
stream (always loaded). It checks `part.state.status` (completed/error) and
metadata `interrupted` for cancelled. It does NOT need the child's messages.

**Changes (choose one):**
- **(Preferred) E2a:** Derive the running count from the parent's task tool
  parts (scan `m.store.parts` for parts with `Tool == "task"` and
  `state.status == "running"`), matching opencode's `taskStatus`. This avoids
  the lazy-load gap and is wire-compatible.
- **(Fallback) E2b:** Eagerly load child messages on session open (extend
  `loadMessagesCmd` to call `loadChildMessagesCmd` for each child).
- **(Simplest) E2c:** Treat `""` as "running" (optimistic) in the footer count
  only, so the count is never an undercount. Document the divergence.

### E3. Correct the icon claim

**Correction:** the plan claimed opencode shows `◔` for running. This is wrong
— `run/footer.subagent.tsx:127-135` renders an **animated spinner** for running,
not `◔`. The `◔` in `statusIcon` (`:29-43`) is effectively dead code for the
running case. Opcode42's sidebar already does the right thing (spinner for
running, `chrome.go:243-245`).

The icons are: spinner (running), `●` (completed), `○` (cancelled), `◍`
(error). Opcode42's sidebar uses spinner/✓/✗/• — the `✗` collapses cancelled and
error into one glyph. opencode distinguishes them. **Optional:** add a
`"cancelled"` return to `childStatus` (check `metadata.interrupted` /
`"Tool execution aborted"`, matching `subagent-data.ts:301-303`) to enable the
`○` vs `◍` distinction. Not required for the count (only running vs not-running
matters).

### E4. De-duplicate childrenOf calls (performance)

`subagentFooterView` calls `childrenOf(cur.ID)` **twice** in the parent case
(`subagent.go:188-189`). The sidebar (`chrome.go:235`) calls it a **third time**
per frame. `childrenOf` is O(n) over all sessions with no index.

**Changes:**
- Bind `kids := m.childrenOf(cur.ID)` once before the switch in
  `subagentFooterView`.
- Consider a per-frame cache field on `Model` (e.g. `m.cachedKids []Session`)
  shared between `subagentFooterView` and `sidebarView` to avoid the third O(n)
  scan.
- Consider a `map[parentID][]Session` index in the store to make `childrenOf`
  O(1) (longer-term).
- `childStatus` (`subagent.go:228-258`) decodes every tool part's JSON state on
  every call — no caching. For a child with many tool parts, this is expensive
  per frame. Consider caching the decoded `toolState` on the `Part` struct.

### E5. Tabs never deleted (parity note)

opencode's `tabs` Map is also a historical, never-pruned set
(`run/subagent-data.ts:333-356` `syncTaskTab` only sets, never deletes). The fix
is to **filter by status**, not to prune completed children. This matches
opencode's approach.

### Acceptance

- After 17 subagents complete, the footer strip shows `"17 recent"` (using
  totalCount), not `"17 sub-agents"`.
- While 3 are running, shows `"3 active"`.
- When 0 children, hides entirely.
- `childStatus == ""` does not cause an undercount.
- New test: `TestSubagentFooter_ShowsActiveVsRecent` — seed 17 children with
  mixed statuses: 3 running + 14 completed → `"3 active"`; 0 running + 17
  completed → `"17 recent"`; 0 children → `""` (hidden).
- Existing `subagent_test.go` stays green (update label assertions).

### Effort re-estimate

The original plan estimated 0.5d. With the two-count logic + the
`childStatus == ""` gap fix (E2), this is closer to **1d** — the status-source
decision (E2a vs E2b vs E2c) is an architectural choice that affects the sidebar
too.

### Adjacent features (noted)

- opencode has a **subagent inspector** (`run/footer.subagent.tsx:45-173`) — a
  footer-region panel showing a selected subagent's live stream. Opcode42's
  equivalent is `enterFirstChild` / `cycleSibling` / `gotoParent`
  (`subagent.go:125-165`) which switches the entire open session — a different
  UX model.
- opencode has a **background subagent** keybind (`session.background`,
  `run/footer.view.tsx:527-533`) — sets `background: true` on the tab. Opcode42
  has no equivalent.
- **No completion notification** in opencode (no toast/banner). The only
  indicators are the status-bar hint and the label switch. Opcode42 matches
  (no notification).

---

## Workstream F — Input box: defined surface + status bar

**Validated against:** `run/footer.view.tsx:642-665` (composer surface + accent),
`run/footer.view.tsx:384-401` (mode label + color), `run/footer.view.tsx:822-910`
(status bar), `run/footer.view.tsx:430-435` (provider hidden), `run/theme.ts:512`
(surface token), `run/footer.prompt.tsx:35-37` (TEXTAREA_MIN/MAX_ROWS),
`run/footer.prompt.tsx:284-294` (placeholder text).

### F1. Composer surface background + left accent

opencode's composer has a `surface` semi-opaque background (`run/theme.ts:512`:
`fade(backgroundMenu, background, 0.18, 0.76, 0.9)`) and a `█` solid-block left
accent in `highlight` color (`run/footer.view.tsx:642-655`).

**Change:** `composer.go:221` currently uses `Background(m.styles.P.Bg)` → swap
to `Background(m.styles.P.BgElev)` (Opcode42's solid equivalent of `surface`).
Document that opencode's `surface` is semi-opaque and `BgElev` is solid — an
intentional minor divergence (solid reads cleanly on transparent terminals).

Keep the existing left accent bar (`composer.go:217-220` ThickBorder in
Blue/Red) — it already matches opencode's `█` intent. Optionally switch to a
literal `█` custom border for pixel parity.

### F2. Status bar: mode chip (mode-colored fg) + model + variant

**Correction from original plan:** the plan said "BgAccent-background box with
the bold mode label." opencode does the **inverse**: `statusAccent` bg +
**mode-colored fg** (cyan/yellow/red — `run/footer.view.tsx:384-401, 824-826`).
The chip bg is a neutral `statusAccent`; the mode color is the **foreground** of
the label.

**Mode label** (`footer.view.tsx:384-390`):
- `"EXIT"` if exiting (red), `"SHELL"` if shell mode (yellow), else `"BUILD"`
  (cyan/highlight).
- **Only BUILD/SHELL/EXIT in the run mini-TUI.** No "plan" mode. The full TUI
  shows the agent name (Title-cased) instead of BUILD
  (`tui/.../prompt/index.tsx:1442-1444`).
- **Exit mode exists** (ctrl+c twice) — the original plan said "we don't have
  plan/exit modes" — exit mode is real, wire it.

**Changes:**
1. `chrome.go:95` `ModeChip` — currently a fixed Blue bg with dark text. Change
   to: bg = `BgSel` (Opcode42's closest neutral lift to `statusAccent`), fg =
   mode-colored (`Blue` for build, `Amber` for shell, `Red` for exit). Bold label.
2. `chrome.go:97-101` — currently shows `· model · provider`. Change to:
   `· model + " " + bold(Amber)(variant)` when `m.model.effectiveVariant() != ""`.
   Drop provider (opencode drops it at `footer.view.tsx:430-435`:
   `provider: undefined`, with a comment "Prefer without provider, but keep it
   on the shared width policy if we add it back").
3. **Agent name vs BUILD:** the full TUI shows the agent name, the run mini-TUI
   shows `BUILD`. Opcode42 currently shows `m.agent` or `"build"`
   (`chrome.go:31-36`) — a hybrid. **Recommendation:** show agent name when an
   agent is active (full-TUI parity), fall back to `BUILD`.

### F3. Status bar background

opencode's status bar bg is `theme().status` (`run/theme.ts:514-519`, a tinted
lift of the footer bg), distinct from `surface`. Opcode42 currently uses
`s.Surface(s.P.Bg)` (`chrome.go:134`). **Change:** use `s.Surface(s.P.BgPanel)`
or add a `StatusBg` token to give the status bar its own surface (this is what
makes it read as a distinct chrome row from the composer).

### F4. Placeholder text

opencode shows placeholder text in the empty composer (`run/footer.prompt.tsx:284-294`):
- Normal: `Ask anything... "Fix a TODO in the codebase"` (only on first prompt)
- Shell: `Run a command... "git status"`

**Change:** add a `Placeholder` to Opcode42's textarea. The bubbles v2
`textarea` supports `Placeholder`.

### F5. Composer multi-line grow

opencode: `TEXTAREA_MIN_ROWS=1`, `TEXTAREA_MAX_ROWS=6` (`run/footer.prompt.tsx:35-37`).
The full TUI: `max_height = prompt?.max_height ?? max(6, floor(height/3))`
(`tui/.../prompt/index.tsx:1340`).

**Change:** verify Opcode42's textarea grows 1→6 rows. The bubbles v2 textarea
should do this by default — verify and document.

### F6. Context hints on the right

opencode shows context-hint chips on the right side of the status bar
(`run/footer.view.tsx:455-473, 884-896`): `background`, `N queued`, `subagents`
(each with key + label, gated by responsive width policy). Opcode42's
`statusBarView` has `ctrl+p commands` on the right. **Change:** consider adding
queued/subagent hints for parity.

### Acceptance

- Composer has `BgElev` background (visually defined against terminal bg).
- Status bar shows a mode chip (neutral bg, mode-colored fg, bold label) + model
  name + variant (bold amber suffix). Provider is dropped.
- Status bar has its own bg (distinct from composer).
- Placeholder text appears in the empty composer.
- New tests: `TestComposer_HasBgElevBackground`, `TestStatusBar_ModeChipAndVariant`
  (assert mode chip + variant appear; provider absent),
  `TestStatusBar_ExitMode` (ctrl+c twice → EXIT chip in Red),
  `TestComposer_Placeholder`.
- Existing `TestStatusBar_ModeChipAndProviderChips` — update to expect the new
  format.

### Adjacent features (noted)

- **`!` shell-mode entry** (`run/footer.prompt.tsx:1055-1094`): typing `!` at
  cursor offset 0 enters shell mode; `esc`/`backspace` at offset 0 exits.
  Opcode42 has shell mode (`composer.go`) — verify the keybind matches.
- **No char counter** on the composer (opencode doesn't have one).
- **No send button** — Enter submits (`run/footer.prompt.tsx:267`).
- **Context usage** shown in status bar as `activityMeta` (session tokens +
  cost, `run/footer.view.tsx:417-423`). Opcode42 has this.

### Performance & clean code

- `statusBarView` (`chrome.go:89-135`) is rebuilt from scratch every frame. At
  ~100ms frame cadence, this is sub-microsecond work — **caching is not worth
  it** unless profiling shows otherwise. The markdown cache is the only render
  cache that matters.
- opencode uses SolidJS fine-grained reactivity (memos for `modeLabel`,
  `modeColor`, `modelStatus`, etc. — only changed spans re-render). Opcode42's
  frame-based render is simpler and fast enough for a 1-row bar.

---

## Workstream G — Paste fix + newline keybinds

**Validated against:** `tui/config/keybind.ts:160-199` (readline keybinds),
`run/footer.prompt.tsx:1391-1415` (paste handling), bubbletea v2
`paste.go:3-12` + `cursed_renderer.go:115-116` (bracketed paste),
bubbles v2 `textarea.go:78-107` (DefaultKeyMap), `textarea.go:1223-1224`
(PasteMsg handler), `internal/tui/model.go:446-565` (key routing).

### G1. Wire tea.PasteMsg (bracketed paste is currently BROKEN)

**Major correction from original plan:** the plan claimed "paste should already
work" with bubbles v2. This is **wrong**.

- bubbletea v2 enables bracketed paste (DECSET 2004) by default
  (`cursed_renderer.go:115-116`). Opcode42's `View()` never sets
  `DisableBracketedPasteMode`, so the terminal advertises it.
- bubbletea v2 parses `\x1b[200~`…`\x1b[201~` into `tea.PasteMsg{Content}`
  (`paste.go:3-12`).
- The bubbles v2 textarea handles `tea.PasteMsg` (`textarea.go:1223-1224`).
- **BUT** `Model.Update` (`model.go:446`) has **no `case tea.PasteMsg:`** —
  verified by `grep -n "PasteMsg" model.go` returning nothing. The message is
  dropped on the floor. **`cmd+v` (macOS bracketed paste) does NOT work today.**
- `ctrl+v` likely works via the bubbles v2 `DefaultKeyMap.Paste` binding
  (`textarea.go:97`) which calls `clipboard.ReadAll()` — but the bracketed-paste
  path is broken.

**Change:** add `case tea.PasteMsg:` to `Model.Update` (alongside
`tea.KeyPressMsg` at `:455`) that forwards to `m.input.Update(msg)`, runs
`resizeComposer()` and `refreshAutocomplete()`, and returns
`tea.Batch(cmd, acCmd)` — mirroring the composer fallthrough at `:560-565`.

**Acceptance:** pasting multi-line text (via `cmd+v` on macOS Terminal.app/
iTerm2) inserts the content at the cursor and auto-grows the composer. New test
`TestComposer_BracketedPasteInsertsText` — send `tea.PasteMsg{Content:
"hello\nworld"}`, assert `m.input.Value() == "hello\nworld"` and
`m.input.Height() >= 2`.

### G2. Readline shortcuts — already provided by bubbles v2 (NO CHANGE)

**Correction from original plan:** the plan proposed adding `ctrl+w`, `ctrl+u`,
`ctrl+a`, `ctrl+e`, `ctrl+k`, `alt+backspace`, `alt+f`/`alt+b` via a KeyMap
override. This is **unnecessary** — bubbles v2's `DefaultKeyMap()`
(`textarea.go:78-107`) already binds ALL of these:

- `ctrl+w`, `alt+backspace` → `DeleteWordBackward` (`:86`)
- `ctrl+u` → `DeleteBeforeCursor` (delete to line start) (`:89`)
- `ctrl+k` → `DeleteAfterCursor` (delete to line end) (`:88`)
- `ctrl+a` → `LineStart` (`:93`); `ctrl+e` → `LineEnd` (`:94`)
- `alt+f`, `alt+b` → `WordForward`/`WordBackward` (`:82-83`)
- `alt+d` → `DeleteWordForward` (`:87`)
- `ctrl+h` → `DeleteCharacterBackward` (`:91`); `ctrl+d` → `DeleteCharacterForward` (`:92`)
- `ctrl+v` → `Paste` (`:97`)

These reach the textarea via the fallthrough at `model.go:562` (no `model.go`
switch case intercepts them). **No code change.** The plan's proposed override
would at best be a no-op and at worst **remove** bindings opencode has
(`ctrl+backspace`, `alt+delete`, `ctrl+right`, `ctrl+left`) that the plan's G2
list omitted. **Leave the default KeyMap intact.**

**tmux conflict:** `ctrl+a` is tmux's prefix. opencode binds it (`keybind.ts:172`);
bubbles v2 binds it (`:93`). Keep it (matches both). tmux users remap anyway.

**Acceptance:** manual verification that ctrl+w/ctrl+u/ctrl+k/ctrl+a/ctrl+e/
alt+f/alt+b work. Add `TestComposer_ReadlineShortcuts` — type "hello world",
press ctrl+w, assert "hello " remains; press ctrl+u, assert input is empty.

### G3. Add ctrl+enter and alt+enter to InsertNewline

opencode binds `input_newline: shift+return, ctrl+return, alt+return, ctrl+j`
(`tui/config/keybind.ts:163`). Opcode42's `New()` only sets
`ta.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j")` (`model.go:244`) —
**missing `ctrl+return` and `alt+return`**.

**Change:** `ta.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j",
"ctrl+enter", "alt+enter")` in `New()`.

**Acceptance:** ctrl+enter and alt+enter insert a newline; plain enter still
submits. Extend `TestComposer_CtrlJNewline_EnterSubmits` to cover ctrl+enter.

### G4. ctrl+c context-dependent clear (optional)

opencode resolves the `input_clear: ctrl+c` / `app_exit: ctrl+c` conflict via
`enabled` guards: ctrl+c clears the composer when it has text
(`tui/.../prompt/index.tsx:806`), exits when empty (`app.tsx:963-966`).
Opcode42 unconditionally quits on ctrl+c (`model.go:469-477`) — a divergence.

**Change (optional):** in the ctrl+c branch, check
`strings.TrimSpace(m.input.Value()) != ""` first → clear input; else quit.

**Acceptance:** ctrl+c with text clears it; ctrl+c with empty composer quits.
New test `TestCtrlC_ClearsInputWhenNonEmpty`.

### G5. Paste summary (DEFERRED)

opencode collapses large pastes (≥3 lines OR >150 chars) into
`[Pasted ~N lines]` virtual text (`tui/.../prompt/index.tsx:1201-1208`, gated by
`paste_summary_enabled` KV). This requires extmark-style virtual text that
bubbles v2's textarea does not natively support. **Defer to a future
workstream** — G ships plain-paste only.

### G6. Mouse support (OUT OF SCOPE)

opencode has full mouse support (click-to-focus, scroll, copy-on-select, dialog
hover — `tui/.../dialog.tsx:210`, `session/index.tsx:337`). Opcode42 has **no
mouse handling** (`grep "tea.Mouse" internal/tui/` is empty). This is a larger
gap than G and belongs in a dedicated mouse workstream. **Not addressed by G.**

### Acceptance summary

- `cmd+v` (bracketed paste) inserts text at cursor (G1).
- `ctrl+v` (clipboard read) still works (unchanged).
- All readline shortcuts work (G2 — already working, add test).
- `ctrl+enter` and `alt+enter` insert newline (G3).
- (Optional) `ctrl+c` clears input when non-empty (G4).
- New tests: `TestComposer_BracketedPasteInsertsText`,
  `TestComposer_ReadlineShortcuts`, extend `TestComposer_CtrlJNewline_EnterSubmits`.

### Effort

- G1 (PasteMsg wiring + test): 0.25d
- G2 (no change, audit + test): 0.1d
- G3 (InsertNewline keys + test): 0.1d
- G4 (ctrl+c context, optional): 0.1d
- **Total: ~0.5d** — unchanged from original estimate, but the work is DIFFERENT
  (paste fix + newline keys, not readline rebind).

### Files touched (revised)

- `internal/tui/model.go` — add `case tea.PasteMsg`; extend `InsertNewline.SetKeys`
  in `New()`; (optional) ctrl+c context check.
- `internal/tui/composer_test.go` — new tests.
- **No changes** to `composer.go`, `canvas.go`, `render.go`, `chrome.go` for G.

---

## Sequencing

```
A (scroll/layout split)  ── foundation; ship first
    ├── B (footer prompt panel)    [depends on A4: body-stays-for-footer-panels]
    ├── C (inline diff)            [independent — toolrender.go]
    ├── D (thinking gate + render) [independent — render.go + store]
    ├── E (subagent count filter)  [independent — subagent.go]
    ├── F (input box + status bar) [depends on A's footer layer]
    └── G (paste + newline)        [independent — model.go]
```

**Critical path:** A → (B, F). C, D, E, G are independent and can ship in
parallel with each other and with A.

B's `renderUnifiedDiff` helper is reused by C — if B ships first, C extracts the
shared helper; if C ships first, B imports it. Coordinate via the plan.

## Effort & sizing

| Workstream | Est | PRs | Notes |
|---|---|---|---|
| A — scroll/layout split + sidebar width 42 | 2d | 1 | Foundation |
| B — footer prompt panel + 3-stage flow + shortcuts | 2d | 1 | Bigger than original: 3-stage flow, multi-question tabs, custom text |
| C — inline diff + cache + multi-file | 1.5d | 1 | Render cache + shared helper extraction |
| D — thinking collapse + duration + incremental cache | 1.5d | 1 | Incremental markdown cache is new work |
| E — subagent two-count + childStatus gap | 1d | 1 | Up from 0.5d (status-source decision) |
| F — input box surface + status bar | 1d | 1 | Mode chip rework + placeholder |
| G — paste fix + newline keys | 0.5d | 1 | Different from original (paste fix, not readline) |
| **Total** | **~9.5d** | **~7 PRs** | Up from ~7d (3-stage flow, incremental cache, childStatus gap) |

## Risks / decisions to flag

1. **Sidebar at 42 cols shows only at width ≥ 121.** Below that the sidebar
   hides and the stream gets the full width. At 121+ cols: leftW = 79 (tight but
   workable). The `maxContentWidth = 100` cap (`render.go:10`) still applies.
2. **Footer height changes** when the composer grows (multi-line) or when the
   tasks/subagent/pty dock opens. The layer split must recompute `footerH` each
   frame via `lipgloss.Height(footer)`. Verify the stream layer's `bodyH`
   shrinks accordingly.
3. **The pre-resize fallback** (`renderSession` at `render.go:15`) still joins
   body + footer — fine for the 0×0 case. The canvas path is the only one that
   needs the split. Extract `buildFooter(leftW)` shared helper for both paths.
4. **B2 — dropping the pending-question in-stream card.** Plan 08e §E4 shipped
   it; opencode doesn't have one. The answered-question card stays (structural
   divergence noted — migrate to tool snapshot when C lands).
5. **D1 — default `hideThinking = true` (collapsed).** Matches opencode's
   full-TUI default ("hide"). The original plan's "default: show" was the run
   mini-TUI's default — wrong reference surface.
6. **D2 — "no new mechanism needed" was misleading.** The incremental markdown
   cache (D3) IS new work — the original plan omitted it by wrongly claiming
   the frame loop was sufficient. Long reasoning parts will stutter without it.
7. **E2 — `childStatus == ""` gap.** The original plan didn't catch this. The
   fix (E2a: derive from parent's task tool parts) is the wire-compatible
   approach and also improves sidebar accuracy.
8. **F2 — mode chip coloring.** The original plan said "BgAccent-background
   box." opencode uses `statusAccent` bg + mode-colored fg. The fix changes
   `ModeChip` to per-mode fg coloring.
9. **G1 — paste is BROKEN, not "should already work."** The original plan was
   wrong. `tea.PasteMsg` is never handled. The fix is a one-case addition.
10. **G2 — readline shortcuts already work.** The original plan proposed
    adding them; they're already bound by bubbles v2 defaults. No change needed
    (just add tests to verify).
11. **C2 — bg-tint divergence.** opencode's run mini-TUI uses transparent diff
    backgrounds; Opcode42 uses tints. Decision: use tints (better readability).
    Log in the known-divergence registry.
12. **F2 — provider in status bar.** opencode drops it (`provider: undefined`).
    Recommendation: drop to match. The comment in opencode's source reserves the
    slot for future re-addition.

## Validation methodology

This plan was validated by 7 parallel research subagents, each reading the
opencode source at `/Users/rotemmiz/git/opencode` with file:line citations.
Key corrections from the original plan:

| Original claim | Correction | Source |
|---|---|---|
| Sidebar 28→36 cols | **42 cols** (opencode's actual width) | `tui/routes/session/sidebar.tsx:31` |
| Sidebar threshold ≥121 | Confirmed (≥121 = `width > 120`) | `tui/routes/session/index.tsx:263` |
| Panel height 12/14 rows | **base + 12 / base + 14** (formula) | `run/footer.ts:700-702` |
| Permission shortcuts a/s/r | **tab/arrows/enter** (opencode) | `run/footer.permission.tsx:214-257` |
| No 3-stage permission flow | **3-stage** (permission → always → reject) | `run/permission.shared.ts:22-23` |
| No full-screen diff viewer | **Exists** in full TUI (not run mini-TUI) | `tui/feature-plugins/system/diff-viewer.tsx` |
| Thinking: hard drop when hidden | **Collapse to header** (full TUI behavior) | `tui/routes/session/index.tsx:1572-1632` |
| Default: show thinking | **Default: hide** (collapsed) | `tui/context/thinking.ts:36` |
| Drop ctrl+x f | **Keep it** (expand body in hide mode) | `tui/routes/session/index.tsx:1577` |
| animTickMsg is flush interval | **Event-driven** (not 100ms tick) | `run/footer.ts:560` |
| No new mechanism needed for D | **Incremental markdown cache needed** | `run/scrollback.surface.ts:287-305` |
| Subagent count: filter by running | **Two-count model** (active + total) | `run/footer.command.tsx:356,374` |
| childStatus is accurate | **Returns "" before child messages loaded** | `internal/tui/subagent.go:228-258` |
| ◔ icon for running | **Spinner** (◔ is dead code) | `run/footer.subagent.tsx:127-135` |
| Mode chip: BgAccent bg | **statusAccent bg + mode-colored fg** | `run/footer.view.tsx:384-401` |
| No plan/exit modes | **Exit mode exists** (ctrl+c twice) | `run/footer.view.tsx:385-390` |
| Paste "should already work" | **BROKEN** — no `case tea.PasteMsg` | `internal/tui/model.go:446` |
| Need to add readline shortcuts | **Already bound** by bubbles v2 defaults | `bubbles/v2/textarea.go:78-107` |