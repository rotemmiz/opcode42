package pluginbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// errClosed is returned by call/notify once the connection's read loop has
// terminated (host crashed, socket closed, or Close was invoked).
var errClosed = errors.New("pluginbridge: connection closed")

// notifyHandler receives an inbound notification from the host (no response).
type notifyHandler func(method string, params json.RawMessage)

// conn is a JSON-RPC 2.0 client/peer over a single duplex stream (a unix-socket
// connection or an stdio pipe pair). It multiplexes concurrent requests by
// numeric id, fans inbound notifications to onNotify, and tears down all
// pending calls when the read loop exits so callers never block forever.
type conn struct {
	rw     io.ReadWriteCloser
	log    *slog.Logger
	writeM sync.Mutex

	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan rpcResponse
	closed  bool

	onNotify notifyHandler
	done     chan struct{}
}

// newConn starts the read loop for rw. onNotify may be nil if the caller does
// not consume host-initiated notifications.
func newConn(rw io.ReadWriteCloser, log *slog.Logger, onNotify notifyHandler) *conn {
	if log == nil {
		log = slog.Default()
	}
	c := &conn{
		rw:       rw,
		log:      log,
		pending:  make(map[uint64]chan rpcResponse),
		onNotify: onNotify,
		done:     make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Done is closed when the read loop exits (the host went away or Close ran).
func (c *conn) Done() <-chan struct{} { return c.done }

func (c *conn) readLoop() {
	defer c.teardown()
	for {
		body, err := readFrame(c.rw)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				c.log.Debug("plugin host read loop ended", "err", err)
			}
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			c.log.Warn("plugin host sent malformed frame", "err", err)
			continue
		}
		switch {
		case msg.Method != "" && msg.ID == nil:
			// Notification from host → fan out (non-blocking dispatch).
			if c.onNotify != nil {
				c.onNotify(msg.Method, msg.Params)
			}
		case msg.Method != "" && msg.ID != nil:
			// The host does not currently call back into Go over this channel
			// (tool ask/metadata go over HTTP per plan 05); reply with an error
			// rather than leaving the host hanging.
			c.respondUnsupported(*msg.ID, msg.Method)
		default:
			// Response to one of our requests.
			c.deliver(rpcResponse{ID: idOrZero(msg.ID), Result: msg.Result, Error: msg.Error})
		}
	}
}

func idOrZero(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

func (c *conn) deliver(resp rpcResponse) {
	c.mu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()
	if ok {
		ch <- resp
	}
}

func (c *conn) respondUnsupported(id uint64, method string) {
	_ = c.writeFrame(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not supported: %s", method)},
	})
}

// teardown marks the connection closed and fails every pending request.
func (c *conn) teardown() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	pending := c.pending
	c.pending = make(map[uint64]chan rpcResponse)
	c.mu.Unlock()

	for id, ch := range pending {
		ch <- rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: errClosed.Error()}}
	}
	close(c.done)
}

func (c *conn) writeFrame(v any) error {
	c.writeM.Lock()
	defer c.writeM.Unlock()
	return writeFrame(c.rw, v)
}

// call sends a request and waits for the matching response or ctx cancellation.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := marshalParams(params)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	if err := c.writeFrame(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// notify sends a fire-and-forget notification (no id, no response).
func (c *conn) notify(method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errClosed
	}
	return c.writeFrame(rpcRequest{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *conn) Close() error {
	c.mu.Lock()
	already := c.closed
	c.mu.Unlock()
	err := c.rw.Close()
	if !already {
		// readLoop will observe the closed stream and run teardown; wait briefly
		// so callers can rely on pending requests being drained.
		<-c.done
	}
	return err
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return raw, nil
}
