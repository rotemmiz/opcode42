package tui

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// @-mention file picker: a trailing "@token" in the composer opens a popup of
// matching file paths (GET /find/file), inserted as "@path " on accept — the
// wire syntax opencode turns into file parts (design app.jsx onInput @-branch).
// MCP resources (plan 08f H10) are merged into the same popup; accepting one
// stages a structured file part with source.type=resource (opencode
// autocomplete.tsx insertPart) and inserts "@name " in the composer.

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

// findFilesCmd fuzzy-searches files (GET /find/file?query=) and merges matching
// MCP resource names into the @-mention options (plan 08f H10 / G.10). An empty
// query skips the file search (daemon requires one) but still lists matching
// MCP resources (all of them when query is empty).
func findFilesCmd(ctx context.Context, c *opcode42client.Opcode42Client, query string, resources []mcpResource) tea.Cmd {
	return func() tea.Msg {
		var files []string
		if strings.TrimSpace(query) != "" {
			_ = c.GetJSON(ctx, "/find/file?query="+url.QueryEscape(query), &files)
		}
		files = mergeMentionOptions(files, filterMCPResourceNames(resources, query))
		if len(files) > maxMentionRows {
			files = files[:maxMentionRows]
		}
		return filesFoundMsg{query: query, files: files}
	}
}

// filterMCPResourceNames returns resource names that match query
// (case-insensitive substring; empty query matches all). Matches on name only
// — same field as opencode autocomplete.tsx (value: res.name). Matching is a
// Go TUI simplification of opencode's fuzzysort+frecency ranking, not full
// behavioral parity.
func filterMCPResourceNames(resources []mcpResource, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []string
	for _, r := range resources {
		if r.Name == "" {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(r.Name), q) {
			out = append(out, r.Name)
		}
	}
	return out
}

// mergeMentionOptions puts matching MCP resource names before file-search
// results so truncation (maxMentionRows) cannot starve resources when the file
// query is broad (opencode lists non-file options before files). Exact name
// collisions keep the resource row (first-wins); files and resources resolve
// differently, so prefer the MCP option when labels collide.
func mergeMentionOptions(files, resources []string) []string {
	seen := make(map[string]struct{}, len(files)+len(resources))
	out := make([]string, 0, len(files)+len(resources))
	for _, r := range resources {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	for _, f := range files {
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

// acceptMention replaces the trailing "@token" with the selected "@path " so the
// daemon resolves it to a file part. For MCP resource rows (plan 08f H10), the
// selected label is the resource name — insert "@name " and stage a structured
// file part with source.type=resource (opencode autocomplete.tsx:378-394), so
// the daemon expands it via mcp.readResource rather than the text mention
// scanner.
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
	if res := m.mcpResourceByName(path); res != nil {
		mime := res.MimeType
		if mime == "" {
			mime = "text/plain"
		}
		src, _ := json.Marshal(map[string]any{
			"type":       "resource",
			"clientName": res.Client,
			"uri":        res.URI,
			"text":       map[string]any{"start": 0, "end": 0, "value": ""},
		})
		m.pendingFiles = append(m.pendingFiles, pendingFile{
			Filename: res.Name,
			Mime:     mime,
			URL:      res.URI,
			Source:   src,
		})
		path = res.Name // display name in composer (opencode insertPart)
	}
	if i := strings.LastIndex(v, "@"); i >= 0 {
		v = v[:i] + "@" + path + " "
	}
	m.input.SetValue(v)
	m.input.CursorEnd()
	m.ac = autocomplete{}
	return m.resizeComposer()
}

// mcpResourceByName returns the MCP resource listed under name, or nil when
// name is a normal file-path option.
func (m Model) mcpResourceByName(name string) *mcpResource {
	for i := range m.mcpResources {
		if m.mcpResources[i].Name == name {
			return &m.mcpResources[i]
		}
	}
	return nil
}
