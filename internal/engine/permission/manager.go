package permission

import (
	"context"
	"errors"
	"sync"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/id"
)

// DeniedError is returned by Ask when a pattern is denied or a Request is rejected.
type DeniedError struct{ Permission string }

func (e *DeniedError) Error() string { return "permission denied: " + e.Permission }

// AskInput describes a permission Request.
type AskInput struct {
	SessionID  string
	Permission string
	Patterns   []string
	Metadata   map[string]any
	// Always lists the patterns persisted as session grants on an "always" reply.
	Always []string
	// Rulesets are the merged agent/config rulesets to evaluate against (the
	// manager appends its own per-session approved grants).
	Rulesets []Ruleset
	Tool     string
}

// Reply kinds.
const (
	ReplyOnce   = "once"
	ReplyAlways = "always"
	ReplyReject = "reject"
)

// Request is the wire payload for permission.asked.
type Request struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"sessionID"`
	Permission string         `json:"permission"`
	Patterns   []string       `json:"patterns"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Always     []string       `json:"always,omitempty"`
	Tool       string         `json:"tool,omitempty"`
}

type pending struct {
	info    AskInput
	req     Request
	resolve chan error
}

// Manager evaluates permission asks and blocks tools until replied. It persists
// per-session "always" grants and cascades rejects.
type Manager struct {
	bus      *bus.Bus
	mu       sync.Mutex
	pending  map[string]*pending // requestID -> pending
	approved map[string]Ruleset  // sessionID -> session-level allow rules
}

// NewManager builds a permission manager publishing on the given bus.
func NewManager(b *bus.Bus) *Manager {
	return &Manager{bus: b, pending: map[string]*pending{}, approved: map[string]Ruleset{}}
}

// Ask evaluates every pattern: any deny returns a DeniedError immediately; all
// allow returns nil; otherwise it publishes permission.asked and blocks until a
// reply (or ctx cancel) (permission/index.ts:171-211).
func (m *Manager) Ask(ctx context.Context, in AskInput) error {
	m.mu.Lock()
	rulesets := m.rulesetsFor(in)
	allAllowed := true
	for _, pat := range in.Patterns {
		switch Evaluate(in.Permission, pat, rulesets...).Action {
		case ActionDeny:
			m.mu.Unlock()
			return &DeniedError{Permission: in.Permission}
		case ActionAllow:
		default:
			allAllowed = false
		}
	}
	if len(in.Patterns) == 0 {
		allAllowed = false // an empty-pattern ask still prompts
	}
	if allAllowed {
		m.mu.Unlock()
		return nil
	}

	p := &pending{
		info: in,
		req: Request{ID: id.Ascending(id.Permission), SessionID: in.SessionID, Permission: in.Permission,
			Patterns: in.Patterns, Metadata: in.Metadata, Always: in.Always, Tool: in.Tool},
		resolve: make(chan error, 1),
	}
	m.pending[p.req.ID] = p
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(bus.NewEvent("permission.asked", p.req))
	}

	select {
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pending, p.req.ID)
		m.mu.Unlock()
		return ctx.Err()
	case err := <-p.resolve:
		return err
	}
}

// Reply resolves a pending Request: once succeeds it; always succeeds it and
// persists its Always patterns as session grants, then unblocks other pending
// requests that now pass; reject fails it and cascades to the session's other
// pending requests (permission/index.ts:213-268).
func (m *Manager) Reply(requestID, reply string) error {
	m.mu.Lock()
	p, ok := m.pending[requestID]
	if !ok {
		m.mu.Unlock()
		return errors.New("unknown permission Request")
	}
	delete(m.pending, requestID)
	sessionID := p.info.SessionID

	switch reply {
	case ReplyReject:
		// Fail this and cascade-reject the session's other pending requests.
		p.resolve <- &DeniedError{Permission: p.info.Permission}
		for rid, other := range m.pending {
			if other.info.SessionID == sessionID {
				delete(m.pending, rid)
				other.resolve <- &DeniedError{Permission: other.info.Permission}
				m.publishReplied(other.req.ID, sessionID, ReplyReject)
			}
		}
	case ReplyAlways:
		for _, pat := range p.info.Always {
			m.approved[sessionID] = append(m.approved[sessionID],
				Rule{Permission: p.info.Permission, Pattern: pat, Action: ActionAllow})
		}
		p.resolve <- nil
		m.unblockNowAllowed(sessionID)
	default: // once
		p.resolve <- nil
	}
	m.mu.Unlock()

	m.publishReplied(requestID, sessionID, reply)
	return nil
}

// unblockNowAllowed succeeds any pending Request for the session that the
// updated grants now fully allow (caller holds mu).
func (m *Manager) unblockNowAllowed(sessionID string) {
	for rid, other := range m.pending {
		if other.info.SessionID != sessionID || len(other.info.Patterns) == 0 {
			continue
		}
		rulesets := m.rulesetsFor(other.info)
		allowed := true
		for _, pat := range other.info.Patterns {
			if Evaluate(other.info.Permission, pat, rulesets...).Action != ActionAllow {
				allowed = false
				break
			}
		}
		if allowed {
			delete(m.pending, rid)
			other.resolve <- nil
			m.publishReplied(other.req.ID, sessionID, ReplyAlways)
		}
	}
}

// rulesetsFor returns the ask's rulesets plus the session's approved grants
// (caller holds mu).
func (m *Manager) rulesetsFor(in AskInput) []Ruleset {
	rulesets := append([]Ruleset(nil), in.Rulesets...)
	if approved := m.approved[in.SessionID]; len(approved) > 0 {
		rulesets = append(rulesets, approved)
	}
	return rulesets
}

func (m *Manager) publishReplied(requestID, sessionID, reply string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(bus.NewEvent("permission.replied", map[string]any{
		"sessionID": sessionID, "requestID": requestID, "reply": reply,
	}))
}

// List returns the currently-pending permission requests.
func (m *Manager) List() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, 0, len(m.pending))
	for _, p := range m.pending {
		out = append(out, p.req)
	}
	return out
}

// AskPermission satisfies processor.PermissionAsker for the doom-loop guard.
func (m *Manager) AskPermission(ctx context.Context, sessionID, permission string, patterns []string, metadata map[string]any) error {
	return m.Ask(ctx, AskInput{SessionID: sessionID, Permission: permission,
		Patterns: patterns, Metadata: metadata, Always: patterns})
}
