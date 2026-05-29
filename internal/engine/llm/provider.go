package llm

import "context"

// ToolChoice constrains tool selection on a request.
type ToolChoice string

// Tool-choice modes a request may set.
const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceRequired ToolChoice = "required"
	ToolChoiceNone     ToolChoice = "none"
)

// ToolDefinition is a tool advertised to the model: a name, description, and
// JSON Schema for its input.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Request is a single LLM completion request (plan 02 §Provider Abstraction).
type Request struct {
	Model           string            `json:"model"`
	SystemPrompts   []string          `json:"systemPrompts,omitempty"`
	Messages        []ModelMessage    `json:"messages"`
	Tools           []ToolDefinition  `json:"tools,omitempty"`
	ToolChoice      ToolChoice        `json:"toolChoice,omitempty"`
	MaxOutputTokens int               `json:"maxOutputTokens,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	TopP            *float64          `json:"topP,omitempty"`
	TopK            *int              `json:"topK,omitempty"`
	ProviderOptions map[string]any    `json:"providerOptions,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

// Capability reports model-level flags the tool registry uses for filtering.
type Capability struct {
	ToolCalls bool
	Streaming bool
	Reasoning bool
	Vision    bool
	PDFInput  bool
}

// Provider is the Go equivalent of the AI SDK's LanguageModelV2: it opens a
// single streaming completion and reports model capability. Implementations
// drain the HTTP response fully and close the channel; cancelling ctx aborts.
type Provider interface {
	// Stream opens a completion and returns a channel of Event. The channel
	// is closed when the stream ends (or ctx is cancelled).
	Stream(ctx context.Context, req *Request) (<-chan Event, error)
	// Capability returns this provider/model's capability flags.
	Capability() Capability
}
