package tui

import (
	"strings"
	"testing"
)

// TestIngestHistory_PopulatesTokensFromLastMessage (plan 08e §E5) asserts that
// ingestHistory backfills the session's aggregated Tokens from the last
// assistant message's tokens when the session row carries none yet. Without
// this, the sidebar's context gauge would blank on session switch until a new
// turn's session.updated SSE arrives.
func TestIngestHistory_PopulatesTokensFromLastMessage(t *testing.T) {
	s := newStore()
	s.sessions = []Session{{ID: "ses_1", Title: "Draft"}} // Tokens zero — the switch case

	// Build a history: user, assistant w/ tokens, assistant w/ newer tokens.
	// The newest assistant message's tokens must win (mirrors opencode's
	// findLast(role == assistant && tokens.output > 0)).
	var older AssistantTokensBuilder
	older.Input, older.Output = 500, 100
	var newer AssistantTokensBuilder
	newer.Input, newer.Output = 1200, 34
	newer.Cache.Read, newer.Cache.Write = 200, 0

	s = s.ingestHistory("ses_1", []wireWithParts{
		{Info: Message{ID: "msg_u1", SessionID: "ses_1", Role: "user"}, Parts: nil},
		{Info: Message{ID: "msg_a1", SessionID: "ses_1", Role: "assistant", Tokens: older.MessageTokens()}, Parts: nil},
		{Info: Message{ID: "msg_a2", SessionID: "ses_1", Role: "assistant", Tokens: newer.MessageTokens()}, Parts: nil},
	})

	got := s.sessionByID("ses_1")
	if got == nil {
		t.Fatal("session dropped from store")
	}
	if got.Tokens.Input != 1200 || got.Tokens.Output != 34 {
		t.Fatalf("backfilled tokens = in=%d out=%d, want in=1200 out=34 (newer assistant)", got.Tokens.Input, got.Tokens.Output)
	}
	if got.Tokens.Cache.Read != 200 || got.Tokens.Cache.Write != 0 {
		t.Fatalf("backfilled cache = read=%d write=%d, want read=200 write=0", got.Tokens.Cache.Read, got.Tokens.Cache.Write)
	}
	if got.Tokens.Total() != (1200 + 34 + 200 + 0) {
		t.Fatalf("backfilled total = %d, want 1434", got.Tokens.Total())
	}
}

// TestIngestHistory_KeepsSessionTokensWhenAlreadySet (plan 08e §E5) asserts the
// backfill is conditional: when the session row already carries tokens (e.g.
// session.updated arrived first), ingestHistory must NOT overwrite them with
// the last message's tokens — the SSE path is authoritative while a turn runs.
func TestIngestHistory_KeepsSessionTokensWhenAlreadySet(t *testing.T) {
	s := newStore()
	existing := Session{ID: "ses_1", Title: "Live"}
	existing.Tokens.Input, existing.Tokens.Output = 9999, 9999
	s.sessions = []Session{existing}

	var newer AssistantTokensBuilder
	newer.Input, newer.Output = 100, 50
	s = s.ingestHistory("ses_1", []wireWithParts{
		{Info: Message{ID: "msg_a1", SessionID: "ses_1", Role: "assistant", Tokens: newer.MessageTokens()}, Parts: nil},
	})

	got := s.sessionByID("ses_1")
	if got.Tokens.Input != 9999 || got.Tokens.Output != 9999 {
		t.Fatalf("backfill overwrote live tokens: got in=%d out=%d, want 9999/9999", got.Tokens.Input, got.Tokens.Output)
	}
}

// TestIngestHistory_LeavesZeroWhenNoAssistant (plan 08e §E5) asserts the
// backfill is a no-op for a draft session (user-only or no messages): the
// session's Tokens stay zero so the sidebar renders the draft '0 / <limit>'
// fallback rather than a misleading non-zero gauge.
func TestIngestHistory_LeavesZeroWhenNoAssistant(t *testing.T) {
	s := newStore()
	s.sessions = []Session{{ID: "ses_1", Title: "Draft"}}
	s = s.ingestHistory("ses_1", []wireWithParts{
		{Info: Message{ID: "msg_u1", SessionID: "ses_1", Role: "user"}, Parts: nil},
	})
	got := s.sessionByID("ses_1")
	if got.Tokens.Total() != 0 {
		t.Fatalf("draft session tokens = %d, want 0", got.Tokens.Total())
	}
}

// TestSidebar_DraftShowsZeroOverLimit (plan 08e §E5) asserts that a session
// with no messages renders '0 / <limit>' in the CONTEXT section instead of
// the bare em-dash placeholder. The limit resolves from the cached providers
// catalog (m.choices) for the active model.
func TestSidebar_DraftShowsZeroOverLimit(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.width, m.height = 120, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Draft"}} // Tokens zero
	m.choices = []modelChoice{
		{Provider: "anthropic", Model: "claude-sonnet-4", ContextLimit: 200000},
	}

	out := m.sidebarView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "0") {
		t.Fatalf("draft sidebar missing '0' in:\n%s", plain)
	}
	if !strings.Contains(plain, "200,000") {
		t.Fatalf("draft sidebar should show the cached limit '200,000', got:\n%s", plain)
	}
	if strings.Contains(plain, "—") {
		t.Fatalf("draft sidebar should not show the em-dash placeholder, got:\n%s", plain)
	}
}

// TestSidebar_DraftFallsBackToDefaultLimit (plan 08e §E5) asserts that when the
// providers cache is empty (before the first /provider response arrives) or
// the active model isn't found in it, the gauge falls back to the
// defaultContextLimit constant so the draft session still shows a limit.
func TestSidebar_DraftFallsBackToDefaultLimit(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.width, m.height = 120, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Draft"}}
	// No m.choices — cache empty.
	out := m.sidebarView()
	plain := stripANSI(out)
	want := humanInt(defaultContextLimit)
	if !strings.Contains(plain, want) {
		t.Fatalf("draft sidebar should fall back to defaultContextLimit %q, got:\n%s", want, plain)
	}
}

// TestSidebar_DraftFallsBackWhenModelNotInCache (plan 08e §E5) covers the case
// where the cache is populated but the active model isn't in it (e.g. the
// user --provider/--model passed an id the daemon doesn't advertise).
func TestSidebar_DraftFallsBackWhenModelNotInCache(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.width, m.height = 120, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Draft"}}
	m.choices = []modelChoice{
		{Provider: "openai", Model: "gpt-4o", ContextLimit: 128000}, // not the active model
	}
	out := m.sidebarView()
	plain := stripANSI(out)
	want := humanInt(defaultContextLimit)
	if !strings.Contains(plain, want) {
		t.Fatalf("draft sidebar should fall back to defaultContextLimit when active model not in cache, got:\n%s", plain)
	}
}

// TestProvidersCache_PersistsAcrossSessionSwitch (plan 08e §E5) asserts that
// the providers catalog (m.choices) is NOT re-fetched on session switch.
// loadProvidersCmd fires only on connectedMsg (once per connection); the
// cache survives a session switch with no re-fetch, so the context gauge can
// resolve the limit synchronously. The test simulates a switch by issuing a
// messagesLoadedMsg (the post-switch event) and asserting m.choices is
// unchanged. (messagesLoadedMsg does emit loadChildrenCmd — that's a different
// concern; the test only checks the providers cache is preserved.)
func TestProvidersCache_PersistsAcrossSessionSwitch(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.width, m.height = 120, 24
	m.choices = []modelChoice{
		{Provider: "anthropic", Model: "claude-sonnet-4", ContextLimit: 200000},
	}
	m.store.sessions = []Session{{ID: "ses_1", Title: "S1"}, {ID: "ses_2", Title: "S2"}}

	// Simulate the post-switch event: messagesLoadedMsg for the new session.
	// The handler may emit a loadChildrenCmd (it does); the assertion is only
	// that m.choices is unchanged — i.e. no loadProvidersCmd was fired.
	m2, _ := step(t, m, messagesLoadedMsg{sessionID: "ses_2", items: nil})
	if len(m2.choices) != 1 || m2.choices[0].ContextLimit != 200000 {
		t.Fatalf("providers cache changed across session switch: %+v", m2.choices)
	}
}

// TestContextLimitForActiveModel (plan 08e §E5) covers the resolver helper
// directly: returns the cached limit when the active model is in the cache,
// zero when the cache is empty or the active model isn't in it.
func TestContextLimitForActiveModel(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.choices = []modelChoice{
		{Provider: "openai", Model: "gpt-4o", ContextLimit: 128000},
		{Provider: "anthropic", Model: "claude-sonnet-4", ContextLimit: 200000},
	}
	if got := m.contextLimitForActiveModel(); got != 200000 {
		t.Fatalf("contextLimitForActiveModel = %d, want 200000", got)
	}

	// Active model not in cache → 0 (caller falls back to defaultContextLimit).
	m2 := New(Config{URL: "http://x", Provider: "anthropic", Model: "claude-haiku-3"})
	m2.choices = m.choices
	if got := m2.contextLimitForActiveModel(); got != 0 {
		t.Fatalf("contextLimitForActiveModel for unknown model = %d, want 0", got)
	}

	// No model resolved (promptModel not ok) → 0.
	m3 := New(Config{URL: "http://x"})
	if got := m3.contextLimitForActiveModel(); got != 0 {
		t.Fatalf("contextLimitForActiveModel with no active model = %d, want 0", got)
	}
}

// AssistantTokensBuilder is a small test helper that builds a MessageTokens
// value for assistant messages in ingestHistory tests. Keeps the test data
// declarative and the field names close to the wire shape.
type AssistantTokensBuilder struct {
	Input, Output, Reasoning float64
	Cache                    struct {
		Read, Write float64
	}
}

// MessageTokens returns the built MessageTokens value.
func (b AssistantTokensBuilder) MessageTokens() MessageTokens {
	t := MessageTokens{Input: b.Input, Output: b.Output, Reasoning: b.Reasoning}
	t.Cache.Read = b.Cache.Read
	t.Cache.Write = b.Cache.Write
	return t
}
