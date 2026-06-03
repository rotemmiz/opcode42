package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/id"
)

// Compaction tuning (compaction.ts:35-39). The summary preserves the last
// defaultTailTurns user turns; older history is summarized into a single
// summary:true assistant message anchored to the compaction user message.
const (
	defaultTailTurns   = 2
	toolOutputMaxChars = 2000
	// pruneMinimum/pruneProtect bound the post-loop tool-output GC
	// (compaction.ts:35-36): prune only when >pruneMinimum tokens are prunable,
	// always protecting the most recent pruneProtect tokens of tool output.
	pruneMinimum = 20_000
	pruneProtect = 40_000
)

// prune marks old completed tool outputs as compacted so future requests replace
// them with a placeholder, reclaiming context. It protects the most recent
// pruneProtect tokens of tool output and only runs when more than pruneMinimum
// tokens are prunable (compaction.ts:296-341). Best-effort; errors are ignored.
func (e *Engine) prune(ctx context.Context, sessionID string) {
	msgs, err := e.cfg.Store.List(ctx, sessionID) // oldest-first
	if err != nil {
		return
	}
	// Walk newest→oldest, collecting prunable tool parts. Protect the two most
	// recent user turns and stop at a summary boundary (compaction.ts:312-330);
	// older completed tool outputs become candidates.
	type entry struct {
		part   *message.ToolPart
		state  message.ToolStateCompleted
		tokens int
	}
	var cands []entry // newest→oldest order
	turns := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if a := m.Info.Assistant; a != nil && a.Summary {
			break // already-summarized history below this point
		}
		if m.Info.IsUser() {
			turns++
		}
		if turns < 2 {
			continue // protect the two most recent turns
		}
		for _, p := range m.Parts {
			tp, ok := p.(*message.ToolPart)
			if !ok || tp.Status() != message.ToolCompleted || tp.Tool == "skill" {
				continue
			}
			var st message.ToolStateCompleted
			if err := unmarshalState(tp.State, &st); err != nil || st.Time.Compacted != nil {
				continue
			}
			cands = append(cands, entry{part: tp, state: st, tokens: estimateTokens(st.Output)})
		}
	}
	// Of the candidates (newest→oldest), protect pruneProtect tokens; prune the rest.
	protected, cut := 0, len(cands)
	for i := range cands {
		if protected >= pruneProtect {
			cut = i
			break
		}
		protected += cands[i].tokens
	}
	prunable := 0
	for i := cut; i < len(cands); i++ {
		prunable += cands[i].tokens
	}
	if prunable <= pruneMinimum {
		return
	}
	now := time.Now().UnixMilli()
	for i := cut; i < len(cands); i++ {
		cands[i].state.Time.Compacted = &now
		cands[i].part.State = marshalState(cands[i].state)
		_ = e.cfg.Store.PutPart(ctx, cands[i].part)
	}
}

func estimateTokens(s string) int { return len(s) / 4 }

func unmarshalState(raw json.RawMessage, dst any) error { return json.Unmarshal(raw, dst) }

func marshalState(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// continuePrompt nudges the model to resume after an auto-compaction.
const continuePrompt = "Continue if you have next steps; otherwise summarize what was accomplished."

// summaryTemplate is opencode's SUMMARY_TEMPLATE (compaction.ts:42-77).
const summaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Goal
- [single-sentence task summary]

## Constraints & Preferences
- [user constraints, preferences, specs, or "(none)"]

## Progress
### Done
- [completed work or "(none)"]

### In Progress
- [current work or "(none)"]

### Blocked
- [blockers or "(none)"]

## Key Decisions
- [decision and why, or "(none)"]

## Next Steps
- [ordered next actions or "(none)"]

## Critical Context
- [important technical facts, errors, open questions, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, commands, error strings, and identifiers when known.
- Do not mention the summary process or that context was compacted.`

// createCompaction appends a compaction user message (the "task" the loop picks
// up on its next iteration) modeled on the most recent real user message
// (compaction.ts create). auto marks an automatic (overflow-driven) compaction
// vs an explicit user-requested summarize; overflow marks the overflow trigger.
func (e *Engine) createCompaction(ctx context.Context, sessionID string, model message.Model, agent string, auto, overflow bool) error {
	now := time.Now().UnixMilli()
	u := &message.UserMessage{ID: id.Ascending(id.Message), SessionID: sessionID, Role: message.RoleUser, Agent: agent, Model: model}
	u.Time.Created = now
	if err := e.cfg.Store.PutMessage(ctx, message.Info{User: u}); err != nil {
		return err
	}
	e.emitMessage(message.Info{User: u})
	part := &message.CompactionPart{
		PartBase: message.PartBase{ID: id.Ascending(id.Part), SessionID: sessionID, MessageID: u.ID},
		Type:     "compaction", Auto: auto, Overflow: overflow,
	}
	if err := e.cfg.Store.PutPart(ctx, part); err != nil {
		return err
	}
	e.emitPart(sessionID, part)
	return nil
}

// processCompaction summarizes the head of the conversation into a summary:true
// assistant message anchored to the compaction user message, records the tail
// start on the compaction part, emits session.compacted, and (for an auto
// compaction) appends a synthetic continue user message so the loop resumes
// (compaction.ts:344-590).
func (e *Engine) processCompaction(ctx context.Context, sessionID string, filtered []message.WithParts, compactionUser *message.UserMessage, part *message.CompactionPart) processor.Outcome {
	// History is everything before the compaction user message.
	var history []message.WithParts
	for _, m := range filtered {
		if m.Info.ID() == compactionUser.ID {
			break
		}
		history = append(history, m)
	}
	head, tailStartID := selectTail(history, defaultTailTurns)
	if len(head) == 0 {
		if len(history) == 0 {
			// Truly nothing to summarize: clear the task so it can't re-fire.
			_, _ = e.cfg.Store.DeletePart(ctx, part.ID)
			return processor.OutcomeContinue
		}
		// Too few turns to keep a tail: summarize the ENTIRE history (no tail) so
		// the summary still shrinks context (opencode: head=all, tail undefined).
		head, tailStartID = history, ""
	}

	providerID := compactionUser.Model.ProviderID
	modelID := compactionUser.Model.ModelID
	provider, err := e.cfg.Providers(ctx, providerID, modelID)
	if err != nil {
		return processor.OutcomeStop
	}

	summary := &message.AssistantMessage{
		ID: id.Ascending(id.Message), SessionID: sessionID, Role: message.RoleAssistant,
		ParentID: compactionUser.ID, ProviderID: providerID, ModelID: modelID,
		// mode "compaction" mirrors opencode's summary message (compaction.ts:416-417).
		Mode:  "compaction",
		Agent: "compaction", Summary: true, Path: message.Path{CWD: e.cfg.Directory, Root: e.root},
	}
	summary.Time.Created = time.Now().UnixMilli()
	if err := e.cfg.Store.PutMessage(ctx, message.Info{Assistant: summary}); err != nil {
		return processor.OutcomeStop
	}
	e.emitAssistant(summary)

	modelMsgs := message.ToModelMessages(head, message.SerializeModel{ProviderID: providerID, ModelID: modelID},
		message.SerializeOptions{StripMedia: true, ToolOutputMaxChars: toolOutputMaxChars})
	modelMsgs = append(modelMsgs, llm.ModelMessage{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: summaryTemplate}}})

	events, err := provider.Stream(ctx, &llm.Request{Model: modelID, Messages: modelMsgs, ToolChoice: llm.ToolChoiceNone})
	if err != nil {
		e.failAssistant(ctx, summary, err.Error())
		return processor.OutcomeStop
	}
	// No executor: the summary turn runs without tools.
	proc := processor.New(processor.Config{Store: e.cfg.Store, Bus: e.cfg.Bus, Catalog: e.cfg.Catalog, SessionID: sessionID}, summary)
	switch proc.Run(ctx, events) {
	case processor.OutcomeStop:
		if summary.Error != nil {
			return processor.OutcomeStop
		}
	case processor.OutcomeCompact:
		// The summary turn itself overflowed: the session can't be compacted
		// further (compaction.ts:459-467). Mark it and stop.
		now := time.Now().UnixMilli()
		summary.Error = &message.Error{Name: "ContextOverflowError",
			Data: map[string]any{"message": "Session too large to compact - context exceeds model limit"}}
		summary.Finish = "error"
		summary.Time.Completed = &now
		_ = e.cfg.Store.PutMessage(ctx, message.Info{Assistant: summary})
		e.emitAssistant(summary)
		return processor.OutcomeStop
	}

	// Record the tail anchor only when a tail was preserved (opencode parity).
	if tailStartID != "" {
		part.TailStartID = tailStartID
		if err := e.cfg.Store.PutPart(ctx, part); err != nil {
			return processor.OutcomeStop
		}
		e.emitPart(sessionID, part)
	}
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(bus.NewEvent("session.compacted", map[string]any{"sessionID": sessionID}))
	}

	// Auto compaction: resume the task with a synthetic continue user message.
	if part.Auto {
		cu := &message.UserMessage{ID: id.Ascending(id.Message), SessionID: sessionID, Role: message.RoleUser,
			Agent: compactionUser.Agent, Model: compactionUser.Model}
		cu.Time.Created = time.Now().UnixMilli()
		if err := e.cfg.Store.PutMessage(ctx, message.Info{User: cu}); err != nil {
			return processor.OutcomeStop
		}
		e.emitMessage(message.Info{User: cu})
		tp := &message.TextPart{PartBase: message.PartBase{ID: id.Ascending(id.Part), SessionID: sessionID, MessageID: cu.ID},
			Type: "text", Text: continuePrompt, Synthetic: true}
		if err := e.cfg.Store.PutPart(ctx, tp); err != nil {
			return processor.OutcomeStop
		}
		e.emitPart(sessionID, tp)
	}
	return processor.OutcomeContinue
}

// selectTail splits history into the head to summarize and the id of the first
// preserved tail turn. It keeps the last tailTurns user turns; if there are not
// enough turns to benefit, it returns an empty head (no compaction). Simplified
// from compaction.ts select (no per-turn token-budget splitting yet).
func selectTail(history []message.WithParts, tailTurns int) (head []message.WithParts, tailStartID string) {
	if tailTurns <= 0 {
		return nil, ""
	}
	var starts []int
	var ids []string
	for i, m := range history {
		if m.Info.User != nil && !hasCompactionPart(m.Parts) {
			starts = append(starts, i)
			ids = append(ids, m.Info.ID())
		}
	}
	if len(starts) <= tailTurns {
		return nil, ""
	}
	ti := len(starts) - tailTurns
	start := starts[ti]
	if start <= 0 {
		return nil, ""
	}
	return history[:start], ids[ti]
}

func hasCompactionPart(parts []message.Part) bool {
	for _, p := range parts {
		if _, ok := p.(*message.CompactionPart); ok {
			return true
		}
	}
	return false
}

// pendingCompaction returns the compaction part on the given user message if it
// has NOT yet been processed — i.e. no finished summary assistant is anchored to
// it. Keying off the summary (not tail_start_id) mirrors opencode's task
// detection and is correct even when the whole history is summarized with no
// preserved tail (tail_start_id stays empty).
func pendingCompaction(filtered []message.WithParts, userID string) *message.CompactionPart {
	var cp *message.CompactionPart
	for _, m := range filtered {
		if m.Info.ID() != userID {
			continue
		}
		for _, p := range m.Parts {
			if c, ok := p.(*message.CompactionPart); ok {
				cp = c
			}
		}
	}
	if cp == nil {
		return nil
	}
	for _, m := range filtered {
		if a := m.Info.Assistant; a != nil && a.Summary && a.Finish != "" && a.ParentID == userID {
			return nil
		}
	}
	return cp
}
