package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/pty"
)

// wsWriteTimeout bounds a single frame write so a client that stops reading
// (TCP backpressure) cannot block the writer/reader goroutines indefinitely.
const wsWriteTimeout = 30 * time.Second

// ptyConnectHandler upgrades GET /pty/{ptyID}/connect to a WebSocket and bridges
// it to the PTY session: it replays the buffer, sends the control frame, streams
// live output as text frames, and writes client input back to the shell
// (pty/index.ts:301-361; handlers/pty.ts ptyConnectHandlers).
func ptyConnectHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := chi.URLParam(r, "ptyID")
		mgr := ptyManager(instances, r)

		// 404 before the upgrade if the session is unknown.
		if _, err := mgr.Get(pid); errors.Is(err, pty.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// A ?ticket= is validated here (Basic auth was bypassed for it upstream).
		if ticket := r.URL.Query().Get("ticket"); ticket != "" {
			if !mgr.ConsumeTicket(ticket, pid) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		initial, sub, err := mgr.Connect(pid, parseCursor(r.URL.Query().Get("cursor")))
		if err != nil {
			_ = conn.Close(websocket.StatusCode(4404), "session not found")
			return
		}
		defer sub.Close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Replay + control frame, in order, before live streaming.
		for _, f := range initial {
			if !wsWrite(ctx, conn, f) {
				return
			}
		}

		// Reader pump: client input → PTY. Cancels everything on read error/close.
		go func() {
			defer cancel()
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				sub.Write(data)
			}
		}()

		// Writer pump: live PTY output → client.
		for {
			select {
			case <-ctx.Done():
				return
			case f, ok := <-sub.Live():
				if !ok {
					return
				}
				if !wsWrite(ctx, conn, f) {
					return
				}
			}
		}
	}
}

// wsWrite sends one frame: binary for the control frame, text for PTY output
// (pty/index.ts: data chunks are strings, meta is a Uint8Array). Each write is
// bounded by wsWriteTimeout so a stalled client cannot block forever. Returns
// false when the connection is gone (or the write timed out).
func wsWrite(ctx context.Context, conn *websocket.Conn, f pty.Frame) bool {
	typ := websocket.MessageText
	if f.Binary {
		typ = websocket.MessageBinary
	}
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return conn.Write(wctx, typ, f.Data) == nil
}

// parseCursor maps the ?cursor= query to opencode's semantics: -1 = current end,
// >=0 = absolute code-unit offset, anything missing/invalid/<-1 = 0 (start).
func parseCursor(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < -1 {
		return 0
	}
	return n
}
