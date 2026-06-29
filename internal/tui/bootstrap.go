package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// wireWithParts is the GET /session/:id/message item shape ({info, parts}).
type wireWithParts struct {
	Info  Message `json:"info"`
	Parts []Part  `json:"parts"`
}

// Bootstrap messages.
type (
	sessionsLoadedMsg struct {
		sessions []Session
		err      error
	}
	messagesLoadedMsg struct {
		sessionID string
		items     []wireWithParts
		err       error
	}
)

// loadSessionsCmd fetches the session list (newest-first).
func loadSessionsCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var sessions []Session
		err := c.GetJSON(ctx, "/session", &sessions)
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

// loadMessagesCmd fetches a session's message history with parts.
func loadMessagesCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		var items []wireWithParts
		err := c.GetJSON(ctx, "/session/"+sessionID+"/message", &items)
		return messagesLoadedMsg{sessionID: sessionID, items: items, err: err}
	}
}

// ingestHistory loads a session's persisted messages+parts into the store.
func (s store) ingestHistory(sessionID string, items []wireWithParts) store {
	for _, it := range items {
		s.messages[sessionID] = upsertByID(s.messages[sessionID], it.Info, func(m Message) string { return m.ID })
		for _, p := range it.Parts {
			mid := p.MessageID
			if mid == "" {
				mid = it.Info.ID
			}
			s.parts[mid] = upsertByID(s.parts[mid], p, func(pt Part) string { return pt.ID })
		}
	}
	return s
}
