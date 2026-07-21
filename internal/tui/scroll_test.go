package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// longSessionModel builds a session whose transcript is far taller than the
// viewport, with the right sidebar visible, so we can exercise scrolling and the
// pinned footer/sidebar invariants (plan 08c bug-fix: no terminal overflow).
func longSessionModel(t *testing.T) Model {
	t.Helper()
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 20 // small viewport → content must scroll
	m.store.sessions = []Session{{ID: "ses_1", Title: "Long session"}}
	msgs := make([]Message, 0, 40)
	for i := 0; i < 40; i++ {
		id := "msg_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, Message{ID: id, SessionID: "ses_1", Role: role})
		m.store.parts[id] = []Part{{ID: "p" + id, MessageID: id, Type: "text",
			Text: "line content number " + id + " with enough text to be a distinct row"}}
	}
	m.store.messages["ses_1"] = msgs
	return m
}

// lineWidths returns the visible (ANSI-stripped) width of each row of a frame.
func frameRows(s string) []string { return strings.Split(s, "\n") }

// TestView_NeverExceedsViewport guards bugs 2 & 3: the composed frame must be
// exactly m.height rows, each exactly m.width visible columns — otherwise the line
// wraps / the frame overflows and the terminal scrolls the whole UI (footer +
// sidebar) instead of the in-app viewport.
func TestView_NeverExceedsViewport(t *testing.T) {
	m := longSessionModel(t)
	for _, off := range []int{0, 3, 9, 1000} {
		m.scroll.Offset = off
		rows := frameRows(m.renderView())
		if len(rows) != m.height {
			t.Fatalf("scrollOffset=%d: got %d rows, want exactly %d (overflow → terminal scrolls everything)", off, len(rows), m.height)
		}
		for i, r := range rows {
			if w := ansi.StringWidth(r); w != m.width {
				t.Fatalf("scrollOffset=%d row %d: visible width %d, want %d (a too-wide row wraps and overflows)", off, i, w, m.width)
			}
		}
	}
}

// TestView_FooterPinnedAcrossScroll guards bug 3: the status bar stays on the
// bottom row regardless of scroll position (it must not scroll with the stream).
func TestView_FooterPinnedAcrossScroll(t *testing.T) {
	m := longSessionModel(t)
	// The status bar always ends with the command hint; assert on that stable
	// token rather than spacing that shifts with the column's inner width.
	const wantMarker = "ctrl+p commands"
	bottomFor := func(off int) string {
		m.scroll.Offset = off
		rows := frameRows(m.renderView())
		// Search the last 3 rows (composer + status bar live at the very bottom).
		return stripANSI(strings.Join(rows[len(rows)-3:], "\n"))
	}
	b0, bN := bottomFor(0), bottomFor(9)
	if !strings.Contains(b0, wantMarker) || !strings.Contains(bN, wantMarker) {
		t.Fatalf("status bar not pinned at bottom across scroll: marker %q\n off=0 bottom:\n%s\n off=9 bottom:\n%s", wantMarker, b0, bN)
	}
}

// TestView_ScrollChangesStreamContent guards bug 2: scrolling back actually shows
// earlier transcript rows that the live-tail view does not.
func TestView_ScrollChangesStreamContent(t *testing.T) {
	m := longSessionModel(t)
	m.scroll.Offset = 0
	tail := stripANSI(m.renderView())
	m.scroll.Offset = 1000 // large offset → clamped to the top of the transcript
	back := stripANSI(m.renderView())
	if tail == back {
		t.Fatal("scrolling changed nothing — stream is not scrollable")
	}
	// The earliest message is visible only when scrolled to the top, not at the tail.
	early := "msg_a0"
	if strings.Contains(tail, early) {
		t.Skip("transcript shorter than expected; tail already shows the top")
	}
	if !strings.Contains(back, early) {
		t.Errorf("scrolled-to-top view does not reveal the earliest row %q", early)
	}
}

// TestKeyScroll_PageAndCtrlArrowsScroll verifies the stream scrolls on keys the
// composer never consumes (pgup/pgdn, ctrl+↑/↓), and that ↓ can't pass the tail.
func TestKeyScroll_PageAndCtrlArrowsScroll(t *testing.T) {
	m := longSessionModel(t)
	m, _ = step(t, m, key("pgup"))
	m, _ = step(t, m, key("ctrl+up"))
	if m.scroll.Offset != 2*scrollStep {
		t.Fatalf("pgup+ctrl+up: Offset=%d want %d", m.scroll.Offset, 2*scrollStep)
	}
	m, _ = step(t, m, key("pgdown"))
	if m.scroll.Offset != scrollStep {
		t.Fatalf("pgdown: Offset=%d want %d", m.scroll.Offset, scrollStep)
	}
	m, _ = step(t, m, key("ctrl+down"))
	m, _ = step(t, m, key("ctrl+down"))
	if m.scroll.Offset != 0 || !m.scroll.AtTail() {
		t.Fatalf("ctrl+down past tail: Offset=%d want 0", m.scroll.Offset)
	}
}

// TestKeyScroll_PlainArrowsAreInputBoxOnly pins the constraint that the input box
// is untouched: plain ↑/↓ drive history recall / the composer, never the stream
// scroll.
func TestKeyScroll_PlainArrowsAreInputBoxOnly(t *testing.T) {
	m := longSessionModel(t)
	m.history = []string{"earlier prompt"}
	m.histIdx = -1
	m, _ = step(t, m, key("up"))
	if m.scroll.Offset != 0 {
		t.Fatalf("plain ↑ scrolled the stream: Offset=%d want 0 (input box only)", m.scroll.Offset)
	}
	if m.input.Value() != "earlier prompt" {
		t.Fatalf("plain ↑ did not recall history: got %q", m.input.Value())
	}
}
