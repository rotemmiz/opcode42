package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"regexp"
	"strings"
)

const (
	defaultUsername = "opencode"
	// wwwAuthenticate is the exact challenge opencode sends on 401
	// (authorization.ts:11).
	wwwAuthenticate = `Basic realm="Secure Area"`
	authTokenQuery  = "auth_token"
)

var basicRe = regexp.MustCompile(`(?i)^Basic\s+(.+)$`)

// ptyConnectPathRe matches the PTY connect WebSocket path (pty-ticket.ts:5).
var ptyConnectPathRe = regexp.MustCompile(`^/pty/[^/]+/connect$`)

// hasPtyConnectTicket reports whether r is a PTY connect request carrying a
// ?ticket=, in which case Basic auth is skipped (pty-ticket.ts:16-18).
func hasPtyConnectTicket(r *http.Request) bool {
	return ptyConnectPathRe.MatchString(r.URL.Path) && r.URL.Query().Get("ticket") != ""
}

// Config holds the resolved auth settings for the daemon.
type Config struct {
	Username string
	Password string
}

// Required reports whether auth is active. opencode treats auth as required only
// when the password env var is set and non-empty (auth.ts:24-34).
func (c Config) Required() bool { return c.Password != "" }

// FromEnv reads the opencode auth env vars. Username defaults to "opencode".
func FromEnv() Config {
	user := os.Getenv("OPENCODE_SERVER_USERNAME")
	if user == "" {
		user = defaultUsername
	}
	return Config{Username: user, Password: os.Getenv("OPENCODE_SERVER_PASSWORD")}
}

// loopbackHosts are the bind hosts treated as loopback-only (not exposed beyond
// the local machine). Kept here rather than imported from the mdns package to
// avoid an import cycle and to keep the security decision self-contained.
var loopbackHosts = map[string]struct{}{
	"127.0.0.1": {},
	"localhost": {},
	"::1":       {},
	"":          {},
}

// IsLoopbackHost reports whether host binds only the local machine.
func IsLoopbackHost(host string) bool {
	_, ok := loopbackHosts[strings.ToLower(strings.TrimSpace(host))]
	return ok
}

// CheckBindExposure enforces Forge's stronger-than-opencode default: a
// non-loopback bind requires a password. opencode merely warns
// (cli/cmd/serve.ts:15); Forge refuses (plan 13 §"Defaults": "0.0.0.0 bind
// requires a password; daemon refuses to start otherwise"). It returns a
// non-nil error the caller should treat as a hard start failure when host is
// exposed and auth is not required; otherwise nil.
func (c Config) CheckBindExposure(host string) error {
	if !c.Required() && !IsLoopbackHost(host) {
		return &ExposureError{Host: host}
	}
	return nil
}

// ExposureError is returned by CheckBindExposure when a non-loopback bind is
// requested without a password.
type ExposureError struct{ Host string }

func (e *ExposureError) Error() string {
	return "refusing to bind non-loopback host " + e.Host +
		" without a password: set OPENCODE_SERVER_PASSWORD (or bind 127.0.0.1)"
}

// Middleware enforces auth. When auth is not required it is a pass-through.
// Otherwise it accepts the credential from ?auth_token or the Authorization
// header and, on failure, responds 401 with a WWW-Authenticate challenge and an
// empty body (matching opencode's validateRawCredential, authorization.ts:89-103).
func (c Config) Middleware(next http.Handler) http.Handler {
	if !c.Required() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A PTY connect WebSocket carrying a ?ticket= bypasses Basic auth; the
		// connect handler validates the single-use ticket instead
		// (server/shared/pty-ticket.ts:5-18). This lets browsers, which cannot
		// set Authorization on a WebSocket, attach with a minted ticket.
		if hasPtyConnectTicket(r) {
			next.ServeHTTP(w, r)
			return
		}
		user, pass := credentialFromRequest(r)
		if !c.authorized(user, pass) {
			w.Header().Set("WWW-Authenticate", wwwAuthenticate)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether the presented (user, pass) match the configured
// credentials. Unlike opencode's plain `===` compare (auth.ts:30), Forge uses
// crypto/subtle.ConstantTimeCompare so the response time does not leak how many
// leading bytes of the password were correct (plan 13 §"Auth", threat row
// "Timing attacks on password compare"). Both fields are compared
// unconditionally — using `&` rather than `&&` — so a username mismatch does not
// short-circuit and reveal, via timing, whether the username was right.
func (c Config) authorized(user, pass string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(c.Username))
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(c.Password))
	return userOK&passOK == 1
}

// credentialFromRequest extracts (username, password) from the request: the
// ?auth_token query param takes precedence over the Authorization: Basic header,
// and both carry standard base64(user:pass) (authorization.ts:81-87).
func credentialFromRequest(r *http.Request) (string, string) {
	if token := r.URL.Query().Get(authTokenQuery); token != "" {
		return decodeCredential(token)
	}
	if m := basicRe.FindStringSubmatch(r.Header.Get("Authorization")); m != nil {
		return decodeCredential(m[1])
	}
	return "", ""
}

// decodeCredential base64-decodes input and splits on the first ':'. A decode
// failure or a missing ':' yields empty credentials (authorization.ts:61-74).
func decodeCredential(input string) (string, string) {
	raw, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return "", ""
	}
	s := string(raw)
	i := strings.IndexByte(s, ':')
	if i == -1 {
		return "", ""
	}
	return s[:i], s[i+1:]
}
