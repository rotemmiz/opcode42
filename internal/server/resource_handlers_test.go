package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
)

// resourceServer builds a server with a catalog so /provider has data.
func resourceServer(t *testing.T, cat catalog.Catalog) http.Handler {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // sandbox ~/.claude, ~/.agents skill dirs
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g := bus.NewGlobal()
	h, err := New(Options{
		Version: "0.0.1", Auth: auth.Config{}, Cwd: t.TempDir(),
		Sessions: session.NewStore(db), Instances: instance.NewManager(g), Global: g,
		Catalog: cat,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAgentEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeMD(t, dir, ".opencode/agent/custom.md", "---\nmode: subagent\ndescription: my agent\n---\nbody")
	h := resourceServer(t, nil)

	rr, body := req(t, h, http.MethodGet, "/agent", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var agents []map[string]any
	if err := json.Unmarshal(body, &agents); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, a := range agents {
		names[a["name"].(string)] = true
		// Required wire fields must be present on every agent.
		if _, ok := a["permission"]; !ok {
			t.Errorf("agent %v missing permission", a["name"])
		}
		if _, ok := a["options"]; !ok {
			t.Errorf("agent %v missing options", a["name"])
		}
	}
	if !names["build"] || !names["custom"] {
		t.Fatalf("expected build + custom agents, got %v", names)
	}
}

func TestCommandEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeMD(t, dir, ".opencode/command/deploy.md", "---\ndescription: ship it\n---\nrun the deploy")
	h := resourceServer(t, nil)

	rr, body := req(t, h, http.MethodGet, "/command", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var cmds []map[string]any
	if err := json.Unmarshal(body, &cmds); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cmds {
		if c["name"] == "deploy" {
			found = true
			if c["template"] != "run the deploy" || c["source"] != "command" {
				t.Errorf("deploy command wrong: %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("deploy command not served: %v", cmds)
	}
}

func TestProviderEndpoint(t *testing.T) {
	t.Setenv("ACME_KEY", "x")
	cat := catalog.Catalog{
		"acme": {ID: "acme", Name: "Acme", Env: []string{"ACME_KEY"},
			Models: map[string]catalog.Model{"m1": {ID: "m1"}}},
	}
	h := resourceServer(t, cat)
	rr, body := req(t, h, http.MethodGet, "/provider", t.TempDir())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var resp struct {
		All       []map[string]any  `json:"all"`
		Default   map[string]string `json:"default"`
		Connected []string          `json:"connected"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.All) != 1 || resp.All[0]["id"] != "acme" {
		t.Fatalf("all = %v", resp.All)
	}
	if len(resp.Connected) != 1 || resp.Connected[0] != "acme" {
		t.Fatalf("connected = %v", resp.Connected)
	}
}

func writeMD(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
