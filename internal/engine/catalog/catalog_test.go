package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/engine/message"
)

func TestFixtureLookupAndModality(t *testing.T) {
	cat := Fixture()
	m, ok := cat.Lookup("openai", "gpt-4o")
	if !ok || m.Name != "GPT-4o" || !m.ToolCall {
		t.Fatalf("gpt-4o lookup failed: %+v ok=%v", m, ok)
	}
	if !m.HasModality("image") || !m.HasModality("text") {
		t.Fatalf("gpt-4o should accept text+image")
	}
	groq, ok := cat.Lookup("groq", "llama-3.3-70b-versatile")
	if !ok || groq.HasModality("image") {
		t.Fatalf("groq llama should be text-only: %+v", groq)
	}
	if _, ok := cat.Lookup("openai", "nope"); ok {
		t.Fatalf("unknown model should not resolve")
	}
}

func TestCostOf(t *testing.T) {
	cat := Fixture()
	var tokens message.TokenCounts
	tokens.Input = 1_000_000
	tokens.Output = 500_000
	tokens.Reasoning = 100_000 // billed at output rate
	tokens.Cache.Read = 200_000
	// 1.0*2.5 + 0.5*10 + 0.2*1.25 + 0.1*10 = 2.5 + 5 + 0.25 + 1.0 = 8.75
	got := cat.CostOf("openai", "gpt-4o", tokens)
	if want := 8.75; got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	// Unknown model / no pricing => 0.
	if c := cat.CostOf("openai", "nope", tokens); c != 0 {
		t.Fatalf("unknown model cost = %v, want 0", c)
	}
}

func TestLiveFetchWritesCacheAndOfflineFallback(t *testing.T) {
	body := fixtureJSON
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "models.json")
	live := &Live{URL: srv.URL, CachePath: cachePath, TTL: time.Hour, HTTPClient: srv.Client()}

	cat, err := live.Get(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, ok := cat.Lookup("openai", "gpt-4o"); !ok {
		t.Fatalf("fetched catalog missing gpt-4o")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	// Second Get within TTL is served from memory (no extra HTTP hit).
	if _, err := live.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 HTTP hit (memory cache), got %d", hits)
	}
}

func TestLiveOfflineFallbackToStaleDisk(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(cachePath, fixtureJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the disk cache stale so the live path must fetch.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(cachePath, old, old)

	// Point at a dead server so the fetch fails and we fall back to stale disk.
	live := &Live{URL: "http://127.0.0.1:0/never", CachePath: cachePath, TTL: time.Minute, HTTPClient: &http.Client{Timeout: time.Second}}
	cat, err := live.Get(context.Background())
	if err != nil {
		t.Fatalf("offline fallback should not error: %v", err)
	}
	if _, ok := cat.Lookup("groq", "llama-3.3-70b-versatile"); !ok {
		t.Fatalf("stale-disk fallback missing expected model")
	}
}

func TestStaticSource(t *testing.T) {
	s := NewStatic(Fixture())
	cat, err := s.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Lookup("openai", "gpt-4o"); !ok {
		t.Fatalf("static source missing gpt-4o")
	}
}
