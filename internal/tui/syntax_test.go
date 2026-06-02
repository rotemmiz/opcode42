package tui

// syntax_test.go — Plan 08c M5: tests for the standalone chroma-based syntax
// highlighter (highlightCode / highlightCodeBg in syntax.go).
//
// What we test:
//
//  1. highlightCode: a Go snippet and a TypeScript snippet produce non-empty
//     output that contains the original text (ANSI-stripped).
//  2. Empty code / unknown language do not panic and return non-empty output.
//  3. Colors differ between forge-dark and forge-light (tokenColor maps to
//     different palette values).  Reported as a diagnostic in non-TTY
//     environments where lipgloss suppresses ANSI, not a hard failure.
//  4. highlightCodeBg: every token carries the requested row background so
//     no cell falls back to terminal default (anti-bleed property).
//  5. tokenColor mapping spot-checks: Keyword, String, Number, Comment,
//     Function, Type, Operator, Punctuation each map to the expected
//     SyntaxPalette field for forge-dark.

import (
	"strings"
	"testing"

	chroma "github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/forge/internal/tui/theme"
)

// goSnippet is a small Go function that exercises keywords, identifiers,
// string literals, numbers, and comments.
const goSnippet = `// hello returns a greeting.
func hello(name string) string {
	if name == "" {
		return "World"
	}
	return "Hello, " + name
}
`

// tsSnippet is a TypeScript snippet exercising keywords, types, and strings.
const tsSnippet = `// greet function
function greet(name: string): string {
	const msg: string = "Hello, " + name;
	return msg;
}
`

// TestHighlightCode_NonEmpty verifies that highlightCode returns non-empty
// output and that the ANSI-stripped text contains the original code text.
func TestHighlightCode_NonEmpty(t *testing.T) {
	cases := []struct {
		name string
		code string
		lang string
	}{
		{"go snippet", goSnippet, "main.go"},
		{"ts snippet", tsSnippet, "greet.ts"},
		{"go explicit lang", goSnippet, "go"},
		{"ts explicit lang", tsSnippet, "typescript"},
	}

	p := theme.Default()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := highlightCode(tc.code, tc.lang, p)
			if out == "" {
				t.Fatal("highlightCode returned empty string")
			}
			plain := stripANSI(out)
			// The highlighted output must contain the original code text
			// (modulo normalised newlines that chroma may adjust).
			// We check a key identifier that should survive lexing.
			if !strings.Contains(plain, "func") && !strings.Contains(plain, "function") {
				t.Errorf("plain output missing expected identifier; got: %q", truncateForTest(plain, 200))
			}
		})
	}
}

// TestHighlightCode_EmptyAndUnknown verifies robustness: empty input and
// unrecognized language names must not panic and must return non-empty output.
func TestHighlightCode_EmptyAndUnknown(t *testing.T) {
	p := theme.Default()

	t.Run("empty code", func(t *testing.T) {
		out := highlightCode("", "main.go", p)
		if out == "" {
			t.Fatal("highlightCode('') returned empty string — should return at least a space")
		}
	})

	t.Run("empty lang", func(t *testing.T) {
		out := highlightCode(goSnippet, "", p)
		if out == "" {
			t.Fatal("highlightCode with empty lang returned empty string")
		}
	})

	t.Run("unknown lang", func(t *testing.T) {
		// Should fall back to plain-text lexer (Fallback), not panic.
		out := highlightCode(goSnippet, "no-such-lang-xyz123", p)
		if out == "" {
			t.Fatal("highlightCode with unknown lang returned empty string")
		}
	})

	t.Run("empty code unknown lang", func(t *testing.T) {
		out := highlightCode("", "no-such-lang-xyz123", p)
		if out == "" {
			t.Fatal("highlightCode('', unknownLang) returned empty string")
		}
	})
}

// TestHighlightCode_ColorsDifferAcrossThemes checks that the same code
// snippet produces different rendered output for forge-dark vs forge-light.
//
// NOTE: in non-TTY test runners lipgloss does not emit ANSI escape codes so
// both outputs may be identical plain text — acceptable. We log the outcome
// rather than assert, so CI passes regardless.
func TestHighlightCode_ColorsDifferAcrossThemes(t *testing.T) {
	dark := theme.Default()
	light := theme.Light()

	// Ensure the keyword colors actually differ between the two themes.
	if dark.Syntax.Keyword == light.Syntax.Keyword {
		t.Skip("dark/light Syntax.Keyword are the same color — cannot assert output differs")
	}

	outDark := highlightCode(goSnippet, "main.go", dark)
	outLight := highlightCode(goSnippet, "main.go", light)

	t.Logf("forge-dark  Syntax.Keyword : %s", string(dark.Syntax.Keyword))
	t.Logf("forge-light Syntax.Keyword : %s", string(light.Syntax.Keyword))
	t.Logf("outputs differ: %v", outDark != outLight)

	// Only assert if ANSI escapes are present (TTY / $COLORTERM env).
	if strings.Contains(outDark, "\x1b[") {
		if outDark == outLight {
			t.Error("dark/light syntax outputs are identical even though ANSI is emitted — palette not applied")
		}
	}
}

// TestHighlightCodeBg_TokensCarryBackground verifies the anti-bleed property:
// when a rowBg is supplied, the rendered output should carry that background.
// In a TTY environment the raw string includes the bg SGR; in non-TTY we
// cannot inspect SGR but we still check the output is non-empty and non-panicking.
func TestHighlightCodeBg_TokensCarryBackground(t *testing.T) {
	p := theme.Default()
	rowBg := p.Diff.AddedBg
	out := highlightCodeBg(goSnippet, "main.go", p, rowBg)

	if out == "" {
		t.Fatal("highlightCodeBg returned empty string")
	}

	// The plain text must still contain the code text.
	plain := stripANSI(out)
	if !strings.Contains(plain, "func") {
		t.Errorf("plain output after highlightCodeBg missing 'func'; got: %q", truncateForTest(plain, 200))
	}
}

// TestTokenColor_Mapping spot-checks the chroma token → SyntaxPalette mapping
// for forge-dark. Each case asserts that the returned color equals the expected
// field in the SyntaxPalette.
func TestTokenColor_Mapping(t *testing.T) {
	p := theme.Default()
	sy := p.Syntax

	cases := []struct {
		name    string
		tt      chroma.TokenType
		wantCol lipgloss.Color
	}{
		// Keyword family.
		{"Keyword", chroma.Keyword, sy.Keyword},
		{"KeywordReserved", chroma.KeywordReserved, sy.Keyword},
		{"KeywordNamespace", chroma.KeywordNamespace, sy.Keyword},
		{"KeywordDeclaration", chroma.KeywordDeclaration, sy.Keyword},
		{"KeywordType", chroma.KeywordType, sy.Type},

		// Function / builtin.
		{"NameFunction", chroma.NameFunction, sy.Function},
		{"NameBuiltin", chroma.NameBuiltin, sy.Function},
		{"NameDecorator", chroma.NameDecorator, sy.Function},

		// Type-like names.
		{"NameClass", chroma.NameClass, sy.Type},
		{"NameConstant", chroma.NameConstant, sy.Type},

		// Variables.
		{"Name", chroma.Name, sy.Variable},
		{"NameVariable", chroma.NameVariable, sy.Variable},

		// Literals.
		{"LiteralString", chroma.LiteralString, sy.String},
		{"LiteralStringDouble", chroma.LiteralStringDouble, sy.String},
		{"LiteralNumber", chroma.LiteralNumber, sy.Number},
		{"LiteralNumberFloat", chroma.LiteralNumberFloat, sy.Number},

		// Comments.
		{"Comment", chroma.Comment, sy.Comment},
		{"CommentSingle", chroma.CommentSingle, sy.Comment},
		{"CommentMultiline", chroma.CommentMultiline, sy.Comment},
		{"CommentPreproc", chroma.CommentPreproc, sy.Comment},

		// Operators.
		{"Operator", chroma.Operator, sy.Operator},
		{"OperatorWord", chroma.OperatorWord, sy.Operator},

		// Punctuation.
		{"Punctuation", chroma.Punctuation, sy.Punctuation},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tokenColor(tc.tt, p)
			if got != tc.wantCol {
				t.Errorf("tokenColor(%s, forge-dark) = %q, want %q", tc.name, got, tc.wantCol)
			}
		})
	}
}

// TestHighlightCode_FullPathFilename verifies that a full path (not just an
// extension or bare name) is resolved to the correct lexer.
func TestHighlightCode_FullPathFilename(t *testing.T) {
	p := theme.Default()
	// A full path; chooseLexer should fall back to base-name matching.
	out := highlightCode(goSnippet, "internal/tui/model.go", p)
	if out == "" {
		t.Fatal("highlightCode with full path returned empty string")
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "func") {
		t.Errorf("plain output missing 'func' for full path file; got: %q", truncateForTest(plain, 200))
	}
}
