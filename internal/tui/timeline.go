package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Timeline + Status are the last two of the seven command modals (design
// modals.jsx). Timeline lists the session's user turns as revert checkpoints;
// Status is a read-only diagnostics panel.

// timelineItem is one user turn in the session (a revert target).
type timelineItem struct {
	messageID string
	title     string // first line of the prompt
}

// timelineItems are the open session's user turns, in chronological order.
func (m Model) timelineItems() []timelineItem {
	var out []timelineItem
	for _, msg := range m.store.messages[m.cfg.SessionID] {
		if msg.Role != "user" {
			continue
		}
		title := msg.ID
		for _, p := range m.store.parts[msg.ID] {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				title = firstLine(p.Text)
				break
			}
		}
		out = append(out, timelineItem{messageID: msg.ID, title: title})
	}
	return out
}

// revertedMsg is the result of a revert/unrevert.
type revertedMsg struct{ err error }

// revertCmd reverts the session to before the given user turn — that turn and
// every message after it are dropped (opencode's checkpoint mechanism); it is
// reversible via POST /session/:id/unrevert.
func revertCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID, messageID string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/session/"+sessionID+"/revert", map[string]string{"messageID": messageID}, nil)
		return revertedMsg{err: err}
	}
}

// unrevertCmd restores the last revert (POST /session/:id/unrevert) —
// messages_redo / plan 08f H1b.
func unrevertCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/session/"+sessionID+"/unrevert", map[string]any{}, nil)
		return revertedMsg{err: err}
	}
}

// undoLastTurn reverts the most recent user turn (messages_undo / <leader>u).
func (m Model) undoLastTurn() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "no session to undo"
		m = m.rerenderChrome()
		return m, nil
	}
	items := m.timelineItems()
	if len(items) == 0 {
		m.status = "nothing to undo"
		m = m.rerenderChrome()
		return m, nil
	}
	last := items[len(items)-1]
	m.status = "undoing…"
	m = m.rerenderChrome()
	return m, revertCmd(m.ctx, m.client, m.cfg.SessionID, last.messageID)
}

// redoTurn restores the last revert (messages_redo).
func (m Model) redoTurn() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "no session to redo"
		m = m.rerenderChrome()
		return m, nil
	}
	m.status = "redoing…"
	m = m.rerenderChrome()
	return m, unrevertCmd(m.ctx, m.client, m.cfg.SessionID)
}

// jumpLastUser scrolls the stream toward older content so the last user turn
// is more likely in view (messages_last_user). Without a retained message→line
// map this is approximate: jump halfway up from the tail when scrolled, else
// a large Back step.
func (m Model) jumpLastUser() Model {
	items := m.timelineItems()
	if len(items) == 0 {
		m.status = "no user turns"
		return m.rerenderChrome()
	}
	bodyH := m.scrollBodyHeight()
	if bodyH < 1 {
		bodyH = 1
	}
	// Prefer a large scroll-back so the latest user turn (near the end of the
	// stream, just above the trailing assistant reply) enters the viewport.
	m.scroll.Back(bodyH)
	m.status = "jumped to last user turn"
	return m.rerenderChrome()
}

// statusLines is the read-only diagnostics shown by the Status modal — all from
// local state, nothing fetched or fabricated.
func (m Model) statusLines() []string {
	conn := map[ConnState]string{
		Connecting: "connecting", Connected: "connected",
		Reconnecting: "reconnecting", ConnError: "error",
	}[m.conn]

	dir := m.cfg.Directory
	if ss := m.currentSession(); ss != nil && ss.Directory != "" {
		dir = ss.Directory
	}

	lines := []string{
		"daemon     " + m.cfg.URL,
		"state      " + conn,
		"directory  " + collapseHome(dir),
		"model      " + m.model.label(),
		"agent      " + m.modeName(),
		"theme      " + m.themeName,
		"events     " + humanInt(m.eventCount),
		"sessions   " + humanInt(len(m.store.sessions)),
	}
	if m.cfg.SessionID != "" {
		lines = append(lines, "session    "+m.cfg.SessionID)
	}
	return lines
}
