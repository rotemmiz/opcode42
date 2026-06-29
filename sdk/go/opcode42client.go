package opcode42client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rotemmiz/opcode42/sdk/go/gen"
)

// Opcode42Client is the Opcode42/opencode SDK: the generated REST client (API) plus
// cross-cutting auth + directory-routing header injection, and a hand-written
// SSE client (sse.go) and a hand-written WebSocket-PTY client (pty.go).
//
// It is wire-generic: point it at a Opcode42 daemon or a real opencode daemon — the
// contract is identical (plan 06 / plan 08 "opencode now, Opcode42-ready").
type Opcode42Client struct {
	// API is the generated typed REST client; use it for any endpoint.
	API *gen.ClientWithResponses

	baseURL   string
	directory string
	auth      string // "Basic <b64>" or ""
	rest      *http.Client
	sse       *http.Client
}

// Options configures a Opcode42Client.
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

// New builds a Opcode42Client for the daemon at baseURL.
func New(baseURL string, opts Options) (*Opcode42Client, error) {
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
	c := &Opcode42Client{baseURL: trimURL(baseURL), directory: opts.Directory, auth: auth, rest: rest, sse: sse}

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
func (c *Opcode42Client) injectHeaders(_ context.Context, req *http.Request) error {
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
func (c *Opcode42Client) Health(ctx context.Context) error {
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

// GetJSON performs an authed GET on a path (relative to the base URL) and
// decodes the JSON response into dst. It is a pragmatic escape hatch for reads
// whose generated typed response is an awkward union (e.g. message lists);
// callers that want typed requests use API directly.
func (c *Opcode42Client) GetJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("Accept", "application/json")
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body) // drain for keep-alive reuse
		return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// PostJSON performs an authed POST of a JSON body to a path and, if dst is
// non-nil and the response carries a body, decodes the JSON response into it.
// Empty/204 responses are tolerated.
func (c *Opcode42Client) PostJSON(ctx context.Context, path string, body, dst any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body) // drain for keep-alive reuse
		return fmt.Errorf("POST %s: status %d", path, resp.StatusCode)
	}
	if dst == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// PatchJSON performs an authed PATCH of a JSON body to a path and, if dst is
// non-nil and a body is returned, decodes the JSON response into it.
func (c *Opcode42Client) PatchJSON(ctx context.Context, path string, body, dst any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("PATCH %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("PATCH %s: status %d", path, resp.StatusCode)
	}
	if dst == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// DeleteJSON performs an authed DELETE and decodes a JSON response body into dst
// (some endpoints, e.g. /session/{id}/share, return the updated resource).
func (c *Opcode42Client) DeleteJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("Accept", "application/json")
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("DELETE %s: status %d", path, resp.StatusCode)
	}
	if dst == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// Delete performs an authed DELETE on a path. Non-2xx is an error.
func (c *Opcode42Client) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE %s: status %d", path, resp.StatusCode)
	}
	return nil
}

// BaseURL returns the daemon base URL.
func (c *Opcode42Client) BaseURL() string { return c.baseURL }

func trimURL(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
