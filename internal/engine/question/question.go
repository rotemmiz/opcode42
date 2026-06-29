// Package question implements the blocking ask/reply flow the `question` tool
// uses: a tool asks the user one or more questions, the daemon publishes a
// question.asked SSE event, and the tool goroutine blocks until a client POSTs
// answers (or the context is cancelled). It mirrors opencode's Question service
// (packages/opencode/src/question/index.ts) — a multi-question request whose
// reply carries one answer (an array of selected option labels) per question —
// and is the same deferred-unblock pattern the permission manager (M7) reuses.
package question

import (
	"context"
	"errors"
	"sync"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/id"
)

// ErrRejected is returned when a question is dismissed by the user.
var ErrRejected = errors.New("the user dismissed this question")

// ErrUnknown is returned when replying to an unknown/already-answered id.
var ErrUnknown = errors.New("unknown question id")

// Option is one selectable choice within a question
// (question/index.ts QuestionOption).
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Info is one question within a request (question/index.ts QuestionInfo).
type Info struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []Option `json:"options"`
	Multiple bool     `json:"multiple,omitempty"`
	Custom   bool     `json:"custom,omitempty"`
}

// Request is the payload published on question.asked.
type Request struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Questions []Info `json:"questions"`
}

// Manager tracks pending questions and unblocks them on reply.
type Manager struct {
	bus     *bus.Bus
	mu      sync.Mutex
	pending map[string]*pending
}

type pending struct {
	req     Request
	resolve chan result
}

type result struct {
	answers [][]string
	err     error
}

// NewManager builds a question manager that publishes on the given bus.
func NewManager(b *bus.Bus) *Manager {
	return &Manager{bus: b, pending: map[string]*pending{}}
}

// Ask publishes a set of questions and blocks until a reply arrives or ctx is
// cancelled. It returns the answers (one array of selected labels per question,
// in order), ErrRejected if dismissed, or ctx.Err().
func (m *Manager) Ask(ctx context.Context, sessionID string, questions []Info) ([][]string, error) {
	req := Request{ID: id.Ascending(id.Question), SessionID: sessionID, Questions: questions}
	p := &pending{req: req, resolve: make(chan result, 1)}

	m.mu.Lock()
	m.pending[req.ID] = p
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(bus.NewEvent("question.asked", req))
	}

	select {
	case <-ctx.Done():
		m.remove(req.ID)
		return nil, ctx.Err()
	case r := <-p.resolve:
		return r.answers, r.err
	}
}

// Reply answers a pending question with one selected-label array per question.
func (m *Manager) Reply(questionID string, answers [][]string) error {
	m.mu.Lock()
	p, ok := m.pending[questionID]
	if ok {
		delete(m.pending, questionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	p.resolve <- result{answers: answers}
	if m.bus != nil {
		m.bus.Publish(bus.NewEvent("question.replied", map[string]any{
			"sessionID": p.req.SessionID, "requestID": questionID, "answers": answers,
		}))
	}
	return nil
}

// Reject dismisses a pending question (the user declined to answer).
func (m *Manager) Reject(questionID string) error {
	m.mu.Lock()
	p, ok := m.pending[questionID]
	if ok {
		delete(m.pending, questionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	p.resolve <- result{err: ErrRejected}
	if m.bus != nil {
		m.bus.Publish(bus.NewEvent("question.rejected", map[string]any{
			"sessionID": p.req.SessionID, "requestID": questionID,
		}))
	}
	return nil
}

// List returns the currently-pending questions.
func (m *Manager) List() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, 0, len(m.pending))
	for _, p := range m.pending {
		out = append(out, p.req)
	}
	return out
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	delete(m.pending, id)
	m.mu.Unlock()
}
