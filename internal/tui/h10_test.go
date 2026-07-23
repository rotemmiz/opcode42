package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Tests for plan 08f H10 (G.10 — MCP resources in the @-mention popup).

func TestH10_FilterMCPResourceNames(t *testing.T) {
	res := []mcpResource{
		{Name: "docs", URI: "mcp://a/docs"},
		{Name: "Notes", URI: "mcp://a/notes"},
		{Name: "other", URI: "mcp://b/other"},
	}
	got := filterMCPResourceNames(res, "no")
	if len(got) != 1 || got[0] != "Notes" {
		t.Fatalf("filterMCPResourceNames(no) = %v, want [Notes]", got)
	}
	all := filterMCPResourceNames(res, "")
	if len(all) != 3 {
		t.Fatalf("empty query should match all, got %v", all)
	}
}

func TestH10_MergeMentionOptions(t *testing.T) {
	// Resources first; colliding "docs" keeps the resource row (file dropped).
	got := mergeMentionOptions([]string{"src/a.go", "docs"}, []string{"docs", "notes"})
	if len(got) != 3 || got[0] != "docs" || got[1] != "notes" || got[2] != "src/a.go" {
		t.Fatalf("merge = %v, want [docs notes src/a.go]", got)
	}
}

func TestH10_AcceptMention_StagesResourcePart(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.mcpResources = []mcpResource{{Name: "docs", URI: "mcp://server/docs", Client: "server", MimeType: "text/markdown"}}
	m.input.SetValue("see @do")
	m.ac = autocomplete{open: true, mode: acMention, files: []string{"docs"}, sel: 0}
	m = m.acceptMention()
	if got := m.input.Value(); got != "see @docs " {
		t.Fatalf("composer = %q, want @name inserted", got)
	}
	if len(m.pendingFiles) != 1 {
		t.Fatalf("pendingFiles = %d, want 1 staged resource part", len(m.pendingFiles))
	}
	f := m.pendingFiles[0]
	if f.URL != "mcp://server/docs" || f.Filename != "docs" || f.Mime != "text/markdown" {
		t.Fatalf("pending file = %+v", f)
	}
	var src struct {
		Type       string `json:"type"`
		ClientName string `json:"clientName"`
		URI        string `json:"uri"`
	}
	if err := json.Unmarshal(f.Source, &src); err != nil {
		t.Fatal(err)
	}
	if src.Type != "resource" || src.ClientName != "server" || src.URI != "mcp://server/docs" {
		t.Fatalf("source = %+v", src)
	}
}

func TestH10_AcceptMention_KeepsFilePath(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.mcpResources = []mcpResource{{Name: "docs", URI: "mcp://server/docs", Client: "server"}}
	m.input.SetValue("@src/main.go")
	m.ac = autocomplete{open: true, mode: acMention, files: []string{"src/main.go"}, sel: 0}
	m = m.acceptMention()
	if got := m.input.Value(); got != "@src/main.go " {
		t.Fatalf("composer = %q, want file path kept", got)
	}
	if len(m.pendingFiles) != 0 {
		t.Fatalf("file accept should not stage pendingFiles, got %d", len(m.pendingFiles))
	}
}

func TestH10_FindFilesCmd_MergesResources(t *testing.T) {
	res := []mcpResource{{Name: "docs", URI: "mcp://x/docs"}}
	// Empty query: no file search, but resources still listed.
	msg := findFilesCmd(context.TODO(), nil, "", res)()
	got, ok := msg.(filesFoundMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	if len(got.files) != 1 || got.files[0] != "docs" {
		t.Fatalf("files = %v, want [docs]", got.files)
	}
}

func TestH10_ResourcesLoadedMsg(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, mcpResourcesLoadedMsg{
		items: []mcpResource{{Name: "docs", URI: "mcp://x/docs", Client: "c"}},
	})
	if len(m.mcpResources) != 1 || m.mcpResources[0].Name != "docs" {
		t.Fatalf("mcpResources = %+v", m.mcpResources)
	}
	// Error leaves prior list intact.
	m, _ = step(t, m, mcpResourcesLoadedMsg{err: errString("boom")})
	if len(m.mcpResources) != 1 {
		t.Fatalf("error should keep prior resources, got %+v", m.mcpResources)
	}
}

func TestH10_Bootstrap_LoadsMCPResources(t *testing.T) {
	m := New(Config{URL: "http://x"})
	_, cmd := step(t, m, connectedMsg{})
	if cmd == nil {
		t.Fatal("connectedMsg should return a bootstrap batch")
	}
	found := false
	for _, msg := range collectMsgs(t, cmd) {
		if _, ok := msg.(mcpResourcesLoadedMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("bootstrap batch should include loadMCPResourcesCmd")
	}
}

func TestH10_RefreshAutocomplete_DispatchesMergedSearch(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.mcpResources = []mcpResource{{Name: "docs", URI: "mcp://x/docs"}}
	// Bare "@" (empty query): skips file GET, still lists MCP resources.
	m.input.SetValue("@")
	_, cmd := m.refreshAutocomplete()
	if cmd == nil {
		t.Fatal("refreshAutocomplete should dispatch findFilesCmd")
	}
	msg := cmd()
	got, ok := msg.(filesFoundMsg)
	if !ok {
		t.Fatalf("cmd() = %T", msg)
	}
	if !strings.Contains(strings.Join(got.files, ","), "docs") {
		t.Fatalf("merged files missing docs: %v", got.files)
	}
}
