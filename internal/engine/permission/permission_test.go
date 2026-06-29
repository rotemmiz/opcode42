package permission

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/bus"
)

func TestEvaluate_DefaultAsk(t *testing.T) {
	r := Evaluate("bash", "git status")
	if r.Action != ActionAsk {
		t.Fatalf("empty ruleset should default to ask, got %s", r.Action)
	}
}

func TestEvaluate_LastMatchWins(t *testing.T) {
	rs := Ruleset{
		{Permission: "bash", Pattern: "*", Action: ActionAllow},
		{Permission: "bash", Pattern: "rm *", Action: ActionDeny},
	}
	if got := Evaluate("bash", "rm -rf /", rs).Action; got != ActionDeny {
		t.Fatalf("want deny (last match), got %s", got)
	}
	if got := Evaluate("bash", "ls", rs).Action; got != ActionAllow {
		t.Fatalf("want allow, got %s", got)
	}
}

func TestEvaluate_AcrossRulesetsOrder(t *testing.T) {
	base := Ruleset{{Permission: "*", Pattern: "*", Action: ActionDeny}}
	override := Ruleset{{Permission: "read", Pattern: "*", Action: ActionAllow}}
	if got := Evaluate("read", "/etc/hosts", base, override).Action; got != ActionAllow {
		t.Fatalf("later ruleset should win, got %s", got)
	}
	if got := Evaluate("bash", "x", base, override).Action; got != ActionDeny {
		t.Fatalf("want deny, got %s", got)
	}
}

func TestEvaluate_WildcardPatterns(t *testing.T) {
	rs := Ruleset{{Permission: "bash", Pattern: "git *", Action: ActionAllow}}
	if Evaluate("bash", "git status", rs).Action != ActionAllow {
		t.Fatal("git * should match git status")
	}
	if Evaluate("bash", "rm -rf", rs).Action != ActionAsk {
		t.Fatal("non-matching pattern should default ask")
	}
}

func TestManager_AllowShortCircuits(t *testing.T) {
	m := NewManager(nil)
	err := m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "read", Patterns: []string{"/x"},
		Rulesets: []Ruleset{{{Permission: "read", Pattern: "*", Action: ActionAllow}}}})
	if err != nil {
		t.Fatalf("pre-allowed ask should not block: %v", err)
	}
}

func TestManager_DenyImmediately(t *testing.T) {
	m := NewManager(nil)
	var denied *DeniedError
	err := m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"rm -rf /"},
		Rulesets: []Ruleset{{{Permission: "bash", Pattern: "rm *", Action: ActionDeny}}}})
	if !errors.As(err, &denied) {
		t.Fatalf("want DeniedError, got %v", err)
	}
}

func TestManager_AskBlocksUntilReplyOnce(t *testing.T) {
	b := bus.NewInstanceBus("s", nil)
	sub, _ := b.Subscribe()
	m := NewManager(b)
	done := make(chan error, 1)
	go func() {
		done <- m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"ls"}})
	}()
	ev := <-sub
	if ev.Type != "permission.asked" {
		t.Fatalf("event = %s", ev.Type)
	}
	reqID := ev.Properties.(Request).ID
	if err := m.Reply(reqID, ReplyOnce); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("once reply should allow: %v", err)
	}
}

func TestManager_AlwaysPersistsAndUnblocksOthers(t *testing.T) {
	m := NewManager(nil)
	// First ask for "git status" pending.
	done1 := make(chan error, 1)
	go func() {
		done1 <- m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash",
			Patterns: []string{"git status"}, Always: []string{"git *"}})
	}()
	id1 := waitPending(t, m, "s")

	// Second concurrent ask for "git log" — should unblock when we reply always to #1.
	done2 := make(chan error, 1)
	go func() {
		done2 <- m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"git log"}})
	}()
	waitCount(t, m, "s", 2)

	if err := m.Reply(id1, ReplyAlways); err != nil {
		t.Fatal(err)
	}
	if err := <-done1; err != nil {
		t.Fatalf("always reply should allow #1: %v", err)
	}
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("#2 should be unblocked by the git* grant: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("#2 was not unblocked by the always grant")
	}
}

func TestManager_RejectCascades(t *testing.T) {
	m := NewManager(nil)
	done1 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() {
		done1 <- m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"a"}})
	}()
	waitPending(t, m, "s")
	go func() {
		done2 <- m.Ask(context.Background(), AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"b"}})
	}()
	waitCount(t, m, "s", 2)

	// Reject the first; the second (same session) must cascade-reject.
	id := m.List()[0].ID
	if err := m.Reply(id, ReplyReject); err != nil {
		t.Fatal(err)
	}
	var d1, d2 *DeniedError
	if err := <-done1; !errors.As(err, &d1) {
		t.Fatalf("#1 want DeniedError, got %v", err)
	}
	select {
	case err := <-done2:
		if !errors.As(err, &d2) {
			t.Fatalf("#2 want cascaded DeniedError, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("#2 was not cascade-rejected")
	}
}

func TestManager_ContextCancel(t *testing.T) {
	m := NewManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- m.Ask(ctx, AskInput{SessionID: "s", Permission: "bash", Patterns: []string{"x"}})
	}()
	waitPending(t, m, "s")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func waitPending(t *testing.T, m *Manager, _ string) string {
	t.Helper()
	return waitCount(t, m, "", 1)
}

func waitCount(t *testing.T, m *Manager, _ string, n int) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		list := m.List()
		if len(list) >= n {
			return list[0].ID
		}
		select {
		case <-deadline:
			t.Fatalf("expected %d pending, have %d", n, len(list))
		case <-time.After(time.Millisecond):
		}
	}
}
