package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

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
// When the session's aggregated Tokens are still zero (a freshly-switched
// session whose session.updated SSE hasn't reported tokens yet — the
// sidebar's context gauge would otherwise blank until a new turn arrives),
// it backfills the session's Tokens from the last assistant message's
// tokens. This mirrors opencode's sidebar context plugin, which reads the
// last assistant message's tokens directly (feature-plugins/sidebar/context.tsx).
// Plan 08e §E5.
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
	// E5: backfill the session's aggregated tokens from the last assistant
	// message when the session row carries none yet. The session's tokens
	// arrive via session.updated SSE; on a switch we load history before that
	// event fires for this session, so the gauge would otherwise read zero.
	if sess := s.sessionByID(sessionID); sess != nil && sess.Tokens.Total() == 0 {
		if last := lastAssistantMessageTokens(s.messages[sessionID]); last != nil {
			for i := range s.sessions {
				if s.sessions[i].ID == sessionID {
					s.sessions[i].Tokens.Input = int(last.Input)
					s.sessions[i].Tokens.Output = int(last.Output)
					s.sessions[i].Tokens.Cache.Read = int(last.Cache.Read)
					s.sessions[i].Tokens.Cache.Write = int(last.Cache.Write)
					break
				}
			}
		}
	}
	return s
}

// replaceHistory clears the session's messages/parts then ingests the wire
// payload. Used by messagesLoadedMsg so a post-revert reload drops turns the
// daemon no longer returns (upsert alone would leave stale entries).
func (s store) replaceHistory(sessionID string, items []wireWithParts) store {
	for _, msg := range s.messages[sessionID] {
		delete(s.parts, msg.ID)
	}
	s.messages[sessionID] = nil
	return s.ingestHistory(sessionID, items)
}

// sessionByID returns the session with the given id, or nil when absent.
func (s store) sessionByID(id string) *Session {
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			return &s.sessions[i]
		}
	}
	return nil
}

// lastAssistantMessageTokens returns the tokens of the last assistant message
// in the slice that has a non-zero output count, mirroring opencode's
// sidebar context selector (findLast(role == assistant && tokens.output > 0)).
// Returns nil when no such message exists (draft / no completed turns).
func lastAssistantMessageTokens(msgs []Message) *MessageTokens {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := &msgs[i]
		if m.Role != "assistant" {
			continue
		}
		if m.Tokens.Output <= 0 {
			continue
		}
		return &m.Tokens
	}
	return nil
}
