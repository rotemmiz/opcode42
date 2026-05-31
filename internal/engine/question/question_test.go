package question

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
)

// oneQuestion is a single-question request fixture.
func oneQuestion(text string, labels ...string) []Info {
	opts := make([]Option, len(labels))
	for i, l := range labels {
		opts[i] = Option{Label: l}
	}
	return []Info{{Question: text, Header: text, Options: opts}}
}

func TestAskReply_Answer(t *testing.T) {
	b := bus.NewInstanceBus("ses_1", nil)
	sub, _ := b.Subscribe()
	m := NewManager(b)

	done := make(chan struct {
		ans [][]string
		err error
	}, 1)
	go func() {
		ans, err := m.Ask(context.Background(), "ses_1", oneQuestion("color?", "red", "blue"))
		done <- struct {
			ans [][]string
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
	if len(req.Questions) != 1 || req.Questions[0].Question != "color?" {
		t.Fatalf("asked request = %+v", req)
	}
	if err := m.Reply(req.ID, [][]string{{"blue"}}); err != nil {
		t.Fatal(err)
	}
	res := <-done
	if res.err != nil || !reflect.DeepEqual(res.ans, [][]string{{"blue"}}) {
		t.Fatalf("ask result = %v err %v", res.ans, res.err)
	}
}

func TestReply_Reject(t *testing.T) {
	m := NewManager(nil)
	done := make(chan error, 1)
	go func() {
		_, err := m.Ask(context.Background(), "ses_1", oneQuestion("ok?"))
		done <- err
	}()

	id := waitForQuestion(t, m)
	if err := m.Reject(id); err != nil {
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
		_, err := m.Ask(ctx, "ses_1", oneQuestion("?"))
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
	if err := m.Reply("nope", [][]string{{"x"}}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("want ErrUnknown, got %v", err)
	}
	if err := m.Reject("nope"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("reject want ErrUnknown, got %v", err)
	}
}
