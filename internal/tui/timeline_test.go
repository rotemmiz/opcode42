package tui

import (
	"strings"
	"testing"
)

func TestTimelineItems_UserTurnsWithTitle(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
		{ID: "msg_2", SessionID: "ses_1", Role: "assistant"},
		{ID: "msg_3", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "first prompt\nmore"}}
	m.store.parts["msg_3"] = []Part{{ID: "p3", Type: "text", Text: "second prompt"}}

	items := m.timelineItems()
	if len(items) != 2 {
		t.Fatalf("want 2 user turns, got %d", len(items))
	}
	if items[0].title != "first prompt" || items[0].messageID != "msg_1" {
		t.Fatalf("first item wrong: %+v", items[0])
	}
	if items[1].title != "second prompt" {
		t.Fatalf("second item wrong: %+v", items[1])
	}
}

func TestTimelineModal_SelectReverts(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.messages["ses_1"] = []Message{{ID: "msg_1", SessionID: "ses_1", Role: "user"}}
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "hi"}}
	m.modal, m.modalSel = modalTimeline, 0
	next, cmd := m.modalSelect()
	if cmd == nil {
		t.Fatal("selecting a timeline turn should dispatch a revert")
	}
	if next.(Model).modal != modalNone {
		t.Fatal("select should close the modal")
	}
}

func TestStatusLines_ContainsDiagnostics(t *testing.T) {
	m := New(Config{URL: "http://h:1", Provider: "google", Model: "gemini", SessionID: "ses_1"})
	lines := strings.Join(m.statusLines(), "\n")
	for _, want := range []string{"daemon", "h:1", "state", "model", "gemini", "agent", "build", "theme", "forge-dark", "session", "ses_1"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("status lines missing %q:\n%s", want, lines)
		}
	}
}

func TestStatusModal_EnterCloses(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.modal, m.modalSel = modalStatus, 0
	next, _ := m.modalSelect()
	if next.(Model).modal != modalNone {
		t.Fatal("enter should close the read-only status modal")
	}
}

func TestSlash_TimelineAndStatusBuiltins(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("/timeline")
	m, _ = m.refreshAutocomplete()
	if n, _ := m.acceptSlash(); n.(Model).modal != modalTimeline {
		t.Fatal("/timeline should open the timeline modal")
	}

	m2 := New(Config{URL: "http://x"})
	m2.input.SetValue("/status")
	m2, _ = m2.refreshAutocomplete()
	if n, _ := m2.acceptSlash(); n.(Model).modal != modalStatus {
		t.Fatal("/status should open the status modal")
	}
}

func TestPalette_HasTimelineAndStatus(t *testing.T) {
	var hasT, hasS bool
	for _, it := range paletteItems {
		switch it.action {
		case paTimeline:
			hasT = true
		case paStatus:
			hasS = true
		}
	}
	if !hasT || !hasS {
		t.Fatal("palette should include Timeline + Status entries")
	}
}
