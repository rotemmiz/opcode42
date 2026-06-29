package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// streamEvent is one Anthropic SSE payload (dispatched on its "type" field).
type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *antUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *antUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type antUsage struct {
	InputTokens              float64 `json:"input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
}

type antBlockState struct {
	kind string // text | tool | reasoning
	id   string
	name string
	json strings.Builder
	sig  strings.Builder
}

// Stream opens a /v1/messages stream and returns a channel of llm.Event.
func (c *Client) Stream(ctx context.Context, req *llm.Request) (<-chan llm.Event, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", c.Version)
	if c.APIKey != "" {
		httpReq.Header.Set("x-api-key", c.APIKey)
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
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}

	ch := make(chan llm.Event)
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		go func() {
			defer close(ch)
			emit(ctx, ch, llm.Event{Type: llm.EventProviderError, StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(errBody))})
		}()
		return ch, nil
	}
	go c.consume(ctx, resp.Body, ch)
	return ch, nil
}

func (c *Client) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/v1/messages"
}

func emit(ctx context.Context, ch chan<- llm.Event, ev llm.Event) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}

// consume parses the Anthropic SSE body into llm.Events.
func (c *Client) consume(ctx context.Context, body io.ReadCloser, ch chan<- llm.Event) {
	defer close(ch)
	defer func() { _ = body.Close() }()

	if !emit(ctx, ch, llm.Event{Type: llm.EventStepStart}) {
		return
	}

	blocks := map[int]*antBlockState{}
	var usage llm.TokenUsage
	var stopReason string

	reader := bufio.NewReader(body)
	for {
		line, readErr := reader.ReadString('\n')
		if data, ok := sseData(line); ok && data != "" {
			var ev streamEvent
			if json.Unmarshal([]byte(data), &ev) != nil {
				continue
			}
			if !c.handleEvent(ctx, ch, ev, blocks, &usage, &stopReason) {
				return
			}
			if ev.Type == "message_stop" {
				break
			}
		}
		if readErr != nil {
			break
		}
	}

	if stopReason == "" {
		stopReason = "stop"
	}
	u := usage
	if !emit(ctx, ch, llm.Event{Type: llm.EventStepFinish, Reason: stopReason, Usage: &u}) {
		return
	}
	emit(ctx, ch, llm.Event{Type: llm.EventFinish})
}

// handleEvent maps a single Anthropic SSE event to zero or more llm.Events.
func (c *Client) handleEvent(ctx context.Context, ch chan<- llm.Event, ev streamEvent, blocks map[int]*antBlockState, usage *llm.TokenUsage, stopReason *string) bool {
	switch ev.Type {
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			applyUsage(usage, ev.Message.Usage)
		}
	case "content_block_start":
		return c.startBlock(ctx, ch, ev, blocks)
	case "content_block_delta":
		return c.deltaBlock(ctx, ch, ev, blocks)
	case "content_block_stop":
		return c.stopBlock(ctx, ch, ev, blocks)
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != "" {
			*stopReason = mapStopReason(ev.Delta.StopReason)
		}
		if ev.Usage != nil {
			applyUsage(usage, ev.Usage)
		}
	case "error":
		msg := ""
		if ev.Error != nil {
			msg = ev.Error.Message
		}
		emit(ctx, ch, llm.Event{Type: llm.EventProviderError, Message: msg})
		return false // stop consuming: an errored stream must not also report a clean step-finish
	}
	return true
}

func (c *Client) startBlock(ctx context.Context, ch chan<- llm.Event, ev streamEvent, blocks map[int]*antBlockState) bool {
	if ev.ContentBlock == nil {
		return true
	}
	b := &antBlockState{}
	blocks[ev.Index] = b
	switch ev.ContentBlock.Type {
	case "text":
		b.kind = "text"
		return emit(ctx, ch, llm.Event{Type: llm.EventTextStart, ID: textID(ev.Index)})
	case "thinking":
		b.kind = "reasoning"
		return emit(ctx, ch, llm.Event{Type: llm.EventReasoningStart, ID: reasoningID(ev.Index)})
	case "tool_use":
		b.kind, b.id, b.name = "tool", ev.ContentBlock.ID, ev.ContentBlock.Name
		return emit(ctx, ch, llm.Event{Type: llm.EventToolInputStart, ID: b.id, Name: b.name})
	}
	return true
}

func (c *Client) deltaBlock(ctx context.Context, ch chan<- llm.Event, ev streamEvent, blocks map[int]*antBlockState) bool {
	b := blocks[ev.Index]
	if b == nil || ev.Delta == nil {
		return true
	}
	switch ev.Delta.Type {
	case "text_delta":
		return emit(ctx, ch, llm.Event{Type: llm.EventTextDelta, ID: textID(ev.Index), Text: ev.Delta.Text})
	case "thinking_delta":
		return emit(ctx, ch, llm.Event{Type: llm.EventReasoningDelta, ID: reasoningID(ev.Index), Text: ev.Delta.Thinking})
	case "signature_delta":
		b.sig.WriteString(ev.Delta.Signature)
	case "input_json_delta":
		b.json.WriteString(ev.Delta.PartialJSON)
		return emit(ctx, ch, llm.Event{Type: llm.EventToolInputDelta, ID: b.id, Delta: ev.Delta.PartialJSON})
	}
	return true
}

func (c *Client) stopBlock(ctx context.Context, ch chan<- llm.Event, ev streamEvent, blocks map[int]*antBlockState) bool {
	b := blocks[ev.Index]
	if b == nil {
		return true
	}
	switch b.kind {
	case "text":
		return emit(ctx, ch, llm.Event{Type: llm.EventTextEnd, ID: textID(ev.Index)})
	case "reasoning":
		md := map[string]any{}
		if sig := b.sig.String(); sig != "" {
			md["anthropic"] = map[string]any{"signature": sig}
		}
		return emit(ctx, ch, llm.Event{Type: llm.EventReasoningEnd, ID: reasoningID(ev.Index), ProviderMetadata: md})
	case "tool":
		if !emit(ctx, ch, llm.Event{Type: llm.EventToolInputEnd, ID: b.id}) {
			return false
		}
		return emit(ctx, ch, llm.Event{Type: llm.EventToolCall, ID: b.id, Name: b.name, Input: parseArgs(b.json.String())})
	}
	return true
}

// applyUsage merges a usage block. The >0 guard is intentional and safe:
// input_tokens arrives only in message_start and output_tokens only in
// message_delta, so the two blocks are disjoint and never overwrite each other.
func applyUsage(u *llm.TokenUsage, a *antUsage) {
	if a.InputTokens > 0 {
		u.Input = a.InputTokens
	}
	if a.OutputTokens > 0 {
		u.Output = a.OutputTokens
	}
	if a.CacheReadInputTokens > 0 {
		u.CacheRead = a.CacheReadInputTokens
	}
	if a.CacheCreationInputTokens > 0 {
		u.CacheWrite = a.CacheCreationInputTokens
	}
}

// mapStopReason maps Anthropic stop_reason to the canonical assistant.finish.
func mapStopReason(r string) string {
	switch r {
	case "tool_use":
		return "tool-calls"
	case "max_tokens":
		return "length"
	case "refusal":
		return "content-filter"
	default: // end_turn, stop_sequence
		return "stop"
	}
}

func sseData(line string) (string, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(line[len("data:"):]), true
}

func parseArgs(s string) map[string]any {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil {
		return map[string]any{}
	}
	return m
}

func textID(i int) string      { return "txt_" + strconv.Itoa(i) }
func reasoningID(i int) string { return "rsn_" + strconv.Itoa(i) }
