package conformance

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/conformance/cassette"
)

// authoritativeEventCatalog reads opencode's frozen wire contract
// (conformance/openapi-reference.json — vendored from
// packages/sdk/openapi.json) and returns the full set of SSE event `type`
// strings from the Event discriminated union. This is the SINGLE source of
// truth for the SSE catalog (Ambiguity #2 resolved: opencode source is
// authoritative). The Event union is generated from opencode's Bus/sync event
// definitions; representative sources, cited file:line:
//
//	server.connected / global.disposed   server/event.ts:5-6
//	server.instance.disposed             bus/index.ts:17-22
//	message.updated / message.removed /
//	  message.part.updated / .part.removed message-v2.ts:517-553
//	message.part.delta                   message-v2.ts:535-545
//	session.created/updated/deleted      session.ts:339-352
//	session.status / session.idle        session/status.ts:34-48
//	session.compacted                    session/compaction.ts:26-33
//	session.diff / session.error         session/session.ts:353-366
//	permission.asked / .replied          permission/index.ts:69-79
//	question.asked/.replied/.rejected    question/index.ts:89-93
//	todo.updated                         session/todo.ts:20
//	mcp.tools.changed / .browser.*       mcp/index.ts:51-58
//	lsp.updated / lsp.client.diagnostics lsp/lsp.ts:20, lsp/client.ts:43
//	pty.created/updated/exited/deleted   pty/index.ts:95-98
//	file.edited / file.watcher.updated   file/index.ts:65, file/watcher.ts:25
//	command.executed                     command/index.ts:18
//	project.updated                      project/project.ts:60
//	vcs.branch.updated                   project/vcs.ts:242
//	workspace.* / worktree.*             control-plane/workspace.ts, worktree/index.ts
//	installation.updated/.update-available installation/index.ts:23-29
//	catalog.model.updated                core/catalog.ts:30
//	session.next.* (experimental)        core/session-event.ts:40-354
func authoritativeEventCatalog(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("openapi-reference.json")
	if err != nil {
		t.Fatalf("read openapi-reference.json: %v", err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				AnyOf []struct {
					Ref string `json:"$ref"`
				} `json:"anyOf"`
				Properties struct {
					Type struct {
						Const string   `json:"const"`
						Enum  []string `json:"enum"`
					} `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("decode openapi-reference.json: %v", err)
	}
	ev, ok := spec.Components.Schemas["Event"]
	if !ok || len(ev.AnyOf) == 0 {
		t.Fatal("openapi-reference.json has no Event union schema")
	}
	out := map[string]bool{}
	for _, m := range ev.AnyOf {
		name := m.Ref[strings.LastIndex(m.Ref, "/")+1:]
		member := spec.Components.Schemas[name]
		typ := member.Properties.Type.Const
		if typ == "" && len(member.Properties.Type.Enum) > 0 {
			typ = member.Properties.Type.Enum[0]
		}
		if typ != "" {
			out[typ] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("extracted zero event types from Event union")
	}
	return out
}

// forgeEmittedEventCatalog is the set of SSE event types Forge's daemon emits.
// Keep this in lockstep with the actual emitters — grep:
//
//	rg 'NewEvent\("' internal/ cmd/
//	bus.EventConnected/Heartbeat/InstanceDisposed (internal/bus/bus.go:18-26)
//
// The test below asserts every entry is a real opencode event type, so an
// invented/misspelled type fails CI.
var forgeEmittedEventCatalog = map[string]bool{
	// transport (internal/bus/bus.go, internal/server SSE handler)
	"server.connected":         true,
	"server.heartbeat":         true,
	"server.instance.disposed": true,
	// engine message/part lifecycle (engine.go, processor/emit.go, loop.go)
	"message.updated":      true,
	"message.part.updated": true,
	"message.part.delta":   true,
	// message deletion (server/prompt_handlers.go deleteMessageHandler;
	// message store DeleteMessage) — opencode session.ts:792, message-v2.ts:524
	"message.removed": true,
	// session lifecycle CRUD (internal/session.Store publishes via the instance
	// bus): created+updated on create/fork, updated on title generation, deleted
	// on delete — opencode session.ts:339-352,557,562,611
	"session.created": true,
	"session.updated": true,
	"session.deleted": true,
	// run-lock status (engine.go emitStatus, M11)
	"session.status":    true,
	"session.idle":      true,
	"session.compacted": true,
	// permission / question round-trips (engine/permission, engine/question)
	"permission.asked":   true,
	"permission.replied": true,
	"question.asked":     true,
	"question.replied":   true,
	"question.rejected":  true,
	// MCP / LSP (internal/mcp, internal/lsp)
	"mcp.tools.changed": true,
	"lsp.updated":       true,
}

// TestForgeEmittedEventsAreAuthoritative gates every SSE event type Forge emits
// against opencode's authoritative catalog (Ambiguity #2: opencode source is
// the truth). A Forge event type absent from opencode's Event union is a wire
// divergence and fails here. This is the catalog half of the Phase-B SSE
// conformance gate (plan 02 §M11); the live dual-run gates ordering/shape.
func TestForgeEmittedEventsAreAuthoritative(t *testing.T) {
	catalog := authoritativeEventCatalog(t)
	// server.heartbeat is a transport-only keepalive injected by the SSE handler
	// (opencode event.ts:32; Forge's SSE handler) — it is published by NEITHER
	// daemon's Bus and so is intentionally absent from the openapi Event union.
	// Both daemons emit it identically, so it is not a divergence.
	transportInjected := map[string]bool{"server.heartbeat": true}
	var unknown []string
	for typ := range forgeEmittedEventCatalog {
		if transportInjected[typ] {
			continue
		}
		if !catalog[typ] {
			unknown = append(unknown, typ)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Errorf("Forge emits event type(s) not in opencode's authoritative catalog: %v", unknown)
	}
}

// TestLiveEventCatalogIsAuthoritative gates the live dual-run's compared event
// set: every type it diffs must be a real opencode event type AND one Forge
// actually emits. This keeps the cross-daemon ordering diff honest — it can only
// assert lifecycle events both daemons genuinely share.
func TestLiveEventCatalogIsAuthoritative(t *testing.T) {
	catalog := authoritativeEventCatalog(t)
	for typ := range liveEventCatalog {
		if !catalog[typ] {
			t.Errorf("liveEventCatalog type %q is not in opencode's authoritative catalog", typ)
		}
		if !forgeEmittedEventCatalog[typ] {
			t.Errorf("liveEventCatalog type %q is gated but Forge does not emit it", typ)
		}
	}
}

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
