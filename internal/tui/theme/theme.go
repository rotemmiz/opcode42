// Package theme is the Opcode42 TUI's color system, lifted verbatim from the
// design handoff's tokens (design/tui/styles.css :root). It exposes a Palette of
// truecolor values and the canonical Lipgloss styles the design defines
// (selection bar, mode chip, semantic text). Lipgloss degrades truecolor to the
// terminal's best available palette automatically.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Color is a truecolor hex string ("#rrggbb"), ANSI index, or color name that
// also implements image/color.Color so it can be handed directly to Lip Gloss
// v2's Foreground/Background (which now take color.Color). Keeping it a string
// preserves the TUI's hex-level color math — SGR building (paint.go), gradient
// lerps (spinner.go/logo.go), and diff tints all do string(c) / hex arithmetic.
//
// Lip Gloss v2 special-cases the NoColor *type* to mean "no color"; an empty
// Color would render opaque black instead, so callers must never pass a
// zero-value Color to a style. In practice every palette field is populated and
// the few local color vars are assigned in exhaustive switches (see diff.go).
type Color string

// RGBA implements image/color.Color by delegating to Lip Gloss's own parser.
func (c Color) RGBA() (r, g, b, a uint32) {
	return lipgloss.Color(string(c)).RGBA()
}

// Color must satisfy color.Color so it can be passed to Lip Gloss v2 styles.
var _ color.Color = Color("")

// Palette holds the design tokens. Names mirror the CSS custom properties.
//
// Flat fields (Bg, Fg, Blue, …) are the original Opcode42 tokens kept for
// back-compat. The three sub-structs (Diff, Markdown, Syntax) and
// BorderActive extend the palette to opencode's full token surface so that
// renderers in M4–M6 can consume standard names. The plan-08c mapping table
// (§1a) records which opencode token each field satisfies.
type Palette struct {
	// Surfaces (neutral charcoal).
	// opencode: background / backgroundPanel / backgroundElement / (selection)
	Bg      Color // terminal background
	BgPanel Color // collapsible panels, composer, autocomplete
	BgElev  Color // modals / popovers
	BgSel   Color // row hover

	// Borders.
	// opencode: border / borderActive / borderSubtle
	Border     Color // table/panel/modal borders
	BorderSoft Color // hairline dividers, sidebar edge (→ borderSubtle)
	// BorderActive is the focused/active component border; brighter than Border.
	// Derived from: opencode token "borderActive" (darkStep8 / lightStep8).
	BorderActive Color

	// Text.
	// opencode: text / textMuted / (faint, ghost are Opcode42 extensions)
	Fg      Color // primary
	FgDim   Color // secondary, tool-call lines
	FgFaint Color // hints, line numbers, metadata
	FgGhost Color // placeholders, disabled, diff gutters

	// Semantic colors (meanings fixed by the design — do not repurpose).
	// opencode mapping: secondary=Blue, accent=Purple, error=Red,
	// warning=Amber, success=Green, info=Cyan.
	Blue   Color // agent mode, prompt accent, function names
	Green  Color // added diff, success, paths, strings
	Red    Color // removed diff, errors, blocked
	Amber  Color // selection highlight, in-progress, thinking
	Purple Color // section headers, keywords, table headers
	Cyan   Color // types, @mentions, links, hunk markers
	Yellow Color // reserve / rarely used

	// Selection bar (modal & table highlight): solid amber, near-black text.
	SelBg Color
	SelFg Color

	// Sub-structs extend Palette to opencode's full token surface (plan 08c §1a).
	// All M2-M6 renderers consume these rather than the raw flat colors above,
	// so adding a new theme only requires filling in these structs.
	Diff     DiffPalette
	Markdown MarkdownPalette
	Syntax   SyntaxPalette
}

// DiffPalette holds per-line and per-cell diff colors mirroring opencode's
// diff token group (opencode.json "diff*" keys). The diff viewer (diff.go,
// M6) reads these exclusively so it can re-theme with the palette.
//
// Naming convention: Go-idiomatic CamelCase of the camelCase opencode name
// with the "diff" prefix dropped (e.g. "diffAddedBg" → "AddedBg").
type DiffPalette struct {
	// Added / Removed / Context are foreground colors for the respective line types.
	// opencode: diffAdded, diffRemoved, diffContext
	Added   Color
	Removed Color
	Context Color

	// HunkHeader is the foreground for @@ hunk-header lines.
	// opencode: diffHunkHeader
	HunkHeader Color

	// HighlightAdded / HighlightRemoved are intra-line span highlights
	// (the changed span within an add/remove line — brighter than AddedBg).
	// opencode: diffHighlightAdded, diffHighlightRemoved
	HighlightAdded   Color
	HighlightRemoved Color

	// AddedBg / RemovedBg / ContextBg are full-row background tints.
	// opencode: diffAddedBg, diffRemovedBg, diffContextBg
	AddedBg   Color
	RemovedBg Color
	ContextBg Color

	// LineNumber is the foreground for the line-number gutter text.
	// opencode: diffLineNumber
	LineNumber Color

	// AddedLineNumberBg / RemovedLineNumberBg are background tints for the
	// line-number gutter column on added/removed rows (darker than the row bg).
	// opencode: diffAddedLineNumberBg, diffRemovedLineNumberBg
	AddedLineNumberBg   Color
	RemovedLineNumberBg Color
}

// MarkdownPalette holds rendering colors for each markdown element, mirroring
// opencode's markdown token group (opencode.json "markdown*" keys). The
// glamour-based markdown renderer (M4) builds its ansi.StyleConfig from these.
//
// Naming convention: CamelCase of the opencode name with "markdown" prefix
// dropped (e.g. "markdownHeading" → "Heading", "markdownCodeBlock" → "CodeBlock").
type MarkdownPalette struct {
	// Text is the base prose color (typically Fg).
	// opencode: markdownText
	Text Color

	// Heading is the h1–h6 foreground.
	// opencode: markdownHeading
	Heading Color

	// Link is the URL foreground (the raw href part).
	// opencode: markdownLink
	Link Color

	// LinkText is the visible link label foreground (the [label] part).
	// opencode: markdownLinkText
	LinkText Color

	// Code is the foreground for inline `code` spans.
	// opencode: markdownCode
	Code Color

	// BlockQuote is the foreground for > blockquote lines.
	// opencode: markdownBlockQuote
	BlockQuote Color

	// Emph is the foreground for *italic* text.
	// opencode: markdownEmph
	Emph Color

	// Strong is the foreground for **bold** text.
	// opencode: markdownStrong
	Strong Color

	// HorizontalRule is the foreground for --- dividers.
	// opencode: markdownHorizontalRule
	HorizontalRule Color

	// ListItem is the foreground for list item text.
	// opencode: markdownListItem
	ListItem Color

	// ListEnumeration is the foreground for the bullet/number marker.
	// opencode: markdownListEnumeration
	ListEnumeration Color

	// Image is the foreground for ![alt](url) image tokens.
	// opencode: markdownImage
	Image Color

	// ImageText is the foreground for the alt-text portion of an image.
	// opencode: markdownImageText
	ImageText Color

	// CodeBlock is the base foreground for fenced code block bodies
	// (before syntax highlighting is applied by M5/chroma).
	// opencode: markdownCodeBlock
	CodeBlock Color
}

// SyntaxPalette holds chroma token-class colors, mirroring opencode's syntax
// token group (opencode.json "syntax*" keys). The chroma-based highlighter
// (M5) builds its custom style from these. The mapping to chroma token classes
// is documented in theme/syntax.go (M5).
//
// Naming convention: CamelCase of the opencode name with "syntax" prefix
// dropped (e.g. "syntaxKeyword" → "Keyword", "syntaxOperator" → "Operator").
type SyntaxPalette struct {
	// Comment is the foreground for line/block comments (muted).
	// opencode: syntaxComment
	Comment Color

	// Keyword is the foreground for language keywords (if, func, return, …).
	// opencode: syntaxKeyword
	Keyword Color

	// Function is the foreground for function/method names at call/def sites.
	// opencode: syntaxFunction
	Function Color

	// Variable is the foreground for variable names and identifiers.
	// opencode: syntaxVariable
	Variable Color

	// String is the foreground for string literals.
	// opencode: syntaxString
	String Color

	// Number is the foreground for numeric literals.
	// opencode: syntaxNumber
	Number Color

	// Type is the foreground for type names, class names, and type annotations.
	// opencode: syntaxType
	Type Color

	// Operator is the foreground for operators (+, -, =>, …).
	// opencode: syntaxOperator
	Operator Color

	// Punctuation is the foreground for brackets, braces, commas, semicolons.
	// opencode: syntaxPunctuation
	Punctuation Color
}

// Accent is the UI accent color (the design aliases --accent to --blue).
func (p Palette) Accent() Color { return p.Blue }

// Default returns the design's charcoal (opcode42-dark) palette.
//
// Sub-struct derivation rationale (plan 08c §1a):
//
// Diff:
//   - Added/Removed/Context/HunkHeader use semantic Green, Red, Cyan directly as
//     fg since those are the design's established diff foregrounds.
//   - HighlightAdded/Removed are brighter versions: #b8db87 (lighter green) and
//     #e26a75 (lighter red) — same values as opencode's dark diff highlights so
//     intra-line spans pop over AddedBg.
//   - AddedBg/RemovedBg are deep tinted darks (#1f2e24 / #2e1f23) that read as
//     subtle green/red wash on the near-black Bg without blowing out the fg text.
//   - ContextBg matches BgPanel so unmodified lines feel like a panel surface.
//   - LineNumber is FgFaint — same treatment as gutter line numbers everywhere.
//   - AddedLineNumberBg/RemovedLineNumberBg are slightly darker than AddedBg/RemovedBg.
//
// Markdown:
//   - Text=Fg, Heading=Purple (section headers), Link=Blue (accent), LinkText=Cyan,
//     Code=Green (strings), BlockQuote=Amber (thinking/warning tone), Emph=Amber,
//     Strong=Red (warning weight), HorizontalRule=FgFaint, ListItem=Fg,
//     ListEnumeration=Cyan (like @mentions), Image=Blue, ImageText=Cyan,
//     CodeBlock=Fg (base; chroma layered on top by M5).
//
// Syntax (mirrors opencode's own dark mapping via the defs in opencode.json):
//   - Comment=FgFaint (muted), Keyword=Purple (keywords), Function=Blue (function names),
//     Variable=Red, String=Green (strings), Number=Amber (warning-adjacent warm tone),
//     Type=Yellow (type annotations), Operator=Cyan, Punctuation=Fg.
func Default() Palette {
	p := Palette{
		Bg: "#15171a", BgPanel: "#1c1f23", BgElev: "#20242a", BgSel: "#262b31",
		Border: "#2c3137", BorderSoft: "#23272c", BorderActive: "#585f67",
		Fg: "#d6dade", FgDim: "#8b929a", FgFaint: "#585f67", FgGhost: "#3a4047",
		Blue: "#6fa8dc", Green: "#8cc265", Red: "#e0606e", Amber: "#d99a4e",
		Purple: "#b08cd4", Cyan: "#5fb3c4", Yellow: "#d6c370",
		SelBg: "#d99a4e", SelFg: "#1a1207",
	}
	p.Diff = DiffPalette{
		Added:               p.Green,
		Removed:             p.Red,
		Context:             p.Cyan,
		HunkHeader:          p.Cyan,
		HighlightAdded:      "#b8db87",
		HighlightRemoved:    "#e26a75",
		AddedBg:             "#1f2e24",
		RemovedBg:           "#2e1f23",
		ContextBg:           p.BgPanel,
		LineNumber:          p.FgFaint,
		AddedLineNumberBg:   "#182620",
		RemovedLineNumberBg: "#271a1f",
	}
	p.Markdown = MarkdownPalette{
		Text:            p.Fg,
		Heading:         p.Purple,
		Link:            p.Blue,
		LinkText:        p.Cyan,
		Code:            p.Green,
		BlockQuote:      p.Amber,
		Emph:            p.Amber,
		Strong:          p.Red,
		HorizontalRule:  p.FgFaint,
		ListItem:        p.Fg,
		ListEnumeration: p.Cyan,
		Image:           p.Blue,
		ImageText:       p.Cyan,
		CodeBlock:       p.Fg,
	}
	p.Syntax = SyntaxPalette{
		Comment:     p.FgFaint,
		Keyword:     p.Purple,
		Function:    p.Blue,
		Variable:    p.Red,
		String:      p.Green,
		Number:      p.Amber,
		Type:        p.Yellow,
		Operator:    p.Cyan,
		Punctuation: p.Fg,
	}
	return p
}

// Light is a paper-on-charcoal inversion of the design palette for light
// terminals — same semantic roles, readable contrast.
//
// Sub-struct derivation rationale (plan 08c §1a):
//
// Diff:
//   - Added/Removed/Context reuse semantic Green, Red, Cyan.
//   - HighlightAdded/Removed use brighter values (#4db380 / #f52a65) so intra-line
//     spans pop on light backgrounds; these match opencode light diff highlights.
//   - AddedBg/RemovedBg are pale tints (#d5e5d5 / #f7d8db) — mirrors the opencode
//     light diff palette for near-pixel parity on light terminals.
//   - ContextBg = BgPanel (light gray).
//   - LineNumber = FgFaint (mid-gray on light bg, readable but subtle).
//   - AddedLineNumberBg/RemovedLineNumberBg are slightly darker tints of AddedBg/RemovedBg.
//
// Markdown:
//   - Same semantic role assignments as Default, colors adjusted for light contrast:
//     Heading=Purple, Code=Green, BlockQuote/Emph=Amber, Strong=Red, Link=Blue, LinkText=Cyan.
//
// Syntax:
//   - Comment=FgFaint (mid gray), others follow same semantic roles, all light-mode values.
func Light() Palette {
	p := Palette{
		Bg: "#f5f6f7", BgPanel: "#eceef0", BgElev: "#e4e7ea", BgSel: "#d8dde2",
		Border: "#c4cad0", BorderSoft: "#d8dde2", BorderActive: "#8b929a",
		Fg: "#1c2126", FgDim: "#5a626b", FgFaint: "#8b929a", FgGhost: "#b4bbc2",
		Blue: "#2f6fb0", Green: "#3f8c2f", Red: "#c0392b", Amber: "#b06f1a",
		Purple: "#7a4fb0", Cyan: "#2f8a99", Yellow: "#9a8420",
		SelBg: "#b06f1a", SelFg: "#fdf6ec",
	}
	p.Diff = DiffPalette{
		Added:               p.Green,
		Removed:             p.Red,
		Context:             p.Cyan,
		HunkHeader:          p.Cyan,
		HighlightAdded:      "#4db380",
		HighlightRemoved:    "#f52a65",
		AddedBg:             "#d5e5d5",
		RemovedBg:           "#f7d8db",
		ContextBg:           p.BgPanel,
		LineNumber:          p.FgFaint,
		AddedLineNumberBg:   "#c5d5c5",
		RemovedLineNumberBg: "#e7c8cb",
	}
	p.Markdown = MarkdownPalette{
		Text:            p.Fg,
		Heading:         p.Purple,
		Link:            p.Blue,
		LinkText:        p.Cyan,
		Code:            p.Green,
		BlockQuote:      p.Amber,
		Emph:            p.Amber,
		Strong:          p.Red,
		HorizontalRule:  p.FgFaint,
		ListItem:        p.Fg,
		ListEnumeration: p.Cyan,
		Image:           p.Blue,
		ImageText:       p.Cyan,
		CodeBlock:       p.Fg,
	}
	p.Syntax = SyntaxPalette{
		Comment:     p.FgFaint,
		Keyword:     p.Purple,
		Function:    p.Blue,
		Variable:    p.Red,
		String:      p.Green,
		Number:      p.Amber,
		Type:        p.Yellow,
		Operator:    p.Cyan,
		Punctuation: p.Fg,
	}
	return p
}

// Mono is a grayscale theme: neutral semantics, errors kept bright so they read.
//
// Sub-struct derivation rationale (plan 08c §1a):
//
// Diff:
//   - In a grayscale palette there are no true red/green hues, so diff fg colors
//     use the lighter/darker grays to create contrast: Added = light gray (Fg),
//     Removed = near-white (Red = #ededed), Context = FgDim.
//   - HighlightAdded/Removed use the brightest and dimmest available grays to
//     provide intra-line contrast without hue.
//   - AddedBg/RemovedBg are very subtle gray steps (#1e2a1e is the closest Mono
//     can get to a tint; on true mono we use stepped neutrals #1a2020 / #201a1a).
//   - ContextBg = BgPanel.
//   - LineNumber = FgFaint (same gutter convention).
//   - AddedLineNumberBg/RemovedLineNumberBg are one step darker than AddedBg/RemovedBg.
//
// Markdown / Syntax:
//   - In a monochrome palette all "semantic color" fields collapse to grays from
//     the Fg/FgDim/FgFaint ramp. Bold/italic rendering style carries the
//     visual weight instead of hue; the sub-struct fields provide the fg grays
//     that M4/M5 can compose with weight attributes.
func Mono() Palette {
	p := Palette{
		Bg: "#0e0e0e", BgPanel: "#161616", BgElev: "#1c1c1c", BgSel: "#242424",
		Border: "#303030", BorderSoft: "#262626", BorderActive: "#4a4a4a",
		Fg: "#e4e4e4", FgDim: "#9a9a9a", FgFaint: "#5e5e5e", FgGhost: "#3a3a3a",
		Blue: "#cfcfcf", Green: "#bdbdbd", Red: "#ededed", Amber: "#d0d0d0",
		Purple: "#c4c4c4", Cyan: "#cfcfcf", Yellow: "#bdbdbd",
		SelBg: "#d0d0d0", SelFg: "#0e0e0e",
	}
	p.Diff = DiffPalette{
		Added:               p.Fg,
		Removed:             p.Red,
		Context:             p.FgDim,
		HunkHeader:          p.FgDim,
		HighlightAdded:      p.Fg,
		HighlightRemoved:    p.Red,
		AddedBg:             "#1a2020",
		RemovedBg:           "#201a1a",
		ContextBg:           p.BgPanel,
		LineNumber:          p.FgFaint,
		AddedLineNumberBg:   "#151c1c",
		RemovedLineNumberBg: "#1c1515",
	}
	p.Markdown = MarkdownPalette{
		Text:            p.Fg,
		Heading:         p.Fg,
		Link:            p.FgDim,
		LinkText:        p.FgDim,
		Code:            p.Green,
		BlockQuote:      p.FgDim,
		Emph:            p.FgDim,
		Strong:          p.Fg,
		HorizontalRule:  p.FgFaint,
		ListItem:        p.Fg,
		ListEnumeration: p.FgDim,
		Image:           p.FgDim,
		ImageText:       p.FgDim,
		CodeBlock:       p.Fg,
	}
	p.Syntax = SyntaxPalette{
		Comment:     p.FgFaint,
		Keyword:     p.Fg,
		Function:    p.FgDim,
		Variable:    p.FgDim,
		String:      p.Green,
		Number:      p.Amber,
		Type:        p.FgDim,
		Operator:    p.FgDim,
		Punctuation: p.Fg,
	}
	return p
}

// Named pairs a theme name with its palette (the theme picker's list).
// The full registry (including 33 embedded opencode themes) is built by
// loader.go; Palettes() / ByName() / PalettesForMode() / ByNameForMode()
// all live there so that embed.FS and JSON parsing stay in one file.
type Named struct {
	Name    string
	Palette Palette
}

// Styles are the canonical reusable Lipgloss styles the design defines. Screen
// renderers compose these rather than re-deriving colors.
type Styles struct {
	P Palette

	// Base is primary text on the terminal background.
	Base lipgloss.Style
	// Dim / Faint / Ghost are the secondary text tiers.
	Dim   lipgloss.Style
	Faint lipgloss.Style
	Ghost lipgloss.Style
	// Section is a purple bold header (markdown h3, modal labels, table headers).
	Section lipgloss.Style
	// Selection is the canonical cursor row: full-width amber bar, dark bold text.
	Selection lipgloss.Style
	// ModeChip is the status-bar mode chip: accent bg, dark bold text.
	ModeChip lipgloss.Style
	// Accent is accent-colored text (prompt accent, agent mode).
	Accent lipgloss.Style
}

// New builds the Styles for a palette.
func New(p Palette) Styles {
	return Styles{
		P:         p,
		Base:      lipgloss.NewStyle().Foreground(p.Fg),
		Dim:       lipgloss.NewStyle().Foreground(p.FgDim),
		Faint:     lipgloss.NewStyle().Foreground(p.FgFaint),
		Ghost:     lipgloss.NewStyle().Foreground(p.FgGhost),
		Section:   lipgloss.NewStyle().Foreground(p.Purple).Bold(true),
		Selection: lipgloss.NewStyle().Background(p.SelBg).Foreground(p.SelFg).Bold(true),
		ModeChip:  lipgloss.NewStyle().Background(p.Accent()).Foreground(p.SelFg).Bold(true).Padding(0, 1),
		Accent:    lipgloss.NewStyle().Foreground(p.Accent()),
	}
}

// Surface returns a style with bg set as the background color and the palette's
// primary foreground — intended for panel/elevated surfaces that must own their
// own background. Lipgloss does NOT inherit a parent Background into joined
// sub-strings, so every block that can be shorter than the full terminal width
// must pin its own bg; otherwise transparent cells bleed through on light
// terminals. Use BgPanel for composer/autocomplete panels, BgElev for modals,
// BgSel for hover rows, or Bg for the base surface.
func (s Styles) Surface(bg Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(bg).Foreground(s.P.Fg)
}

// DefaultStyles returns the Styles for the default palette.
func DefaultStyles() Styles { return New(Default()) }
