package question

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
)

func TestAskReply_Answer(t *testing.T) {
	b := bus.NewInstanceBus("ses_1", nil)
	sub, _ := b.Subscribe()
	m := NewManager(b)

	done := make(chan struct {
		ans string
		err error
	}, 1)
	go func() {
		ans, err := m.Ask(context.Background(), "ses_1", "color?", []string{"red", "blue"})
		done <- struct {
			ans string
			err error
		}{ans, err}
	}()

	// The asked event carries the id we reply to.
	var ev bus.Event
	select {
	case ev = <-sub:
	case <-time.After(time.Second):
		t.Fatal("no question.asked event")
	}
	if ev.Type != "question.asked" {
		t.Fatalf("event type = %s", ev.Type)
	}
	req := ev.Properties.(Request)
	if err := m.Reply(req.ID, "blue", false); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.err != nil || res.ans != "blue" {
		t.Fatalf("ask result = %q err %v", res.ans, res.err)
	}
}

func TestReply_Reject(t *testing.T) {
	m := NewManager(nil)
	done := make(chan error, 1)
	go func() {
		_, err := m.Ask(context.Background(), "ses_1", "ok?", nil)
		done <- err
	}()

	id := waitForQuestion(t, m)
	if err := m.Reply(id, "", true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrRejected) {
		t.Fatalf("want ErrRejected, got %v", err)
	}
}

// waitForQuestion polls until a question is registered and returns its id.
func waitForQuestion(t *testing.T, m *Manager) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if list := m.List(); len(list) > 0 {
			return list[0].ID
		}
		select {
		case <-deadline:
			t.Fatal("question never registered")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestAsk_ContextCancel(t *testing.T) {
	m := NewManager(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := m.Ask(ctx, "ses_1", "?", nil)
		done <- err
	}()
	for len(m.List()) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if len(m.List()) != 0 {
		t.Fatalf("pending not cleaned up after cancel")
	}
}

func TestReply_Unknown(t *testing.T) {
	m := NewManager(nil)
	if err := m.Reply("nope", "x", false); !errors.Is(err, ErrUnknown) {
		t.Fatalf("want ErrUnknown, got %v", err)
	}
}
