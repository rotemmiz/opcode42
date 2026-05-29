package auth

import (
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
		if user != c.Username || pass != c.Password {
			w.Header().Set("WWW-Authenticate", wwwAuthenticate)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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
