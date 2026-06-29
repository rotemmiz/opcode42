// Package openai is a hand-rolled OpenAI / OpenAI-compatible chat-completions
// client implementing llm.Provider. It POSTs to {BaseURL}/chat/completions and
// parses the SSE stream directly into llm.Event — no vendored SDK — so the
// single OpenAI-compatible code path reaches OpenAI, Groq, Cerebras, OpenRouter,
// Together, local Ollama, etc. by varying BaseURL + APIKey (plan 02 addendum:
// OpenAI-first, Anthropic deferred).
package openai

import (
	"net/http"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// Client is one configured OpenAI-compatible endpoint+model.
type Client struct {
	// BaseURL is the API root, e.g. "https://api.groq.com/openai/v1".
	BaseURL string
	// APIKey is sent as a Bearer token (omitted when empty, e.g. local Ollama).
	APIKey string
	// Model is the default model id; a request's Model overrides it when set.
	Model string
	// Headers are extra headers merged into every request.
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
	return &Client{
		BaseURL:    opts.BaseURL,
		APIKey:     opts.APIKey,
		Model:      opts.Model,
		Headers:    opts.Headers,
		HTTPClient: hc,
		Cap:        capability,
	}
}

// Capability returns the configured capability flags.
func (c *Client) Capability() llm.Capability { return c.Cap }
