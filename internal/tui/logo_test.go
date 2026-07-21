package tui

// logo_test.go — Plan 08c M10: tests for block-pixel "opcode42" logo + shimmer sweep.
//
// Coverage:
//  1. logoFrame: pure and deterministic — same (frame, palette) always yields the
//     same output.
//  2. logoFrame: stable row count and row rune-width across frames.
//  3. logoFrame: different frames produce different per-column colors (shimmer moved).
//  4. Background anti-bleed: every logoFrame row, when wrapped with fill(), produces
//     a string that is full-width on both dark and light palettes.
//  5. animating() returns true on ScreenSplash, false on an idle ScreenSession.

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// ── logoFrame tests ───────────────────────────────────────────────────────────

// TestLogoFrameRowCount asserts that logoFrame always returns exactly 5 rows
// (the height of the block-pixel glyph matrix).
func TestLogoFrameRowCount(t *testing.T) {
	p := theme.Default()
	for _, frame := range []int{0, 1, 10, 45, 46, 100} {
		rows := logoFrame(frame, p)
		if len(rows) != 5 {
			t.Errorf("frame %d: want 5 rows, got %d", frame, len(rows))
		}
	}
}

// TestLogoFrameRowWidthStable asserts that every row in logoFrame has the same
// stripped rune count (logoWidth) regardless of frame or palette.
// We strip ANSI escapes (same helper used by spinner_test.go) before measuring.
func TestLogoFrameRowWidthStable(t *testing.T) {
	palettes := []struct {
		name string
		p    theme.Palette
	}{
		{"dark", theme.Default()},
		{"light", theme.Light()},
		{"mono", theme.Mono()},
	}
	for _, tc := range palettes {
		for _, frame := range []int{0, 23, 45} {
			rows := logoFrame(frame, tc.p)
			for i, row := range rows {
				plain := stripANSI(row)
				w := len([]rune(plain))
				if w != logoWidth {
					t.Errorf("palette=%s frame=%d row=%d: stripped width %d, want %d",
						tc.name, frame, i, w, logoWidth)
				}
			}
		}
	}
}

// TestLogoFrameDeterministic asserts that calling logoFrame twice with the same
// (frame, palette) produces identical output — it is a pure function.
func TestLogoFrameDeterministic(t *testing.T) {
	p := theme.Default()
	for _, frame := range []int{0, 7, 23, 45} {
		a := logoFrame(frame, p)
		b := logoFrame(frame, p)
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("frame %d row %d not deterministic: got different output on repeat calls", frame, i)
			}
		}
	}
}

// TestLogoFramesDiffer asserts that different animation frames produce different
// per-column colors, i.e. the shimmer actually moves.
//
// Note: lipgloss strips all ANSI colors when there is no TTY (go test runs without
// one), so we test via shimmerBrightness and columnColor directly rather than via
// the rendered string.  This is the same approach used by TestScannerFramesDifferViaColor
// in spinner_test.go.
func TestLogoFramesDiffer(t *testing.T) {
	p := theme.Default()

	// Frames 0 and 23 should differ for at least some columns.
	anyDiff := false
	for x := range logoWidth {
		b0 := shimmerBrightness(x, 0)
		b23 := shimmerBrightness(x, 23)
		if b0 != b23 {
			anyDiff = true
			break
		}
	}
	if !anyDiff {
		t.Error("shimmerBrightness is identical for frames 0 and 23: shimmer did not advance")
	}

	// Also verify columnColor produces different results for different frames on column 5.
	c0 := columnColor(5, 0, p)
	c23 := columnColor(5, 23, p)
	if c0 == c23 {
		t.Errorf("columnColor(5, frame) identical for frames 0 and 23: %s", c0)
	}
}

// TestLogoFrameGoldenFrame0 checks that frame 0 produces the correct glyph text.
// With rings=2 and offset=0.5 for ring 1, the second ring has phase=0.5 at frame 0
// (envelope=sin(π/2)=1), so the shimmer is bright at the mid-span even at frame 0.
// We verify only that the stripped text content equals the padded glyph rows —
// the shimmer only changes colors, never text.
func TestLogoFrameGoldenFrame0(t *testing.T) {
	p := theme.Default()
	rows := logoFrame(0, p)
	for i, row := range rows {
		plain := stripANSI(row)
		want := []rune(opcode42Glyph[i])
		// Pad to logoWidth.
		for len(want) < logoWidth {
			want = append(want, ' ')
		}
		got := []rune(plain)
		if string(got) != string(want) {
			t.Errorf("frame 0 row %d text mismatch:\n got:  %q\n want: %q", i, string(got), string(want))
		}
	}

	// At frame 0, ring 0 has phase=0 (floor brightness only) but ring 1 has
	// phase=0.5 (peak envelope).  The mid-span columns (near x=11) receive high
	// brightness from ring 1.  Verify the floor and the peak end of the spectrum.
	//
	// Floor: column 0 at frame 0.  Ring 0 contributes negligibly (phase=0, env=0);
	// ring 1's head is at 0.5*(18+5)=11.5 → dx=0-11.5=-11.5, far from soft shoulder.
	// Only breath floor (0.04) + small tail (dist>tailRange=13) → very low.
	b0 := shimmerBrightness(0, 0)
	if b0 > 0.15 {
		t.Errorf("frame 0 column 0 (far from head): expected low brightness (≤0.15), got %.4f", b0)
	}

	// Peak: column ~11 at frame 0 should be bright (ring 1 head is there).
	b11 := shimmerBrightness(11, 0)
	if b11 < 0.5 {
		t.Errorf("frame 0 column 11 (ring-1 head at phase=0.5): expected brightness >0.5, got %.4f", b11)
	}
}

// TestLogoFrameGoldenFramePeak checks frame 23 (≈ half-period, peak envelope):
// envelope = sin(23/46 * π) = sin(π/2) = 1, eased = 1.
// The shimmer head is near mid-span; brightness near the head should be high.
func TestLogoFrameGoldenFramePeak(t *testing.T) {
	// At frame 23 (phase = 23/46 = 0.5), envelope = 1.
	// Head position ≈ 0.5 * reach = 0.5 * (18 + 5) = 11.5 → near column 11.
	// shimmerBrightness(11, 23) should be well above the floor.
	b := shimmerBrightness(11, 23)
	if b < 0.5 {
		t.Errorf("frame 23 column 11 (near head): expected brightness > 0.5, got %.4f", b)
	}

	// Columns far from the head (e.g. column 0) should be below the peak.
	bFar := shimmerBrightness(0, 23)
	if bFar >= b {
		t.Errorf("frame 23: column 0 brightness (%.4f) should be < column 11 brightness (%.4f)", bFar, b)
	}
}

// ── Background anti-bleed test ─────────────────────────────────────────────────

// TestLogoRowsBackgroundFilled asserts that each logoFrame row, when wrapped with
// the same full-width fill() style used by viewSplash, produces a string whose
// visible (stripped) rune count equals the requested width.  This validates the
// Tier 0 invariant: no cell shorter than width (plan 08c §T golden-render assertion).
//
// We use width=40 (realistic terminal), both dark and light palettes.
func TestLogoRowsBackgroundFilled(t *testing.T) {
	palettes := []struct {
		name string
		p    theme.Palette
	}{
		{"dark", theme.Default()},
		{"light", theme.Light()},
	}
	w := 40
	for _, tc := range palettes {
		rows := logoFrame(5, tc.p)
		for i, row := range rows {
			filled := lipgloss.NewStyle().
				Background(tc.p.Bg).
				Width(w).
				Align(lipgloss.Center).
				Render(row)
			plain := stripANSI(filled)
			// Count only printable runes (not ANSI control chars that slipped through).
			var count int
			for _, r := range plain {
				if !unicode.IsControl(r) {
					count++
				}
			}
			if count != w {
				t.Errorf("palette=%s row=%d: filled row width %d, want %d (content: %q)",
					tc.name, i, count, w, plain)
			}
		}
	}
}

// TestLogoFrameTextUnchanged asserts that stripping ANSI from logoFrame rows
// yields only the glyph characters (no text corruption by the shimmer path).
func TestLogoFrameTextUnchanged(t *testing.T) {
	p := theme.Default()
	for _, frame := range []int{0, 15, 30, 45} {
		rows := logoFrame(frame, p)
		for i, row := range rows {
			plain := stripANSI(row)
			// Build the expected padded glyph row.
			want := opcode42Glyph[i]
			for len([]rune(want)) < logoWidth {
				want += " "
			}
			if plain != want {
				t.Errorf("frame %d row %d text changed by shimmer:\n got:  %q\n want: %q",
					frame, i, plain, want)
			}
		}
	}
}

// ── animating() splash/session tests ─────────────────────────────────────────

// makeTestModelForScreen returns a minimal Model on the given screen with no
// running tools.  Reuses the helpers from spinner_test.go pattern.
func makeTestModelForScreen(screen Screen) Model {
	m := Model{
		cfg:    Config{SessionID: "s1"},
		store:  newStore(),
		screen: screen,
	}
	return m
}

// TestAnimatingSplashScreen asserts animating() returns true on ScreenSplash
// (logo shimmer must tick continuously while the home screen is visible).
func TestAnimatingSplashScreen(t *testing.T) {
	m := makeTestModelForScreen(ScreenSplash)
	if !m.animating() {
		t.Error("animating() should be true on ScreenSplash (logo shimmer)")
	}
}

// TestAnimatingSplashScreenNoSession asserts that animating() is true on
// ScreenSplash even with no session ID — the shimmer doesn't require a session.
func TestAnimatingSplashScreenNoSession(t *testing.T) {
	m := Model{
		cfg:    Config{SessionID: ""},
		store:  newStore(),
		screen: ScreenSplash,
	}
	if !m.animating() {
		t.Error("animating() should be true on ScreenSplash regardless of session")
	}
}

// TestAnimatingSessionIdleIsFalse asserts animating() is false on ScreenSession
// with no running tools — the shimmer does not run at idle session screen.
func TestAnimatingSessionIdleIsFalse(t *testing.T) {
	m := makeTestModelForScreen(ScreenSession)
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartStateForLogo("completed")},
	}
	if m.animating() {
		t.Error("animating() should be false on ScreenSession with only completed tools")
	}
}

// TestAnimatingSessionRunningToolIsTrue asserts animating() is still true on
// ScreenSession with a running tool (original spinner behavior unchanged).
func TestAnimatingSessionRunningToolIsTrue(t *testing.T) {
	m := makeTestModelForScreen(ScreenSession)
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartStateForLogo("running")},
	}
	if !m.animating() {
		t.Error("animating() should be true on ScreenSession with a running tool")
	}
}

// makeToolPartStateForLogo builds a JSON-encoded toolState for logo_test.go.
// (mirrors makeToolPartState in spinner_test.go but avoids a duplicate symbol
// by using a distinct name scoped to this file.)
func makeToolPartStateForLogo(status string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"status": status})
	return raw
}

// ── shimmerBrightness unit tests ──────────────────────────────────────────────

// TestShimmerBrightnessClamp asserts that shimmerBrightness always returns a
// value in [0,1] for any (x, frame) combination.
func TestShimmerBrightnessClamp(t *testing.T) {
	for frame := range 100 {
		for x := range logoWidth {
			b := shimmerBrightness(x, frame)
			if b < 0 || b > 1 {
				t.Errorf("shimmerBrightness(%d, %d) = %.6f outside [0,1]", x, frame, b)
			}
		}
	}
}

// TestShimmerBrightnessPeriodic asserts the shimmer is periodic: frame N and
// frame N+shimmerPeriodFrames produce the same brightness for all columns.
func TestShimmerBrightnessPeriodic(t *testing.T) {
	for _, frame := range []int{0, 7, 15, 22, 30} {
		for x := range logoWidth {
			b1 := shimmerBrightness(x, frame)
			b2 := shimmerBrightness(x, frame+shimmerPeriodFrames)
			if b1 != b2 {
				t.Errorf("shimmerBrightness not periodic: (%d,%d)=%.6f vs (%d,%d)=%.6f",
					x, frame, b1, x, frame+shimmerPeriodFrames, b2)
			}
		}
	}
}

// TestColumnColorDark asserts that columnColor on a dark palette returns a
// theme.Color that is a valid "#rrggbb" hex string for all (x, frame).
func TestColumnColorDark(t *testing.T) {
	p := theme.Default()
	for _, frame := range []int{0, 10, 23, 45} {
		for x := range logoWidth {
			col := columnColor(x, frame, p)
			s := string(col)
			if !strings.HasPrefix(s, "#") || len(s) != 7 {
				t.Errorf("columnColor(%d, %d, dark): invalid color %q", x, frame, s)
			}
		}
	}
}
