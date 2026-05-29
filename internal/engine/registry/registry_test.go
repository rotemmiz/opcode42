package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/engine/tool"
)

func builtins() *Registry {
	return New(tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{}, tool.Glob{}, tool.Grep{}, tool.Patch{},
		tool.WebFetch{}, tool.WebSearch{}, tool.TodoWrite{})
}

func names(r *Registry, in FilterInput) []string {
	var out []string
	for _, d := range r.Definitions(in) {
		out = append(out, d.Name)
	}
	return out
}

func has(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestDefinitions_EditWriteForNonGPT(t *testing.T) {
	ids := names(builtins(), FilterInput{ProviderID: "anthropic", ModelID: "claude-sonnet-4-6"})
	if !has(ids, "edit") || !has(ids, "write") || has(ids, "patch") {
		t.Fatalf("non-gpt should get edit/write, not patch: %v", ids)
	}
}

func TestDefinitions_PatchForGPT5(t *testing.T) {
	ids := names(builtins(), FilterInput{ProviderID: "openai", ModelID: "gpt-5"})
	if !has(ids, "patch") || has(ids, "edit") || has(ids, "write") {
		t.Fatalf("gpt-5 should get patch, not edit/write: %v", ids)
	}
}

func TestDefinitions_GPT4KeepsEditWrite(t *testing.T) {
	ids := names(builtins(), FilterInput{ProviderID: "openai", ModelID: "gpt-4o"})
	if has(ids, "patch") || !has(ids, "edit") {
		t.Fatalf("gpt-4o should keep edit/write (not patch): %v", ids)
	}
}

func TestDefinitions_WebSearchGated(t *testing.T) {
	off := names(builtins(), FilterInput{ModelID: "gpt-4o"})
	if has(off, "websearch") {
		t.Fatalf("websearch should be gated off by default: %v", off)
	}
	on := names(builtins(), FilterInput{ModelID: "gpt-4o", Flags: Flags{WebSearch: true}})
	if !has(on, "websearch") {
		t.Fatalf("websearch should appear when flagged: %v", on)
	}
}

func TestExecutor_RunsToolWithoutGate(t *testing.T) {
	exec := &Executor{Registry: builtins(), SessionID: "s", Directory: t.TempDir()}
	res, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "echo hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "hi") {
		t.Fatalf("output = %q", res.Output)
	}
}

func TestExecutor_PermissionDeniedSurfaces(t *testing.T) {
	pm := permission.NewManager(nil)
	exec := &Executor{Registry: builtins(), Asker: pm, SessionID: "s", Directory: t.TempDir(),
		Rulesets: []permission.Ruleset{{{Permission: "bash", Pattern: "*", Action: permission.ActionDeny}}}}
	_, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "rm -rf /"}})
	var denied *permission.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("want DeniedError, got %v", err)
	}
}

func TestExecutor_PermissionAllowedRuns(t *testing.T) {
	pm := permission.NewManager(nil)
	dir := t.TempDir()
	exec := &Executor{Registry: builtins(), Asker: pm, SessionID: "s", Directory: dir,
		Rulesets: []permission.Ruleset{{{Permission: "bash", Pattern: "*", Action: permission.ActionAllow}}}}
	if _, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "echo ok"}}); err != nil {
		t.Fatalf("allowed bash should run: %v", err)
	}
}

func TestExecutor_UnknownTool(t *testing.T) {
	exec := &Executor{Registry: builtins(), SessionID: "s"}
	if _, err := exec.Execute(context.Background(), processor.ToolCall{Name: "nope", SessionID: "s"}); err == nil {
		t.Fatal("unknown tool should error")
	}
}

func TestSystemPromptVariant(t *testing.T) {
	cases := map[string]string{
		"gpt-4o": "Forge", "gemini-2.0-flash": "Forge", "claude-sonnet-4-6": "Forge", "llama-3.3": "Forge",
	}
	for model := range cases {
		if !strings.Contains(SystemPrompt(model), "Forge") {
			t.Fatalf("prompt for %s missing", model)
		}
	}
	if systemPromptVariant("claude-sonnet-4-6") != "anthropic" {
		t.Fatal("claude should route to anthropic prompt")
	}
	if systemPromptVariant("gpt-5") != "gpt" {
		t.Fatal("gpt-5 should route to gpt prompt")
	}
}

func TestEnvironmentBlock(t *testing.T) {
	env := Environment(EnvInfo{ModelID: "gpt-4o", WorkingDir: "/proj", IsGit: true})
	for _, want := range []string{"<env>", "Working directory: /proj", "Is a git repository: true", "gpt-4o"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env block missing %q: %s", want, env)
		}
	}
}
