package resource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/permission"
)

// writeFile creates a file (and parents) under root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// project sets up a temp dir with an isolated config home and HOME (so the
// global layers — ~/.config/opencode, ~/.claude, ~/.agents — don't leak the
// test machine's real dirs) and returns the dir.
func project(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	return t.TempDir()
}

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		in, wantYAML, wantBody string
	}{
		{"---\nmode: primary\n---\nbody text", "mode: primary", "body text"},
		{"no frontmatter here", "", "no frontmatter here"},
		{"---\nunterminated\nbody", "", "---\nunterminated\nbody"},
		{"---foo not a fence", "", "---foo not a fence"},
		{"---\r\nmode: primary\r\n---\r\nbody", "mode: primary", "body"}, // CRLF
		{"---\n---\nonly body", "", "only body"},                         // empty frontmatter
	}
	for _, c := range cases {
		y, b := splitFrontmatter([]byte(c.in))
		if string(y) != c.wantYAML || b != c.wantBody {
			t.Errorf("split(%q) = (%q,%q); want (%q,%q)", c.in, y, b, c.wantYAML, c.wantBody)
		}
	}
}

func TestEntryName(t *testing.T) {
	cases := map[string]string{
		"agent/foo/bar.md": "foo/bar",
		"agent/foo.md":     "foo",
		"agents/baz.md":    "baz",
		"standalone.md":    "standalone",
	}
	for in, want := range cases {
		if got := entryName(in, []string{"agent/", "agents/"}); got != want {
			t.Errorf("entryName(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestLoadAgents_BuiltinsAndOverride(t *testing.T) {
	dir := project(t)
	// A triage agent mirroring opencode's fixture: forced fields + tools map.
	writeFile(t, dir, ".opencode/agent/triage.md",
		"---\nmode: primary\nhidden: true\nmodel: opencode/gpt-5.4-nano\ntools:\n  \"*\": false\n  \"github-triage\": true\n---\nYou are a triage agent.")

	agents := LoadAgents(dir, map[string]any{})
	byName := map[string]Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}

	for _, n := range []string{"build", "plan", "general", "compaction", "title", "summary"} {
		if _, ok := byName[n]; !ok {
			t.Fatalf("built-in agent %q missing", n)
		}
	}
	tr, ok := byName["triage"]
	if !ok {
		t.Fatal("triage agent not loaded")
	}
	if tr.Mode != "primary" || !tr.Hidden {
		t.Errorf("triage mode/hidden wrong: %+v", tr)
	}
	if tr.Model == nil || tr.Model.ProviderID != "opencode" || tr.Model.ModelID != "gpt-5.4-nano" {
		t.Errorf("triage model wrong: %+v", tr.Model)
	}
	if tr.Prompt != "You are a triage agent." {
		t.Errorf("triage prompt = %q", tr.Prompt)
	}
	// tools: {"*":false, "github-triage":true} → deny *, allow github-triage.
	want := map[string]permission.Action{"*": permission.ActionDeny, "github-triage": permission.ActionAllow}
	got := map[string]permission.Action{}
	for _, rule := range tr.Permission {
		got[rule.Permission] = rule.Action
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("permission[%q] = %v; want %v (all: %+v)", k, got[k], v, tr.Permission)
		}
	}
}

func TestParseAgent_BodyWinsOverFrontmatterPrompt(t *testing.T) {
	a := parseAgent([]byte("---\nprompt: from frontmatter\n---\nfrom body"))
	if a.Prompt != "from body" {
		t.Errorf("body should win over frontmatter prompt; got %q", a.Prompt)
	}
	// Empty body falls back to the frontmatter prompt.
	a = parseAgent([]byte("---\nprompt: only frontmatter\n---\n"))
	if a.Prompt != "only frontmatter" {
		t.Errorf("empty body should fall back to frontmatter prompt; got %q", a.Prompt)
	}
}

func TestParsePermissionConfig(t *testing.T) {
	// Bare action string → {"*": action}.
	rs := parsePermissionConfig("ask")
	if len(rs) != 1 || rs[0].Permission != "*" || rs[0].Pattern != "*" || rs[0].Action != permission.ActionAsk {
		t.Fatalf("string form = %+v", rs)
	}
	// {key: action} map.
	rs = parsePermissionConfig(map[string]any{"bash": "ask", "read": "allow"})
	got := map[string]permission.Action{}
	for _, r := range rs {
		if r.Pattern != "*" {
			t.Errorf("expected * pattern, got %q", r.Pattern)
		}
		got[r.Permission] = r.Action
	}
	if got["bash"] != permission.ActionAsk || got["read"] != permission.ActionAllow {
		t.Fatalf("map form = %+v", rs)
	}
	// {key: {pattern: action}} nested map.
	rs = parsePermissionConfig(map[string]any{"bash": map[string]any{"git *": "allow", "*": "deny"}})
	byPat := map[string]permission.Action{}
	for _, r := range rs {
		byPat[r.Pattern] = r.Action
	}
	if byPat["git *"] != permission.ActionAllow || byPat["*"] != permission.ActionDeny {
		t.Fatalf("nested form = %+v", rs)
	}
	// Invalid action is dropped.
	if rs := parsePermissionConfig(map[string]any{"bash": "maybe"}); len(rs) != 0 {
		t.Fatalf("invalid action should be dropped, got %+v", rs)
	}
}

func TestParseAgent_PermissionFrontmatterAfterTools(t *testing.T) {
	// tools denies everything; permission then re-allows read (last match wins).
	a := parseAgent([]byte("---\ntools:\n  \"*\": false\npermission:\n  read: allow\n---\nx"))
	var starDeny, readAllow bool
	for _, r := range a.Permission {
		if r.Permission == "*" && r.Action == permission.ActionDeny {
			starDeny = true
		}
		if r.Permission == "read" && r.Action == permission.ActionAllow {
			readAllow = true
		}
	}
	if !starDeny || !readAllow {
		t.Fatalf("combined tools+permission wrong: %+v", a.Permission)
	}
}

func TestLoadAgents_DisableRemovesBuiltin(t *testing.T) {
	dir := project(t)
	agents := LoadAgents(dir, map[string]any{
		"agent": map[string]any{"plan": map[string]any{"disable": true}},
	})
	for _, a := range agents {
		if a.Name == "plan" {
			t.Fatal("plan should have been disabled via config")
		}
	}
}

func TestLoadAgents_RequiredFieldsAlwaysSet(t *testing.T) {
	dir := project(t)
	writeFile(t, dir, ".opencode/agent/bare.md", "just a body, no frontmatter")
	for _, a := range LoadAgents(dir, map[string]any{}) {
		if a.Options == nil {
			t.Errorf("agent %q has nil Options (must marshal as {})", a.Name)
		}
		if a.Permission == nil {
			t.Errorf("agent %q has nil Permission (must marshal as [])", a.Name)
		}
		if a.Mode == "" {
			t.Errorf("agent %q has empty Mode", a.Name)
		}
	}
}

func TestLoadCommands(t *testing.T) {
	dir := project(t)
	writeFile(t, dir, ".opencode/command/commit.md",
		"---\ndescription: git commit and push\nsubtask: true\n---\ncommit and push")
	writeFile(t, dir, ".opencode/command/nested/deep.md", "deep template")

	cmds := LoadCommands(dir, map[string]any{})
	byName := map[string]Command{}
	for _, c := range cmds {
		byName[c.Name] = c
	}
	commit, ok := byName["commit"]
	if !ok {
		t.Fatal("commit command not loaded")
	}
	if !commit.Subtask || commit.Description != "git commit and push" {
		t.Errorf("commit fields wrong: %+v", commit)
	}
	if commit.Template != "commit and push" || commit.Source != "command" {
		t.Errorf("commit template/source wrong: %+v", commit)
	}
	if commit.Hints == nil {
		t.Error("Hints must be non-nil (marshals as [])")
	}
	if _, ok := byName["nested/deep"]; !ok {
		t.Errorf("nested command name wrong; got %v", byName)
	}
}

func TestBuildProviderList(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	t.Setenv("ACME_KEY", "secret") // marks acme connected
	t.Setenv("OTHER_KEY", "")      // beta stays disconnected
	cat := catalog.Catalog{
		"acme": {ID: "acme", Name: "Acme", Env: []string{"ACME_KEY"}, Models: map[string]catalog.Model{
			"z-model": {ID: "z-model"}, "a-model": {ID: "a-model"},
		}},
		"beta": {ID: "beta", Name: "Beta", Env: []string{"OTHER_KEY"}, Models: map[string]catalog.Model{
			"only": {ID: "only"},
		}},
	}
	got := BuildProviderList(cat, map[string]any{})

	if len(got.All) != 2 {
		t.Fatalf("all len = %d", len(got.All))
	}
	if got.All[0].ID != "acme" { // sorted
		t.Errorf("all not sorted: %v", got.All)
	}
	if len(got.Connected) != 1 || got.Connected[0] != "acme" {
		t.Errorf("connected = %v; want [acme]", got.Connected)
	}
	if got.Default["acme"] != "a-model" { // lowest id
		t.Errorf("default[acme] = %q; want a-model", got.Default["acme"])
	}
	// Env + Options must be non-nil for wire validity.
	for _, p := range got.All {
		if p.Env == nil || p.Options == nil {
			t.Errorf("provider %q has nil Env/Options", p.ID)
		}
	}
}

func TestBuildProviderList_AuthAndConfig(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_CONTENT", `{"acme":{"type":"api","key":"k"}}`)
	cat := catalog.Catalog{
		"acme":  {ID: "acme", Name: "Acme", Models: map[string]catalog.Model{"m": {ID: "m"}}},
		"gamma": {ID: "gamma", Name: "Gamma", Models: map[string]catalog.Model{"m": {ID: "m"}}},
	}
	cfg := map[string]any{"provider": map[string]any{"gamma": map[string]any{}}}
	got := BuildProviderList(cat, cfg)
	connected := map[string]bool{}
	for _, c := range got.Connected {
		connected[c] = true
	}
	if !connected["acme"] || !connected["gamma"] {
		t.Errorf("connected = %v; want acme (auth.json) + gamma (config)", got.Connected)
	}
}

func TestBuildProviderList_DisabledEnabled(t *testing.T) {
	cat := catalog.Catalog{
		"a": {ID: "a", Models: map[string]catalog.Model{"m": {ID: "m"}}},
		"b": {ID: "b", Models: map[string]catalog.Model{"m": {ID: "m"}}},
		"c": {ID: "c", Models: map[string]catalog.Model{"m": {ID: "m"}}},
	}
	got := BuildProviderList(cat, map[string]any{
		"enabled_providers":  []any{"a", "b"},
		"disabled_providers": []any{"b"},
	})
	ids := map[string]bool{}
	for _, p := range got.All {
		ids[p.ID] = true
	}
	if !ids["a"] || ids["b"] || ids["c"] {
		t.Errorf("enabled/disabled filter wrong: %v", ids)
	}
}
