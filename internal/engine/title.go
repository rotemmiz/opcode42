package engine

import (
	"context"
	"regexp"
	"strings"

	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
)

// thinkTagRe strips a leading <think>...</think> block some models emit before
// the title (prompt.ts:288).
var thinkTagRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// maybeGenerateTitle forks a goroutine that streams the built-in title agent
// over the first user turn and sets the session title, but only while the title
// is still the default (prompt.ts:241-300,1295). It is a no-op when no title
// setter is configured (e.g. inside a subagent).
func (e *Engine) maybeGenerateTitle(ctx context.Context, sessionID string, history []message.WithParts) {
	if e.cfg.Titles == nil {
		return
	}
	// Only the very first user turn warrants a title; later turns leave it alone.
	if firstRealUser(history) != 0 {
		return
	}
	title, err := e.cfg.Titles.Title(ctx, sessionID)
	if err != nil || !e.cfg.Titles.IsDefaultTitle(title) {
		return
	}

	// Snapshot the inputs the goroutine needs: the run context may end before the
	// (fire-and-forget) title stream completes, so detach from cancellation.
	snapshot := append([]message.WithParts(nil), history...)
	go e.generateTitle(context.WithoutCancel(ctx), sessionID, snapshot)
}

// generateTitle streams the title agent and persists the result if the session
// still carries its default title.
func (e *Engine) generateTitle(ctx context.Context, sessionID string, history []message.WithParts) {
	providerID, modelID := e.titleModel(history)
	if providerID == "" || modelID == "" {
		return
	}
	provider, err := e.cfg.Providers(ctx, providerID, modelID)
	if err != nil {
		return
	}

	turn := titleContext(history)
	msgs := message.ToModelMessages(turn, message.SerializeModel{ProviderID: providerID, ModelID: modelID}, message.SerializeOptions{})
	msgs = append([]llm.ModelMessage{{
		Role:    llm.RoleUser,
		Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "Generate a title for this conversation:\n"}},
	}}, msgs...)

	events, err := provider.Stream(ctx, &llm.Request{
		Model:         modelID,
		SystemPrompts: []string{titlePrompt},
		Messages:      msgs,
		ToolChoice:    llm.ToolChoiceNone,
	})
	if err != nil {
		return
	}

	var b strings.Builder
	for ev := range events {
		if ev.Type == llm.EventTextDelta {
			b.WriteString(ev.Text)
		}
	}

	title := cleanTitle(b.String())
	if title == "" {
		return
	}
	// Re-check the default title under the lock-free setter: SetTitle is a no-op
	// if the user already renamed the session meanwhile.
	_ = e.cfg.Titles.SetTitle(ctx, sessionID, title)
}

// titleModel resolves the provider/model for title generation: the configured
// TitleModel, else the first user turn's own model (prompt.ts:264-270).
func (e *Engine) titleModel(history []message.WithParts) (string, string) {
	if e.cfg.TitleModel != nil {
		return e.cfg.TitleModel.ProviderID, e.cfg.TitleModel.ModelID
	}
	for _, m := range history {
		if m.Info.User != nil {
			return m.Info.User.Model.ProviderID, m.Info.User.Model.ModelID
		}
	}
	return "", ""
}

// titleContext returns the history slice up to and including the first real user
// turn (prompt.ts:262).
func titleContext(history []message.WithParts) []message.WithParts {
	idx := firstRealUser(history)
	if idx < 0 {
		return nil
	}
	return history[:idx+1]
}

// firstRealUser returns the index of the first user message that has at least one
// non-synthetic part, or -1 (prompt.ts:250-254).
func firstRealUser(history []message.WithParts) int {
	for i, m := range history {
		if m.Info.User == nil {
			continue
		}
		if hasRealPart(m.Parts) {
			return i
		}
	}
	return -1
}

func hasRealPart(parts []message.Part) bool {
	for _, p := range parts {
		if tp, ok := p.(*message.TextPart); ok {
			if !tp.Synthetic && strings.TrimSpace(tp.Text) != "" {
				return true
			}
		}
	}
	return false
}

// cleanTitle strips think tags, takes the first non-empty line, and clamps to
// 100 chars with an ellipsis (prompt.ts:288-295).
func cleanTitle(text string) string {
	text = thinkTagRe.ReplaceAllString(text, "")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > 100 {
			return string(r[:97]) + "..."
		}
		return line
	}
	return ""
}
