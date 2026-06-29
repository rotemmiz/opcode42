// Command record captures real opencode traffic into an http-recorder cassette
// (task C2). It is a pure-Go alternative to opencode's TS recorder (the plan
// assumed bun; a Go recorder avoids that dependency and keeps the cassette
// format identical via conformance/cassette).
//
// It records the SSE event catalog — the instance /event stream (a BARE
// {id,type,properties}) vs the global /global/event stream (a WRAPPED
// {payload:{...}}) — which locks Finding #2 against committed real data.
//
//	go run ./conformance/cmd/record -url http://127.0.0.1:4096 -out conformance/cassettes/sse-catalog.json
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rotemmiz/opcode42/conformance/cassette"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:4096", "opencode base URL")
	out := flag.String("out", "conformance/cassettes/sse-catalog.json", "cassette output path")
	dir := flag.String("dir", "/tmp/opcode42-record", "x-opencode-directory")
	flag.Parse()

	rec := &recorder{base: strings.TrimRight(*url, "/"), dir: *dir, client: &http.Client{}}
	c := &cassette.Cassette{
		Version:  1,
		Metadata: map[string]any{"name": "sse-catalog", "recordedAt": time.Now().UTC().Format(time.RFC3339)},
	}

	if err := rec.http(c, http.MethodGet, "/global/health"); err != nil {
		fatal(err)
	}
	if err := rec.sse(c, "/event"); err != nil {
		fatal(err)
	}
	if err := rec.sse(c, "/global/event"); err != nil {
		fatal(err)
	}

	data, err := c.Encode()
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "recorded %d interactions -> %s\n", len(c.Interactions), *out)
}

type recorder struct {
	base   string
	dir    string
	client *http.Client
}

func (r *recorder) http(c *cassette.Cassette, method, path string) error {
	req, err := http.NewRequest(method, r.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-opencode-directory", r.dir)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	c.Interactions = append(c.Interactions, cassette.Interaction{
		Transport: cassette.TransportHTTP,
		Request: &cassette.RequestSnapshot{
			Method: method, URL: r.base + path,
			Headers: map[string]string{"x-opencode-directory": r.dir}, Body: "",
		},
		Response: &cassette.ResponseSnapshot{
			Status:  resp.StatusCode,
			Headers: map[string]string{"content-type": resp.Header.Get("Content-Type")},
			Body:    string(body),
		},
	})
	return nil
}

// sse captures the SSE stream until the first data event (the response body is
// the raw "data: {...}\n\n" text, per plan 12 §b which records SSE as HTTP).
func (r *recorder) sse(c *cassette.Cassette, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-opencode-directory", r.dir)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var b strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		b.WriteString(line)
		b.WriteString("\n")
		if strings.HasPrefix(strings.TrimRight(line, "\r"), "data:") {
			b.WriteString("\n") // terminate the SSE event
			break
		}
	}
	c.Interactions = append(c.Interactions, cassette.Interaction{
		Transport: cassette.TransportHTTP,
		Request: &cassette.RequestSnapshot{
			Method: http.MethodGet, URL: r.base + path,
			Headers: map[string]string{"x-opencode-directory": r.dir}, Body: "",
		},
		Response: &cassette.ResponseSnapshot{
			Status:  resp.StatusCode,
			Headers: map[string]string{"content-type": resp.Header.Get("Content-Type")},
			Body:    b.String(),
		},
	})
	return nil
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "record:", err)
	os.Exit(1)
}
