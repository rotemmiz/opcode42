// Package enginetest is the agent engine's deterministic integration harness.
//
// Its centerpiece is MockProvider: an llm.Provider that replays a scripted
// []llm.Event with no network or API key, so tests assert the exact
// Event -> SSE -> DB sequence reproducibly. The harness is scaffolded here
// at M1 and grown each milestone — M2 adds an httptest wire-level variant, M9
// drives full Prompt/Loop scenarios (text-only, tool-call), M10 compaction.
package enginetest

import (
	"context"
	"sync"

	"github.com/rotemmiz/forge/internal/engine/llm"
)

// MockProvider replays one or more scripted streams. Each call to Stream pops
// the next script (the last script repeats once exhausted), letting a single
// provider drive a multi-step agent loop deterministically.
type MockProvider struct {
	mu         sync.Mutex
	scripts    [][]llm.Event
	calls      int
	capability llm.Capability
	requests   []*llm.Request
}

// NewMockProvider builds a provider that replays the given scripts in order.
func NewMockProvider(scripts ...[]llm.Event) *MockProvider {
	return &MockProvider{
		scripts:    scripts,
		capability: llm.Capability{ToolCalls: true, Streaming: true},
	}
}

// WithCapability overrides the reported capability flags.
func (m *MockProvider) WithCapability(c llm.Capability) *MockProvider {
	m.capability = c
	return m
}

// Calls reports how many times Stream has been invoked.
func (m *MockProvider) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// Requests returns a copy of every request the engine issued, safe to call
// while streams are in flight.
func (m *MockProvider) Requests() []*llm.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*llm.Request(nil), m.requests...)
}

// Stream returns the next scripted event sequence on a fresh channel.
func (m *MockProvider) Stream(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	m.mu.Lock()
	events := m.scriptFor(m.calls)
	m.calls++
	m.requests = append(m.requests, req)
	m.mu.Unlock()

	ch := make(chan llm.Event)
	go func() {
		defer close(ch)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

// Capability returns the configured capability flags.
func (m *MockProvider) Capability() llm.Capability { return m.capability }

func (m *MockProvider) scriptFor(i int) []llm.Event {
	if len(m.scripts) == 0 {
		return nil
	}
	if i >= len(m.scripts) {
		return m.scripts[len(m.scripts)-1]
	}
	return m.scripts[i]
}
