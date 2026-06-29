package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.lsp.dev/uri"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

// fileMentionRegex matches `@mentions` in prompt text. It is a direct port of
// opencode's FILE_REGEX (config/markdown.ts:6):
//
//	/(?<![\w`])@(\.?[^\s`,.]*(?:\.[^\s`,.]+)*)/g
//
// Go's regexp (RE2) lacks lookbehind, so the `(?<![\w`])` guard (no word char or
// backtick immediately before the `@`) is enforced manually in scanMentions.
var fileMentionRegex = regexp.MustCompile("@(\\.?[^\\s`,.]*(?:\\.[^\\s`,.]+)*)")

// mention is one `@name` occurrence in a text part: the captured name (sans `@`)
// plus the source span (the literal `@name` text and its byte offsets within the
// part), mirroring opencode's mentionSource (prompt.ts:149-152).
type mention struct {
	name  string
	value string // the full match including the leading "@"
	start int    // byte offset of "@" within the text
	end   int    // byte offset one past the match
}

// filePartSourceText is the `text` span carried by every FilePartSource variant
// (message-v2.ts:125-130): the mention literal and its offsets.
type filePartSourceText struct {
	Value string `json:"value"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// lspRange mirrors the LSP Range shape (line/character, 0-based) carried by a
// symbol source (message-v2.ts:139-146 uses LSP.Range).
type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// symbolSource is the SymbolSource variant (message-v2.ts:139-146): a `@symbol`
// mention resolved via LSP workspace/symbol.
type symbolSource struct {
	Type  string             `json:"type"` // "symbol"
	Path  string             `json:"path"`
	Range lspRange           `json:"range"`
	Name  string             `json:"name"`
	Kind  int                `json:"kind"`
	Text  filePartSourceText `json:"text"`
}

// ResolvePromptParts expands `@file`, `@dir`, and `@symbol` mentions found in the
// prompt's text parts into structured file parts, preserving the original parts.
//
// It ports opencode's SessionPrompt.resolvePromptParts (prompt.ts:144-238): each
// unique mention is resolved once (de-duplicated by name, first occurrence wins),
// the original text part is kept, and one file part is appended per resolved
// mention. File/dir mentions become FileSource parts (url = file:// URL, mime
// "text/plain" or "application/x-directory"); symbol mentions are resolved via
// LSP workspace/symbol into SymbolSource parts. Mentions that resolve to neither
// are left as plain text (no part emitted), matching opencode dropping unknown
// `@names`.
//
// The later expansion of these file parts into Read-tool synthetic text
// (prompt.ts:792-1043) is the message-processor's job; here we only normalize the
// draft parts into opencode's resulting file-part shapes.
func (e *Engine) ResolvePromptParts(parts []PartInput) []PartInput {
	root := worktree.Root(worktree.Resolve(e.cfg.Directory))
	seen := make(map[string]bool)
	out := make([]PartInput, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
		if p.Type != "text" || p.Text == "" {
			continue
		}
		for _, m := range scanMentions(p.Text) {
			if seen[m.name] {
				continue
			}
			seen[m.name] = true
			if fp := e.resolveMention(root, m); fp != nil {
				out = append(out, *fp)
			}
		}
	}
	return out
}

// resolveMention resolves a single mention to a file part, or nil if it matches
// neither a filesystem path nor an LSP symbol. Path resolution mirrors
// prompt.ts:217-219 (`~/` → home, else resolve against the worktree).
func (e *Engine) resolveMention(root string, m mention) *PartInput {
	if fp := resolveFileMention(root, m); fp != nil {
		return fp
	}
	return e.resolveSymbolMention(m)
}

// resolveFileMention resolves an `@file`/`@dir` mention against the filesystem,
// returning a file part or nil if the path does not exist. The part shape mirrors
// opencode's resolvePromptParts file/dir parts exactly (prompt.ts:208-233):
// {type, url, filename, mime} with no source (a source is only carried by
// client-supplied or symbol parts).
func resolveFileMention(root string, m mention) *PartInput {
	var abs string
	switch {
	case strings.HasPrefix(m.name, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		abs = filepath.Join(home, m.name[2:])
	default:
		abs = filepath.Join(root, m.name)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	mime := "text/plain"
	if info.IsDir() {
		mime = "application/x-directory"
	}
	return &PartInput{
		Type:     "file",
		MIME:     mime,
		URL:      string(uri.File(abs)),
		Filename: m.name,
	}
}

// resolveSymbolMention resolves an `@symbol` mention via LSP workspace/symbol,
// returning a SymbolSource file part for the first matching symbol, or nil when
// LSP is unavailable or no symbol matches. The file part's url points at the
// symbol's file with `?start=&end=` line markers so the downstream Read
// expansion reads the symbol's range (prompt.ts:896-915).
func (e *Engine) resolveSymbolMention(m mention) *PartInput {
	if e.cfg.LSP == nil || m.name == "" {
		return nil
	}
	return symbolPartFromResults(e.cfg.LSP.WorkspaceSymbol(m.name), m)
}

// symbolPartFromResults builds a SymbolSource file part from the first usable
// workspace/symbol result, or nil if none decode to a file-backed symbol. Split
// out as a pure function so the part shape can be tested without a live LSP.
func symbolPartFromResults(results []json.RawMessage, m mention) *PartInput {
	for _, raw := range results {
		var sym workspaceSymbol
		if err := json.Unmarshal(raw, &sym); err != nil {
			continue
		}
		// Only file:// symbols can be turned into a Read-backed file part;
		// opencode's fileURLToPath rejects non-file schemes (e.g. untitled:).
		if !strings.HasPrefix(sym.Location.URI, "file://") {
			continue
		}
		path := uri.New(sym.Location.URI).Filename()
		if path == "" {
			continue
		}
		rng := sym.Location.Range
		src, _ := json.Marshal(symbolSource{
			Type:  "symbol",
			Path:  path,
			Range: rng,
			Name:  sym.Name,
			Kind:  sym.Kind,
			Text:  filePartSourceText{Value: m.value, Start: m.start, End: m.end},
		})
		// start/end are 1-based line numbers in the read range query
		// (prompt.ts:896-915 reads searchParams as 1-based line offsets).
		startLine := rng.Start.Line + 1
		endLine := rng.End.Line + 1
		fileURI := string(uri.File(path))
		url := fileURI + "?start=" + strconv.Itoa(startLine) + "&end=" + strconv.Itoa(endLine)
		return &PartInput{
			Type:     "file",
			MIME:     "text/plain",
			URL:      url,
			Filename: m.name,
			Source:   src,
		}
	}
	return nil
}

// workspaceSymbol is the minimal subset of an LSP WorkspaceSymbol /
// SymbolInformation needed to build a SymbolSource: name, kind, and the symbol's
// location. opencode's workspace/symbol results carry a Location (uri + range);
// see lsp/query.go's curated workspace symbols.
type workspaceSymbol struct {
	Name     string `json:"name"`
	Kind     int    `json:"kind"`
	Location struct {
		URI   string   `json:"uri"`
		Range lspRange `json:"range"`
	} `json:"location"`
}

// scanMentions extracts `@name` mentions from text, enforcing opencode's
// negative-lookbehind guard (no word char or backtick immediately before the
// `@`) that RE2 cannot express directly (config/markdown.ts:6).
func scanMentions(text string) []mention {
	idxs := fileMentionRegex.FindAllStringSubmatchIndex(text, -1)
	out := make([]mention, 0, len(idxs))
	for _, loc := range idxs {
		start, end := loc[0], loc[1]
		if start > 0 {
			prev := text[start-1]
			if prev == '`' || isWordByte(prev) {
				continue
			}
		}
		name := text[loc[2]:loc[3]]
		if name == "" {
			continue
		}
		out = append(out, mention{
			name:  name,
			value: text[start:end],
			start: start,
			end:   end,
		})
	}
	return out
}

// isWordByte reports whether b is a `\w` byte (ASCII letter, digit, or "_").
// opencode's lookbehind uses JavaScript `\w`, which is ASCII-only.
func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}
