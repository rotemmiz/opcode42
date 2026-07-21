package tui

import (
	"context"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hinshun/vt10x"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// PTY pane (plan 08b §2). An embedded terminal inside the TUI: a vt10x virtual
// screen fed by the PTY WebSocket (SDK ConnectPTY), rendered as a bottom split.
// When focused, keystrokes are forwarded to the shell as raw bytes; ctrl+]
// returns focus to the conversation.
//
// vt10x library choice (the plan's spike): hinshun/vt10x — a stable cell-grid
// emulator with a simple Cell(x,y)/Cursor()/Size() view, vs charmbracelet/x/vt
// which is an untagged pseudo-version carrying a "fix typecheck errors" warning.

// ptyMinRows / ptyMaxRows bound the terminal split height.
const (
	ptyMinRows  = 6
	ptyFraction = 2 // the pane takes up to 1/ptyFraction of the screen height
)

// ptyState is the embedded-terminal sub-model (zero value = closed).
type ptyState struct {
	open       bool
	focused    bool // keystrokes go to the shell
	connecting bool
	err        error
	id         string // pty id (POST /pty)
	conn       *opcode42client.PTYConn
	term       vt10x.Terminal // the virtual screen (shared pointer across Model copies)
	cols, rows int
	gen        int // monotonic open generation; stamps async msgs so stale ones drop
}

// PTY lifecycle messages. gen identifies the pane open they belong to, so a
// frame or close from a previous (closed) connection is ignored after reopen.
type (
	ptyConnectedMsg struct {
		gen  int
		id   string
		conn *opcode42client.PTYConn
		err  error
	}
	ptyOutputMsg struct {
		gen  int
		data []byte
	}
	ptyClosedMsg struct {
		gen int
		err error
	}
)

// openPTYCmd creates a pseudo-terminal in the session directory and dials its
// WebSocket (replaying from the start so the grid reflects full state).
func openPTYCmd(ctx context.Context, c *opcode42client.Opcode42Client, cwd string, cols, rows, gen int) tea.Cmd {
	return func() tea.Msg {
		info, err := c.CreatePTY(ctx, opcode42client.PTYCreate{Cwd: cwd})
		if err != nil {
			return ptyConnectedMsg{gen: gen, err: err}
		}
		_ = c.ResizePTY(ctx, info.ID, cols, rows) // best-effort initial size
		conn, err := c.ConnectPTY(ctx, info.ID, 0)
		return ptyConnectedMsg{gen: gen, id: info.ID, conn: conn, err: err}
	}
}

// ptyReadCmd waits for the next output chunk (re-issued after each, like the SSE
// listen loop). When the stream closes it surfaces the terminal error.
func ptyReadCmd(conn *opcode42client.PTYConn, gen int) tea.Cmd {
	return func() tea.Msg {
		b, ok := <-conn.Output()
		if !ok {
			var err error
			select {
			case err = <-conn.Err():
			default:
			}
			return ptyClosedMsg{gen: gen, err: err}
		}
		return ptyOutputMsg{gen: gen, data: b}
	}
}

// resizePTYCmd pushes a new size to the daemon (PUT /pty/{id}).
func resizePTYCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		_ = c.ResizePTY(ctx, id, cols, rows)
		return nil
	}
}

// ptyGridSize computes the terminal grid for the current layout (left column
// width, a fraction of the screen height, clamped).
func (m Model) ptyGridSize() (cols, rows int) {
	cols = m.leftColumnWidth()
	if cols <= 0 {
		cols = maxContentWidth
	}
	rows = m.height / ptyFraction
	if rows < ptyMinRows {
		rows = ptyMinRows
	}
	return cols, rows
}

// focusOrOpenPTY (leader `) opens the terminal pane focused on first use, or
// re-focuses an already-open pane. Closing is from the terminal side (ctrl+]).
func (m Model) focusOrOpenPTY() (tea.Model, tea.Cmd) {
	if m.pty.open {
		m.pty.focused = true
		return m, nil
	}
	cols, rows := m.ptyGridSize()
	m.ptyGen++ // a fresh generation; output from any prior pane is now stale
	m.pty = ptyState{
		open:       true,
		focused:    true,
		connecting: true,
		term:       vt10x.New(vt10x.WithSize(cols, rows)),
		cols:       cols,
		rows:       rows,
		gen:        m.ptyGen,
	}
	return m, openPTYCmd(m.ctx, m.client, m.cfg.Directory, cols, rows, m.ptyGen)
}

// resizePTY reflows the terminal grid to the current layout, returning a cmd to
// push the new size to the daemon (nil when the pane is closed or unchanged).
func (m *Model) resizePTY() tea.Cmd {
	if !m.pty.open || m.pty.term == nil {
		return nil
	}
	cols, rows := m.ptyGridSize()
	if cols == m.pty.cols && rows == m.pty.rows {
		return nil
	}
	m.pty.cols, m.pty.rows = cols, rows
	m.pty.term.Resize(cols, rows)
	if m.pty.id == "" {
		return nil // not connected yet; the initial create already sent this size
	}
	return resizePTYCmd(m.ctx, m.client, m.pty.id, cols, rows)
}

// closePTY tears down the connection and clears the pane.
func (m *Model) closePTY() {
	if m.pty.conn != nil {
		m.pty.conn.Close()
	}
	m.pty = ptyState{}
}

// handlePTYKey forwards a keystroke to the shell while the pane is focused.
// ctrl+] releases focus back to the conversation; ctrl+c is forwarded (so the
// shell can interrupt) rather than quitting the TUI.
func (m Model) handlePTYKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+]" {
		m.pty.focused = false
		m.status = "terminal unfocused — ctrl+x ` to refocus"
		return m, nil
	}
	if m.pty.conn == nil {
		return m, nil // still connecting; drop input
	}
	if b := keyToBytes(msg); len(b) > 0 {
		conn := m.pty.conn
		ctx := m.ctx
		return m, func() tea.Msg { _ = conn.Write(ctx, b); return nil }
	}
	return m, nil
}

// keyToBytes encodes a Bubble Tea key event as the raw bytes a terminal expects.
//
// bubbletea v2: a Key carries a Code rune (special keys are high PUA runes that
// never collide with printable ASCII), the printable Text, and a Mod bitmask.
// Special keys are matched first so Enter/Tab map to \r/\t regardless of Text;
// then printable characters (incl. space, the old KeyRunes/KeySpace cases) are
// forwarded verbatim; then ctrl+a..ctrl+z collapse to control bytes 0x01..0x1a.
func keyToBytes(msg tea.KeyMsg) []byte {
	k := msg.Key()
	switch k.Code {
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeyEsc:
		return []byte("\x1b")
	case tea.KeyBackspace:
		return []byte("\x7f")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	}
	// Printable characters (letters, digits, space, …) arrive as Text.
	if k.Text != "" {
		return []byte(k.Text)
	}
	// ctrl+a..ctrl+z → bytes 0x01..0x1a.
	if k.Mod&tea.ModCtrl != 0 && k.Code >= 'a' && k.Code <= 'z' {
		return []byte{byte(k.Code-'a') + 1}
	}
	return nil
}

// ptyPaneView renders the terminal grid as a bottom split (empty when closed).
func (m Model) ptyPaneView(width int) string {
	if !m.pty.open {
		return ""
	}
	s := m.styles
	title := "terminal"
	if m.pty.focused {
		title += " ⌨"
	} else {
		title += " (ctrl+x ` to focus)"
	}
	bar := lipgloss.NewStyle().Foreground(s.P.Purple).Render("▌ " + title)
	if m.pty.connecting {
		return lipgloss.JoinVertical(lipgloss.Left, bar, s.Faint.Render("  connecting…"))
	}
	if m.pty.err != nil {
		return lipgloss.JoinVertical(lipgloss.Left, bar, lipgloss.NewStyle().Foreground(s.P.Red).Render("  "+m.pty.err.Error()))
	}
	return lipgloss.JoinVertical(lipgloss.Left, bar, m.renderGrid(width))
}

// vt10x glyph-mode attribute bits. These mirror the (unexported) attr* constants
// in hinshun/vt10x state.go:13-21 — the emulator stores SGR attributes in the
// public Glyph.Mode int16 using this fixed bit layout, so decoding it here is how
// the render path recovers bold/underline/italic/reverse. attrGfx/attrBlink/
// attrWrap are not visually rendered (line-graphics glyphs already arrive as the
// right runes; blink/wrap have no Lipgloss analogue).
const (
	vtAttrReverse   = 1 << 0 // attrReverse
	vtAttrUnderline = 1 << 1 // attrUnderline
	vtAttrBold      = 1 << 2 // attrBold
	vtAttrItalic    = 1 << 4 // attrItalic
)

// cellAttrs is the visual styling of one terminal cell: resolved colors (after
// any reverse swap) plus the bold/underline/italic flags. Two adjacent cells
// share a style run iff their cellAttrs are equal, so it is the run-batching key.
type cellAttrs struct {
	fg, bg                  theme.Color
	colorOK                 bool
	bold, underline, italic bool
}

// styleCell builds a lipgloss style for a cell run (no-op when the run carries no
// color and no attributes).
func (a cellAttrs) styleCell() lipgloss.Style {
	st := lipgloss.NewStyle()
	if a.colorOK {
		if a.fg != "" {
			st = st.Foreground(a.fg)
		}
		if a.bg != "" {
			st = st.Background(a.bg)
		}
	}
	if a.bold {
		st = st.Bold(true)
	}
	if a.underline {
		st = st.Underline(true)
	}
	if a.italic {
		st = st.Italic(true)
	}
	return st
}

// renderGrid snapshots the vt10x screen into styled lines, batching runs of
// same-styled cells (color + bold/underline/italic). The cursor cell is shown
// reversed when the pane is focused; an SGR-reverse cell swaps fg/bg too.
func (m Model) renderGrid(width int) string {
	t := m.pty.term
	if t == nil {
		return ""
	}
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	if width > 0 && cols > width {
		cols = width
	}
	cur := t.Cursor()
	curVis := t.CursorVisible() && m.pty.focused

	var b strings.Builder
	for y := 0; y < rows; y++ {
		var run strings.Builder
		var runAttrs cellAttrs
		flush := func() {
			if run.Len() == 0 {
				return
			}
			b.WriteString(runAttrs.styleCell().Render(run.String()))
			run.Reset()
		}
		for x := 0; x < cols; x++ {
			g := t.Cell(x, y)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			fg, fgOK := vtColor(g.FG)
			bg, bgOK := vtColor(g.BG)
			// An SGR-reverse cell or the focused cursor cell swaps fg/bg; when both
			// apply they cancel out (a reversed cell under the cursor reads normal).
			reverse := g.Mode&vtAttrReverse != 0
			if y == cur.Y && x == cur.X && curVis {
				reverse = !reverse
			}
			if reverse {
				fg, bg, fgOK, bgOK = bg, fg, bgOK, fgOK
				if !bgOK { // ensure a visible block even on default bg
					bg, bgOK = m.styles.P.Fg, true
				}
			}
			attrs := cellAttrs{
				fg: fg, bg: bg, colorOK: fgOK || bgOK,
				bold:      g.Mode&vtAttrBold != 0,
				underline: g.Mode&vtAttrUnderline != 0,
				italic:    g.Mode&vtAttrItalic != 0,
			}
			if run.Len() > 0 && attrs != runAttrs {
				flush()
			}
			runAttrs = attrs
			run.WriteRune(ch)
		}
		flush()
		if y < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// vtPalette pre-renders the 256 palette indices to lipgloss colors so the hot
// render path doesn't strconv.Itoa per cell.
var vtPalette = func() [256]theme.Color {
	var p [256]theme.Color
	for i := range p {
		p[i] = theme.Color(strconv.Itoa(i))
	}
	return p
}()

// vtColor maps a vt10x color to a lipgloss color; ok is false for the terminal
// default (so the cell inherits the surrounding theme). vt10x emits palette
// indices [0,256) plus the Default* sentinels (1<<24+, no truecolor).
func vtColor(c vt10x.Color) (theme.Color, bool) {
	if c < 256 {
		return vtPalette[c], true
	}
	return "", false // DefaultFG / DefaultBG / DefaultCursor and anything ≥256
}
