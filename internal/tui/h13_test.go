package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests for plan 08f H13 (G.15 — TUI config file resolution).

func TestH13_Parse_NestedTuiKey(t *testing.T) {
	cfg, err := parseTUIConfigJSON([]byte(`{
		"tui": {
			"theme": "catppuccin",
			"scroll_speed": 2,
			"mouse": false,
			"diff_style": "stacked",
			"leader_timeout": 1500,
			"prompt": { "max_height": 10 }
		},
		"keybinds": { "session_new": "ctrl+n" }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "catppuccin" {
		t.Fatalf("theme=%q", cfg.Theme)
	}
	if cfg.ScrollSpeed == nil || *cfg.ScrollSpeed != 2 {
		t.Fatalf("scroll_speed=%v", cfg.ScrollSpeed)
	}
	if cfg.Mouse == nil || *cfg.Mouse {
		t.Fatal("mouse should be false")
	}
	if cfg.DiffStyle != "stacked" {
		t.Fatalf("diff_style=%q", cfg.DiffStyle)
	}
	if cfg.LeaderTimeout == nil || *cfg.LeaderTimeout != 1500 {
		t.Fatalf("leader_timeout=%v", cfg.LeaderTimeout)
	}
	if cfg.Prompt == nil || cfg.Prompt.MaxHeight == nil || *cfg.Prompt.MaxHeight != 10 {
		t.Fatalf("prompt=%+v", cfg.Prompt)
	}
	if cfg.Keybinds["session_new"] != "ctrl+n" {
		t.Fatalf("keybinds=%v", cfg.Keybinds)
	}
}

func TestH13_Parse_JSONCComments(t *testing.T) {
	cfg, err := parseTUIConfigJSON([]byte(`{
		// theme override
		"theme": "nord",
		"scroll_speed": 1
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "nord" {
		t.Fatalf("theme=%q", cfg.Theme)
	}
}

func TestH13_ScrollStepFromSpeed(t *testing.T) {
	if got := scrollStepFromSpeed(0); got != defaultScrollStep {
		t.Fatalf("0 → %d", got)
	}
	if got := scrollStepFromSpeed(1); got != defaultScrollStep {
		t.Fatalf("1 → %d", got)
	}
	if got := scrollStepFromSpeed(2); got != 6 {
		t.Fatalf("2 → %d, want 6", got)
	}
	if got := scrollStepFromSpeed(0.1); got != 1 {
		t.Fatalf("0.1 → %d, want 1 (min)", got)
	}
}

func TestH13_ResolvePaths_Override(t *testing.T) {
	got := resolveTUIConfigPaths("/tmp/custom.json", "/proj")
	if len(got) != 1 || got[0] != "/tmp/custom.json" {
		t.Fatalf("override paths = %v", got)
	}
}

func TestH13_ResolvePaths_Discovery(t *testing.T) {
	got := resolveTUIConfigPaths("", "/proj")
	wantSuffixes := []string{
		filepath.Join("opencode", "tui.json"),
		filepath.Join("/proj", "tui.json"),
		filepath.Join("/proj", "opencode.json"),
	}
	for _, suf := range wantSuffixes {
		found := false
		for _, p := range got {
			if filepath.Base(filepath.Dir(p)) == "opencode" && filepath.Base(p) == "tui.json" && suf == wantSuffixes[0] {
				found = true
				break
			}
			if p == suf {
				found = true
				break
			}
		}
		if !found && suf != wantSuffixes[0] {
			t.Fatalf("missing %q in %v", suf, got)
		}
	}
	if len(got) < 4 {
		t.Fatalf("expected discovery paths, got %v", got)
	}
}

func TestH13_Apply_Overlay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.json")
	if err := os.WriteFile(path, []byte(`{
		"theme": "tokyonight",
		"scroll_speed": 2,
		"mouse": false,
		"diff_style": "stacked",
		"leader_timeout": 2500,
		"prompt": { "max_height": 8 },
		"keybinds": { "help_show": "f2" }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withEnv(t, map[string]string{"OPENCODE_DISABLE_MOUSE": ""})
	m := New(Config{URL: "http://x", TUIConfigPath: path})
	if !m.mouseDisabled {
		t.Fatal("mouse:false should disable mouse capture")
	}
	if m.scrollStep != 6 {
		t.Fatalf("scrollStep=%d, want 6", m.scrollStep)
	}
	if !m.diffTreeHidden {
		t.Fatal("diff_style=stacked should hide diff tree")
	}
	if m.cfg.Theme != "tokyonight" {
		t.Fatalf("theme=%q", m.cfg.Theme)
	}
	if m.leaderTimeoutMs != 2500 {
		t.Fatalf("leaderTimeoutMs=%d", m.leaderTimeoutMs)
	}
	if m.composerMaxRows != 8 {
		t.Fatalf("composerMaxRows=%d", m.composerMaxRows)
	}
	if m.tuiKeybinds["help_show"] != "f2" {
		t.Fatalf("keybinds=%v", m.tuiKeybinds)
	}
	if m.scrollLines() != 6 {
		t.Fatalf("scrollLines=%d", m.scrollLines())
	}
}

func TestH13_Apply_EnvMouseWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.json")
	// File says mouse:true, but env disables — env wins (already set before apply).
	if err := os.WriteFile(path, []byte(`{"mouse": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withEnv(t, map[string]string{"OPENCODE_DISABLE_MOUSE": "1"})
	m := New(Config{URL: "http://x", TUIConfigPath: path})
	if !m.mouseDisabled {
		t.Fatal("OPENCODE_DISABLE_MOUSE should win over file mouse:true")
	}
}

func TestH13_Apply_CLIThemeWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.json")
	if err := os.WriteFile(path, []byte(`{"theme": "nord"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(Config{URL: "http://x", Theme: "monochrome", TUIConfigPath: path})
	if m.cfg.Theme != "monochrome" {
		t.Fatalf("CLI --theme should win, got %q", m.cfg.Theme)
	}
}

func TestH13_Merge_LaterWins(t *testing.T) {
	a := tuiFileConfig{Theme: "a"}
	sp := 2.0
	b := tuiFileConfig{Theme: "b", ScrollSpeed: &sp}
	got := mergeTUIFileConfig(a, b)
	if got.Theme != "b" || got.ScrollSpeed == nil || *got.ScrollSpeed != 2 {
		t.Fatalf("merge = %+v", got)
	}
}

func TestH13_MissingFile_NoError(t *testing.T) {
	cfg, err := loadTUIConfigFile(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "" {
		t.Fatalf("empty cfg expected, got %+v", cfg)
	}
}
