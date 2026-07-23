package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Tests for plan 08f H8 (docs.open — leftover after H1a shipped /export+/copy).

func TestH8_BrowserCommand(t *testing.T) {
	name, args := browserCommand("darwin", docsURL)
	if name != "open" || len(args) != 1 || args[0] != docsURL {
		t.Fatalf("darwin: got %q %v", name, args)
	}
	name, args = browserCommand("linux", docsURL)
	if name != "xdg-open" || len(args) != 1 || args[0] != docsURL {
		t.Fatalf("linux: got %q %v", name, args)
	}
	name, args = browserCommand("windows", docsURL)
	if name != "rundll32" || len(args) != 2 || args[1] != docsURL {
		t.Fatalf("windows: got %q %v", name, args)
	}
	name, _ = browserCommand("plan9", docsURL)
	if name != "" {
		t.Fatalf("unsupported GOOS should return empty name, got %q", name)
	}
}

func TestH8_SlashDocs_DispatchesOpen(t *testing.T) {
	var opened string
	prev := openURLFn
	openURLFn = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openURLFn = prev })

	m := New(Config{URL: "http://x"})
	m.input.SetValue("/docs")
	m.ac = autocomplete{open: true, mode: acSlash, items: filterSlash("docs", nil), sel: 0}
	next, cmd := m.acceptSlash()
	if cmd == nil {
		t.Fatal("/docs should dispatch openDocsCmd")
	}
	msg := cmd()
	got, ok := msg.(openURLDoneMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want openURLDoneMsg", msg)
	}
	if got.URL != docsURL || got.Err != nil {
		t.Fatalf("openURLDoneMsg = %+v", got)
	}
	if opened != docsURL {
		t.Fatalf("opened %q, want %s", opened, docsURL)
	}
	nm := next.(Model)
	nm, _ = step(t, nm, got)
	if !strings.Contains(nm.status, docsURL) {
		t.Fatalf("status after open = %q", nm.status)
	}
}

func TestH8_Palette_OpenDocs(t *testing.T) {
	var opened string
	prev := openURLFn
	openURLFn = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openURLFn = prev })

	m := New(Config{URL: "http://x"})
	m.modal, m.modalSel = modalPalette, indexOfAction(paDocs)
	next, cmd := m.modalSelect()
	if cmd == nil {
		t.Fatal("Open docs palette entry should dispatch openDocsCmd")
	}
	msg := cmd()
	got, ok := msg.(openURLDoneMsg)
	if !ok || got.URL != docsURL {
		t.Fatalf("cmd() = %#v", msg)
	}
	_ = next
	if opened != docsURL {
		t.Fatalf("opened %q", opened)
	}
}

func TestH8_SlashDocs_Listed(t *testing.T) {
	found := false
	for _, it := range builtinCommands {
		if it.name == "/docs" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/docs missing from builtinCommands")
	}
	if indexOfAction(paDocs) < 0 {
		t.Fatal("paDocs missing from paletteItems")
	}
}

func TestH8_OpenURLDone_ErrorStatus(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, openURLDoneMsg{URL: docsURL, Err: errString("no browser")})
	if !strings.Contains(m.status, "open docs:") || !strings.Contains(m.status, "no browser") {
		t.Fatalf("status = %q", m.status)
	}
}
