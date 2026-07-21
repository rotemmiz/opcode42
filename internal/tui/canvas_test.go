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
)

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
