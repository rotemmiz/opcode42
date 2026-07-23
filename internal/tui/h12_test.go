package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Tests for plan 08f H12 (G.14 environment flags): OPENCODE_DISABLE_MOUSE,
// OPENCODE_DISABLE_TERMINAL_TITLE (New()-time, not just Restore()),
// OPENCODE_FAST_BOOT, OPENCODE_ROUTE, and OPENCODE_TUI_CONFIG.

func TestH12_DisableMouse_Env(t *testing.T) {
	t.Setenv("OPENCODE_DISABLE_MOUSE", "1")
	m := New(Config{URL: "http://x"})
	if !m.mouseDisabled {
		t.Fatal("OPENCODE_DISABLE_MOUSE set should set mouseDisabled")
	}
	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("View().MouseMode = %v, want MouseModeNone", got)
	}
}

func TestH12_DisableMouse_DefaultOn(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if m.mouseDisabled {
		t.Fatal("mouseDisabled should default false")
	}
	if got := m.View().MouseMode; got != tea.MouseModeAllMotion {
		t.Fatalf("View().MouseMode = %v, want MouseModeAllMotion", got)
	}
}

// TestH12_DisableTerminalTitle_NewWithoutRestore pins the H12 spec item that
// New()/View() must respect OPENCODE_DISABLE_TERMINAL_TITLE even when the
// caller never runs Restore() (h6_test.go already covers the Restore() path).
func TestH12_DisableTerminalTitle_NewWithoutRestore(t *testing.T) {
	t.Setenv("OPENCODE_DISABLE_TERMINAL_TITLE", "1")
	m := New(Config{URL: "http://x"})
	if m.terminalTitleEnabled {
		t.Fatal("OPENCODE_DISABLE_TERMINAL_TITLE should disable terminalTitleEnabled in New()")
	}
	if got := m.windowTitle(); got != "" {
		t.Fatalf("windowTitle() = %q, want empty", got)
	}
	if got := m.View().WindowTitle; got != "" {
		t.Fatalf("View().WindowTitle = %q, want empty", got)
	}
}

func TestH12_FastBoot_WithSession_SkipsSplash(t *testing.T) {
	t.Setenv("OPENCODE_FAST_BOOT", "1")
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	if !m.fastBoot {
		t.Fatal("fastBoot should be true")
	}
	if m.screen != ScreenSession {
		t.Fatalf("screen = %v, want ScreenSession", m.screen)
	}
	if m.view.bgPulse {
		t.Fatal("bg-pulse should be off once fast-boot lands on ScreenSession")
	}
}

func TestH12_FastBoot_WithoutSession_FreezesAnimation(t *testing.T) {
	t.Setenv("OPENCODE_FAST_BOOT", "1")
	m := New(Config{URL: "http://x"})
	if m.screen != ScreenSplash {
		t.Fatalf("screen = %v, want ScreenSplash (no session id to jump to)", m.screen)
	}
	if !m.noAnim {
		t.Fatal("fast-boot without a session id should freeze the splash animation (noAnim=true)")
	}
}

func TestH12_FastBoot_Disabled_NoEffect(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	if m.fastBoot {
		t.Fatal("fastBoot should default false")
	}
	if m.screen != ScreenSplash {
		t.Fatalf("screen = %v, want ScreenSplash (fast-boot not requested)", m.screen)
	}
	if m.noAnim {
		t.Fatal("noAnim should default false without --no-anim or fast-boot")
	}
}

func TestH12_Route_Home_ForcesSplashAndClearsSession(t *testing.T) {
	t.Setenv("OPENCODE_ROUTE", "home")
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	if m.screen != ScreenSplash {
		t.Fatalf("screen = %v, want ScreenSplash", m.screen)
	}
	if m.cfg.SessionID != "" {
		t.Fatalf("SessionID = %q, want cleared by OPENCODE_ROUTE=home", m.cfg.SessionID)
	}
}

func TestH12_Route_Session_OpensConfiguredSession(t *testing.T) {
	t.Setenv("OPENCODE_ROUTE", "session")
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	if m.screen != ScreenSession {
		t.Fatalf("screen = %v, want ScreenSession", m.screen)
	}
	if m.cfg.SessionID != "ses_1" {
		t.Fatalf("SessionID = %q, want ses_1", m.cfg.SessionID)
	}
}

func TestH12_Route_Session_NoConfiguredSession_StaysSplash(t *testing.T) {
	t.Setenv("OPENCODE_ROUTE", "session")
	m := New(Config{URL: "http://x"})
	if m.screen != ScreenSplash {
		t.Fatalf("screen = %v, want ScreenSplash (no session id known)", m.screen)
	}
}

func TestH12_Route_LiteralSessionID(t *testing.T) {
	t.Setenv("OPENCODE_ROUTE", "ses_route_target")
	m := New(Config{URL: "http://x"})
	if m.screen != ScreenSession {
		t.Fatalf("screen = %v, want ScreenSession", m.screen)
	}
	if m.cfg.SessionID != "ses_route_target" {
		t.Fatalf("SessionID = %q, want ses_route_target", m.cfg.SessionID)
	}
}

// TestH12_Route_OverridesFastBoot pins the documented precedence: OPENCODE_ROUTE
// is the more specific ask and wins over OPENCODE_FAST_BOOT / --session.
func TestH12_Route_OverridesFastBoot(t *testing.T) {
	t.Setenv("OPENCODE_FAST_BOOT", "1")
	t.Setenv("OPENCODE_ROUTE", "home")
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	if m.screen != ScreenSplash {
		t.Fatalf("screen = %v, want ScreenSplash (route=home overrides fast-boot)", m.screen)
	}
	if m.cfg.SessionID != "" {
		t.Fatalf("SessionID = %q, want cleared", m.cfg.SessionID)
	}
}

// TestH12_TUIConfigPath_StoredOnConfig pins the G.14 "plumb onto Config"
// requirement: OPENCODE_TUI_CONFIG has no consumer yet (that's H13 / G.15),
// but the value must round-trip through Config so main.go's env read has
// somewhere to land.
func TestH12_TUIConfigPath_StoredOnConfig(t *testing.T) {
	m := New(Config{URL: "http://x", TUIConfigPath: "/tmp/opencode.json"})
	if m.cfg.TUIConfigPath != "/tmp/opencode.json" {
		t.Fatalf("cfg.TUIConfigPath = %q, want /tmp/opencode.json", m.cfg.TUIConfigPath)
	}
}
