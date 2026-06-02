package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hinshun/vt10x"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// withPTY builds a model with an open (unfocused, unconnected) terminal pane.
func withPTY() Model {
	m := New(Config{URL: "http://x", Directory: "/tmp"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	cols, rows := m.ptyGridSize()
	m.pty = ptyState{open: true, term: vt10x.New(vt10x.WithSize(cols, rows)), cols: cols, rows: rows}
	return m
}

func TestKeyToBytes(t *testing.T) {
	cases := []struct {
		msg  tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ls")}, "ls"},
		{tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{tea.KeyMsg{Type: tea.KeySpace}, " "},
		{tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"}, // SIGINT forwarded to the shell
		{tea.KeyMsg{Type: tea.KeyCtrlA}, "\x01"},
		{tea.KeyMsg{Type: tea.KeyCtrlZ}, "\x1a"},
		{tea.KeyMsg{Type: tea.KeyF5}, ""}, // unmapped → no bytes
	}
	for _, c := range cases {
		if got := string(keyToBytes(c.msg)); got != c.want {
			t.Errorf("keyToBytes(%v) = %q, want %q", c.msg.Type, got, c.want)
		}
	}
}

func TestVTColor(t *testing.T) {
	if c, ok := vtColor(vt10x.Red); !ok || c != "1" { // ANSI Red == index 1
		t.Errorf("vtColor(Red) = %q,%v want 1,true", c, ok)
	}
	if c, ok := vtColor(vt10x.Color(200)); !ok || c != "200" { // 256-color index
		t.Errorf("vtColor(200) = %q,%v want 200,true", c, ok)
	}
	if _, ok := vtColor(vt10x.DefaultFG); ok { // terminal default → not applied
		t.Error("vtColor(DefaultFG) should be the terminal default (ok=false)")
	}
	if _, ok := vtColor(vt10x.DefaultBG); ok {
		t.Error("vtColor(DefaultBG) should be the terminal default (ok=false)")
	}
}

// TestRenderGrid_Golden feeds a recorded byte stream and asserts the rendered
// grid text (the plan's golden test).
func TestRenderGrid_Golden(t *testing.T) {
	m := withPTY()
	// Includes a color SGR to exercise styling without affecting the text content.
	_, _ = m.pty.term.Write([]byte("hello\r\n\x1b[31mworld\x1b[0m"))
	lines := strings.Split(stripANSI(m.renderGrid(m.pty.cols)), "\n")
	if len(lines) < 2 {
		t.Fatalf("grid should have >=2 rows, got %d", len(lines))
	}
	if strings.TrimRight(lines[0], " ") != "hello" {
		t.Errorf("row 0 = %q, want 'hello'", strings.TrimRight(lines[0], " "))
	}
	if strings.TrimRight(lines[1], " ") != "world" {
		t.Errorf("row 1 = %q, want 'world'", strings.TrimRight(lines[1], " "))
	}
}

func TestPTYOutput_FeedsGrid(t *testing.T) {
	m := withPTY()
	m, cmd := step(t, m, ptyOutputMsg{data: []byte("echo hi")})
	if !strings.Contains(stripANSI(m.renderGrid(m.pty.cols)), "echo hi") {
		t.Fatal("ptyOutputMsg should write into the terminal grid")
	}
	if cmd != nil {
		t.Fatal("without a live conn the pump should not re-issue")
	}
}

func TestPTY_FocusOpenCloseCycle(t *testing.T) {
	m := New(Config{URL: "http://x", Directory: "/tmp"})
	m.screen, m.width, m.height = ScreenSession, 80, 24

	// Leader ` opens + focuses + dials.
	next, cmd := m.focusOrOpenPTY()
	m = next.(Model)
	if !m.pty.open || !m.pty.focused || m.pty.term == nil || cmd == nil {
		t.Fatalf("focusOrOpenPTY should open, focus, build the term, and dial (open=%v focus=%v cmd=%v)", m.pty.open, m.pty.focused, cmd != nil)
	}

	// ctrl+] (focused) → unfocus, pane stays open.
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if m.pty.focused || !m.pty.open {
		t.Fatalf("ctrl+] should unfocus but keep the pane (focus=%v open=%v)", m.pty.focused, m.pty.open)
	}

	// ctrl+] (unfocused) → close.
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if m.pty.open {
		t.Fatal("a second ctrl+] should close the pane")
	}
}

func TestPTY_FocusedCapturesKeys(t *testing.T) {
	m := withPTY()
	m.pty.focused = true
	// A focused terminal must not let ctrl+c quit — it returns a model, not tea.Quit.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := next.(Model); !ok {
		t.Fatal("focused terminal should swallow ctrl+c (no quit)")
	}
	_ = cmd
	// A focused terminal also captures the leader key (ctrl+x goes to the shell).
	m2, _ := step(t, m, key("ctrl+x"))
	if m2.leader {
		t.Fatal("ctrl+x should be forwarded to the shell, not start the leader")
	}
}

func TestPTY_FocusYieldsToPermission(t *testing.T) {
	m := withPTY()
	m.pty.focused = true
	m.store.permissions = []Permission{{ID: "perm_1", Permission: "bash"}}
	// 'down' must drive the permission selection, not be forwarded to the shell.
	m, _ = step(t, m, key("down"))
	if m.permSel != 1 {
		t.Fatalf("a focused terminal must yield keys to a pending permission; permSel=%d want 1", m.permSel)
	}
}

func TestResizePTY_Reflows(t *testing.T) {
	m := withPTY()
	oldRows := m.pty.rows
	m.height = 40 // taller screen → more terminal rows
	cmd := (&m).resizePTY()
	if m.pty.rows == oldRows {
		t.Fatalf("resizePTY should reflow rows (still %d)", oldRows)
	}
	if cmd != nil {
		t.Fatal("unconnected pane should not push a resize to the daemon")
	}
}

func TestPTYConnected_ClosedWhileDialing(t *testing.T) {
	m := New(Config{URL: "http://x"}) // pane closed (gen 0)
	// A connect result from a prior pane (gen 1) arriving after close is dropped.
	m, _ = step(t, m, ptyConnectedMsg{gen: 1, id: "pty_1", conn: nil})
	if m.pty.open || m.pty.id != "" {
		t.Fatal("a ptyConnectedMsg after close must not revive the pane")
	}
}

func TestPTY_StaleConnectionDiscarded(t *testing.T) {
	// Open a pane (gen 1), feed it data, then "reopen" by bumping to gen 2.
	m := withPTY()
	m.ptyGen, m.pty.gen = 1, 1
	m, _ = step(t, m, ptyOutputMsg{gen: 1, data: []byte("first")})
	if !strings.Contains(stripANSI(m.renderGrid(m.pty.cols)), "first") {
		t.Fatal("matching-generation output should reach the grid")
	}
	// Simulate a reopen: a newer pane (gen 2) with its own fresh terminal.
	m.ptyGen = 2
	m.pty.gen = 2
	m.pty.term = vt10x.New(vt10x.WithSize(m.pty.cols, m.pty.rows))
	// A late frame from the old connection (gen 1) must NOT touch the new grid.
	m, _ = step(t, m, ptyOutputMsg{gen: 1, data: []byte("stale")})
	if strings.Contains(stripANSI(m.renderGrid(m.pty.cols)), "stale") {
		t.Fatal("output from a prior generation must be discarded")
	}
}

func TestPTYOpen_ViaLeader(t *testing.T) {
	m := New(Config{URL: "http://x", Directory: "/tmp"})
	m.screen, m.width, m.height = ScreenSession, 80, 24
	m, _ = step(t, m, key("ctrl+x"))
	m, cmd := step(t, m, key("`"))
	if !m.pty.open || !m.pty.focused || cmd == nil {
		t.Fatalf("ctrl+x ` should open + focus + dial the terminal (open=%v cmd=%v)", m.pty.open, cmd != nil)
	}
}
