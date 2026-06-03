package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/enginetest"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/worktree"
)

// TestPrompt_TaskToolSpawnsSubagent drives a parent prompt whose model emits a
// `task` tool call; the subagent runner must create a child session, run a
// nested loop, and return the subagent's text back into the parent turn.
func TestPrompt_TaskToolSpawnsSubagent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	resolved := worktree.Resolve(dir)
	sessions := session.NewStore(db)
	parent, err := sessions.Create(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	// Non-default title so the forked step-0 title stream does not add a fourth
	// provider call to the asserted parent+child+parent count.
	if err := sessions.SetTitle(context.Background(), parent.ID, "named"); err != nil {
		t.Fatal(err)
	}

	usage := llm.TokenUsage{Input: 1, Output: 1}
	// Call order across the shared mock: parent step-1 (task call), child step-1
	// (text answer), parent step-2 (final text after the tool result).
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().
			ToolCall("c1", "task", map[string]any{
				"description": "research", "prompt": "find the answer", "agent": "general",
			}).StepFinish("tool-calls", usage).Finish().Events(),
		enginetest.NewScript().StepStart().Text("ct", "subagent answer").
			StepFinish("stop", usage).Finish().Events(),
		enginetest.NewScript().StepStart().Text("pt", "parent final").
			StepFinish("stop", usage).Finish().Events(),
	)
	h, err := New(Options{
		Version: "test", Auth: auth.Config{}, Cwd: dir,
		Sessions: sessions, Instances: instance.NewManager(bus.NewGlobal()),
		Messages: message.NewStore(db), Catalog: catalog.Fixture(),
		Registry:  registry.New(tool.Task{}),
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"model": map[string]string{"providerID": "openai", "modelID": "gpt-4o"},
		"parts": []map[string]any{{"type": "text", "text": "delegate this"}},
	}
	buf, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/session/"+parent.ID+"/message", bytes.NewReader(buf))
	r.Header.Set("x-opencode-directory", dir)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}

	// The mock was called 3 times (parent, child, parent) — proving the nested
	// subagent loop actually ran.
	if mock.Calls() != 3 {
		t.Fatalf("expected 3 provider calls (parent+child+parent), got %d", mock.Calls())
	}

	// A child session linked to the parent must now exist.
	children, err := sessions.Children(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].ParentID != parent.ID {
		t.Fatalf("expected one child session linked to parent, got %+v", children)
	}
}

func TestLastTextAndWrap(t *testing.T) {
	base := func(id string) message.PartBase { return message.PartBase{ID: id} }
	w := message.WithParts{Parts: []message.Part{
		&message.TextPart{PartBase: base("p1"), Type: "text", Text: "thinking out loud"},
		&message.ToolPart{PartBase: base("p2")},
		&message.TextPart{PartBase: base("p3"), Type: "text", Text: "the answer"},
	}}
	if got := lastText(w); got != "the answer" {
		t.Fatalf("lastText = %q; want last text part", got)
	}
	out := wrapTaskResult("ses_child", "the answer")
	want := "<task id=\"ses_child\" state=\"completed\">\n<task_result>\nthe answer\n</task_result>\n</task>"
	if out != want {
		t.Fatalf("wrapTaskResult =\n%q\nwant\n%q", out, want)
	}
}

// TestTaskUnavailableWithoutRunner confirms the task tool errors clearly when no
// subagent runner is injected (e.g. inside a subagent — recursion bound).
func TestTaskUnavailableWithoutRunner(t *testing.T) {
	_, err := (tool.Task{}).Run(context.Background(),
		map[string]any{"description": "d", "prompt": "p"}, tool.Context{SessionID: "s"})
	if err == nil {
		t.Fatal("task should be unavailable without a runner")
	}
}
