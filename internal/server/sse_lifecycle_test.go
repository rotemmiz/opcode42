package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
)

// lifecycleServer builds a fully-wired httptest server (sessions + messages +
// instances) with the lifecycle bus resolver wired exactly as production does,
// so session.* and message.removed events fan out to /event subscribers.
func lifecycleServer(t *testing.T) (*httptest.Server, *message.Store) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("OPENCODE_AUTH_CONTENT", "")

	db, err := storage.Open(filepath.Join(t.TempDir(), "forge.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	g := bus.NewGlobal()
	instances := instance.NewManager(g)
	sessions := session.NewStore(db).WithBus(func(directory string) session.EventPublisher {
		return instances.BusFor(directory)
	})
	msgs := message.NewStore(db)
	h, err := New(Options{
		Version:   "test",
		Auth:      auth.Config{},
		Cwd:       t.TempDir(),
		Sessions:  sessions,
		Instances: instances,
		Messages:  msgs,
		Catalog:   catalog.Fixture(),
		Registry:  registry.New(tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{}),
		Todos:     tool.NewTodoStore(),
		Global:    g,
		Providers: func(context.Context, string, string) (llm.Provider, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, msgs
}

// eventCollector subscribes to an instance /event stream and accumulates the
// decoded {id,type,properties} events until Close.
type eventCollector struct {
	cancel context.CancelFunc
	events chan map[string]any
}

func subscribeEvents(t *testing.T, srv *httptest.Server, dir string) *eventCollector {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/event", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	req.Header.Set("x-opencode-directory", dir)
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose // closed in the reader goroutine's defer
	if err != nil {
		cancel()
		t.Fatalf("subscribe /event: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("/event status = %d", resp.StatusCode)
	}
	c := &eventCollector{cancel: cancel, events: make(chan map[string]any, 64)}
	ready := make(chan struct{})
	go func() {
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		first := true
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data:")
			if !ok {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &ev); err != nil {
				continue
			}
			if first {
				// server.connected lands before we send any CRUD; once we see it
				// the subscription is established and won't miss later events.
				first = false
				close(ready)
			}
			select {
			case c.events <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("never received server.connected")
	}
	t.Cleanup(c.cancel)
	return c
}

// waitFor returns the first collected event of the given type, or fails after a
// timeout. Earlier non-matching events are discarded.
func (c *eventCollector) waitFor(t *testing.T, typ string) map[string]any {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if ev["type"] == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q event", typ)
			return nil
		}
	}
}

// doReq performs an HTTP request, fully drains and closes the body, and returns
// the status code plus the body bytes.
func doReq(t *testing.T, srv *httptest.Server, method, path, dir string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("x-opencode-directory", dir)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, body
}

// assertSessionEvent verifies a session lifecycle event carries the opencode
// {sessionID, info:{id,...}} shape with info.id == sessionID.
func assertSessionEvent(t *testing.T, ev map[string]any, wantSessionID string) {
	t.Helper()
	props, ok := ev["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%v: properties not an object: %v", ev["type"], ev["properties"])
	}
	if props["sessionID"] != wantSessionID {
		t.Errorf("%v: properties.sessionID = %v, want %s", ev["type"], props["sessionID"], wantSessionID)
	}
	info, ok := props["info"].(map[string]any)
	if !ok {
		t.Fatalf("%v: properties.info not an object: %v", ev["type"], props["info"])
	}
	if info["id"] != wantSessionID {
		t.Errorf("%v: properties.info.id = %v, want %s", ev["type"], info["id"], wantSessionID)
	}
}

// TestSessionLifecycleSSE asserts the daemon publishes session.created (+ the
// backwards-compat session.updated) on create, and session.deleted on delete —
// matching opencode session.ts:557,562,611. Each event carries {sessionID,info}.
func TestSessionLifecycleSSE(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()
	coll := subscribeEvents(t, srv, dir)

	status, body := doReq(t, srv, http.MethodPost, "/session", dir)
	if status != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", status, body)
	}
	var created session.Info
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	createdEv := coll.waitFor(t, "session.created")
	assertSessionEvent(t, createdEv, created.ID)
	// The backwards-compat session.updated also fires on create.
	updatedEv := coll.waitFor(t, "session.updated")
	assertSessionEvent(t, updatedEv, created.ID)

	status, body = doReq(t, srv, http.MethodDelete, "/session/"+created.ID, dir)
	if status != http.StatusOK {
		t.Fatalf("delete status = %d; body=%s", status, body)
	}
	deletedEv := coll.waitFor(t, "session.deleted")
	assertSessionEvent(t, deletedEv, created.ID)
}

// TestForkLifecycleSSE asserts fork publishes session.created for the new
// (forked) session id (opencode publishes Event.Created from the shared create
// path; session.ts:557).
func TestForkLifecycleSSE(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()

	_, body := doReq(t, srv, http.MethodPost, "/session", dir)
	var parent session.Info
	if err := json.Unmarshal(body, &parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	coll := subscribeEvents(t, srv, dir)
	_, body = doReq(t, srv, http.MethodPost, "/session/"+parent.ID+"/fork", dir)
	var forked session.Info
	if err := json.Unmarshal(body, &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	ev := coll.waitFor(t, "session.created")
	assertSessionEvent(t, ev, forked.ID)
}

// TestMessageRemovedSSE asserts DELETE /session/:id/message/:messageID returns
// bare `true` and publishes message.removed{sessionID,messageID} (opencode
// session.ts:373-379,792-795).
func TestMessageRemovedSSE(t *testing.T) {
	srv, msgs := lifecycleServer(t)
	dir := t.TempDir()

	_, body := doReq(t, srv, http.MethodPost, "/session", dir)
	var sess session.Info
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}

	// Seed a user message so the delete has a real row to remove.
	msgID := "msg_test_0001"
	user := &message.UserMessage{ID: msgID, SessionID: sess.ID, Role: message.RoleUser}
	user.Time.Created = time.Now().UnixMilli()
	if err := msgs.PutMessage(context.Background(), message.Info{User: user}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	coll := subscribeEvents(t, srv, dir)
	status, rbody := doReq(t, srv, http.MethodDelete, "/session/"+sess.ID+"/message/"+msgID, dir)
	if status != http.StatusOK || strings.TrimSpace(string(rbody)) != "true" {
		t.Fatalf("delete message status=%d body=%q, want 200 true", status, rbody)
	}

	ev := coll.waitFor(t, "message.removed")
	props, ok := ev["properties"].(map[string]any)
	if !ok {
		t.Fatalf("message.removed properties not an object: %v", ev["properties"])
	}
	if props["sessionID"] != sess.ID {
		t.Errorf("message.removed sessionID = %v, want %s", props["sessionID"], sess.ID)
	}
	if props["messageID"] != msgID {
		t.Errorf("message.removed messageID = %v, want %s", props["messageID"], msgID)
	}

	// The row is gone after delete.
	if _, err := msgs.GetMessage(context.Background(), sess.ID, msgID); err == nil {
		t.Errorf("message still present after delete")
	}
}

// TestMessageRemovedMissingSession 404s before touching the bus, matching
// opencode's requireSession gate (session.ts:374).
func TestMessageRemovedMissingSession(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()
	status, _ := doReq(t, srv, http.MethodDelete, "/session/ses_missing/message/msg_x", dir)
	if status != http.StatusNotFound {
		t.Errorf("delete-missing-session status = %d, want 404", status)
	}
}
