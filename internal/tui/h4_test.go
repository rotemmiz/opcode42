package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestH4_ModalPanelGeometry pins the border+padding offset (H4's
// borderAndPadTop constant) directly against buildModalPanel/centeredCardPos
// — independent of Update's mouse dispatch — so a regression in the panel's
// vertical layout (border, Padding(1,2)) breaks a focused test instead of
// only the wiring tests below.
func TestH4_ModalPanelGeometry(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 40
	m.modal, m.modalSel = modalPalette, 0

	b := m.buildModalPanel()
	if !b.ok {
		t.Fatal("palette modal should report selectable rows")
	}
	px, py, ok := centeredCardPos(m.width, m.height, b.panel)
	if !ok {
		t.Fatal("centeredCardPos should succeed for a 120x40 screen")
	}
	x := px + 2
	wantY0 := py + 2 + b.rowFirstLine // 1 border row + 1 Padding(1,2) top row

	if row, ok := m.modalRowAtY(x, wantY0); !ok || row != 0 {
		t.Fatalf("modalRowAtY(%d,%d) = (%d,%v), want (0,true)", x, wantY0, row, ok)
	}
	if row, ok := m.modalRowAtY(x, wantY0+1); !ok || row != 1 {
		t.Fatalf("modalRowAtY(%d,%d) = (%d,%v), want (1,true)", x, wantY0+1, row, ok)
	}
	if _, ok := m.modalRowAtY(x, py); ok {
		t.Fatal("a y inside the border/padding band should not hit a row")
	}
	if _, ok := m.modalRowAtY(px-1, wantY0); ok {
		t.Fatal("an x left of the panel should not hit a row")
	}
}

// TestH4_ModalRowAtY_NoRowsWhenRename verifies the rename text-input overlay
// (no row list) never reports a hit — clicking it must be a no-op, not an
// accidental submit.
func TestH4_ModalRowAtY_NoRowsWhenRename(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 40
	m.modal = modalRename
	m.renameInput.Focus()

	for y := 0; y < m.height; y++ {
		if _, ok := m.modalRowAtY(m.width/2, y); ok {
			t.Fatalf("rename overlay should never hit a row (y=%d)", y)
		}
	}
}

// TestH4_ModalMouseMotion_UpdatesSel drives a real tea.MouseMotionMsg through
// Update and asserts hovering a row previews the selection without dispatching
// anything (modal stays open).
func TestH4_ModalMouseMotion_UpdatesSel(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 0

	b := m.buildModalPanel()
	px, py, ok := centeredCardPos(m.width, m.height, b.panel)
	if !ok {
		t.Fatal("centeredCardPos should succeed")
	}
	x, y := px+2, py+2+b.rowFirstLine+1 // row index 1

	next, cmd := step(t, m, tea.MouseMotionMsg{X: x, Y: y})
	if next.modalSel != 1 {
		t.Fatalf("motion over row 1 should set modalSel=1, got %d", next.modalSel)
	}
	if next.modal != modalPalette {
		t.Fatalf("motion should not close the modal, got %v", next.modal)
	}
	if cmd != nil {
		t.Fatal("hover must not dispatch a command")
	}
}

// TestH4_ModalMouseMotion_OutsidePanelIsNoop hovering outside the panel must
// leave modalSel untouched.
func TestH4_ModalMouseMotion_OutsidePanelIsNoop(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 5

	next, _ := step(t, m, tea.MouseMotionMsg{X: 0, Y: 0})
	if next.modalSel != 5 {
		t.Fatalf("hover in the corner (outside the panel) should not change modalSel, got %d", next.modalSel)
	}
}

// TestH4_ModalMouseClick_SelectsAndSubmits clicks the first palette row
// ("New session") and asserts the same dispatch as pressing enter would.
func TestH4_ModalMouseClick_SelectsAndSubmits(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 0

	b := m.buildModalPanel()
	px, py, ok := centeredCardPos(m.width, m.height, b.panel)
	if !ok {
		t.Fatal("centeredCardPos should succeed")
	}
	x, y := px+2, py+2+b.rowFirstLine // row index 0 = "New session"

	next, cmd := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if next.modal != modalNone || cmd == nil {
		t.Fatalf("click on 'New session' should close the modal + dispatch, modal=%v cmd=%v", next.modal, cmd != nil)
	}
}

// TestH4_ModalMouseClick_DifferentRowSelectsThatRow clicks the second palette
// row ("Switch session") — proving the click both moves modalSel AND submits
// the clicked row, not the previously-selected one.
func TestH4_ModalMouseClick_DifferentRowSelectsThatRow(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 0 // starts on "New session"

	b := m.buildModalPanel()
	px, py, ok := centeredCardPos(m.width, m.height, b.panel)
	if !ok {
		t.Fatal("centeredCardPos should succeed")
	}
	x, y := px+2, py+2+b.rowFirstLine+1 // row index 1 = "Switch session"

	next, cmd := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if next.modal != modalSessions || cmd == nil {
		t.Fatalf("click on 'Switch session' should open the sessions modal + load, modal=%v cmd=%v", next.modal, cmd != nil)
	}
}

// TestH4_ModalMouseClick_OutsidePanelIsNoop clicking off the panel must not
// touch modalSel or the modal, and must not dispatch anything.
func TestH4_ModalMouseClick_OutsidePanelIsNoop(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 2

	next, cmd := step(t, m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if next.modal != modalPalette || next.modalSel != 2 || cmd != nil {
		t.Fatalf("click outside the panel should be a no-op, modal=%v sel=%d cmd=%v", next.modal, next.modalSel, cmd != nil)
	}
}

// TestH4_ModalMouseClick_RightButtonIgnored a non-left click must not submit.
func TestH4_ModalMouseClick_RightButtonIgnored(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 0

	b := m.buildModalPanel()
	px, py, _ := centeredCardPos(m.width, m.height, b.panel)
	x, y := px+2, py+2+b.rowFirstLine

	next, cmd := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	if next.modal != modalPalette || cmd != nil {
		t.Fatalf("a right-click should not submit, modal=%v cmd=%v", next.modal, cmd != nil)
	}
}

// TestH4_AutocompleteMouseMotion_UpdatesSel hovers the slash-command popup's
// second row and asserts ac.sel follows it.
func TestH4_AutocompleteMouseMotion_UpdatesSel(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("/")) // opens the popup, sel=0
	if !m.ac.open || m.ac.count() < 2 {
		t.Fatal("setup: expected the popup open with at least 2 rows")
	}

	x, y := m.acPopupX()+2, m.acPopupY()+1+1 // border row + row index 1
	next, cmd := step(t, m, tea.MouseMotionMsg{X: x, Y: y})
	if next.ac.sel != 1 {
		t.Fatalf("hover over row 1 should set ac.sel=1, got %d", next.ac.sel)
	}
	if !next.ac.open {
		t.Fatal("hover must not close the popup")
	}
	if cmd != nil {
		t.Fatal("hover must not dispatch a command")
	}
}

// TestH4_AutocompleteMouseMotion_OutsidePopupIsNoop hovering off the popup
// must leave ac.sel untouched.
func TestH4_AutocompleteMouseMotion_OutsidePopupIsNoop(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("/"))
	m.ac.sel = 0

	next, _ := step(t, m, tea.MouseMotionMsg{X: 0, Y: 0})
	if next.ac.sel != 0 {
		t.Fatalf("hover outside the popup should not change ac.sel, got %d", next.ac.sel)
	}
}

// TestH4_AutocompleteMouseClick_AcceptsSlash clicks the single "/models"
// match and asserts it runs the same as pressing enter would.
func TestH4_AutocompleteMouseClick_AcceptsSlash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/models")
	m, _ = m.refreshAutocomplete()
	if !m.ac.open || len(m.ac.items) != 1 {
		t.Fatalf("setup: expected exactly one match, got %+v", m.ac.items)
	}

	x, y := m.acPopupX()+2, m.acPopupY()+1 // border row + row index 0
	next, cmd := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if next.modal != modalModels || cmd == nil {
		t.Fatalf("click on /models should open the model switcher, modal=%v cmd=%v", next.modal, cmd != nil)
	}
	if next.ac.open || next.input.Value() != "" {
		t.Fatal("accepting via click should close the popup and clear the composer")
	}
}

// TestH4_AutocompleteMouseClick_AcceptsMention clicks a file row in the
// @-mention popup and asserts the composer gets the "@path " insertion.
func TestH4_AutocompleteMouseClick_AcceptsMention(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("@fo")
	m.ac = autocomplete{open: true, mode: acMention, files: []string{"foo.go", "bar.go"}, sel: 0}

	x, y := m.acPopupX()+2, m.acPopupY()+1+1 // row index 1 = bar.go
	next, _ := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if next.ac.open {
		t.Fatal("accepting via click should close the popup")
	}
	if got := next.input.Value(); got != "@bar.go " {
		t.Fatalf("click on bar.go should insert it, got %q", got)
	}
}

// TestH4_AutocompleteMouseClick_OutsidePopupIsNoop clicking off the popup must
// not accept anything or move ac.sel.
func TestH4_AutocompleteMouseClick_OutsidePopupIsNoop(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("/"))
	m.ac.sel = 0

	next, cmd := step(t, m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if !next.ac.open || next.ac.sel != 0 || cmd != nil {
		t.Fatalf("click outside the popup should be a no-op, open=%v sel=%d cmd=%v", next.ac.open, next.ac.sel, cmd != nil)
	}
}

// TestH4_ModalTakesPriorityOverAutocomplete: a modal open over a lingering
// autocomplete state (shouldn't normally coexist, but the guard order
// matters) routes mouse events to the modal, matching the key-handler
// priority (handleModalKey before handleAutocompleteKey).
func TestH4_ModalTakesPriorityOverAutocomplete(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.ac = autocomplete{open: true, mode: acSlash, items: []slashItem{{name: "/new", kind: slashBuiltin}}, sel: 0}
	m.modal, m.modalSel = modalPalette, 0

	b := m.buildModalPanel()
	px, py, _ := centeredCardPos(m.width, m.height, b.panel)
	x, y := px+2, py+2+b.rowFirstLine+1 // row index 1

	next, _ := step(t, m, tea.MouseMotionMsg{X: x, Y: y})
	if next.modalSel != 1 {
		t.Fatalf("modal should take priority over the (stale) open autocomplete, modalSel=%d", next.modalSel)
	}
	if next.ac.sel != 0 {
		t.Fatalf("autocomplete sel should be untouched while a modal owns mouse input, got %d", next.ac.sel)
	}
}

// TestH4_PermissionOutranksModalMouse: a pending permission overlay must
// swallow mouse clicks even when a modal is still open in the model (SSE can
// arrive while the palette is up). Mirrors KeyPressMsg / MouseWheel priority.
func TestH4_PermissionOutranksModalMouse(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m = openSes(m, "ses_1")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.modal, m.modalSel = modalPalette, 0
	m.store = m.store.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "ls"}))
	if m.pendingPermission() == nil {
		t.Fatal("setup: pendingPermission nil")
	}
	b := m.buildModalPanel()
	px, py, _ := centeredCardPos(m.width, m.height, b.panel)
	x, y := px+2, py+2+b.rowFirstLine

	next, cmd := step(t, m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if next.modal != modalPalette {
		t.Fatalf("permission should block modal click, modal=%v", next.modal)
	}
	if cmd != nil {
		t.Fatal("permission should block modalSelect dispatch")
	}
	if next.modalSel != 0 {
		t.Fatalf("modalSel should be unchanged, got %d", next.modalSel)
	}
}
