package openai

import (
	"encoding/json"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// chatRequest is the OpenAI chat-completions request body.
type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []chatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Tools         []chatTool     `json:"tools,omitempty"`
	ToolChoice    string         `json:"tool_choice,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type toolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Index    int        `json:"index,omitempty"`
	Function toolCallFn `json:"function"`
}

type toolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type contentPart struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL *imageURLField `json:"image_url,omitempty"`
}

type imageURLField struct {
	URL string `json:"url"`
}

// buildRequest renders an llm.Request into the OpenAI wire body.
func (c *Client) buildRequest(req *llm.Request) *chatRequest {
	model := req.Model
	if model == "" {
		model = c.Model
	}
	out := &chatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		MaxTokens:     req.MaxOutputTokens,
	}
	for _, sp := range req.SystemPrompts {
		out.Messages = append(out.Messages, chatMessage{Role: "system", Content: sp})
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, renderMessage(m)...)
	}
	if req.ToolChoice != "" {
		out.ToolChoice = string(req.ToolChoice)
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, chatTool{
			Type:     "function",
			Function: chatToolFunc{Name: t.Name, Description: t.Description, Parameters: t.InputSchema},
		})
	}
	return out
}

// renderMessage flattens one neutral ModelMessage into one or more OpenAI
// messages (a tool-role message expands to one OpenAI tool message per result).
func renderMessage(m llm.ModelMessage) []chatMessage {
	switch m.Role {
	case llm.RoleTool:
		var out []chatMessage
		for _, p := range m.Content {
			if p.Kind != llm.ContentToolResult {
				continue
			}
			out = append(out, chatMessage{Role: "tool", ToolCallID: p.ToolCallID, Content: p.Output})
		}
		return out
	case llm.RoleAssistant:
		msg := chatMessage{Role: "assistant"}
		var text string
		for _, p := range m.Content {
			switch p.Kind {
			case llm.ContentText:
				text += p.Text
			case llm.ContentToolCall:
				args, _ := json.Marshal(orEmptyObject(p.Input))
				msg.ToolCalls = append(msg.ToolCalls, toolCall{
					ID: p.ToolCallID, Type: "function",
					Function: toolCallFn{Name: p.ToolName, Arguments: string(args)},
				})
				// ContentReasoning is dropped: OpenAI input does not accept it.
			}
		}
		if text != "" {
			msg.Content = text
		}
		return []chatMessage{msg}
	default: // user / system
		return []chatMessage{{Role: string(m.Role), Content: renderUserContent(m.Content)}}
	}
}

// renderUserContent returns a plain string when the message is text-only, else a
// parts array carrying image_url entries for media.
func renderUserContent(parts []llm.ContentPart) any {
	hasFile := false
	for _, p := range parts {
		if p.Kind == llm.ContentFile {
			hasFile = true
			break
		}
	}
	if !hasFile {
		var text string
		for _, p := range parts {
			if p.Kind == llm.ContentText {
				text += p.Text
			}
		}
		return text
	}
	var out []contentPart
	for _, p := range parts {
		switch p.Kind {
		case llm.ContentText:
			out = append(out, contentPart{Type: "text", Text: p.Text})
		case llm.ContentFile:
			out = append(out, contentPart{Type: "image_url", ImageURL: &imageURLField{URL: p.URL}})
		}
	}
	return out
}

func orEmptyObject(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
