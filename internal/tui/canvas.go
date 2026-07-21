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
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// z-ordering for the compositor. Lower draws first (underneath).
const (
	zBase    = 0 // the Bg fill
	zPane    = 1 // sidebar, stream body, footer, composer, dock
	zPopup   = 2 // autocomplete popup (above the composer, below modals)
	zOverlay = 5 // in-stream cards (subagent, question) — above panes
	zToast   = 10
	zModal   = 20 // modals, permission, question, diff reviewer
)

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
	if m.width == 0 || m.height == 0 {
		// Pre-first-resize: fall back to the raw body so something renders
		// during the very first frame. Existing tests that set 0×0 expect
		// a non-panic, possibly-empty result.
		return m.bodyContent()
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
	// We use the Compositor for the z-ordered drawing (it flattens + sorts
	// by z), but we draw onto our pre-filled canvas rather than letting the
	// compositor create its own (which would be sized to the union of layer
	// bounds, not the full terminal).
	var layers []*lipgloss.Layer
	for _, l := range m.bodyLayers() {
		if l != nil {
			layers = append(layers, l)
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

	return canvas.Render()
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
// question, diff, autocomplete, toasts). Only one modal-class overlay is
// active at a time — renderView's switch picked the topmost, but on the
// canvas we can compose them all; the z-order resolves which is visible.
// We still gate each by its active condition so only the visible ones
// compose (cheaper, and matches the v1 behavior where only one shows).
func (m Model) overlayLayers() []*lipgloss.Layer {
	var layers []*lipgloss.Layer

	// Autocomplete popup: sits just above the composer. Only on session
	// screen (the splash composer doesn't autocomplete). Z below modals so
	// an open modal hides it.
	if ac := m.autocompleteView(); ac != "" && m.screen == ScreenSession {
		layers = append(layers, positionedPopup(ac, m.acPopupX(), m.acPopupY(), zPopup))
	}

	// Modals / blocking overlays: centered, zModal. The renderView switch
	// priority is preserved (permission > question > diff > modal) but on
	// the canvas only one of them is active at a time per the model state.
	switch {
	case m.pendingPermission() != nil:
		layers = append(layers, centeredLayer(m.permissionView(), m.width, m.height, zModal))
	case m.pendingQuestion() != nil:
		layers = append(layers, centeredLayer(m.questionView(), m.width, m.height, zModal))
	case m.diff.open:
		// The diff reviewer is full-screen, not centered — it renders at
		// (0,0) covering the whole canvas.
		if d := m.diffView(); d != "" {
			layers = append(layers, lipgloss.NewLayer(d).X(0).Y(0).Z(zModal))
		}
	case m.modal != modalNone:
		layers = append(layers, centeredLayer(m.modalView(), m.width, m.height, zModal))
	}

	// Toasts: bottom-right, zToast. Above panes, below modals.
	if t := m.toastOverlayView(); t != "" {
		layers = append(layers, positionedPopup(t, m.toastPopupX(t), m.toastPopupY(t), zToast))
	}

	return layers
}

// sessionLayers returns the layers for the session screen: an optional right
// sidebar and a left column (stream body + footer + composer + dock). Each
// is positioned at its (x,y) so the canvas composes them without overlap.
func (m Model) sessionLayers() []*lipgloss.Layer {
	var layers []*lipgloss.Layer

	leftW := m.leftColumnWidth()

	// Footer: composer + status bar (and the tasks/pty/subagent strips above
	// the composer when present). Stacked bottom-up; the composer is the
	// lowest row of the footer.
	footer := m.composerView() + "\n" + m.statusBarView(leftW)
	footerParts := []string{footer}
	if ac := m.autocompleteView(); ac != "" {
		// When the autocomplete popup is open it renders above the composer;
		// we keep the popup as its own overlay layer (see overlayLayers)
		// and leave the composer itself in the footer.
		_ = ac
	}
	if dock := m.tasksDockView(leftW); dock != "" {
		footerParts = append([]string{dock}, footerParts...)
	}
	if sf := m.subagentFooterView(leftW); sf != "" {
		footerParts = append([]string{sf}, footerParts...)
	}
	if pty := m.ptyPaneView(leftW); pty != "" {
		footerParts = append([]string{pty}, footerParts...)
	}
	footer = strings.Join(footerParts, "\n")

	// Body: the conversation stream, scrolled. The body is a single string
	// that we position at the top of the left column; the footer is at the
	// bottom. The body's height is the screen height minus the footer's
	// height; the existing frame() helper does the scroll windowing.
	sid := m.cfg.SessionID
	header := m.styles.Section.Render(truncate(m.sessionTitle(sid), leftW))
	var blocks []string
	for _, msg := range m.store.messages[sid] {
		if b := m.renderMessage(msg, m.store.parts[msg.ID]); b != "" {
			blocks = append(blocks, b)
		}
	}
	body := header + "\n\n" + strings.Join(blocks, "\n\n")
	scrolled := m.frame(body, footer)
	layers = append(layers, lipgloss.NewLayer(scrolled).X(0).Y(0).Z(zPane))

	// Sidebar: right column, full height. Its own layer so it z-orders
	// above the body's trailing cells (the body is width-restricted to
	// leftW, so there's no overlap; but the sidebar layer's own Bg fills
	// its column cleanly).
	if m.sidebarVisible() {
		sidebar := m.sidebarView()
		layers = append(layers, lipgloss.NewLayer(sidebar).X(leftW).Y(0).Z(zPane))
	}

	return layers
}

// splashLayers returns the layers for the splash screen: a centered wordmark
// + composer + status. Each is its own layer so the canvas positions them
// without the v1 lipgloss.Place whole-frame-centering hack.
func (m Model) splashLayers() []*lipgloss.Layer {
	s := m.styles
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return nil
	}

	// Build the splash content as a single joined string and center it as
	// one layer. This mirrors viewSplash's layout: wordmark, blank, composer,
	// blank, hint, blank, status. The bg fill is owned by the base layer.
	logoRows := logoFrame(m.animFrame, s.P)
	logoLines := make([]string, len(logoRows))
	for i, row := range logoRows {
		logoLines[i] = lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(row)
	}
	wordmark := lipgloss.JoinVertical(lipgloss.Left, logoLines...)

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
	// Center the body in the canvas as a single layer.
	content := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, body)
	return []*lipgloss.Layer{lipgloss.NewLayer(content).X(0).Y(0).Z(zPane)}
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

// centeredLayer returns a Layer whose content is centered on a w×h canvas.
// Used for modals and blocking overlays: the content is pre-centered via
// lipgloss.Place and placed at (0,0) so it covers the whole canvas. The
// canvas's own base + body layers render underneath because of z-order.
func centeredLayer(content string, w, h, z int) *lipgloss.Layer {
	if content == "" || w == 0 || h == 0 {
		return nil
	}
	placed := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
	return lipgloss.NewLayer(placed).X(0).Y(0).Z(z)
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
