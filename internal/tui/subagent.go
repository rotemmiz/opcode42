package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Sub-agent navigation (plan 08b §9). Sub-agent runs are child sessions: a
// session with a ParentID. opencode surfaces them with a footer (label + "i of
// n") and parent/prev/next navigation; this mirrors that over the same wire data
// (the store already mirrors every session via SSE, and GET /session/{id}/children
// keeps the parent's child set fresh when we descend into it).

// childrenLoadedMsg carries a session's children (GET /session/{id}/children).
type childrenLoadedMsg struct {
	children []Session
	err      error
}

// loadChildrenCmd fetches a session's sub-agent children. Exercises the frozen
// GET /session/{id}/children endpoint and tops up any child the store missed.
func loadChildrenCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		var children []Session
		err := c.GetJSON(ctx, "/session/"+sessionID+"/children", &children)
		return childrenLoadedMsg{children: children, err: err}
	}
}

// childrenOf returns the sub-agent child sessions of sid, in store order
// (ascending id == chronological).
func (m Model) childrenOf(sid string) []Session {
	if sid == "" {
		return nil
	}
	var out []Session
	for _, s := range m.store.sessions {
		if s.ParentID == sid {
			out = append(out, s)
		}
	}
	return out
}

// indexOfSession returns the position of id in ss, or -1.
func indexOfSession(ss []Session, id string) int {
	for i := range ss {
		if ss[i].ID == id {
			return i
		}
	}
	return -1
}

// subagentTitleRe extracts the agent name from an opencode sub-agent title
// ("@review subagent: …" → "review"); other titles fall back to "Subagent".
var subagentTitleRe = regexp.MustCompile(`@(\w+) subagent`)

// subagentLabel is the display name for a child session (the spawning agent's
// name when the title encodes it, else "Subagent").
func subagentLabel(s Session) string {
	if mm := subagentTitleRe.FindStringSubmatch(s.Title); mm != nil {
		return titlecase(mm[1])
	}
	return "Subagent"
}

func titlecase(s string) string {
	if s == "" {
		return s
	}
	b := []rune(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

// openSession switches the open session to id (loading its stream + children).
// A no-op when id is empty or already current.
func (m Model) openSession(id string) (Model, tea.Cmd) {
	if id == "" || id == m.cfg.SessionID {
		return m, nil
	}
	m.cfg.SessionID = id
	m.screen = ScreenSession
	m.scrollOffset = 0 // snap to the live tail of the new stream
	// loadMessagesCmd's completion also fetches this session's children, so the
	// sub-agent footer is fresh without a second call here.
	return m, loadMessagesCmd(m.ctx, m.client, id)
}

// enterFirstChild descends into the current session's first sub-agent child.
func (m Model) enterFirstChild() (tea.Model, tea.Cmd) {
	kids := m.childrenOf(m.cfg.SessionID)
	if len(kids) == 0 {
		m.status = "no sub-agents in this session"
		return m, nil
	}
	nm, cmd := m.openSession(kids[0].ID)
	return nm, cmd
}

// gotoParent returns from a sub-agent child to its parent session.
func (m Model) gotoParent() (tea.Model, tea.Cmd) {
	cur := m.currentSession()
	if cur == nil || cur.ParentID == "" {
		m.status = "not in a sub-agent session"
		return m, nil
	}
	nm, cmd := m.openSession(cur.ParentID)
	return nm, cmd
}

// cycleSibling moves between sibling sub-agents of the current child session
// (dir +1 = next, -1 = previous), wrapping around.
func (m Model) cycleSibling(dir int) (tea.Model, tea.Cmd) {
	cur := m.currentSession()
	if cur == nil || cur.ParentID == "" {
		m.status = "not in a sub-agent session"
		return m, nil
	}
	sib := m.childrenOf(cur.ParentID)
	if len(sib) < 2 {
		return m, nil
	}
	i := indexOfSession(sib, cur.ID)
	if i < 0 {
		return m, nil
	}
	next := ((i+dir)%len(sib) + len(sib)) % len(sib)
	nm, cmd := m.openSession(sib[next].ID)
	return nm, cmd
}

// subagentFooterView renders the sub-agent context strip above the composer:
// when in a child session, its label + position among siblings + nav hints;
// when in a parent that spawned sub-agents, an invitation to descend. Empty
// otherwise.
func (m Model) subagentFooterView(width int) string {
	cur := m.currentSession()
	if cur == nil {
		return ""
	}
	switch {
	case cur.ParentID != "":
		sib := m.childrenOf(cur.ParentID)
		info := subagentLabel(*cur)
		if n := len(sib); n > 0 {
			info += fmt.Sprintf(" (%d of %d)", indexOfSession(sib, cur.ID)+1, n)
		}
		hint := "⌃x↑ parent"
		if len(sib) > 1 {
			hint += " · ⌃x[ prev · ⌃x] next"
		}
		return m.subagentBar(width, info, hint)
	case len(m.childrenOf(cur.ID)) > 0:
		n := len(m.childrenOf(cur.ID))
		info := fmt.Sprintf("%d sub-agent", n)
		if n != 1 {
			info += "s"
		}
		return m.subagentBar(width, info, "⌃x↓ enter")
	default:
		return ""
	}
}

// subagentBar draws a single accent-barred strip: a purple label on the left,
// faint key hints on the right, bounded to width.
func (m Model) subagentBar(width int, info, hint string) string {
	s := m.styles
	if width <= 0 {
		width = maxContentWidth
	}
	label := lipgloss.NewStyle().Foreground(s.P.Purple).Bold(true).Render("⦿ " + info)
	keys := s.Faint.Render(hint)
	gap := width - lipgloss.Width(label) - lipgloss.Width(keys)
	if gap < 1 {
		return lipgloss.NewStyle().Width(width).Render(label)
	}
	return label + strings.Repeat(" ", gap) + keys
}
