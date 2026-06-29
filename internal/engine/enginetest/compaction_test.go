package enginetest

import (
	"context"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
)

// seedTurn inserts a completed user+assistant turn directly into the store so a
// session has enough history for compaction to have something to summarize.
func (r *rig) seedTurn(t *testing.T, uid, aid, text string, created int64) {
	t.Helper()
	ctx := context.Background()
	u := &message.UserMessage{ID: uid, SessionID: r.sessionID, Role: message.RoleUser,
		Agent: "build", Model: message.Model{ProviderID: "openai", ModelID: "gpt-4o"}}
	u.Time.Created = created
	if err := r.store.PutMessage(ctx, message.Info{User: u}); err != nil {
		t.Fatal(err)
	}
	if err := r.store.PutPart(ctx, &message.TextPart{
		PartBase: message.PartBase{ID: "prt_" + uid, SessionID: r.sessionID, MessageID: uid}, Type: "text", Text: text}); err != nil {
		t.Fatal(err)
	}
	a := &message.AssistantMessage{ID: aid, SessionID: r.sessionID, Role: message.RoleAssistant,
		ParentID: uid, ProviderID: "openai", ModelID: "gpt-4o", Agent: "build", Finish: "stop"}
	a.Time.Created = created + 1
	if err := r.store.PutMessage(ctx, message.Info{Assistant: a}); err != nil {
		t.Fatal(err)
	}
	if err := r.store.PutPart(ctx, &message.TextPart{
		PartBase: message.PartBase{ID: "prt_" + aid, SessionID: r.sessionID, MessageID: aid}, Type: "text", Text: "ok"}); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_Compaction: a turn that overflows the context triggers an
// auto-compaction — a summary:true assistant message is produced, session.compacted
// is emitted, and the loop resumes to a final answer.
func TestE2E_Compaction(t *testing.T) {
	overflow := NewScript().StepStart().Text("t1", "working...").
		StepFinish("stop", llm.TokenUsage{Input: 120000, Output: 10}).Finish().Events() // > gpt-4o usable budget
	summary := NewScript().StepStart().Text("s1", "## Goal\n- summarized").
		StepFinish("stop", llm.TokenUsage{Input: 500, Output: 50}).Finish().Events()
	final := NewScript().StepStart().Text("f1", "All done.").
		StepFinish("stop", llm.TokenUsage{Input: 600, Output: 5}).Finish().Events()

	r := newRig(t, overflow, summary, final)
	// Three prior turns so there is a head to summarize after keeping the tail.
	r.seedTurn(t, "msg_0001", "msg_0002", "first task", 1)
	r.seedTurn(t, "msg_0003", "msg_0004", "second task", 3)
	r.seedTurn(t, "msg_0005", "msg_0006", "third task", 5)

	out := r.prompt(t, "fourth task")

	if out.Info.Assistant == nil || out.Info.Assistant.Finish != "stop" {
		t.Fatalf("final assistant should finish stop: %+v", out.Info.Assistant)
	}
	if r.mock.Calls() != 3 {
		t.Fatalf("want 3 provider calls (overflow turn, summary, resume), got %d", r.mock.Calls())
	}

	events := r.drain()
	if countType(events, "session.compacted") != 1 {
		t.Fatalf("want 1 session.compacted event, got %d", countType(events, "session.compacted"))
	}

	// A summary:true assistant message must exist, and the compaction part must
	// carry a tail_start_id.
	msgs, _ := r.store.List(context.Background(), r.sessionID)
	var sawSummary, sawTail bool
	for _, m := range msgs {
		if a := m.Info.Assistant; a != nil && a.Summary {
			sawSummary = true
		}
		for _, p := range m.Parts {
			if c, ok := p.(*message.CompactionPart); ok && c.TailStartID != "" {
				sawTail = true
			}
		}
	}
	if !sawSummary {
		t.Fatalf("no summary:true assistant message found")
	}
	if !sawTail {
		t.Fatalf("compaction part has no tail_start_id")
	}
}

// TestSummarize_AutoFlag verifies that the request body's auto flag is forwarded
// through SummarizeInput into the emitted CompactionPart.auto, matching opencode
// (handlers/session.ts:280 auto: ctx.payload.auto ?? false → compaction.create,
// surfaced as the required CompactionPart.auto boolean, message-v2.ts:187).
func TestSummarize_AutoFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		auto bool
	}{
		{"auto_true", true},
		{"default_false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := NewScript().StepStart().Text("s1", "## Goal\n- summarized").
				StepFinish("stop", llm.TokenUsage{Input: 500, Output: 50}).Finish().Events()
			final := NewScript().StepStart().Text("f1", "All done.").
				StepFinish("stop", llm.TokenUsage{Input: 600, Output: 5}).Finish().Events()

			r := newRig(t, summary, final)
			r.seedTurn(t, "msg_0001", "msg_0002", "first task", 1)
			r.seedTurn(t, "msg_0003", "msg_0004", "second task", 3)

			if err := r.eng.Summarize(context.Background(), engine.SummarizeInput{
				SessionID: r.sessionID, Provider: "openai", Model: "gpt-4o", Auto: tc.auto,
			}); err != nil {
				t.Fatalf("summarize: %v", err)
			}

			msgs, _ := r.store.List(context.Background(), r.sessionID)
			var found bool
			for _, m := range msgs {
				for _, p := range m.Parts {
					if c, ok := p.(*message.CompactionPart); ok {
						found = true
						if c.Auto != tc.auto {
							t.Fatalf("CompactionPart.auto = %v, want %v", c.Auto, tc.auto)
						}
					}
				}
			}
			if !found {
				t.Fatalf("no compaction part found")
			}
		})
	}
}
