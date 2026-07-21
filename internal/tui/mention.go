package tui

import (
	"context"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// @-mention file picker: a trailing "@token" in the composer opens a popup of
// matching file paths (GET /find/file), inserted as "@path " on accept — the
// wire syntax opencode turns into file parts (design app.jsx onInput @-branch).

// mentionQuery returns the active "@token" at the end of text: the token must
// start at the beginning or just after whitespace, and run to the end with no
// inner whitespace (a space ends the mention). The bool is false when there's no
// active mention.
func mentionQuery(text string) (string, bool) {
	i := strings.LastIndex(text, "@")
	if i < 0 {
		return "", false
	}
	if i > 0 { // must be token-initial
		switch text[i-1] {
		case ' ', '\t', '\n':
		default:
			return "", false
		}
	}
	tok := text[i+1:]
	if strings.ContainsAny(tok, " \t\n") {
		return "", false // the mention is finished
	}
	return tok, true
}

// filesFoundMsg carries a file-search result, tagged with the query that
// produced it so a stale response can be discarded.
type filesFoundMsg struct {
	query string
	files []string
}

// findFilesCmd fuzzy-searches files (GET /find/file?query=). An empty query is
// not searched (the daemon requires one) — the popup just shows nothing.
func findFilesCmd(ctx context.Context, c *opcode42client.Opcode42Client, query string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(query) == "" {
			return filesFoundMsg{query: query}
		}
		var files []string
		_ = c.GetJSON(ctx, "/find/file?query="+url.QueryEscape(query), &files)
		if len(files) > maxMentionRows {
			files = files[:maxMentionRows]
		}
		return filesFoundMsg{query: query, files: files}
	}
}

// acceptMention replaces the trailing "@token" with the selected "@path " so the
// daemon resolves it to a file part.
func (m Model) acceptMention() Model {
	v := m.input.Value()
	// Don't trust the open state alone — only edit when there's a live @token.
	if m.ac.sel >= len(m.ac.files) {
		m.ac = autocomplete{}
		return m
	}
	if _, ok := mentionQuery(v); !ok {
		m.ac = autocomplete{}
		return m
	}
	path := m.ac.files[m.ac.sel]
	if i := strings.LastIndex(v, "@"); i >= 0 {
		v = v[:i] + "@" + path + " "
	}
	m.input.SetValue(v)
	m.input.CursorEnd()
	m.ac = autocomplete{}
	return m.resizeComposer()
}
