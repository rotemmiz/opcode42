package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// plan17b_test.go — Plan 17 Workstream B acceptance tests.
//
// These tests verify the acceptance criteria listed in the plan:
//   - Permission/question render as footer panels (bottom, not centered) with
//     BgElev background.
//   - Panel disappears immediately on answer — composer returns same frame.
//   - 3-stage permission flow (permission → always confirm → reject with
//     message).
//   - Question has multi-question tabs, confirm review, and custom-text option.
//   - Keyboard shortcuts match opencode (tab/arrows/enter + digits for
//     question).
//   - Permission panel shows inline diff for edit/apply_patch (reuse C's
//     helper).
//   - Permission has priority over question when both are pending.
//   - Stream stays visible above the footer panel (body not hidden).

// TestPermissionView_FooterPanelNotCentered verifies the permission view is
// NOT centered (the panel is sized to innerW × panelH and the canvas positions
// it at the bottom). The rendered permissionView string's width is innerW (the
// gutter-reduced left column, plan 18 §B2 — not the full screen width via
// Place), and the canvas places it at the bottom rows.
func TestPermissionView_FooterPanelNotCentered(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	panel := m.permissionView()
	if panel == "" {
		t.Fatal("permissionView returned empty")
	}
	// The panel is sized to innerW (the gutter-reduced left column, plan 18
	// §B2) — not a full-screen Place. Width should be leftW - 2*streamGutter,
	// not 80.
	innerW := m.leftColumnWidth() - 2*streamGutter
	if w := lipgloss.Width(panel); w != innerW {
		t.Fatalf("panel width = %d, want innerW %d (panel should not be full-screen Place'd)", w, innerW)
	}
	// The panel height should be ≤ the screen height (not the full 24 rows
	// a centered Place would produce).
	panelH := lipgloss.Height(panel)
	if panelH <= 0 || panelH >= m.height {
		t.Fatalf("panel height = %d, want 0 < h < %d (footer panel, not full-screen)", panelH, m.height)
	}
}

// TestQuestionView_FooterPanelNotCentered verifies the question view is NOT
// centered (the panel is sized to innerW × panelH and the canvas positions it
// at the bottom).
func TestQuestionView_FooterPanelNotCentered(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{Question: "Pick a color", Options: []QuestionOption{opt("red")}}},
	}}
	m.qBody = questionBodyState{}

	panel := m.questionView()
	if panel == "" {
		t.Fatal("questionView returned empty")
	}
	innerW := m.leftColumnWidth() - 2*streamGutter
	if w := lipgloss.Width(panel); w != innerW {
		t.Fatalf("panel width = %d, want innerW %d (panel should not be full-screen Place'd)", w, innerW)
	}
	panelH := lipgloss.Height(panel)
	if panelH <= 0 || panelH >= m.height {
		t.Fatalf("panel height = %d, want 0 < h < %d (footer panel, not full-screen)", panelH, m.height)
	}
}

// TestPermissionReplied_PanelDisappearsImmediately verifies that on a
// successful reply the panel disappears immediately (the composer returns
// the same frame). The pending permission is cleared optimistically so the
// next render has no permission panel.
func TestPermissionReplied_PanelDisappearsImmediately(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	// The panel renders while the permission is pending.
	with := m.composeView()
	if !strings.Contains(stripANSI(with), "Permission required") {
		t.Fatal("permission panel should render while pending")
	}

	// Reply once (enter) → dispatches the reply cmd; the optimistic clear
	// marks the state as replying but keeps the permission until the HTTP
	// response. Drive the success msg to clear it.
	m, _ = step(t, m, key("enter"))
	m, _ = step(t, m, permissionRepliedMsg{id: "perm_1"})

	// The panel should be gone now (no pending permission).
	if m.pendingPermission() != nil {
		t.Fatal("the pending permission should be cleared after a successful reply")
	}
	without := m.composeView()
	if strings.Contains(stripANSI(without), "Permission required") {
		t.Fatal("permission panel should NOT render after the reply clears")
	}
}

// TestPermissionPriorityOverQuestion verifies that when both a permission and
// a question are pending, the permission takes priority (the permission panel
// renders, not the question panel). This matches opencode's pickBlockerView
// (session-data.ts:219-229): permission > question.
func TestPermissionPriorityOverQuestion(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{Question: "Pick a color", Options: []QuestionOption{opt("red")}}},
	}}
	m.permState = newPermissionState()
	m.qBody = questionBodyState{}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "Permission required") {
		t.Fatal("permission panel should render (priority over question)")
	}
	if strings.Contains(plain, "Pick a color") {
		t.Fatal("question panel should NOT render while a permission is pending (priority)")
	}
}

// TestPermissionPanel_HeightIncludesBase verifies the panel height includes
// the base (non-textarea chrome height): panelH = base + PERMISSION_ROWS where
// base = lipgloss.Height(m.statusBarView(leftW)). This matches opencode's
// applyHeight (footer.ts:697-722) with PERMISSION_ROWS=12.
func TestPermissionPanel_HeightIncludesBase(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	base := lipgloss.Height(m.statusBarView(m.leftColumnWidth()))
	if base < 1 {
		t.Fatalf("base (status bar height) should be ≥ 1, got %d", base)
	}
	panelH := lipgloss.Height(m.permissionView())
	// The panel height should be ≥ base + 12 (PERMISSION_ROWS). The actual
	// height may be larger if the content (title, detail, buttons, hint)
	// exceeds 12 rows, but it must include the base.
	if panelH < base+12 {
		t.Fatalf("panel height %d should be ≥ base(%d) + 12 = %d", panelH, base, base+12)
	}
}

// TestPermission_DiffRendersInline verifies the permission panel renders the
// inline diff when the permission metadata carries a diff (plan 17 §B4 —
// reuse Workstream C's renderUnifiedDiff helper).
func TestPermission_DiffRendersInline(t *testing.T) {
	diff := "@@ -1,3 +1,3 @@\n line1\n-old\n+new\n line3\n"
	meta := map[string]any{
		"filePath": "src/main.go",
		"diff":     diff,
	}
	mb, _ := json.Marshal(meta)
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "edit",
		Metadata:   mb,
	}}
	m.permState = newPermissionState()

	out := m.composeView()
	plain := stripANSI(out)
	// The diff content should appear in the panel (the added line "new" and
	// the removed line "old" are part of the rendered diff).
	if !strings.Contains(plain, "new") {
		t.Errorf("permission panel should render the inline diff (added line 'new')\n%s", plain)
	}
	if !strings.Contains(plain, "old") {
		t.Errorf("permission panel should render the inline diff (removed line 'old')\n%s", plain)
	}
}

// TestPermission_AlwaysStageShowsPatterns verifies the "always" confirmation
// stage shows the patterns that will be allowed (plan 17 §B3 —
// permissionAlwaysLines, permission.shared.ts:126-135).
func TestPermission_AlwaysStageShowsPatterns(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Patterns:   []string{"ls"},
		Always:     []string{"ls"},
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	// Transition to the always stage (tab to always, enter).
	m, _ = step(t, m, key("tab"))
	m, _ = step(t, m, key("enter"))
	if m.permState.stage != permStageAlways {
		t.Fatalf("expected always stage, got %v", m.permState.stage)
	}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "Always allow") {
		t.Errorf("always stage should show 'Always allow' title\n%s", plain)
	}
	if !strings.Contains(plain, "until OpenCode is restarted") {
		t.Errorf("always stage should show the 'until OpenCode is restarted' summary\n%s", plain)
	}
	if !strings.Contains(plain, "- ls") {
		t.Errorf("always stage should list the patterns (- ls)\n%s", plain)
	}
}

// TestQuestion_MultiQuestionTabs verifies the multi-question tab bar renders
// all question headers + the Confirm tab (plan 17 §B5 —
// footer.question.tsx:282-315).
func TestQuestion_MultiQuestionTabs(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{
			{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red")}},
			{Header: "Size", Question: "Pick a size", Options: []QuestionOption{opt("S")}},
		},
	}}
	m.qBody = questionBodyState{}

	out := m.composeView()
	plain := stripANSI(out)
	// The tab bar should show both headers + Confirm.
	for _, want := range []string{"Color", "Size", "Confirm"} {
		if !strings.Contains(plain, want) {
			t.Errorf("multi-question tab bar should show %q\n%s", want, plain)
		}
	}
}

// TestQuestion_CustomTextOption verifies the custom-text "Type your own
// answer" option renders when a question supports custom answers
// (plan 17 §B5 — question.shared.ts:70-72, footer.question.tsx:425-506).
func TestQuestion_CustomTextOption(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{
			Question: "Pick a color",
			Custom:   true,
			Options:  []QuestionOption{opt("red"), opt("green")},
		}},
	}}
	m.qBody = questionBodyState{}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "Type your own answer") {
		t.Errorf("custom-text option should render when custom=true\n%s", plain)
	}
}

// TestFooterPanel_BgElevBackground verifies the permission and question
// panels carry the BgElev background (Opcode42's equivalent of opencode's
// `surface`). We check by rendering the panel and confirming a cell inside
// the panel carries the BgElev color.
func TestFooterPanel_BgElevBackground(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	canvas := m.composeCanvas()
	if canvas == nil {
		t.Fatal("composeCanvas returned nil")
	}
	panelH := lipgloss.Height(m.permissionView())
	panelTopY := m.height - panelH
	if panelTopY < 0 {
		panelTopY = 0
	}
	// A cell inside the panel (e.g. the title row) should carry the BgElev
	// background color. Find a cell with the BgElev bg.
	bgElev := m.styles.P.BgElev
	found := false
	for y := panelTopY; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			c := canvas.CellAt(x, y)
			if c == nil {
				continue
			}
			if colorEqual(c.Style.Bg, bgElev) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("permission panel should carry the BgElev background color %s", bgElev)
	}
}

// TestFooterPanel_BgElevBackground_Question is the question-side counterpart.
func TestFooterPanel_BgElevBackground_Question(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{Question: "Pick a color", Options: []QuestionOption{opt("red")}}},
	}}
	m.qBody = questionBodyState{}

	canvas := m.composeCanvas()
	if canvas == nil {
		t.Fatal("composeCanvas returned nil")
	}
	panelH := lipgloss.Height(m.questionView())
	panelTopY := m.height - panelH
	if panelTopY < 0 {
		panelTopY = 0
	}
	bgElev := m.styles.P.BgElev
	found := false
	for y := panelTopY; y < m.height; y++ {
		for x := 0; x < m.width; x++ {
			c := canvas.CellAt(x, y)
			if c == nil {
				continue
			}
			if colorEqual(c.Style.Bg, bgElev) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("question panel should carry the BgElev background color %s", bgElev)
	}
}

// TestPermission_RejectStageShowsMessageField verifies the reject stage shows
// the rejection-message textarea prompt (plan 17 §B3 — "Tell OpenCode what to
// do differently", footer.permission.tsx:285-342).
func TestPermission_RejectStageShowsMessageField(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls"}`),
	}}
	m.permState = newPermissionState()

	// Transition to the reject stage (esc from permission).
	m, _ = step(t, m, key("esc"))
	if m.permState.stage != permStageReject {
		t.Fatalf("expected reject stage, got %v", m.permState.stage)
	}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "Reject permission") {
		t.Errorf("reject stage should show 'Reject permission' title\n%s", plain)
	}
	if !strings.Contains(plain, "Tell OpenCode what to do differently") {
		t.Errorf("reject stage should show the message prompt\n%s", plain)
	}
}

// TestQuestion_ConfirmReviewTab verifies the Confirm tab shows the review
// summary of all answers (plan 17 §B5 — footer.question.tsx:319-354).
func TestQuestion_ConfirmReviewTab(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{
			{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green")}},
			{Header: "Size", Question: "Pick a size", Options: []QuestionOption{opt("S"), opt("M")}},
		},
	}}
	m.qBody = questionBodyState{}

	// Q1 = red (enter advances to Q2 tab).
	m, _ = step(t, m, key("enter"))
	// Q2 = M (down then enter advances to Confirm).
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	if !questionConfirm(m.pendingQuestion(), m.qBody) {
		t.Fatalf("expected Confirm tab, got tab=%d", m.qBody.tab)
	}

	out := m.composeView()
	plain := stripANSI(out)
	// The review should show both questions with their selected answers.
	for _, want := range []string{"Review", "Color", "red", "Size", "M"} {
		if !strings.Contains(plain, want) {
			t.Errorf("confirm review should show %q\n%s", want, plain)
		}
	}
}

// TestQuestion_TabNavigation wraps the tab navigation: tab/shift+tab/left/
// right move between tabs in a multi-question flow.
func TestQuestion_TabNavigation(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{
			{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red")}},
			{Header: "Size", Question: "Pick a size", Options: []QuestionOption{opt("S")}},
		},
	}}
	m.qBody = questionBodyState{}

	// tab → Q2 tab
	m, _ = step(t, m, key("tab"))
	if m.qBody.tab != 1 {
		t.Fatalf("tab should move to tab 1, got %d", m.qBody.tab)
	}
	// tab → Confirm tab
	m, _ = step(t, m, key("tab"))
	if m.qBody.tab != 2 {
		t.Fatalf("tab should move to Confirm tab (2), got %d", m.qBody.tab)
	}
	// tab wraps → Q1 tab
	m, _ = step(t, m, key("tab"))
	if m.qBody.tab != 0 {
		t.Fatalf("tab should wrap to Q1 tab (0), got %d", m.qBody.tab)
	}
	// shift+tab wraps back → Confirm
	m, _ = step(t, m, key("shift+tab"))
	if m.qBody.tab != 2 {
		t.Fatalf("shift+tab should wrap to Confirm (2), got %d", m.qBody.tab)
	}
}

// TestFooterPanel_StreamStaysVisible_B is the B counterpart of the A4 test
// in plan17a_test.go. Verifies the stream body stays visible above the footer
// panel (opencode keeps the stream visible; the body is NOT skipped when only
// a footer panel is up).
func TestFooterPanel_StreamStaysVisible_B(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "Stream B session"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_u1", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_u1"] = []Part{{ID: "pu1", MessageID: "msg_u1", Type: "text", Text: "STREAM B VISIBLE"}}
	m.store.questions = []Question{{
		ID:        "qst_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{Question: "Pick a color", Options: []QuestionOption{opt("red")}}},
	}}
	m.qBody = questionBodyState{}

	if m.modalClassActive() {
		t.Fatal("modalClassActive should be false when only a footer panel is up")
	}
	if !m.footerPanelActive() {
		t.Fatal("footerPanelActive should be true when a question is pending")
	}
	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "STREAM B VISIBLE") {
		t.Errorf("stream body should be visible above the question footer panel\nout:\n%s", plain)
	}
	if !strings.Contains(plain, "Pick a color") {
		t.Errorf("question panel should be visible\nout:\n%s", plain)
	}
}

// TestQuestionSubmit_UnansweredQuestionIsEmptyArray locks in the B1 review
// fix: unanswered questions in a multi-question flow must serialize as `[]`,
// not `null`, to match opencode's wire shape (question.shared.ts:310-312
// `state.answers[idx] ?? []`). Go's encoding/json marshals a nil slice as
// `null`; this test guards against a regression that would send
// `[["red"], null]` instead of `[["red"], []]`.
func TestQuestionSubmit_UnansweredQuestionIsEmptyArray(t *testing.T) {
	q := &Question{
		ID:        "q_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{
			{Question: "Q1", Options: []QuestionOption{{Label: "a"}}},
			{Question: "Q2", Options: []QuestionOption{{Label: "b"}}},
		},
	}
	// Answer only Q1; Q2 is unanswered (no entry in s.answers).
	s := questionBodyState{
		requestID: "q_1",
		tab:       0,
		answers:   [][]string{{"a"}},
	}
	got := questionSubmit(q, s)
	if len(got) != 2 {
		t.Fatalf("expected 2 answer slices, got %d", len(got))
	}
	if got[1] == nil {
		t.Errorf("unanswered question must serialize as [] not null; got nil")
	}
	if len(got[1]) != 0 {
		t.Errorf("unanswered question must be empty slice; got %v", got[1])
	}
}

// TestPermission_AlwaysStageUsesAlwaysField locks in the B2 review fix:
// the "always" confirmation stage renders the `Always` field (the patterns
// that will be persisted as session grants), NOT the `Patterns` field (the
// patterns the request would touch). opencode uses `request.always`
// (permission.shared.ts:126-135); the prior code passed `p.Patterns` which
// happened to work when Patterns == Always but is wrong in general.
func TestPermission_AlwaysStageUsesAlwaysField(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_1", Title: "Always session"}}
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Patterns:   []string{"src/**.go"},
		Always:     []string{"*"},
	}}
	m.permRequestID = "perm_1"
	m.permState = permissionState{stage: permStageAlways, selected: permOptConfirm}

	out := m.composeView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "allow bash until OpenCode is restarted") {
		t.Errorf("always stage should render the Always=[*] one-liner, not the Patterns list\nout:\n%s", plain)
	}
	if strings.Contains(plain, "src/**.go") {
		t.Errorf("always stage should NOT render the Patterns field\nout:\n%s", plain)
	}
}
