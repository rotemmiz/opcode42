package opcode42client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A mock daemon that exercises the full PTY flow: create → connect-token → WS
// connect → data frame + 0x00 cursor control frame → echo one input.
func ptyMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/pty", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(PTYInfo{ID: "pty_1", Status: "running", Command: "sh"})
	})
	mux.HandleFunc("/pty/pty_1/connect-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-opencode-ticket") != "1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ticket": "tkt", "expires_in": 60})
	})
	mux.HandleFunc("/pty/pty_1/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ticket") != "tkt" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte("hello")) // opencode sends data as TEXT
		meta, _ := json.Marshal(map[string]int{"cursor": 5})
		_ = c.Write(ctx, websocket.MessageBinary, append([]byte{0x00}, meta...)) // control frame
		if _, in, err := c.Read(ctx); err == nil {
			_ = c.Write(ctx, websocket.MessageText, append([]byte("echo:"), in...))
		}
		time.Sleep(50 * time.Millisecond)
	})
	return mux
}

func recvBytes(t *testing.T, ch <-chan []byte) string {
	t.Helper()
	select {
	case b := <-ch:
		return string(b)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output")
		return ""
	}
}

func TestPTY_CreateConnectFramingEcho(t *testing.T) {
	srv := httptest.NewServer(ptyMux(t))
	defer srv.Close()
	c, err := New(srv.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	info, err := c.CreatePTY(ctx, PTYCreate{Command: "sh"})
	if err != nil || info.ID != "pty_1" {
		t.Fatalf("CreatePTY: id=%q err=%v", info.ID, err)
	}

	conn, err := c.ConnectPTY(ctx, "pty_1", 0)
	if err != nil {
		t.Fatalf("ConnectPTY: %v", err)
	}
	defer conn.Close()

	// data frame
	if got := recvBytes(t, conn.Output()); got != "hello" {
		t.Fatalf("first output should be the data frame %q, got %q", "hello", got)
	}
	// control frame → cursor
	select {
	case cur := <-conn.Cursor():
		if cur != 5 {
			t.Fatalf("cursor should be 5, got %d", cur)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no cursor control frame")
	}
	// input → echo (proves the write path + that 0x00 control frames aren't mistaken for data)
	if err := conn.Write(ctx, []byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := recvBytes(t, conn.Output()); got != "echo:ping" {
		t.Fatalf("echo should be %q, got %q", "echo:ping", got)
	}
}

func TestPTY_ConnectTokenForbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pty/pty_x/connect-token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, _ := New(srv.URL, Options{})
	if _, err := c.ConnectPTY(context.Background(), "pty_x", 0); err == nil {
		t.Fatal("ConnectPTY should fail when the connect-token is refused")
	}
}
