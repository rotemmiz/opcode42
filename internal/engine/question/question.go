// Package question implements the blocking ask/reply flow the `question` tool
// uses: a tool asks the user a question, the daemon publishes a question.asked
// SSE event, and the tool goroutine blocks until a client POSTs a reply (or the
// context is cancelled). It mirrors opencode's Question service and is the same
// deferred-unblock pattern the permission manager (M7) reuses.
package question

import (
	"context"
	"errors"
	"sync"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/id"
)

// ErrRejected is returned when a question is dismissed by the user.
var ErrRejected = errors.New("the user dismissed this question")

// ErrUnknown is returned when replying to an unknown/already-answered id.
var ErrUnknown = errors.New("unknown question id")

// Request is the payload published on question.asked.
type Request struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Text      string   `json:"text"`
	Options   []string `json:"options,omitempty"`
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
	answer string
	err    error
}

// NewManager builds a question manager that publishes on the given bus.
func NewManager(b *bus.Bus) *Manager {
	return &Manager{bus: b, pending: map[string]*pending{}}
}

// Ask publishes a question and blocks until a reply arrives or ctx is cancelled.
// It returns the chosen answer text, ErrRejected if dismissed, or ctx.Err().
func (m *Manager) Ask(ctx context.Context, sessionID, text string, options []string) (string, error) {
	req := Request{ID: id.Ascending(id.Question), SessionID: sessionID, Text: text, Options: options}
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
		return "", ctx.Err()
	case r := <-p.resolve:
		return r.answer, r.err
	}
}

// Reply answers a pending question. answer="" with reject=true dismisses it.
func (m *Manager) Reply(questionID, answer string, reject bool) error {
	m.mu.Lock()
	p, ok := m.pending[questionID]
	if ok {
		delete(m.pending, questionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknown
	}
	if reject {
		p.resolve <- result{err: ErrRejected}
	} else {
		p.resolve <- result{answer: answer}
	}
	if m.bus != nil {
		m.bus.Publish(bus.NewEvent("question.replied", map[string]any{
			"sessionID": p.req.SessionID, "questionID": questionID, "answer": answer, "rejected": reject,
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
