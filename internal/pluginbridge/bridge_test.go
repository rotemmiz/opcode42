package pluginbridge

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeHost is an in-process JSON-RPC peer standing in for the Node/Bun sidecar.
// It speaks the same length-prefixed framing over a net.Pipe end and lets each
// test script how it answers plugin.trigger:* requests.
type fakeHost struct {
	rw      io.ReadWriteCloser
	mu      sync.Mutex
	onTrig  func(method string, params json.RawMessage) (json.RawMessage, *rpcError, bool)
	notifs  chan rpcRequest
	stopped chan struct{}
}

func newFakeHost(rw io.ReadWriteCloser) *fakeHost {
	h := &fakeHost{rw: rw, notifs: make(chan rpcRequest, 16), stopped: make(chan struct{})}
	go h.loop()
	return h
}

func (h *fakeHost) loop() {
	defer close(h.stopped)
	for {
		body, err := readFrame(h.rw)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		if msg.ID == nil {
			h.notifs <- rpcRequest{Method: msg.Method, Params: msg.Params}
			continue
		}
		h.mu.Lock()
		fn := h.onTrig
		h.mu.Unlock()
		var (
			result  json.RawMessage
			rpcErr  *rpcError
			handled bool
		)
		if fn != nil {
			result, rpcErr, handled = fn(msg.Method, msg.Params)
		}
		if !handled {
			rpcErr = &rpcError{Code: -32601, Message: "unhandled"}
		}
		_ = writeFrame(h.rw, rpcResponse{JSONRPC: "2.0", ID: *msg.ID, Result: result, Error: rpcErr})
	}
}

func (h *fakeHost) handle(fn func(method string, params json.RawMessage) (json.RawMessage, *rpcError, bool)) {
	h.mu.Lock()
	h.onTrig = fn
	h.mu.Unlock()
}

func (h *fakeHost) sendReady(t *testing.T) {
	t.Helper()
	if err := writeFrame(h.rw, rpcRequest{JSONRPC: "2.0", Method: "host.ready", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("send ready: %v", err)
	}
}

func (h *fakeHost) sendNotify(t *testing.T, method string, params any) {
	t.Helper()
	raw, _ := json.Marshal(params)
	if err := writeFrame(h.rw, rpcRequest{JSONRPC: "2.0", Method: method, Params: raw}); err != nil {
		t.Fatalf("send notify: %v", err)
	}
}

// connectBridge wires an enabled Bridge to a fakeHost over net.Pipe, bypassing
// process spawn (which needs bun/node). Returns the bridge and host.
func connectBridge(t *testing.T, cfg Config) (*Bridge, *fakeHost) {
	t.Helper()
	cfg.Enabled = true
	b := New(cfg)
	goSide, hostSide := net.Pipe()
	host := newFakeHost(hostSide)
	c := newConn(goSide, b.log, b.onNotify)
	b.mu.Lock()
	b.conn = c
	b.mu.Unlock()
	t.Cleanup(func() {
		_ = c.Close()
		_ = hostSide.Close()
	})
	return b, host
}

func waitReady(t *testing.T, b *Bridge) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !b.Ready() {
		select {
		case <-deadline:
			t.Fatal("bridge never became ready")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestDisabledBridgeIsNoOp(t *testing.T) {
	b := New(Config{Enabled: false})
	if b.Enabled() {
		t.Fatal("disabled bridge reports enabled")
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start on disabled bridge: %v", err)
	}
	out := map[string]any{"temperature": 0.7}
	b.Trigger(context.Background(), HookChatParams, map[string]any{"sessionID": "s"}, &out)
	if out["temperature"] != 0.7 {
		t.Fatalf("disabled Trigger mutated output: %v", out)
	}
	b.Event("session.idle", map[string]any{})
	b.Stop(context.Background())
}

func TestNilBridgeIsNoOp(t *testing.T) {
	var b *Bridge
	if b.Enabled() || b.Ready() {
		t.Fatal("nil bridge should be disabled/not-ready")
	}
	out := map[string]any{"x": 1}
	b.Trigger(context.Background(), HookChatParams, nil, &out)
	if out["x"] != 1 {
		t.Fatal("nil Trigger mutated output")
	}
	b.Event("e", nil)
	b.Stop(context.Background())
}

func TestTriggerMutatesOutput(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.handle(func(method string, params json.RawMessage) (json.RawMessage, *rpcError, bool) {
		if method != "plugin.trigger:"+HookChatParams {
			return nil, nil, false
		}
		// Echo back a mutated output: bump temperature.
		var p struct {
			Output map[string]any `json:"output"`
		}
		_ = json.Unmarshal(params, &p)
		p.Output["temperature"] = 0.1
		raw, _ := json.Marshal(p.Output)
		return raw, nil, true
	})
	host.sendReady(t)
	waitReady(t, b)

	out := map[string]any{"temperature": 0.7, "topP": 0.9}
	b.Trigger(context.Background(), HookChatParams, map[string]any{"sessionID": "s"}, &out)
	if out["temperature"] != 0.1 {
		t.Fatalf("hook did not mutate temperature: %v", out)
	}
	if out["topP"] != 0.9 {
		t.Fatalf("hook dropped unmutated field: %v", out)
	}
}

func TestTriggerBeforeReadyIsNoOp(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.handle(func(string, json.RawMessage) (json.RawMessage, *rpcError, bool) {
		t.Error("host should not be called before ready")
		return nil, nil, false
	})
	out := map[string]any{"temperature": 0.7}
	b.Trigger(context.Background(), HookChatParams, nil, &out)
	if out["temperature"] != 0.7 {
		t.Fatal("trigger before ready mutated output")
	}
}

func TestTriggerTimeoutKeepsOutput(t *testing.T) {
	// Override the hook budget to a tiny value via a context the test controls:
	// here the host simply never answers, and we cap Trigger via the hook's own
	// timeout. Use a hook with a short budget (shell.env = 3s) but cancel faster
	// through ctx to keep the test quick.
	b, host := connectBridge(t, Config{})
	host.handle(func(string, json.RawMessage) (json.RawMessage, *rpcError, bool) {
		// Never respond (handled=false would respond with an error); block by
		// returning handled but with a long sleep is not possible here, so we
		// simulate no-response by spinning the response off — instead we drop it.
		select {} //nolint:staticcheck // intentionally blocks; goroutine leaks per test process only
	})
	host.sendReady(t)
	waitReady(t, b)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out := map[string]any{"env": map[string]any{}}
	start := time.Now()
	b.Trigger(ctx, HookShellEnv, nil, &out)
	if time.Since(start) > time.Second {
		t.Fatalf("Trigger did not honour ctx timeout: %s", time.Since(start))
	}
	if _, ok := out["env"]; !ok {
		t.Fatal("timed-out trigger dropped output")
	}
}

func TestTriggerErrorKeepsOutput(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.handle(func(string, json.RawMessage) (json.RawMessage, *rpcError, bool) {
		return nil, &rpcError{Code: -32000, Message: "plugin threw"}, true
	})
	host.sendReady(t)
	waitReady(t, b)

	out := map[string]any{"headers": map[string]any{"x": "1"}}
	b.Trigger(context.Background(), HookChatHeaders, nil, &out)
	if hm := out["headers"].(map[string]any); hm["x"] != "1" {
		t.Fatalf("errored trigger mutated output: %v", out)
	}
}

func TestEventNotify(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.sendReady(t)
	waitReady(t, b)

	b.Event("session.idle", map[string]any{"sessionID": "s1"})
	select {
	case n := <-host.notifs:
		if n.Method != "plugin.event" {
			t.Fatalf("unexpected notify method %q", n.Method)
		}
		var p map[string]any
		_ = json.Unmarshal(n.Params, &p)
		evt := p["event"].(map[string]any)
		if evt["type"] != "session.idle" {
			t.Fatalf("event type not forwarded: %v", evt)
		}
	case <-time.After(time.Second):
		t.Fatal("event notification not received")
	}
}

func TestPluginToolsRegistration(t *testing.T) {
	var got []ToolSpec
	done := make(chan struct{})
	b, host := connectBridge(t, Config{OnTools: func(ts []ToolSpec) { got = ts; close(done) }})
	host.sendReady(t)
	waitReady(t, b)

	host.sendNotify(t, "plugin.tools", map[string]any{
		"tools": []map[string]any{
			{"id": "my_tool", "description": "does things", "parameters": map[string]any{"type": "object"}},
		},
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OnTools never fired")
	}
	if len(got) != 1 || got[0].ID != "my_tool" {
		t.Fatalf("unexpected tools: %v", got)
	}
	if len(b.Tools()) != 1 {
		t.Fatalf("Tools() not populated: %v", b.Tools())
	}
}

func TestExecuteTool(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.handle(func(method string, _ json.RawMessage) (json.RawMessage, *rpcError, bool) {
		if method != "tool.execute" {
			return nil, nil, false
		}
		return json.RawMessage(`{"title":"ok","output":"42"}`), nil, true
	})
	host.sendReady(t)
	waitReady(t, b)

	res, err := b.ExecuteTool(context.Background(), "my_tool", json.RawMessage(`{"n":42}`), map[string]any{"sessionID": "s"})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.Title != "ok" || res.Output != "42" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCrashKeepsOutputAndClearsReady(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.sendReady(t)
	waitReady(t, b)
	// Wire watchConn manually since connectBridge skips Start.
	b.mu.RLock()
	c := b.conn
	b.mu.RUnlock()
	go b.watchConn(c)

	// Simulate a crash: close the host side.
	_ = host.rw.Close()

	deadline := time.After(2 * time.Second)
	for b.Ready() {
		select {
		case <-deadline:
			t.Fatal("ready not cleared after host crash")
		case <-time.After(time.Millisecond):
		}
	}
	out := map[string]any{"temperature": 0.7}
	b.Trigger(context.Background(), HookChatParams, nil, &out)
	if out["temperature"] != 0.7 {
		t.Fatal("trigger after crash mutated output")
	}
}

func TestLargePayloadRoundTrip(t *testing.T) {
	b, host := connectBridge(t, Config{})
	host.handle(func(_ string, params json.RawMessage) (json.RawMessage, *rpcError, bool) {
		var p struct {
			Output map[string]any `json:"output"`
		}
		_ = json.Unmarshal(params, &p)
		raw, _ := json.Marshal(p.Output)
		return raw, nil, true
	})
	host.sendReady(t)
	waitReady(t, b)

	msgs := make([]map[string]any, 200)
	for i := range msgs {
		msgs[i] = map[string]any{"role": "user", "text": "message body that is reasonably long to add bulk"}
	}
	out := map[string]any{"messages": msgs}
	b.Trigger(context.Background(), HookMessagesTransform, map[string]any{}, &out)
	if got := out["messages"].([]any); len(got) != 200 {
		t.Fatalf("large payload round-trip lost messages: %d", len(got))
	}
}
