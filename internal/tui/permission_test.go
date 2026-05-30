package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

func permEvent(t *testing.T, id, perm string, meta map[string]any) forgeclient.SSEEvent {
	t.Helper()
	m, _ := json.Marshal(meta)
	props, _ := json.Marshal(map[string]any{
		"id": id, "sessionID": "ses_1", "permission": perm, "metadata": json.RawMessage(m),
	})
	return forgeclient.SSEEvent{Type: "permission.asked", Properties: props}
}

func TestPermission_AskedThenRepliedReduces(t *testing.T) {
	s := newStore()
	s = s.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "rm -rf x"}))
	if len(s.permissions) != 1 || s.permissions[0].ID != "perm_1" {
		t.Fatalf("permission.asked should add a pending permission: %+v", s.permissions)
	}
	props, _ := json.Marshal(map[string]any{"requestID": "perm_1", "reply": "once"})
	s = s.Reduce(forgeclient.SSEEvent{Type: "permission.replied", Properties: props})
	if len(s.permissions) != 0 {
		t.Fatalf("permission.replied should clear it, got %+v", s.permissions)
	}
}

func TestPermission_OverlayBlocksAndReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(permEvent(t, "perm_1", "bash", map[string]any{"command": "ls"}))

	if m.pendingPermission() == nil {
		t.Fatal("a pending permission should be present")
	}
	// the overlay renders the action + detail
	view := m.View()
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
	// enter dispatches a reply; the overlay STAYS up (no optimistic clear) until
	// the reply resolves — a failed POST must not silently drop a blocked request.
	m, cmd := step(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("enter should dispatch a reply")
	}
	if m.pendingPermission() == nil || !m.permReplying {
		t.Fatal("overlay should stay up (replying) until the reply resolves")
	}
	// failure keeps the request so the user can retry
	mFail, _ := step(t, m, permissionRepliedMsg{id: "perm_1", err: errTest})
	if mFail.pendingPermission() == nil || mFail.permReplying {
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

func TestPermission_ShortcutKeys(t *testing.T) {
	mk := func() Model {
		m := New(Config{URL: "http://x"})
		m.store = m.store.Reduce(permEvent(t, "perm_1", "edit", nil))
		return m
	}
	for _, kk := range []string{"a", "s", "r"} {
		m := mk()
		_, cmd := m.handlePermissionKey(key(kk))
		if cmd == nil {
			t.Fatalf("%q should dispatch a reply", kk)
		}
	}
	// down moves the selection without replying
	m := mk()
	next, cmd := m.handlePermissionKey(key("down"))
	if cmd != nil || next.(Model).permSel != 1 {
		t.Fatalf("down should move selection to 1 without replying, sel=%d", next.(Model).permSel)
	}
}
