// Package conformance is the dual-run harness: it drives a set of scenarios
// against a target daemon (opencode or forge) and writes a normalized result
// file that the diff tool (conformance/cmd/diff) compares against another run.
package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rotemmiz/forge/conformance/normalize"
	"github.com/rotemmiz/forge/conformance/result"
)

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
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Dir:     dir,
		Norm:    normalize.New(append([]string{dir}, paths...)...),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if c.Dir != "" {
		req.Header.Set("x-opencode-directory", c.Dir)
	}
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Do performs an HTTP request and returns a normalized step. The request body,
// if any, is JSON-encoded; the response body is normalized.
func (c *Client) Do(stepName, method, path string, body any) (result.Step, error) {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return result.Step{}, err
		}
		raw = b
	}
	req, err := c.newRequest(context.Background(), method, path, raw)
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

	return result.Step{
		Name:   stepName,
		Method: method,
		Path:   c.Norm.Path(path),
		Status: resp.StatusCode,
		Body:   c.normalizeBody(respBody),
	}, nil
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
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
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
// trimmed and verbatim.
func (c *Client) normalizeBody(raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	if norm, err := c.Norm.NormalizeJSON(raw); err == nil {
		return string(norm)
	}
	return strings.TrimSpace(string(raw))
}
