package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setAuthDir points the credstore at a temp XDG_DATA_HOME so tests never touch a
// real ~/.local/share/opencode/auth.json.
func setAuthDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	return filepath.Join(dir, "opencode", "auth.json")
}

func TestPKCEChallengeIsS256(t *testing.T) {
	p, err := newPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Verifier) != 64 {
		t.Fatalf("verifier length = %d, want 64", len(p.Verifier))
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Fatalf("challenge mismatch: got %q want %q", p.Challenge, want)
	}
	// base64url alphabet: no +, /, or = padding.
	if strings.ContainsAny(p.Challenge, "+/=") {
		t.Fatalf("challenge not base64url-safe: %q", p.Challenge)
	}
}

func TestNewStateIsUnique(t *testing.T) {
	a, err := newState()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newState()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two states collided")
	}
}

func TestValidateProxyURL(t *testing.T) {
	for _, ok := range []string{"", "http://localhost:9000", "https://opcode42.example.com", "https://x.com/cb/"} {
		if err := validateProxyURL(ok); err != nil {
			t.Errorf("validateProxyURL(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"ftp://x", "://noscheme", "https://"} {
		if err := validateProxyURL(bad); err == nil {
			t.Errorf("validateProxyURL(%q) = nil, want error", bad)
		}
	}
}

func TestRedirectURI(t *testing.T) {
	loop := newCallbackServer("/callback", "")
	if got := loop.redirectURI(56000); got != "http://127.0.0.1:56000/callback" {
		t.Errorf("loopback redirectURI = %q", got)
	}
	proxied := newCallbackServer("/callback", "https://opcode42.example.com/")
	if got := proxied.redirectURI(56000); got != "https://opcode42.example.com/callback" {
		t.Errorf("proxied redirectURI = %q", got)
	}
}

func TestXaiRegistersFixedCallbackPort(t *testing.T) {
	if got := newXaiProvider().CallbackPort(); got != xaiCallbackPort {
		t.Fatalf("xai CallbackPort = %d, want %d (registered redirect_uri port)", got, xaiCallbackPort)
	}
}

func TestEnsureStartedPortConflict(t *testing.T) {
	s := newCallbackServer("/callback", "")
	port, err := s.ensureStarted(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown(context.Background())
	// Requesting the same OS-assigned port is fine.
	if _, err := s.ensureStarted(port); err != nil {
		t.Fatalf("re-ensure same port: %v", err)
	}
	// A different required port on an already-bound server is an error.
	if _, err := s.ensureStarted(port + 1); err == nil {
		t.Fatal("expected conflict error for a different required port")
	}
}

func TestCallbackServerBindsLoopbackOnly(t *testing.T) {
	s := newCallbackServer("/callback", "")
	port, err := s.ensureStarted(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown(context.Background())
	if port == 0 {
		t.Fatal("port not assigned")
	}
	// Idempotent: second call returns same port, no rebind.
	if p2, _ := s.ensureStarted(0); p2 != port {
		t.Fatalf("ensureStarted not idempotent: %d != %d", p2, port)
	}
	// The listener must be on 127.0.0.1 — confirm a loopback request succeeds and
	// the rejection path (no pending state) returns 400.
	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?state=nope&code=x")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown-state callback status = %d, want 400", resp.StatusCode)
	}
}

func TestCallbackServerRejectsCSRFState(t *testing.T) {
	s := newCallbackServer("/callback", "")
	port, err := s.ensureStarted(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown(context.Background())
	ch := s.register("good-state")
	defer s.unregister("good-state")

	// Redirect with a mismatched state must NOT resolve the waiter.
	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?state=evil&code=abc")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	select {
	case r := <-ch:
		t.Fatalf("waiter resolved on bad state: %+v", r)
	case <-time.After(100 * time.Millisecond):
		// expected: no delivery
	}
}

func TestCallbackServerDeliversCode(t *testing.T) {
	s := newCallbackServer("/callback", "")
	port, err := s.ensureStarted(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown(context.Background())
	ch := s.register("state123")

	resp, err := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?state=state123&code=the-code")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	select {
	case r := <-ch:
		if r.err != nil || r.code != "the-code" {
			t.Fatalf("got %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no callback delivered")
	}
	// Single-use: state consumed, a second redirect finds no pending handshake.
	resp2, _ := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?state=state123&code=again")
	if resp2 != nil {
		if resp2.StatusCode != http.StatusBadRequest {
			t.Fatalf("replay status = %d, want 400 (consumed)", resp2.StatusCode)
		}
		_ = resp2.Body.Close()
	}
}

func TestCallbackServerProviderError(t *testing.T) {
	s := newCallbackServer("/callback", "")
	port, err := s.ensureStarted(0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.shutdown(context.Background())
	ch := s.register("st")

	resp, _ := http.Get("http://127.0.0.1:" + itoa(port) + "/callback?state=st&error=access_denied&error_description=user+said+no")
	if resp != nil {
		_ = resp.Body.Close()
	}
	select {
	case r := <-ch:
		if r.err == nil || !strings.Contains(r.err.Error(), "user said no") {
			t.Fatalf("expected provider error, got %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no error delivered")
	}
}

// newTestXaiService builds a Service whose only provider is an xai provider
// pointed at a fake token endpoint, with the real authorize URL replaced so the
// generated URL is inspectable.
func newTestXaiService(t *testing.T, tokenURL, authorizeURL string) *Service {
	t.Helper()
	xp := newXaiProvider()
	xp.tokenURL = tokenURL
	xp.authorizeURL = authorizeURL
	xp.callbackPort = 0 // OS-assigned port so parallel test runs never clash on 56121
	s := &Service{
		providers: map[string]Provider{xp.ID(): xp},
		cbServer:  newCallbackServer("/callback", ""),
		pending:   map[string]*pending{},
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s
}

func TestMethodsListsBuiltins(t *testing.T) {
	s, err := NewService("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	m := s.Methods()
	if _, ok := m["xai"]; !ok {
		t.Fatalf("xai not in methods: %v", m)
	}
	if m["xai"][0].Type != "oauth" {
		t.Fatalf("xai method type = %q", m["xai"][0].Type)
	}
}

func TestAuthorizeUnknownProvider(t *testing.T) {
	s, err := NewService("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	_, err = s.Authorize(context.Background(), "nope", 0, nil)
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
}

func TestCallbackWithoutAuthorize(t *testing.T) {
	s, err := NewService("")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())
	err = s.Callback(context.Background(), "xai", "")
	if !errors.Is(err, ErrOauthMissing) {
		t.Fatalf("err = %v, want ErrOauthMissing", err)
	}
}

// TestFullLoopbackFlow exercises authorize → simulated browser redirect →
// callback → persisted token, all against a fake xAI token endpoint. No real
// network or browser is involved.
func TestFullLoopbackFlow(t *testing.T) {
	authPath := setAuthDir(t)

	var gotForm url.Values
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"acc-1","refresh_token":"ref-1","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	s := newTestXaiService(t, tokenSrv.URL, "https://auth.example/authorize")

	auth, err := s.Authorize(context.Background(), "xai", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Method != "auto" {
		t.Fatalf("method = %q, want auto", auth.Method)
	}
	// Authorize URL must carry PKCE S256 + state + our loopback redirect_uri.
	u, err := url.Parse(auth.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE challenge: %v", q)
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("missing state")
	}
	redirect := q.Get("redirect_uri")
	if !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		t.Fatalf("redirect_uri not loopback: %q", redirect)
	}

	// Simulate the browser redirect to the loopback callback server, then run
	// Callback concurrently (it blocks until the redirect is captured).
	done := make(chan error, 1)
	go func() { done <- s.Callback(context.Background(), "xai", "") }()

	// Give Callback a beat to start waiting, then fire the redirect.
	time.Sleep(50 * time.Millisecond)
	resp, err := http.Get(redirect + "?state=" + url.QueryEscape(state) + "&code=auth-code-xyz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if err := <-done; err != nil {
		t.Fatalf("callback failed: %v", err)
	}

	// The token endpoint must have received the authorization_code grant with the
	// PKCE verifier and our code.
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Fatalf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "auth-code-xyz" {
		t.Fatalf("code = %q", gotForm.Get("code"))
	}
	if gotForm.Get("code_verifier") == "" {
		t.Fatal("missing code_verifier in exchange")
	}

	// Persisted record must be the opencode oauth shape.
	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("auth.json not written: %v", err)
	}
	var store map[string]oauthRecord
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatal(err)
	}
	rec := store["xai"]
	if rec.Type != "oauth" || rec.Access != "acc-1" || rec.Refresh != "ref-1" {
		t.Fatalf("bad persisted record: %+v", rec)
	}
	if rec.Expires == 0 {
		t.Fatal("expires not set from expires_in")
	}
}

// TestCallbackExchangeFailure verifies a non-200 token response surfaces as
// ErrOauthCallbackFailed and nothing is persisted.
func TestCallbackExchangeFailure(t *testing.T) {
	authPath := setAuthDir(t)
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenSrv.Close()

	s := newTestXaiService(t, tokenSrv.URL, "https://auth.example/authorize")
	auth, err := s.Authorize(context.Background(), "xai", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(auth.URL)
	state := u.Query().Get("state")
	redirect := u.Query().Get("redirect_uri")

	done := make(chan error, 1)
	go func() { done <- s.Callback(context.Background(), "xai", "") }()
	time.Sleep(50 * time.Millisecond)
	resp, _ := http.Get(redirect + "?state=" + url.QueryEscape(state) + "&code=c")
	if resp != nil {
		_ = resp.Body.Close()
	}
	err = <-done
	if !errors.Is(err, ErrOauthCallbackFailed) {
		t.Fatalf("err = %v, want ErrOauthCallbackFailed", err)
	}
	if _, statErr := os.Stat(authPath); statErr == nil {
		t.Fatal("auth.json written despite exchange failure")
	}
}

// itoa avoids importing strconv just for ports in tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
