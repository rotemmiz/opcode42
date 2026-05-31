// Package websearch implements the websearch tool's provider, mirroring
// opencode (tool/websearch.ts + mcp-websearch.ts): it calls Exa's or Parallel's
// hosted MCP endpoint (JSON-RPC tools/call) and returns the result text. It is
// flag-gated by env — with no EXA_API_KEY/PARALLEL_API_KEY it returns a clear
// "not configured" error rather than failing the run.
package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Hosted MCP endpoints (mcp-websearch.ts:4-7).
const (
	exaBase     = "https://mcp.exa.ai/mcp"
	parallelURL = "https://search.parallel.ai/mcp"
)

// Client is an env-configured web-search provider. The base URLs are fields so
// tests can point them at a stub server.
type Client struct {
	HTTP        *http.Client
	ExaBase     string
	ParallelURL string
}

// New builds a Client with the default hosted endpoints.
func New() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 25 * time.Second},
		ExaBase:     exaBase,
		ParallelURL: parallelURL,
	}
}

// Search runs a query against the configured provider and returns the result
// text. Provider selection mirrors opencode: OPENCODE_WEBSEARCH_PROVIDER wins,
// else whichever API key is present (websearch.ts:35-42).
func (c *Client) Search(ctx context.Context, query string) (string, error) {
	provider, endpoint, headers, err := c.resolve()
	if err != nil {
		return "", err
	}
	var name string
	var args map[string]any
	if provider == "parallel" {
		name = "web_search"
		args = map[string]any{"objective": query, "search_queries": []string{query}}
	} else {
		name = "web_search_exa"
		args = map[string]any{"query": query, "type": "auto", "numResults": 8, "livecrawl": "fallback"}
	}
	text, err := c.callMCP(ctx, endpoint, headers, name, args)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "No search results found. Please try a different query.", nil
	}
	return text, nil
}

// resolve picks the provider + endpoint + headers from env, or errors when no
// provider is configured.
func (c *Client) resolve() (provider, endpoint string, headers map[string]string, err error) {
	exaKey := os.Getenv("EXA_API_KEY")
	parallelKey := os.Getenv("PARALLEL_API_KEY")
	switch os.Getenv("OPENCODE_WEBSEARCH_PROVIDER") {
	case "parallel":
		provider = "parallel"
	case "exa":
		provider = "exa"
	default:
		switch {
		case parallelKey != "":
			provider = "parallel"
		case exaKey != "":
			provider = "exa"
		default:
			return "", "", nil, fmt.Errorf("websearch: no provider configured (set EXA_API_KEY or PARALLEL_API_KEY)")
		}
	}
	if provider == "parallel" {
		if parallelKey == "" {
			return "", "", nil, fmt.Errorf("websearch: PARALLEL_API_KEY is required for the parallel provider")
		}
		return provider, c.ParallelURL, map[string]string{"Authorization": "Bearer " + parallelKey}, nil
	}
	if exaKey == "" {
		return "", "", nil, fmt.Errorf("websearch: EXA_API_KEY is required for the exa provider")
	}
	return provider, c.ExaBase + "?exaApiKey=" + url.QueryEscape(exaKey), nil, nil
}

// mcpResult is the JSON-RPC tools/call result envelope (mcp-websearch.ts:9-17).
type mcpResult struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

// callMCP POSTs a JSON-RPC tools/call and returns the first content text. The
// response is either a direct JSON object or an SSE stream of data: lines.
func (c *Client) callMCP(ctx context.Context, endpoint string, headers map[string]string, tool string, args map[string]any) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("websearch: provider returned HTTP %d", resp.StatusCode)
	}
	return parseResponse(string(body)), nil
}

// parseResponse extracts the result text from a direct JSON body or an SSE
// stream (mcp-websearch.ts:29-43).
func parseResponse(body string) string {
	if t := parsePayload(strings.TrimSpace(body)); t != "" {
		return t
	}
	for _, line := range strings.Split(body, "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			if t := parsePayload(strings.TrimSpace(data)); t != "" {
				return t
			}
		}
	}
	return ""
}

func parsePayload(s string) string {
	if !strings.HasPrefix(s, "{") {
		return ""
	}
	var r mcpResult
	if json.Unmarshal([]byte(s), &r) != nil {
		return ""
	}
	for _, item := range r.Result.Content {
		if item.Text != "" {
			return item.Text
		}
	}
	return ""
}
