package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFilterSlash_BuiltinsFirstByPrefix(t *testing.T) {
	daemon := []slashItem{{name: "/init", kind: slashDaemon}, {name: "/newthing", kind: slashDaemon}}

	got := filterSlash("new", daemon)
	if len(got) != 2 || got[0].name != "/new" || got[1].name != "/newthing" {
		t.Fatalf("filterSlash(new) should be [/new (builtin), /newthing (daemon)], got %+v", got)
	}
	if all := filterSlash("", daemon); len(all) != len(builtinCommands)+len(daemon) {
		t.Fatalf("empty query should list everything, got %d", len(all))
	}
	if none := filterSlash("zzz", daemon); len(none) != 0 {
		t.Fatalf("no-match query should be empty, got %+v", none)
	}
}

func TestSlashArguments(t *testing.T) {
	cases := map[string]string{
		"/review":          "",
		"/review pr":       "pr",
		"/review  pr main": "pr main",
		"/init":            "",
		"/cmd\targ":        "arg",
	}
	for in, want := range cases {
		if got := slashArguments(in); got != want {
			t.Errorf("slashArguments(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefreshAutocomplete_OpenFilterClose(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.commands = []slashItem{{name: "/init", desc: "setup", kind: slashDaemon}}

	m.input.SetValue("/")
	m, _ = m.refreshAutocomplete()
	if !m.ac.open || len(m.ac.items) == 0 {
		t.Fatal("typing / should open the popup with all commands")
	}

	m.input.SetValue("/mod")
	m, _ = m.refreshAutocomplete()
	if !m.ac.open || m.ac.items[0].name != "/models" {
		t.Fatalf("/mod should filter to /models, got %+v", m.ac.items)
	}

	m.input.SetValue("hello")
	m, _ = m.refreshAutocomplete()
	if m.ac.open {
		t.Fatal("non-slash text should close the popup")
	}

	// Arguments after the command word keep the popup on that command.
	m.input.SetValue("/init now")
	m, _ = m.refreshAutocomplete()
	if !m.ac.open || m.ac.items[0].name != "/init" {
		t.Fatalf("/init now should still match /init, got %+v", m.ac.items)
	}
}

func TestSlash_TypeNavTabEsc(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m, _ = step(t, m, key("/")) // opens; sel 0 = /new
	if !m.ac.open {
		t.Fatal("typing / should open the popup")
	}
	before := m.ac.sel
	m, _ = step(t, m, key("down"))
	if m.ac.sel != before+1 {
		t.Fatalf("down should move selection, got %d", m.ac.sel)
	}
	m, _ = step(t, m, key("up"))
	if m.ac.sel != before {
		t.Fatalf("up should move selection back, got %d", m.ac.sel)
	}
	m, _ = step(t, m, key("tab")) // complete the selected /new
	if m.input.Value() != "/new " || m.ac.open {
		t.Fatalf("tab should complete to %q and close the popup, got %q open=%v", "/new ", m.input.Value(), m.ac.open)
	}
}

func TestSlash_EnterRunsBuiltinModels(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("/models")
	m, _ = m.refreshAutocomplete()
	next, cmd := step(t, m, key("enter"))
	if next.modal != modalModels || cmd == nil {
		t.Fatalf("/models enter should open the model switcher, modal=%v cmd=%v", next.modal, cmd != nil)
	}
	if next.ac.open || next.input.Value() != "" {
		t.Fatal("accepting should close the popup and clear the composer")
	}
}

func TestSlash_EnterRunsDaemonCommand(t *testing.T) {
	// with a session → runs directly
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.commands = []slashItem{{name: "/review", kind: slashDaemon}}
	m.input.SetValue("/review pr")
	m, _ = m.refreshAutocomplete()
	next, cmd := m.acceptSlash()
	if cmd == nil {
		t.Fatal("accepting a daemon command with a session should dispatch the run")
	}
	if nm := next.(Model); nm.ac.open || nm.input.Value() != "" {
		t.Fatal("accept should close the popup and clear the composer")
	}

	// no session → creates one first (still dispatches)
	m2 := New(Config{URL: "http://x"})
	m2.commands = []slashItem{{name: "/review", kind: slashDaemon}}
	m2.input.SetValue("/review")
	m2, _ = m2.refreshAutocomplete()
	if _, cmd := m2.acceptSlash(); cmd == nil {
		t.Fatal("a daemon command with no session should create one then run")
	}
}

func TestAutocompleteView_EmptyWhenClosed(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if m.autocompleteView() != "" {
		t.Fatal("popup view should be empty when closed")
	}
	m.input.SetValue("/")
	m, _ = m.refreshAutocomplete()
	if m.autocompleteView() == "" {
		t.Fatal("popup view should render when open")
	}
}
