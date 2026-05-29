// Package llm defines the provider-neutral LLM wire abstraction: the request /
// model-message shapes a provider client consumes, and (added in M2) the
// streaming Provider interface and Event taxonomy.
//
// The types here are deliberately decoupled from the stored message model
// (internal/engine/message): message.ToModelMessages converts persisted
// MessageV2 parts into the []ModelMessage a provider request carries, so the
// dependency runs message -> llm (never the reverse), keeping llm free of any
// storage concern.
package llm

// Role is a chat role on the wire. Tool results are their own "tool" message
// (the OpenAI/AI-SDK convention), not nested inside the assistant turn.
type Role string

// Wire chat roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentKind discriminates a ContentPart.
type ContentKind string

const (
	// ContentText is a plain-text span.
	ContentText ContentKind = "text"
	// ContentFile is inline media (image/PDF/…) carried by URL (often a data: URI).
	ContentFile ContentKind = "file"
	// ContentReasoning is a model reasoning/thinking span (round-tripped to the
	// same provider only).
	ContentReasoning ContentKind = "reasoning"
	// ContentToolCall is an assistant request to invoke a tool.
	ContentToolCall ContentKind = "tool-call"
	// ContentToolResult is the result of a tool invocation (lives on a RoleTool
	// message).
	ContentToolResult ContentKind = "tool-result"
)

// ContentPart is one element of a ModelMessage's content. It is a tagged union;
// only the fields relevant to Kind are populated. This mirrors the AI SDK's
// ModelMessage content-part union closely enough that provider request builders
// can render it to OpenAI or Anthropic wire JSON without a second intermediate.
type ContentPart struct {
	Kind ContentKind `json:"kind"`

	// ContentText / ContentReasoning
	Text string `json:"text,omitempty"`

	// ContentFile
	MediaType string `json:"mediaType,omitempty"`
	URL       string `json:"url,omitempty"`
	Filename  string `json:"filename,omitempty"`

	// ContentToolCall / ContentToolResult
	ToolCallID string         `json:"toolCallID,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	Input      map[string]any `json:"input,omitempty"`

	// ContentToolResult
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"isError,omitempty"`

	// Provider-specific passthrough (e.g. Anthropic reasoning signature). Only
	// replayed to the same provider that produced it.
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty"`
}

// ModelMessage is a single provider-neutral chat message: a role plus ordered
// content parts. ToolCallID on a RoleTool message ties its single tool-result
// part back to the assistant tool-call that produced it.
type ModelMessage struct {
	Role    Role          `json:"role"`
	Content []ContentPart `json:"content"`
}
