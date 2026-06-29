package tui

import (
	"encoding/json"
	"sort"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// The TUI mirrors the daemon's state from the SSE stream into a small, sorted
// store of view-models (a thin subset of the wire shapes — just what the
// conversation renderer needs). Reduce(event) is the pure reducer, mirroring
// opencode's TUI sync store (plan 08); IDs are monotonic, so sorted insertion
// keeps everything in chronological order.

// Session is a session list entry.
type Session struct {
	ID        string        `json:"id"`
	ParentID  string        `json:"parentID,omitempty"` // set on sub-agent child sessions
	Title     string        `json:"title"`
	Directory string        `json:"directory"`
	Cost      float64       `json:"cost"`
	Tokens    SessionTokens `json:"tokens"`
	Share     *SessionShare `json:"share,omitempty"`
}

// SessionShare carries a published share link (session.share.url).
type SessionShare struct {
	URL string `json:"url"`
}

// SessionTokens is the running token accounting carried on a session.
type SessionTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Cache  struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// Total is all tokens attributed to the session (prompt + completion + cache).
func (t SessionTokens) Total() int { return t.Input + t.Output + t.Cache.Read + t.Cache.Write }

// Message is one turn (user/assistant) in a session.
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionID"`
	Role      string    `json:"role"`
	Error     *MsgError `json:"error,omitempty"`
}

// MsgError is an assistant turn's error (NamedError shape {name, data:{message}}).
type MsgError struct {
	Name string `json:"name"`
	Data struct {
		Message string `json:"message"`
	} `json:"data"`
}

// text returns the human-facing error string.
func (e *MsgError) text() string {
	if e.Data.Message != "" {
		return e.Data.Message
	}
	return e.Name
}

// Part is one piece of a message's content. Type discriminates; only the
// relevant fields are populated. State (tool) is kept raw for the renderer.
type Part struct {
	ID        string          `json:"id"`
	MessageID string          `json:"messageID"`
	SessionID string          `json:"sessionID"`
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callID,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
}

// Permission is a pending permission request (permission.asked) awaiting a reply.
type Permission struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionID"`
	Permission string          `json:"permission"` // the action (e.g. "bash", "edit")
	Metadata   json.RawMessage `json:"metadata"`   // tool-specific detail (command, path, …)
	Tool       json.RawMessage `json:"tool"`
}

// Question is a pending question request (question.asked) awaiting answers.
type Question struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionID"`
	Questions []QuestionInfo `json:"questions"`
}

// QuestionInfo is one question within a request.
type QuestionInfo struct {
	Question string           `json:"question"`
	Header   string           `json:"header"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple"`
	Custom   bool             `json:"custom"`
}

// QuestionOption is one selectable answer.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// store holds the mirrored, sorted view-state.
type store struct {
	sessions    []Session            // sorted by id
	messages    map[string][]Message // sessionID -> sorted by id
	parts       map[string][]Part    // messageID -> sorted by id
	permissions []Permission         // pending permission requests (FIFO)
	questions   []Question           // pending question requests (FIFO)
}

func newStore() store {
	return store{messages: map[string][]Message{}, parts: map[string][]Part{}}
}

// Reduce applies one SSE event to the store, returning the updated store. Pure
// w.r.t. the slices it reassigns; the maps are mutated in place (single-threaded
// in the Bubble Tea loop).
func (s store) Reduce(ev opcode42client.SSEEvent) store {
	switch ev.Type {
	case "session.updated":
		var p struct {
			Info Session `json:"info"`
		}
		if decode(ev.Properties, &p) && p.Info.ID != "" {
			s.sessions = upsertSession(s.sessions, p.Info)
		}
	case "session.deleted":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if decode(ev.Properties, &p) {
			s.sessions = removeSession(s.sessions, p.SessionID)
		}
	case "message.updated":
		var p struct {
			Info Message `json:"info"`
		}
		if decode(ev.Properties, &p) && p.Info.ID != "" {
			s.messages[p.Info.SessionID] = upsertByID(s.messages[p.Info.SessionID], p.Info,
				func(m Message) string { return m.ID })
		}
	case "message.part.updated":
		var p struct {
			Part Part `json:"part"`
		}
		if decode(ev.Properties, &p) && p.Part.ID != "" {
			s.parts[p.Part.MessageID] = upsertByID(s.parts[p.Part.MessageID], p.Part,
				func(pt Part) string { return pt.ID })
		}
	case "message.part.delta":
		var p struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if decode(ev.Properties, &p) && p.Field == "text" {
			parts := s.parts[p.MessageID]
			for i := range parts {
				if parts[i].ID == p.PartID {
					parts[i].Text += p.Delta
					break
				}
			}
		}
	case "message.removed":
		var p struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
		}
		if decode(ev.Properties, &p) {
			s.messages[p.SessionID] = removeByID(s.messages[p.SessionID], p.MessageID, func(m Message) string { return m.ID })
			delete(s.parts, p.MessageID)
		}
	case "message.part.removed":
		var p struct {
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
		}
		if decode(ev.Properties, &p) {
			s.parts[p.MessageID] = removeByID(s.parts[p.MessageID], p.PartID, func(pt Part) string { return pt.ID })
		}
	case "permission.asked":
		var p Permission
		if decode(ev.Properties, &p) && p.ID != "" {
			s.permissions = upsertByID(s.permissions, p, func(q Permission) string { return q.ID })
		}
	case "permission.replied":
		var p struct {
			RequestID string `json:"requestID"`
		}
		if decode(ev.Properties, &p) {
			s.permissions = removeByID(s.permissions, p.RequestID, func(q Permission) string { return q.ID })
		}
	case "question.asked":
		var q Question
		if decode(ev.Properties, &q) && q.ID != "" {
			s.questions = upsertByID(s.questions, q, func(x Question) string { return x.ID })
		}
	case "question.replied", "question.rejected":
		var p struct {
			RequestID string `json:"requestID"`
		}
		if decode(ev.Properties, &p) {
			s.questions = removeByID(s.questions, p.RequestID, func(x Question) string { return x.ID })
		}
	}
	return s
}

// upsertByID inserts or replaces v in a slice kept sorted by its id key. The
// store keeps slices sorted ASCENDING by id (chronological); callers that want
// "newest" read the last element (or the API response slice).
func upsertByID[T any](items []T, v T, id func(T) string) []T {
	key := id(v)
	i := sort.Search(len(items), func(i int) bool { return id(items[i]) >= key })
	if i < len(items) && id(items[i]) == key {
		items[i] = v
		return items
	}
	items = append(items, v)
	copy(items[i+1:], items[i:])
	items[i] = v
	return items
}

func upsertSession(items []Session, v Session) []Session {
	return upsertByID(items, v, func(s Session) string { return s.ID })
}

// removeByID drops the first element whose id matches key, preserving order.
func removeByID[T any](items []T, key string, id func(T) string) []T {
	for i := range items {
		if id(items[i]) == key {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func removeSession(items []Session, id string) []Session {
	for i := range items {
		if items[i].ID == id {
			return append(items[:i], items[i+1:]...)
		}
	}
	return items
}

func decode(raw json.RawMessage, dst any) bool {
	return len(raw) > 0 && json.Unmarshal(raw, dst) == nil
}
