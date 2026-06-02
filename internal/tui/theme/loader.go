// Package theme – loader.go
//
// JSON theme loader: parses opencode's opencode.ai/theme.json schema into a
// Palette for a given mode (dark|light), embeds all 33 bundled theme files,
// and extends the registry (Palettes / ByName) with the embedded set so that
// a Forge user can pick gruvbox, tokyonight, catppuccin, etc., exactly as in
// opencode (plan 08c §1b).
//
// Resolution semantics match opencode's context/theme.tsx resolveColor():
//
//   - A JSON string starting with '#' is a literal hex color.
//   - A JSON string that is exactly "transparent" or "none" maps to the resolved
//     `background` token for surface/diff backgrounds; for foreground-only tokens
//     it falls back to the Default()/Light() baseline value so renderers never
//     see an empty lipgloss.Color.
//   - A JSON string that is neither a '#'-literal nor "transparent"/"none" is a
//     reference: first looked up in the `defs` map, then in the `theme` map (for
//     cross-token refs like "textMuted", "diffContext").  Circular refs are caught
//     by a chain-depth guard.
//   - A JSON object with "dark"/"light" keys is resolved for the requested mode.
//   - An 8-digit #rrggbbaa hex is accepted: the alpha byte is stripped and the
//     RGB portion is used directly (Lipgloss and most terminals ignore per-cell
//     alpha anyway; stripping prevents malformed ANSI output).
//   - Any token omitted from the JSON falls back to the corresponding field from
//     Default() (dark) / Light() (light) so nothing is ever zero-value.
//
// Dark/light threading:
//
//	PalettesForMode(dark bool) is the primary registry accessor used by model.go
//	so that embedded theme tokens resolve in the correct mode.  The legacy
//	Palettes() wrapper calls PalettesForMode(true) to keep all existing callers
//	unchanged; the theme picker (modal.go) and other static callers already show
//	themes by name so the dark/light split is transparent to them.
//
// config.theme wiring:
//
//	model.go's applyThemeByName already iterates Palettes() and looks up by name.
//	After this change Palettes() includes all 33 embedded themes, so a KV-stored
//	or config-file theme name like "gruvbox" resolves automatically through the
//	existing code path (model.go:233, kv.go Theme field).  No additional wiring
//	is required; the embedded name catalogue is the contract.
package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//go:embed themes/*.json
var embeddedThemes embed.FS

// themeJSON is the on-disk shape of an opencode.ai/theme.json file.
// Only the fields the loader needs are decoded; $schema is ignored.
type themeJSON struct {
	Defs  map[string]json.RawMessage `json:"defs"`
	Theme map[string]json.RawMessage `json:"theme"`
}

// colorVariant is the decoded form of a {"dark":…,"light":…} leaf.
type colorVariant struct {
	Dark  json.RawMessage `json:"dark"`
	Light json.RawMessage `json:"light"`
}

// resolveChainMax caps cross-token reference depth to detect circular refs.
const resolveChainMax = 16

// loadEmbeddedThemesForMode returns Named palettes for the given mode.
func loadEmbeddedThemesForMode(dark bool) []Named {
	entries, err := embeddedThemes.ReadDir("themes")
	if err != nil {
		return nil
	}
	out := make([]Named, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := embeddedThemes.ReadFile(filepath.Join("themes", e.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		dp, err := ParseThemeJSON(data, dark)
		if err != nil {
			continue
		}
		out = append(out, Named{Name: name, Palette: dp})
	}
	return out
}

// ParseThemeJSON parses the opencode.ai/theme.json bytes into a Forge Palette.
// dark=true resolves the "dark" variant of each token; dark=false resolves
// "light".  Any token absent from the JSON falls back to Default()/Light().
func ParseThemeJSON(data []byte, dark bool) (Palette, error) {
	var tj themeJSON
	if err := json.Unmarshal(data, &tj); err != nil {
		return Palette{}, fmt.Errorf("theme: unmarshal: %w", err)
	}

	// Baseline palette — provides fallback values for any token the JSON omits,
	// and serves as the fallback for "transparent"/"none" foreground tokens.
	base := Default()
	if !dark {
		base = Light()
	}
	mode := "dark"
	if !dark {
		mode = "light"
	}

	// resolve resolves a raw JSON value to a hex color string for mode.
	// chain tracks visited ref names to detect cycles.
	var resolve func(raw json.RawMessage, chain []string) (string, error)
	resolve = func(raw json.RawMessage, chain []string) (string, error) {
		if len(chain) > resolveChainMax {
			return "", fmt.Errorf("theme: circular color reference: %v", chain)
		}
		// Strip whitespace for type inspection.
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			return "", fmt.Errorf("theme: empty color value")
		}

		// Object with "dark"/"light" split.
		if strings.HasPrefix(trimmed, "{") {
			var variant colorVariant
			if err := json.Unmarshal(raw, &variant); err != nil {
				return "", fmt.Errorf("theme: decode variant: %w", err)
			}
			var modeRaw json.RawMessage
			if mode == "dark" {
				modeRaw = variant.Dark
			} else {
				modeRaw = variant.Light
			}
			return resolve(modeRaw, chain)
		}

		// Unquote string values.
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("theme: decode string: %w", err)
		}

		// Transparent / none → signal caller to substitute fallback.
		if s == "transparent" || s == "none" {
			return "transparent", nil
		}

		// Literal hex color (3-, 6-, or 8-digit).
		if strings.HasPrefix(s, "#") {
			return normalizeHex(s), nil
		}

		// Reference: look up in defs first, then in theme map.
		if def, ok := tj.Defs[s]; ok {
			return resolve(def, append(chain, s))
		}
		if thRef, ok := tj.Theme[s]; ok {
			return resolve(thRef, append(chain, s))
		}
		return "", fmt.Errorf("theme: ref %q not found in defs or theme", s)
	}

	// resolveToken resolves a named token from the theme map.
	// Returns ("", false) if the token is absent or errors during resolution.
	resolveToken := func(token string) (string, bool) {
		raw, ok := tj.Theme[token]
		if !ok {
			return "", false
		}
		s, err := resolve(raw, nil)
		if err != nil {
			return "", false
		}
		return s, true
	}

	// get returns a lipgloss.Color string for a token.
	// If the token is absent, errors, or resolves to "transparent", the fallback
	// is used instead.  fallback must always be a non-empty color string.
	get := func(token string, fallback string) string {
		s, ok := resolveToken(token)
		if !ok || s == "transparent" {
			return fallback
		}
		return s
	}

	// getDiffBg resolves a diff background token, substituting the bgPanel
	// surface for "transparent".  Transparent diff backgrounds mean the theme
	// relies on terminal transparency; since Forge always paints we use the
	// nearest panel surface so no cell is left empty.
	getDiffBg := func(token, bgPanel, fallback string) string {
		s, ok := resolveToken(token)
		if !ok {
			return fallback
		}
		if s == "transparent" {
			return bgPanel
		}
		return s
	}

	// --- Surface tokens (resolve first; needed as fallbacks below) -----------
	bg := get("background", string(base.Bg))
	// Transparent background: fall back through the surface hierarchy.
	if raw, ok := tj.Theme["background"]; ok {
		if s, err := resolve(raw, nil); err == nil && s == "transparent" {
			bg = string(base.Bg)
		}
	}

	bgPanel := get("backgroundPanel", string(base.BgPanel))
	if raw, ok := tj.Theme["backgroundPanel"]; ok {
		if s, err := resolve(raw, nil); err == nil && s == "transparent" {
			bgPanel = bg
		}
	}

	bgElev := get("backgroundElement", string(base.BgElev))
	if raw, ok := tj.Theme["backgroundElement"]; ok {
		if s, err := resolve(raw, nil); err == nil && s == "transparent" {
			bgElev = bgPanel
		}
	}

	// BgSel reuses backgroundElement (the hovered row surface).
	bgSel := bgElev

	// --- Assemble Palette ----------------------------------------------------
	p := Palette{
		Bg:      lipgloss.Color(bg),
		BgPanel: lipgloss.Color(bgPanel),
		BgElev:  lipgloss.Color(bgElev),
		BgSel:   lipgloss.Color(bgSel),

		Fg:      lipgloss.Color(get("text", string(base.Fg))),
		FgDim:   lipgloss.Color(get("textMuted", string(base.FgDim))),
		FgFaint: base.FgFaint, // no direct opencode token; keep baseline
		FgGhost: base.FgGhost, // no direct opencode token; keep baseline

		Blue:   lipgloss.Color(get("secondary", string(base.Blue))),
		Purple: lipgloss.Color(get("accent", string(base.Purple))),
		Red:    lipgloss.Color(get("error", string(base.Red))),
		Amber:  lipgloss.Color(get("warning", string(base.Amber))),
		Green:  lipgloss.Color(get("success", string(base.Green))),
		Cyan:   lipgloss.Color(get("info", string(base.Cyan))),
		Yellow: base.Yellow, // no opencode token; baseline scaling

		Border:       lipgloss.Color(get("border", string(base.Border))),
		BorderSoft:   lipgloss.Color(get("borderSubtle", string(base.BorderSoft))),
		BorderActive: lipgloss.Color(get("borderActive", string(base.BorderActive))),

		// SelBg: opencode uses "primary" as the selection accent color.
		// SelFg stays near-black from baseline (it is always readable against
		// the bright primary color, per the original design intent).
		SelBg: lipgloss.Color(get("primary", string(base.SelBg))),
		SelFg: base.SelFg,
	}

	// --- Diff sub-struct ------------------------------------------------------
	p.Diff = DiffPalette{
		Added:               lipgloss.Color(get("diffAdded", string(base.Diff.Added))),
		Removed:             lipgloss.Color(get("diffRemoved", string(base.Diff.Removed))),
		Context:             lipgloss.Color(get("diffContext", string(base.Diff.Context))),
		HunkHeader:          lipgloss.Color(get("diffHunkHeader", string(base.Diff.HunkHeader))),
		HighlightAdded:      lipgloss.Color(get("diffHighlightAdded", string(base.Diff.HighlightAdded))),
		HighlightRemoved:    lipgloss.Color(get("diffHighlightRemoved", string(base.Diff.HighlightRemoved))),
		AddedBg:             lipgloss.Color(getDiffBg("diffAddedBg", bgPanel, string(base.Diff.AddedBg))),
		RemovedBg:           lipgloss.Color(getDiffBg("diffRemovedBg", bgPanel, string(base.Diff.RemovedBg))),
		ContextBg:           lipgloss.Color(getDiffBg("diffContextBg", bgPanel, string(base.Diff.ContextBg))),
		LineNumber:          lipgloss.Color(get("diffLineNumber", string(base.Diff.LineNumber))),
		AddedLineNumberBg:   lipgloss.Color(getDiffBg("diffAddedLineNumberBg", bgPanel, string(base.Diff.AddedLineNumberBg))),
		RemovedLineNumberBg: lipgloss.Color(getDiffBg("diffRemovedLineNumberBg", bgPanel, string(base.Diff.RemovedLineNumberBg))),
	}

	// --- Markdown sub-struct --------------------------------------------------
	p.Markdown = MarkdownPalette{
		Text:            lipgloss.Color(get("markdownText", string(base.Markdown.Text))),
		Heading:         lipgloss.Color(get("markdownHeading", string(base.Markdown.Heading))),
		Link:            lipgloss.Color(get("markdownLink", string(base.Markdown.Link))),
		LinkText:        lipgloss.Color(get("markdownLinkText", string(base.Markdown.LinkText))),
		Code:            lipgloss.Color(get("markdownCode", string(base.Markdown.Code))),
		BlockQuote:      lipgloss.Color(get("markdownBlockQuote", string(base.Markdown.BlockQuote))),
		Emph:            lipgloss.Color(get("markdownEmph", string(base.Markdown.Emph))),
		Strong:          lipgloss.Color(get("markdownStrong", string(base.Markdown.Strong))),
		HorizontalRule:  lipgloss.Color(get("markdownHorizontalRule", string(base.Markdown.HorizontalRule))),
		ListItem:        lipgloss.Color(get("markdownListItem", string(base.Markdown.ListItem))),
		ListEnumeration: lipgloss.Color(get("markdownListEnumeration", string(base.Markdown.ListEnumeration))),
		Image:           lipgloss.Color(get("markdownImage", string(base.Markdown.Image))),
		ImageText:       lipgloss.Color(get("markdownImageText", string(base.Markdown.ImageText))),
		CodeBlock:       lipgloss.Color(get("markdownCodeBlock", string(base.Markdown.CodeBlock))),
	}

	// --- Syntax sub-struct ----------------------------------------------------
	p.Syntax = SyntaxPalette{
		Comment:     lipgloss.Color(get("syntaxComment", string(base.Syntax.Comment))),
		Keyword:     lipgloss.Color(get("syntaxKeyword", string(base.Syntax.Keyword))),
		Function:    lipgloss.Color(get("syntaxFunction", string(base.Syntax.Function))),
		Variable:    lipgloss.Color(get("syntaxVariable", string(base.Syntax.Variable))),
		String:      lipgloss.Color(get("syntaxString", string(base.Syntax.String))),
		Number:      lipgloss.Color(get("syntaxNumber", string(base.Syntax.Number))),
		Type:        lipgloss.Color(get("syntaxType", string(base.Syntax.Type))),
		Operator:    lipgloss.Color(get("syntaxOperator", string(base.Syntax.Operator))),
		Punctuation: lipgloss.Color(get("syntaxPunctuation", string(base.Syntax.Punctuation))),
	}

	return p, nil
}

// normalizeHex strips the alpha byte from an 8-digit #rrggbbaa hex literal and
// returns the canonical 6-digit form.  Lipgloss and most terminal emulators
// accept only RGB; including the alpha byte produces malformed ANSI output.
// 3-digit shorthand (#rgb) is returned unchanged — Lipgloss handles it.
func normalizeHex(h string) string {
	if len(h) == 9 && h[0] == '#' { // #rrggbbaa → #rrggbb
		return h[:7]
	}
	return h
}

// PalettesForMode returns the full ordered theme registry resolved for the
// requested mode: the 3 native Forge themes first, then the 33 embedded
// opencode themes in file-system (alphabetical) order — 36 total.
//
// Call this from paths that know the terminal mode (e.g., model.go after
// lipgloss.HasDarkBackground() has been evaluated).
func PalettesForMode(dark bool) []Named {
	return append(nativeThemes(), loadEmbeddedThemesForMode(dark)...)
}

// nativeThemes returns the 3 hand-coded Forge palettes.
// The native set is mode-independent — Default is always the dark variant,
// Light always the light variant, Mono always neutral.
func nativeThemes() []Named {
	return []Named{
		{"forge-dark", Default()},
		{"forge-light", Light()},
		{"monochrome", Mono()},
	}
}

// Palettes returns the registry resolved for dark mode.  This preserves the
// existing call-signature used by modal.go, agents.go, and model.go; all 36
// themes (3 native + 33 embedded) are present.
//
// For mode-aware resolution (where embedded theme tokens must reflect the
// terminal's dark/light state) use PalettesForMode.
func Palettes() []Named {
	return PalettesForMode(true)
}

// ByNameForMode looks up a palette by name for a specific dark/light mode.
// Returns (Default()/Light(), false) when the name is unknown.
func ByNameForMode(name string, dark bool) (Palette, bool) {
	for _, n := range PalettesForMode(dark) {
		if n.Name == name {
			return n.Palette, true
		}
	}
	if dark {
		return Default(), false
	}
	return Light(), false
}

// ByName returns a palette by theme name resolved for dark mode (back-compat).
// Returns (Default(), false) when unknown.
func ByName(name string) (Palette, bool) {
	return ByNameForMode(name, true)
}
