package tui

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// contentWidth caps prose width for readability (the design's stream column).
const maxContentWidth = 100

// renderSession draws the conversation stream for the selected session: a title,
// the message blocks (user/assistant parts → user-turn/prose/thinking/tool-row),
// and the status line, scrolled to the newest content.
func (m Model) renderSession() string {
	s := m.styles

	// Optional right sidebar; the stream + composer take the remaining width.
	sidebar := ""
	leftW := m.leftColumnWidth()
	if m.sidebarVisible() {
		sidebar = m.sidebarView() // width == sidebarWidth (pinned by a test)
	}
	m.streamWidth = leftW // narrows the stream/composer wrap to the left column

	footer := m.composerView() + "\n" + m.statusBarView(leftW)
	if ac := m.autocompleteView(); ac != "" {
		footer = ac + "\n" + footer // popup sits just above the composer
	}
	if dock := m.tasksDockView(leftW); dock != "" {
		footer = dock + "\n" + footer // tasks dock above the composer area
	}
	if sf := m.subagentFooterView(leftW); sf != "" {
		footer = sf + "\n" + footer // sub-agent context strip (plan 08b §9)
	}

	sid := m.cfg.SessionID
	header := s.Section.Render(m.sessionTitle(sid))
	var blocks []string
	for _, msg := range m.store.messages[sid] {
		if b := m.renderMessage(msg, m.store.parts[msg.ID]); b != "" {
			blocks = append(blocks, b)
		}
	}
	body := header + "\n\n" + strings.Join(blocks, "\n\n")

	left := m.frame(body, footer)
	if sidebar == "" {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sidebar)
}

func (m Model) sessionTitle(sid string) string {
	for _, ss := range m.store.sessions {
		if ss.ID == sid && ss.Title != "" {
			return ss.Title
		}
	}
	return "session " + sid
}

// renderMessage renders one message's parts into stacked blocks.
func (m Model) renderMessage(msg Message, parts []Part) string {
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			txt := strings.TrimRight(p.Text, "\n")
			if txt == "" {
				continue
			}
			if msg.Role == "user" {
				out = append(out, m.userTurn(txt))
			} else {
				out = append(out, m.prose(txt))
			}
		case "reasoning":
			if m.view.hideThinking || strings.TrimSpace(p.Text) == "" {
				continue
			}
			out = append(out, m.thinking(p.Text))
		case "tool":
			if m.view.hideTools {
				continue
			}
			out = append(out, m.toolRow(p))
		}
	}
	// Surface an assistant turn's error (auth, overflow, rate limit, …) — never
	// swallow it; an errored turn often has no text parts at all.
	if msg.Error != nil {
		out = append(out, m.errorLine(msg.Error))
	}
	return strings.Join(out, "\n")
}

// errorLine renders an assistant error in red.
func (m Model) errorLine(e *MsgError) string {
	return lipgloss.NewStyle().Foreground(m.styles.P.Red).Width(m.contentWidth()).
		Render("⚠ " + e.Name + ": " + e.text())
}

// userTurn renders a user prompt with the design's blue left accent bar.
func (m Model) userTurn(text string) string {
	bar := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(m.styles.P.Blue).
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	return bar.Render(m.styles.Base.Render(text))
}

func (m Model) prose(text string) string {
	return lipgloss.NewStyle().Width(m.contentWidth()).Render(m.styles.Base.Render(text))
}

// thinking renders the design's "+ Thought" line (truncated to the column).
func (m Model) thinking(text string) string {
	head := "+ Thought "
	body := truncate(firstLine(text), m.contentWidth()-lipgloss.Width(head))
	return lipgloss.NewStyle().Foreground(m.styles.P.Amber).Render(head) + m.styles.Faint.Render(body)
}

// toolRow renders a terse tool one-liner colored by status (design tool grammar).
func (m Model) toolRow(p Part) string {
	s := m.styles
	var st struct {
		Status string `json:"status"`
		Title  string `json:"title"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(p.State, &st)

	glyph, gstyle := "•", lipgloss.NewStyle().Foreground(s.P.Amber)
	switch st.Status {
	case "completed":
		glyph, gstyle = "✓", lipgloss.NewStyle().Foreground(s.P.Green)
	case "error":
		glyph, gstyle = "✗", lipgloss.NewStyle().Foreground(s.P.Red)
	}
	label := s.Dim.Render(p.Tool)
	detail := st.Title
	if detail == "" {
		detail = st.Status
	}
	// Bound the row to the column: the prefix (glyph + tool) is short, so trim the
	// detail to whatever space is left.
	avail := m.contentWidth() - lipgloss.Width(glyph) - 1 - lipgloss.Width(p.Tool) - 1
	row := gstyle.Render(glyph) + " " + label + " " + s.Faint.Render(truncate(detail, avail))
	if st.Status == "error" && st.Error != "" {
		errLine := truncate(firstLine(st.Error), m.contentWidth()-2)
		row += "\n  " + lipgloss.NewStyle().Foreground(s.P.Red).Render(errLine)
	}
	return row
}

func (m Model) contentWidth() int {
	w := m.streamWidth // set when a sidebar narrows the stream column
	if w == 0 {
		w = m.width
	}
	if w == 0 || w > maxContentWidth {
		w = maxContentWidth
	}
	return w
}

// barWidth is the content width an accent-bar block (left ThickBorder) should
// use so the bar+content fit exactly in contentWidth — lipgloss renders the
// border outside the style's Width, so reserve its one column here.
func (m Model) barWidth() int {
	if w := m.contentWidth() - 1; w > 0 {
		return w
	}
	return 1
}

// frame trims body to the tail that fits the viewport (auto-scroll to newest)
// and pins the status line at the bottom.
// composerView renders the prompt input with the design's blue left accent bar.
func (m Model) composerView() string {
	accent := m.styles.P.Blue
	if m.shellMode {
		accent = m.styles.P.Red // shell mode: distinct accent so it's unmistakable
	}
	bar := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(accent).
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	view := bar.Render(m.input.View())
	if m.shellMode {
		label := lipgloss.NewStyle().Foreground(m.styles.P.Red).Render("! shell — enter run · esc cancel")
		return lipgloss.JoinVertical(lipgloss.Left, label, view)
	}
	return view
}

// statusLine is the bottom status: connection state plus the active model.
func (m Model) statusLine() string {
	return m.status + " · " + m.model.label()
}

// frame tail-scrolls body to the lines that fit above footer and pins footer to
// the bottom (padding a short body so the composer/status bar stay anchored).
func (m Model) frame(body, footer string) string {
	if m.height <= 0 {
		return body + "\n" + footer
	}
	avail := m.height - lipgloss.Height(footer)
	if avail < 1 {
		avail = 1
	}
	lines := strings.Split(body, "\n")
	if len(lines) > avail {
		// Window the body to `avail` lines, scrolled up from the bottom by
		// scrollOffset (clamped so we can't scroll past the top/bottom).
		maxOff := len(lines) - avail
		off := m.scrollOffset
		if off > maxOff {
			off = maxOff
		}
		if off < 0 {
			off = 0
		}
		end := len(lines) - off
		lines = lines[end-avail : end]
	} else {
		for len(lines) < avail { // pad so footer sits at the bottom
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n") + "\n" + footer
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// centerScreen places body in the middle of a width×height screen, returning it
// unplaced when either dimension is still zero (pre-first-resize). Shared by the
// full-screen overlays (modals, diff reviewer, prompts).
func centerScreen(width, height int, body string) string {
	if width == 0 || height == 0 {
		return body
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

// windowAround returns the [start,end) slice of count rows that fits height
// lines with sel kept roughly centered; the whole range when it already fits.
func windowAround(sel, count, height int) (int, int) {
	if count <= height {
		return 0, count
	}
	start := sel - height/2
	if start < 0 {
		start = 0
	}
	if hi := count - height; start > hi {
		start = hi
	}
	return start, start + height
}

// windowFrom returns the [start,end) slice of count rows starting at offset off
// (clamped so the last line can reach the bottom), fitting height lines — the
// top-anchored counterpart to windowAround, for scroll offsets.
func windowFrom(off, count, height int) (int, int) {
	maxOff := count - height
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	end := off + height
	if end > count {
		end = count
	}
	return off, end
}
