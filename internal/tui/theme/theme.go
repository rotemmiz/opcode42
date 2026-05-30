// Package theme is the Forge TUI's color system, lifted verbatim from the
// design handoff's tokens (design/tui/styles.css :root). It exposes a Palette of
// truecolor values and the canonical Lipgloss styles the design defines
// (selection bar, mode chip, semantic text). Lipgloss degrades truecolor to the
// terminal's best available palette automatically.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette holds the design tokens. Names mirror the CSS custom properties.
type Palette struct {
	// Surfaces (neutral charcoal).
	Bg         lipgloss.Color // terminal background
	BgPanel    lipgloss.Color // collapsible panels, composer, autocomplete
	BgElev     lipgloss.Color // modals / popovers
	BgSel      lipgloss.Color // row hover
	Border     lipgloss.Color // table/panel/modal borders
	BorderSoft lipgloss.Color // hairline dividers, sidebar edge

	// Text.
	Fg      lipgloss.Color // primary
	FgDim   lipgloss.Color // secondary, tool-call lines
	FgFaint lipgloss.Color // hints, line numbers, metadata
	FgGhost lipgloss.Color // placeholders, disabled, diff gutters

	// Semantic colors (meanings fixed by the design — do not repurpose).
	Blue   lipgloss.Color // agent mode, prompt accent, function names
	Green  lipgloss.Color // added diff, success, paths, strings
	Red    lipgloss.Color // removed diff, errors, blocked
	Amber  lipgloss.Color // selection highlight, in-progress, thinking
	Purple lipgloss.Color // section headers, keywords, table headers
	Cyan   lipgloss.Color // types, @mentions, links, hunk markers
	Yellow lipgloss.Color // reserve / rarely used

	// Selection bar (modal & table highlight): solid amber, near-black text.
	SelBg lipgloss.Color
	SelFg lipgloss.Color
}

// Accent is the UI accent color (the design aliases --accent to --blue).
func (p Palette) Accent() lipgloss.Color { return p.Blue }

// Default returns the design's charcoal palette.
func Default() Palette {
	return Palette{
		Bg: "#15171a", BgPanel: "#1c1f23", BgElev: "#20242a", BgSel: "#262b31",
		Border: "#2c3137", BorderSoft: "#23272c",
		Fg: "#d6dade", FgDim: "#8b929a", FgFaint: "#585f67", FgGhost: "#3a4047",
		Blue: "#6fa8dc", Green: "#8cc265", Red: "#e0606e", Amber: "#d99a4e",
		Purple: "#b08cd4", Cyan: "#5fb3c4", Yellow: "#d6c370",
		SelBg: "#d99a4e", SelFg: "#1a1207",
	}
}

// Light is a paper-on-charcoal inversion of the design palette for light
// terminals — same semantic roles, readable contrast.
func Light() Palette {
	return Palette{
		Bg: "#f5f6f7", BgPanel: "#eceef0", BgElev: "#e4e7ea", BgSel: "#d8dde2",
		Border: "#c4cad0", BorderSoft: "#d8dde2",
		Fg: "#1c2126", FgDim: "#5a626b", FgFaint: "#8b929a", FgGhost: "#b4bbc2",
		Blue: "#2f6fb0", Green: "#3f8c2f", Red: "#c0392b", Amber: "#b06f1a",
		Purple: "#7a4fb0", Cyan: "#2f8a99", Yellow: "#9a8420",
		SelBg: "#b06f1a", SelFg: "#fdf6ec",
	}
}

// Mono is a grayscale theme: neutral semantics, errors kept bright so they read.
func Mono() Palette {
	return Palette{
		Bg: "#0e0e0e", BgPanel: "#161616", BgElev: "#1c1c1c", BgSel: "#242424",
		Border: "#303030", BorderSoft: "#262626",
		Fg: "#e4e4e4", FgDim: "#9a9a9a", FgFaint: "#5e5e5e", FgGhost: "#3a3a3a",
		Blue: "#cfcfcf", Green: "#bdbdbd", Red: "#ededed", Amber: "#d0d0d0",
		Purple: "#c4c4c4", Cyan: "#cfcfcf", Yellow: "#bdbdbd",
		SelBg: "#d0d0d0", SelFg: "#0e0e0e",
	}
}

// Named pairs a theme name with its palette (the theme picker's list).
type Named struct {
	Name    string
	Palette Palette
}

// Palettes is the ordered theme registry; the first is the default.
func Palettes() []Named {
	return []Named{
		{"forge-dark", Default()},
		{"forge-light", Light()},
		{"monochrome", Mono()},
	}
}

// ByName returns a palette by theme name (Default + false when unknown).
func ByName(name string) (Palette, bool) {
	for _, n := range Palettes() {
		if n.Name == name {
			return n.Palette, true
		}
	}
	return Default(), false
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

// DefaultStyles returns the Styles for the default palette.
func DefaultStyles() Styles { return New(Default()) }
