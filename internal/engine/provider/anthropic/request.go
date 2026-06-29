package anthropic

import (
	"strings"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// messagesRequest is the Anthropic /v1/messages request body.
type messagesRequest struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens"`
	Messages    []antMessage   `json:"messages"`
	System      string         `json:"system,omitempty"`
	Tools       []antTool      `json:"tools,omitempty"`
	ToolChoice  map[string]any `json:"tool_choice,omitempty"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	TopP        *float64       `json:"top_p,omitempty"`
}

type antMessage struct {
	Role    string     `json:"role"`
	Content []antBlock `json:"content"`
}

// antBlock is a content block in either direction (only the fields relevant to
// Type are populated).
type antBlock struct {
	Type string `json:"type"`
	// text / thinking
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// tool_use
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// image
	Source *antSource `json:"source,omitempty"`
}

type antSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type antTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// buildRequest renders an llm.Request into the Anthropic wire body.
func (c *Client) buildRequest(req *llm.Request) *messagesRequest {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = c.MaxTokens
	}
	out := &messagesRequest{
		Model: model, MaxTokens: maxTokens, Stream: true,
		System:      strings.Join(req.SystemPrompts, "\n\n"),
		Temperature: req.Temperature, TopP: req.TopP,
	}
	for _, m := range req.Messages {
		out.appendMessage(renderMessage(m))
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, antTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	if tc := toolChoice(req.ToolChoice); tc != nil {
		out.ToolChoice = tc
	}
	return out
}

// appendMessage merges consecutive same-role messages (Anthropic requires
// alternating roles; a user text followed by tool_results both map to "user").
func (r *messagesRequest) appendMessage(m antMessage) {
	if len(m.Content) == 0 {
		return
	}
	if n := len(r.Messages); n > 0 && r.Messages[n-1].Role == m.Role {
		r.Messages[n-1].Content = append(r.Messages[n-1].Content, m.Content...)
		return
	}
	r.Messages = append(r.Messages, m)
}

// renderMessage maps one neutral ModelMessage to an Anthropic message.
func renderMessage(m llm.ModelMessage) antMessage {
	switch m.Role {
	case llm.RoleTool:
		out := antMessage{Role: "user"}
		for _, p := range m.Content {
			if p.Kind != llm.ContentToolResult {
				continue
			}
			out.Content = append(out.Content, antBlock{Type: "tool_result", ToolUseID: p.ToolCallID, Content: p.Output, IsError: p.IsError})
		}
		return out
	case llm.RoleAssistant:
		out := antMessage{Role: "assistant"}
		for _, p := range m.Content {
			switch p.Kind {
			case llm.ContentText:
				if p.Text != "" {
					out.Content = append(out.Content, antBlock{Type: "text", Text: p.Text})
				}
			case llm.ContentReasoning:
				out.Content = append(out.Content, antBlock{Type: "thinking", Thinking: p.Text, Signature: signatureOf(p.ProviderMetadata)})
			case llm.ContentToolCall:
				out.Content = append(out.Content, antBlock{Type: "tool_use", ID: p.ToolCallID, Name: p.ToolName, Input: orEmpty(p.Input)})
			}
		}
		return out
	default: // user / system text
		out := antMessage{Role: "user"}
		for _, p := range m.Content {
			switch p.Kind {
			case llm.ContentText:
				out.Content = append(out.Content, antBlock{Type: "text", Text: p.Text})
			case llm.ContentFile:
				out.Content = append(out.Content, imageBlock(p))
			}
		}
		return out
	}
}

// imageBlock renders a media file part as an Anthropic image block (base64 data
// URI → base64 source; otherwise a url source). Anthropic requires media_type on
// a base64 source, so it falls back to the MIME embedded in the data URI.
func imageBlock(p llm.ContentPart) antBlock {
	if strings.HasPrefix(p.URL, "data:") {
		if i := strings.Index(p.URL, ","); i >= 0 {
			mediaType := p.MediaType
			if mediaType == "" {
				mediaType = mimeFromDataURI(p.URL)
			}
			return antBlock{Type: "image", Source: &antSource{Type: "base64", MediaType: mediaType, Data: p.URL[i+1:]}}
		}
	}
	return antBlock{Type: "image", Source: &antSource{Type: "url", URL: p.URL}}
}

// mimeFromDataURI extracts the MIME type from a "data:<mime>[;base64],..." URI.
func mimeFromDataURI(uri string) string {
	rest := strings.TrimPrefix(uri, "data:")
	if i := strings.IndexAny(rest, ";,"); i >= 0 {
		return rest[:i]
	}
	return ""
}

func toolChoice(tc llm.ToolChoice) map[string]any {
	switch tc {
	case llm.ToolChoiceAuto:
		return map[string]any{"type": "auto"}
	case llm.ToolChoiceRequired:
		return map[string]any{"type": "any"}
	default:
		return nil // none / unset: omit (tools may simply not be sent)
	}
}

func signatureOf(metadata map[string]any) string {
	anth, ok := metadata["anthropic"].(map[string]any)
	if !ok {
		return ""
	}
	if s, ok := anth["signature"].(string); ok {
		return s
	}
	return ""
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
