// Package anthropic is a hand-rolled Anthropic Messages API client implementing
// llm.Provider. It POSTs to {BaseURL}/v1/messages and parses the SSE stream
// directly into llm.Event — no vendored SDK — mirroring the OpenAI client. It is
// the deferred provider from the plan-02 addendum (OpenAI-first, Anthropic after
// M9) and handles extended-thinking blocks (reasoning + signature passthrough).
package anthropic

import (
	"net/http"

	"github.com/rotemmiz/forge/internal/engine/llm"
)

// Defaults for the Anthropic API.
const (
	defaultBaseURL    = "https://api.anthropic.com"
	defaultAPIVersion = "2023-06-01"
	defaultMaxTokens  = 4096
)

// Client is one configured Anthropic endpoint+model.
type Client struct {
	// BaseURL is the API root; defaults to https://api.anthropic.com.
	BaseURL string
	// APIKey is sent as the x-api-key header.
	APIKey string
	// Model is the default model id; a request's Model overrides it when set.
	Model string
	// Version is the anthropic-version header value.
	Version string
	// MaxTokens is the default max_tokens when a request does not set one.
	MaxTokens int
	// Headers are extra headers merged into every request (e.g. beta flags).
	Headers map[string]string
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Cap is the capability reported to the tool registry.
	Cap llm.Capability
}

var _ llm.Provider = (*Client)(nil)

// Options configures a Client.
type Options struct {
	BaseURL    string
	APIKey     string
	Model      string
	Version    string
	MaxTokens  int
	Headers    map[string]string
	HTTPClient *http.Client
	Capability llm.Capability
}

// New builds a Client with defaults filled in.
func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	capability := opts.Capability
	capability.Streaming = true
	c := &Client{
		BaseURL: orDefault(opts.BaseURL, defaultBaseURL),
		APIKey:  opts.APIKey, Model: opts.Model,
		Version:   orDefault(opts.Version, defaultAPIVersion),
		MaxTokens: opts.MaxTokens, Headers: opts.Headers, HTTPClient: hc, Cap: capability,
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = defaultMaxTokens
	}
	return c
}

// Capability returns the configured capability flags.
func (c *Client) Capability() llm.Capability { return c.Cap }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
