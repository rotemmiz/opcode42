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

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
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

	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
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

// doReqBody is doReq with a request body (and Content-Type: application/json),
// used to exercise PATCH/POST endpoints.
func doReqBody(t *testing.T, srv *httptest.Server, method, path, dir, body string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("x-opencode-directory", dir)
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, respBody
}

// TestSessionUpdateRenameSSE asserts PATCH /session/:id renames the session,
// returns the full updated session, and publishes session.updated{sessionID,info}
// (opencode session.update — handlers/session.ts:180-198).
func TestSessionUpdateRenameSSE(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()

	_, body := doReq(t, srv, http.MethodPost, "/session", dir)
	var created session.Info
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	coll := subscribeEvents(t, srv, dir)
	status, body := doReqBody(t, srv, http.MethodPatch, "/session/"+created.ID, dir,
		`{"title":"renamed"}`)
	if status != http.StatusOK {
		t.Fatalf("patch status = %d; body=%s", status, body)
	}
	var updated session.Info
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if updated.ID != created.ID || updated.Title != "renamed" {
		t.Errorf("patch response = %+v, want id=%s title=renamed", updated, created.ID)
	}
	if updated.Time.Archived != nil {
		t.Errorf("rename set archived: %+v", updated.Time)
	}
	ev := coll.waitFor(t, "session.updated")
	assertSessionEvent(t, ev, created.ID)
}

// TestSessionUpdateArchive asserts PATCH /session/:id with time.archived persists
// the archived timestamp, and that a subsequent {time:{archived:null}} is a no-op
// (opencode's UpdatePayload types archived as a finite number; null is dropped and
// archived stays set — observed dual-run contract; session.ts:731, groups/session.ts:51).
func TestSessionUpdateArchive(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()

	_, body := doReq(t, srv, http.MethodPost, "/session", dir)
	var created session.Info
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	status, body := doReqBody(t, srv, http.MethodPatch, "/session/"+created.ID, dir,
		`{"time":{"archived":1717000000000}}`)
	if status != http.StatusOK {
		t.Fatalf("archive status = %d; body=%s", status, body)
	}
	var archived session.Info
	if err := json.Unmarshal(body, &archived); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	if archived.Time.Archived == nil || *archived.Time.Archived != 1717000000000 {
		t.Errorf("archived = %v, want 1717000000000", archived.Time.Archived)
	}
	// GET reflects the persisted archived timestamp.
	_, body = doReq(t, srv, http.MethodGet, "/session/"+created.ID, dir)
	var got session.Info
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Time.Archived == nil || *got.Time.Archived != 1717000000000 {
		t.Errorf("persisted archived = %v, want 1717000000000", got.Time.Archived)
	}

	// {time:{archived:null}} is a no-op: opencode keeps the archived timestamp set
	// (the schema drops a null archived; there is no un-archive path).
	status, body = doReqBody(t, srv, http.MethodPatch, "/session/"+created.ID, dir,
		`{"time":{"archived":null}}`)
	if status != http.StatusOK {
		t.Fatalf("archived-null status = %d; body=%s", status, body)
	}
	var afterNull session.Info
	if err := json.Unmarshal(body, &afterNull); err != nil {
		t.Fatalf("decode archived-null: %v", err)
	}
	if afterNull.Time.Archived == nil || *afterNull.Time.Archived != 1717000000000 {
		t.Errorf("archived:null changed archived = %v, want 1717000000000 (no-op)", afterNull.Time.Archived)
	}
}

// TestSessionUpdateNotFound asserts PATCH on a missing session 404s with the
// standard NotFoundError envelope.
func TestSessionUpdateNotFound(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()

	status, body := doReqBody(t, srv, http.MethodPatch,
		"/session/ses_nonexistent00000000000000", dir, `{"title":"x"}`)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", status, body)
	}
	var nf map[string]any
	if err := json.Unmarshal(body, &nf); err != nil {
		t.Fatalf("decode 404: %v", err)
	}
	if nf["name"] != "NotFoundError" {
		t.Errorf("404 name = %v, want NotFoundError", nf["name"])
	}
}

// TestSessionUpdateBodyContract pins PATCH /session/{id} body handling to
// opencode's observed contract (verified live):
//   - empty / absent body            -> 400 BadRequest
//   - wrong-typed title / archived   -> 400 BadRequest
//   - unknown top-level field        -> 200, ignored (NOT rejected)
//   - malformed JSON                 -> 400 (Opcode42; opencode 500s — known divergence)
func TestSessionUpdateBodyContract(t *testing.T) {
	srv, _ := lifecycleServer(t)
	dir := t.TempDir()

	_, body := doReq(t, srv, http.MethodPost, "/session", dir)
	var created session.Info
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	path := "/session/" + created.ID

	assertBadRequest := func(name, reqBody string) {
		t.Helper()
		status, respBody := doReqBody(t, srv, http.MethodPatch, path, dir, reqBody)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", name, status, respBody)
		}
		var be map[string]any
		if err := json.Unmarshal(respBody, &be); err != nil {
			t.Fatalf("%s: decode 400: %v", name, err)
		}
		if be["name"] != "BadRequest" {
			t.Errorf("%s: 400 name = %v, want BadRequest", name, be["name"])
		}
	}

	// Empty / absent body -> 400 (opencode: "Expected object, got undefined").
	assertBadRequest("empty-body", "")
	assertBadRequest("whitespace-body", "   ")
	// Wrong-typed fields -> 400.
	assertBadRequest("title-non-string", `{"title":123}`)
	assertBadRequest("archived-non-number", `{"time":{"archived":"x"}}`)
	// Malformed JSON -> 400 (Opcode42's intentional divergence from opencode's 500).
	assertBadRequest("malformed-json", `{"title":`)

	// Unknown top-level field is IGNORED, returning 200 with the session (matching
	// opencode's runtime, which drops extra keys despite additionalProperties:false).
	status, respBody := doReqBody(t, srv, http.MethodPatch, path, dir, `{"bogus":1}`)
	if status != http.StatusOK {
		t.Fatalf("unknown-field: status = %d, want 200; body=%s", status, respBody)
	}
	var info session.Info
	if err := json.Unmarshal(respBody, &info); err != nil {
		t.Fatalf("unknown-field: decode 200: %v", err)
	}
	if info.ID != created.ID {
		t.Errorf("unknown-field: id = %q, want %q", info.ID, created.ID)
	}

	// An empty object body {} is a valid no-op -> 200.
	status, respBody = doReqBody(t, srv, http.MethodPatch, path, dir, `{}`)
	if status != http.StatusOK {
		t.Fatalf("empty-object: status = %d, want 200; body=%s", status, respBody)
	}
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
