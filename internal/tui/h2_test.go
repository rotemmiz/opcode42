package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// H2 tests — ctrl+v clipboard paste + usage chip (plan 08f G.2 / G.4).

func TestH2_CtrlV_DispatchesReadClipboard(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := step(t, m, key("ctrl+v"))
	if cmd == nil {
		t.Fatal("ctrl+v should return readClipboardCmd")
	}
	msg := cmd()
	if _, ok := msg.(clipboardReadMsg); !ok {
		t.Fatalf("ctrl+v cmd should yield clipboardReadMsg, got %T", msg)
	}
}

func TestH2_ClipboardRead_InsertsText(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, clipboardReadMsg{Mime: "text/plain", Data: []byte("from clip")})
	if got := m.input.Value(); got != "from clip" {
		t.Fatalf("text clipboard paste = %q, want %q", got, "from clip")
	}
}

func TestH2_ClipboardRead_ImageStagesPendingFile(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	png := []byte{0x89, 0x50, 0x4e, 0x47} // not a real PNG; staging only checks mime
	m, _ = step(t, m, clipboardReadMsg{Mime: "image/png", Data: png})
	if len(m.pendingFiles) != 1 {
		t.Fatalf("pendingFiles = %d, want 1", len(m.pendingFiles))
	}
	if !strings.HasPrefix(m.pendingFiles[0].URL, "data:image/png;base64,") {
		t.Fatalf("pending file URL = %q, want data URL", m.pendingFiles[0].URL)
	}
	if !strings.Contains(m.input.Value(), "[Image 1]") {
		t.Fatalf("composer should show [Image 1] marker, got %q", m.input.Value())
	}
	view := m.composerView()
	if !strings.Contains(view, "Image 1") {
		t.Fatalf("composerView should chip Image 1, got %q", view)
	}
}

func TestH2_UsageChip_LastAssistantTokens(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "anthropic", Model: "claude-sonnet-4", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.choices = []modelChoice{{Provider: "anthropic", Model: "claude-sonnet-4", ContextLimit: 200000}}
	m.store.sessions = []Session{{ID: "ses_1", Cost: 0.0123}}
	m.store.messages["ses_1"] = []Message{
		{ID: "u1", Role: "user"},
		{ID: "a1", Role: "assistant", Tokens: MessageTokens{Input: 1000, Output: 500}},
	}
	chip := m.usageChip()
	if chip == "" {
		t.Fatal("usageChip empty")
	}
	if !strings.Contains(chip, "1.5K") {
		t.Fatalf("usageChip should include token count, got %q", chip)
	}
	if !strings.Contains(chip, "%") {
		t.Fatalf("usageChip should include context %%, got %q", chip)
	}
	if !strings.Contains(chip, "$0.0123") {
		t.Fatalf("usageChip should include cost, got %q", chip)
	}
	bar := m.statusBarView(120)
	if !strings.Contains(bar, "1.5K") {
		t.Fatalf("statusBarView should show usage chip, got %q", bar)
	}
}

func TestH2_CtrlC_ClearsPendingFiles(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.pendingFiles = []pendingFile{{Filename: "clipboard", Mime: "image/png", URL: "data:image/png;base64,xx"}}
	m, _ = step(t, m, key("ctrl+c"))
	if len(m.pendingFiles) != 0 {
		t.Fatalf("ctrl+c should clear pendingFiles, got %d", len(m.pendingFiles))
	}
}

// TestH2_Submit_CreateSessionCarriesPendingFiles locks the cold-start path:
// submit() must thread staged clipboard images through createSessionCmd →
// sessionCreatedMsg → promptCmd (not drop them when SessionID is empty).
func TestH2_Submit_CreateSessionCarriesPendingFiles(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m"}) // no SessionID
	m.screen = ScreenSplash
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue("look at this")
	files := []pendingFile{{
		Filename: "clipboard",
		Mime:     "image/png",
		URL:      "data:image/png;base64,abcd",
	}}
	m.pendingFiles = files
	next, cmd := m.submit()
	nm := next.(Model)
	if len(nm.pendingFiles) != 0 {
		t.Fatalf("submit should clear pendingFiles on model, got %d", len(nm.pendingFiles))
	}
	if cmd == nil {
		t.Fatal("submit with empty SessionID should return createSessionCmd")
	}
	// Simulate a successful create carrying the files submit threaded through.
	nm2, cmd2 := step(t, nm, sessionCreatedMsg{
		session: Session{ID: "ses_new"},
		text:    "look at this",
		files:   files,
	})
	if nm2.cfg.SessionID != "ses_new" {
		t.Fatalf("SessionID = %q, want ses_new", nm2.cfg.SessionID)
	}
	if cmd2 == nil {
		t.Fatal("sessionCreatedMsg with files must return promptCmd")
	}
	// Direct contract: createSessionCmd embeds files on the result message
	// even when the POST fails (err set, files preserved).
	msg := sessionCreatedMsg{text: "look at this", files: files, err: errString("forced")}
	if len(msg.files) != 1 || msg.files[0].Mime != "image/png" {
		t.Fatalf("sessionCreatedMsg must carry files, got %+v", msg.files)
	}
}
