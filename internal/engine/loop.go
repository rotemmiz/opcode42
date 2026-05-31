package engine

import (
	"context"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/id"
)

// runLoop is the master agent loop (prompt.ts:1244-1496 / plan 02 §runLoop): it
// reorders history, checks the exit condition, streams one assistant turn, and
// repeats until the model finishes without pending tool calls.
func (e *Engine) runLoop(ctx context.Context, sessionID string) (message.WithParts, error) {
	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			break
		}
		msgs, err := e.cfg.Store.Stream(ctx, sessionID)
		if err != nil {
			return e.finalAssistant(ctx, sessionID), err
		}
		filtered := message.FilterCompacted(msgs)
		latest := message.LatestOf(filtered)
		if latest.User == nil {
			break
		}

		// A pending compaction task is processed before any normal turn or the
		// exit check (prompt.ts:1310-1329).
		if cp := pendingCompaction(filtered, latest.User.ID); cp != nil {
			if e.processCompaction(ctx, sessionID, filtered, latest.User, cp) == processor.OutcomeStop {
				return e.finalAssistant(ctx, sessionID), nil
			}
			continue
		}

		if e.shouldExit(latest) {
			break
		}

		providerID := latest.User.Model.ProviderID
		modelID := latest.User.Model.ModelID
		provider, err := e.cfg.Providers(ctx, providerID, modelID)
		if err != nil {
			return e.finalAssistant(ctx, sessionID), err
		}

		assistant := e.newAssistant(sessionID, latest.User)
		if err := e.cfg.Store.PutMessage(ctx, message.Info{Assistant: assistant}); err != nil {
			return message.WithParts{}, err
		}
		e.emitAssistant(assistant)

		req := &llm.Request{
			Model:         modelID,
			SystemPrompts: e.buildSystem(modelID, latest.User.System),
			Messages:      message.ToModelMessages(filtered, message.SerializeModel{ProviderID: providerID, ModelID: modelID}, message.SerializeOptions{}),
			Tools:         e.cfg.Registry.Definitions(registry.FilterInput{ProviderID: providerID, ModelID: modelID, Flags: e.cfg.Flags}),
			ToolChoice:    llm.ToolChoiceAuto,
		}
		events, err := provider.Stream(ctx, req)
		if err != nil {
			e.failAssistant(ctx, assistant, err.Error())
			return e.withParts(ctx, assistant), nil
		}

		executor := &registry.Executor{
			Registry: e.cfg.Registry, Asker: e.cfg.Permissions,
			SessionID: sessionID, Directory: e.cfg.Directory, Rulesets: e.cfg.Rulesets,
		}
		// Assign only when present so a nil *question.Manager does not become a
		// non-nil tool.Asker interface (the tool guards on Questioner == nil).
		if e.cfg.Questions != nil {
			executor.Questioner = e.cfg.Questions
		}
		proc := processor.New(processor.Config{
			Store: e.cfg.Store, Bus: e.cfg.Bus, Catalog: e.cfg.Catalog,
			Executor: executor, Asker: e.cfg.Permissions, SessionID: sessionID,
		}, assistant)

		switch proc.Run(ctx, events) {
		case processor.OutcomeStop:
			return e.withParts(ctx, assistant), nil
		case processor.OutcomeCompact:
			// Context overflowed: enqueue an auto-compaction task; the next
			// iteration summarizes the head and resumes (compaction.ts).
			if err := e.createCompaction(ctx, sessionID, latest.User.Model, latest.User.Agent, true); err != nil {
				return e.withParts(ctx, assistant), err
			}
			continue
		default: // OutcomeContinue
			continue
		}
	}
	e.prune(ctx, sessionID)
	return e.finalAssistant(ctx, sessionID), nil
}

// shouldExit mirrors prompt.ts:1272-1291: a finished assistant whose finish is
// not "tool-calls" and which is newer than the last user message ends the loop.
// The processor resolves all tool calls before returning, so there are never
// dangling pending tool parts to check here.
func (e *Engine) shouldExit(latest message.Latest) bool {
	a := latest.Assistant
	if a == nil || a.Finish == "" || a.Finish == "tool-calls" {
		return false
	}
	return latest.User != nil && latest.User.ID < a.ID
}

func (e *Engine) newAssistant(sessionID string, user *message.UserMessage) *message.AssistantMessage {
	a := &message.AssistantMessage{
		ID: id.Ascending(id.Message), SessionID: sessionID, Role: message.RoleAssistant,
		ParentID: user.ID, ProviderID: user.Model.ProviderID, ModelID: user.Model.ModelID,
		Agent: user.Agent, Path: message.Path{CWD: e.cfg.Directory, Root: e.cfg.Directory},
	}
	a.Time.Created = time.Now().UnixMilli()
	return a
}

func (e *Engine) buildSystem(modelID, override string) []string {
	env := registry.EnvInfo{ModelID: modelID, WorkingDir: e.cfg.Directory}
	return registry.BuildSystem(modelID, env, override)
}

func (e *Engine) emitAssistant(a *message.AssistantMessage) {
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(bus.NewEvent("message.updated", map[string]any{
			"sessionID": a.SessionID, "info": message.Info{Assistant: a},
		}))
	}
}

func (e *Engine) failAssistant(ctx context.Context, a *message.AssistantMessage, msg string) {
	a.Error = &message.Error{Name: "APIError", Data: map[string]any{"message": msg, "isRetryable": false}}
	completed := time.Now().UnixMilli()
	a.Time.Completed = &completed
	_ = e.cfg.Store.PutMessage(ctx, message.Info{Assistant: a})
	e.emitAssistant(a)
}

// withParts loads a message with its parts (the loop's return value).
func (e *Engine) withParts(ctx context.Context, a *message.AssistantMessage) message.WithParts {
	wp, err := e.cfg.Store.GetMessage(ctx, a.SessionID, a.ID)
	if err != nil {
		return message.WithParts{Info: message.Info{Assistant: a}}
	}
	return wp
}

// finalAssistant returns the newest assistant message in the session.
func (e *Engine) finalAssistant(ctx context.Context, sessionID string) message.WithParts {
	msgs, err := e.cfg.Store.Stream(ctx, sessionID) // newest-first
	if err != nil {
		return message.WithParts{}
	}
	for _, m := range msgs {
		if m.Info.Assistant != nil {
			return m
		}
	}
	return message.WithParts{}
}
