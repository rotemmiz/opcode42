package oauth

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// callbackResult is delivered to the waiting authorize flow when the OAuth
// provider redirects the browser back to the loopback server.
type callbackResult struct {
	code string
	err  error
}

// pendingCallback is one in-flight loopback OAuth handshake. The state value is
// the CSRF guard: the redirect must echo it back exactly (xai.ts:462-469).
type pendingCallback struct {
	state string
	ch    chan callbackResult
}

// callbackServer is the SHARED loopback OAuth callback listener. Both provider
// OAuth (this package) and — when wired — MCP remote auth can register a pending
// handshake and await the browser redirect. It is the Go analogue of xai.ts
// startOAuthServer, generalized to multiple concurrent providers keyed by the
// `state` param.
//
// Security posture (plan 13 "Threat Model"; matches xai.ts):
//   - Binds 127.0.0.1 ONLY (loopback). Remote daemons expose the callback via an
//     operator-provided --oauth-callback-proxy-url, never by binding 0.0.0.0.
//   - The redirect must carry a `state` that matches a registered handshake;
//     mismatched/absent state is rejected as a CSRF attempt and never resolves a
//     waiter.
//   - Each handshake is single-use: the first matching redirect consumes it.
type callbackServer struct {
	// path is the redirect path the provider redirects to (e.g. "/callback").
	path string
	// proxyURL, when non-empty, is the externally reachable base URL an operator
	// put in front of the loopback listener (reverse proxy / SSH tunnel /
	// Cloudflare tunnel) so a headless/remote daemon can still complete the
	// browser redirect. The redirect_uri sent to the provider uses this base
	// instead of http://127.0.0.1:<port>. (plan 13: --oauth-callback-proxy-url,
	// addressing plan 03 remote-reachability risk #3.)
	proxyURL string

	mu      sync.Mutex
	srv     *http.Server
	port    int
	pending map[string]*pendingCallback // keyed by state
}

// newCallbackServer constructs a not-yet-listening callback server. proxyURL may
// be empty (loopback-only). path defaults to "/callback".
func newCallbackServer(path, proxyURL string) *callbackServer {
	if path == "" {
		path = "/callback"
	}
	return &callbackServer{
		path:     path,
		proxyURL: proxyURL,
		pending:  map[string]*pendingCallback{},
	}
}

// ensureStarted lazily binds the loopback listener and starts serving.
//
// wantPort is the provider's required fixed callback port (e.g. xAI's 56121), or
// 0 for "any free port". The first caller binds the listener; subsequent callers
// reuse it. If a later provider requires a different fixed port than the one
// already bound, that is an error — a single shared loopback listener can only
// own one port, so concurrent providers with conflicting pinned ports are not
// supported (none ship today; revisit with a per-port listener map if needed).
func (s *callbackServer) ensureStarted(wantPort int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		if wantPort != 0 && wantPort != s.port {
			return 0, fmt.Errorf("oauth callback server already bound to port %d, cannot also serve required port %d", s.port, wantPort)
		}
		return s.port, nil
	}
	// Loopback-only bind. wantPort==0 lets the OS pick a free port; we read it
	// back so the redirect_uri reflects the real port.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", wantPort))
	if err != nil {
		return 0, fmt.Errorf("oauth callback listen on 127.0.0.1:%d: %w", wantPort, err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc(s.path, s.handle)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.srv = srv
	// Capture srv in the goroutine rather than re-reading s.srv: shutdown may set
	// s.srv = nil before this goroutine runs, which would panic on a nil deref.
	go func() { _ = srv.Serve(ln) }()
	return s.port, nil
}

// redirectURI returns the redirect_uri to send to the OAuth provider for the
// given bound port: the operator proxy URL when configured, otherwise the
// loopback URL.
func (s *callbackServer) redirectURI(port int) string {
	if s.proxyURL != "" {
		base := s.proxyURL
		// Trim a single trailing slash so we don't double it with s.path.
		if n := len(base); n > 0 && base[n-1] == '/' {
			base = base[:n-1]
		}
		return base + s.path
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, s.path)
}

// register adds a pending handshake for state and returns a channel that
// receives the callback result (code or error). The caller must call
// unregister(state) when done (e.g. on timeout) to avoid leaking the entry.
func (s *callbackServer) register(state string) <-chan callbackResult {
	ch := make(chan callbackResult, 1)
	s.mu.Lock()
	s.pending[state] = &pendingCallback{state: state, ch: ch}
	s.mu.Unlock()
	return ch
}

// unregister removes a pending handshake (idempotent).
func (s *callbackServer) unregister(state string) {
	s.mu.Lock()
	delete(s.pending, state)
	s.mu.Unlock()
}

// handle services the browser redirect: validates state (CSRF guard), extracts
// the code or provider error, resolves the matching waiter exactly once, and
// renders a minimal close-this-window page.
func (s *callbackServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	provErr := q.Get("error")
	provErrDesc := q.Get("error_description")
	code := q.Get("code")

	// Look up and atomically consume the pending handshake. An unknown/empty
	// state is treated as a CSRF attempt and never resolves a waiter
	// (xai.ts:462-469).
	s.mu.Lock()
	pc := s.pending[state]
	if pc != nil {
		delete(s.pending, state)
	}
	s.mu.Unlock()

	if pc == nil {
		writeCallbackHTML(w, http.StatusBadRequest, "Invalid state — possible CSRF; no matching login is in progress.")
		return
	}

	if provErr != "" {
		msg := provErrDesc
		if msg == "" {
			msg = provErr
		}
		pc.ch <- callbackResult{err: fmt.Errorf("provider returned error: %s", msg)}
		writeCallbackHTML(w, http.StatusOK, "Authorization failed: "+msg)
		return
	}
	if code == "" {
		pc.ch <- callbackResult{err: errors.New("missing authorization code")}
		writeCallbackHTML(w, http.StatusBadRequest, "Missing authorization code.")
		return
	}
	pc.ch <- callbackResult{code: code}
	writeCallbackHTML(w, http.StatusOK, "Authorization successful. You can close this window and return to Opcode42.")
}

// shutdown stops the loopback listener (best-effort) and fails any waiters.
func (s *callbackServer) shutdown(ctx context.Context) {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	for state, pc := range s.pending {
		pc.ch <- callbackResult{err: errors.New("daemon shutting down")}
		delete(s.pending, state)
	}
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
}

// writeCallbackHTML renders a tiny status page for the user's browser. The
// message is escaped to keep provider-controlled error text inert (no XSS via
// error_description).
func writeCallbackHTML(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\">"+
		"<title>Opcode42 OAuth</title></head><body style=\"font-family:system-ui;text-align:center;margin-top:4rem\">"+
		"<h1>Opcode42</h1><p>%s</p></body></html>", html.EscapeString(message))
}

// validateProxyURL checks that an --oauth-callback-proxy-url is a well-formed
// absolute http(s) URL, returning a helpful error otherwise.
func validateProxyURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid oauth-callback-proxy-url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("oauth-callback-proxy-url must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("oauth-callback-proxy-url must include a host")
	}
	return nil
}
