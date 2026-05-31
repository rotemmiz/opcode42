// Package engine wires Forge's agent loop end-to-end: it turns a prompt into a
// persisted user message, runs the LLM stream + tool loop under the per-session
// run lock, and drives the processor to emit parts and SSE. It is the keystone
// that composes message, llm, catalog, processor, registry, permission and
// runstate (plan 02 M9).
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/question"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/runstate"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/id"
	"github.com/rotemmiz/forge/internal/mcp"
)

// ProviderFactory builds a streaming provider for a provider/model pair (e.g. an
// OpenAI-compatible client configured from the catalog + credentials).
type ProviderFactory func(ctx context.Context, providerID, modelID string) (llm.Provider, error)

// maxSteps caps loop iterations as a runaway guard (agent maxSteps is wired in
// plan 04; this is the hard ceiling).
const maxSteps = 100

// Config wires an Engine. The bus/permissions/directory are per-instance; the
// store/sessions/catalog/registry/providers are shared.
type Config struct {
	Store       *message.Store
	Catalog     catalog.Catalog
	Registry    *registry.Registry
	Providers   ProviderFactory
	Permissions *permission.Manager
	Questions   *question.Manager
	// Subagent runs nested agent tasks for the `task` tool. It is set only on
	// top-level prompts (nil inside a subagent) to bound recursion.
	Subagent tool.SubagentRunner
	// Skills loads named skills for the `skill` tool.
	Skills tool.SkillSource
	// MCP exposes this instance's MCP servers' tools to the loop (nil ⇒ none).
	MCP       *mcp.Manager
	Bus       *bus.Bus
	RunState  *runstate.RunState
	Directory string
	// Rulesets are the merged agent/config permission rules for tool gating.
	Rulesets []permission.Ruleset
	// SystemInstructions are the resolved AGENTS.md/CLAUDE.md/config rules blocks
	// appended to the system prompt (plan 04 instructions).
	SystemInstructions []string
	Flags              registry.Flags
}

// Engine runs prompts for one instance (working directory).
type Engine struct {
	cfg Config
}

// New builds an Engine, defaulting the run state if unset.
func New(cfg Config) *Engine {
	if cfg.RunState == nil {
		cfg.RunState = runstate.New()
	}
	return &Engine{cfg: cfg}
}

// PartInput is a draft part on a prompt (id/messageID are server-allocated).
type PartInput struct {
	Type string `json:"type"` // "text" | "file"
	Text string `json:"text,omitempty"`
	MIME string `json:"mime,omitempty"`
	URL  string `json:"url,omitempty"`
}

// PromptInput is a request to prompt a session.
type PromptInput struct {
	SessionID string
	Parts     []PartInput
	Agent     string
	Provider  string
	Model     string
	System    string
	Tools     map[string]bool
}

// Prompt creates the user message + parts, then runs the loop, returning the
// final assistant message with its parts (mirrors prompt.ts:1215-1233).
func (e *Engine) Prompt(ctx context.Context, in PromptInput) (message.WithParts, error) {
	if in.Provider == "" || in.Model == "" {
		return message.WithParts{}, fmt.Errorf("prompt: provider and model are required")
	}
	now := time.Now().UnixMilli()
	user := &message.UserMessage{
		ID: id.Ascending(id.Message), SessionID: in.SessionID, Role: message.RoleUser,
		Agent:  orDefault(in.Agent, "build"),
		Model:  message.Model{ProviderID: in.Provider, ModelID: in.Model},
		System: in.System, Tools: in.Tools,
	}
	user.Time.Created = now
	if err := e.cfg.Store.PutMessage(ctx, message.Info{User: user}); err != nil {
		return message.WithParts{}, err
	}
	e.emitMessage(message.Info{User: user})

	for _, pin := range ResolvePromptParts(in.Parts) {
		part := toPart(pin, user.SessionID, user.ID)
		if part == nil {
			continue
		}
		if err := e.cfg.Store.PutPart(ctx, part); err != nil {
			return message.WithParts{}, err
		}
		e.emitPart(user.SessionID, part)
	}

	return e.Loop(ctx, in.SessionID)
}

// Loop runs the agent loop under the session run lock (idempotent).
func (e *Engine) Loop(ctx context.Context, sessionID string) (message.WithParts, error) {
	return e.cfg.RunState.EnsureRunning(ctx, sessionID, func(runCtx context.Context) (message.WithParts, error) {
		return e.runLoop(runCtx, sessionID)
	})
}

// Cancel interrupts a session's active run.
func (e *Engine) Cancel(sessionID string) { e.cfg.RunState.Cancel(sessionID) }

// ResolvePromptParts normalizes draft parts. For Phase B this passes text/file
// parts through; @file/@symbol mention resolution is wired with LSP in plan 03.
func ResolvePromptParts(parts []PartInput) []PartInput { return parts }

func toPart(in PartInput, sessionID, messageID string) message.Part {
	base := message.PartBase{ID: id.Ascending(id.Part), SessionID: sessionID, MessageID: messageID}
	switch in.Type {
	case "file":
		return &message.FilePart{PartBase: base, Type: "file", MIME: in.MIME, URL: in.URL}
	default:
		if in.Text == "" {
			return nil
		}
		return &message.TextPart{PartBase: base, Type: "text", Text: in.Text}
	}
}

func (e *Engine) emitMessage(info message.Info) {
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(bus.NewEvent("message.updated", map[string]any{
			"sessionID": info.User.SessionID, "info": info,
		}))
	}
}

func (e *Engine) emitPart(sessionID string, part message.Part) {
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(bus.NewEvent("message.part.updated", map[string]any{
			"sessionID": sessionID, "part": part, "time": time.Now().UnixMilli(),
		}))
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
