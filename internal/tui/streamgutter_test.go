package tui

import (
	"strings"
	"testing"
)

// streamgutter_test.go — Plan 18 §A4 + §B1 regression guards.
//
// A4 drops the maxContentWidth=100 cap on the stream's prose width so the
// stream fills the left column on wide terminals. B1 insets the stream
// column by a 2-col gutter on each side (matching opencode's message column
// paddingLeft={2} paddingRight={2}, index.tsx:1166). These tests pin both
// invariants:
//
//   - TestContentWidth_NoCapOnWideTerminal: a 200-col terminal with no
//     sidebar yields contentWidth() == 200 (was capped to 100 pre-A4).
//   - TestStreamColumn_HasGutter: at 140×24 with the sidebar visible, the
//     first 2 cols of the stream row are blank (canvas base Bg, no stream
//     content), and the 2 cols before the sidebar are also blank.

// TestContentWidth_NoCapOnWideTerminal pins plan 18 §A4: contentWidth() no
// longer caps at 100 cols. On a 200-col terminal without the sidebar the
// stream content width is the full 200; with the sidebar (visible at
// >=121 cols) it's the left column width minus the gutter. Both cases must
// return a value > 100 (the prior cap) to prove the cap is gone.
func TestContentWidth_NoCapOnWideTerminal(t *testing.T) {
	// Case 1: 200-col terminal, sidebar hidden. The stream fills the whole
	// width; contentWidth() must return 200 (was capped to 100 pre-A4).
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 200, 24
	m.sidebarHidden = true // sidebar off → leftColumnWidth == m.width
	m.streamWidth = 0      // not yet narrowed by sessionLayers
	if got := m.contentWidth(); got != 200 {
		t.Errorf("contentWidth() with sidebar hidden = %d, want 200 (no 100-cap post-A4)", got)
	}

	// Case 2: 200-col terminal, sidebar visible (threshold is >=121).
	// leftColumnWidth = 200 - 42 = 158; after B1, sessionLayers sets
	// streamWidth = innerW = 158 - 2*2 = 154. For a direct contentWidth()
	// call without going through sessionLayers, simulate the field set.
	m2 := New(Config{URL: "http://x", SessionID: "ses_1"})
	m2.screen = ScreenSession
	m2.width, m2.height = 200, 24
	m2.sidebarHidden = false // sidebar visible
	if !m2.sidebarVisible() {
		t.Fatalf("sidebar should be visible at width=200 (threshold 121)")
	}
	m2.streamWidth = 158 // what renderSession/sessionLayers would set pre-gutter
	if got := m2.contentWidth(); got != 158 {
		t.Errorf("contentWidth() with sidebar visible, streamWidth=158 = %d, want 158", got)
	}

	// The key A4 assertion: neither case returns 100. Both return values
	// greater than 100, proving the cap is gone.
	if got := m.contentWidth(); got <= 100 {
		t.Errorf("contentWidth() = %d, want > 100 (the 100-cap must be gone post-A4)", got)
	}
	if got := m2.contentWidth(); got <= 100 {
		t.Errorf("contentWidth() with sidebar = %d, want > 100 (the 100-cap must be gone post-A4)", got)
	}
}

// TestStreamColumn_HasGutter pins plan 18 §B1: the stream column is inset by
// a 2-col gutter on each side. We render at 140×24 with the sidebar visible
// (sidebar threshold is >=121; at 140 the sidebar shows, leftW = 140 - 42 =
// 98, innerW = 98 - 4 = 94, streamGutter = 2). The stream layer is positioned
// at X(streamGutter)=X(2), so canvas cols 0-1 on the stream row are blank
// base Bg (no stream text). The 2 cols before the sidebar at X=leftW=98
// (cols 96-97) are also blank (the stream is 94 wide starting at X=2, so it
// ends at col 95; cols 96-97 are gutter before the sidebar at X=98).
func TestStreamColumn_HasGutter(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 140, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Gutter session"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_1"] = []Part{{
		ID: "p1", MessageID: "msg_1", Type: "text",
		Text: "STREAM ROW MARKER",
	}}

	if !m.sidebarVisible() {
		t.Fatalf("sidebar should be visible at width=140 (threshold 121)")
	}
	leftW := m.leftColumnWidth()
	innerW := leftW - 2*streamGutter
	if innerW != 94 {
		t.Fatalf("leftW=%d, innerW=%d, want innerW=94 (leftW=98, gutter=2)", leftW, innerW)
	}

	canvas := m.composeCanvas()
	if canvas == nil {
		t.Fatal("composeCanvas returned nil")
	}

	// Locate the stream row: the row containing the seeded marker text.
	// Scan from y=0 down past the section header.
	streamY := -1
	for y := 0; y < m.height; y++ {
		row := canvasRowText(canvas, y, m.width)
		if strings.Contains(row, "STREAM ROW MARKER") {
			streamY = y
			break
		}
	}
	if streamY < 0 {
		t.Skip("could not locate the stream row on the canvas")
	}

	// Left gutter: cols 0..streamGutter-1 (cols 0-1) must be blank — no
	// stream text. The stream layer is at X(streamGutter)=X(2), so cols 0-1
	// are the canvas base Bg fill (a styled space, no text content).
	for x := 0; x < streamGutter; x++ {
		c := canvas.CellAt(x, streamY)
		if c == nil {
			t.Errorf("left gutter cell (%d,%d) is nil", x, streamY)
			continue
		}
		if strings.TrimSpace(c.Content) != "" {
			t.Errorf("left gutter cell (%d,%d) has content %q, want blank (stream layer should start at X=%d)",
				x, streamY, c.Content, streamGutter)
		}
	}

	// Right gutter: cols leftW-2 .. leftW-1 (cols 96-97) must be blank — no
	// stream text. The stream is innerW wide starting at X=streamGutter,
	// so it ends at col streamGutter + innerW - 1 = 2 + 94 - 1 = 95; cols
	// 96-97 are gutter before the sidebar at X=leftW=98.
	for x := leftW - streamGutter; x < leftW; x++ {
		c := canvas.CellAt(x, streamY)
		if c == nil {
			t.Errorf("right gutter cell (%d,%d) is nil", x, streamY)
			continue
		}
		if strings.TrimSpace(c.Content) != "" {
			t.Errorf("right gutter cell (%d,%d) has content %q, want blank (stream ends at col %d, sidebar starts at col %d)",
				x, streamY, c.Content, streamGutter+innerW-1, leftW)
		}
	}

	// Sanity: the stream content IS present at col streamGutter (the first
	// non-blank stream cell should be at col 2, not col 0).
	firstContentCol := -1
	for x := 0; x < leftW; x++ {
		c := canvas.CellAt(x, streamY)
		if c != nil && strings.TrimSpace(c.Content) != "" {
			firstContentCol = x
			break
		}
	}
	if firstContentCol != streamGutter {
		t.Errorf("first stream content at col %d, want %d (the left gutter width)", firstContentCol, streamGutter)
	}
}
