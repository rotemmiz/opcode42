package push

import (
	"strings"
	"testing"

	"github.com/rotemmiz/opcode42/internal/bus"
)

func TestFromEventMapping(t *testing.T) {
	tests := []struct {
		name        string
		event       bus.Event
		wantOK      bool
		wantTitle   string
		wantSession string
	}{
		{
			name:        "session.idle",
			event:       bus.NewEvent("session.idle", map[string]any{"sessionID": "ses_1"}),
			wantOK:      true,
			wantTitle:   "Agent finished",
			wantSession: "ses_1",
		},
		{
			name:        "permission.asked with tool",
			event:       bus.NewEvent("permission.asked", map[string]any{"sessionID": "ses_2", "tool": "bash"}),
			wantOK:      true,
			wantTitle:   "Permission needed",
			wantSession: "ses_2",
		},
		{
			name:        "question.asked",
			event:       bus.NewEvent("question.asked", map[string]any{"sessionID": "ses_3", "text": "Which file?"}),
			wantOK:      true,
			wantTitle:   "Agent has a question",
			wantSession: "ses_3",
		},
		{
			name:   "session.status not mapped (idle handled via session.idle)",
			event:  bus.NewEvent("session.status", map[string]any{"sessionID": "ses_4"}),
			wantOK: false,
		},
		{
			name:   "message.updated not mapped",
			event:  bus.NewEvent("message.updated", map[string]any{"sessionID": "ses_5"}),
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := FromEvent(tt.event)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if n.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", n.Title, tt.wantTitle)
			}
			if n.SessionID != tt.wantSession {
				t.Errorf("session = %q, want %q", n.SessionID, tt.wantSession)
			}
			if n.EventType != tt.event.Type {
				t.Errorf("event_type = %q, want %q", n.EventType, tt.event.Type)
			}
			if n.data()["session_id"] != tt.wantSession {
				t.Errorf("data session_id = %q, want %q", n.data()["session_id"], tt.wantSession)
			}
		})
	}
}

func TestPermissionBodyMentionsTool(t *testing.T) {
	n, ok := FromEvent(bus.NewEvent("permission.asked", map[string]any{"tool": "edit"}))
	if !ok {
		t.Fatal("permission.asked should map")
	}
	if !strings.Contains(n.Body, "edit") {
		t.Errorf("body %q should mention tool name", n.Body)
	}
}

func TestTruncate(t *testing.T) {
	long := strings.Repeat("x", maxBody+50)
	n, _ := FromEvent(bus.NewEvent("question.asked", map[string]any{"text": long}))
	if len([]rune(n.Body)) > maxBody+1 { // +1 for the ellipsis rune
		t.Errorf("body length %d exceeds cap", len([]rune(n.Body)))
	}
	if !strings.HasSuffix(n.Body, "…") {
		t.Errorf("truncated body should end with ellipsis: %q", n.Body)
	}
}
