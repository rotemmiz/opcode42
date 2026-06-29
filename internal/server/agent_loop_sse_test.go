package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/enginetest"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
)

// sseEvent is a decoded {id,type,properties} SSE frame.
type sseEvent struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

// streamEvents opens an SSE endpoint and pushes decoded frames onto a channel
// until ctx is cancelled. It's used to observe the agent loop end-to-end.
func streamEvents(ctx context.Context, t *testing.T, url, dir string) <-chan sseEvent {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("x-opencode-directory", dir)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed in the streaming goroutine below
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", resp.StatusCode)
	}
	out := make(chan sseEvent, 256)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var ev sseEvent
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) == nil {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// TestAgentLoopSSESequence is the M11 agent-loop SSE baseline: a prompt drives
// the documented event sequence over the /event stream — server.connected, the
// user message, streamed text deltas, and the completed assistant message — all
// as bare {id,type,properties} frames. Uses the mock provider (no real LLM).
func TestAgentLoopSSESequence(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	sessions := session.NewStore(db)
	sess, err := sessions.Create(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().Text("t1", "Hello, ", "world").
			StepFinish("stop", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events(),
	)
	handler, err := New(Options{
		Version: "test", Auth: auth.Config{}, Cwd: dir,
		Sessions: sessions, Instances: instance.NewManager(bus.NewGlobal()),
		Messages: message.NewStore(db), Catalog: catalog.Fixture(),
		Registry:  registry.New(tool.Read{}),
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := streamEvents(ctx, t, srv.URL+"/event", dir)

	// server.connected must be the first frame.
	first := <-events
	if first.Type != "server.connected" {
		t.Fatalf("first event = %q, want server.connected", first.Type)
	}

	// Run the prompt synchronously in a goroutine so the run is fully quiesced
	// (cleanup done, all events emitted) before we assert and tear down; we read
	// the stream concurrently meanwhile.
	body, _ := json.Marshal(map[string]any{
		"model": map[string]string{"providerID": "openai", "modelID": "gpt-4o"},
		"parts": []map[string]any{{"type": "text", "text": "hi"}},
	})
	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/session/"+sess.ID+"/message", bytes.NewReader(body))
		req.Header.Set("x-opencode-directory", dir)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- 0
			return
		}
		_ = resp.Body.Close()
		done <- resp.StatusCode
	}()

	var sawUser, sawDelta, sawAssistantDone bool
	var deltaText strings.Builder
	inspect := func(ev sseEvent) {
		switch ev.Type {
		case "message.updated":
			// info is the flattened message (role discriminator; no envelope).
			if info, ok := ev.Properties["info"].(map[string]any); ok {
				switch info["role"] {
				case "user":
					sawUser = true
				case "assistant":
					if fin, _ := info["finish"].(string); fin == "stop" {
						sawAssistantDone = true
					}
				}
			}
		case "message.part.delta":
			sawDelta = true
			if d, ok := ev.Properties["delta"].(string); ok {
				deltaText.WriteString(d)
			}
		}
	}

	// Collect events until the COMPLETION event arrives — SSE delivery lags the
	// synchronous prompt's return, so waiting on `done` alone would race and miss
	// events. Once the assistant-complete frame is seen, confirm the prompt
	// returned 200 (run quiesced) before asserting/teardown.
	for !sawAssistantDone {
		select {
		case ev := <-events:
			inspect(ev)
		case <-ctx.Done():
			t.Fatalf("timeout; sawUser=%v sawDelta=%v done=%v", sawUser, sawDelta, sawAssistantDone)
		}
	}
	if status := <-done; status != http.StatusOK {
		t.Fatalf("prompt status = %d", status)
	}
	if !sawUser {
		t.Error("never saw the user message.updated event")
	}
	if !sawAssistantDone {
		t.Error("never saw the completed assistant message.updated event")
	}
	if !sawDelta || !strings.Contains(deltaText.String(), "Hello") {
		t.Errorf("text deltas missing/incomplete: %q", deltaText.String())
	}
}
