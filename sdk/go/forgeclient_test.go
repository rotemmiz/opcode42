package forgeclient

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew_InjectsAuthAndDirectory(t *testing.T) {
	var gotAuth, gotDir string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotDir = r.Header.Get("X-Opencode-Directory")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(srv.URL, Options{Username: "user", Password: "pass", Directory: "/proj dir", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if gotAuth != wantAuth {
		t.Fatalf("auth header = %q, want %q", gotAuth, wantAuth)
	}
	if gotDir != "%2Fproj%20dir" { // url.PathEscape("/proj dir") — space as %20 so it round-trips
		t.Fatalf("directory header = %q", gotDir)
	}
}

func TestHealth_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected unauthorized error")
	}
}

func sseHandler(t *testing.T, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write sse: %v", err)
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func readEvents(t *testing.T, s *EventStream, n int) []SSEEvent {
	t.Helper()
	var out []SSEEvent
	timeout := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case ev := <-s.Events():
			out = append(out, ev)
		case <-timeout:
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}

func TestEvents_ParsesInstanceStream(t *testing.T) {
	const body = `data: {"id":"evt_1","type":"message.part.delta","properties":{"delta":"hi"}}

data: {"id":"evt_2","type":"message.updated","properties":{}}

`
	srv := httptest.NewServer(sseHandler(t, body))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})

	s, err := c.Events(context.Background())
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer s.Close()

	evs := readEvents(t, s, 2)
	if evs[0].ID != "evt_1" || evs[0].Type != "message.part.delta" {
		t.Fatalf("event 0 wrong: %+v", evs[0])
	}
	if string(evs[0].Properties) != `{"delta":"hi"}` {
		t.Fatalf("properties = %s", evs[0].Properties)
	}
	if evs[1].Type != "message.updated" {
		t.Fatalf("event 1 wrong: %+v", evs[1])
	}
}

func TestGlobalEvents_UnwrapsEnvelope(t *testing.T) {
	const body = `data: {"payload":{"id":"evt_9","type":"session.updated","properties":{"x":1}},"directory":"/repo"}

`
	srv := httptest.NewServer(sseHandler(t, body))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})

	s, err := c.GlobalEvents(context.Background())
	if err != nil {
		t.Fatalf("global events: %v", err)
	}
	defer s.Close()

	ev := readEvents(t, s, 1)[0]
	if ev.ID != "evt_9" || ev.Type != "session.updated" || ev.Directory != "/repo" {
		t.Fatalf("unwrapped event wrong: %+v", ev)
	}
}

func TestEventStream_CloseStops(t *testing.T) {
	srv := httptest.NewServer(sseHandler(t, "data: {\"id\":\"e\",\"type\":\"t\",\"properties\":{}}\n\n"))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})
	s, err := c.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = readEvents(t, s, 1)
	s.Close() // must not panic / deadlock
}

func TestGetJSON_DecodesAndSendsHeaders(t *testing.T) {
	var gotAuth, gotDir, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotDir, gotAccept = r.Header.Get("Authorization"), r.Header.Get("X-Opencode-Directory"), r.Header.Get("Accept")
		_, _ = io.WriteString(w, `[{"id":"ses_1","title":"T"}]`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, Options{Username: "u", Password: "p", Directory: "/d", HTTPClient: srv.Client()})

	var out []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := c.GetJSON(context.Background(), "/session", &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "ses_1" || out[0].Title != "T" {
		t.Fatalf("decode wrong: %+v", out)
	}
	if gotAuth == "" || gotDir != "%2Fd" || gotAccept != "application/json" {
		t.Fatalf("headers wrong: auth=%q dir=%q accept=%q", gotAuth, gotDir, gotAccept)
	}
}

func TestPostJSON_SendsBodyDecodesResponse(t *testing.T) {
	var gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotCT = string(b), r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"id":"ses_new"}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})
	var out struct {
		ID string `json:"id"`
	}
	if err := c.PostJSON(context.Background(), "/session", map[string]any{"x": 1}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "ses_new" || gotCT != "application/json" || !strings.Contains(gotBody, `"x":1`) {
		t.Fatalf("post wrong: out=%+v ct=%q body=%q", out, gotCT, gotBody)
	}
}

func TestPostJSON_Tolerates204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, Options{HTTPClient: srv.Client()})
	if err := c.PostJSON(context.Background(), "/x", map[string]any{}, nil); err != nil {
		t.Fatalf("204 should be tolerated: %v", err)
	}
}
