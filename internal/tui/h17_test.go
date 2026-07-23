package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Tests for plan 08f H17 (G.19 compaction part + G.20 permission edge kinds).

func TestH17_CompactionPart_RendersDivider(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	got := stripANSI(m.renderMessage(Message{Role: "user"}, []Part{{Type: "compaction"}}))
	if !strings.Contains(got, "Compaction") {
		t.Fatalf("compaction part missing titled divider:\n%s", got)
	}
	if !strings.Contains(got, "─") {
		t.Fatalf("compaction divider should use a top border rule:\n%s", got)
	}
	// Empty compaction-only message should still produce the marker (no text).
	if strings.TrimSpace(got) == "" {
		t.Fatal("compaction divider should not be empty")
	}
}

func TestH17_ExternalDirectory_ParentDir(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{
		"parentDir": "/tmp/outside",
		"filepath":  "/tmp/ignored",
	})
	body := permissionBody(Permission{
		Permission: "external_directory",
		Metadata:   meta,
		Patterns:   []string{"/tmp/outside/**", "/tmp/other/*"},
	})
	if body.Icon != "←" {
		t.Fatalf("icon = %q, want ←", body.Icon)
	}
	if body.Title != "Access external directory /tmp/outside" {
		t.Fatalf("title = %q", body.Title)
	}
	wantBody := []string{"Patterns", "- /tmp/outside/**", "- /tmp/other/*"}
	if strings.Join(body.Body, "\n") != strings.Join(wantBody, "\n") {
		t.Fatalf("body = %#v, want %#v", body.Body, wantBody)
	}
}

func TestH17_ExternalDirectory_FilepathFallback(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"filepath": "/var/data"})
	body := permissionBody(Permission{
		Permission: "external_directory",
		Metadata:   meta,
		Patterns:   []string{"/var/data/file.txt"},
	})
	if body.Title != "Access external directory /var/data" {
		t.Fatalf("title = %q (filepath fallback)", body.Title)
	}
}

func TestH17_ExternalDirectory_PatternDirname(t *testing.T) {
	body := permissionBody(Permission{
		Permission: "external_directory",
		Patterns:   []string{"/home/me/docs/**/*.md"},
	})
	// Matches Node path.dirname / Go filepath.Dir: strips the final segment
	// (*.md), leaving "/home/me/docs/**" (permission.tsx:337-338).
	if body.Title != "Access external directory /home/me/docs/**" {
		t.Fatalf("title = %q (dirname of wildcard pattern)", body.Title)
	}
}

func TestH17_ExternalDirectory_PatternLiteral(t *testing.T) {
	body := permissionBody(Permission{
		Permission: "external_directory",
		Patterns:   []string{"/opt/secret"},
	})
	if body.Title != "Access external directory /opt/secret" {
		t.Fatalf("title = %q (literal pattern)", body.Title)
	}
}

func TestH17_DoomLoop(t *testing.T) {
	body := permissionBody(Permission{Permission: "doom_loop"})
	if body.Icon != "⟳" {
		t.Fatalf("icon = %q, want ⟳", body.Icon)
	}
	if body.Title != "Continue after repeated failures" {
		t.Fatalf("title = %q", body.Title)
	}
	want := "This keeps the session running despite repeated failures."
	if len(body.Body) != 1 || body.Body[0] != want {
		t.Fatalf("body = %#v, want [%q]", body.Body, want)
	}
}

func TestH17_PermissionView_RendersEdgeKinds(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	meta, _ := json.Marshal(map[string]any{"parentDir": "/ext"})
	m.store.permissions = []Permission{{
		ID: "p_ext", SessionID: "ses_1", Permission: "external_directory",
		Metadata: meta, Patterns: []string{"/ext/**"},
	}}
	plain := stripANSI(m.permissionView())
	for _, want := range []string{"←", "Access external directory /ext", "Patterns", "- /ext/**"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("external_directory view missing %q:\n%s", want, plain)
		}
	}

	m.store.permissions = []Permission{{
		ID: "p_doom", SessionID: "ses_1", Permission: "doom_loop",
	}}
	plain = stripANSI(m.permissionView())
	for _, want := range []string{
		"⟳",
		"Continue after repeated failures",
		"This keeps the session running despite repeated failures.",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("doom_loop view missing %q:\n%s", want, plain)
		}
	}
}
