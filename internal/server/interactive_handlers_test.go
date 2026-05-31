package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/question"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/worktree"
)

// interactiveServer builds a server wired with instances, sessions, and a todo
// store, returning the handler plus the deps the tests drive directly.
func interactiveServer(t *testing.T) (http.Handler, *instance.Manager, *session.Store, *tool.TodoStore) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))
	db, err := storage.Open(filepath.Join(t.TempDir(), "forge.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	g := bus.NewGlobal()
	instances := instance.NewManager(g)
	sessions := session.NewStore(db)
	todos := tool.NewTodoStore()
	h, err := New(Options{
		Version:   "0.0.1",
		Auth:      auth.Config{},
		Cwd:       t.TempDir(),
		Sessions:  sessions,
		Instances: instances,
		Global:    g,
		Todos:     todos,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, instances, sessions, todos
}

func postJSON(t *testing.T, h http.Handler, path, dir string, body any) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(http.MethodPost, path, &buf)
	if dir != "" {
		r.Header.Set("x-opencode-directory", dir)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	out, _ := io.ReadAll(rr.Body)
	return rr, out
}

func TestPermissionReply_Once(t *testing.T) {
	h, instances, _, _ := interactiveServer(t)
	dir := t.TempDir()
	inst := instances.Get(worktree.Resolve(dir))

	asked := make(chan error, 1)
	go func() {
		asked <- inst.Permissions.Ask(context.Background(), permission.AskInput{
			SessionID: "ses_1", Permission: "bash", Patterns: []string{"ls"},
		})
	}()
	id := waitPermission(t, inst.Permissions)

	rr, body := postJSON(t, h, "/permission/"+id+"/reply", dir, map[string]string{"reply": "once"})
	if rr.Code != http.StatusOK || string(bytesTrim(body)) != "true" {
		t.Fatalf("reply status=%d body=%s", rr.Code, body)
	}
	select {
	case err := <-asked:
		if err != nil {
			t.Fatalf("ask resolved with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ask never unblocked")
	}
}

func TestPermissionReply_Errors(t *testing.T) {
	h, _, _, _ := interactiveServer(t)
	dir := t.TempDir()

	rr, _ := postJSON(t, h, "/permission/perm_missing/reply", dir, map[string]string{"reply": "once"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown id status = %d (want 404)", rr.Code)
	}
	rr, _ = postJSON(t, h, "/permission/perm_missing/reply", dir, map[string]string{"reply": "maybe"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad enum status = %d (want 400)", rr.Code)
	}
}

func TestQuestionReplyAndReject(t *testing.T) {
	h, instances, _, _ := interactiveServer(t)
	dir := t.TempDir()
	inst := instances.Get(worktree.Resolve(dir))

	// Reply path.
	answered := make(chan [][]string, 1)
	go func() {
		ans, _ := inst.Questions.Ask(context.Background(), "ses_1",
			[]question.Info{{Question: "color?", Header: "Color", Options: []question.Option{{Label: "red"}, {Label: "blue"}}}})
		answered <- ans
	}()
	id := waitQuestion(t, inst.Questions)
	rr, body := postJSON(t, h, "/question/"+id+"/reply", dir, map[string]any{"answers": [][]string{{"blue"}}})
	if rr.Code != http.StatusOK || string(bytesTrim(body)) != "true" {
		t.Fatalf("reply status=%d body=%s", rr.Code, body)
	}
	select {
	case ans := <-answered:
		if len(ans) != 1 || len(ans[0]) != 1 || ans[0][0] != "blue" {
			t.Fatalf("answers = %v", ans)
		}
	case <-time.After(time.Second):
		t.Fatal("ask never unblocked")
	}

	// Reject path.
	rejected := make(chan error, 1)
	go func() {
		_, err := inst.Questions.Ask(context.Background(), "ses_1",
			[]question.Info{{Question: "ok?", Header: "OK"}})
		rejected <- err
	}()
	id = waitQuestion(t, inst.Questions)
	rr, _ = postJSON(t, h, "/question/"+id+"/reject", dir, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject status = %d", rr.Code)
	}
	if err := <-rejected; err != question.ErrRejected {
		t.Fatalf("reject err = %v", err)
	}

	// Unknown id → 404.
	rr, _ = postJSON(t, h, "/question/qst_missing/reply", dir, map[string]any{"answers": [][]string{}})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown reply status = %d (want 404)", rr.Code)
	}
}

func TestTodoList(t *testing.T) {
	h, _, sessions, todos := interactiveServer(t)
	dir := t.TempDir()
	info, err := sessions.Create(context.Background(), worktree.Resolve(dir))
	if err != nil {
		t.Fatal(err)
	}
	todos.Set(info.ID, []tool.TodoItem{
		{ID: "1", Content: "build", Status: "in_progress", Priority: "high"},
		{ID: "2", Content: "ship", Status: "pending", Priority: "low"},
	})

	r := httptest.NewRequest(http.MethodGet, "/session/"+info.ID+"/todo", nil)
	r.Header.Set("x-opencode-directory", dir)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var out []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	first := out[0]
	if first["content"] != "build" || first["status"] != "in_progress" || first["priority"] != "high" {
		t.Fatalf("first todo = %v", first)
	}
	if _, ok := first["id"]; ok {
		t.Fatalf("wire shape leaked id: %v", first)
	}

	// Unknown session → 404.
	rr2, _ := req(t, h, http.MethodGet, "/session/ses_missing/todo", dir)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d (want 404)", rr2.Code)
	}
}

func waitPermission(t *testing.T, m *permission.Manager) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if list := m.List(); len(list) > 0 {
			return list[0].ID
		}
		select {
		case <-deadline:
			t.Fatal("permission never registered")
		case <-time.After(time.Millisecond):
		}
	}
}

func waitQuestion(t *testing.T, m *question.Manager) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if list := m.List(); len(list) > 0 {
			return list[0].ID
		}
		select {
		case <-deadline:
			t.Fatal("question never registered")
		case <-time.After(time.Millisecond):
		}
	}
}

func bytesTrim(b []byte) []byte { return bytes.TrimSpace(b) }
