package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rotemmiz/forge/internal/engine/tool"
)

func TestSkillEndpoint(t *testing.T) {
	dir := t.TempDir()
	writeMD(t, dir, ".opencode/skills/effect/SKILL.md",
		"---\nname: effect\ndescription: Work with Effect\n---\nUse Effect.")
	h := resourceServer(t, nil)

	rr, body := req(t, h, http.MethodGet, "/skill", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var skills []map[string]any
	if err := json.Unmarshal(body, &skills); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range skills {
		if s["name"] == "effect" {
			found = true
			if s["location"] == "" || s["content"] != "Use Effect." {
				t.Errorf("skill fields wrong: %v", s)
			}
		}
	}
	if !found {
		t.Fatalf("effect skill not served: %v", skills)
	}
}

func TestSkillEndpoint_EmptyIsArray(t *testing.T) {
	h := resourceServer(t, nil)
	rr, body := req(t, h, http.MethodGet, "/skill", t.TempDir())
	if rr.Code != http.StatusOK || (string(body) != "[]\n" && string(body) != "[]") {
		t.Fatalf("empty /skill must be []; status=%d body=%q", rr.Code, body)
	}
}

func TestSkillResolverLoadsByName(t *testing.T) {
	dir := t.TempDir()
	writeMD(t, dir, ".opencode/skills/foo/SKILL.md", "---\nname: foo\n---\nfoo body")
	r := skillResolver{directory: dir}
	if c, err := r.Load("foo"); err != nil || c != "foo body" {
		t.Fatalf("Load(foo) = %q, %v", c, err)
	}
	if _, err := r.Load("nope"); err == nil {
		t.Fatal("Load(nope) should error")
	}
}

func TestSkillToolUnavailableWithoutSource(t *testing.T) {
	_, err := (tool.Skill{}).Run(context.Background(),
		map[string]any{"name": "x"}, tool.Context{})
	if err == nil {
		t.Fatal("skill tool should be unavailable without a source")
	}
}
