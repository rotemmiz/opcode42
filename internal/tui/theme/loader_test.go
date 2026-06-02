package theme

// loader_test.go — plan 08c M2 contract tests.
//
// Three test groups:
//
//  1. TestLoadAllEmbeddedThemes_NoZeroTokens: load every embedded theme in both
//     dark and light mode; assert that no token resolves to zero-value
//     (empty lipgloss.Color).  Reuses checkStructNoZero from theme_test.go.
//
//  2. TestOpencodeDarkGolden: golden test for the `opencode` theme (dark) —
//     asserts a representative set of tokens against values derived by hand
//     from the JSON defs in themes/opencode.json.
//
//  3. TestRegistry_36Themes: asserts the registry contains exactly 36 themes
//     (3 native + 33 embedded) and that gruvbox, tokyonight, and catppuccin
//     resolve via ByName.

import (
	"testing"
)

// TestLoadAllEmbeddedThemes_NoZeroTokens loads every embedded JSON theme in
// both dark and light mode and asserts that no sub-struct field (Diff,
// Markdown, Syntax) and no flat M1 field (BorderActive) is the empty-string
// zero value.  This is the M2 companion to TestPaletteNoZeroTokens in
// theme_test.go and guarantees that all 33 × 2 = 66 resolved palettes are
// fully populated.
func TestLoadAllEmbeddedThemes_NoZeroTokens(t *testing.T) {
	t.Helper()
	for _, dark := range []bool{true, false} {
		mode := "dark"
		if !dark {
			mode = "light"
		}
		embedded := loadEmbeddedThemesForMode(dark)
		if len(embedded) == 0 {
			t.Fatalf("no embedded themes loaded (mode=%s)", mode)
		}
		for _, named := range embedded {
			p := named.Palette
			t.Run(named.Name+"/"+mode, func(t *testing.T) {
				// BorderActive — flat field that must be set.
				if p.BorderActive == "" {
					t.Error("BorderActive is zero-value (empty string)")
				}
				// Sub-structs.
				checkStructNoZero(t, "Diff", p.Diff)
				checkStructNoZero(t, "Markdown", p.Markdown)
				checkStructNoZero(t, "Syntax", p.Syntax)
			})
		}
	}
}

// TestOpencodeDarkGolden asserts a representative set of tokens from the
// opencode theme (dark mode) against values derived by hand from
// themes/opencode.json.
//
// Selected tokens and their expected resolved values:
//
//	primary       → defs.darkStep9   = #fab283
//	secondary     → defs.darkSecondary = #5c9cf5
//	background    → defs.darkStep1   = #0a0a0a
//	backgroundPanel → defs.darkStep2 = #141414
//	text          → defs.darkStep12  = #eeeeee
//	textMuted     → defs.darkStep11  = #808080
//	diffAdded     → literal          = #4fd6be
//	diffAddedBg   → literal          = #20303b
//	syntaxKeyword → defs.darkAccent  = #9d7cd8
//	markdownHeading → defs.darkAccent = #9d7cd8
func TestOpencodeDarkGolden(t *testing.T) {
	data, err := embeddedThemes.ReadFile("themes/opencode.json")
	if err != nil {
		t.Fatalf("could not read opencode.json: %v", err)
	}
	p, err := ParseThemeJSON(data, true)
	if err != nil {
		t.Fatalf("ParseThemeJSON(opencode, dark): %v", err)
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		// primary → SelBg (loader maps primary → SelBg)
		{"SelBg (primary)", string(p.SelBg), "#fab283"},
		// secondary → Blue
		{"Blue (secondary)", string(p.Blue), "#5c9cf5"},
		// background → Bg
		{"Bg", string(p.Bg), "#0a0a0a"},
		// backgroundPanel → BgPanel
		{"BgPanel", string(p.BgPanel), "#141414"},
		// text → Fg
		{"Fg", string(p.Fg), "#eeeeee"},
		// textMuted → FgDim
		{"FgDim", string(p.FgDim), "#808080"},
		// diffAdded (literal in JSON)
		{"Diff.Added", string(p.Diff.Added), "#4fd6be"},
		// diffAddedBg (literal in JSON)
		{"Diff.AddedBg", string(p.Diff.AddedBg), "#20303b"},
		// syntaxKeyword → defs.darkAccent = #9d7cd8
		{"Syntax.Keyword", string(p.Syntax.Keyword), "#9d7cd8"},
		// markdownHeading → defs.darkAccent = #9d7cd8
		{"Markdown.Heading", string(p.Markdown.Heading), "#9d7cd8"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("got %q, want %q", c.got, c.want)
			}
		})
	}
}

// TestRegistry_36Themes asserts:
//   - Palettes() returns exactly 36 entries (3 native + 33 embedded).
//   - gruvbox, tokyonight, and catppuccin resolve via ByName (ok=true).
//   - All 36 names are distinct (no duplicates).
func TestRegistry_36Themes(t *testing.T) {
	ps := Palettes()
	const wantCount = 36
	if len(ps) != wantCount {
		t.Errorf("Palettes() returned %d themes, want %d", len(ps), wantCount)
	}

	// All names distinct.
	seen := make(map[string]bool, len(ps))
	for _, n := range ps {
		if seen[n.Name] {
			t.Errorf("duplicate theme name %q", n.Name)
		}
		seen[n.Name] = true
	}

	// Key embedded themes resolve via ByName.
	for _, name := range []string{"gruvbox", "tokyonight", "catppuccin"} {
		if _, ok := ByName(name); !ok {
			t.Errorf("ByName(%q) returned ok=false", name)
		}
	}
}

// TestPalettesForMode_BothModes verifies that PalettesForMode returns 36 themes
// in both dark and light mode, and that the same named palette resolves to
// different colors between modes (gruvbox dark bg ≠ gruvbox light bg).
func TestPalettesForMode_BothModes(t *testing.T) {
	dark := PalettesForMode(true)
	light := PalettesForMode(false)

	if len(dark) != 36 {
		t.Errorf("dark registry: got %d, want 36", len(dark))
	}
	if len(light) != 36 {
		t.Errorf("light registry: got %d, want 36", len(light))
	}

	// Gruvbox dark bg (#282828) should differ from light bg (#fbf1c7).
	dp, ok1 := ByNameForMode("gruvbox", true)
	lp, ok2 := ByNameForMode("gruvbox", false)
	if !ok1 || !ok2 {
		t.Fatal("gruvbox must be present in both modes")
	}
	if dp.Bg == lp.Bg {
		t.Errorf("gruvbox dark.Bg (%q) should differ from light.Bg (%q)", dp.Bg, lp.Bg)
	}
}

// TestNormalizeHex covers the alpha-stripping helper.
func TestNormalizeHex(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"#aabbcc", "#aabbcc"},   // 6-digit unchanged
		{"#aabbccdd", "#aabbcc"}, // 8-digit: strip alpha
		{"#abc", "#abc"},         // 3-digit unchanged
		{"#rrggbb", "#rrggbb"},   // literal (non-hex digits but valid len)
	}
	for _, c := range cases {
		got := normalizeHex(c.in)
		if got != c.out {
			t.Errorf("normalizeHex(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// TestTransparentBackground_LucentOrng verifies that the lucent-orng theme
// (which uses "transparent" for background/backgroundPanel/backgroundElement)
// still produces non-empty Bg/BgPanel/BgElev by falling back to the baseline.
func TestTransparentBackground_LucentOrng(t *testing.T) {
	data, err := embeddedThemes.ReadFile("themes/lucent-orng.json")
	if err != nil {
		t.Fatalf("could not read lucent-orng.json: %v", err)
	}
	for _, dark := range []bool{true, false} {
		mode := "dark"
		if !dark {
			mode = "light"
		}
		p, err := ParseThemeJSON(data, dark)
		if err != nil {
			t.Fatalf("ParseThemeJSON(lucent-orng, %s): %v", mode, err)
		}
		if p.Bg == "" {
			t.Errorf("lucent-orng/%s: Bg is empty", mode)
		}
		if p.BgPanel == "" {
			t.Errorf("lucent-orng/%s: BgPanel is empty", mode)
		}
		if p.BgElev == "" {
			t.Errorf("lucent-orng/%s: BgElev is empty", mode)
		}
		// Diff bg tokens that are transparent should also be non-empty.
		if p.Diff.AddedBg == "" {
			t.Errorf("lucent-orng/%s: Diff.AddedBg is empty", mode)
		}
	}
}

// TestBareStringToken verifies that bare string values (neither a dark/light
// object nor a "#..." literal) are resolved through defs/theme cross-refs.
// aura.json uses "purple" as a bare defs ref for "primary", and
// lucent-orng.json uses "textMuted" (a theme token ref) for "diffLineNumber".
func TestBareStringToken(t *testing.T) {
	// aura: primary = "purple" → defs.purple = "#a277ff"
	auraData, err := embeddedThemes.ReadFile("themes/aura.json")
	if err != nil {
		t.Fatalf("read aura.json: %v", err)
	}
	aura, err := ParseThemeJSON(auraData, true)
	if err != nil {
		t.Fatalf("ParseThemeJSON(aura, dark): %v", err)
	}
	// primary maps to SelBg in the loader.
	if string(aura.SelBg) != "#a277ff" {
		t.Errorf("aura SelBg (primary→purple): got %q, want #a277ff", aura.SelBg)
	}

	// lucent-orng: diffLineNumber = "textMuted" (cross-theme ref)
	// textMuted → defs.darkStep11 = "#808080" (dark mode)
	lucentData, err := embeddedThemes.ReadFile("themes/lucent-orng.json")
	if err != nil {
		t.Fatalf("read lucent-orng.json: %v", err)
	}
	lucent, err := ParseThemeJSON(lucentData, true)
	if err != nil {
		t.Fatalf("ParseThemeJSON(lucent-orng, dark): %v", err)
	}
	if string(lucent.Diff.LineNumber) != "#808080" {
		t.Errorf("lucent-orng Diff.LineNumber (textMuted→darkStep11): got %q, want #808080", lucent.Diff.LineNumber)
	}
}
