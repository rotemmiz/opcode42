package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
)

// TestPrompt_InjectsProjectInstructions proves an AGENTS.md in the project dir
// reaches the model's system prompt through the engine.
func TestPrompt_InjectsProjectInstructions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	writeMD(t, dir, "AGENTS.md", "ALWAYS write Go, never Python.")
	sessions := session.NewStore(db)
	sess, err := sessions.Create(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Give the session a non-default title so the forked title-generation stream
	// (step-0) does not race into the asserted provider call/request order.
	if err := sessions.SetTitle(context.Background(), sess.ID, "named"); err != nil {
		t.Fatal(err)
	}
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().Text("t", "ok").
			StepFinish("stop", llm.TokenUsage{Input: 1, Output: 1}).Finish().Events(),
	)
	h, err := New(Options{
		Version: "test", Auth: auth.Config{}, Cwd: dir,
		Sessions: sessions, Instances: instance.NewManager(bus.NewGlobal()),
		Messages: message.NewStore(db), Catalog: catalog.Fixture(),
		Registry:  registry.New(tool.Read{}),
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"model": map[string]string{"providerID": "openai", "modelID": "gpt-4o"},
		"parts": []map[string]any{{"type": "text", "text": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/session/"+sess.ID+"/message", bytes.NewReader(body))
	req.Header.Set("x-opencode-directory", dir)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, rr.Body.String())
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no provider request captured")
	}
	joined := strings.Join(reqs[0].SystemPrompts, "\n")
	if !strings.Contains(joined, "ALWAYS write Go, never Python.") {
		t.Fatalf("AGENTS.md not in system prompt; got: %q", joined)
	}
	if !strings.Contains(joined, "Instructions from: ") {
		t.Fatalf("instruction header missing; got: %q", joined)
	}
}
