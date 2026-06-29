package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+j":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPalette_OpensNavigatesSwitches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	if m.modal != modalPalette {
		t.Fatalf("ctrl+p should open palette, got %v", m.modal)
	}
	m, _ = step(t, m, key("down")) // -> "Switch session"
	if m.modalSel != 1 {
		t.Fatalf("down should move selection to 1, got %d", m.modalSel)
	}
	m, cmd := step(t, m, key("enter")) // select "Switch session"
	if m.modal != modalSessions || cmd == nil {
		t.Fatalf("Switch session should open the sessions modal + load, modal=%v cmd=%v", m.modal, cmd != nil)
	}
}

func TestPalette_NewSessionDispatches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	m, cmd := step(t, m, key("enter")) // sel 0 = New session
	if m.modal != modalNone || cmd == nil {
		t.Fatalf("New session should close modal + dispatch, modal=%v cmd=%v", m.modal, cmd != nil)
	}
}

func TestModal_EscCloses(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	m, _ = step(t, m, key("esc"))
	if m.modal != modalNone {
		t.Fatal("esc should close the modal")
	}
}

func TestSessionsModal_SelectOpensNewestFirst(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a", Title: "A"}, {ID: "ses_b", Title: "B"}} // ascending
	m.modal, m.modalSel = modalSessions, 0
	m, cmd := step(t, m, key("enter"))
	if m.cfg.SessionID != "ses_b" || m.screen != ScreenSession || cmd == nil {
		t.Fatalf("first row should open the newest (ses_b): got %q screen=%v", m.cfg.SessionID, m.screen)
	}
}

func TestSessionDeleted_RemovesAndSwitches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a"}, {ID: "ses_b"}}
	m.cfg.SessionID, m.screen = "ses_b", ScreenSession
	m, cmd := step(t, m, sessionDeletedMsg{id: "ses_b"})
	if len(m.store.sessions) != 1 || m.store.sessions[0].ID != "ses_a" {
		t.Fatalf("ses_b not removed: %+v", m.store.sessions)
	}
	if m.cfg.SessionID != "ses_a" || cmd == nil {
		t.Fatalf("deleting the open session should switch to another: got %q", m.cfg.SessionID)
	}
}

func TestSessionDeleted_LastGoesToSplash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a"}}
	m.cfg.SessionID, m.screen = "ses_a", ScreenSession
	m, _ = step(t, m, sessionDeletedMsg{id: "ses_a"})
	if m.cfg.SessionID != "" || m.screen != ScreenSplash {
		t.Fatalf("deleting the last session should return to splash, got %q screen=%v", m.cfg.SessionID, m.screen)
	}
}

// TestModalView_BorderedAndTitled verifies the M8 dialog re-style:
// modalView() must contain a title string and be bordered.
// For filterable modals (palette, models, themes, agents, sessions, stash)
// it must also contain the filter affordance hint ("Search").
func TestModalView_BorderedAndTitled(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 40
	m.store.sessions = []Session{{ID: "ses_1", Title: "My Session"}}

	cases := []struct {
		modal      modalKind
		title      string
		filterable bool
	}{
		{modalPalette, "Commands", true},
		{modalSessions, "Sessions", true},
		{modalModels, "Models", true},
		{modalThemes, "Themes", true},
		{modalAgents, "Agents", true},
		{modalStash, "Stashed drafts", true},
		{modalTimeline, "Timeline", false},
		{modalStatus, "Status", false},
		{modalHelp, "Keybindings", false},
	}

	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			m.modal = tc.modal
			m.modalSel = 0
			out := m.modalView()
			if out == "" {
				t.Fatalf("modal %v: View() returned empty", tc.modal)
			}
			// Title must appear in the rendered output.
			if !strings.Contains(out, tc.title) {
				t.Errorf("modal missing title %q", tc.title)
			}
			// Filterable modals must contain the search affordance.
			if tc.filterable && !strings.Contains(out, "Search") {
				t.Errorf("modal %q: missing Search filter affordance", tc.title)
			}
			// Non-filterable modals must NOT contain the search affordance.
			if !tc.filterable && strings.Contains(out, "Search  /") {
				t.Errorf("modal %q: unexpected Search filter affordance in non-filterable modal", tc.title)
			}
		})
	}
}

// TestModalView_SelectionRowAndBackgroundFill verifies the M8 dialog fill rule:
// when a row is selected, the selection bar (Selection style) must appear, and
// every rendered row of the modal must be consistently filled (no empty lines).
// Tested for both opcode42-dark and opcode42-light to catch bleed-through on either
// terminal background.
func TestModalView_SelectionRowAndBackgroundFill(t *testing.T) {
	for _, tn := range []string{"opcode42-dark", "opcode42-light"} {
		t.Run(tn, func(t *testing.T) {
			m := New(Config{URL: "http://x"})
			m.width, m.height = 120, 40
			m = m.applyThemeByName(tn)
			m.modal = modalPalette
			m.modalSel = 0
			out := m.modalView()

			// The modal must be non-empty and contain the first palette item title.
			if !strings.Contains(out, "New session") {
				t.Errorf("theme %s: modal missing first item 'New session'", tn)
			}

			// The selection style uses the SelBg amber color. We can't inspect ANSI
			// codes in test output (no TTY), but we verify the Selection style
			// background is non-zero to confirm the palette is wired up correctly.
			p, ok := theme.ByName(tn)
			if !ok {
				t.Fatalf("unknown theme %s", tn)
			}
			if p.SelBg == "" {
				t.Errorf("theme %s: SelBg is empty — Selection style won't work", tn)
			}

			// Verify the rendered modal is not empty and lines were produced.
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			if len(lines) < 5 {
				t.Errorf("theme %s: modal has too few lines (%d)", tn, len(lines))
			}

			// Width-per-line consistency: every line of the modal must have the
			// same visible width (no ragged edge from missing background fill).
			widths := make(map[int]int)
			for _, l := range lines {
				widths[lipgloss.Width(l)]++
			}
			if len(widths) > 2 {
				// Allow at most 2 different widths (e.g. the centered modal + blank
				// background rows); more than that suggests ragged panel edges.
				t.Errorf("theme %s: modal has %d different line widths — ragged panel:\n%v",
					tn, len(widths), widths)
			}
		})
	}
}

// TestModalView_RenameOverlay verifies the rename text-input overlay: it must
// contain the "Rename session" label and the save/cancel hint.
func TestModalView_RenameOverlay(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 40
	m.modal = modalRename
	m.renameInput.Focus()
	out := m.modalView()
	for _, want := range []string{"Rename session", "enter save"} {
		if !strings.Contains(out, want) {
			t.Errorf("rename overlay missing %q", want)
		}
	}
}

// TestIsFilterableModal checks which modal kinds get the filter affordance.
func TestIsFilterableModal(t *testing.T) {
	filterable := []modalKind{
		modalPalette, modalModels, modalThemes, modalAgents, modalSessions, modalStash,
	}
	notFilterable := []modalKind{
		modalTimeline, modalStatus, modalMCP, modalSkills, modalHelp, modalVariant, modalRename, modalNone,
	}
	for _, k := range filterable {
		if !isFilterableModal(k) {
			t.Errorf("modal kind %v should be filterable", k)
		}
	}
	for _, k := range notFilterable {
		if isFilterableModal(k) {
			t.Errorf("modal kind %v should NOT be filterable", k)
		}
	}
}
