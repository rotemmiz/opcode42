package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Connection lifecycle messages.
type (
	connectedMsg    struct{}            // health check passed
	connErrMsg      struct{ err error } // terminal connect/auth failure
	streamOpenedMsg struct {            // SSE subscription opened (or failed)
		stream *opcode42client.EventStream
		err    error
	}
	sseEventMsg  struct{ ev opcode42client.SSEEvent } // one streamed event
	sseClosedMsg struct{}                             // the stream ended; reconnect
	reconnectMsg struct{}                             // backoff elapsed; reopen
)

// reconnectBase / reconnectMax bound the exponential backoff (mirrors plan 08 /
// opencode sdk.tsx: 1s..30s).
const (
	reconnectBase = time.Second
	reconnectMax  = 30 * time.Second
)

// healthCmd checks the daemon is reachable+authorized.
func healthCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		if err := c.Health(ctx); err != nil {
			return connErrMsg{err: err}
		}
		return connectedMsg{}
	}
}

// openSSECmd opens the global event stream.
func openSSECmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		s, err := c.GlobalEvents(ctx)
		return streamOpenedMsg{stream: s, err: err}
	}
}

// listenCmd waits for the next event on a stream (re-issued after each event so
// the Bubble Tea loop pulls events one at a time).
func listenCmd(s *opcode42client.EventStream) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-s.Events()
		if !ok {
			return sseClosedMsg{}
		}
		return sseEventMsg{ev: ev}
	}
}

// backoffCmd schedules a reconnect after an exponential delay (clamped to
// reconnectMax). attempt is bounded before the shift so it can't overflow.
func backoffCmd(attempt int) tea.Cmd {
	const maxShift = 5 // reconnectBase<<5 = 32s already exceeds reconnectMax (30s)
	if attempt > maxShift {
		attempt = maxShift
	}
	delay := reconnectBase << attempt
	if delay > reconnectMax {
		delay = reconnectMax
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return reconnectMsg{} })
}
