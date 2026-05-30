package forgeclient

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
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
