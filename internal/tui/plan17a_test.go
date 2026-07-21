package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestCanvas_FooterLayerPinned(t *testing.T) {
	m := longSessionModel(t)
	const wantMarker = "ctrl+p commands"
	footerYFor := func(off int) int {
		m.scroll.Offset = off
		canvas := m.composeCanvas()
		if canvas == nil {
			t.Fatal("composeCanvas returned nil")
		}
		for y := m.height - 1; y >= 0; y-- {
			row := canvasRowText(canvas, y, m.width)
			if strings.Contains(row, wantMarker) {
				return y
			}
		}
		return -1
	}
	y0 := footerYFor(0)
	y3 := footerYFor(3)
	yBig := footerYFor(1000)
	if y0 < 0 || y3 < 0 || yBig < 0 {
		t.Fatalf("footer marker %q not found: y0=%d y3=%d yBig=%d", wantMarker, y0, y3, yBig)
	}
	if y0 != y3 || y0 != yBig {
		t.Fatalf("footer Y moved across scroll: y0=%d y3=%d yBig=%d (want all equal)", y0, y3, yBig)
	}
}

func TestCanvas_SidebarLayerPinned(t *testing.T) {
	m := longSessionModel(t)
	m.width = 140
	leftW := m.leftColumnWidth()
	findSidebarY := func(canvas *lipgloss.Canvas) int {
		for y := 0; y < m.height; y++ {
			row := canvasRowText(canvas, y, m.width)
			if strings.Contains(row, "Opcode42") {
				return y
			}
		}
		return -1
	}
	m.scroll.Offset = 0
	canvas0 := m.composeCanvas()
	y0 := findSidebarY(canvas0)
	if y0 < 0 {
		t.Fatal("sidebar 'Opcode42' marker not found at offset 0")
	}
	for _, off := range []int{3, 1000} {
		m.scroll.Offset = off
		canvas := m.composeCanvas()
		row := canvasRowText(canvas, y0, m.width)
		if !strings.Contains(row, "Opcode42") {
			t.Fatalf("sidebar at scrollOffset=%d: 'Opcode42' not at y=%d (moved)", off, y0)
		}
		leftContent := canvasRowText(canvas, y0, leftW)
		if strings.Contains(leftContent, "Opcode42") {
			t.Fatalf("sidebar at scrollOffset=%d: 'Opcode42' leaked into the left column (x<%d)", off, leftW)
		}
	}
}

func TestKeyScroll_HomeEndJumpsToTopAndTail(t *testing.T) {
	m := longSessionModel(t)
	m.scroll.Offset = 0
	m, _ = step(t, m, key("home"))
	if m.scroll.Offset <= 0 {
		t.Fatalf("home should jump to top (Offset>0), got %d", m.scroll.Offset)
	}
	m, _ = step(t, m, key("end"))
	if m.scroll.Offset != 0 {
		t.Fatalf("end should jump to tail (Offset=0), got %d", m.scroll.Offset)
	}
	m, _ = step(t, m, key("ctrl+g"))
	if m.scroll.Offset <= 0 {
		t.Fatalf("ctrl+g should jump to top (Offset>0), got %d", m.scroll.Offset)
	}
	m, _ = step(t, m, key("ctrl+alt+g"))
	if m.scroll.Offset != 0 {
		t.Fatalf("ctrl+alt+g should jump to tail (Offset=0), got %d", m.scroll.Offset)
	}
}

func TestKeyScroll_HalfPageScroll(t *testing.T) {
	m := longSessionModel(t)
	bodyH := m.scrollBodyHeight()
	want := bodyH / 4
	if want < 1 {
		want = 1
	}
	m.scroll.Offset = 0
	m, _ = step(t, m, key("ctrl+alt+u"))
	if m.scroll.Offset != want {
		t.Fatalf("ctrl+alt+u (half-page up): Offset=%d want %d (bodyH=%d/4)", m.scroll.Offset, want, bodyH)
	}
	m, _ = step(t, m, key("ctrl+alt+d"))
	if m.scroll.Offset != 0 {
		t.Fatalf("ctrl+alt+d (half-page down): Offset=%d want 0", m.scroll.Offset)
	}
}

func TestFooterPanel_StreamStaysVisible(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Stream visible session"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_u1", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_u1"] = []Part{{ID: "pu1", MessageID: "msg_u1", Type: "text", Text: "STREAM VISIBLE ABOVE PANEL"}}
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls -la"}`),
		Tool:       []byte(`{"name":"bash"}`),
	}}
	m.permSel = 0

	if m.modalClassActive() {
		t.Fatal("modalClassActive should be false when only a footer panel is up (A4)")
	}
	if !m.footerPanelActive() {
		t.Fatal("footerPanelActive should be true when a permission is pending (A4)")
	}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "STREAM VISIBLE ABOVE PANEL") {
		t.Errorf("stream body should be visible above the footer panel (A4)\nout:\n%s", plain)
	}
	if !strings.Contains(plain, "Permission required") {
		t.Errorf("permission panel should also be visible\nout:\n%s", plain)
	}
}

func TestCenteredCardPos(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
		card   string
		wantX  int
		wantY  int
		ok     bool
	}{
		{"centered", 80, 24, "card", 38, 11, true},
		{"zero-width", 0, 24, "card", 0, 0, false},
		{"zero-height", 80, 0, "card", 0, 0, false},
		{"card-larger-than-canvas", 5, 5, "this is a very long card that exceeds width", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y, ok := centeredCardPos(c.width, c.height, c.card)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !c.ok {
				return
			}
			if x != c.wantX || y != c.wantY {
				t.Fatalf("pos = (%d,%d), want (%d,%d)", x, y, c.wantX, c.wantY)
			}
		})
	}
}

func TestBuildFooter_ConsistentAcrossPaths(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 140, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	leftW := m.leftColumnWidth()
	footer := m.buildFooter(leftW)
	if footer == "" {
		t.Fatal("buildFooter returned empty")
	}
	plain := stripANSI(footer)
	if !strings.Contains(plain, "ctrl+p commands") {
		t.Errorf("buildFooter should contain the status bar marker\n%s", plain)
	}
	if lipgloss.Height(footer) < 2 {
		t.Errorf("buildFooter should be at least 2 rows (composer + status), got %d", lipgloss.Height(footer))
	}
}
