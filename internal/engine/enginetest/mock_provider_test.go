package enginetest

import (
	"context"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

func drain(t *testing.T, ch <-chan llm.Event) []llm.Event {
	t.Helper()
	var out []llm.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestMockProvider_ReplaysScriptAndAdvances(t *testing.T) {
	first := NewScript().StepStart().Text("t1", "hello", " world").StepFinish("stop", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events()
	second := NewScript().Text("t2", "again").Finish().Events()
	mp := NewMockProvider(first, second)

	got := drain(t, mustStream(t, mp))
	if len(got) != len(first) || got[0].Type != llm.EventStepStart {
		t.Fatalf("first stream mismatch: %+v", got)
	}
	got2 := drain(t, mustStream(t, mp))
	if len(got2) != len(second) || got2[1].Text != "again" {
		t.Fatalf("second stream mismatch: %+v", got2)
	}
	// Third call repeats the last script.
	got3 := drain(t, mustStream(t, mp))
	if len(got3) != len(second) {
		t.Fatalf("third stream should repeat last script, got %+v", got3)
	}
	if mp.Calls() != 3 || len(mp.Requests()) != 3 {
		t.Fatalf("want 3 calls recorded, got calls=%d reqs=%d", mp.Calls(), len(mp.Requests()))
	}
}

func TestMockProvider_RespectsContextCancel(t *testing.T) {
	mp := NewMockProvider(NewScript().Text("t", "a", "b", "c").Events())
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := mp.Stream(ctx, &llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// Drain should terminate (channel closes) without deadlock.
	for range ch { //nolint:revive // intentional drain
	}
}

func mustStream(t *testing.T, mp *MockProvider) <-chan llm.Event {
	t.Helper()
	ch, err := mp.Stream(context.Background(), &llm.Request{Model: "mock"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	return ch
}
