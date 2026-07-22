package tui

import (
	"encoding/json"
	"testing"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func ev(typ string, props any) opcode42client.SSEEvent {
	raw, _ := json.Marshal(props)
	return opcode42client.SSEEvent{Type: typ, Properties: raw}
}

func TestReduce_SessionsSortedUpsertDelete(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("session.updated", map[string]any{"info": map[string]any{"id": "ses_b", "title": "B"}}))
	s = s.Reduce(ev("session.updated", map[string]any{"info": map[string]any{"id": "ses_a", "title": "A"}}))
	if len(s.sessions) != 2 || s.sessions[0].ID != "ses_a" || s.sessions[1].ID != "ses_b" {
		t.Fatalf("sessions not sorted-inserted: %+v", s.sessions)
	}
	// upsert replaces in place
	s = s.Reduce(ev("session.updated", map[string]any{"info": map[string]any{"id": "ses_a", "title": "A2"}}))
	if len(s.sessions) != 2 || s.sessions[0].Title != "A2" {
		t.Fatalf("upsert did not replace: %+v", s.sessions)
	}
	s = s.Reduce(ev("session.deleted", map[string]any{"sessionID": "ses_a"}))
	if len(s.sessions) != 1 || s.sessions[0].ID != "ses_b" {
		t.Fatalf("delete failed: %+v", s.sessions)
	}
}

func TestReduce_MessagesAndParts(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("message.updated", map[string]any{"info": map[string]any{"id": "msg_1", "sessionID": "ses_1", "role": "assistant"}}))
	if len(s.messages["ses_1"]) != 1 || s.messages["ses_1"][0].Role != "assistant" {
		t.Fatalf("message not stored: %+v", s.messages)
	}
	s = s.Reduce(ev("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_1", "messageID": "msg_1", "type": "text", "text": "Hel"}}))
	if len(s.parts["msg_1"]) != 1 || s.parts["msg_1"][0].Text != "Hel" {
		t.Fatalf("part not stored: %+v", s.parts)
	}
}

func TestReduce_PartDeltaAppendsText(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_1", "messageID": "msg_1", "type": "text", "text": "Hel"}}))
	s = s.Reduce(ev("message.part.delta", map[string]any{"messageID": "msg_1", "partID": "prt_1", "field": "text", "delta": "lo"}))
	if s.parts["msg_1"][0].Text != "Hello" {
		t.Fatalf("delta not appended: %q", s.parts["msg_1"][0].Text)
	}
	// a delta to a non-text field is ignored
	s = s.Reduce(ev("message.part.delta", map[string]any{"messageID": "msg_1", "partID": "prt_1", "field": "other", "delta": "X"}))
	if s.parts["msg_1"][0].Text != "Hello" {
		t.Fatalf("non-text delta should be ignored: %q", s.parts["msg_1"][0].Text)
	}
}

func TestReduce_IgnoresMalformedAndUnknown(t *testing.T) {
	s := newStore()
	s = s.Reduce(opcode42client.SSEEvent{Type: "message.updated", Properties: json.RawMessage(`not json`)})
	s = s.Reduce(opcode42client.SSEEvent{Type: "totally.unknown", Properties: json.RawMessage(`{}`)})
	if len(s.sessions) != 0 || len(s.messages) != 0 {
		t.Fatalf("malformed/unknown events should be no-ops")
	}
}

func TestReduce_RemovalEvents(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("message.updated", map[string]any{"info": map[string]any{"id": "msg_1", "sessionID": "ses_1", "role": "assistant"}}))
	s = s.Reduce(ev("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_1", "messageID": "msg_1", "type": "text", "text": "x"}}))
	s = s.Reduce(ev("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_2", "messageID": "msg_1", "type": "text", "text": "y"}}))

	// remove one part
	s = s.Reduce(ev("message.part.removed", map[string]any{"messageID": "msg_1", "partID": "prt_1"}))
	if len(s.parts["msg_1"]) != 1 || s.parts["msg_1"][0].ID != "prt_2" {
		t.Fatalf("part not removed: %+v", s.parts["msg_1"])
	}
	// remove the message (drops it + its parts)
	s = s.Reduce(ev("message.removed", map[string]any{"sessionID": "ses_1", "messageID": "msg_1"}))
	if len(s.messages["ses_1"]) != 0 || len(s.parts["msg_1"]) != 0 {
		t.Fatalf("message not removed: msgs=%+v parts=%+v", s.messages["ses_1"], s.parts["msg_1"])
	}
}

func TestReduce_DecodesAssistantError(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("message.updated", map[string]any{"info": map[string]any{
		"id": "msg_1", "sessionID": "ses_1", "role": "assistant",
		"error": map[string]any{"name": "ContextOverflowError", "data": map[string]any{"message": "too large"}},
	}}))
	msgs := s.messages["ses_1"]
	if len(msgs) != 1 || msgs[0].Error == nil || msgs[0].Error.Name != "ContextOverflowError" || msgs[0].Error.text() != "too large" {
		t.Fatalf("error not decoded: %+v", msgs)
	}
}

// TestStore_VersionIncrementsOnReduce verifies that the store's version
// counter increments on every Reduce call (plan 19 §1). This counter is the
// "content changed" signal for render caches: during pure scroll the store
// is untouched, so the version stays stable and caches hit.
func TestStore_VersionIncrementsOnReduce(t *testing.T) {
	s := newStore()
	if s.version != 0 {
		t.Fatalf("initial version should be 0, got %d", s.version)
	}
	s = s.Reduce(ev("session.updated", map[string]any{"info": map[string]any{"id": "s1", "title": "S1"}}))
	if s.version != 1 {
		t.Fatalf("version should be 1 after one Reduce, got %d", s.version)
	}
	s = s.Reduce(ev("message.updated", map[string]any{"info": map[string]any{"id": "m1", "sessionID": "s1", "role": "user"}}))
	if s.version != 2 {
		t.Fatalf("version should be 2 after two Reduces, got %d", s.version)
	}
	// Even an unknown event type increments the version — Reduce is called,
	// and the counter tracks "Reduce ran", not "content changed". This is
	// conservative: a spurious event causes a cache miss (rebuild), which is
	// correct (safe) behaviour. The hot path (pure scroll) never calls Reduce.
	s = s.Reduce(ev("totally.unknown", map[string]any{}))
	if s.version != 3 {
		t.Fatalf("version should be 3 after three Reduces, got %d", s.version)
	}
}

// TestStore_VersionStableWithoutReduce verifies that the version does NOT
// change when no Reduce is called — the invariant that makes pure-scroll
// cache hits safe. The test simulates the scroll scenario: the store is
// read but never mutated between frames.
func TestStore_VersionStableWithoutReduce(t *testing.T) {
	s := newStore()
	s = s.Reduce(ev("session.updated", map[string]any{"info": map[string]any{"id": "s1", "title": "S1"}}))
	v := s.version
	// "Scroll" — read the store but don't call Reduce
	for i := 0; i < 10; i++ {
		if s.version != v {
			t.Fatalf("version changed without Reduce: was %d, now %d", v, s.version)
		}
	}
}
