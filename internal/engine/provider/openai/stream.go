package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// streamChunk is one OpenAI chat-completions SSE chunk.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			Reasoning        string          `json:"reasoning"`
			ToolCalls        []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageChunk `json:"usage"`
}

type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type usageChunk struct {
	PromptTokens        float64 `json:"prompt_tokens"`
	CompletionTokens    float64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens float64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens float64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// Stream opens a chat-completions stream and returns a channel of llm.Event.
func (c *Client) Stream(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.Headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	//nolint:bodyclose // resp.Body is closed by consume() (success) or below (error).
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	ch := make(chan llm.Event)
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		go func() {
			defer close(ch)
			emit(ctx, ch, llm.Event{
				Type:       llm.EventProviderError,
				StatusCode: resp.StatusCode,
				Message:    strings.TrimSpace(string(errBody)),
			})
		}()
		return ch, nil
	}

	go c.consume(ctx, resp.Body, ch)
	return ch, nil
}

func (c *Client) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
}

// emit sends ev unless ctx is cancelled; returns false if cancelled.
func emit(ctx context.Context, ch chan<- llm.Event, ev llm.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}

type toolAccum struct {
	id      string
	name    string
	args    strings.Builder
	started bool
}

// consume parses the SSE body into llm.Events, closing body and ch when done.
func (c *Client) consume(ctx context.Context, body io.ReadCloser, ch chan<- llm.Event) {
	defer close(ch)
	defer func() { _ = body.Close() }()

	if !emit(ctx, ch, llm.Event{Type: llm.EventStepStart}) {
		return
	}

	const textID, reasoningID = "txt_0", "rsn_0"
	var (
		textOpen      bool
		reasoningOpen bool
		toolOrder     []int
		tools         = map[int]*toolAccum{}
		finishReason  string
		usage         *llm.TokenUsage
	)

	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if data, ok := sseData(line); ok {
			if data == "[DONE]" {
				break
			}
			var chunk streamChunk
			if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr != nil {
				continue // tolerate keep-alive / non-JSON lines
			}
			if chunk.Usage != nil {
				usage = mapUsage(chunk.Usage)
			}
			for _, choice := range chunk.Choices {
				d := choice.Delta
				if d.Content != "" {
					if !textOpen {
						if !emit(ctx, ch, llm.Event{Type: llm.EventTextStart, ID: textID}) {
							return
						}
						textOpen = true
					}
					if !emit(ctx, ch, llm.Event{Type: llm.EventTextDelta, ID: textID, Text: d.Content}) {
						return
					}
				}
				if r := firstNonEmpty(d.ReasoningContent, d.Reasoning); r != "" {
					if !reasoningOpen {
						if !emit(ctx, ch, llm.Event{Type: llm.EventReasoningStart, ID: reasoningID}) {
							return
						}
						reasoningOpen = true
					}
					if !emit(ctx, ch, llm.Event{Type: llm.EventReasoningDelta, ID: reasoningID, Text: r}) {
						return
					}
				}
				for _, tc := range d.ToolCalls {
					if !c.handleToolDelta(ctx, ch, tc, tools, &toolOrder) {
						return
					}
				}
				if choice.FinishReason != "" {
					finishReason = mapFinishReason(choice.FinishReason)
				}
			}
		}
		if err != nil {
			break // EOF or read error: finalize whatever we have.
		}
	}

	if textOpen && !emit(ctx, ch, llm.Event{Type: llm.EventTextEnd, ID: textID}) {
		return
	}
	if reasoningOpen && !emit(ctx, ch, llm.Event{Type: llm.EventReasoningEnd, ID: reasoningID}) {
		return
	}
	for _, idx := range toolOrder {
		acc := tools[idx]
		if !emit(ctx, ch, llm.Event{Type: llm.EventToolInputEnd, ID: acc.id}) {
			return
		}
		input := parseArgs(acc.args.String())
		if !emit(ctx, ch, llm.Event{Type: llm.EventToolCall, ID: acc.id, Name: acc.name, Input: input}) {
			return
		}
	}
	if finishReason == "" {
		finishReason = "stop"
	}
	if !emit(ctx, ch, llm.Event{Type: llm.EventStepFinish, Reason: finishReason, Usage: usage}) {
		return
	}
	emit(ctx, ch, llm.Event{Type: llm.EventFinish})
}

// handleToolDelta accumulates a streamed tool-call fragment, emitting
// tool-input-start on first sight of an index and tool-input-delta per fragment.
func (c *Client) handleToolDelta(ctx context.Context, ch chan<- llm.Event, tc toolCallDelta, tools map[int]*toolAccum, order *[]int) bool {
	acc, ok := tools[tc.Index]
	if !ok {
		acc = &toolAccum{}
		tools[tc.Index] = acc
		*order = append(*order, tc.Index)
	}
	if tc.ID != "" {
		acc.id = tc.ID
	}
	if tc.Function.Name != "" {
		acc.name = tc.Function.Name
	}
	if !acc.started && acc.id != "" && acc.name != "" {
		acc.started = true
		if !emit(ctx, ch, llm.Event{Type: llm.EventToolInputStart, ID: acc.id, Name: acc.name}) {
			return false
		}
	}
	if tc.Function.Arguments != "" {
		acc.args.WriteString(tc.Function.Arguments)
		if !emit(ctx, ch, llm.Event{Type: llm.EventToolInputDelta, ID: acc.id, Delta: tc.Function.Arguments}) {
			return false
		}
	}
	return true
}

// sseData extracts the payload of a "data:" SSE line, or ok=false otherwise.
func sseData(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(line[len("data:"):]), true
}

// mapFinishReason maps OpenAI finish_reason to the canonical assistant.finish
// values opencode uses (stop, tool-calls, length, content-filter).
func mapFinishReason(r string) string {
	switch r {
	case "tool_calls":
		return "tool-calls"
	case "content_filter":
		return "content-filter"
	default:
		return r
	}
}

// mapUsage converts an OpenAI usage block to neutral token usage. OpenAI folds
// cached input into prompt_tokens and reasoning into completion_tokens, so they
// are split out to avoid double-counting in cost accounting.
func mapUsage(u *usageChunk) *llm.TokenUsage {
	var cached, reasoning float64
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		reasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	// Clamp every component to >= 0: a provider that double-reports must not
	// produce negative token counts (and thus negative cost). Mirrors opencode's
	// safe() in session.ts:379-381.
	return &llm.TokenUsage{
		Input:     nonNeg(u.PromptTokens - cached),
		Output:    nonNeg(u.CompletionTokens - reasoning),
		Reasoning: nonNeg(reasoning),
		CacheRead: nonNeg(cached),
	}
}

func nonNeg(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func parseArgs(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
