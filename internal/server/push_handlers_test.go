package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/push"
	"github.com/rotemmiz/opcode42/internal/storage"
)

func pushServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h, err := New(Options{
		Version: "test",
		Auth:    auth.Config{},
		Cwd:     t.TempDir(),
		Push:    push.NewStore(db.DB),
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return h
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

func TestPushRegisterLifecycle(t *testing.T) {
	h := pushServer(t)

	rr := do(t, h, http.MethodPost, "/push/register",
		`{"device_id":"dev1","fcm_token":"tok1","platform":"android"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("register = %d, body=%s", rr.Code, rr.Body.String())
	}

	rr = do(t, h, http.MethodGet, "/push/register", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "dev1") {
		t.Errorf("list should contain dev1: %s", rr.Body.String())
	}

	rr = do(t, h, http.MethodDelete, "/push/register/dev1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("unregister = %d", rr.Code)
	}

	rr = do(t, h, http.MethodDelete, "/push/register/dev1", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unregister missing = %d, want 404", rr.Code)
	}
}

func TestPushRegisterValidation(t *testing.T) {
	h := pushServer(t)

	rr := do(t, h, http.MethodPost, "/push/register", `{"device_id":"dev1"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fcm_token = %d, want 400", rr.Code)
	}

	rr = do(t, h, http.MethodPost, "/push/register", `not json`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", rr.Code)
	}
}

// TestPushDisabledFallsThroughTo501 confirms that without a Push store the
// endpoints are not wired as real handlers (they fall through to the 501 stub
// or, for a path absent from the reference, 404). /push/register is a Opcode42
// known-addition not in the frozen contract, so absent it is 404.
func TestPushDisabledNotWired(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h, err := New(Options{Version: "test", Auth: auth.Config{}, Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	rr := do(t, h, http.MethodPost, "/push/register", `{"device_id":"d","fcm_token":"t"}`)
	if rr.Code == http.StatusOK {
		t.Fatalf("push endpoints should not be wired without a Push store; got 200")
	}
}
