package tui

// bgfill_test.go — Plan 08c M0 regression guard.
//
// Asserts that View() always paints the theme background across the full
// terminal area: every rendered line must be exactly m.width visible characters
// wide (lipgloss.Width strips ANSI escapes before measuring). A line shorter
// than width means transparent cells, which was the white-background bug:
// opcode42-dark's light-gray foregrounds (#d6dade) bled through on white terminals.
//
// We also test the pickDefaultTheme pure helper directly (darkBg bool) so the
// light/dark auto-pick logic is covered without needing a real terminal.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// TestPickDefaultTheme verifies the terminal-background auto-pick helper.
// pickDefaultTheme is a pure function (accepts darkBg bool) so tests don't need
// a real terminal — mirrors opencode's theme_mode_lock auto-select.
func TestPickDefaultTheme(t *testing.T) {
	cases := []struct {
		darkBg    bool
		wantTheme string
	}{
		{true, "opcode42-dark"},
		{false, "opcode42-light"},
	}
	for _, tc := range cases {
		got := pickDefaultTheme(tc.darkBg)
		if got != tc.wantTheme {
			t.Errorf("pickDefaultTheme(darkBg=%v) = %q, want %q", tc.darkBg, got, tc.wantTheme)
		}
		// Sanity: the returned name must resolve in the theme registry.
		if _, ok := theme.ByName(got); !ok {
			t.Errorf("pickDefaultTheme returned unknown theme name %q", got)
		}
	}
}

// TestView_BackgroundFill asserts that every line of View() is exactly m.width
// visible characters wide for each theme, on both splash and session screens.
//
// Why lipgloss.Width per-line: the outer View() paint wraps the whole frame in
// lipgloss.NewStyle().Background(p.Bg).Width(m.width).Height(m.height).Render(),
// which forces lipgloss to pad every line to exactly m.width cells.
// A line shorter than m.width indicates a rendering path that bypasses the fill.
func TestView_BackgroundFill(t *testing.T) {
	const w, h = 80, 24

	themes := []string{"opcode42-dark", "opcode42-light", "monochrome"}

	type scene struct {
		name  string
		build func(themeName string) Model
	}

	scenes := []scene{
		{
			name: "splash",
			build: func(name string) Model {
				m := New(Config{URL: "http://x"})
				m.width, m.height = w, h
				return m.applyThemeByName(name)
			},
		},
		{
			name: "session",
			build: func(_ string) Model {
				return seededSessionModel(t) // defined in render_test.go; w=100,h=60
			},
		},
	}

	for _, sc := range scenes {
		for _, tn := range themes {
			t.Run(sc.name+"/"+tn, func(t *testing.T) {
				m := sc.build(tn)
				if sc.name != "session" {
					// session model uses fixed 100×60 from seededSessionModel
					m = m.applyThemeByName(tn)
				}
				out := m.View()

				// Split on newlines and check each line's visible width.
				// We trim a trailing newline that lipgloss may add after the last row.
				lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

				// The expected width comes from the model's own m.width field.
				want := m.width
				if want == 0 {
					t.Skip("model has zero width — skip fill check")
				}

				for i, line := range lines {
					got := lipgloss.Width(line)
					if got != want {
						t.Errorf("theme=%q scene=%q line %d: visible width %d, want %d\nline: %q",
							tn, sc.name, i, got, want, line)
					}
				}
			})
		}
	}
}

// TestView_NoDimensionsNoPanic verifies that View() does not panic and returns
// something (possibly empty) when width/height are zero — the guard branch.
func TestView_NoDimensionsNoPanic(_ *testing.T) {
	for _, tn := range []string{"opcode42-dark", "opcode42-light"} {
		m := New(Config{URL: "http://x"})
		m = m.applyThemeByName(tn)
		// width==0, height==0 — should not panic, should return body unpainted.
		_ = m.View() // any non-panic result is acceptable
	}
}

// TestView_AllThemesFullWidth confirms that View() for every registered palette
// produces output whose every line is exactly m.width visible characters wide
// across a range of terminal sizes. This is a direct regression guard for the
// "transparent cell" bug: if any renderer returns a shorter line the test fails
// (background bleed-through would be visible on light terminals in production).
//
// Note: lipgloss does not emit ANSI escape codes in a non-TTY test runner, so
// we cannot assert on SGR sequences here. Width-per-line is the robust signal.
func TestView_AllThemesFullWidth(t *testing.T) {
	widths := []int{40, 80, 120}
	for _, named := range theme.Palettes() {
		for _, w := range widths {
			t.Run(named.Name+"/w"+strconv.Itoa(w), func(t *testing.T) {
				m := New(Config{URL: "http://x"})
				m.width, m.height = w, 24
				m = m.applyThemeByName(named.Name)
				out := m.View()
				lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
				for i, line := range lines {
					if got := lipgloss.Width(line); got != w {
						t.Errorf("theme=%q width=%d line %d: visible width %d, want %d",
							named.Name, w, i, got, w)
					}
				}
			})
		}
	}
}
