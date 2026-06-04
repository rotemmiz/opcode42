package push

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
)

// rateWindow is the minimum interval between pushes for the same
// (device, session) pair — plan 13 §13.8 "max 1 notification per device per
// session per minute to avoid flooding during rapid agent loops".
const rateWindow = time.Minute

// clientGrace is how long after the last client disconnect the relay still
// suppresses push, absorbing the brief gap during an SSE reconnect so a
// momentary network blip mid-stream doesn't fire a spurious notification.
const clientGrace = 5 * time.Second

// busSource is the slice of the global bus the relay consumes. *bus.Global
// satisfies it; tests use a fake.
type busSource interface {
	Subscribe() (<-chan bus.GlobalEvent, func())
	Clients() int
}

// Relay subscribes to the global event bus, maps push-worthy events to
// notifications, and dispatches them via the Sender to every registered device
// that wants the session — but only when no client is actively connected
// (plan 13 §13.8). When the Sender is nil (no FCM credential configured) the
// relay is a no-op: it logs once at start and never subscribes, so the daemon
// and CI run fine without FCM infrastructure.
type Relay struct {
	store  *Store
	sender Sender
	source busSource
	log    *slog.Logger

	// rate tracks the last send time per (deviceID|sessionID) for rate limiting.
	mu   sync.Mutex
	rate map[string]time.Time

	// now and clients are injectable for tests.
	now func() time.Time
}

// NewRelay builds a relay. sender may be nil (no-op mode). log may be nil
// (defaults to slog.Default()).
func NewRelay(store *Store, sender Sender, source busSource, log *slog.Logger) *Relay {
	if log == nil {
		log = slog.Default()
	}
	return &Relay{
		store:  store,
		sender: sender,
		source: source,
		log:    log,
		rate:   make(map[string]time.Time),
		now:    time.Now,
	}
}

// Enabled reports whether live FCM delivery is configured. When false, device
// registration still works but no push is sent (the dispatcher loop exits).
func (r *Relay) Enabled() bool { return r.sender != nil }

// Run consumes the global bus until ctx is cancelled. When the relay is a no-op
// (no sender) it logs a notice and returns immediately — there is no point
// draining the bus. Run blocks; call it in a goroutine.
func (r *Relay) Run(ctx context.Context) {
	if r.sender == nil {
		r.log.Info("push relay disabled: no FCM service account configured; device registration persists but no push is sent")
		return
	}
	r.log.Info("push relay enabled: dispatching FCM notifications when no client is connected")

	events, unsubscribe := r.source.Subscribe()
	defer unsubscribe()

	// lastDisconnect is set the moment the relay first observes zero clients
	// after having seen one, so an event arriving within clientGrace of an SSE
	// drop (a reconnect in flight) is suppressed rather than pushed.
	var lastDisconnect time.Time
	clientsSeen := false
	for {
		select {
		case <-ctx.Done():
			return
		case ge, ok := <-events:
			if !ok {
				return
			}
			clients := r.source.Clients()
			switch {
			case clients > 0:
				clientsSeen = true
				lastDisconnect = time.Time{}
			case clientsSeen && lastDisconnect.IsZero():
				// First event observed after the last client went away.
				lastDisconnect = r.now()
			}

			n, want := FromEvent(ge.Payload)
			if !want {
				continue
			}
			// Suppress push while a client is connected, plus a short grace after
			// the last disconnect to ride out SSE reconnects.
			if clients > 0 {
				continue
			}
			if !lastDisconnect.IsZero() && r.now().Sub(lastDisconnect) < clientGrace {
				continue
			}
			// Dispatch off the consumer goroutine so a slow/unreachable FCM send
			// (up to the per-send timeout) never blocks bus consumption and stalls
			// other subscribers sharing the global bus (whose buffered channel
			// would otherwise fill and drop events). The rate limiter dedups, so a
			// fresh dispatch per event is cheap.
			go r.dispatch(ctx, n)
		}
	}
}

// dispatch fans one notification out to every registered device that wants the
// session, honouring the per-device-per-session rate limit. Sends run inline
// (the global bus channel is buffered); each FCM call has its own timeout.
func (r *Relay) dispatch(ctx context.Context, n Notification) {
	devices, err := r.store.targets(n.SessionID)
	if err != nil {
		r.log.Warn("push: list target devices failed", "error", err)
		return
	}
	for _, d := range devices {
		if !r.allow(d.DeviceID, n.SessionID) {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := r.sender.Send(sendCtx, d.FCMToken, n)
		cancel()
		switch {
		case err == nil:
			r.log.Debug("push: sent", "device", d.DeviceID, "event", n.EventType, "session", n.SessionID)
		case errors.Is(err, errUnregistered):
			r.log.Info("push: pruning unregistered token", "device", d.DeviceID)
			r.store.removeByToken(d.FCMToken)
		default:
			r.log.Warn("push: send failed", "device", d.DeviceID, "error", err)
		}
	}
}

// allow enforces the per-(device,session) rate window. It returns true and
// records the send time when a push is permitted. It also evicts rate entries
// older than the window so the map does not grow without bound on a long-lived
// daemon that serves many sessions (a stale entry is past its window anyway, so
// dropping it cannot suppress a future push).
func (r *Relay) allow(deviceID, sessionID string) bool {
	key := deviceID + "|" + sessionID
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, last := range r.rate {
		if now.Sub(last) >= rateWindow {
			delete(r.rate, k)
		}
	}
	if last, ok := r.rate[key]; ok && now.Sub(last) < rateWindow {
		return false
	}
	r.rate[key] = now
	return true
}
