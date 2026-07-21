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
