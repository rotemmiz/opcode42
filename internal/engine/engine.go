// Package engine wires Forge's agent loop end-to-end: it turns a prompt into a
// persisted user message, runs the LLM stream + tool loop under the per-session
// run lock, and drives the processor to emit parts and SSE. It is the keystone
// that composes message, llm, catalog, processor, registry, permission and
// runstate (plan 02 M9).
package engine

import (
	"context"
	"encoding/json"
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
	"github.com/rotemmiz/forge/internal/lsp"
	"github.com/rotemmiz/forge/internal/mcp"
	"github.com/rotemmiz/forge/internal/worktree"
)

// ProviderFactory builds a streaming provider for a provider/model pair (e.g. an
// OpenAI-compatible client configured from the catalog + credentials).
type ProviderFactory func(ctx context.Context, providerID, modelID string) (llm.Provider, error)

// PluginHooks is the engine's view of the flag-gated plugin host (plan 05). The
// concrete *pluginbridge.Bridge satisfies it; a nil PluginHooks (the default,
// plugin-host off) makes every call site a no-op. Trigger mutates out in place
// per opencode's hook contract; on any failure out is left untouched.
//
// Keeping this an interface lets the engine route the plan-05 hook call sites
// without importing the sidecar package, so the wiring seam stays additive.
type PluginHooks interface {
	Trigger(ctx context.Context, name string, input any, out any)
}

// defaultMaxSteps caps loop iterations when the resolved agent does not set its
// own maxSteps. opencode falls back to a finite ceiling for an unset agent.steps
// (prompt.ts:1339-1340); Forge uses 100.
const defaultMaxSteps = 100

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
	MCP *mcp.Manager
	// LSP is this instance's LSP service (nil ⇒ none). The `lsp` tool and
	// diagnostics wiring land in plan 03 M3-4/M3-5; the foundation just carries it.
	LSP       *lsp.Service
	Bus       *bus.Bus
	RunState  *runstate.RunState
	Directory string
	// Rulesets are the merged agent/config permission rules for tool gating.
	Rulesets []permission.Ruleset
	// SystemInstructions are the resolved AGENTS.md/CLAUDE.md/config rules blocks
	// appended to the system prompt (plan 04 instructions).
	SystemInstructions []string
	Flags              registry.Flags
	// Plugins routes the plan-05 hook call sites through the flag-gated plugin
	// host. nil ⇒ no plugin host (the default): every call site is a no-op.
	Plugins PluginHooks
	// MaxSteps is the resolved agent's step ceiling for this run. Zero falls back
	// to defaultMaxSteps. On the last allowed step the loop appends the MAX_STEPS
	// sentinel so the model answers with text only (prompt.ts:1339-1340,1451).
	MaxSteps int
	// Titles sets a session title once, used by step-0 title generation. nil
	// disables title generation (e.g. inside a subagent).
	Titles TitleSetter
	// TitleModel overrides the model used for title generation. When nil the
	// session's own provider/model is used (prompt.ts:264-270).
	TitleModel *message.Model
}

// TitleSetter persists a generated session title. SetTitle must be a no-op when
// the session no longer carries the default title (the loop calls it
// unconditionally; the implementation guards on isDefaultTitle).
type TitleSetter interface {
	SetTitle(ctx context.Context, sessionID, title string) error
	IsDefaultTitle(title string) bool
	Title(ctx context.Context, sessionID string) (string, error)
}

// Engine runs prompts for one instance (working directory).
type Engine struct {
	cfg Config
	// root is the VCS worktree enclosing cfg.Directory ("/" for a non-git dir),
	// stamped onto every assistant message's path.root to mirror opencode's
	// path: { cwd: ctx.directory, root: ctx.worktree } (prompt.ts:1354). Computed
	// once at construction rather than stat'ing the filesystem per turn.
	root string
}

// New builds an Engine, defaulting the run state if unset.
func New(cfg Config) *Engine {
	if cfg.RunState == nil {
		cfg.RunState = runstate.New()
	}
	return &Engine{cfg: cfg, root: worktree.Root(cfg.Directory)}
}

// PartInput is a draft part on a prompt (id/messageID are server-allocated).
// File parts may carry a Filename and a Source (file/symbol/resource provenance):
// these are populated both by the client (structured mentions) and by
// ResolvePromptParts when it expands `@file`/`@dir`/`@symbol` mentions.
type PartInput struct {
	Type     string          `json:"type"` // "text" | "file"
	Text     string          `json:"text,omitempty"`
	MIME     string          `json:"mime,omitempty"`
	URL      string          `json:"url,omitempty"`
	Filename string          `json:"filename,omitempty"`
	Source   json.RawMessage `json:"source,omitempty"`
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
	// Format requests a structured response. When Format.Type == "json_schema"
	// the loop injects the StructuredOutput tool and forces tool use
	// (prompt.ts:1403-1467).
	Format *message.OutputFormat
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
		System: in.System, Tools: in.Tools, Format: in.Format,
	}
	user.Time.Created = now
	if err := e.cfg.Store.PutMessage(ctx, message.Info{User: user}); err != nil {
		return message.WithParts{}, err
	}
	e.emitMessage(message.Info{User: user})

	var parts []message.Part
	for _, pin := range e.ResolvePromptParts(in.Parts) {
		part := toPart(pin, user.SessionID, user.ID)
		if part == nil {
			continue
		}
		if err := e.cfg.Store.PutPart(ctx, part); err != nil {
			return message.WithParts{}, err
		}
		e.emitPart(user.SessionID, part)
		parts = append(parts, part)
	}

	// chat.message fires after the user message and its parts are stored, exactly
	// as opencode (prompt.ts:1073). It is observe-only: the plugin sees the stored
	// message+parts but the loop does not consume the hook's output.
	e.triggerHook(ctx, hookChatMessage, map[string]any{
		"sessionID": user.SessionID,
		"agent":     user.Agent,
		"model":     map[string]any{"providerID": user.Model.ProviderID, "modelID": user.Model.ModelID},
		"messageID": user.ID,
	}, &chatMessageOutput{Message: user, Parts: parts})

	return e.Loop(ctx, in.SessionID)
}

// hookChatMessage mirrors opencode's chat.message hook key (plugin/src/index.ts:
// 234-242). It is observe-only: a plugin sees the stored message but cannot
// alter the loop's input.
const hookChatMessage = "chat.message"

// chatMessageOutput is the observe-only output of the chat.message hook
// (plugin/src/index.ts:241): the stored user message and its parts.
type chatMessageOutput struct {
	Message *message.UserMessage `json:"message"`
	Parts   []message.Part       `json:"parts"`
}

// Loop runs the agent loop under the session run lock (idempotent). The run-lock
// busy/idle transitions drive the session.status (+ deprecated session.idle) SSE
// events, mirroring opencode's run-state onBusy/onIdle (run-state.ts:58-63,
// status.ts:77-86).
func (e *Engine) Loop(ctx context.Context, sessionID string) (message.WithParts, error) {
	return e.cfg.RunState.EnsureRunning(ctx, sessionID, func(runCtx context.Context) (message.WithParts, error) {
		return e.runLoop(runCtx, sessionID)
	}, runstate.Hooks{
		OnBusy: func() { e.emitStatus(sessionID, "busy") },
		OnIdle: func() { e.emitStatus(sessionID, "idle") },
	})
}

// SummarizeInput requests an explicit (user-driven) compaction of a session.
type SummarizeInput struct {
	SessionID string
	Provider  string
	Model     string
	// Agent overrides the agent used for the summary turn; when empty the
	// session's last-user agent (or "build") is used (handlers/session.ts:271-272).
	Agent string
	// Auto marks the emitted CompactionPart as auto vs manual. opencode forwards
	// the request body's auto flag (handlers/session.ts:280: auto: ctx.payload.auto
	// ?? false) into compaction.create, surfacing as the required CompactionPart.auto
	// boolean (message-v2.ts:187).
	Auto bool
}

// Summarize runs an explicit AI compaction of the session, mirroring opencode's
// summarize handler (handlers/session.ts:264-283): it enqueues a non-auto
// compaction task and runs the loop, which produces the summary:true assistant
// message and emits session.compacted.
func (e *Engine) Summarize(ctx context.Context, in SummarizeInput) error {
	if in.Provider == "" || in.Model == "" {
		return fmt.Errorf("summarize: provider and model are required")
	}
	agent := in.Agent
	if agent == "" {
		agent = e.lastUserAgent(ctx, in.SessionID)
	}
	model := message.Model{ProviderID: in.Provider, ModelID: in.Model}
	if err := e.createCompaction(ctx, in.SessionID, model, agent, in.Auto, false); err != nil {
		return err
	}
	_, err := e.Loop(ctx, in.SessionID)
	return err
}

// lastUserAgent returns the agent of the session's most recent user message, or
// "build" when none is found (matches opencode's defaultAgent fallback).
func (e *Engine) lastUserAgent(ctx context.Context, sessionID string) string {
	msgs, err := e.cfg.Store.Stream(ctx, sessionID) // newest-first
	if err == nil {
		for _, m := range msgs {
			if m.Info.User != nil && m.Info.User.Agent != "" {
				return m.Info.User.Agent
			}
		}
	}
	return "build"
}

// emitStatus publishes session.status with the given status type and, when idle,
// the deprecated session.idle event opencode still emits (status.ts:79-82).
func (e *Engine) emitStatus(sessionID, statusType string) {
	if e.cfg.Bus == nil {
		return
	}
	e.cfg.Bus.Publish(bus.NewEvent("session.status", map[string]any{
		"sessionID": sessionID,
		"status":    map[string]any{"type": statusType},
	}))
	if statusType == "idle" {
		e.cfg.Bus.Publish(bus.NewEvent("session.idle", map[string]any{
			"sessionID": sessionID,
		}))
	}
}

// Cancel interrupts a session's active run.
func (e *Engine) Cancel(sessionID string) { e.cfg.RunState.Cancel(sessionID) }

func toPart(in PartInput, sessionID, messageID string) message.Part {
	base := message.PartBase{ID: id.Ascending(id.Part), SessionID: sessionID, MessageID: messageID}
	switch in.Type {
	case "file":
		return &message.FilePart{
			PartBase: base, Type: "file", MIME: in.MIME, URL: in.URL,
			Filename: in.Filename, Source: in.Source,
		}
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

// triggerHook routes one plan-05 plugin hook through the (flag-gated) plugin
// host. With no host configured it is a no-op and out is unchanged. The hook
// name strings mirror opencode's Hooks interface
// (opencode/packages/plugin/src/index.ts:222-334).
func (e *Engine) triggerHook(ctx context.Context, name string, input any, out any) {
	if e.cfg.Plugins == nil {
		return
	}
	e.cfg.Plugins.Trigger(ctx, name, input, out)
}

// transformMessages routes the message list through the
// experimental.chat.messages.transform hook just before serialization
// (prompt.ts:1433, compaction.ts:405). With no plugin host it returns msgs
// unchanged. A plugin may rewrite the list; on any failure (decode error, host
// down) the original list is preserved. The input is empty per opencode.
func (e *Engine) transformMessages(ctx context.Context, msgs []message.WithParts) []message.WithParts {
	if e.cfg.Plugins == nil {
		return msgs
	}
	out := message.TransformList{Messages: msgs}
	e.cfg.Plugins.Trigger(ctx, hookMessagesTransform, map[string]any{}, &out)
	if out.Messages == nil {
		return msgs
	}
	return out.Messages
}
