package tui

import (
	"encoding/json"
	"testing"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func TestH16_ToastShow(t *testing.T) {
	m := New(Config{URL: "http://x"})
	props, _ := json.Marshal(map[string]any{"message": "hello from server", "variant": "success"})
	nm, cmd, ok := m.handleTUIControlEvent(opcode42client.SSEEvent{Type: "tui.toast.show", Properties: props})
	if !ok {
		t.Fatal("should handle tui.toast.show")
	}
	if len(nm.toasts) != 1 || nm.toasts[0].kind != toastSuccess {
		t.Fatalf("toasts = %+v", nm.toasts)
	}
	if cmd == nil {
		t.Fatal("toast should kick anim")
	}
}

func TestH16_PromptAppend(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("hi")
	props, _ := json.Marshal(map[string]any{"text": " there"})
	nm, _, ok := m.handleTUIControlEvent(opcode42client.SSEEvent{Type: "tui.prompt.append", Properties: props})
	if !ok {
		t.Fatal("should handle tui.prompt.append")
	}
	if nm.input.Value() != "hi there" {
		t.Fatalf("composer = %q", nm.input.Value())
	}
}

func TestH16_SessionSelect(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_old"})
	props, _ := json.Marshal(map[string]any{"sessionID": "ses_new"})
	nm, cmd, ok := m.handleTUIControlEvent(opcode42client.SSEEvent{Type: "tui.session.select", Properties: props})
	if !ok {
		t.Fatal("should handle tui.session.select")
	}
	if nm.cfg.SessionID != "ses_new" {
		t.Fatalf("SessionID = %q", nm.cfg.SessionID)
	}
	if cmd == nil {
		t.Fatal("should load messages for new session")
	}
}

func TestH16_CommandExecute_PromptClear(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("draft")
	props, _ := json.Marshal(map[string]any{"command": "prompt.clear"})
	nm, _, ok := m.handleTUIControlEvent(opcode42client.SSEEvent{Type: "tui.command.execute", Properties: props})
	if !ok {
		t.Fatal("should handle tui.command.execute")
	}
	if nm.input.Value() != "" {
		t.Fatalf("composer = %q, want cleared", nm.input.Value())
	}
}

func TestH16_SSEEventMsg_RoutesControl(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	props, _ := json.Marshal(map[string]any{"text": "x"})
	nm, _ := step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "tui.prompt.append", Properties: props}})
	if nm.input.Value() != "x" {
		t.Fatalf("SSE path should append, got %q", nm.input.Value())
	}
}
