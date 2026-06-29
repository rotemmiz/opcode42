package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
)

// newBackedServer builds a server with a real SQLite store and an empty config
// home, for exercising the M1/M2 endpoints end-to-end.
func newBackedServer(t *testing.T, authCfg auth.Config) http.Handler {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))
	for _, k := range []string{
		"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT",
		"OPENCODE_DISABLE_PROJECT_CONFIG",
	} {
		t.Setenv(k, "")
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	g := bus.NewGlobal()
	instances := instance.NewManager(g)
	sessions := session.NewStore(db).WithBus(func(directory string) session.EventPublisher {
		return instances.BusFor(directory)
	})
	h, err := New(Options{
		Version:   "0.0.1",
		Auth:      authCfg,
		Cwd:       t.TempDir(),
		Sessions:  sessions,
		Instances: instances,
		Global:    g,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func req(t *testing.T, h http.Handler, method, path, dir string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if dir != "" {
		r.Header.Set("x-opencode-directory", dir)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	body, _ := io.ReadAll(rr.Body)
	return rr, body
}

func TestConfigEndpointDefaults(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	rr, body := req(t, h, http.MethodGet, "/config", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, body)
	}
	var cfg map[string]any
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg["$schema"] != "https://opencode.ai/config.json" {
		t.Errorf("$schema = %v", cfg["$schema"])
	}
	if _, ok := cfg["username"].(string); !ok {
		t.Errorf("username missing: %v", cfg)
	}
}

func TestSessionCRUDFlow(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()

	rr, body := req(t, h, http.MethodPost, "/session", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status = %d; body=%s", rr.Code, body)
	}
	var created session.Info
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if !strings.HasPrefix(created.ID, "ses_") {
		t.Errorf("bad id %q", created.ID)
	}

	rr, body = req(t, h, http.MethodGet, "/session", dir)
	var list []session.Info
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if rr.Code != http.StatusOK || len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list status=%d body=%s", rr.Code, body)
	}

	rr, _ = req(t, h, http.MethodGet, "/session/"+created.ID, dir)
	if rr.Code != http.StatusOK {
		t.Errorf("get status = %d, want 200", rr.Code)
	}

	rr, body = req(t, h, http.MethodDelete, "/session/"+created.ID, dir)
	if rr.Code != http.StatusOK || strings.TrimSpace(string(body)) != "true" {
		t.Errorf("delete status=%d body=%q, want 200 true", rr.Code, body)
	}

	rr, body = req(t, h, http.MethodGet, "/session/"+created.ID, dir)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete status = %d, want 404; body=%s", rr.Code, body)
	}
	var nf map[string]any
	if err := json.Unmarshal(body, &nf); err != nil {
		t.Fatalf("decode 404: %v", err)
	}
	if nf["name"] != "NotFoundError" {
		t.Errorf("404 name = %v, want NotFoundError", nf["name"])
	}
}

func TestForkAndChildrenEndpoints(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()

	_, body := req(t, h, http.MethodPost, "/session", dir)
	var parent session.Info
	if err := json.Unmarshal(body, &parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	rr, body := req(t, h, http.MethodPost, "/session/"+parent.ID+"/fork", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("fork status = %d; body=%s", rr.Code, body)
	}
	var forked session.Info
	if err := json.Unmarshal(body, &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	if forked.ID == parent.ID || !strings.HasSuffix(forked.Title, "(fork #1)") {
		t.Errorf("fork = %+v", forked)
	}

	rr, body = req(t, h, http.MethodGet, "/session/"+parent.ID+"/children", dir)
	if rr.Code != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("children status=%d body=%q, want 200 []", rr.Code, body)
	}
}

func TestAuthEnforcedOnConfig(t *testing.T) {
	h := newBackedServer(t, auth.Config{Username: "opencode", Password: "secret"})
	rr, _ := req(t, h, http.MethodGet, "/config", "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-auth status = %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header")
	}
}
