package tui

// syntax.go — Plan 08c M5: chroma-based syntax highlighter for the diff viewer
// (and any standalone code that needs ANSI highlighting without glamour's
// markdown context).
//
// WHY a standalone highlighter (not glamour): glamour's chroma integration is
// wired into its goldmark pipeline and only fires inside fenced code blocks.
// The diff viewer (M6) needs to highlight individual stripped source lines
// (no markdown framing) and must compose the syntax foreground colors WITH a
// per-row diff background tint. The standalone path here gives us that control.
//
// WHY token→lipgloss instead of a chroma Style + ANSI formatter: the standard
// chroma ANSI formatter emits its own background/reset sequences keyed against
// a chroma.Style, but we need each token to carry the diff-row background so
// no transparent cells appear between tokens. Building lipgloss.Style per token
// lets us set both Foreground (syntax color) and Background (diff tint) in one
// SGR sequence, eliminating any gap between tokens.
//
// Token mapping (chroma type → SyntaxPalette field):
//
//	Keyword / KeywordReserved / KeywordNamespace / KeywordDeclaration / KeywordPseudo → Keyword
//	KeywordType                                                                        → Type
//	NameFunction / NameFunctionMagic / NameDecorator / NameBuiltin / NameBuiltinPseudo → Function
//	NameClass / NameConstant / NameException                                           → Type
//	NameVariable / NameVariableClass / NameVariableGlobal / NameVariableInstance /
//	  NameVariableMagic / NameVariableAnonymous                                        → Variable
//	LiteralString and all sub-types                                                    → String
//	LiteralNumber and all sub-types                                                    → Number
//	Comment / CommentSingle / CommentMultiline / CommentHashbang / CommentSpecial /
//	  CommentPreproc / CommentPreprocFile                                              → Comment
//	Operator / OperatorWord                                                            → Operator
//	Punctuation                                                                        → Punctuation
//	Name (bare identifier)                                                             → Variable
//	anything else                                                                      → Fg (default)
//
// Anti-bleed: every token style produced by tokenStyle() / tokenStyleBg() sets
// a Background, so no cell between tokens reverts to terminal default. See
// highlightCodeBg for the diff-viewer path that passes a row background.

import (
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	chroma "github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// tokenColor returns the theme.Color from p.Syntax that corresponds to the
// given chroma TokenType. The TokenType hierarchy in chroma is represented by
// integer ranges; we use In() to check each parent category in priority order.
//
// The mapping follows opencode's dark-theme syntax token assignment (verified
// against design/tui/styles.css and opencode.json syntax* keys).
func tokenColor(tt chroma.TokenType, p theme.Palette) theme.Color {
	sy := p.Syntax

	switch {
	// Keywords — high-priority; check KeywordType before the generic Keyword block.
	case tt == chroma.KeywordType:
		return sy.Type

	case tt.InCategory(chroma.Keyword):
		// Keyword, KeywordConstant, KeywordDeclaration, KeywordNamespace,
		// KeywordPseudo, KeywordReserved all map to Keyword.
		return sy.Keyword

	// Functions and built-ins.
	case tt == chroma.NameFunction,
		tt == chroma.NameFunctionMagic,
		tt == chroma.NameDecorator,
		tt == chroma.NameBuiltin,
		tt == chroma.NameBuiltinPseudo:
		return sy.Function

	// Types and class-like names.
	case tt == chroma.NameClass,
		tt == chroma.NameConstant,
		tt == chroma.NameException:
		return sy.Type

	// Variable names (all sub-variants).
	case tt == chroma.Name,
		tt == chroma.NameVariable,
		tt == chroma.NameVariableAnonymous,
		tt == chroma.NameVariableClass,
		tt == chroma.NameVariableGlobal,
		tt == chroma.NameVariableInstance,
		tt == chroma.NameVariableMagic:
		return sy.Variable

	// String literals — all LiteralString sub-types.
	// InSubCategory is used (not InCategory) because LiteralString (3100) and
	// LiteralNumber (3200) share the same 1000-block (Literal = 3000); using
	// InCategory for LiteralString would incorrectly match LiteralNumber too.
	case tt.InSubCategory(chroma.LiteralString):
		return sy.String

	// Numeric literals (3200 sub-category).
	case tt.InSubCategory(chroma.LiteralNumber):
		return sy.Number

	// Comments (all flavors; Comment = 6000, sub-categories 6000 and 6100).
	// Use InCategory here since Comment (6000) and CommentPreproc (6100) are
	// sub-categories within the same 1000-block and we want both.
	case tt.InCategory(chroma.Comment):
		return sy.Comment

	// Operators (Operator = 4000; OperatorWord = 4001).
	case tt.InCategory(chroma.Operator):
		return sy.Operator

	// Punctuation.
	case tt == chroma.Punctuation:
		return sy.Punctuation

	// Fallback: emit the palette's primary foreground color.
	default:
		return p.Fg
	}
}

// tokenStyleBg returns a lipgloss.Style with the syntax foreground and a
// specific row background tint set.
// Used by the diff viewer to compose syntax foreground + diff row tint without
// any gap or reset exposing terminal-default background between tokens.
func tokenStyleBg(tt chroma.TokenType, p theme.Palette, bg theme.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(tokenColor(tt, p)).
		Background(bg)
}

// chooseLexer resolves a chroma Lexer from a filename or explicit language tag.
//
// Strategy (in order):
//  1. lexers.Get(filenameOrLang)  — exact name / alias match (e.g. "go", "python").
//  2. lexers.Match(filenameOrLang) — file-glob match (e.g. "main.go", "*.ts").
//  3. lexers.Match on the base name of filenameOrLang (handles full paths).
//  4. lexers.Fallback            — plain text, no highlighting.
//
// Never returns nil.
func chooseLexer(filenameOrLang string) chroma.Lexer {
	// Try as a language name / alias first (fast path for explicit "go", "ts" etc.).
	if l := lexers.Get(filenameOrLang); l != nil {
		return chroma.Coalesce(l)
	}
	// Try as a filename glob (handles "main.go", "*.ts", full paths).
	if l := lexers.Match(filenameOrLang); l != nil {
		return chroma.Coalesce(l)
	}
	// For full paths, also try just the base name (e.g. "internal/tui/model.go" → "model.go").
	if base := filepath.Base(filenameOrLang); base != filenameOrLang {
		if l := lexers.Match(base); l != nil {
			return chroma.Coalesce(l)
		}
	}
	return chroma.Coalesce(lexers.Fallback)
}

// highlightCode syntax-highlights code using chroma, themed from the given
// palette. Each token is rendered with its syntax foreground on p.Bg so that
// the output is consistently background-painted. Suitable for standalone code
// display (not inside a diff row — use highlightCodeBg for that).
//
// Parameters:
//   - code: the source text to highlight (may be empty or multi-line).
//   - filenameOrLang: a filename ("model.go"), file glob ("*.ts"), or language
//     alias ("go", "typescript") used to select the lexer. An empty string or
//     unrecognized value falls back to the plain-text lexer (no panic).
//   - p: the active theme palette; tokenColor(tt, p) maps each chroma token to
//     the appropriate SyntaxPalette field.
//
// Return value: an ANSI-colored string. The string does NOT have a trailing
// newline unless code itself ends with one — callers are responsible for
// joining lines.
//
// Robustness guarantees:
//   - Empty code → returns a single space rendered with Fg on p.Bg (non-empty,
//     no panic). Callers that need a truly empty string should check code first.
//   - Unknown language / nil lexer → falls back to lexers.Fallback (plain text),
//     which still works and is styled with Fg.
//   - Tokenisation error → returns code styled as plain text (Fg on Bg).
func highlightCode(code, filenameOrLang string, p theme.Palette) string {
	return highlightCodeBg(code, filenameOrLang, p, p.Bg)
}

// highlightCodeBg is the diff-viewer variant of highlightCode: every token is
// rendered with syntax foreground on rowBg instead of p.Bg. This ensures that
// the diff row background tint fills every cell — including the spaces between
// tokens — with no terminal-default transparent gaps.
//
// Anti-bleed mechanism: lipgloss does NOT re-apply a parent Background after an
// inner \x1b[0m reset. Each token style produced here sets Background(rowBg)
// explicitly, so every SGR span carries the row tint. The caller also post-pads
// the whole row to pane width using the same rowBg, covering any trailing cells
// after the last token (see diffPatchPane).
func highlightCodeBg(code, filenameOrLang string, p theme.Palette, rowBg theme.Color) string {
	if code == "" {
		// Return a single space on rowBg so the caller can still measure width.
		return lipgloss.NewStyle().Background(rowBg).Foreground(p.Fg).Render(" ")
	}

	lexer := chooseLexer(filenameOrLang)

	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		// Tokenisation error: fall back to plain text with the fg color.
		return lipgloss.NewStyle().Foreground(p.Fg).Background(rowBg).Render(code)
	}

	var sb strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		if tok.Value == "" {
			continue
		}
		st := tokenStyleBg(tok.Type, p, rowBg)
		sb.WriteString(st.Render(tok.Value))
	}
	return sb.String()
}
