// Package conformance is the dual-run harness: it drives a set of scenarios
// against a target daemon (opencode or forge) and writes a normalized result
// file that the diff tool (conformance/cmd/diff) compares against another run.
package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rotemmiz/forge/conformance/normalize"
	"github.com/rotemmiz/forge/conformance/result"
)

// AuthMode selects how a request authenticates.
type AuthMode int

// Auth modes for a request.
const (
	AuthDefault AuthMode = iota // Basic header when the client has credentials
	AuthNone                    // send no credentials (to exercise 401)
	AuthToken                   // ?auth_token=base64(user:pass), no Basic header
)

// DirMode selects how the per-directory instance is addressed.
type DirMode int

// Directory-addressing modes for a request.
const (
	DirHeader DirMode = iota // x-opencode-directory header (the usual path)
	DirQuery                 // ?directory=<dir> query param, no header
	DirNone                  // neither (exercise the cwd fallback)
)

// ReqOpts tunes a single request for auth/routing conformance scenarios.
type ReqOpts struct {
	Auth    AuthMode
	Dir     DirMode
	Capture []string // response header names to record into the step
	Body    any
}

// Client is a thin HTTP client for one target daemon. It injects the directory
// header and optional Basic auth, and normalizes every captured response so two
// runs are directly comparable.
type Client struct {
	BaseURL  string
	Dir      string // x-opencode-directory
	User     string // optional Basic-auth user
	Pass     string // optional Basic-auth pass
	Norm     *normalize.Normalizer
	HTTP     *http.Client
	lastBody []byte // raw bytes of the most recent response, for chaining steps
}

// NewClient builds a client whose normalizer maps the directory (and its
// symlink-resolved form) to <path>.
func NewClient(baseURL, dir string, paths ...string) *Client {
	return newClient(baseURL, dir, false, paths...)
}

// NewLiveClient is NewClient with the live normalizer (model-output masking) and
// a longer HTTP timeout, for the live dual-run scenarios that drive a real LLM.
func NewLiveClient(baseURL, dir string, paths ...string) *Client {
	return newClient(baseURL, dir, true, paths...)
}

func newClient(baseURL, dir string, live bool, paths ...string) *Client {
	norm := normalize.New(append([]string{dir}, paths...)...)
	timeout := 30 * time.Second
	if live {
		norm = normalize.NewLive(append([]string{dir}, paths...)...)
		timeout = 5 * time.Minute // a real model round-trip (incl. tool loops) is slow
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Dir:     dir,
		Norm:    norm,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) buildRequest(ctx context.Context, method, path string, o ReqOpts) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if o.Dir == DirQuery && c.Dir != "" {
		q.Set("directory", c.Dir)
	}
	if o.Auth == AuthToken {
		q.Set("auth_token", base64.StdEncoding.EncodeToString([]byte(c.User+":"+c.Pass)))
	}
	u.RawQuery = q.Encode()

	var raw []byte
	if o.Body != nil {
		b, err := json.Marshal(o.Body)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	var body io.Reader
	if raw != nil {
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if o.Dir == DirHeader && c.Dir != "" {
		req.Header.Set("x-opencode-directory", c.Dir)
	}
	if o.Auth == AuthDefault && (c.User != "" || c.Pass != "") {
		req.SetBasicAuth(c.User, c.Pass)
	}
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Do performs a standard request (Basic auth, directory header) and returns a
// normalized step.
func (c *Client) Do(stepName, method, path string, body any) (result.Step, error) {
	return c.Probe(stepName, method, path, ReqOpts{Body: body})
}

// Probe performs a request with explicit auth/routing options and optional
// response-header capture — used by the auth and directory-routing scenarios.
func (c *Client) Probe(stepName, method, path string, o ReqOpts) (result.Step, error) {
	req, err := c.buildRequest(context.Background(), method, path, o)
	if err != nil {
		return result.Step{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return result.Step{}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return result.Step{}, err
	}
	c.lastBody = respBody

	step := result.Step{
		Name:   stepName,
		Method: method,
		Path:   c.Norm.Path(path),
		Status: resp.StatusCode,
		Body:   c.normalizeBody(method, path, respBody),
	}
	if len(o.Capture) > 0 {
		step.Headers = map[string]string{}
		for _, h := range o.Capture {
			step.Headers[strings.ToLower(h)] = resp.Header.Get(h)
		}
	}
	return step, nil
}

// LastJSON unmarshals the most recent response body, for chaining steps (e.g.
// reading the created session's id to build the next request path).
func (c *Client) LastJSON(v any) error {
	return json.Unmarshal(c.lastBody, v)
}

// SSE connects to an SSE endpoint and captures up to maxEvents events (or until
// wait elapses), returning a step whose SSE field holds the normalized events.
func (c *Client) SSE(stepName, path string, wait time.Duration, maxEvents int) (result.Step, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	req, err := c.buildRequest(ctx, http.MethodGet, path, ReqOpts{})
	if err != nil {
		return result.Step{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return result.Step{}, fmt.Errorf("SSE %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var events []string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		norm, err := c.Norm.NormalizeJSON([]byte(strings.TrimSpace(data)))
		if err != nil {
			return result.Step{}, err
		}
		events = append(events, string(norm))
		if len(events) >= maxEvents {
			break
		}
	}
	// Surface a truncated capture (deadline hit mid-stream) rather than silently
	// returning a short event list that would only show up as a diff later.
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return result.Step{}, fmt.Errorf("SSE %s: %w", path, err)
	}
	if len(events) == 0 {
		return result.Step{}, fmt.Errorf("SSE %s: no events captured before %s timeout", path, wait)
	}
	return result.Step{Name: stepName, Method: http.MethodGet, Path: path, SSE: events}, nil
}

// normalizeBody normalizes a JSON response body; non-JSON bodies are returned
// trimmed and verbatim. Two GET lists are normalized order-insensitively (a
// canonical-sorted set, see Normalizer.NormalizeSetJSON):
//   - GET /session — a global, accumulating list whose element COUNT is
//     non-deterministic run-to-run (every entry normalizes to the same placeholder
//     object), so the set dedup removes the count noise.
//   - GET /command — opencode returns the command list in a non-deterministic
//     (map/glob) ORDER, while Forge sorts by name (a recorded known-addition,
//     masterplan decision #6). Set-normalizing makes the order irrelevant so the
//     two runs' identical command SET compares equal; a genuinely missing or extra
//     command still changes the set and fails.
func (c *Client) normalizeBody(method, path string, raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	norm := c.Norm.NormalizeJSON
	if method == http.MethodGet && orderInsensitiveListPath(path) {
		norm = c.Norm.NormalizeSetJSON
	}
	if out, err := norm(raw); err == nil {
		return string(out)
	}
	return strings.TrimSpace(string(raw))
}

// orderInsensitiveListPath reports whether path is a GET list endpoint whose
// top-level array order is non-deterministic and so must be set-normalized:
// /session (global accumulating list) or /command (opencode's non-deterministic
// order vs Forge's sort). The query string (e.g. ?directory=...) is ignored.
func orderInsensitiveListPath(path string) bool {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	return path == "/session" || path == "/command"
}
