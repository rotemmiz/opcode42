package runstate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/engine/message"
)

func TestEnsureRunning_RunsWork(t *testing.T) {
	rs := New()
	want := message.WithParts{Info: message.Info{User: &message.UserMessage{ID: "msg_1"}}}
	got, err := rs.EnsureRunning(context.Background(), "ses_1", func(context.Context) (message.WithParts, error) {
		return want, nil
	})
	if err != nil || got.Info.ID() != "msg_1" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if rs.Busy("ses_1") {
		t.Fatalf("session should be idle after completion")
	}
}

func TestEnsureRunning_Idempotent(t *testing.T) {
	rs := New()
	var runs int32
	release := make(chan struct{})
	work := func(context.Context) (message.WithParts, error) {
		atomic.AddInt32(&runs, 1)
		<-release
		return message.WithParts{Info: message.Info{User: &message.UserMessage{ID: "msg_1"}}}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rs.EnsureRunning(context.Background(), "ses_1", work)
		}()
	}
	// Give the goroutines time to coalesce onto one run.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := atomic.LoadInt32(&runs); n != 1 {
		t.Fatalf("work ran %d times, want 1 (idempotent)", n)
	}
}

func TestCancel_InterruptsRun(t *testing.T) {
	rs := New()
	started := make(chan struct{})
	work := func(ctx context.Context) (message.WithParts, error) {
		close(started)
		<-ctx.Done()
		return message.WithParts{}, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := rs.EnsureRunning(context.Background(), "ses_1", work)
		done <- err
	}()
	<-started
	rs.Cancel("ses_1")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt the run")
	}
}

// TestEnsureRunning_HooksFireOncePerRun verifies OnBusy fires before work and
// OnIdle after it completes, exactly once each, for the run that actually starts.
func TestEnsureRunning_HooksFireOncePerRun(t *testing.T) {
	rs := New()
	var busy, idle int32
	var order []string
	var mu sync.Mutex
	record := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }

	_, err := rs.EnsureRunning(context.Background(), "ses_1",
		func(context.Context) (message.WithParts, error) {
			record("work")
			return message.WithParts{}, nil
		},
		Hooks{
			OnBusy: func() { atomic.AddInt32(&busy, 1); record("busy") },
			OnIdle: func() { atomic.AddInt32(&idle, 1); record("idle") },
		})
	if err != nil {
		t.Fatalf("EnsureRunning err: %v", err)
	}
	if b, i := atomic.LoadInt32(&busy), atomic.LoadInt32(&idle); b != 1 || i != 1 {
		t.Fatalf("hooks fired busy=%d idle=%d, want 1/1", b, i)
	}
	mu.Lock()
	got := strings.Join(order, ",")
	mu.Unlock()
	if got != "busy,work,idle" {
		t.Fatalf("hook order = %q, want busy,work,idle", got)
	}
}

// TestEnsureRunning_HooksNotReFiredForCoalesced verifies a caller that joins an
// in-flight run does not re-fire busy/idle (only the run that starts owns them).
func TestEnsureRunning_HooksNotReFiredForCoalesced(t *testing.T) {
	rs := New()
	var busy int32
	release := make(chan struct{})
	work := func(context.Context) (message.WithParts, error) {
		<-release
		return message.WithParts{}, nil
	}
	hooks := Hooks{OnBusy: func() { atomic.AddInt32(&busy, 1) }}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rs.EnsureRunning(context.Background(), "ses_1", work, hooks)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if b := atomic.LoadInt32(&busy); b != 1 {
		t.Fatalf("OnBusy fired %d times, want 1 (coalesced callers must not re-fire)", b)
	}
}

func TestAssertNotBusy(t *testing.T) {
	rs := New()
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = rs.EnsureRunning(context.Background(), "ses_1", func(context.Context) (message.WithParts, error) {
			close(started)
			<-release
			return message.WithParts{}, nil
		})
	}()
	<-started
	var busyErr *BusyError
	if err := rs.AssertNotBusy("ses_1"); !errors.As(err, &busyErr) {
		t.Fatalf("want BusyError, got %v", err)
	}
	if err := rs.AssertNotBusy("ses_other"); err != nil {
		t.Fatalf("idle session should not be busy: %v", err)
	}
	close(release)
}
