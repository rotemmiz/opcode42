package theme

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDefaultPaletteMatchesTokens(t *testing.T) {
	p := Default()
	cases := map[string]lipgloss.Color{
		"#15171a": p.Bg, "#1c1f23": p.BgPanel, "#20242a": p.BgElev,
		"#d6dade": p.Fg, "#8b929a": p.FgDim,
		"#6fa8dc": p.Blue, "#8cc265": p.Green, "#e0606e": p.Red,
		"#d99a4e": p.Amber, "#b08cd4": p.Purple, "#5fb3c4": p.Cyan,
		"#1a1207": p.SelFg,
	}
	for hex, got := range cases {
		if string(got) != hex {
			t.Errorf("token mismatch: got %q want %q", string(got), hex)
		}
	}
	if p.Accent() != p.Blue {
		t.Fatalf("accent should alias blue")
	}
}

func TestSelectionStyleColors(t *testing.T) {
	s := DefaultStyles()
	// The canonical cursor row is amber bg + near-black fg, bold.
	out := s.Selection.Render("row")
	if !strings.Contains(out, "row") {
		t.Fatalf("selection render lost content: %q", out)
	}
	if s.Selection.GetBackground() != Default().SelBg || s.Selection.GetForeground() != Default().SelFg {
		t.Fatalf("selection colors wrong")
	}
	if !s.Selection.GetBold() {
		t.Fatalf("selection should be bold")
	}
}

func TestSectionIsPurpleBold(t *testing.T) {
	s := DefaultStyles()
	if s.Section.GetForeground() != Default().Purple || !s.Section.GetBold() {
		t.Fatalf("section header should be purple bold")
	}
}

// TestPaletteNoZeroTokens is the M1 contract test: every field in the Diff,
// Markdown, and Syntax sub-structs plus BorderActive must be non-empty for all
// registered palettes. M2 (JSON theme loader) relies on this invariant to
// guarantee that renderers in M4–M6 always get a usable color.
func TestPaletteNoZeroTokens(t *testing.T) {
	for _, named := range Palettes() {
		p := named.Palette
		t.Run(named.Name, func(t *testing.T) {
			// BorderActive (flat field added in M1).
			if p.BorderActive == "" {
				t.Error("BorderActive is zero-value (empty string)")
			}

			// DiffPalette — all fields must be set.
			checkStructNoZero(t, "Diff", p.Diff)

			// MarkdownPalette — all fields must be set.
			checkStructNoZero(t, "Markdown", p.Markdown)

			// SyntaxPalette — all fields must be set.
			checkStructNoZero(t, "Syntax", p.Syntax)
		})
	}
}

// checkStructNoZero reflects over a struct of lipgloss.Color fields and fails
// the test for every field that is the empty-string zero value.
func checkStructNoZero(t *testing.T, prefix string, v interface{}) {
	t.Helper()
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		field := rt.Field(i)
		val := rv.Field(i).Interface()
		color, ok := val.(lipgloss.Color)
		if !ok {
			t.Errorf("%s.%s: unexpected non-Color field type %T", prefix, field.Name, val)
			continue
		}
		if color == "" {
			t.Errorf("%s.%s is zero-value (empty lipgloss.Color)", prefix, field.Name)
		}
	}
}

// TestDiffPaletteDefaultValues spot-checks concrete values in the Default diff
// palette to guard against accidental zero-assignment in code edits.
func TestDiffPaletteDefaultValues(t *testing.T) {
	p := Default()
	cases := []struct {
		name string
		got  lipgloss.Color
		want lipgloss.Color
	}{
		{"Diff.Added", p.Diff.Added, p.Green},
		{"Diff.Removed", p.Diff.Removed, p.Red},
		{"Diff.Context", p.Diff.Context, p.Cyan},
		{"Diff.HunkHeader", p.Diff.HunkHeader, p.Cyan},
		{"Diff.HighlightAdded", p.Diff.HighlightAdded, "#b8db87"},
		{"Diff.HighlightRemoved", p.Diff.HighlightRemoved, "#e26a75"},
		{"Diff.AddedBg", p.Diff.AddedBg, "#1f2e24"},
		{"Diff.RemovedBg", p.Diff.RemovedBg, "#2e1f23"},
		{"Diff.ContextBg", p.Diff.ContextBg, p.BgPanel},
		{"Diff.LineNumber", p.Diff.LineNumber, p.FgFaint},
		{"Diff.AddedLineNumberBg", p.Diff.AddedLineNumberBg, "#182620"},
		{"Diff.RemovedLineNumberBg", p.Diff.RemovedLineNumberBg, "#271a1f"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.got, c.want)
		}
	}
}

// TestMarkdownPaletteDefaultValues spot-checks concrete values in the Default
// markdown palette.
func TestMarkdownPaletteDefaultValues(t *testing.T) {
	p := Default()
	cases := []struct {
		name string
		got  lipgloss.Color
		want lipgloss.Color
	}{
		{"Markdown.Text", p.Markdown.Text, p.Fg},
		{"Markdown.Heading", p.Markdown.Heading, p.Purple},
		{"Markdown.Link", p.Markdown.Link, p.Blue},
		{"Markdown.LinkText", p.Markdown.LinkText, p.Cyan},
		{"Markdown.Code", p.Markdown.Code, p.Green},
		{"Markdown.BlockQuote", p.Markdown.BlockQuote, p.Amber},
		{"Markdown.Emph", p.Markdown.Emph, p.Amber},
		{"Markdown.Strong", p.Markdown.Strong, p.Red},
		{"Markdown.HorizontalRule", p.Markdown.HorizontalRule, p.FgFaint},
		{"Markdown.ListItem", p.Markdown.ListItem, p.Fg},
		{"Markdown.ListEnumeration", p.Markdown.ListEnumeration, p.Cyan},
		{"Markdown.Image", p.Markdown.Image, p.Blue},
		{"Markdown.ImageText", p.Markdown.ImageText, p.Cyan},
		{"Markdown.CodeBlock", p.Markdown.CodeBlock, p.Fg},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.got, c.want)
		}
	}
}

// TestSyntaxPaletteDefaultValues spot-checks concrete values in the Default
// syntax palette.
func TestSyntaxPaletteDefaultValues(t *testing.T) {
	p := Default()
	cases := []struct {
		name string
		got  lipgloss.Color
		want lipgloss.Color
	}{
		{"Syntax.Comment", p.Syntax.Comment, p.FgFaint},
		{"Syntax.Keyword", p.Syntax.Keyword, p.Purple},
		{"Syntax.Function", p.Syntax.Function, p.Blue},
		{"Syntax.Variable", p.Syntax.Variable, p.Red},
		{"Syntax.String", p.Syntax.String, p.Green},
		{"Syntax.Number", p.Syntax.Number, p.Amber},
		{"Syntax.Type", p.Syntax.Type, p.Yellow},
		{"Syntax.Operator", p.Syntax.Operator, p.Cyan},
		{"Syntax.Punctuation", p.Syntax.Punctuation, p.Fg},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.got, c.want)
		}
	}
}

// TestBorderActiveIsSet ensures BorderActive is distinct from Border for the
// 3 native Forge palettes where the design mandates a brighter active border.
// Embedded opencode themes are excluded because some (e.g. ayu) legitimately
// share the same color for border and borderActive.
func TestBorderActiveIsSet(t *testing.T) {
	nativeNames := map[string]bool{
		"forge-dark":  true,
		"forge-light": true,
		"monochrome":  true,
	}
	for _, named := range Palettes() {
		if !nativeNames[named.Name] {
			continue
		}
		p := named.Palette
		if p.BorderActive == "" {
			t.Errorf("%s: BorderActive is zero", named.Name)
		}
		// BorderActive should be a different (brighter) value than Border
		// for the two color themes; monochrome is exempt.
		if named.Name != "monochrome" && p.BorderActive == p.Border {
			t.Errorf("%s: BorderActive %q should differ from Border %q",
				named.Name, p.BorderActive, p.Border)
		}
	}
}

// TestAllPalettesHaveConsistentSubstructFieldCounts checks that the number of
// fields in each sub-struct matches what we expect (catches accidental field
// removal via reflection count).
func TestAllPalettesHaveConsistentSubstructFieldCounts(t *testing.T) {
	wantDiff := 12
	wantMarkdown := 14
	wantSyntax := 9

	rt := reflect.TypeOf(DiffPalette{})
	if rt.NumField() != wantDiff {
		t.Errorf("DiffPalette has %d fields, want %d", rt.NumField(), wantDiff)
	}
	rt = reflect.TypeOf(MarkdownPalette{})
	if rt.NumField() != wantMarkdown {
		t.Errorf("MarkdownPalette has %d fields, want %d", rt.NumField(), wantMarkdown)
	}
	rt = reflect.TypeOf(SyntaxPalette{})
	if rt.NumField() != wantSyntax {
		t.Errorf("SyntaxPalette has %d fields, want %d", rt.NumField(), wantSyntax)
	}
}
