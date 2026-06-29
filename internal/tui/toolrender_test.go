package tui

// toolrender_test.go — Plan 08c M7: table-driven tests for rich tool rendering.
//
// Covers (per plan spec §Tests):
//  1. toolHeader for each tool type → expected header string.
//  2. Todo rendering: mixed statuses → correct glyphs + status text.
//  3. Collapse: collapsed tool → header only; expanded → output panel.
//  4. Reasoning fold: collapsed one-liner vs expanded full text.
//  5. Panel background-fill: no transparent trailing cells for opcode42-dark/opcode42-light.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// ── 1. toolHeader table ───────────────────────────────────────────────────────

func TestToolHeader_PerToolType(t *testing.T) {
	cases := []struct {
		tool string
		inp  toolInput
		want string
	}{
		// bash: prefers description, falls back to command, then bare "Bash"
		{"bash", toolInput{Command: "npm test", Description: "Run tests"}, "Bash Run tests"},
		{"bash", toolInput{Command: "npm test"}, "Bash npm test"},
		{"bash", toolInput{}, "Bash"},

		// shell alias
		{"shell", toolInput{Command: "ls -la"}, "Bash ls -la"},

		// read / write / edit
		{"read", toolInput{FilePath: "src/x.ts"}, "Read src/x.ts"},
		{"read", toolInput{}, "Read"},
		{"write", toolInput{FilePath: "out/y.go"}, "Write out/y.go"},
		{"write", toolInput{}, "Write"},
		{"edit", toolInput{FilePath: "internal/a.go"}, "Edit internal/a.go"},
		{"edit", toolInput{}, "Edit"},

		// apply_patch: no salient arg
		{"apply_patch", toolInput{}, "Patch"},

		// grep / glob: pattern + optional path
		{"grep", toolInput{Pattern: "TODO", Path: "src/"}, `Grep "TODO" in src/`},
		{"grep", toolInput{Pattern: "foo"}, `Grep "foo"`},
		{"grep", toolInput{}, "Grep"},
		{"glob", toolInput{Pattern: "*.go", Path: "internal/"}, `Glob "*.go" in internal/`},
		{"glob", toolInput{Pattern: "*.ts"}, `Glob "*.ts"`},
		{"glob", toolInput{}, "Glob"},

		// webfetch / websearch
		{"webfetch", toolInput{URL: "https://example.com"}, "WebFetch https://example.com"},
		{"webfetch", toolInput{}, "WebFetch"},
		{"websearch", toolInput{Query: "golang lipgloss"}, `WebSearch "golang lipgloss"`},
		{"websearch", toolInput{}, "WebSearch"},

		// todowrite
		{"todowrite", toolInput{}, "Todos"},
		{"todo_write", toolInput{}, "Todos"},

		// task
		{"task", toolInput{Description: "write tests", SubagentType: "coding"}, "Coding Task — write tests"},
		{"task", toolInput{Description: "fix bug"}, "General Task — fix bug"},
		{"task", toolInput{}, "Task"},

		// skill
		{"skill", toolInput{Name: "my-skill"}, `Skill "my-skill"`},
		{"skill", toolInput{}, "Skill"},

		// fallback: unknown tool name returned as-is
		{"unknown_tool_xyz", toolInput{}, "unknown_tool_xyz"},
		// empty tool name
		{"", toolInput{}, "tool"},
	}

	for _, tc := range cases {
		got := toolHeader(tc.tool, tc.inp)
		if got != tc.want {
			t.Errorf("toolHeader(%q, %+v) = %q, want %q", tc.tool, tc.inp, got, tc.want)
		}
	}
}

// ── 2. Todo list rendering ────────────────────────────────────────────────────

func TestRenderTodos_GlyphsAndColors(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	todos := []todoItem{
		{ID: "1", Status: "completed", Content: "implement feature"},
		{ID: "2", Status: "in_progress", Content: "write tests"},
		{ID: "3", Status: "pending", Content: "update docs"},
	}
	out := m.renderTodos(todos, 80)

	// All todo content should appear in the output.
	for _, want := range []string{"implement feature", "write tests", "update docs"} {
		if !strings.Contains(out, want) {
			t.Errorf("todo output missing %q:\n%s", want, out)
		}
	}
	// Status glyphs: completed=[✓], in_progress=[•], pending=[ ]
	for _, glyph := range []string{"[✓]", "[•]", "[ ]"} {
		if !strings.Contains(out, glyph) {
			t.Errorf("todo output missing glyph %q:\n%s", glyph, out)
		}
	}
}

func TestToolRow_TodoWrite_ShowsItems(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	state := rawState(t, map[string]any{
		"status": "completed",
		"input": map[string]any{
			"todos": []map[string]any{
				{"id": "a", "status": "completed", "content": "done item"},
				{"id": "b", "status": "in_progress", "content": "wip item"},
				{"id": "c", "status": "pending", "content": "todo item"},
			},
		},
	})
	row := m.toolRow(Part{ID: "p1", Tool: "todowrite", Type: "tool", State: state})

	for _, want := range []string{"Todos", "done item", "wip item", "todo item", "[✓]", "[•]", "[ ]"} {
		if !strings.Contains(row, want) {
			t.Errorf("todowrite row missing %q:\n%s", want, row)
		}
	}
}

// ── 3. Collapse state ─────────────────────────────────────────────────────────

func TestToolRow_CollapsedShowsHeaderOnly(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	state := rawState(t, map[string]any{
		"status": "completed",
		"output": "line1\nline2\nline3",
		"input":  map[string]any{"command": "ls"},
	})
	part := Part{ID: "tool_1", Tool: "bash", Type: "tool", State: state}

	// Expand first (default): should contain output.
	expanded := m.toolRow(part)
	if !strings.Contains(expanded, "line1") {
		t.Errorf("expanded tool row should contain output, got:\n%s", expanded)
	}

	// Collapse it.
	m.view = m.view.toggleToolCollapse("tool_1")
	collapsed := m.toolRow(part)
	if strings.Contains(collapsed, "line1") {
		t.Errorf("collapsed tool row should NOT contain output, got:\n%s", collapsed)
	}
	// Header must still be present.
	if !strings.Contains(collapsed, "Bash ls") {
		t.Errorf("collapsed tool row should contain header, got:\n%s", collapsed)
	}
	// Collapsed affordance ▸.
	if !strings.Contains(collapsed, "▸") {
		t.Errorf("collapsed tool row should show ▸ affordance, got:\n%s", collapsed)
	}
}

func TestToolRow_ExpandedShowsOutput(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	state := rawState(t, map[string]any{
		"status": "completed",
		"output": "hello world",
		"input":  map[string]any{"filePath": "foo.go"},
	})
	part := Part{ID: "tool_2", Tool: "read", Type: "tool", State: state}

	// Default: expanded.
	row := m.toolRow(part)
	if !strings.Contains(row, "hello world") {
		t.Errorf("expanded tool row should contain output, got:\n%s", row)
	}
	if !strings.Contains(row, "▾") {
		t.Errorf("expanded tool row should show ▾ affordance, got:\n%s", row)
	}
}

func TestToggleToolCollapse_FlipsState(t *testing.T) {
	v := viewState{}
	if v.isToolCollapsed("p1") {
		t.Error("should start expanded")
	}
	v = v.toggleToolCollapse("p1")
	if !v.isToolCollapsed("p1") {
		t.Error("should be collapsed after first toggle")
	}
	v = v.toggleToolCollapse("p1")
	if v.isToolCollapsed("p1") {
		t.Error("should be expanded after second toggle")
	}
}

// ── 4. Reasoning fold ─────────────────────────────────────────────────────────

func TestThinking_CollapsedOneLiner(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.expandedThinking = false
	out := m.thinking("This is my reasoning\nsecond line\nthird line")
	// Collapsed: should start with ▸ Thought and only show the first line.
	if !strings.Contains(out, "▸ Thought") {
		t.Errorf("collapsed thinking should contain '▸ Thought', got: %q", out)
	}
	if !strings.Contains(out, "This is my reasoning") {
		t.Errorf("collapsed thinking should show first line, got: %q", out)
	}
	// Must NOT contain the second line in collapsed mode.
	if strings.Contains(out, "second line") {
		t.Errorf("collapsed thinking should NOT show second line, got: %q", out)
	}
}

func TestThinking_ExpandedShowsFullText(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.expandedThinking = true
	out := m.thinking("First thought\nSecond thought\nThird thought")
	// Expanded: ▾ header + full body.
	if !strings.Contains(out, "▾ Thought") {
		t.Errorf("expanded thinking should contain '▾ Thought', got: %q", out)
	}
	for _, want := range []string{"First thought", "Second thought", "Third thought"} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded thinking should contain %q, got: %q", want, out)
		}
	}
}

// ── 5. Panel background-fill ──────────────────────────────────────────────────

// TestToolPanel_BackgroundFill checks that every line in an expanded tool output
// panel is exactly panelWidth visible characters wide (no transparent trailing
// cells). panelWidth = contentWidth - 2 (2-column indent). plan 08c Tier 0.
func TestToolPanel_BackgroundFill(t *testing.T) {
	themeNames := []string{"opcode42-dark", "opcode42-light"}
	for _, tn := range themeNames {
		t.Run(tn, func(t *testing.T) {
			m := New(Config{URL: "http://x"})
			m.width = 80
			p, ok := theme.ByName(tn)
			if !ok {
				t.Skipf("theme %q not found", tn)
			}
			m = m.applyTheme(tn, p)

			output := "alpha\nbeta\ngamma"
			panelW := m.contentWidth() - 2
			panel := m.renderOutputPanel(output, m.contentWidth())

			for i, line := range strings.Split(panel, "\n") {
				if line == "" {
					continue
				}
				got := lipgloss.Width(line)
				// Each line is "  " + panelStyle.Width(panelW).Render(content), so
				// total visible width = 2 + panelW.
				want := 2 + panelW
				if got != want {
					t.Errorf("theme=%q line %d: visible width %d, want %d\nline: %q",
						tn, i, got, want, line)
				}
			}
		})
	}
}

// TestToolRow_NoOutputNoPanelAffordance ensures that a tool with no output does
// not show a fold affordance (▸/▾) — there's nothing to fold.
func TestToolRow_NoOutputNoPanelAffordance(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	state := rawState(t, map[string]any{
		"status": "running",
		"input":  map[string]any{"filePath": "foo.go"},
	})
	row := m.toolRow(Part{ID: "p3", Tool: "read", Type: "tool", State: state})
	if strings.Contains(row, "▸") || strings.Contains(row, "▾") {
		t.Errorf("tool with no output should not show fold affordance:\n%s", row)
	}
}

// TestToolRow_Truncation checks that output longer than maxPanelLines gets a
// "… N more lines" hint.
func TestToolRow_Truncation(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80

	var outputLines []string
	for i := 0; i < maxPanelLines+5; i++ {
		outputLines = append(outputLines, "line")
	}
	state := rawState(t, map[string]any{
		"status": "completed",
		"output": strings.Join(outputLines, "\n"),
		"input":  map[string]any{"command": "cat big.txt"},
	})
	row := m.toolRow(Part{ID: "p4", Tool: "bash", Type: "tool", State: state})
	if !strings.Contains(row, "more line") {
		t.Errorf("tool row should show truncation hint for long output:\n%s", row)
	}
}

// TestLastToolPartID returns the ID of the most recent tool part.
func TestLastToolPartID(t *testing.T) {
	m := seededSessionModel(t)
	// seededSessionModel has prt_3 as the only tool part.
	if got := m.lastToolPartID(); got != "prt_3" {
		t.Errorf("lastToolPartID() = %q, want %q", got, "prt_3")
	}
}

// TestLastToolPartID_Empty returns "" when no tool parts exist.
func TestLastToolPartID_Empty(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.messages["ses_1"] = []Message{{ID: "msg_1", SessionID: "ses_1", Role: "assistant"}}
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "hi"}}
	if got := m.lastToolPartID(); got != "" {
		t.Errorf("lastToolPartID() = %q, want empty", got)
	}
}

// ── JSON round-trip ────────────────────────────────────────────────────────────

// TestParseToolState checks that parseToolState correctly extracts fields from
// a typical wire ToolState JSON.
func TestParseToolState_RoundTrip(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"status": "completed",
		"output": "hello",
		"input": map[string]any{
			"filePath": "main.go",
			"command":  "go build",
		},
		"error": "",
	})
	st, inp := parseToolState(raw)
	if st.Status != "completed" {
		t.Errorf("status: got %q, want %q", st.Status, "completed")
	}
	if st.Output != "hello" {
		t.Errorf("output: got %q, want %q", st.Output, "hello")
	}
	if inp.FilePath != "main.go" {
		t.Errorf("filePath: got %q, want %q", inp.FilePath, "main.go")
	}
	if inp.Command != "go build" {
		t.Errorf("command: got %q, want %q", inp.Command, "go build")
	}
}
