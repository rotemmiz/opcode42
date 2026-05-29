package pty

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ticketTTL bounds how long a connect ticket is valid (plan 01 §6). Tickets are
// single-use; they let a browser open the PTY WebSocket without Basic auth
// (server/shared/pty-ticket.ts).
const ticketTTL = 60 * time.Second

type ticketEntry struct {
	ptyID   string
	expires time.Time
}

// tickets is a single-use, TTL-bounded connect-ticket store, scoped to one
// instance's PTY manager.
type tickets struct {
	mu      sync.Mutex
	entries map[string]ticketEntry
}

func newTickets() *tickets { return &tickets{entries: make(map[string]ticketEntry)} }

// IssueTicket mints a one-time ticket for ptyID, valid for ticketTTL.
func (m *Manager) IssueTicket(ptyID string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw[:])
	m.tickets.mu.Lock()
	m.tickets.purgeLocked()
	m.tickets.entries[token] = ticketEntry{ptyID: ptyID, expires: time.Now().Add(ticketTTL)}
	m.tickets.mu.Unlock()
	return token, nil
}

// ConsumeTicket validates and burns a ticket for ptyID.
func (m *Manager) ConsumeTicket(token, ptyID string) bool {
	m.tickets.mu.Lock()
	defer m.tickets.mu.Unlock()
	e, ok := m.tickets.entries[token]
	if !ok {
		return false
	}
	// Only burn the ticket on a successful match (opencode invalidates only when
	// the predicate matches, ticket.ts:51-53); a wrong-ptyID probe leaves a valid
	// ticket usable by the legitimate client.
	if e.ptyID == ptyID && time.Now().Before(e.expires) {
		delete(m.tickets.entries, token)
		return true
	}
	return false
}

// purgeLocked drops expired tickets; caller holds the lock.
func (t *tickets) purgeLocked() {
	now := time.Now()
	for k, e := range t.entries {
		if now.After(e.expires) {
			delete(t.entries, k)
		}
	}
}
