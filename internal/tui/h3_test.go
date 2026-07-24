package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestH3_SmartPaste_StagesLargePaste(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	big := "line1\nline2\nline3\nline4"
	m, _ = step(t, m, tea.PasteMsg{Content: big})
	if len(m.pasteParts) != 1 {
		t.Fatalf("pasteParts = %d, want 1", len(m.pasteParts))
	}
	if m.pasteParts[0].Text != big {
		t.Fatalf("staged text = %q", m.pasteParts[0].Text)
	}
	if m.input.Value() != "" {
		t.Fatalf("textarea should stay empty on smart paste, got %q", m.input.Value())
	}
	view := m.composerView()
	if !strings.Contains(view, "[Pasted ~4 lines]") {
		t.Fatalf("composerView missing paste chip, got %q", view)
	}
}

func TestH3_SmartPaste_SmallPasteInserts(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, tea.PasteMsg{Content: "hi"})
	if len(m.pasteParts) != 0 {
		t.Fatalf("small paste should not stage, got %d", len(m.pasteParts))
	}
	if m.input.Value() != "hi" {
		t.Fatalf("small paste should insert, got %q", m.input.Value())
	}
}

func TestH3_SmartPaste_DisabledInsertsFull(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.pasteSummaryEnabled = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	big := "a\nb\nc\nd"
	m, _ = step(t, m, tea.PasteMsg{Content: big})
	if len(m.pasteParts) != 0 {
		t.Fatal("disabled summary should not stage")
	}
	if m.input.Value() != big {
		t.Fatalf("disabled summary should insert full text, got %q", m.input.Value())
	}
}

func TestH3_Submit_ExpandsPasteParts(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.pasteParts = []pastePart{{Text: "one\ntwo\nthree", Lines: 3}}
	m.input.SetValue("and more")
	if got := m.composeSubmitText(); got != "one\ntwo\nthree\nand more" {
		t.Fatalf("composeSubmitText = %q", got)
	}
	next, cmd := m.submit()
	nm := next.(Model)
	if len(nm.pasteParts) != 0 {
		t.Fatal("submit should clear pasteParts")
	}
	if cmd == nil {
		t.Fatal("submit should dispatch promptCmd")
	}
}

func TestH3_CtrlC_ClearsPasteParts(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.pasteParts = []pastePart{{Text: "a\nb\nc", Lines: 3}}
	m, _ = step(t, m, key("ctrl+c"))
	if len(m.pasteParts) != 0 {
		t.Fatal("ctrl+c should clear pasteParts")
	}
}

func TestH3_LongSingleLine_Stages(t *testing.T) {
	m := New(Config{URL: "http://x"})
	long := strings.Repeat("x", 151)
	m, _ = m.maybeSmartPaste(long)
	if len(m.pasteParts) != 1 || m.pasteParts[0].Lines != 1 {
		t.Fatalf("151-char line should stage as 1-line paste, got %+v", m.pasteParts)
	}
}
