package llm

// EventType is the discriminator for a streamed Event. The taxonomy mirrors
// @opencode-ai/llm's Event (plan 02 §Provider Abstraction) so the processor
// (M4) consumes one provider-neutral event stream regardless of the underlying
// provider's wire protocol.
type EventType string

// Event type discriminators, covering the full provider stream taxonomy.
const (
	EventTextStart      EventType = "text-start"
	EventTextDelta      EventType = "text-delta"
	EventTextEnd        EventType = "text-end"
	EventReasoningStart EventType = "reasoning-start"
	EventReasoningDelta EventType = "reasoning-delta"
	EventReasoningEnd   EventType = "reasoning-end"
	EventToolInputStart EventType = "tool-input-start"
	EventToolInputDelta EventType = "tool-input-delta"
	EventToolInputEnd   EventType = "tool-input-end"
	EventToolCall       EventType = "tool-call"
	EventToolResult     EventType = "tool-result"
	EventToolError      EventType = "tool-error"
	EventStepStart      EventType = "step-start"
	EventStepFinish     EventType = "step-finish"
	EventProviderError  EventType = "provider-error"
	EventFinish         EventType = "finish"
)

// TokenUsage is per-step usage reported by a provider on step-finish.
type TokenUsage struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Event is one event in a provider stream. Only the fields relevant to Type
// are populated.
type Event struct {
	Type EventType `json:"type"`

	// ID is the stream-scoped id for the text/reasoning span or tool call this
	// event belongs to.
	ID string `json:"id,omitempty"`

	// text-delta / reasoning-delta
	Text string `json:"text,omitempty"`

	// tool-input-start / tool-call
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`

	// tool-input-delta: raw argument fragment.
	Delta string `json:"delta,omitempty"`

	// step-finish
	Reason string      `json:"reason,omitempty"`
	Usage  *TokenUsage `json:"usage,omitempty"`

	// provider-error
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`

	// Provider-specific passthrough (e.g. Anthropic reasoning signature).
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}
