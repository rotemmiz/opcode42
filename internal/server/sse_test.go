package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/auth"
)

// firstSSEEvent connects to an SSE endpoint and returns the first "data:" line
// (decoded JSON) plus the response, so headers can be asserted.
func firstSSEEvent(t *testing.T, srv *httptest.Server, path, dir string) (map[string]any, http.Header) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dir != "" {
		req.Header.Set("x-opencode-directory", dir)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &ev); err != nil {
			t.Fatalf("decode SSE data: %v", err)
		}
		return ev, resp.Header
	}
	t.Fatalf("%s: no SSE data event before timeout", path)
	return nil, nil
}

func TestInstanceEventConnectedIsBare(t *testing.T) {
	srv := httptest.NewServer(newBackedServer(t, auth.Config{}))
	defer srv.Close()

	ev, hdr := firstSSEEvent(t, srv, "/event", t.TempDir())

	if ev["type"] != "server.connected" {
		t.Errorf("type = %v, want server.connected", ev["type"])
	}
	if _, ok := ev["properties"]; !ok {
		t.Errorf("missing properties: %v", ev)
	}
	if _, ok := ev["id"]; !ok {
		t.Errorf("missing id: %v", ev)
	}
	// Instance stream is BARE — it must NOT be wrapped in a payload envelope.
	if _, ok := ev["payload"]; ok {
		t.Errorf("instance event must be bare, got wrapped: %v", ev)
	}
	if got := hdr.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := hdr.Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := hdr.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q", got)
	}
}

func TestGlobalEventConnectedIsWrapped(t *testing.T) {
	srv := httptest.NewServer(newBackedServer(t, auth.Config{}))
	defer srv.Close()

	ev, _ := firstSSEEvent(t, srv, "/global/event", "")

	// Global stream WRAPS the event in a payload envelope (Finding #2).
	payload, ok := ev["payload"].(map[string]any)
	if !ok {
		t.Fatalf("global event missing payload envelope: %v", ev)
	}
	if payload["type"] != "server.connected" {
		t.Errorf("payload.type = %v, want server.connected", payload["type"])
	}
	if _, ok := payload["properties"]; !ok {
		t.Errorf("payload missing properties: %v", payload)
	}
}
