package push

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/bus"
)

// fakeSender records every Send and can be told to report a token unregistered.
type fakeSender struct {
	mu            sync.Mutex
	sends         []sendRecord
	unregistered  map[string]bool
	failTransient bool
}

type sendRecord struct {
	token string
	n     Notification
}

func (f *fakeSender) Send(_ context.Context, token string, n Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, sendRecord{token, n})
	if f.unregistered[token] {
		return errUnregistered
	}
	if f.failTransient {
		return context.DeadlineExceeded
	}
	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

// fakeSource is a controllable busSource.
type fakeSource struct {
	ch      chan bus.GlobalEvent
	clients int32
}

func newFakeSource() *fakeSource { return &fakeSource{ch: make(chan bus.GlobalEvent, 16)} }

func (s *fakeSource) Subscribe() (<-chan bus.GlobalEvent, func()) {
	return s.ch, func() {}
}
func (s *fakeSource) Clients() int           { return int(atomic.LoadInt32(&s.clients)) }
func (s *fakeSource) setClients(n int)       { atomic.StoreInt32(&s.clients, int32(n)) }
func (s *fakeSource) emit(e bus.GlobalEvent) { s.ch <- e }

func idleEvent(session string) bus.GlobalEvent {
	return bus.GlobalEvent{Payload: bus.NewEvent("session.idle", map[string]any{"sessionID": session})}
}

// waitFor polls until cond is true or the deadline passes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestRelayNoopWhenNoSender(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "t1"})
	src := newFakeSource()
	r := NewRelay(store, nil, src, nil)
	if r.Enabled() {
		t.Fatal("relay with nil sender should be disabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	// Run must return immediately in no-op mode (no draining of the bus).
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return in no-op mode")
	}
}

func TestRelaySendsWhenNoClient(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "t1"})
	src := newFakeSource()
	src.setClients(0)
	sender := &fakeSender{}
	r := NewRelay(store, sender, src, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	src.emit(idleEvent("ses_1"))
	waitFor(t, func() bool { return sender.count() == 1 })

	rec := sender.sends[0]
	if rec.token != "t1" || rec.n.SessionID != "ses_1" {
		t.Fatalf("unexpected send: %+v", rec)
	}
}

func TestRelaySuppressesWhenClientConnected(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "t1"})
	src := newFakeSource()
	src.setClients(1) // a client is actively connected
	sender := &fakeSender{}
	r := NewRelay(store, sender, src, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	src.emit(idleEvent("ses_1"))
	// Give the relay a moment; it must NOT send while a client is connected.
	time.Sleep(50 * time.Millisecond)
	if sender.count() != 0 {
		t.Fatalf("relay sent %d while client connected; want 0", sender.count())
	}
}

func TestRelayRateLimitsPerDeviceSession(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "t1"})
	src := newFakeSource()
	sender := &fakeSender{}
	r := NewRelay(store, sender, src, nil)

	var fakeNow atomic.Int64
	fakeNow.Store(time.Now().UnixNano())
	r.now = func() time.Time { return time.Unix(0, fakeNow.Load()) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	src.emit(idleEvent("ses_1"))
	waitFor(t, func() bool { return sender.count() == 1 })

	// Second idle within the rate window: suppressed.
	src.emit(idleEvent("ses_1"))
	time.Sleep(50 * time.Millisecond)
	if sender.count() != 1 {
		t.Fatalf("rate limit failed: count = %d, want 1", sender.count())
	}

	// Advance past the window: next idle sends again.
	fakeNow.Add(int64(rateWindow + time.Second))
	src.emit(idleEvent("ses_1"))
	waitFor(t, func() bool { return sender.count() == 2 })
}

func TestRelayPrunesUnregisteredToken(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "dead"})
	src := newFakeSource()
	sender := &fakeSender{unregistered: map[string]bool{"dead": true}}
	r := NewRelay(store, sender, src, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	src.emit(idleEvent("ses_1"))
	waitFor(t, func() bool {
		devices, _ := store.List()
		return len(devices) == 0
	})
}

func TestRelayIgnoresNonPushEvents(t *testing.T) {
	store := testStore(t)
	_ = store.Register(Device{DeviceID: "d1", FCMToken: "t1"})
	src := newFakeSource()
	sender := &fakeSender{}
	r := NewRelay(store, sender, src, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	src.emit(bus.GlobalEvent{Payload: bus.NewEvent("message.updated", map[string]any{"sessionID": "ses_1"})})
	// Then a real one to create a barrier we can wait on.
	src.emit(idleEvent("ses_1"))
	waitFor(t, func() bool { return sender.count() == 1 })
	if sender.sends[0].n.EventType != "session.idle" {
		t.Fatalf("first send should be session.idle, got %s", sender.sends[0].n.EventType)
	}
}
