package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func promptTestServer(t *testing.T, mock *enginetest.MockProvider) (http.Handler, string, string) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	sessions := session.NewStore(db)
	sess, err := sessions.Create(context.Background(), dir)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	reg := registry.New(tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{})
	h, err := New(Options{
		Version:   "test",
		Auth:      auth.Config{},
		Cwd:       dir,
		Sessions:  sessions,
		Instances: instance.NewManager(bus.NewGlobal()),
		Messages:  message.NewStore(db),
		Catalog:   catalog.Fixture(),
		Registry:  reg,
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return h, sess.ID, dir
}

func TestPromptEndpoint_TextOnly(t *testing.T) {
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().Text("t1", "Hello").
			StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events(),
	)
	h, sessionID, dir := promptTestServer(t, mock)

	body := `{"model":{"providerID":"openai","modelID":"gpt-4o"},"parts":[{"type":"text","text":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/session/"+sessionID+"/message?directory="+dir, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Info  map[string]any   `json:"info"`
		Parts []map[string]any `json:"parts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	if out.Info["role"] != "assistant" || out.Info["finish"] != "stop" {
		t.Fatalf("assistant info wrong: %+v", out.Info)
	}
	var text string
	for _, p := range out.Parts {
		if p["type"] == "text" {
			text += p["text"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("assistant text = %q, want Hello", text)
	}
}

func TestMessagesEndpoint_ListsConversation(t *testing.T) {
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().Text("t1", "yo").StepFinish("stop", llm.TokenUsage{}).Finish().Events(),
	)
	h, sessionID, dir := promptTestServer(t, mock)

	post := httptest.NewRequest(http.MethodPost, "/session/"+sessionID+"/message?directory="+dir,
		strings.NewReader(`{"model":{"providerID":"openai","modelID":"gpt-4o"},"parts":[{"type":"text","text":"hi"}]}`))
	h.ServeHTTP(httptest.NewRecorder(), post)

	get := httptest.NewRequest(http.MethodGet, "/session/"+sessionID+"/message?directory="+dir, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	var msgs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages (user+assistant), got %d", len(msgs))
	}
}

func TestPromptEndpoint_RejectsMissingModel(t *testing.T) {
	mock := enginetest.NewMockProvider()
	h, sessionID, dir := promptTestServer(t, mock)
	req := httptest.NewRequest(http.MethodPost, "/session/"+sessionID+"/message?directory="+dir,
		strings.NewReader(`{"parts":[{"type":"text","text":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing model should 400, got %d", rec.Code)
	}
}

func TestPromptEndpoint_UnknownSession404(t *testing.T) {
	mock := enginetest.NewMockProvider()
	h, _, dir := promptTestServer(t, mock)
	req := httptest.NewRequest(http.MethodPost, "/session/ses_nope/message?directory="+dir,
		strings.NewReader(`{"model":{"providerID":"openai","modelID":"gpt-4o"},"parts":[{"type":"text","text":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session should 404, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestPromptAsync_Returns204(t *testing.T) {
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().Text("t1", "ok").StepFinish("stop", llm.TokenUsage{}).Finish().Events(),
	)
	h, sessionID, dir := promptTestServer(t, mock)
	req := httptest.NewRequest(http.MethodPost, "/session/"+sessionID+"/prompt_async?directory="+dir,
		strings.NewReader(`{"model":{"providerID":"openai","modelID":"gpt-4o"},"parts":[{"type":"text","text":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("prompt_async should be 204 empty, got %d body=%q", rec.Code, rec.Body.String())
	}
}
