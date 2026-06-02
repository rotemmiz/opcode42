package tui

import "strings"

// Prompt-draft stash (plan 08b §6). A named-draft store next to the prompt
// history: park the current composer buffer and recall (pop) or delete it later.
// Despite opencode's name it is composer-draft stashing, not git stash. Backed
// by the same local KV as history (no endpoint).

const stashMax = 50

// stashDraft saves the current composer buffer (most-recent-first), clears the
// composer, and persists. A no-op on an empty buffer.
func (m Model) stashDraft() Model {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		m.status = "nothing to stash"
		return m
	}
	m.stash = append([]string{text}, m.stash...)
	if len(m.stash) > stashMax {
		m.stash = m.stash[:stashMax]
	}
	m.input.SetValue("")
	m = m.resizeComposer()
	m.status = "draft stashed"
	m.persist()
	return m
}

// popStash loads the stashed draft at i into the composer and removes it.
func (m Model) popStash(i int) Model {
	if i < 0 || i >= len(m.stash) {
		return m
	}
	draft := m.stash[i]
	m.stash = append(m.stash[:i:i], m.stash[i+1:]...)
	m.input.SetValue(draft)
	m.input.CursorEnd()
	m = m.resizeComposer()
	m.status = "draft restored"
	m.persist()
	return m
}

// deleteStash drops the stashed draft at i.
func (m Model) deleteStash(i int) Model {
	if i < 0 || i >= len(m.stash) {
		return m
	}
	m.stash = append(m.stash[:i:i], m.stash[i+1:]...)
	m.persist()
	return m
}
