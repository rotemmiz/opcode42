# Plan 18 — Terminal control: mouse wheel, sticky scroll, stream gutter

> **Outcome.** The TUI fully owns the terminal: mouse wheel scrolls the
> stream (not the terminal), new content auto-follows when the user is at the
> tail, the footer + sidebar never move, and the stream column has a 2-col
> gutter on each side matching opencode. The user can scroll infinitely back
> through the whole session history; the bottom (composer + status bar) and
> the right sidebar are pinned for every scroll position.
>
> **Root cause (why this didn't land before).** An earlier `tui-graphics-
> fineness` branch (5 commits, June 2026) attempted mouse-wheel scrollback
> twice — first via `WithMouseCellMotion` (broke native text selection/copy),
> then via DECSET 1007 alt-scroll (worked but was reverted because plain
> arrows conflicted with the input box). Neither landed on `main` because the
> branch predated the Bubble Tea v2 migration (#171) and was abandoned. The
> `scrollregion` package from that work IS on main (via #209) but its
> `Guard`/alt-scroll wiring is NOT used. Bubble Tea v2 changed the API:
> mouse mode is now a field on `View` (`View.MouseMode =
> MouseModeCellMotion`), not a program option — so the v1 approaches need
> re-implementation on the v2 surface.
>
> **What's already on main (verified).** The canvas 3-layer split (plan 17 §A,
> #220) pins the footer + sidebar as separate layers — they don't ride the
> scroll. `tea.PasteMsg` is wired (#217, §G1). ctrl+c context-clear works
> (#217, §G4). The `scrollregion.Region` tail-anchored viewport math is in use.
> What's missing: (1) mouse wheel → scroll, (2) sticky-scroll auto-follow at
> the tail, (3) the 2-col stream gutter, (4) the `maxContentWidth=100` cap is
> a readability relic that prevents the stream from filling wide terminals.

## Links

- **Prior work (abandoned, pre-v2):** `origin/tui-graphics-fineness`
  - `f2cd733` mouse-wheel scrollback (`WithMouseCellMotion`)
  - `880a869` native-selection-safe scrollback via DECSET 1007 + `scrollregion`
  - `b7231ce` revert mouse + revert alt-scroll (plain arrows = input box)
  - `fa61e8b` graphics-fineness: drop 100-col cap, sidebar 28→42, tool boxing
  - `3aa0915` stream column gutter (paddingLeft=2 / paddingRight=2)
- **On main:** `scrollregion/` package (Region + Guard + altscroll), canvas
  3-layer split (`internal/tui/canvas.go:318 sessionLayers`), scroll keys
  (`internal/tui/model.go:606-637`).
- **opencode reference:** `packages/tui/src/routes/session/index.tsx:1165-1184`
  (flexbox layout, `<scrollbox stickyScroll={true} stickyStart="bottom">`),
  `:1166` (`paddingLeft={2} paddingRight={2}`), `:258` (`scrollbar_visible`
  KV signal, default false), `:263` (`width > 120` sidebar threshold).
- **bubbletea v2 API:** `View.MouseMode` (`tea.go:177`), `MouseModeCellMotion`
  (`tea.go:297`), `MouseWheelMsg` (`mouse.go:113`), `View.OnMouse`
  (`tea.go:126`).

---

## Workstream A — Mouse wheel scroll + sticky auto-follow (foundation)

**Validated against:** `index.tsx:1181-1182` (`stickyScroll={true}
stickyStart="bottom"`), `util/scroll.ts` (scroll acceleration, default
speed 3), `scrollregion/scrollregion.go` (tail-anchored Region).

### A1. Enable mouse reporting via `View.MouseMode`

Bubble Tea v2 moved mouse enablement from a program option to a `View` field.
`View.MouseMode = MouseModeCellMotion` enables click, release, and wheel
events (not all-motion — all-motion would break selection even when the
app doesn't need it). The `cursed_renderer.go:121-126` emits
`SetModeMouseButtonEvent + SetModeMouseExtSgr` when `MouseModeCellMotion`
is set.

**Trade-off:** `MouseModeCellMotion` does grab the mouse from the terminal
(like opencode's opentui does — opencode re-implements selection in-app).
This is the intentional trade: full terminal control vs. native selection.
opencode chose full control; Opcode42 matches. Users who want native
selection can hold Shift to bypass (terminal convention for
mouse-reporting apps).

**Change:** in `View()` (`internal/tui/model.go:1775`):
```go
func (m Model) View() tea.View {
    v := tea.NewView(m.composeView())
    v.AltScreen = true
    v.MouseMode = tea.MouseModeCellMotion
    return v
}
```

### A2. Handle `MouseWheelMsg` → scroll the stream

Add a `case tea.MouseWheelMsg:` to `Model.Update` (alongside the existing
key handlers) that routes wheel up/down to `m.scroll.Back(scrollStep)` /
`m.scroll.Forward(scrollStep)`. Ignore wheel events when an overlay owns the
view (focused PTY, diff reviewer, modal, pending permission/question) —
matching the guard pattern from the old `f2cd733` commit.

**Scroll step:** `scrollStep = 3` (already defined at `model.go:1701`),
matching opencode's default `scroll_speed: 3` (`util/scroll.ts:22`).
opencode also has `MacOSScrollAccel` for momentum scrolling; that's a
future enhancement, not in scope for A.

**Change:** add to `Model.Update` (`internal/tui/model.go`):
```go
case tea.MouseWheelMsg:
    if (m.pty.open && m.pty.focused) || m.diff.open || m.modal != modalNone ||
        m.pendingPermission() != nil || m.pendingQuestion() != nil {
        return m, nil
    }
    switch msg.Button {
    case tea.MouseWheelUp:
        m.scroll.Back(scrollStep)
    case tea.MouseWheelDown:
        m.scroll.Forward(scrollStep)
    }
    return m, nil
```

### A3. Sticky-scroll: auto-follow at the tail when new content arrives

**This is the behavior the user is missing.** opencode's `<scrollbox
stickyScroll={true} stickyStart="bottom">` means: when the viewport is at
the bottom (tail), new content keeps it pinned to the tail. When the user
has scrolled up, the view stays at its position (content doesn't drift
underneath).

Opcode42's current `scroll.Region` is tail-anchored: `Offset=0` means
"show the last N lines." When new content arrives and `Offset=0`, the
window naturally includes the new content — auto-follow works at the
tail. **But** when the user scrolls up (`Offset>0`) and new content
arrives, the window's `end = len(lines) - Offset` shifts forward by the
number of new lines — the user's view drifts upward. This is NOT sticky
behavior.

**Fix:** in the `sseEventMsg` handler (`model.go:1078`), after
`m.store = m.store.Reduce(msg.ev)`, check if the scroll was at the tail
BEFORE the new content arrived. If so, keep it at the tail (Offset stays
0, the window naturally follows). If not at the tail, adjust Offset to
keep the same content in view (Offset += linesAdded).

```go
wasAtTail := m.scroll.AtTail()
m.store = m.store.Reduce(msg.ev)
if wasAtTail {
    m.scroll.ToTail()  // explicit: stay pinned to the tail
}
```

Since `Offset=0` already shows the tail and `Reduce` only grows the
body, `wasAtTail && ToTail()` is a no-op at the tail (Offset was 0, stays
0). The key is what happens when NOT at the tail: the view should NOT
drift. But with the tail-anchored model, `Offset>0` means "N lines above
the tail," and when the tail grows, the view shifts. To make it sticky
(not drift), we'd need to track the *content anchor* (the first visible
line's ID) and re-derive Offset after the body grows. That's complex.

**Simpler approach (match opencode's actual behavior):** opencode's
`stickyScroll` only sticks when at the bottom. When scrolled up, new
content is added below but the view doesn't move — because the scrollbox
content grows downward and the viewport stays at its scroll position
(content-anchored, not tail-anchored). Opcode42's tail-anchored model
can't do this without tracking the anchor. **Decision: implement the
simple tail-sticky behavior (A3-simple) now; defer content-anchored
scroll-up to a future workstream.** This matches the user's primary
complaint (footer riding scroll, not scroll-up drift).

**A3-simple:** the `sseEventMsg` handler keeps Offset at 0 when it was 0.
When scrolled up, the view drifts slightly but the footer + sidebar
stay pinned (the canvas split guarantees that). This is the 90% solution.

### A4. Drop the 100-col content width cap

`maxContentWidth = 100` (`render.go:13`) caps the stream at 100 cols even
on a 200-col wide terminal. opencode's full TUI has no such cap — the
stream fills the left column (`flexGrow={1}`). The cap was a readability
heuristic from plan 08c that predates the sidebar-aware layout.

**Change:** remove the `maxContentWidth` cap from `contentWidth()`
(`render.go:380-389`). The stream fills `leftW` (which is
`m.width - sidebarWidth` when the sidebar is visible). On a 200-col
terminal with sidebar: `leftW = 158`, stream = 158 cols. On 80-col
without sidebar: stream = 80 cols.

### Acceptance

- **Mouse wheel scrolls the stream.** Wheel up → toward older content;
  wheel down → toward newer. The footer + sidebar do not move.
  New test: `TestMouseWheel_ScrollsStream` — send `MouseWheelMsg{Button:
  MouseWheelUp}` N times, assert `m.scroll.Offset == N*scrollStep`; send
  `MouseWheelDown` M times, assert `m.scroll.Offset == max(0, N-M)*scrollStep`.
- **Wheel ignored when overlay is active.** New test:
  `TestMouseWheel_IgnoredUnderModal` — set `m.modal = modalPalette`, send
  wheel, assert `m.scroll.Offset == 0`.
- **Sticky at tail.** New test: `TestScroll_StickyAtTailOnNewContent` —
  seed a session, scroll to offset 0, stream in new content via SSE,
  assert the view still shows the tail (last line is the new content).
- **No 100-col cap.** New test: `TestContentWidth_NoCapOnWideTerminal` —
  set `m.width = 200`, `m.sidebarVisible() = false`, assert
  `m.contentWidth() == 200` (was capped to 100).
- Existing `TestView_NeverExceedsViewport`, `TestView_FooterPinnedAcrossScroll`,
  `TestKeyScroll_PageAndCtrlArrowsScroll` stay green.
- Existing `TestCanvas_FooterLayerPinned`, `TestCanvas_SidebarLayerPinned`
  stay green.

### Divergences to log

- **`tui-mouse-cell-motion`** — Opcode42 enables `MouseModeCellMotion`
  (grabs the mouse from the terminal). opencode's opentui does the same
  (re-implements selection in-app). Trade-off: native terminal selection
  requires holding Shift. Intentional; matches opencode's full-TUI choice.

---

## Workstream B — Stream column gutter (visual parity)

**Validated against:** `index.tsx:1166` (`paddingLeft={2} paddingRight={2}`
on the message column), the old `3aa0915` commit (`streamGutter = 2`).

### B1. Inset the stream column with a 2-col gutter on each side

opencode's message column has `paddingLeft={2} paddingRight={2}`. The old
`3aa0915` commit implemented this on v1 as `streamGutter = 2` with a
`lipgloss.NewStyle().Width(leftW).Padding(0, streamGutter).Render(left)`
wrap. On the v2 canvas, the gutter is an X offset on the stream layer +
a width reduction.

**Change:** in `sessionLayers()` (`canvas.go:318`):
- Stream layer: `X(streamGutter).Y(0)` instead of `X(0).Y(0)`, and render
  the body at `leftW - 2*streamGutter` width.
- Footer layer: `X(streamGutter).Y(bodyH)` instead of `X(0).Y(bodyH)`,
  and render the footer at `leftW - 2*streamGutter` width.
- Sidebar: stays at `X(leftW)`, unchanged.
- The 2-col left gutter + 2-col right gutter (before the sidebar) are
  painted by the canvas base Bg fill (already opaque).

```go
const streamGutter = 2

func (m Model) sessionLayers() []*lipgloss.Layer {
    leftW := m.leftColumnWidth()
    innerW := leftW - 2*streamGutter
    if innerW < 1 { innerW = 1 }

    footer := m.frameFooter(m.buildFooter(innerW))
    footerH := lipgloss.Height(footer)
    bodyH := m.height - footerH
    if bodyH < 1 { bodyH = 1 }

    sid := m.cfg.SessionID
    header := m.styles.Section.Render(truncate(m.sessionTitle(sid), innerW))
    blocks := m.sessionStreamBlocks(sid)
    body := header + "\n\n" + strings.Join(blocks, "\n\n")
    stream := m.frameStream(body, bodyH)
    layers = append(layers, lipgloss.NewLayer(stream).X(streamGutter).Y(0).Z(zPane))
    layers = append(layers, lipgloss.NewLayer(footer).X(streamGutter).Y(bodyH).Z(zPane))
    if m.sidebarVisible() {
        sidebar := m.sidebarView()
        layers = append(layers, lipgloss.NewLayer(sidebar).X(leftW).Y(0).Z(zPane))
    }
    return layers
}
```

**Also update:** `renderSession()` (the pre-resize fallback at
`render.go:25`) to apply the same gutter so both paths agree. Use the
`lipgloss.NewStyle().Width(leftW).Padding(0, streamGutter).Render(left)`
wrap (the v1 approach from `3aa0915`).

**Also update:** `contentWidth()` (`render.go:380`) to return
`innerW - 2*streamGutter` (or the callers to use `innerW` directly). The
stream text wraps at `innerW`, not `leftW`, so it doesn't touch the
edges.

### B2. Composer + status bar gutter

The composer and status bar are part of the footer layer. They already
render at the width passed to `buildFooter(innerW)`. With B1, they render
at `innerW` and are positioned at `X(streamGutter)`, so they're inset
matching the stream. opencode's footer also has `paddingLeft={2}
paddingRight={2}` (`index.tsx` flexbox inherits the padding).

### Acceptance

- The stream column has a 2-col blank gutter on the left (between the
  terminal edge and the text) and a 2-col gutter on the right (between
  the text and the sidebar).
- The composer + status bar are inset by the same gutter.
- The sidebar is flush-right (no right gutter — it goes to the terminal
  edge, matching opencode).
- New test: `TestStreamColumn_HasGutter` — render at 100×24 with
  sidebar, assert the first 2 columns of the stream row are blank (Bg
  fill, no content), and the 2 columns before the sidebar are blank.
- Existing goldens will need regeneration (the gutter shifts content by
  2 cols). Regenerate with `go test ./internal/tui/ -run
  TestCanvas_Golden -args -update`.

---

## Workstream C — Scrollbar (optional, parity)

**Validated against:** `index.tsx:1173-1180` (vertical scrollbar,
`scrollbar_visible` KV signal, default false), `keybind.ts:82`
(`session.toggle.scrollbar`).

### C1. Toggleable scrollbar on the right edge of the stream

opencode's scrollbar is off by default (`scrollbar_visible: false`),
toggleable via `session.toggle.scrollbar`. When visible, it renders on
the right edge of the scrollbox with a track + thumb.

**Decision: defer to a follow-up.** The scrollbar is a visual nicety, not
a functional requirement. The mouse wheel + sticky scroll (A) is the
functional fix. The scrollbar can be added later as a thin layer at
`X(leftW - scrollbarWidth)` over the stream. Logged for future work.

---

## Sequencing

```
A (mouse wheel + sticky scroll + drop 100-col cap)  ── ship first
B (stream column gutter)                            ── after A (both touch sessionLayers)
C (scrollbar)                                       ── deferred
```

**Critical path:** A → B. C is independent and deferred.

A and B both touch `sessionLayers()` — if shipped as separate PRs,
B rebases onto A. If shipped as one PR, no conflict. **Recommendation:
ship A and B as one PR** (they're tightly coupled — the gutter + the
scroll + the cap removal are all "make the stream fill the space
correctly and scroll right") to avoid a rebase round.

## Effort

| Workstream | Est | Notes |
|---|---|---|
| A — mouse wheel + sticky scroll + drop 100-col cap | 0.5d | View.MouseMode + MouseWheelMsg handler + sticky-at-tail check + cap removal |
| B — stream column gutter | 0.25d | X offset on layers + innerW + fallback path + golden regen |
| C — scrollbar | deferred | visual nicety, not functional |
| **Total** | **~0.75d, 1 PR** | |

## Risks / decisions

1. **Mouse reporting breaks native selection.** `MouseModeCellMotion`
   grabs the mouse. The terminal can't do native click-drag selection
   while the TUI is running. Users hold Shift to bypass (standard
   terminal convention). opencode makes the same trade. This is the
   user's explicit request ("take control over the mouse").

2. **Sticky-scroll is tail-sticky only (A3-simple).** When scrolled up
   and new content arrives, the view drifts slightly (the tail-anchored
   model shifts). True content-anchored sticky-scroll would require
   tracking the first-visible-line ID and re-deriving Offset after body
   growth — deferred. The primary complaint (footer riding scroll) is
   fixed by the canvas split (already on main); A3 adds the tail-sticky
   auto-follow which is the visible "new content appears" behavior.

3. **Gutter changes goldens.** B1 shifts all stream content by 2 cols.
   Regenerate the canvas goldens (`-update` flag). The existing golden
   tests assert content, not position, so they'll need update not
   removal.

4. **`maxContentWidth` removal.** The 100-col cap was a readability
   heuristic. On wide terminals the stream now fills the full left
   column. If readability is a concern, a future `max_reading_width` KV
   setting could re-introduce a cap (opencode has no cap). For now,
   match opencode (no cap).

5. **Mouse click handling.** A2 only handles the wheel. Mouse clicks
   (click-to-focus, click-to-toggle, click links) are out of scope —
   opencode has full click handling (`onMouseUp`/`onMouseDown` on every
   component) but that requires a hit-testing framework Opcode42
   doesn't have. The `View.OnMouse` callback (`tea.go:126`) could be
   used for view-specific click handling in the future. For now: wheel
   only.

## Validation methodology

This plan was validated by:
- Building fresh from `main` and running an integration test (40 SSE-
  streamed messages, scroll offsets 0/1/3/9/20/1000) confirming the
  canvas 3-layer split pins the footer at the bottom row across all
  offsets — the footer-rides-scroll bug IS fixed in the model.
- Reading the bubbletea v2 `tea.go:84-305` (View struct, MouseMode
  field) and `cursed_renderer.go:121-186` (mouse mode emission) to
  confirm the v2 API for mouse enablement.
- Reading the abandoned `tui-graphics-fineness` branch (5 commits,
  `f2cd733`..`b7231ce`) to understand the prior approaches and why they
  were reverted (selection breakage, arrow-key conflict).
- Reading opencode's full TUI source (`index.tsx:1165-1184` scrollbox,
  `:1166` padding, `:258` scrollbar toggle, `util/scroll.ts` scroll
  speed) for the reference behavior.
- Confirming the `scrollregion` package is on main (`scrollregion.go`,
  `altscroll.go`, `keys.go`) but its `Guard` is not wired in
  `cmd/opcode-tui/main.go`.

## What this plan does NOT do

- **Content-anchored scroll-up.** When scrolled up and new content
  arrives, the view drifts slightly (tail-anchored model). True
  sticky-scroll (content stays, new content appears below) is deferred.
- **Mouse click handling.** Wheel only. Click-to-focus, click-to-toggle,
  click-links require a hit-testing framework.
- **Scroll acceleration.** opencode has `MacOSScrollAccel` for
  momentum scrolling. Opcode42 uses a fixed `scrollStep = 3`. Future
  enhancement.
- **Scrollbar.** Deferred (C). The wheel + sticky scroll is the
  functional fix; the scrollbar is visual.
- **DECSET 1007 alt-scroll.** Not used. `MouseModeCellMotion` is the
  primary path (full mouse control). Alt-scroll would be a fallback
  when mouse reporting is off, but since we're enabling mouse
  reporting, it's redundant. The `scrollregion.Guard` helper stays in
  the package for future use but is not wired.
