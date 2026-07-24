package tui

// paste.go — smart paste summary (plan 08f H3 / G.2).
//
// When a paste is ≥3 lines or >150 chars and paste_summary_enabled is on,
// Opcode42 stages the full text in pasteParts and shows a `[Pasted ~N lines]`
// chip above the composer (Bubble Tea has no extmarks). On submit the staged
// text is concatenated with any typed composer content and sent in full.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pastePart is one smart-pasted blob staged for the next submit.
type pastePart struct {
	Text  string
	Lines int
}

// maybeSmartPaste inserts text into the composer, collapsing large pastes to a
// summary chip when paste_summary_enabled is on (opencode pasteInputText).
func (m Model) maybeSmartPaste(raw string) (Model, tea.Cmd) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	trimmed := strings.TrimSpace(normalized)
	if m.pasteSummaryEnabled && trimmed != "" {
		lines := strings.Count(trimmed, "\n") + 1
		if lines >= 3 || len(trimmed) > 150 {
			m.histIdx = -1
			m.exiting = false
			m.deleting = false
			m.pasteParts = append(m.pasteParts, pastePart{Text: trimmed, Lines: lines})
			m = m.rerenderFull()
			return m, nil
		}
	}
	return m.insertComposerText(normalized)
}

// composeSubmitText joins staged pasteParts with the typed composer value.
// Cleared by the caller after use.
func (m Model) composeSubmitText() string {
	var b strings.Builder
	for i, p := range m.pasteParts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(p.Text)
	}
	typed := strings.TrimSpace(m.input.Value())
	if typed != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(typed)
	}
	return strings.TrimSpace(b.String())
}

// pasteSummaryLabel formats the chip text for one staged paste.
func pasteSummaryLabel(p pastePart) string {
	return fmt.Sprintf("[Pasted ~%d lines]", p.Lines)
}
