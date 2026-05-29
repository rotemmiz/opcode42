package conformance

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/conformance/cassette"
)

// firstSSEEvent extracts and parses the first "data:" event from an SSE body.
func firstSSEEvent(t *testing.T, body string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "data:")
		if !ok {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &ev); err != nil {
			t.Fatalf("parse SSE event: %v", err)
		}
		return ev
	}
	t.Fatal("no data event in SSE body")
	return nil
}

func interactionForPath(c *cassette.Cassette, suffix string) *cassette.Interaction {
	for i := range c.Interactions {
		it := &c.Interactions[i]
		if it.Request != nil && strings.HasSuffix(it.Request.URL, suffix) {
			return it
		}
	}
	return nil
}

// TestSSECatalogShapesFromRecordedTruth locks Finding #2 against the committed
// real-opencode cassette: the instance /event stream sends a BARE
// {id,type,properties}, while the global /global/event stream WRAPS it as
// {payload:{...}}. Forge must reproduce both shapes distinctly.
func TestSSECatalogShapesFromRecordedTruth(t *testing.T) {
	raw, err := os.ReadFile("cassettes/sse-catalog.json")
	if err != nil {
		t.Fatalf("read catalog cassette: %v", err)
	}
	c, err := cassette.Decode(raw)
	if err != nil {
		t.Fatalf("decode cassette: %v", err)
	}

	inst := interactionForPath(c, "/event")
	if inst == nil {
		t.Fatal("no /event interaction recorded")
	}
	ev := firstSSEEvent(t, inst.Response.Body)
	if ev["type"] != "server.connected" {
		t.Errorf("instance event: want top-level type=server.connected, got %v", ev["type"])
	}
	if _, hasPayload := ev["payload"]; hasPayload {
		t.Errorf("instance /event must be BARE (no payload wrapper): %v", ev)
	}
	if _, ok := ev["properties"]; !ok {
		t.Errorf("instance event missing top-level properties: %v", ev)
	}

	global := interactionForPath(c, "/global/event")
	if global == nil {
		t.Fatal("no /global/event interaction recorded")
	}
	gev := firstSSEEvent(t, global.Response.Body)
	payload, ok := gev["payload"].(map[string]any)
	if !ok {
		t.Fatalf("global /global/event must be WRAPPED in payload: %v", gev)
	}
	if _, hasType := gev["type"]; hasType {
		t.Errorf("global event must NOT have top-level type (it's under payload): %v", gev)
	}
	if payload["type"] != "server.connected" {
		t.Errorf("global event payload.type: want server.connected, got %v", payload["type"])
	}
}
