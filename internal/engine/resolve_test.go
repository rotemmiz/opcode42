package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanMentions(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		wants []string // expected mention names (sans "@"), in order
	}{
		{"plain", "see @main.go please", []string{"main.go"}},
		{"leading", "@README.md is the readme", []string{"README.md"}},
		{"path", "open @internal/engine/engine.go", []string{"internal/engine/engine.go"}},
		{"dot-prefix", "check @.gitignore", []string{".gitignore"}},
		{"none", "no mentions here", nil},
		// negative-lookbehind: an "@" preceded by a word char or backtick is not a mention
		{"email", "mail me at user@host.com", nil},
		{"backtick", "literal `@main.go` token", nil},
		{"multiple", "@a.go and @b.go", []string{"a.go", "b.go"}},
		// trailing punctuation is excluded by the regex char class
		{"trailing-comma", "use @main.go, then stop", []string{"main.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanMentions(tt.text)
			if len(got) != len(tt.wants) {
				t.Fatalf("scanMentions(%q) returned %d mentions, want %d: %+v", tt.text, len(got), len(tt.wants), got)
			}
			for i, w := range tt.wants {
				if got[i].name != w {
					t.Errorf("mention[%d].name = %q, want %q", i, got[i].name, w)
				}
				if got[i].value != "@"+w {
					t.Errorf("mention[%d].value = %q, want %q", i, got[i].value, "@"+w)
				}
				// offsets must point at the literal in the source text
				if tt.text[got[i].start:got[i].end] != got[i].value {
					t.Errorf("mention[%d] offsets [%d:%d] = %q, want %q",
						i, got[i].start, got[i].end, tt.text[got[i].start:got[i].end], got[i].value)
				}
			}
		})
	}
}

func TestResolvePromptParts_FileAndDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A .git marker makes dir the worktree root so mentions resolve against it.
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	e := New(Config{Directory: dir})
	out := e.ResolvePromptParts([]PartInput{
		{Type: "text", Text: "read @notes.md and list @pkg and ignore @missing.txt"},
	})

	// Original text part is preserved first, then one file part per resolved mention.
	if len(out) != 3 {
		t.Fatalf("got %d parts, want 3 (text + notes.md + pkg): %+v", len(out), out)
	}
	if out[0].Type != "text" {
		t.Fatalf("out[0] = %+v, want the original text part", out[0])
	}

	file := out[1]
	if file.Type != "file" || file.MIME != "text/plain" || file.Filename != "notes.md" {
		t.Errorf("file part = %+v, want file/text-plain/notes.md", file)
	}
	if !strings.HasPrefix(file.URL, "file://") || !strings.HasSuffix(file.URL, "/notes.md") {
		t.Errorf("file URL = %q, want a file:// URL ending in /notes.md", file.URL)
	}
	// File/dir parts carry no source, matching opencode's resolvePromptParts
	// output (prompt.ts:208-233).
	if file.Source != nil {
		t.Errorf("file part source = %s, want nil (opencode file parts have no source)", file.Source)
	}

	d := out[2]
	if d.Type != "file" || d.MIME != "application/x-directory" || d.Filename != "pkg" {
		t.Errorf("dir part = %+v, want file/x-directory/pkg", d)
	}
}

func TestResolvePromptParts_Dedup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{Directory: dir})
	out := e.ResolvePromptParts([]PartInput{
		{Type: "text", Text: "@a.go first"},
		{Type: "text", Text: "@a.go again"},
	})
	// 2 text parts + exactly 1 file part (the duplicate mention is resolved once).
	if len(out) != 3 {
		t.Fatalf("got %d parts, want 3 (two text + one deduped file): %+v", len(out), out)
	}
	files := 0
	for _, p := range out {
		if p.Type == "file" {
			files++
		}
	}
	if files != 1 {
		t.Errorf("got %d file parts, want 1 after dedup", files)
	}
}

func TestResolvePromptParts_NoLSP_SymbolDropped(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := New(Config{Directory: dir}) // LSP nil
	out := e.ResolvePromptParts([]PartInput{
		{Type: "text", Text: "jump to @SomeSymbol"},
	})
	// No file on disk, no LSP: the mention yields no part, only the text survives.
	if len(out) != 1 || out[0].Type != "text" {
		t.Fatalf("got %+v, want only the original text part", out)
	}
}

func TestSymbolPartFromResults(t *testing.T) {
	// A workspace/symbol result for a Function (kind 12) at lines 9-11 (0-based).
	abs := filepath.Join(t.TempDir(), "main.go")
	results := []json.RawMessage{json.RawMessage(`{
		"name": "Run",
		"kind": 12,
		"location": {
			"uri": "file://` + abs + `",
			"range": {"start": {"line": 9, "character": 0}, "end": {"line": 11, "character": 1}}
		}
	}`)}
	m := mention{name: "Run", value: "@Run", start: 8, end: 12}

	part := symbolPartFromResults(results, m)
	if part == nil {
		t.Fatal("symbolPartFromResults returned nil")
	}
	if part.Type != "file" || part.MIME != "text/plain" || part.Filename != "Run" {
		t.Errorf("part = %+v, want file/text-plain/Run", part)
	}
	// url carries 1-based line markers (range.start.line+1 .. range.end.line+1).
	if !strings.Contains(part.URL, "?start=10&end=12") {
		t.Errorf("url = %q, want ?start=10&end=12", part.URL)
	}
	var src symbolSource
	if err := json.Unmarshal(part.Source, &src); err != nil {
		t.Fatalf("symbol source decode: %v", err)
	}
	if src.Type != "symbol" || src.Name != "Run" || src.Kind != 12 {
		t.Errorf("source = %+v, want symbol/Run/kind=12", src)
	}
	if src.Path != abs {
		t.Errorf("source path = %q, want %q", src.Path, abs)
	}
	if src.Range.Start.Line != 9 || src.Range.End.Line != 11 {
		t.Errorf("source range = %+v, want 0-based start.line=9 end.line=11", src.Range)
	}
	if src.Text.Value != "@Run" || src.Text.Start != 8 || src.Text.End != 12 {
		t.Errorf("source text span = %+v, want @Run [8:12]", src.Text)
	}
}

func TestSymbolPartFromResults_SkipsNonFile(t *testing.T) {
	// First result has an unusable (non-file) URI; second is a real file.
	abs := filepath.Join(t.TempDir(), "x.go")
	results := []json.RawMessage{
		json.RawMessage(`{"name":"A","kind":12,"location":{"uri":"untitled:nope","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`),
		json.RawMessage(`{"name":"B","kind":12,"location":{"uri":"file://` + abs + `","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}`),
	}
	part := symbolPartFromResults(results, mention{name: "Sym", value: "@Sym"})
	if part == nil {
		t.Fatal("expected the second (file-backed) symbol to be used")
	}
	var src symbolSource
	_ = json.Unmarshal(part.Source, &src)
	if src.Name != "B" {
		t.Errorf("used symbol %q, want B (the file-backed one)", src.Name)
	}
}
