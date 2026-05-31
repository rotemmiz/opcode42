package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clearEnv removes all websearch env so each test controls provider selection.
func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("PARALLEL_API_KEY", "")
	t.Setenv("OPENCODE_WEBSEARCH_PROVIDER", "")
}

func TestSearch_NotConfigured(t *testing.T) {
	clearEnv(t)
	_, err := New().Search(context.Background(), "anything")
	if err == nil || !strings.Contains(err.Error(), "no provider configured") {
		t.Fatalf("want not-configured error, got %v", err)
	}
}

func TestSearch_ExaDirectJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXA_API_KEY", "exa-key")

	var gotReq map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "exaApiKey=exa-key") {
			t.Errorf("exa key not in query: %q", r.URL.RawQuery)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		_, _ = w.Write([]byte(`{"result":{"content":[{"type":"text","text":"exa says hello"}]}}`))
	}))
	defer srv.Close()

	c := New()
	c.ExaBase = srv.URL + "/mcp"
	out, err := c.Search(context.Background(), "what is go")
	if err != nil {
		t.Fatal(err)
	}
	if out != "exa says hello" {
		t.Fatalf("out = %q", out)
	}
	// Verify the JSON-RPC tools/call shape + Exa tool name.
	params, _ := gotReq["params"].(map[string]any)
	if gotReq["method"] != "tools/call" || params["name"] != "web_search_exa" {
		t.Fatalf("request shape wrong: %v", gotReq)
	}
}

func TestSearch_SSEResponseAndParallel(t *testing.T) {
	clearEnv(t)
	t.Setenv("PARALLEL_API_KEY", "p-key")

	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		// SSE framing: the result arrives on a data: line.
		_, _ = w.Write([]byte("event: message\n" +
			`data: {"result":{"content":[{"type":"text","text":"parallel result"}]}}` + "\n\n"))
	}))
	defer srv.Close()

	c := New()
	c.ParallelURL = srv.URL + "/mcp"
	out, err := c.Search(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if out != "parallel result" {
		t.Fatalf("out = %q", out)
	}
	if auth != "Bearer p-key" {
		t.Fatalf("parallel auth header = %q", auth)
	}
}

func TestSearch_NoResults(t *testing.T) {
	clearEnv(t)
	t.Setenv("EXA_API_KEY", "k")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"content":[]}}`))
	}))
	defer srv.Close()
	c := New()
	c.ExaBase = srv.URL + "/mcp"
	out, err := c.Search(context.Background(), "q")
	if err != nil || !strings.Contains(out, "No search results") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestSearch_ProviderOverride(t *testing.T) {
	clearEnv(t)
	// Override forces parallel; with no PARALLEL_API_KEY it must error clearly.
	t.Setenv("OPENCODE_WEBSEARCH_PROVIDER", "parallel")
	if _, err := New().Search(context.Background(), "q"); err == nil ||
		!strings.Contains(err.Error(), "PARALLEL_API_KEY") {
		t.Fatalf("override to parallel without key should error, got %v", err)
	}
}
