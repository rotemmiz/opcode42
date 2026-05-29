package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/instance"
)

// streamContext derives a context for a long-lived stream that is cancelled when
// EITHER the client disconnects (r.Context()) OR the server begins shutting down
// (base). This lets graceful shutdown unblock SSE/PTY handlers promptly so
// http.Server.Shutdown can drain (plan 01 §9).
func streamContext(base context.Context, r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	if base != nil {
		go func() {
			select {
			case <-base.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}

// heartbeatInterval matches opencode's 10s SSE keep-alive (handlers/event.ts:32).
const heartbeatInterval = 10 * time.Second

// setSSEHeaders writes the exact headers opencode uses for its event streams
// (handlers/event.ts:46-50).
func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
}

// writeSSE marshals v and writes one SSE frame (event name "message"), then
// flushes. It returns false if the client has gone away.
func writeSSE(w http.ResponseWriter, fl http.Flusher, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data); err != nil {
		return false
	}
	fl.Flush()
	return true
}

// instanceEventHandler streams the per-directory instance bus as bare
// {id,type,properties} events: server.connected first, then live events merged
// with a 10s heartbeat, terminating on server.instance.disposed
// (handlers/event.ts).
func instanceEventHandler(base context.Context, mgr *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		inst := mgr.Get(DirectoryFromContext(r.Context()))
		// Subscribe BEFORE sending server.connected so no publish is lost in the
		// connect window (handlers/event.ts:23-27).
		events, unsubscribe := inst.Bus.Subscribe()
		defer unsubscribe()

		setSSEHeaders(w)
		w.WriteHeader(http.StatusOK)
		if !writeSSE(w, fl, bus.NewEvent(bus.EventConnected, nil)) {
			return
		}

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		ctx, cancel := streamContext(base, r)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !writeSSE(w, fl, bus.NewEvent(bus.EventHeartbeat, nil)) {
					return
				}
			case e, ok := <-events:
				if !ok {
					return
				}
				if !writeSSE(w, fl, e) {
					return
				}
				if e.Type == bus.EventInstanceDisposed {
					return
				}
			}
		}
	}
}

// globalEventHandler streams the process-global bus. Unlike the instance
// stream, each event is wrapped in a {payload,...} envelope (handlers/global.ts;
// conformance Finding #2).
func globalEventHandler(base context.Context, global *bus.Global) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		events, unsubscribe := global.Subscribe()
		defer unsubscribe()

		setSSEHeaders(w)
		w.WriteHeader(http.StatusOK)
		if !writeSSE(w, fl, bus.GlobalEvent{Payload: bus.NewEvent(bus.EventConnected, nil)}) {
			return
		}

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		ctx, cancel := streamContext(base, r)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !writeSSE(w, fl, bus.GlobalEvent{Payload: bus.NewEvent(bus.EventHeartbeat, nil)}) {
					return
				}
			case e, ok := <-events:
				if !ok {
					return
				}
				if !writeSSE(w, fl, e) {
					return
				}
			}
		}
	}
}
