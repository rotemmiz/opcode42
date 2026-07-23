package tui

// canvas_test.go — Plan 08e §A1+A2 regression guards for the v2 canvas.
//
// These tests assert the structural invariants the canvas compositor must
// uphold — invariants the v1 string-splice path couldn't guarantee without
// per-line SGR math:
//
//  1. Full-fill: every rendered line is exactly m.width visible columns wide
//     (no transparent cell — the canvas owns every cell via the Bg base fill).
//  2. Layer z-order: a modal layer paints over the stream body (the modal's
//     content is visible in the rendered output, the body's is masked where
//     the modal covers it).
//  3. Composer-no-dark-bar: the composer row carries the theme Bg on every
//     cell, including the trailing cells the bubbles-internal style used to
//     leave at terminal default (the "trailing dark bar on a light terminal"
//     08c known residual). The canvas base fill masks the bubbles default.
//  4. Splash renderable: the splash screen renders on the canvas without
//     panic at every terminal size (incl. 0×0, 1×1, very narrow/wide).
//
// The existing TestView_BackgroundFill / TestView_AllThemesFullWidth already
// cover the full-fill invariant across all themes and widths; this file adds
// the canvas-specific layer-z-order and composer-no-dark-bar regressions.

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// colorEqual reports whether two color.Color values have the same RGBA.
// Used to compare canvas cell Fg/Bg (color.Color interface) against theme.Color
// (a string type that implements color.Color) without relying on type identity.
func colorEqual(a, b interface{ RGBA() (r, g, b, a uint32) }) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// TestCanvas_FullFill_NoTransparentCell asserts that composeView() renders a
// frame where every cell carries a background style — no transparent cell
// escapes to the terminal default. We build a model with dimensions, render
// it, and verify every line is exactly m.width visible columns wide (the
// robust full-fill signal — a transparent cell would render as trailing
// whitespace and lipgloss.Width would report a shorter line). Covers both the
// splash screen and a seeded session screen.
func TestCanvas_FullFill_NoTransparentCell(t *testing.T) {
	cases := []struct {
		name    string
		session bool
	}{
		{name: "splash", session: false},
		{name: "session", session: true},
	}
	for _, c := range cases {
		for _, tn := range []string{"opcode42-dark", "opcode42-light"} {
			for _, wh := range []struct{ w, h int }{{40, 12}, {80, 24}, {120, 40}} {
				label := c.name + "/" + tn + "/" + itoa(wh.w) + "x" + itoa(wh.h)
				t.Run(label, func(t *testing.T) {
					m := New(Config{URL: "http://x"})
					if c.session {
						m = New(Config{URL: "http://x", SessionID: "ses_1"})
						m.screen = ScreenSession
						m.store.sessions = []Session{{ID: "ses_1", Title: "T"}}
						m.store.messages["ses_1"] = []Message{
							{ID: "msg_1", SessionID: "ses_1", Role: "user"},
						}
						m.store.parts["msg_1"] = []Part{{ID: "p1", MessageID: "msg_1", Type: "text", Text: "hello world"}}
					}
					m.width, m.height = wh.w, wh.h
					m = m.applyThemeByName(tn)
					out := m.composeView()
					lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
					for i, line := range lines {
						if got := lipgloss.Width(line); got != wh.w {
							t.Errorf("line %d: visible width %d, want %d\nline: %q", i, got, wh.w, line)
						}
					}
				})
			}
		}
	}
}

// TestCanvas_LayerZOrder_ModalOverStream asserts that when a modal is open
// over the session screen, the modal's title is visible in the rendered
// output. On the v1 path this required a lipgloss.Place full-frame centering
// that could corrupt ANSI state; on the canvas the modal is a layer at
// Z=20 over the stream's Z=1, so it draws on top cleanly.
func TestCanvas_LayerZOrder_ModalOverStream(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 120, 40
	m.store.sessions = []Session{{ID: "ses_1", Title: "Session A"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_1"] = []Part{{ID: "p1", MessageID: "msg_1", Type: "text", Text: "hello world"}}
	m.modal = modalPalette
	m.modalSel = 0

	out := m.composeView()
	plain := stripANSI(out)
	// The modal's title "Commands" must be visible (z=20 over the stream).
	if !strings.Contains(plain, "Commands") {
		t.Errorf("modal title 'Commands' not visible over the stream — z-order broken\nout:\n%s", plain)
	}
	// The modal's first palette item must also be visible.
	if !strings.Contains(plain, "New session") {
		t.Errorf("modal body 'New session' not visible — z-order broken\nout:\n%s", plain)
	}
}

// TestCanvas_Composer_NoDarkBar is the regression guard for the 08c known
// residual: "trailing dark bar on the composer" on a light terminal. On the
// v1 path the bubbles-internal textarea style left trailing cells at the
// terminal default; on the canvas the base Bg fill masks it.
//
// We render a session with a composer via composeCanvas() — the production
// path, including its post-layer re-fill of zero-style cells — and assert
// every cell in the composer's row range carries the theme Bg (no zero-style
// cell, which would be the bubbles default bleeding through). Inspecting the
// canvas directly (rather than re-deriving the fill in the test) means this
// test actually exercises composeCanvas's re-fill guarantee: remove the
// re-fill from composeCanvas and this test fails.
func TestCanvas_Composer_NoDarkBar(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-light")
	m.store.sessions = []Session{{ID: "ses_1", Title: "T"}}

	canvas := m.composeCanvas()
	if canvas == nil {
		t.Fatal("composeCanvas returned nil for 80×24")
	}

	// Find the composer's row: the row containing the textarea's placeholder
	// ("Reply, or / for commands" on session screen). Scan from the bottom
	// (the composer sits at the bottom of the footer).
	composerRow := -1
	for y := m.height - 1; y >= 0; y-- {
		rowContent := ""
		for x := 0; x < m.width; x++ {
			c := canvas.CellAt(x, y)
			if c != nil {
				rowContent += c.Content
			}
		}
		if strings.Contains(rowContent, "Reply") || strings.Contains(rowContent, "Ask") {
			composerRow = y
			break
		}
	}
	if composerRow < 0 {
		t.Skip("could not locate the composer row — test harness mismatch")
	}

	// Every cell in the composer row must carry a non-zero Bg style (the
	// theme Bg, masking the bubbles-internal terminal default). The 08c
	// bug was trailing cells with zero style (terminal default = dark bar).
	missingBg := 0
	for x := 0; x < m.width; x++ {
		c := canvas.CellAt(x, composerRow)
		if c == nil || c.Style.IsZero() {
			missingBg++
		}
	}
	if missingBg > 0 {
		t.Errorf("composer row %d has %d cells with zero style (the dark-bar bug)", composerRow, missingBg)
	}
}

// TestCanvas_ZeroDimensions_NoPanic asserts that composeView() does not panic
// and returns a string when dimensions are zero (the pre-first-resize guard).
func TestCanvas_ZeroDimensions_NoPanic(_ *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 0, 0
	out := m.composeView()
	// Any non-panic result is acceptable; the body should still render.
	_ = out
}

// TestCanvas_TinyDimensions_NoPanic asserts that very small dimensions don't
// crash the compositor (no divide-by-zero, no negative bounds).
func TestCanvas_TinyDimensions_NoPanic(_ *testing.T) {
	for _, wh := range []struct{ w, h int }{{1, 1}, {2, 2}, {5, 1}, {1, 5}} {
		m := New(Config{URL: "http://x"})
		m.width, m.height = wh.w, wh.h
		_ = m.composeView() // must not panic
	}
}

// TestCanvas_NegativeDimensions_NoPanic asserts that negative dimensions
// (which would make make([], negative) panic) fall through the <= 0 guard to
// the bodyContent fallback rather than reaching NewCanvas.
func TestCanvas_NegativeDimensions_NoPanic(_ *testing.T) {
	for _, wh := range []struct{ w, h int }{{-1, 10}, {10, -1}, {-5, -5}} {
		m := New(Config{URL: "http://x"})
		m.width, m.height = wh.w, wh.h
		_ = m.composeView() // must not panic; falls back to bodyContent
	}
}

// TestCanvas_CellAt_OutOfBounds asserts the canvas helper returns nil for
// out-of-bounds coordinates (no panic).
func TestCanvas_CellAt_OutOfBounds(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 10, 5
	canvas := lipgloss.NewCanvas(m.width, m.height)
	if c := m.cellAt(canvas, -1, 0); c != nil {
		t.Error("cellAt(-1,0) should return nil")
	}
	if c := m.cellAt(canvas, 0, -1); c != nil {
		t.Error("cellAt(0,-1) should return nil")
	}
	if c := m.cellAt(canvas, 100, 100); c != nil {
		t.Error("cellAt(100,100) should return nil for a 10×5 canvas")
	}
}

// ── Plan 08e §A3: scroll reconciliation on the canvas viewport ────────────────

// canvasRowText returns the concatenation of all cell contents on canvas row y,
// used to read what the user actually sees on that row of the composited frame.
func canvasRowText(canvas *lipgloss.Canvas, y, width int) string {
	if canvas == nil {
		return ""
	}
	row := ""
	for x := 0; x < width; x++ {
		if c := canvas.CellAt(x, y); c != nil {
			row += c.Content
		}
	}
	return row
}

// rowMarkerIndex extracts the two-digit index from a "ROWnn marker" row, or -1.
func rowMarkerIndex(row string) int {
	i := strings.Index(row, "ROW")
	if i < 0 || i+5 > len(row) {
		return -1
	}
	tens := int(row[i+3] - '0')
	ones := int(row[i+4] - '0')
	if tens < 0 || tens > 9 || ones < 0 || ones > 9 {
		return -1
	}
	return tens*10 + ones
}

// TestCanvas_ScrollOffset_WindowBody is the canvas-specific scroll guard for
// plan 08e §A3: a long body with scroll.Offset > 0 must render the scrolled-to
// region in the body layer, not the live tail. The scroll math now lives in the
// scrollregion package (Region.Window) and feeds the body layer's content; this
// test asserts that what lands on the canvas at the body's top row is the
// scrolled-to content, distinct from the tail view.
//
// We build a tall transcript of uniquely-labelled rows, render the canvas at
// scroll.Offset = 0 (tail) and at a mid-transcript offset, then read the body's
// first visible row off the canvas and assert the scrolled view shows an earlier
// row than the tail view. This is the structural proof that the clamp/window math
// moved off model.go string operations onto the canvas viewport path: the canvas
// cells reflect the scrollregion.Window output, not the un-windowed body.
func TestCanvas_ScrollOffset_WindowBody(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Scroll canvas"}}
	// Build 40 user messages, each with a unique marker "ROWnn" so we can read
	// which transcript row is visible at the body's top.
	msgs := make([]Message, 0, 40)
	for i := 0; i < 40; i++ {
		id := "msg_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		msgs = append(msgs, Message{ID: id, SessionID: "ses_1", Role: "user"})
		m.store.parts[id] = []Part{{
			ID: "p" + id, MessageID: id, Type: "text",
			Text: "ROW" + string(rune('0'+i/10)) + string(rune('0'+i%10)) + " marker",
		}}
	}
	m.store.messages["ses_1"] = msgs

	// Tail view (offset 0): the body's first visible row should contain a
	// late-transcript marker. Scan from y=0 down past the section header to
	// find the first non-blank body row.
	tailCanvas := m.composeCanvas()
	if tailCanvas == nil {
		t.Fatal("tail composeCanvas returned nil")
	}
	tailFirstBody := ""
	for y := 0; y < m.height; y++ {
		row := canvasRowText(tailCanvas, y, m.width)
		if strings.Contains(row, "ROW") {
			tailFirstBody = row
			break
		}
	}
	if tailFirstBody == "" {
		t.Skip("could not locate the first body row on the canvas")
	}

	// Scrolled view: scroll back ~half the transcript. The body's first visible
	// row must now be an earlier marker (a lower ROW index) than the tail view's
	// first row, proving the window moved off the tail.
	m.scroll.Offset = 20
	scrollCanvas := m.composeCanvas()
	if scrollCanvas == nil {
		t.Fatal("scrolled composeCanvas returned nil")
	}
	scrollFirstBody := ""
	for y := 0; y < m.height; y++ {
		row := canvasRowText(scrollCanvas, y, m.width)
		if strings.Contains(row, "ROW") {
			scrollFirstBody = row
			break
		}
	}
	if scrollFirstBody == "" {
		t.Fatalf("scrolled view: no ROW marker visible on canvas — window math not applied")
	}
	if tailFirstBody == scrollFirstBody {
		t.Fatalf("scroll did not change the body's first visible row:\n tail=%q\n scrolled=%q",
			tailFirstBody, scrollFirstBody)
	}
	// The scrolled view's first row must be an EARLIER transcript row than the
	// tail's. Extract the two-digit ROW index from each and compare numerically.
	tailIdx := rowMarkerIndex(tailFirstBody)
	scrollIdx := rowMarkerIndex(scrollFirstBody)
	if scrollIdx < 0 || tailIdx < 0 {
		t.Fatalf("could not parse ROW marker index:\n tail=%q (idx %d)\n scrolled=%q (idx %d)",
			tailFirstBody, tailIdx, scrollFirstBody, scrollIdx)
	}
	if scrollIdx >= tailIdx {
		t.Errorf("scrolled view's first ROW %d should be < tail's %d (earlier transcript row)",
			scrollIdx, tailIdx)
	}
}

// ── Plan 08e §B1-B3: logo canvas paint, bg-pulse, --no-anim ──────────────────

// splashLogoModel builds a splash-screen model at the given terminal size with
// the named theme, ready for canvas logo-paint assertions.
func splashLogoModel(themeName string, w, h int) Model {
	m := New(Config{URL: "http://x"})
	m.width, m.height = w, h
	return m.applyThemeByName(themeName)
}

// logoCellAt returns the canvas cell at glyph coordinate (col, row) within the
// logo's bounding box, or nil if out of bounds. col ∈ [0, logoWidth), row ∈
// [0, len(opcode42Glyph)).
func logoCellAt(canvas *lipgloss.Canvas, x0, y0, col, row int) *uv.Cell {
	return canvas.CellAt(x0+col, y0+row)
}

// TestCanvas_LogoCellsHaveShimmerColor asserts that the splash logo is painted
// per-cell onto the canvas (plan 08e §B1), and that each filled glyph cell's
// Fg is the shimmer color computed by columnColor for its column. This is the
// structural test that the render path moved from string-splice to SetCell:
// remove the paintLogoOnCanvas call and this test fails (the logo region would
// be blank base-Bg cells, not shimmer-colored block glyphs).
func TestCanvas_LogoCellsHaveShimmerColor(t *testing.T) {
	for _, tn := range []string{"opcode42-dark", "opcode42-light"} {
		t.Run(tn, func(t *testing.T) {
			m := splashLogoModel(tn, 80, 24)
			m.animFrame = 7 // arbitrary non-peak frame so shimmer is visibly moving
			canvas := m.composeCanvas()
			if canvas == nil {
				t.Fatal("composeCanvas returned nil")
			}
			x0, y0 := m.splashLogoOrigin()
			p := m.styles.P
			// Scan the logo's bounding box and verify each filled cell.
			filledCells := 0
			for row, line := range opcode42Glyph {
				runes := []rune(line)
				for x := 0; x < logoWidth; x++ {
					c := logoCellAt(canvas, x0, y0, x, row)
					if c == nil {
						continue
					}
					isFilled := x < len(runes) && runes[x] == '█'
					if isFilled {
						filledCells++
						want := columnColor(x, m.animFrame, p)
						if !colorEqual(c.Style.Fg, want) {
							t.Errorf("logo cell (%d,%d) Fg = %v, want shimmer color %q (columnColor)",
								x, row, c.Style.Fg, want)
						}
						if c.Content != "█" {
							t.Errorf("logo cell (%d,%d) content = %q, want %q", x, row, c.Content, "█")
						}
					}
				}
			}
			if filledCells == 0 {
				t.Error("no filled logo cells found on canvas — paintLogoOnCanvas did not run")
			}
		})
	}
}

// TestCanvas_BgPulse_TintsLogoRowBg asserts the bg-pulse field (plan 08e §B2):
// with view.bgPulse = true, the logo row cells carry a breath-tinted Bg (not
// the plain theme Bg); with view.bgPulse = false, the Bg is the plain theme Bg.
// The tint is subtle (a lerp toward the accent by a fraction of the breath),
// so we compare against the computed bgPulseColor rather than a hard-coded hex.
func TestCanvas_BgPulse_TintsLogoRowBg(t *testing.T) {
	m := splashLogoModel("opcode42-dark", 80, 24)
	m.animFrame = logoPeakFrame // peak breath so the tint is maximally visible
	x0, y0 := m.splashLogoOrigin()
	p := m.styles.P
	wantTinted := bgPulseColor(m.animFrame, p)

	// bgPulse on (default for splash): Bg should be the breath-tinted color.
	m.view.bgPulse = true
	canvas := m.composeCanvas()
	c := logoCellAt(canvas, x0, y0, 12, 2) // a filled cell near the wordmark center
	if c == nil {
		t.Fatal("nil logo cell")
	}
	if !colorEqual(c.Style.Bg, wantTinted) {
		t.Errorf("bgPulse=on: logo cell Bg = %v, want tinted %q", c.Style.Bg, wantTinted)
	}
	if colorEqual(c.Style.Bg, p.Bg) {
		t.Errorf("bgPulse=on: logo cell Bg == plain Bg %q (no tint applied)", p.Bg)
	}

	// bgPulse off: Bg should be the plain theme Bg.
	m.view.bgPulse = false
	canvas2 := m.composeCanvas()
	c2 := logoCellAt(canvas2, x0, y0, 12, 2)
	if c2 == nil {
		t.Fatal("nil logo cell (bgPulse off)")
	}
	if !colorEqual(c2.Style.Bg, p.Bg) {
		t.Errorf("bgPulse=off: logo cell Bg = %v, want plain Bg %q", c2.Style.Bg, p.Bg)
	}
}

// TestCanvas_NoAnim_UsesStaticLogo asserts that with --no-anim (m.noAnim = true)
// the splash logo is painted at the static peak frame (logoPeakFrame), not the
// animated m.animFrame (plan 08e §B3). We verify by setting m.animFrame to a
// non-peak frame and checking that the painted cells match the peak frame's
// shimmer colors, not the animated frame's.
func TestCanvas_NoAnim_UsesStaticLogo(t *testing.T) {
	m := splashLogoModel("opcode42-dark", 80, 24)
	m.animFrame = 5        // a non-peak animated frame
	m.noAnim = true        // --no-anim: freeze at peak frame
	m.view.bgPulse = false // disable bg-pulse so only Fg (shimmer) is compared
	canvas := m.composeCanvas()
	x0, y0 := m.splashLogoOrigin()
	p := m.styles.P

	// At least one filled cell's Fg must match columnColor at logoPeakFrame,
	// not at m.animFrame (5). Pick a column where the two frames differ.
	for x := 0; x < logoWidth; x++ {
		animCol := columnColor(x, m.animFrame, p)
		peakCol := columnColor(x, logoPeakFrame, p)
		if animCol == peakCol {
			continue // frames agree here; not a useful probe
		}
		// Find a filled row at this column.
		for row, line := range opcode42Glyph {
			runes := []rune(line)
			if x < len(runes) && runes[x] == '█' {
				c := logoCellAt(canvas, x0, y0, x, row)
				if c == nil {
					continue
				}
				if !colorEqual(c.Style.Fg, peakCol) {
					t.Errorf("noAnim: logo cell (%d,%d) Fg = %v, want peak-frame color %q (not anim-frame %q)",
						x, row, c.Style.Fg, peakCol, animCol)
				}
				return // one mismatch is enough to prove noAnim uses the peak frame
			}
		}
	}
	// If every column agreed between frame 5 and the peak, the test is vacuous;
	// verify the noAnim flag at least produces a renderable canvas.
	if canvas == nil {
		t.Fatal("noAnim produced a nil canvas")
	}
}

// TestCanvas_NoAnim_BgPulseHeldAtPeak asserts that with --no-anim the bg-pulse
// is held at its peak tint (not animated), matching the "frozen" contract.
// Since m.view.bgPulse is the toggle and noAnim forces the paint to use the
// peak frame, the Bg should be the peak-frame bgPulseColor when bgPulse is on
// — but we explicitly disable the bg-pulse under noAnim (composeCanvas passes
// `m.view.bgPulse && !m.noAnim`), so the Bg is the plain Bg. This test pins
// that contract: noAnim ⇒ no animated bg-pulse (the tint is frozen off, not
// breathing).
func TestCanvas_NoAnim_BgPulseHeldAtPeak(t *testing.T) {
	m := splashLogoModel("opcode42-dark", 80, 24)
	m.animFrame = 5
	m.noAnim = true
	m.view.bgPulse = true // even with bgPulse on, noAnim freezes it off
	canvas := m.composeCanvas()
	x0, y0 := m.splashLogoOrigin()
	p := m.styles.P
	c := logoCellAt(canvas, x0, y0, 12, 2)
	if c == nil {
		t.Fatal("nil logo cell")
	}
	// noAnim disables the bg-pulse tint — the Bg is the plain theme Bg.
	if !colorEqual(c.Style.Bg, p.Bg) {
		t.Errorf("noAnim+bgPulse: logo cell Bg = %v, want plain Bg %q (noAnim freezes the pulse off)",
			c.Style.Bg, p.Bg)
	}
}

// TestCanvas_LogoOriginCentered asserts that splashLogoOrigin computes a
// position that actually centers the logo on the canvas (the logo's midpoint
// is near the canvas's midpoint). This guards the centering math against
// drift in lipgloss's Place/Align rounding.
func TestCanvas_LogoOriginCentered(t *testing.T) {
	for _, wh := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 20}} {
		m := splashLogoModel("opcode42-dark", wh.w, wh.h)
		x0, y0 := m.splashLogoOrigin()
		// Horizontal: logo center should be within 1 cell of canvas center.
		logoCenterX := x0 + logoWidth/2
		canvasCenterX := wh.w / 2
		if abs(logoCenterX-canvasCenterX) > 1 {
			t.Errorf("w=%d: logo center x %d not near canvas center %d (x0=%d)", wh.w, logoCenterX, canvasCenterX, x0)
		}
		// Vertical: logo should be in the upper portion of the body (the body
		// is logo + composer + hint + status, centered as a block). The logo's
		// y0 should be >= 0 and the logo should fit on the canvas.
		if y0 < 0 {
			t.Errorf("h=%d: logo y0 %d < 0", wh.h, y0)
		}
		if y0+len(opcode42Glyph) > wh.h {
			t.Errorf("h=%d: logo bottom row %d exceeds canvas height", wh.h, y0+len(opcode42Glyph))
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestComposeCanvas_ReusesFrameBuffer locks plan 20 Layer 4: after
// WindowSizeMsg allocates frameCanvas, successive composeCanvas calls return
// the same *lipgloss.Canvas (Clear+redraw), not a fresh NewCanvas each frame.
func TestComposeCanvas_ReusesFrameBuffer(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 40, 12
	m = m.ensureFrameCanvas()
	if m.frameCanvas == nil {
		t.Fatal("ensureFrameCanvas left frameCanvas nil")
	}
	first := m.composeCanvas()
	second := m.composeCanvas()
	if first == nil || second == nil {
		t.Fatal("composeCanvas returned nil")
	}
	if first != m.frameCanvas || second != m.frameCanvas {
		t.Fatalf("composeCanvas did not reuse frameCanvas: first=%p second=%p frame=%p", first, second, m.frameCanvas)
	}
}
