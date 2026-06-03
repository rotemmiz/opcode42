// Package processor consumes a provider's llm.Event stream and turns it into
// persisted message parts plus SSE emissions, mirroring opencode's
// SessionProcessor (packages/opencode/src/session/processor.ts).
//
// Forge-specific: the OpenAI-compatible client emits only tool-call events (no
// tool-result), so the processor executes tool calls via an injected
// ToolExecutor and awaits them in cleanup — the AI-SDK maxSteps pattern (plan 02
// §Tool execution). When a provider DOES emit tool-result/tool-error events
// (a scripted mock, or future server-executed tools), those complete the call
// directly and it is not re-executed.
package processor

import (
	"context"
	"sync"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/id"
)

// Outcome is the processor's verdict for the run loop after a stream completes.
type Outcome int

const (
	// OutcomeContinue means the loop should evaluate its exit condition and maybe iterate.
	OutcomeContinue Outcome = iota
	// OutcomeStop is a hard stop (provider error or aborted).
	OutcomeStop
	// OutcomeCompact means context overflowed; the loop should compact before continuing.
	OutcomeCompact
)

// ToolCall identifies a tool invocation handed to the executor.
type ToolCall struct {
	CallID    string
	Name      string
	Input     map[string]any
	SessionID string
	MessageID string
}

// ToolResult is a successful tool execution result.
type ToolResult struct {
	Output      string
	Title       string
	Metadata    map[string]any
	Attachments []message.FilePart
}

// ToolExecutor runs a tool call. Implementations must respect ctx cancellation.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// PermissionAsker is the optional doom-loop hook (satisfied by the permission
// manager). A non-nil error aborts the offending tool call.
type PermissionAsker interface {
	AskPermission(ctx context.Context, sessionID, permission string, patterns []string, metadata map[string]any) error
}

// doomLoopThreshold is the count of identical consecutive tool calls that trips
// the doom-loop guard (processor.ts:425).
const doomLoopThreshold = 3

// Config wires a Processor's collaborators.
type Config struct {
	Store     *message.Store
	Bus       *bus.Bus
	Catalog   catalog.Catalog
	Executor  ToolExecutor    // nil for text-only runs
	Asker     PermissionAsker // nil disables the doom-loop ask
	SessionID string
	// StructuredTool names the synthetic tool whose successful call carries the
	// run's structured output (the StructuredOutput tool injected for a
	// json_schema response format). Empty disables capture (prompt.ts:1404,1458).
	StructuredTool string
}

// Processor turns one assistant turn's event stream into parts + SSE.
type Processor struct {
	cfg       Config
	assistant *message.AssistantMessage

	mu            sync.Mutex
	currentText   *message.TextPart
	reasoning     map[string]*message.ReasoningPart
	tools         map[string]*message.ToolPart // by callID
	toolOrder     []string
	pending       map[string]ToolCall // calls awaiting executor resolution
	wg            sync.WaitGroup
	needsCompact  bool
	shouldBreak   bool
	structured    any  // captured StructuredOutput input
	hasStructured bool // whether structured was captured this run
}

// New builds a Processor for the given (already-persisted) assistant message.
func New(cfg Config, assistant *message.AssistantMessage) *Processor {
	return &Processor{
		cfg:       cfg,
		assistant: assistant,
		reasoning: map[string]*message.ReasoningPart{},
		tools:     map[string]*message.ToolPart{},
		pending:   map[string]ToolCall{},
	}
}

// Run consumes events until the channel closes, then finalizes. It returns the
// loop outcome. ctx cancellation interrupts in-flight tool calls and leaves the
// assistant message marked aborted.
func (p *Processor) Run(ctx context.Context, events <-chan llm.Event) Outcome {
	for ev := range events {
		p.handle(ctx, ev)
	}
	// Execute any tool calls the provider requested but did not itself resolve.
	p.executePending(ctx)
	p.wg.Wait()
	p.cleanup(ctx)

	switch {
	case p.needsCompact:
		return OutcomeCompact
	case p.shouldBreak || p.assistant.Error != nil:
		return OutcomeStop
	default:
		return OutcomeContinue
	}
}

// Structured returns the captured StructuredOutput input and whether a
// StructuredOutput call completed successfully this run (prompt.ts:1458-1462).
func (p *Processor) Structured() (any, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.structured, p.hasStructured
}

func (p *Processor) handle(ctx context.Context, ev llm.Event) {
	switch ev.Type {
	case llm.EventStepStart:
		p.onStepStart(ctx)
	case llm.EventStepFinish:
		p.onStepFinish(ctx, ev)
	case llm.EventTextStart:
		p.onTextStart(ctx, ev)
	case llm.EventTextDelta:
		p.onTextDelta(ctx, ev)
	case llm.EventTextEnd:
		p.onTextEnd(ctx)
	case llm.EventReasoningStart:
		p.onReasoningStart(ctx, ev)
	case llm.EventReasoningDelta:
		p.onReasoningDelta(ctx, ev)
	case llm.EventReasoningEnd:
		p.onReasoningEnd(ctx, ev)
	case llm.EventToolInputStart:
		p.ensureTool(ctx, ev.ID, ev.Name)
	case llm.EventToolInputDelta:
		// AI SDK delivers parsed input on tool-call; raw fragments are unused.
	case llm.EventToolInputEnd:
		p.ensureTool(ctx, ev.ID, ev.Name)
	case llm.EventToolCall:
		p.onToolCall(ctx, ev)
	case llm.EventToolResult:
		p.onToolResult(ctx, ev)
	case llm.EventToolError:
		p.onToolError(ctx, ev)
	case llm.EventProviderError:
		p.onProviderError(ev)
	case llm.EventFinish:
		// no-op
	}
}

func (p *Processor) onStepStart(ctx context.Context) {
	p.updatePart(ctx, &message.StepStartPart{PartBase: p.partBase(), Type: "step-start"})
}

func (p *Processor) onStepFinish(ctx context.Context, ev llm.Event) {
	p.finishAllReasoning(ctx)
	tokens := tokensFromUsage(ev.Usage)
	cost := catalog.ModelCost(p.modelMeta(), tokens)

	p.mu.Lock()
	p.assistant.Finish = orStop(ev.Reason)
	p.assistant.Tokens = tokens
	p.assistant.Cost += cost
	p.mu.Unlock()

	p.updatePart(ctx, &message.StepFinishPart{
		PartBase: p.partBase(), Type: "step-finish",
		Reason: orStop(ev.Reason), Cost: cost, Tokens: tokens,
	})
	p.updateMessage(ctx)

	if isOverflow(tokens, p.modelMeta()) {
		p.mu.Lock()
		p.needsCompact = true
		p.mu.Unlock()
	}
}

func (p *Processor) onTextStart(ctx context.Context, ev llm.Event) {
	now := time.Now().UnixMilli()
	part := &message.TextPart{PartBase: p.partBase(), Type: "text", Time: &message.PartTime{Start: now}}
	if ev.ProviderMetadata != nil {
		part.Metadata = ev.ProviderMetadata
	}
	p.mu.Lock()
	p.currentText = part
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func (p *Processor) onTextDelta(_ context.Context, ev llm.Event) {
	p.mu.Lock()
	if p.currentText == nil {
		p.mu.Unlock()
		return
	}
	p.currentText.Text += ev.Text
	if ev.ProviderMetadata != nil {
		p.currentText.Metadata = ev.ProviderMetadata
	}
	part := p.currentText
	p.mu.Unlock()
	p.publishDelta(part.ID, part.MessageID, "text", ev.Text)
}

func (p *Processor) onTextEnd(ctx context.Context) {
	p.mu.Lock()
	part := p.currentText
	if part == nil {
		p.mu.Unlock()
		return
	}
	end := time.Now().UnixMilli()
	if part.Time == nil {
		part.Time = &message.PartTime{Start: end}
	}
	part.Time.End = &end
	p.currentText = nil
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func (p *Processor) onReasoningStart(ctx context.Context, ev llm.Event) {
	p.mu.Lock()
	if _, ok := p.reasoning[ev.ID]; ok {
		p.mu.Unlock()
		return
	}
	part := &message.ReasoningPart{PartBase: p.partBase(), Type: "reasoning",
		Time: message.PartTime{Start: time.Now().UnixMilli()}, Metadata: ev.ProviderMetadata}
	p.reasoning[ev.ID] = part
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func (p *Processor) onReasoningDelta(_ context.Context, ev llm.Event) {
	p.mu.Lock()
	part, ok := p.reasoning[ev.ID]
	if !ok {
		p.mu.Unlock()
		return
	}
	part.Text += ev.Text
	if ev.ProviderMetadata != nil {
		part.Metadata = ev.ProviderMetadata
	}
	p.mu.Unlock()
	p.publishDelta(part.ID, part.MessageID, "text", ev.Text)
}

func (p *Processor) onReasoningEnd(ctx context.Context, ev llm.Event) {
	p.mu.Lock()
	part, ok := p.reasoning[ev.ID]
	if ok && ev.ProviderMetadata != nil {
		part.Metadata = ev.ProviderMetadata
	}
	p.mu.Unlock()
	if ok {
		p.finishReasoning(ctx, ev.ID)
	}
}

func (p *Processor) finishReasoning(ctx context.Context, id string) {
	p.mu.Lock()
	part, ok := p.reasoning[id]
	if !ok {
		p.mu.Unlock()
		return
	}
	end := time.Now().UnixMilli()
	part.Time.End = &end
	delete(p.reasoning, id)
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func (p *Processor) finishAllReasoning(ctx context.Context) {
	p.mu.Lock()
	ids := make([]string, 0, len(p.reasoning))
	for k := range p.reasoning {
		ids = append(ids, k)
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.finishReasoning(ctx, id)
	}
}

func (p *Processor) onProviderError(ev llm.Event) {
	p.mu.Lock()
	p.assistant.Error = &message.Error{Name: "APIError", Data: map[string]any{
		"message": ev.Message, "isRetryable": false,
	}}
	p.shouldBreak = true
	p.mu.Unlock()
}

func orStop(reason string) string {
	if reason == "" {
		return "stop"
	}
	return reason
}

func (p *Processor) partBase() message.PartBase {
	return message.PartBase{ID: id.Ascending(id.Part), SessionID: p.cfg.SessionID, MessageID: p.assistant.ID}
}

func (p *Processor) modelMeta() catalog.Model {
	m, _ := p.cfg.Catalog.Lookup(p.assistant.ProviderID, p.assistant.ModelID)
	return m
}
