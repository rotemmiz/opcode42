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
	// Tokens is the assistant message's cumulative token usage (wire field
	// `tokens`). User messages carry no tokens — the field stays zero. The
	// sidebar's context gauge reads the last assistant message's tokens to
	// populate on session switch before a new turn arrives (plan 08e §E5).
	Tokens MessageTokens `json:"tokens,omitempty"`
}

// MessageTokens mirrors the assistant message's token accounting block
// (openapi AssistantMessage.tokens). Total is the sum of all directions and
// matches the Session.Tokens aggregate shape so the gauge can read either.
type MessageTokens struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	Cache     struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}

// Total is all token directions summed (matches SessionTokens.Total).
func (t MessageTokens) Total() float64 {
	return t.Input + t.Output + t.Reasoning + t.Cache.Read + t.Cache.Write
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

	// Time is the part's start/end timestamps (wire field `time`,
	// openapi.json ReasoningPart/TextPart.time). Populated from
	// message.part.updated events. End is 0 while the part is still
	// streaming; the renderer uses End != 0 as the "isDone" signal
	// (plan 17 §D4 — matching opencode's ReasoningPart isDone,
	// tui/routes/session/index.tsx:1585).
	Time PartTime `json:"time,omitempty"`

	// File-part fields (type == "file"; plan 08e §E2). Mime is the file's
	// content type (e.g. "image/png"). URL carries the bytes for inline
	// images as a "data:<mime>;base64,<payload>" URL (the shape opencode's
	// pasteAttachment emits — packages/tui/src/component/prompt/index.tsx:1246
	// and image/image.ts:148). Filename is the optional display name.
	Mime     string `json:"mime,omitempty"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// PartTime carries the part's start/end timestamps (ms since epoch). Start is
// required by the wire schema; End is set by the daemon when the part
// finalizes. While End == 0 the part is still streaming.
type PartTime struct {
	Start int64 `json:"start"`
	End   int64 `json:"end,omitempty"`
}

// Done reports whether the part has finalized (the daemon set Time.End).
// Used by the reasoning renderer to switch from the streaming spinner header
// to the static "Thought · <duration>" header (plan 17 §D4).
func (t PartTime) Done() bool { return t.End != 0 }

// Duration returns End - Start in milliseconds, clamped at zero. Returns 0
// when the part is still streaming (End == 0) — callers should gate on Done()
// before formatting the duration for display.
func (t PartTime) Duration() int64 {
	if t.End == 0 || t.End < t.Start {
		return 0
	}
	return t.End - t.Start
}

// Permission is a pending permission request (permission.asked) awaiting a reply.
type Permission struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionID"`
	Permission string          `json:"permission"` // the action (e.g. "bash", "edit")
	Metadata   json.RawMessage `json:"metadata"`   // tool-specific detail (command, path, diff…)
	Tool       json.RawMessage `json:"tool"`
	// Patterns is the set of patterns the request would touch (the
	// permission.asked payload's `patterns` field, e.g. ["src/**.go"]).
	// Used by permissionInfo for the title/detail lines.
	Patterns []string `json:"patterns"`
	// Always is the set of patterns an "always" reply would persist as
	// session grants (the permission.asked payload's `always` field). A
	// single "*" means "allow this permission entirely" (the one-liner
	// confirmation, permission.shared.ts:127-129). The "always"
	// confirmation stage (plan 17 §B3) lists these as the patterns that
	// will be allowed until OpenCode is restarted.
	Always []string `json:"always"`
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

// AnsweredQuestion is a finalized question request kept in the stream scrollback
// (plan 08e §E4). It captures the question text + the selected labels (or
// "Skipped" when rejected) so the conversation history shows the question that
// was asked, not just a transient modal. One entry per question request, keyed
// by the request id (deduped across the local reply path and the SSE path).
type AnsweredQuestion struct {
	ID        string         // the question request id (dedup key)
	SessionID string         // the session this question belongs to
	Skipped   bool           // true when the request was rejected
	Answers   [][]string     // selected labels per sub-question (empty when skipped)
	Questions []QuestionInfo // the question texts + headers, captured at finalize time
}

// store holds the mirrored, sorted view-state.
type store struct {
	sessions          []Session                     // sorted by id
	messages          map[string][]Message          // sessionID -> sorted by id
	parts             map[string][]Part             // messageID -> sorted by id
	permissions       []Permission                  // pending permission requests (FIFO)
	questions         []Question                    // pending question requests (FIFO)
	answeredQuestions map[string][]AnsweredQuestion // sessionID -> finalized questions (plan 08e §E4)

	// version is a monotonic counter incremented on every Reduce call and
	// on every direct store mutation in model.go (plan 19 §1). It is the
	// single reliable "content changed" signal: render caches key on it to
	// skip rebuilding byte-identical output during pure scroll. Every code
	// path that mutates store fields OUTSIDE Reduce must also bump this —
	// see the direct-mutation sites in model.go (sessionOpenedMsg,
	// sessionDeletedMsg, sessionCreatedMsg, renamedMsg, sharedMsg,
	// forkedMsg, permissionRepliedMsg, questionRepliedMsg,
	// permissionsReconciledMsg, questionsReconciledMsg, sessionsLoadedMsg,
	// messagesLoadedMsg, childrenLoadedMsg, childMessagesLoadedMsg) and
	// question.go (recordAnsweredQuestion).
	version int
}

func newStore() store {
	return store{
		messages:          map[string][]Message{},
		parts:             map[string][]Part{},
		answeredQuestions: map[string][]AnsweredQuestion{},
	}
}

// recordAnsweredQuestion appends a finalized question to the session's
// answered-questions slice (plan 08e §E4), deduped by request id so the local
// reply path and the SSE path can both record the same finalization without
// double-rendering. The slice is kept in arrival order (chronological), which
// mirrors how the pending question was positioned in the stream.
//
// If an entry with the same id already exists (e.g. the SSE event arrived
// before the local HTTP response), the entry is UPGRADED in place when the new
// record carries richer info (non-skipped + non-empty Answers). This guards the
// edge case where the SSE label-less fallback records first and the local
// reply path (with the specific labels) arrives second — the labels should
// win, not the first writer. Skipped state is never downgraded by a later
// non-skipped record (a reject can't become a reply).
func (s store) recordAnsweredQuestion(aq AnsweredQuestion) store {
	if aq.ID == "" {
		return s
	}
	if s.answeredQuestions == nil {
		s.answeredQuestions = map[string][]AnsweredQuestion{}
	}
	for i, existing := range s.answeredQuestions[aq.SessionID] {
		if existing.ID == aq.ID {
			// Upgrade in place: a non-skipped record with answers wins over a
			// skipped or label-less one. Never downgrade an existing skipped
			// entry (reject can't become a reply).
			if !aq.Skipped && len(aq.Answers) > 0 {
				s.answeredQuestions[aq.SessionID][i] = aq
			}
			return s
		}
	}
	s.answeredQuestions[aq.SessionID] = append(s.answeredQuestions[aq.SessionID], aq)
	return s
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
			// Plan 08e §E4: capture the finalized question for the in-stream
			// answered card BEFORE removing it from the pending slice. The SSE
			// event carries only the request id (not the selected labels), so
			// the card shows "Answered" rather than the specific labels when the
			// reply originated elsewhere; the local reply path
			// (questionRepliedMsg in model.go) records the specific labels and
			// dedupes against this entry by id.
			skipped := ev.Type == "question.rejected"
			for _, q := range s.questions {
				if q.ID == p.RequestID {
					s = s.recordAnsweredQuestion(AnsweredQuestion{
						ID:        q.ID,
						SessionID: q.SessionID,
						Skipped:   skipped,
						Questions: append([]QuestionInfo(nil), q.Questions...),
					})
					break
				}
			}
			s.questions = removeByID(s.questions, p.RequestID, func(x Question) string { return x.ID })
		}
	}
	s.version++
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
