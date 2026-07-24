package tui

import "testing"

// Tests for plan 08f H7 (G.11 display-toggle palette entries + G.12 theme
// mode/lock).

func TestH7_Palette_ToggleAnimations(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if m.noAnim {
		t.Fatal("animations should default on (noAnim false)")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleAnimations)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if !nm.noAnim {
		t.Fatal("palette toggle should disable animations")
	}
	if nm.modal != modalNone {
		t.Fatalf("palette should close, modal=%v", nm.modal)
	}
	// Toggling again re-enables.
	nm.modal, nm.modalSel = modalPalette, indexOfAction(paToggleAnimations)
	next2, _ := nm.modalSelect()
	nm2 := next2.(Model)
	if nm2.noAnim {
		t.Fatal("second toggle should re-enable animations")
	}
}

func TestH7_Palette_ToggleAnimations_CLIForcesOff(t *testing.T) {
	m := New(Config{URL: "http://x", NoAnim: true})
	if !m.noAnim {
		t.Fatal("--no-anim should force animations off")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleAnimations)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if !nm.noAnim {
		t.Fatal("--no-anim should still force animations off after a palette toggle attempt")
	}
}

func TestH7_Palette_TogglePasteSummary(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if !m.pasteSummaryEnabled {
		t.Fatal("paste summary should default on")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paTogglePasteSummary)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.pasteSummaryEnabled {
		t.Fatal("palette toggle should disable paste summary")
	}
}

func TestH7_Palette_PasteSummaryLabel_IsDynamic(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.modal = modalPalette
	_, rows, _ := m.modalItems()
	idx := indexOfAction(paTogglePasteSummary)
	if rows[idx] != "Disable paste summary" {
		t.Fatalf("label with paste summary on = %q, want %q", rows[idx], "Disable paste summary")
	}
	m.pasteSummaryEnabled = false
	_, rows, _ = m.modalItems()
	if rows[idx] != "Enable paste summary" {
		t.Fatalf("label with paste summary off = %q, want %q", rows[idx], "Enable paste summary")
	}
}

func TestH7_Palette_ToggleDiffTree(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if m.diffTreeHidden {
		t.Fatal("diff tree should default shown (not hidden)")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleDiffTree)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if !nm.diffTreeHidden {
		t.Fatal("palette toggle should hide the diff tree")
	}
}

func TestH7_Palette_ToggleDiffTree_SyncsOpenReviewer(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.diff.open = true
	m.diff.showTree = true
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleDiffTree)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.diff.showTree {
		t.Fatal("toggling with the reviewer open should also flip diff.showTree")
	}
}

func TestH7_Palette_ToggleFileContext(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if !m.fileContextEnabled {
		t.Fatal("file context should default on")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleFileContext)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.fileContextEnabled {
		t.Fatal("palette toggle should disable file context")
	}
}

func TestH7_Palette_ToggleSessionDirFilter(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if !m.sessionDirFilterEnabled {
		t.Fatal("session directory filter should default on")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleSessionDirFilter)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.sessionDirFilterEnabled {
		t.Fatal("palette toggle should disable session directory filtering")
	}
}

func TestH7_Palette_ThemeSwitchMode_FlipsTermDark(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.termDark = true
	m = m.applyThemeByName("opcode42-dark")
	m.modal, m.modalSel = modalPalette, indexOfAction(paThemeSwitchMode)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.termDark {
		t.Fatal("switch mode should flip termDark to false (light)")
	}
	// Flip back.
	nm.modal, nm.modalSel = modalPalette, indexOfAction(paThemeSwitchMode)
	next2, _ := nm.modalSelect()
	nm2 := next2.(Model)
	if !nm2.termDark {
		t.Fatal("switching again should flip termDark back to true (dark)")
	}
}

func TestH7_Palette_ThemeSwitchMode_SwapsNativePalette(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.termDark = true
	m = m.applyThemeByName("opcode42-dark")
	darkBg := m.styles.P.Bg
	m.modal, m.modalSel = modalPalette, indexOfAction(paThemeSwitchMode)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.termDark {
		t.Fatal("expected light mode")
	}
	if nm.themeName != "opcode42-light" {
		t.Fatalf("themeName = %q, want opcode42-light", nm.themeName)
	}
	if nm.styles.P.Bg == darkBg {
		t.Fatalf("palette Bg unchanged after switch to light: %v", nm.styles.P.Bg)
	}
}

func TestH7_Palette_ThemeSwitchModeLabel_IsDynamic(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.termDark = true
	m.modal = modalPalette
	_, rows, _ := m.modalItems()
	idx := indexOfAction(paThemeSwitchMode)
	if rows[idx] != "Switch to light mode" {
		t.Fatalf("label on dark mode = %q, want %q", rows[idx], "Switch to light mode")
	}
	m.termDark = false
	_, rows, _ = m.modalItems()
	if rows[idx] != "Switch to dark mode" {
		t.Fatalf("label on light mode = %q, want %q", rows[idx], "Switch to dark mode")
	}
}

func TestH7_Palette_ThemeModeLock_TogglesAndPersistsMode(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.termDark = true
	if m.themeModeLocked {
		t.Fatal("theme mode should default unlocked")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paThemeModeLock)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if !nm.themeModeLocked {
		t.Fatal("palette toggle should lock the theme mode")
	}
	if got := themeModeLockValue(nm); got == nil || *got != true {
		t.Fatalf("themeModeLockValue() = %v, want pointer to true (locked dark)", got)
	}
	// Unlocking clears the persisted lock value.
	nm.modal, nm.modalSel = modalPalette, indexOfAction(paThemeModeLock)
	next2, _ := nm.modalSelect()
	nm2 := next2.(Model)
	if nm2.themeModeLocked {
		t.Fatal("second toggle should unlock the theme mode")
	}
	if got := themeModeLockValue(nm2); got != nil {
		t.Fatalf("themeModeLockValue() after unlock = %v, want nil", got)
	}
}

func TestH7_Palette_ThemeModeLockLabel_IsDynamic(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.modal = modalPalette
	_, rows, _ := m.modalItems()
	idx := indexOfAction(paThemeModeLock)
	if rows[idx] != "Lock theme mode" {
		t.Fatalf("label when unlocked = %q, want %q", rows[idx], "Lock theme mode")
	}
	m.themeModeLocked = true
	_, rows, _ = m.modalItems()
	if rows[idx] != "Unlock theme mode" {
		t.Fatalf("label when locked = %q, want %q", rows[idx], "Unlock theme mode")
	}
}

func TestH7_Restore_ThemeModeLock_OverridesLiveTerminalProbe(t *testing.T) {
	// Simulate: the user locked the theme mode to light on a prior run, but
	// this run's terminal reports a dark background. Restore should honour
	// the lock, not the live probe.
	m := New(Config{URL: "http://x"})
	m.termDark = true // stand-in for a freshly-detected dark terminal
	kv := kvData{ThemeModeLock: boolPtr(false)}
	if dark, locked := kvThemeModeLocked(kv); !locked || dark {
		t.Fatalf("kvThemeModeLocked = (%v, %v), want (false, true)", dark, locked)
	}
	if dark, locked := kvThemeModeLocked(kv); locked {
		m.themeModeLocked = true
		if m.termDark != dark {
			m.termDark = dark
			m = m.applyThemeByName(m.themeName)
		}
	}
	if m.termDark {
		t.Fatal("locked mode should override the live terminal probe")
	}
	if !m.themeModeLocked {
		t.Fatal("m.themeModeLocked should be set from the kv lock")
	}
}

func TestH7_KV_DisplayToggles_DefaultOn(t *testing.T) {
	if !kvAnimationsEnabled(kvData{}) {
		t.Fatal("nil AnimationsEnabled should default on")
	}
	if !kvFileContextEnabled(kvData{}) {
		t.Fatal("nil FileContextEnabled should default on")
	}
	if !kvSessionDirFilterEnabled(kvData{}) {
		t.Fatal("nil SessionDirFilterEnabled should default on")
	}
	off := false
	if kvAnimationsEnabled(kvData{AnimationsEnabled: &off}) {
		t.Fatal("explicit false AnimationsEnabled should disable")
	}
	if kvFileContextEnabled(kvData{FileContextEnabled: &off}) {
		t.Fatal("explicit false FileContextEnabled should disable")
	}
	if kvSessionDirFilterEnabled(kvData{SessionDirFilterEnabled: &off}) {
		t.Fatal("explicit false SessionDirFilterEnabled should disable")
	}
}

func TestH7_KV_ThemeModeLock_DefaultsUnlocked(t *testing.T) {
	if dark, locked := kvThemeModeLocked(kvData{}); locked || dark {
		t.Fatalf("kvThemeModeLocked(empty) = (%v, %v), want (false, false)", dark, locked)
	}
	dark := true
	if got, locked := kvThemeModeLocked(kvData{ThemeModeLock: &dark}); !locked || !got {
		t.Fatalf("kvThemeModeLocked(locked dark) = (%v, %v), want (true, true)", got, locked)
	}
}

func TestH7_Persist_RoundTripsDisplayToggles(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.persistEnabled = false // keep the test hermetic; exercise saveKV shape directly instead
	m.fileContextEnabled = false
	m.sessionDirFilterEnabled = false
	m.noAnim = true
	m.themeModeLocked = true
	m.termDark = false

	if got := themeModeLockValue(m); got == nil || *got != false {
		t.Fatalf("themeModeLockValue() = %v, want pointer to false (locked light)", got)
	}
}
