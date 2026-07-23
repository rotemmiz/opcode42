package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// whichkey_test.go — plan 08e §F2 + §F3 tests.
//
// F2: the which-key overlay renders when the ctrl+x leader is armed and
// disappears on the next key (the chord, esc, or any other key). The overlay
// is a Layer.Z(15) in overlayLayers, gated on m.leader.
//
// F3: the help overlay (modalHelp) is reachable via F1, ctrl+x h, and /help.
// Its content (helpRows) covers the major keybinds.

// TestWhichKeyOverlay_RendersOnLeader presses ctrl+x and asserts:
//   - m.leader is true
//   - whichKeyView() returns a non-empty strip
//   - the overlay layer is present in overlayLayers at Z=zWhichKey
//   - the strip names the chord keys (l, n, m, a, …)
func TestWhichKeyOverlay_RendersOnLeader(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})

	// ctrl+x arms the leader. The old behavior mutated m.status with the
	// cheat-sheet string; the new behavior leaves m.status alone and renders
	// the strip via the which-key overlay layer.
	prevStatus := m.status
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !m.leader {
		t.Fatal("ctrl+x should arm the leader")
	}
	if m.status != prevStatus {
		t.Fatalf("ctrl+x should NOT mutate m.status (the which-key overlay replaces the cheat-sheet); got %q want %q", m.status, prevStatus)
	}

	// whichKeyView() returns a non-empty strip when the leader is armed.
	view := m.whichKeyView()
	if view == "" {
		t.Fatal("whichKeyView() should return a non-empty strip when m.leader is true")
	}
	// The strip names the chord keys. Spot-check a representative subset.
	plain := stripANSI(view)
	for _, want := range []string{"ctrl+x", "l", "sessions", "n", "new", "h", "help"} {
		if !strings.Contains(plain, want) {
			t.Errorf("whichKeyView missing %q in:\n%s", want, plain)
		}
	}

	// The overlay layer is present in overlayLayers at Z=zWhichKey.
	layers := m.overlayLayers()
	var found bool
	for _, l := range layers {
		if l.GetZ() == zWhichKey {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("overlayLayers should include a Z=%d (zWhichKey) layer when m.leader is true; got %d layers", zWhichKey, len(layers))
	}
}

// TestWhichKeyOverlay_ClearsOnChord presses ctrl+x then a chord (l → sessions
// modal) and asserts the overlay is gone (m.leader is false, no Z=zWhichKey
// layer in overlayLayers).
func TestWhichKeyOverlay_ClearsOnChord(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})

	// Arm the leader.
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !m.leader {
		t.Fatal("ctrl+x should arm the leader")
	}
	// The overlay layer should be present while armed.
	if !hasWhichKeyLayer(m.overlayLayers()) {
		t.Fatal("which-key overlay should be present while m.leader is true")
	}

	// Press a chord (l → sessions modal). handleLeaderKey opens the modal
	// and clears the leader; the overlay should be gone on the next render.
	m, _ = step(t, m, key("l"))
	if m.leader {
		t.Fatal("the leader should clear after the chord")
	}
	if m.modal != modalSessions {
		t.Fatalf("ctrl+x l should open the sessions modal, got %v", m.modal)
	}
	if hasWhichKeyLayer(m.overlayLayers()) {
		t.Fatal("which-key overlay should be gone after the chord (m.leader is false)")
	}

	// whichKeyView() should return "" when the leader is not armed.
	if view := m.whichKeyView(); view != "" {
		t.Fatalf("whichKeyView() should return \"\" when m.leader is false, got %q", view)
	}
}

// TestWhichKeyOverlay_ClearsOnEsc presses ctrl+x then esc and asserts the
// overlay is gone. esc doesn't open a modal — it just clears the leader via
// the fall-through to the composer Update (esc in the composer exits shell
// mode or is a no-op, but the leader is already cleared by the leader
// handler's first branch). We assert the overlay disappears because
// m.leader is false after any key follows ctrl+x.
func TestWhichKeyOverlay_ClearsOnEsc(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !m.leader {
		t.Fatal("ctrl+x should arm the leader")
	}
	// esc goes through the leader handler (m.leader is true → handleLeaderKey)
	// and falls through the switch (no case "esc"), clearing the leader.
	m, _ = step(t, m, key("esc"))
	if m.leader {
		t.Fatal("esc after ctrl+x should clear the leader (handleLeaderKey fall-through)")
	}
	if hasWhichKeyLayer(m.overlayLayers()) {
		t.Fatal("which-key overlay should be gone after esc")
	}
}

// TestWhichKeyOverlay_MatchesLeaderKey asserts that every chord key listed in
// whichKeyChords is handled by handleLeaderKey (no stale entries on either
// side). This pins the rendering table against the dispatch table so a drift
// is a test failure, not a silent UI bug.
func TestWhichKeyOverlay_MatchesLeaderKey(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	for _, c := range whichKeyChords {
		// Arm the leader for each chord (handleLeaderKey clears it).
		m.leader = true
		before := m
		// For arrow keys, the key string is "down"/"up"; for printable chords
		// the first rune is the code. Build a key msg that matches what
		// handleLeaderKey sees (msg.String()).
		var msg tea.KeyPressMsg
		switch c.key {
		case "↓":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "↑":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			msg = tea.KeyPressMsg{Code: rune(c.key[0]), Text: c.key}
		}
		next, cmd := m.handleLeaderKey(msg)
		// The dispatch must produce SOME observable effect: either a state
		// change (modal/sidebar/view/status) or a non-nil command (e.g. "n"
		// dispatches newSessionCmd, "e" dispatches openEditorCmd, "`"
		// dispatches the PTY dial). A completely unchanged model AND a nil
		// cmd means the chord fell through (no case in the switch), which
		// means the whichKeyChords entry is stale.
		if chordUnchanged(before, next.(Model)) && cmd == nil {
			t.Errorf("whichKeyChords lists %q (%q) but handleLeaderKey did not change any state and returned no cmd — stale entry or missing case?", c.key, c.label)
		}
	}
}

// chordUnchanged reports whether the model is unchanged by handleLeaderKey
// for a given chord. It compares the fields a chord can mutate: modal,
// sidebarHidden, tasksOpen, view (toggles), status. A chord that produces no
// observable change is a fall-through (no case in the switch), which means
// the whichKeyChords entry is stale (lists a key that doesn't do anything).
// viewState contains a map so it's compared with reflect.DeepEqual.
func chordUnchanged(a, b Model) bool {
	return a.modal == b.modal &&
		a.sidebarHidden == b.sidebarHidden &&
		a.tasksOpen == b.tasksOpen &&
		reflect.DeepEqual(a.view, b.view) &&
		a.status == b.status
}

// hasWhichKeyLayer reports whether the layer slice contains a Z=zWhichKey
// layer (the which-key overlay).
func hasWhichKeyLayer(layers []*lipgloss.Layer) bool {
	for _, l := range layers {
		if l.GetZ() == zWhichKey {
			return true
		}
	}
	return false
}

// TestWhichKeyOverlay_FullWidth asserts the strip is rendered at the full
// screen width (so it replaces the status bar's row cleanly, no terminal-
// default bleed on either side).
func TestWhichKeyOverlay_FullWidth(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m.width, m.height = 120, 24
	m.leader = true
	view := m.whichKeyView()
	if view == "" {
		t.Fatal("whichKeyView() should return a non-empty strip when m.leader is true")
	}
	if got := lipgloss.Width(view); got != m.width {
		t.Fatalf("whichKeyView width = %d, want %d (full screen width)", got, m.width)
	}
}

// TestWhichKeyOverlay_NoWrapAtCommonWidths asserts the strip is always a
// single line (height=1) across a range of screen widths. The strip is
// rendered at the bottom row (whichKeyLayerHeight=1); a wrap would overflow
// into the composer and break the layer-height-1 assumption. The strip
// truncates with " …" when the chord list doesn't fit, so narrow terminals
// see fewer chords (the most frequent first) but never a wrapped strip.
func TestWhichKeyOverlay_NoWrapAtCommonWidths(t *testing.T) {
	for _, w := range []int{10, 15, 20, 30, 40, 60, 80, 100, 120, 160, 200} {
		m := New(Config{URL: "http://x"})
		m.screen = ScreenSession
		m.width, m.height = w, 24
		m.leader = true
		view := m.whichKeyView()
		if h := lipgloss.Height(view); h != 1 {
			t.Errorf("width=%d: whichKeyView height=%d, want 1 (strip must not wrap). content:\n%s", w, h, stripANSI(view))
		}
	}
}

// TestWhichKeyOverlay_TruncatesAtNarrowWidths asserts the strip truncates
// with " …" when the chord list doesn't fit, and that the most frequent
// chords are shown first (whichKeyChords is ordered by frequency). At a
// narrow width (e.g. 40 cols) the strip shows fewer chords than at a wide
// width (e.g. 480 cols, where all chords fit).
func TestWhichKeyOverlay_TruncatesAtNarrowWidths(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m.leader = true

	m.width, m.height = 40, 24
	narrow := stripANSI(m.whichKeyView())
	if !strings.Contains(narrow, "…") {
		t.Errorf("at width=40 the strip should truncate with '…'; got:\n%s", narrow)
	}

	m.width, m.height = 480, 24
	wide := stripANSI(m.whichKeyView())
	if strings.Contains(wide, "…") {
		t.Errorf("at width=480 the strip should show all chords (no '…'); got:\n%s", wide)
	}

	// The narrow strip should show fewer chords than the wide strip. We
	// count " · " separators as a proxy for the chord count.
	narrowCount := strings.Count(narrow, " · ")
	wideCount := strings.Count(wide, " · ")
	if narrowCount >= wideCount {
		t.Errorf("narrow strip should show fewer chords than wide: narrow=%d wide=%d", narrowCount, wideCount)
	}
}

// TestWhichKeyOverlay_Position asserts the overlay layer is positioned at the
// bottom of the screen (Y = height - 1) and X = 0.
func TestWhichKeyOverlay_Position(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m.width, m.height = 120, 24
	m.leader = true
	if got := m.whichKeyLayerX(); got != 0 {
		t.Errorf("whichKeyLayerX = %d, want 0", got)
	}
	if got := m.whichKeyLayerY(); got != m.height-whichKeyLayerHeight {
		t.Errorf("whichKeyLayerY = %d, want %d (height - whichKeyLayerHeight)", got, m.height-whichKeyLayerHeight)
	}
}

// --- F3: help overlay ---

// TestHelpModal_ContainsAllKeybinds opens modalHelp and asserts the rendered
// output contains the major keybinds (ctrl+p, ctrl+x, esc, enter, F1, etc.).
// This pins helpRows() against the keybind surface the TUI documents.
func TestHelpModal_ContainsAllKeybinds(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 40
	m.modal = modalHelp
	out := m.modalView()
	if out == "" {
		t.Fatal("modalHelp should render a non-empty panel")
	}
	plain := stripANSI(out)
	for _, want := range []string{
		"Keybindings",
		"ctrl+p",   // palette
		"ctrl+x l", // sessions chord
		"ctrl+x h", // help chord
		"F1",       // help key
		"/help",    // help slash
		"enter",    // send
		"ctrl+c",   // quit
		"ctrl+x ↓", // subagent descend
		"ctrl+x `", // terminal
		"Navigation",
		"Subagents",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("help modal missing %q in:\n%s", want, plain)
		}
	}
}

// TestHelpModal_NoRowWraps asserts every helpRow fits within the modal panel's
// inner width (52 cols) so the surfaceRow Width(52) doesn't wrap a row into
// multiple lines (which would break the panel layout with stray wrapped rows).
// This pins helpRows() against the panel width budget — a row > 52 chars
// wraps under lipgloss Width(52) and shows as an extra line.
func TestHelpModal_NoRowWraps(t *testing.T) {
	const innerWidth = 52 // modal panel width (56) minus 2×Padding(1,2)
	for i, row := range helpRows() {
		// The rendered row is " "+row (modalView prepends a leading space).
		// lipgloss.Width counts visible runes; ANSI escapes (none in
		// helpRows — it's plain text) would not count.
		w := lipgloss.Width(" " + row)
		if w > innerWidth {
			t.Errorf("helpRows[%d] width %d > innerWidth %d (would wrap): %q", i, w, innerWidth, row)
		}
	}
}

// TestSlashHelp_OpensHelp types /help and asserts modalHelp opens.
func TestSlashHelp_OpensHelp(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/help")
	m, _ = m.refreshAutocomplete()
	m, _ = step(t, m, key("enter"))
	if m.modal != modalHelp {
		t.Fatalf("/help enter should open modalHelp, got %v", m.modal)
	}
	if m.input.Value() != "" {
		t.Errorf("/help should clear the composer, got %q", m.input.Value())
	}
}

// TestF1_OpensHelp presses F1 and asserts modalHelp opens.
func TestF1_OpensHelp(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	if m.modal != modalHelp {
		t.Fatalf("F1 should open modalHelp, got %v", m.modal)
	}
}

// TestLeaderH_OpensHelp presses ctrl+x then h and asserts modalHelp opens
// (plan 08e §F3 — ctrl+x h is the leader-key path to the help overlay).
func TestLeaderH_OpensHelp(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !m.leader {
		t.Fatal("ctrl+x should arm the leader")
	}
	m, _ = step(t, m, key("h"))
	if m.modal != modalHelp {
		t.Fatalf("ctrl+x h should open modalHelp, got %v", m.modal)
	}
	if m.leader {
		t.Fatal("ctrl+x h should clear the leader")
	}
}

// TestF1_FallsThroughModalOpen asserts F1 is a no-op when a modal is already
// open (the modal's handleModalKey handles keys, and F1 is not a modal key,
// so it falls through). This is the conservative behavior: F1 doesn't open a
// second modal on top of the first.
func TestF1_FallsThroughModalOpen(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	// Open the palette first.
	m, _ = step(t, m, key("ctrl+p"))
	if m.modal != modalPalette {
		t.Fatalf("ctrl+p should open the palette, got %v", m.modal)
	}
	// F1 while a modal is open: handleModalKey processes it, F1 is not a
	// modal key, so it's a no-op — the palette stays open (modalHelp does
	// NOT replace it).
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	if m.modal != modalPalette {
		t.Fatalf("F1 while a modal is open should be a no-op (the modal stays), got %v", m.modal)
	}
}
