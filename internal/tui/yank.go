package tui

import "strings"

// Copy-to-clipboard helpers (plan 08a §C — "yank"). The composer is always
// focused, so vim j/k message navigation would collide with typing; instead the
// stream scrolls with ctrl+up/down + pgup/pgdn, and copy targets a turn via the
// timeline overlay (y) or the last response via the ctrl+x y leader chord.

// messageText concatenates a message's text + reasoning parts into plain text.
func (m Model) messageText(messageID string) string {
	var b strings.Builder
	for _, p := range m.store.parts[messageID] {
		switch p.Type {
		case "text", "reasoning":
			if t := strings.TrimRight(p.Text, "\n"); t != "" {
				if b.Len() > 0 {
					b.WriteString("\n\n")
				}
				b.WriteString(t)
			}
		}
	}
	return b.String()
}

// lastAssistantText returns the text of the most recent assistant message in the
// open session (the "copy last response" target), or "" if there is none.
func (m Model) lastAssistantText() string {
	msgs := m.store.messages[m.cfg.SessionID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			if t := m.messageText(msgs[i].ID); t != "" {
				return t
			}
		}
	}
	return ""
}

// formatTranscript builds a plain-text transcript of the open session
// (plan 08f H1a — /export + /copy). User/assistant turns are labeled; empty
// messages are skipped.
func (m Model) formatTranscript() string {
	sid := m.cfg.SessionID
	if sid == "" {
		return ""
	}
	var b strings.Builder
	if cur := m.currentSession(); cur != nil && cur.Title != "" {
		b.WriteString("# ")
		b.WriteString(cur.Title)
		b.WriteString("\n\n")
	}
	for _, msg := range m.store.messages[sid] {
		body := m.messageText(msg.ID)
		if body == "" {
			continue
		}
		role := msg.Role
		if role == "" {
			role = "message"
		}
		b.WriteString("## ")
		b.WriteString(role)
		b.WriteString("\n\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}
