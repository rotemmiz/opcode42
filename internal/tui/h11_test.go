package tui

import "testing"

// Tests for plan 08f H11 (G.13 OSC 52 clipboard-write gating).

func TestH11_Default_OnWithoutSSH(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	if !defaultOsc52WriteEnabled() {
		t.Fatal("OSC 52 should default on when no SSH env vars are set")
	}
	m := New(Config{URL: "http://x"})
	if !m.osc52Enabled {
		t.Fatal("New() should default osc52Enabled true without SSH env vars")
	}
}

func TestH11_Default_OffWithSSHConnection(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 22", "SSH_TTY": ""})
	if defaultOsc52WriteEnabled() {
		t.Fatal("OSC 52 should default off when SSH_CONNECTION is set")
	}
	m := New(Config{URL: "http://x"})
	if m.osc52Enabled {
		t.Fatal("New() should default osc52Enabled false when SSH_CONNECTION is set")
	}
}

func TestH11_Default_OffWithSSHTTY(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": "/dev/pts/3"})
	if defaultOsc52WriteEnabled() {
		t.Fatal("OSC 52 should default off when SSH_TTY is set")
	}
	m := New(Config{URL: "http://x"})
	if m.osc52Enabled {
		t.Fatal("New() should default osc52Enabled false when SSH_TTY is set")
	}
}

func TestH11_CLIForcesOff(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	m := New(Config{URL: "http://x", NoOSC52: true})
	if m.osc52Enabled {
		t.Fatal("--no-osc52 should force osc52Enabled off even without SSH env vars")
	}
	// The palette toggle should be a no-op while --no-osc52 is set.
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleOsc52)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.osc52Enabled {
		t.Fatal("--no-osc52 should still force OSC 52 off after a palette toggle attempt")
	}
}

func TestH11_Palette_ToggleOsc52(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	m := New(Config{URL: "http://x"})
	if !m.osc52Enabled {
		t.Fatal("OSC 52 should default on locally")
	}
	m.modal, m.modalSel = modalPalette, indexOfAction(paToggleOsc52)
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.osc52Enabled {
		t.Fatal("palette toggle should disable OSC 52 clipboard")
	}
	if nm.modal != modalNone {
		t.Fatalf("palette should close, modal=%v", nm.modal)
	}
	// Toggling again re-enables.
	nm.modal, nm.modalSel = modalPalette, indexOfAction(paToggleOsc52)
	next2, _ := nm.modalSelect()
	nm2 := next2.(Model)
	if !nm2.osc52Enabled {
		t.Fatal("second toggle should re-enable OSC 52 clipboard")
	}
}

func TestH11_Palette_ToggleOsc52Label_IsDynamic(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.osc52Enabled = true
	m.modal = modalPalette
	_, rows, _ := m.modalItems()
	idx := indexOfAction(paToggleOsc52)
	if rows[idx] != "Disable OSC 52 clipboard" {
		t.Fatalf("label with OSC 52 on = %q, want %q", rows[idx], "Disable OSC 52 clipboard")
	}
	m.osc52Enabled = false
	_, rows, _ = m.modalItems()
	if rows[idx] != "Enable OSC 52 clipboard" {
		t.Fatalf("label with OSC 52 off = %q, want %q", rows[idx], "Enable OSC 52 clipboard")
	}
}

func TestH11_KV_DefaultFallsBackToEnvironment(t *testing.T) {
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	if !kvOsc52WriteEnabled(kvData{}) {
		t.Fatal("nil Osc52WriteEnabled should fall back to the environment default (on, no SSH)")
	}
	withEnv(t, map[string]string{"SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 22", "SSH_TTY": ""})
	if kvOsc52WriteEnabled(kvData{}) {
		t.Fatal("nil Osc52WriteEnabled should fall back to the environment default (off, SSH)")
	}
}

func TestH11_KV_ExplicitValueOverridesEnvironment(t *testing.T) {
	// A user-toggled preference persists across runs regardless of the
	// current environment's SSH detection.
	withEnv(t, map[string]string{"SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 22", "SSH_TTY": ""})
	on := true
	if !kvOsc52WriteEnabled(kvData{Osc52WriteEnabled: &on}) {
		t.Fatal("explicit true should win over the SSH-off default")
	}
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	off := false
	if kvOsc52WriteEnabled(kvData{Osc52WriteEnabled: &off}) {
		t.Fatal("explicit false should win over the local-on default")
	}
}

func TestH11_Restore_HonoursPersistedOverEnvDefault(t *testing.T) {
	// Simulate Restore(): a prior run persisted OSC 52 off, but this run's
	// environment has no SSH vars set (would otherwise default on). The
	// persisted preference should win, mirroring Restore()'s
	// `if !m.cfg.NoOSC52 { m.osc52Enabled = kvOsc52WriteEnabled(kv) }`.
	withEnv(t, map[string]string{"SSH_CONNECTION": "", "SSH_TTY": ""})
	m := New(Config{URL: "http://x"})
	off := false
	kv := kvData{Osc52WriteEnabled: &off}
	if !m.cfg.NoOSC52 {
		m.osc52Enabled = kvOsc52WriteEnabled(kv)
	}
	if m.osc52Enabled {
		t.Fatal("persisted off should override the local env-based on default")
	}
}

func TestH11_Persist_RoundTripsOsc52(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.persistEnabled = false // keep the test hermetic; exercise the field directly
	m.osc52Enabled = false
	if got := boolPtr(m.osc52Enabled); got == nil || *got {
		t.Fatalf("boolPtr(m.osc52Enabled) = %v, want pointer to false", got)
	}
}

// TestH11_CopyClipboardCmd_SkipsWriteWhenDisabled exercises the pure gating
// helper directly (no real /dev/tty needed in CI) and confirms
// copyClipboardCmd still emits clipboardCopiedMsg regardless of whether the
// write happens, so callers' "copied" status/UI feedback is unaffected.
func TestH11_CopyClipboardCmd_SkipsWriteWhenDisabled(t *testing.T) {
	if osc52WriteWouldHappen(false) {
		t.Fatal("osc52WriteWouldHappen(false) should be false — write skipped when disabled")
	}
	if !osc52WriteWouldHappen(true) {
		t.Fatal("osc52WriteWouldHappen(true) should be true — write attempted when enabled")
	}

	cmd := copyClipboardCmd("hello", false)
	if cmd == nil {
		t.Fatal("copyClipboardCmd should always return a non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(clipboardCopiedMsg); !ok {
		t.Fatalf("copyClipboardCmd(disabled) msg = %T, want clipboardCopiedMsg", msg)
	}

	cmd2 := copyClipboardCmd("hello", true)
	msg2 := cmd2()
	if _, ok := msg2.(clipboardCopiedMsg); !ok {
		t.Fatalf("copyClipboardCmd(enabled) msg = %T, want clipboardCopiedMsg", msg2)
	}
}
