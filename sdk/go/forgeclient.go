package forgeclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rotemmiz/forge/sdk/go/gen"
)

// ForgeClient is the Forge/opencode SDK: the generated REST client (API) plus
// cross-cutting auth + directory-routing header injection, and a hand-written
// SSE client (sse.go). A WebSocket-PTY client is forthcoming (plan 06).
//
// It is wire-generic: point it at a Forge daemon or a real opencode daemon — the
// contract is identical (plan 06 / plan 08 "opencode now, Forge-ready").
type ForgeClient struct {
	// API is the generated typed REST client; use it for any endpoint.
	API *gen.ClientWithResponses

	baseURL   string
	directory string
	auth      string // "Basic <b64>" or ""
	rest      *http.Client
	sse       *http.Client
}

// Options configures a ForgeClient.
type Options struct {
	// Directory is sent as x-opencode-directory (per-request routing). Optional.
	Directory string
	// Username/Password produce Basic auth. Ignored if AuthToken is set.
	Username string
	Password string
	// AuthToken is a pre-encoded base64("user:pass") for Basic auth (also usable
	// as ?auth_token= on WS connections). Takes precedence over Username/Password.
	AuthToken string
	// HTTPClient overrides the REST client's transport (defaults to a fresh client).
	HTTPClient *http.Client
}

// New builds a ForgeClient for the daemon at baseURL.
func New(baseURL string, opts Options) (*ForgeClient, error) {
	auth := ""
	switch {
	case opts.AuthToken != "":
		auth = "Basic " + opts.AuthToken
	case opts.Username != "":
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(opts.Username+":"+opts.Password))
	}
	// REST gets a default request timeout; SSE must NOT (it's a long-lived
	// stream). A caller-supplied HTTPClient is used for REST as-is; the SSE
	// client reuses its transport with the timeout cleared.
	rest := opts.HTTPClient
	if rest == nil {
		rest = &http.Client{Timeout: 30 * time.Second}
	}
	sse := &http.Client{Transport: rest.Transport}
	c := &ForgeClient{baseURL: trimURL(baseURL), directory: opts.Directory, auth: auth, rest: rest, sse: sse}

	api, err := gen.NewClientWithResponses(c.baseURL,
		gen.WithHTTPClient(rest),
		gen.WithRequestEditorFn(c.injectHeaders),
	)
	if err != nil {
		return nil, err
	}
	c.API = api
	return c, nil
}

// injectHeaders adds auth + directory routing to every generated request.
func (c *ForgeClient) injectHeaders(_ context.Context, req *http.Request) error {
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	if c.directory != "" {
		// PathEscape (not QueryEscape): opencode decodes this header with JS
		// decodeURIComponent, which turns '+' into a literal '+', not a space.
		// PathEscape encodes space as %20 so directories with spaces route right.
		req.Header.Set("X-Opencode-Directory", url.PathEscape(c.directory))
	}
	return nil
}

// Health checks the daemon is reachable and authorized via GET /global/health.
func (c *ForgeClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/global/health", nil)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("health: unauthorized (check --username/--password)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: status %d", resp.StatusCode)
	}
	return nil
}

// BaseURL returns the daemon base URL.
func (c *ForgeClient) BaseURL() string { return c.baseURL }

func trimURL(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
