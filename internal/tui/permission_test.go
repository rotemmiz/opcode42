package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func permEvent(t *testing.T, id, perm string, meta map[string]any) opcode42client.SSEEvent {
	t.Helper()
	m, _ := json.Marshal(meta)
	props, _ := json.Marshal(map[string]any{
		"id": id, "sessionID": "ses_1", "permission": perm, "metadata": json.RawMessage(m),
	})
	return opcode42client.SSEEvent{Type: "permission.asked", Properties: props}
}

func TestPermission_AskedThenRepliedReduces(t *testing.T) {
	s := newStore()
	s = s.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "rm -rf x"}))
	if len(s.permissions) != 1 || s.permissions[0].ID != "perm_1" {
		t.Fatalf("permission.asked should add a pending permission: %+v", s.permissions)
	}
	props, _ := json.Marshal(map[string]any{"requestID": "perm_1", "reply": "once"})
	s = s.Reduce(opcode42client.SSEEvent{Type: "permission.replied", Properties: props})
	if len(s.permissions) != 0 {
		t.Fatalf("permission.replied should clear it, got %+v", s.permissions)
	}
}

func TestPermission_OverlayBlocksAndReplies(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "ls"}))

	if m.pendingPermission() == nil {
		t.Fatal("a pending permission should be present")
	}
	// the overlay renders the action + detail
	view := m.renderView()
	for _, want := range []string{"Permission required", "bash", "ls"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission overlay missing %q", want)
		}
	}
	// typing does NOT reach the composer while blocked
	m, _ = step(t, m, key("x"))
	if m.input.Value() != "" {
		t.Fatal("keys should not reach the composer while a permission is pending")
	}
	// enter dispatches a reply (the default selection is "once"); the overlay
	// STAYS up (no optimistic clear) until the reply resolves — a failed POST
	// must not silently drop a blocked request.
	m, cmd := step(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("enter should dispatch a reply")
	}
	if m.pendingPermission() == nil || !m.permState.replying {
		t.Fatal("overlay should stay up (replying) until the reply resolves")
	}
	// failure keeps the request so the user can retry
	mFail, _ := step(t, m, permissionRepliedMsg{id: "perm_1", err: errTest})
	if mFail.pendingPermission() == nil || mFail.permState.replying {
		t.Fatal("a failed reply should keep the request and clear the replying flag")
	}
	// success clears it
	mOK, _ := step(t, m, permissionRepliedMsg{id: "perm_1"})
	if mOK.pendingPermission() != nil {
		t.Fatal("a successful reply should clear the permission")
	}
}

var errTest = errTestType("boom")

type errTestType string

func (e errTestType) Error() string { return string(e) }

// TestPermission_ShortcutKeys verifies the opencode-matching shortcuts
// (plan 17 §B2): tab/left/h/right/l shift selection, enter confirms, esc
// escapes. The old a/s/r shortcuts no longer work.
func TestPermission_ShortcutKeys(t *testing.T) {
	mk := func() Model {
		m := openSes(New(Config{URL: "http://x"}), "ses_1")
		m.store = m.store.Reduce(permEvent(t, "perm_1", "edit", nil))
		return m
	}
	// The legacy a/s/r shortcuts are NOT dispatch replies anymore (they're
	// not in opencode's keymap; plan 17 §B2).
	for _, kk := range []string{"a", "s", "r"} {
		m := mk()
		_, cmd := m.handlePermissionKey(key(kk))
		if cmd != nil {
			t.Fatalf("%q should NOT dispatch a reply (only tab/arrows + enter; plan 17 §B2)", kk)
		}
		if m.permState.stage != permStagePermission {
			t.Fatalf("%q should not transition the stage", kk)
		}
	}
	// enter dispatches a reply (default selection is "once").
	m := mk()
	next, cmd := m.handlePermissionKey(key("enter"))
	if cmd == nil {
		t.Fatal("enter should dispatch a reply (default selection = once)")
	}
	m = next.(Model)
	if !m.permState.replying {
		t.Fatal("enter should mark the state as replying")
	}

	// tab / right / l move the selection right without replying.
	for _, kk := range []string{"tab", "right", "l"} {
		mm := mk()
		nn, _ := mm.handlePermissionKey(key(kk))
		if got := nn.(Model).permState.selected; got != permOptAlways {
			t.Fatalf("%q should move selection to always (1), got %v", kk, got)
		}
	}

	// left / h move the selection left (wrapping) without replying.
	for _, kk := range []string{"left", "h"} {
		mm := mk()
		// move to index 1 first, then left back to 0
		nn, _ := mm.handlePermissionKey(key("right"))
		mm = nn.(Model)
		nn, _ = mm.handlePermissionKey(key(kk))
		if got := nn.(Model).permState.selected; got != permOptOnce {
			t.Fatalf("%q should move selection back to once (0), got %v", kk, got)
		}
	}

	// shift+tab moves the selection left (wrapping from 0 → last).
	m = mk()
	next, _ = m.handlePermissionKey(key("shift+tab"))
	if got := next.(Model).permState.selected; got != permOptReject {
		t.Fatalf("shift+tab should wrap selection to reject (2), got %v", got)
	}
}

// TestPermission_ThreeStageFlow verifies the 3-stage state machine
// (plan 17 §B3): permission → always confirm → reject message.
func TestPermission_ThreeStageFlow(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "ls"}))

	// Stage 1: permission. Select "always" (tab) and confirm (enter) →
	// transition to the always stage.
	if m.permState.stage != permStagePermission {
		t.Fatalf("initial stage should be permission, got %v", m.permState.stage)
	}
	m, _ = step(t, m, key("tab")) // once → always
	if m.permState.selected != permOptAlways {
		t.Fatalf("tab should move selection to always, got %v", m.permState.selected)
	}
	m, _ = step(t, m, key("enter"))
	if m.permState.stage != permStageAlways {
		t.Fatalf("confirming always should transition to the always stage, got %v", m.permState.stage)
	}
	if m.permState.selected != permOptConfirm {
		t.Fatalf("always stage's default selection should be confirm, got %v", m.permState.selected)
	}

	// Stage 2: always. Cancel returns to the permission stage.
	m, _ = step(t, m, key("tab")) // confirm → cancel
	if m.permState.selected != permOptCancel {
		t.Fatalf("tab should move selection to cancel, got %v", m.permState.selected)
	}
	m, _ = step(t, m, key("enter"))
	if m.permState.stage != permStagePermission {
		t.Fatalf("cancel should return to the permission stage, got %v", m.permState.stage)
	}

	// Stage 1 again: select "reject" and confirm → transition to the
	// reject stage. After the cancel we're at permOptAlways (index 1);
	// one tab moves to permOptReject (index 2).
	m, _ = step(t, m, key("tab")) // always → reject
	if m.permState.selected != permOptReject {
		t.Fatalf("one tab should move selection to reject, got %v", m.permState.selected)
	}
	m, _ = step(t, m, key("enter"))
	if m.permState.stage != permStageReject {
		t.Fatalf("confirming reject should transition to the reject stage, got %v", m.permState.stage)
	}

	// Stage 3: reject. Type a message, then enter dispatches the reject
	// reply with the message attached.
	m, _ = step(t, m, key("n"))
	m, _ = step(t, m, key("o"))
	if m.permState.message != "no" {
		t.Fatalf("typing should append to the reject message, got %q", m.permState.message)
	}
	m, cmd := step(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("enter in the reject stage should dispatch the reject reply")
	}
	if !m.permState.replying {
		t.Fatal("reject send should mark the state as replying")
	}
}

// TestPermission_EscapeTransitions verifies the esc key behavior
// (permissionEscape, permission.shared.ts:242-256):
//   - from the always stage → back to the permission stage
//   - from the permission stage → transition to the reject stage
//   - from the reject stage → back to the permission stage (cancel)
func TestPermission_EscapeTransitions(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(permEvent(t, "perm_1", "edit", nil))

	// permission → reject (esc)
	m, _ = step(t, m, key("esc"))
	if m.permState.stage != permStageReject {
		t.Fatalf("esc from permission should go to reject, got %v", m.permState.stage)
	}
	// reject → permission (esc = cancel). permCancelReject keeps
	// selected = permOptReject (matching opencode's permissionCancel,
	// permission.shared.ts:234-240).
	m, _ = step(t, m, key("esc"))
	if m.permState.stage != permStagePermission {
		t.Fatalf("esc from reject should cancel back to permission, got %v", m.permState.stage)
	}
	if m.permState.selected != permOptReject {
		t.Fatalf("cancel from reject should keep selected at reject, got %v", m.permState.selected)
	}
	// permission → always: tab wraps reject→once, tab once→always, enter.
	m, _ = step(t, m, key("tab")) // reject → once (wrap)
	m, _ = step(t, m, key("tab")) // once → always
	m, _ = step(t, m, key("enter"))
	if m.permState.stage != permStageAlways {
		t.Fatal("expected always stage")
	}
	// always → permission (esc)
	m, _ = step(t, m, key("esc"))
	if m.permState.stage != permStagePermission {
		t.Fatalf("esc from always should return to permission, got %v", m.permState.stage)
	}
}
