package tui

// canvas.go — Plan 08e §A1+A2: the v2 canvas compositor.
//
// This file replaces the v1 string-compositing render path (paint.go's
// paintBackground + overlayToasts + the lipgloss.Place full-frame centering)
// with lipgloss v2's NewCanvas + Layer API. Every cell is now owned by the
// canvas — no terminal-default bleed, no manual bg re-emit after each SGR
// reset, no string-splice overlays. The canvas is the render root.
//
// Architecture:
//
//   - composeView() is the single render entry. It builds a NewCanvas(w,h),
//     fills the base with the theme Bg (a base layer with a plain whitespace
//     string styled with Bg — the canvas's own cells default to EmptyCell,
//     which renders as a bare space with no SGR; the base layer paints every
//     cell with the Bg SGR so the reset-after-style trick from paint.go is
//     no longer needed: the canvas owns every cell).
//   - Each pane (sidebar, stream body, footer, composer, splash content)
//     becomes a Layer at its (x, y, z) over the base. The pane's content is
//     still produced by the existing view functions (sidebarView(),
//     composerView(), etc) — they return strings, and a Layer's Draw path
//     parses those strings into cells via uv.StyledString.
//   - Overlays (modals, permission, question, diff, autocomplete, toasts)
//     become top-level Layers at Z=20 (modals) or Z=10 (toasts). The canvas
//     z-sorts the compositor's flattened layer list, so a higher-Z layer
//     paints over a lower one. No more string-splice overlays.
//
// Why this kills the known 08c residuals:
//   - "Trailing dark bar on the composer" — the bubbles-internal style we
//     couldn't reach was visible because the outer paintBackground couldn't
//     fill *behind* the textarea's own frame. On the canvas, the composer
//     renders as a layer over the base Bg fill; any cell the composer doesn't
//     paint is the canvas's base Bg cell, so the bubbles-internal default
//     background is masked.
//   - "Modal over a distorted base" — the v1 centerScreen did a
//     lipgloss.Place(w, h, Center, Center, body) which wrapped the base
//     stream in whitespace padding, potentially corrupting ANSI state. On
//     the canvas, the modal is a layer at the centered (x,y); the stream
//     underneath is untouched.

import (
	"math"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// z-ordering for the compositor. Lower draws first (underneath).
const (
	zBase     = 0 // the Bg fill
	zPane     = 1 // sidebar, stream body, footer, composer, dock
	zPopup    = 2 // autocomplete popup (above the composer, below modals)
	zOverlay  = 5 // in-stream cards (subagent, question) — above panes
	zToast    = 10
	zWhichKey = 15 // ctrl+x leader which-key strip (plan 08e §F2) — above toasts, below modals
	zModal    = 20 // modals, permission, question, diff reviewer
)

// streamGutter is the 2-col blank gutter inset on each side of the stream
// column, matching opencode's message column `paddingLeft={2} paddingRight={2}`
// (tui/routes/session/index.tsx:1166). The stream + footer layers are
// positioned at X(streamGutter) and rendered at leftW - 2*streamGutter; the
// gutter cells are painted by the canvas base Bg fill.
const streamGutter = 2

// composeView is the v2 canvas render root, replacing the old renderView +
// paintBackground + overlayToasts string-splice pipeline.
//
// It creates a NewCanvas(w,h), fills every cell with the theme Bg color
// (so the base is opaque Bg, not terminal default — the structural fix for
// the 08c "always paint background" workarounds), then composes each pane
// and overlay as a Layer at its (x,y,z). The compositor handles the z-ordered
// drawing of layers onto the canvas. Every cell is owned by the canvas — no
// terminal-default bleed, no manual bg re-emit, no string-splice overlays.
func (m Model) composeView() string {
	c := m.composeCanvas()
	if c == nil {
		// Pre-first-resize (or non-positive dimensions): fall back to the
		// raw body so something renders during the very first frame.
		return m.bodyContent()
	}
	return c.Render()
}

// composeCanvas builds and returns the v2 canvas for the current frame, or
// nil when dimensions are non-positive (the caller falls back to bodyContent
// in that case). Exposed so tests can inspect the canvas cells directly
// rather than re-deriving the fill/composite logic.
func (m Model) composeCanvas() *lipgloss.Canvas {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}

	canvas := lipgloss.NewCanvas(m.width, m.height)

	// Fill the base with the theme Bg so every cell is owned. We set each
	// cell directly with a space content and the Bg style — this is the
	// structural replacement for paintBackground's SGR re-emit hack. The
	// canvas's own NewScreenBuffer initializes cells to EmptyCell (space,
	// no style), which renderLine trims as trailing whitespace; by giving
	// each cell an explicit Bg style, every cell renders as a styled
	// space, so the full w×h frame is opaque Bg.
	bgStyle := uv.Style{Bg: m.styles.P.Bg}
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			canvas.SetCell(x, y, &uv.Cell{Content: " ", Width: 1, Style: bgStyle})
		}
	}

	// Collect body + overlay layers and draw them in z-order onto the canvas.
	// When a centered modal-class overlay (diff/modal) is active the session
	// body is skipped entirely — matching the v1 renderView switch, which
	// never rendered the body under a modal. Only the base Bg fill + the
	// overlay layers compose, so the fully-covered body work is avoided and
	// no stale body cells leak around the modal.
	//
	// Plan 17 §A4: permission/question are footer panels, NOT centered
	// modals — the body STAYS when only a footer panel is up, so the stream
	// remains visible above the panel (matching opencode). The body is
	// skipped only when modalClassActive() is true (diff/modal).
	modalClassActive := m.modalClassActive()
	var layers []*lipgloss.Layer
	if !modalClassActive {
		for _, l := range m.bodyLayers() {
			if l != nil {
				layers = append(layers, l)
			}
		}
	}
	for _, l := range m.overlayLayers() {
		if l != nil {
			layers = append(layers, l)
		}
	}

	compositor := lipgloss.NewCompositor(layers...)
	compositor.Draw(canvas, canvas.Bounds())

	// Re-fill any cell that ended up without a style after the layers drew.
	// The layers' Draw path clears their bounds to nil before drawing, and
	// lipgloss.Place's whitespace padding produces style-less space cells
	// (treated as EmptyCell by renderLine → trimmed as trailing whitespace,
	// which was the "ragged line width" bug). Re-applying the Bg style to
	// any zero-style cell makes every cell opaque Bg again — the structural
	// "canvas owns every cell" guarantee.
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			c := canvas.CellAt(x, y)
			if c == nil {
				canvas.SetCell(x, y, &uv.Cell{Content: " ", Width: 1, Style: bgStyle})
				continue
			}
			if c.Style.IsZero() {
				c.Style = bgStyle
				if c.Content == "" || c.Content == " " {
					c.Content = " "
					c.Width = 1
				}
			}
		}
	}

	// Splash logo: paint the block-pixel wordmark per-cell onto the canvas
	// (plan 08e §B1 — the native opentui idiom, replacing the v1 string-splice
	// render path). Each glyph cell carries the shimmer Fg and, when the
	// bg-pulse toggle is on (§B2), the breath-tinted Bg. splashContent left
	// 5 blank rows where the logo goes so the body's centered geometry is
	// preserved; we compute the same (x0, y0) the string-based wordmark
	// would have occupied and SetCell each glyph directly. With --no-anim
	// (§B3) the frame is the static peak frame (logoStatic), freezing the
	// shimmer and bg-pulse for deterministic capture / accessibility.
	if m.screen == ScreenSplash && !modalClassActive && !m.footerPanelActive() {
		x0, y0 := m.splashLogoOrigin()
		frame := m.animFrame
		if m.noAnim {
			frame = logoPeakFrame
		}
		paintLogoOnCanvas(canvas, x0, y0, frame, m.styles.P, m.view.bgPulse && !m.noAnim)
	}

	return canvas
}

// bodyLayers returns the layers for the active screen (splash or session),
// excluding overlays. Each pane is positioned at its (x,y) so the canvas
// composes them at the right place without any string-splice math.
func (m Model) bodyLayers() []*lipgloss.Layer {
	switch m.screen {
	case ScreenSession:
		return m.sessionLayers()
	default:
		return m.splashLayers()
	}
}

// overlayLayers returns the top-level overlay layers (modals, permission,
// question, diff, autocomplete, toasts, which-key). Only one modal-class
// overlay is active at a time; we gate each by its active condition so only
// the visible ones compose. When any modal-class overlay is active the body
// layers are skipped upstream in composeCanvas, matching the v1 renderView
// switch that never rendered the body under a modal.
func (m Model) overlayLayers() []*lipgloss.Layer {
	var layers []*lipgloss.Layer

	// Autocomplete popup: sits just above the composer. Only on session
	// screen (the splash composer doesn't autocomplete). Z below modals so
	// an open modal hides it.
	if ac := m.autocompleteView(); ac != "" && m.screen == ScreenSession {
		layers = append(layers, positionedPopup(ac, m.acPopupX(), m.acPopupY(), zPopup))
	}

	// Which-key overlay (plan 08e §F2): when the ctrl+x leader is armed,
	// show the chord-options strip at the bottom of the screen (above the
	// status bar). Z=15: above toasts, below modals. The overlay is gated
	// on m.leader AND no modal-class overlay or footer panel being active
	// — a modal open during a leader (e.g. ctrl+x then a key that opens a
	// modal) clears the leader in handleLeaderKey, and a pending
	// permission/question intercepts keys before the leader check in
	// Update, so the states are mutually exclusive in practice; the guard
	// is defensive.
	if m.leader && !m.modalClassActive() && !m.footerPanelActive() {
		if wkv := m.whichKeyView(); wkv != "" {
			layers = append(layers, lipgloss.NewLayer(wkv).
				X(m.whichKeyLayerX()).
				Y(m.whichKeyLayerY()).
				Z(zWhichKey))
		}
	}

	// Modals / blocking overlays. Two placement strategies:
	//
	//  - Centered modals (diff, modal): each renderer already centers itself
	//    via centerScreen / lipgloss.Place, so we place the already-centered
	//    string directly at (0,0) — no outer Place (that would double-Place).
	//    The body is skipped upstream (modalClassActive) so the full-screen
	//    Place string doesn't cover a rendered body.
	//  - Footer panels (permission, question): plan 17 §B1 — opencode renders
	//    these as a footer-region panel (bottom of screen), NOT a centered
	//    modal. The body STAYS so the stream is visible above the panel. The
	//    panel body (sized innerW × panelH, BgElev bg) is placed at
	//    X(streamGutter) so the panel surface aligns with the stream surface
	//    (plan 18 §B2 review fix) and Y = m.height - panelH so it sits at the
	//    bottom; its blank padding doesn't paint over the body layer above it.
	//
	// The renderView priority is preserved (permission > question > diff >
	// modal); only one is active per the model state.
	switch {
	case m.pendingPermission() != nil:
		if p := m.permissionView(); p != "" {
			panelH := lipgloss.Height(p)
			y := m.height - panelH
			if y < 0 {
				y = 0
			}
			layers = append(layers, lipgloss.NewLayer(p).X(streamGutter).Y(y).Z(zModal))
		}
	case m.pendingQuestion() != nil:
		if q := m.questionView(); q != "" {
			panelH := lipgloss.Height(q)
			y := m.height - panelH
			if y < 0 {
				y = 0
			}
			layers = append(layers, lipgloss.NewLayer(q).X(streamGutter).Y(y).Z(zModal))
		}
	case m.diff.open:
		// The diff reviewer is full-screen, not centered — it renders at
		// (0,0) covering the whole canvas.
		if d := m.diffView(); d != "" {
			layers = append(layers, lipgloss.NewLayer(d).X(0).Y(0).Z(zModal))
		}
	case m.modal != modalNone:
		if mv := m.modalView(); mv != "" {
			layers = append(layers, lipgloss.NewLayer(mv).X(0).Y(0).Z(zModal))
		}
	}

	// Toasts: bottom-right, zToast. Above panes, below modals.
	if t := m.toastOverlayView(); t != "" {
		layers = append(layers, positionedPopup(t, m.toastPopupX(t), m.toastPopupY(t), zToast))
	}

	return layers
}

// modalClassActive reports whether any centered modal-class overlay (diff
// reviewer, or a modal) is currently active. Used by the which-key overlay
// guard to avoid composing the strip under a modal — the leader and a modal
// are mutually exclusive in practice (handleLeaderKey clears the leader
// before opening a modal), but the guard is defensive. Also used by
// composeCanvas to skip the body layers under a centered modal.
//
// Plan 17 §A4: permission/question footer panels are NOT in this set. They
// are footer panels (footerPanelActive), not centered modals — opencode
// keeps the stream visible above the footer panel, so the body must NOT be
// skipped when only a footer panel is up. Only true centered modals (diff,
// sessions, help, palette, etc.) hide the body.
func (m Model) modalClassActive() bool {
	return m.diff.open || m.modal != modalNone
}

// footerPanelActive reports whether a footer-region panel (permission or
// question) is currently active. Plan 17 §A4: these are NOT centered modals
// — opencode renders them as a footer panel with the stream still visible
// above it, so composeCanvas keeps the body layers when only a footer panel
// is up. This is a prerequisite for Workstream B (the footer-panel render of
// permission/question); for now the overlayLayers path still places them at
// (0,0) via centerScreen, but the body is no longer hidden behind them.
func (m Model) footerPanelActive() bool {
	return m.pendingPermission() != nil || m.pendingQuestion() != nil
}

// sessionLayers returns the layers for the session screen: an optional right
// sidebar and a left column (stream body + footer + composer + dock). Each
// is positioned at its (x,y) so the canvas composes them without overlap.
//
// Plan 17 §A1: the stream body and the footer are now SEPARATE layers. The
// stream layer (Y=0, height = screen height - footer height) is the only
// layer that goes through the scroll window, so the footer stays pinned at
// Y=bodyH across every scroll offset. The prior form joined body+footer and
// ran the window over the joined string, which made the footer a suffix of
// the scrollable content — the root cause of bug #1 (composer/status bar
// rode the scroll).
func (m Model) sessionLayers() []*lipgloss.Layer {
	var layers []*lipgloss.Layer

	leftW := m.leftColumnWidth()
	innerW := leftW - 2*streamGutter
	if innerW < 1 {
		innerW = 1
	}
	// Set the stream column's inner width so contentWidth() returns innerW
	// and the body / footer wrap at the gutter-reduced width. The receiver
	// is a value copy; this write does NOT mutate the caller's model — it
	// only affects this render pass (contentWidth is read below).
	m.streamWidth = innerW

	// Footer: composer + status bar (and the tasks/pty/subagent strips above
	// the composer when present). Stacked bottom-up via the shared buildFooter
	// helper (plan 17 §A1) so the fallback path (renderSession) and this path
	// agree on the stacking order. The autocomplete popup is NOT part of the
	// footer — it composes as its own overlay layer at zPopup.
	footer := m.frameFooter(m.buildFooter(innerW))
	footerH := lipgloss.Height(footer)
	bodyH := m.height - footerH
	if bodyH < 1 {
		bodyH = 1
	}

	// Body: the conversation stream, windowed to the stream height. The body
	// is positioned at the top of the left column (offset by streamGutter so
	// the 2-col left gutter is blank base Bg); only this layer scrolls, and
	// the window is computed over the body alone (the footer is not a
	// suffix), so the scroll math never touches the footer.
	sid := m.cfg.SessionID
	bodyLines := m.cachedBodyLines(sid, innerW)
	stream := m.frameStreamLines(bodyLines, bodyH)
	layers = append(layers, lipgloss.NewLayer(stream).X(streamGutter).Y(0).Z(zPane))

	// Footer layer: pinned at Y=bodyH so it never moves with the scroll. The
	// footer string is unchanged across scroll offsets — the only way the
	// composer/status bar can move is if the footer's own height changes
	// (e.g. the composer grows to a second row), which re-derives bodyH.
	// Positioned at X(streamGutter) so the composer + status bar are inset
	// by the same gutter as the stream (plan 18 §B2).
	layers = append(layers, lipgloss.NewLayer(footer).X(streamGutter).Y(bodyH).Z(zPane))

	// Sidebar: right column, full height. Its own layer so it z-orders
	// above the body's trailing cells (the body is width-restricted to
	// innerW starting at X(streamGutter), so the 2-col right gutter between
	// the stream and the sidebar at X(leftW) is blank base Bg).
	if m.sidebarVisible() {
		sidebar := m.cachedSidebar()
		layers = append(layers, lipgloss.NewLayer(sidebar).X(leftW).Y(0).Z(zPane))
	}

	return layers
}

// splashLayers returns the layers for the splash screen: a centered wordmark
// + composer + status. Each is its own layer so the canvas positions them
// without the v1 lipgloss.Place whole-frame-centering hack.
//
// The wordmark itself is NOT in the layer string — splashContent leaves 5
// blank rows where the logo goes so the body's centered geometry is
// preserved, and composeCanvas paints the logo directly onto the canvas via
// per-cell SetCell (plan 08e §B1: the native opentui idiom, and the only way
// to set a per-cell Bg for the bg-pulse field §B2). The string-based logo
// render is retained only for the pre-resize fallback (viewSplash →
// splashContent with w<=0, which renders the glyph text plainly).
func (m Model) splashLayers() []*lipgloss.Layer {
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return nil
	}
	content := m.splashContent(w, h)
	return []*lipgloss.Layer{lipgloss.NewLayer(content).X(0).Y(0).Z(zPane)}
}

// splashContent builds the splash screen body (wordmark, blank, composer,
// blank, hint, blank, status) and centers it on a w×h frame. Shared by
// splashLayers (the canvas path) and viewSplash (the pre-resize fallback).
//
// On the canvas path (w>0, h>0) the wordmark rows are blank — the actual logo
// is painted per-cell by composeCanvas via paintLogoOnCanvas (plan 08e §B1).
// The 5 blank rows preserve the body's centered geometry so the logo lands at
// the correct (x0, y0). For the pre-resize fallback (w<=0) the wordmark is the
// plain "opcode42" text (no shimmer, no canvas) so something renders before
// the first WindowSizeMsg.
func (m Model) splashContent(w, h int) string {
	s := m.styles
	if w <= 0 {
		// Pre-first-resize: stack the elements plain (no centering, no bg fill).
		return lipgloss.JoinVertical(lipgloss.Center,
			s.Base.Bold(true).Render("opcode42"), "", m.composerView(), "",
			s.Faint.Render("enter send · ctrl+j newline · ctrl+p commands · ctrl+c quit"))
	}

	// Logo rows: 5 blank full-width rows. The canvas path paints the actual
	// block-pixel wordmark per-cell onto these rows (plan 08e §B1). Keeping
	// the rows in the body preserves the centered geometry so the paint
	// lands at the same (x0, y0) the string-based wordmark would have.
	logoRows := make([]string, len(opcode42Glyph))
	for i := range logoRows {
		logoRows[i] = lipgloss.NewStyle().Width(w).Render("")
	}
	wordmark := lipgloss.JoinVertical(lipgloss.Left, logoRows...)

	composer := m.composerView()
	composer = lipgloss.PlaceHorizontal(w, lipgloss.Center, composer)

	statusLine := m.statusLine()
	if m.err != nil {
		statusLine = m.err.Error()
	}
	status := lipgloss.NewStyle().Foreground(s.P.FgFaint).Width(w).Align(lipgloss.Center).Render(statusLine)
	hint := lipgloss.NewStyle().Foreground(s.P.FgFaint).Width(w).Align(lipgloss.Center).Render("enter send · ctrl+j newline · ctrl+p commands · ctrl+c quit")
	blank := lipgloss.NewStyle().Width(w).Render("")

	body := lipgloss.JoinVertical(lipgloss.Left, wordmark, blank, composer, blank, hint, blank, status)
	if h <= 0 {
		return body
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, body)
}

// splashLogoOrigin returns the canvas (x0, y0) of the logo's top-left corner,
// matching the centering math of splashContent so the per-cell paint lands
// exactly where the string-based wordmark would have. Used by composeCanvas
// to paint the logo via SetCell (plan 08e §B1).
//
// Horizontal: the logo row is Width(w).Align(Center) on the 19-wide glyph, so
// the glyph's left padding is (w - logoWidth) / 2 (integer floor; remainder on
// the right — lipgloss align.go Center case).
// Vertical: the body is Place'd with PlaceVertical Center, which pads
// `gap - round(gap*0.5)` blank rows above (lipgloss position.go), where
// gap = h - bodyHeight. The logo is the top 5 rows of the body, so its y0 is
// the top padding. bodyHeight = 5 (logo) + 1 (blank) + composerH + 1 + 1 + 1
// + 1 = 10 + composerH.
func (m Model) splashLogoOrigin() (int, int) {
	w, h := m.width, m.height
	x0 := (w - logoWidth) / 2
	if x0 < 0 {
		x0 = 0
	}
	composerH := lipgloss.Height(m.composerView())
	bodyHeight := len(opcode42Glyph) + 1 + composerH + 1 + 1 + 1 + 1 // 10 + composerH
	gap := h - bodyHeight
	if gap < 0 {
		gap = 0
	}
	y0 := gap - int(math.Round(float64(gap)*0.5))
	if y0 < 0 {
		y0 = 0
	}
	return x0, y0
}

// bodyContent returns the unpositioned body string for the pre-resize fallback
// (width/height == 0). It returns whatever the active screen would render,
// without the canvas compositing — matching the v1 guard behavior.
func (m Model) bodyContent() string {
	switch {
	case m.pendingPermission() != nil:
		return m.permissionView()
	case m.pendingQuestion() != nil:
		return m.questionView()
	case m.diff.open:
		return m.diffView()
	case m.modal != modalNone:
		return m.modalView()
	case m.screen == ScreenSession:
		return m.renderSession()
	default:
		return m.viewSplash()
	}
}

// positionedPopup returns a Layer at an explicit (x,y) — used for popups that
// aren't centered (autocomplete above the composer, toasts bottom-right).
func positionedPopup(content string, x, y, z int) *lipgloss.Layer {
	if content == "" {
		return nil
	}
	return lipgloss.NewLayer(content).X(x).Y(y).Z(z)
}

// acPopupX/Y returns the position for the autocomplete popup: just above the
// composer (the composer is the bottom row of the footer; the popup sits one
// row above it). The popup is left-aligned with the composer.
func (m Model) acPopupX() int { return 0 }

func (m Model) acPopupY() int {
	// Composer height (1 row minimum, grows to maxComposerRows) + status bar
	// (1 row). The popup sits above both.
	h := lipgloss.Height(m.composerView()) + lipgloss.Height(m.statusBarView(m.leftColumnWidth()))
	for _, extra := range []string{
		m.tasksDockView(m.leftColumnWidth()),
		m.subagentFooterView(m.leftColumnWidth()),
		m.ptyPaneView(m.leftColumnWidth()),
	} {
		if extra != "" {
			h += lipgloss.Height(extra)
		}
	}
	y := m.height - h - lipgloss.Height(m.autocompleteView())
	if y < 0 {
		y = 0
	}
	return y
}

// toastPopupX/Y returns the bottom-right position for the toast stack.
// The toast stack is anchored to the bottom-right corner with a small margin.
func (m Model) toastPopupX(content string) int {
	w := lipgloss.Width(content)
	x := m.width - w - 1 // 1-col right margin
	if x < 0 {
		x = 0
	}
	return x
}

func (m Model) toastPopupY(content string) int {
	h := lipgloss.Height(content)
	y := m.height - h - 1 // 1-row bottom margin
	if y < 0 {
		y = 0
	}
	return y
}

// cellAt returns the canvas cell at (x,y) for tests that want to assert on
// per-cell content/style. Returns nil if out of bounds.
func (m Model) cellAt(canvas *lipgloss.Canvas, x, y int) *uv.Cell {
	if canvas == nil {
		return nil
	}
	return canvas.CellAt(x, y)
}
