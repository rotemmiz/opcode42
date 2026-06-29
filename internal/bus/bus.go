package bus

import (
	"sync"

	"github.com/rotemmiz/opcode42/internal/id"
)

// Event is the payload published on the bus and streamed to SSE clients,
// matching opencode's shape exactly (bus/index.ts:24-28): {id,type,properties}.
type Event struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Properties any    `json:"properties"`
}

// Well-known event types emitted by the transport itself.
const (
	// EventConnected is sent first on every SSE stream (handlers/event.ts:38).
	EventConnected = "server.connected"
	// EventHeartbeat is emitted every 10s to keep the stream alive.
	EventHeartbeat = "server.heartbeat"
	// EventInstanceDisposed terminates an instance /event stream
	// (bus/index.ts:17-22; handlers/event.ts:30-31).
	EventInstanceDisposed = "server.instance.disposed"
)

// NewEvent builds an Event with a fresh ascending evt_ id. A nil props
// marshals as {} (never null), matching opencode's empty-properties events.
func NewEvent(typ string, props any) Event {
	if props == nil {
		props = map[string]any{}
	}
	return Event{ID: id.Ascending(id.Event), Type: typ, Properties: props}
}

// subBuffer is each subscriber's channel depth. A slow subscriber that fills it
// has further events dropped (non-blocking publish) rather than stalling the bus.
const subBuffer = 256

// Bus is a per-instance publish/subscribe fan-out. Every publish also forwards
// to the process-global bus, wrapped with this instance's directory
// (bus/index.ts:100-119).
type Bus struct {
	mu        sync.Mutex
	subs      map[int]chan Event
	next      int
	directory string
	global    *Global
}

// NewInstanceBus creates an instance bus bound to directory; published events
// are also forwarded to global (may be nil).
func NewInstanceBus(directory string, global *Global) *Bus {
	return &Bus{subs: make(map[int]chan Event), directory: directory, global: global}
}

// Subscribe registers a subscriber and returns its receive channel plus an
// unsubscribe func that closes the channel. The subscription is acquired before
// the caller sends server.connected, closing the subscribe-before-publish race
// (handlers/event.ts:23-27).
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.next
	b.next++
	ch := make(chan Event, subBuffer)
	b.subs[key] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[key]; ok {
			delete(b.subs, key)
			close(c)
		}
	}
}

// Publish fans an event out to instance subscribers (non-blocking; a full
// subscriber channel drops the event) and forwards it to the global bus.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop rather than block the publisher
		}
	}
	b.mu.Unlock()

	if b.global != nil {
		b.global.Publish(GlobalEvent{Payload: e, Directory: b.directory})
	}
}

// GlobalEvent is the envelope sent on /global/event: the bus payload plus the
// optional origin (directory/project/workspace). The global SSE stream sends
// this whole wrapper — note the instance stream sends only the bare payload
// (bus/global.ts:5-8; conformance Finding #2, locked by TestSSECatalog).
type GlobalEvent struct {
	Payload   Event  `json:"payload"`
	Directory string `json:"directory,omitempty"`
	Project   string `json:"project,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

// Global is the process-level fan-out backing /global/event.
type Global struct {
	mu   sync.Mutex
	subs map[int]chan GlobalEvent
	next int
	// clients counts active client SSE connections (instance + global /event
	// streams). The push relay subscribes to the global bus directly (which does
	// NOT increment this counter), so it can read clients to decide whether a
	// client is actively connected — push is only sent when clients == 0
	// (plan 13 §13.8: "Push covers it when the client is backgrounded or
	// offline").
	clients int
}

// NewGlobal creates an empty global bus.
func NewGlobal() *Global {
	return &Global{subs: make(map[int]chan GlobalEvent)}
}

// ClientConnected increments the active-client counter; the returned func
// decrements it (call once, e.g. via defer). SSE handlers wrap a client stream
// in this so the push relay can tell whether any client is currently connected.
func (g *Global) ClientConnected() func() {
	g.mu.Lock()
	g.clients++
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.clients--
			g.mu.Unlock()
		})
	}
}

// Clients returns the number of active client SSE connections.
func (g *Global) Clients() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.clients
}

// Subscribe registers a global subscriber and returns its channel + unsubscribe.
func (g *Global) Subscribe() (<-chan GlobalEvent, func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := g.next
	g.next++
	ch := make(chan GlobalEvent, subBuffer)
	g.subs[key] = ch
	return ch, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if c, ok := g.subs[key]; ok {
			delete(g.subs, key)
			close(c)
		}
	}
}

// Publish fans a global event out to all subscribers (non-blocking).
func (g *Global) Publish(ge GlobalEvent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, ch := range g.subs {
		select {
		case ch <- ge:
		default:
		}
	}
}
