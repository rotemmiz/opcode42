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

// recordHooks is a test PluginHooks that records hook names and runs a mutator
// over the typed output, standing in for the real flag-gated bridge.
type recordHooks struct {
	names  []string
	mutate func(name string, out any)
}

func (r *recordHooks) Trigger(_ context.Context, name string, _ any, out any) {
	r.names = append(r.names, name)
	if r.mutate != nil {
		r.mutate(name, out)
	}
}

func (r *recordHooks) fired(name string) bool {
	for _, n := range r.names {
		if n == name {
			return true
		}
	}
	return false
}

// TestExecutor_ToolBeforeHookRewritesArgs asserts a plugin's tool.execute.before
// args rewrite is the value the tool actually runs with (session/tools.ts:87-91):
// the plugin redirects the command, and that command's output is what comes back.
func TestExecutor_ToolBeforeHookRewritesArgs(t *testing.T) {
	hooks := &recordHooks{mutate: func(name string, out any) {
		if name != hookToolExecuteBefore {
			return
		}
		if o, ok := out.(*toolBeforeOutput); ok {
			o.Args = map[string]any{"command": "echo rewritten"}
		}
	}}
	exec := &Executor{Registry: builtins(), SessionID: "s", Directory: t.TempDir(), Plugins: hooks}
	res, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "echo original"}})
	if err != nil {
		t.Fatal(err)
	}
	if !hooks.fired(hookToolExecuteBefore) || !hooks.fired(hookToolExecuteAfter) {
		t.Fatalf("expected before+after hooks to fire: %v", hooks.names)
	}
	if !strings.Contains(res.Output, "rewritten") || strings.Contains(res.Output, "original") {
		t.Fatalf("before hook did not rewrite args: output = %q", res.Output)
	}
}

// TestExecutor_ToolAfterHookRewritesResult asserts a plugin's tool.execute.after
// rewrite of the result title/output/metadata is returned to the caller
// (session/tools.ts:103-107).
func TestExecutor_ToolAfterHookRewritesResult(t *testing.T) {
	hooks := &recordHooks{mutate: func(name string, out any) {
		if name != hookToolExecuteAfter {
			return
		}
		if o, ok := out.(*toolAfterOutput); ok {
			o.Output = "patched output"
			o.Title = "patched title"
			o.Metadata = map[string]any{"by": "plugin"}
		}
	}}
	exec := &Executor{Registry: builtins(), SessionID: "s", Directory: t.TempDir(), Plugins: hooks}
	res, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "echo hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "patched output" || res.Title != "patched title" {
		t.Fatalf("after hook not applied: %+v", res)
	}
	if res.Metadata["by"] != "plugin" {
		t.Fatalf("after hook metadata not applied: %+v", res.Metadata)
	}
}

// TestExecutor_NilPluginsIsNoOp asserts the tool path is unchanged when no
// plugin host is configured (the default).
func TestExecutor_NilPluginsIsNoOp(t *testing.T) {
	exec := &Executor{Registry: builtins(), SessionID: "s", Directory: t.TempDir()}
	res, err := exec.Execute(context.Background(), processor.ToolCall{
		Name: "bash", CallID: "c1", SessionID: "s", Input: map[string]any{"command": "echo hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "hi") {
		t.Fatalf("nil plugins should run the tool unchanged: %q", res.Output)
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
