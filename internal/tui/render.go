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
	sid := m.cfg.SessionID

	header := s.Section.Render(m.sessionTitle(sid))

	var blocks []string
	for _, msg := range m.store.messages[sid] {
		if b := m.renderMessage(msg, m.store.parts[msg.ID]); b != "" {
			blocks = append(blocks, b)
		}
	}
	body := header + "\n\n" + strings.Join(blocks, "\n\n")
	return m.frame(body)
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
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			out = append(out, m.thinking(p.Text))
		case "tool":
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

// thinking renders the design's "+ Thought" line.
func (m Model) thinking(text string) string {
	head := lipgloss.NewStyle().Foreground(m.styles.P.Amber).Render("+ Thought ")
	return head + m.styles.Faint.Render(firstLine(text))
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
	row := gstyle.Render(glyph) + " " + label + " " + s.Faint.Render(detail)
	if st.Status == "error" && st.Error != "" {
		row += "\n  " + lipgloss.NewStyle().Foreground(s.P.Red).Render(firstLine(st.Error))
	}
	return row
}

func (m Model) contentWidth() int {
	w := m.width
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
	bar := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(m.styles.P.Blue).
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	return bar.Render(m.input.View())
}

// statusLine is the bottom status: connection state plus the active model.
func (m Model) statusLine() string {
	return m.status + " · " + m.model.label()
}

func (m Model) frame(body string) string {
	footer := m.composerView() + "\n" + m.styles.Faint.Render(m.statusLine())
	if m.height <= 0 {
		return body + "\n" + footer
	}
	avail := m.height - lipgloss.Height(footer)
	if avail < 1 {
		avail = 1
	}
	lines := strings.Split(body, "\n")
	if len(lines) > avail {
		lines = lines[len(lines)-avail:]
	}
	return strings.Join(lines, "\n") + "\n" + footer
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
