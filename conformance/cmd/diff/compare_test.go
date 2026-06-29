package main

import (
	"bytes"
	"testing"

	"github.com/rotemmiz/opcode42/conformance/result"
)

func file(scenarios ...result.Scenario) *result.File {
	return &result.File{Target: "t", Scenarios: scenarios}
}

func scn(name string, steps ...result.Step) result.Scenario {
	return result.Scenario{Name: name, Steps: steps}
}

func TestIdenticalRunsHaveNoFindings(t *testing.T) {
	f := file(scn("session-create-list",
		result.Step{Name: "create", Method: "POST", Path: "/session", Status: 200,
			Body: `{"id":"<id>","title":""}`},
	))
	r := Compare(f, f)
	if len(r.Findings) != 0 {
		t.Fatalf("identical runs should have no findings, got %d: %+v", len(r.Findings), r.Findings)
	}
	if code := r.Print(&bytes.Buffer{}, nil); code != 0 {
		t.Errorf("exit code: want 0, got %d", code)
	}
}

func TestStatusMismatchIsBlocking(t *testing.T) {
	exp := file(scn("session-get-delete",
		result.Step{Name: "get", Method: "GET", Path: "/session/x", Status: 200},
	))
	act := file(scn("session-get-delete",
		result.Step{Name: "get", Method: "GET", Path: "/session/x", Status: 501},
	))
	r := Compare(exp, act)
	if len(r.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(r.Findings))
	}
	var buf bytes.Buffer
	if code := r.Print(&buf, nil); code != 1 {
		t.Errorf("exit code: want 1 (blocking), got 1=%d", code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("HTTP status: 200 != 501")) {
		t.Errorf("missing status detail in output:\n%s", buf.String())
	}
}

func TestNestedBodyFieldDiffHasPath(t *testing.T) {
	exp := file(scn("prompt-tool-call",
		result.Step{Name: "final", Body: `{"properties":{"state":{"status":"completed"}}}`},
	))
	act := file(scn("prompt-tool-call",
		result.Step{Name: "final", Body: `{"properties":{"state":{"status":"running"}}}`},
	))
	r := Compare(exp, act)
	if len(r.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(r.Findings), r.Findings)
	}
	want := `body.properties.state.status: "completed" != "running"`
	if r.Findings[0].Detail != want {
		t.Errorf("detail:\n want %s\n got  %s", want, r.Findings[0].Detail)
	}
}

func TestKnownDivergenceIsWarningNotBlocking(t *testing.T) {
	exp := file(scn("sync-replay", result.Step{Name: "replay", Status: 200}))
	act := file(scn("sync-replay", result.Step{Name: "replay", Status: 501}))
	r := Compare(exp, act)
	divs := []Divergence{{Scenario: "sync-replay", Reason: "not in Phase A"}}
	var buf bytes.Buffer
	if code := r.Print(&buf, divs); code != 0 {
		t.Errorf("known divergence must not block; exit code want 0, got %d", code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("KNOWN-DIVERGENCE")) {
		t.Errorf("expected KNOWN-DIVERGENCE tag:\n%s", buf.String())
	}
}

func TestGlobDivergenceMatch(t *testing.T) {
	divs := []Divergence{{Scenario: "experimental-*", Reason: "best-effort"}}
	if matchDivergence(divs, "experimental-foo", "anything") == "" {
		t.Error("experimental-* should match experimental-foo")
	}
	if matchDivergence(divs, "session-create", "anything") != "" {
		t.Error("experimental-* should not match session-create")
	}
}

func TestDetailScopedDivergence(t *testing.T) {
	// A detail-scoped entry suppresses only matching findings, not the whole scenario.
	divs := []Divergence{{Scenario: "live-*", Detail: "body.info.mode", Reason: "engine gap"}}
	if got := matchDivergence(divs, "live-prompt-text", `body.info.mode: "build" != ""`); got == "" {
		t.Error("detail-scoped entry should match the info.mode finding")
	}
	if got := matchDivergence(divs, "live-prompt-text", `body.info.cost: 1 != 2`); got != "" {
		t.Errorf("detail-scoped entry must not suppress unrelated findings, got %q", got)
	}
}

func TestSSEEventCountMismatch(t *testing.T) {
	exp := file(scn("prompt-text-only",
		result.Step{Name: "stream", SSE: []string{`{"type":"server.connected"}`, `{"type":"message.updated"}`}},
	))
	act := file(scn("prompt-text-only",
		result.Step{Name: "stream", SSE: []string{`{"type":"server.connected"}`}},
	))
	r := Compare(exp, act)
	if len(r.Findings) != 1 || !bytes.Contains([]byte(r.Findings[0].Detail), []byte("SSE event count: 2 != 1")) {
		t.Fatalf("expected SSE count finding, got %+v", r.Findings)
	}
}

func TestMissingScenarioReported(t *testing.T) {
	exp := file(scn("a", result.Step{Status: 200}), scn("b", result.Step{Status: 200}))
	act := file(scn("a", result.Step{Status: 200}))
	r := Compare(exp, act)
	if len(r.Findings) != 1 || r.Findings[0].Scenario != "b" {
		t.Fatalf("expected missing-scenario finding for b, got %+v", r.Findings)
	}
}
