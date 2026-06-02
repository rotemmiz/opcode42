package tui

import "testing"

// --- Variant (plan 08b §7) ---

func withVariants() Model {
	m := New(Config{URL: "http://x"})
	m.model = promptModel{Provider: "anthropic", Model: "claude"}
	m.choices = []modelChoice{
		{Provider: "anthropic", Model: "claude", Variants: []string{"default", "thinking"}},
		{Provider: "openai", Model: "gpt", Variants: nil},
	}
	return m
}

func TestActiveVariants(t *testing.T) {
	m := withVariants()
	if got := m.activeVariants(); len(got) != 2 || got[0] != "default" || got[1] != "thinking" {
		t.Fatalf("activeVariants = %v, want [default thinking]", got)
	}
	m.model = promptModel{Provider: "openai", Model: "gpt"}
	if got := m.activeVariants(); got != nil {
		t.Fatalf("a model with no variants → nil, got %v", got)
	}
}

func TestCycleVariant(t *testing.T) {
	m := withVariants() // Variant "" → not found → cycles to index 0 "default"
	m = m.cycleVariant()
	if m.model.Variant != "default" {
		t.Fatalf("cycle #1 = %q, want default", m.model.Variant)
	}
	m = m.cycleVariant()
	if m.model.Variant != "thinking" {
		t.Fatalf("cycle #2 = %q, want thinking", m.model.Variant)
	}
	m = m.cycleVariant() // wraps
	if m.model.Variant != "default" {
		t.Fatalf("cycle #3 (wrap) = %q, want default", m.model.Variant)
	}
	// A model with no variants is a no-op.
	m.model = promptModel{Provider: "openai", Model: "gpt", Variant: "x"}
	if m = m.cycleVariant(); m.model.Variant != "x" {
		t.Fatalf("no-variants cycle should not change the variant, got %q", m.model.Variant)
	}
}

func TestEffectiveVariant(t *testing.T) {
	cases := map[string]string{"": "", "default": "", "thinking": "thinking"}
	for in, want := range cases {
		if got := (promptModel{Variant: in}).effectiveVariant(); got != want {
			t.Errorf("effectiveVariant(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVariantPicker_Selects(t *testing.T) {
	m := withVariants()
	m.modal, m.modalSel = modalVariant, 1 // "thinking"
	next, _ := m.modalSelect()
	if next.(Model).model.Variant != "thinking" {
		t.Fatalf("selecting variant row 1 → %q, want thinking", next.(Model).model.Variant)
	}
}

func TestModelSwitch_ResetsVariant(t *testing.T) {
	m := withVariants()
	m.model.Variant = "thinking"
	m.modal, m.modalSel = modalModels, 0 // anthropic/claude
	next, _ := m.modalSelect()
	if v := next.(Model).model.Variant; v != "" {
		t.Fatalf("switching model should reset the variant, got %q", v)
	}
}

func TestVariantCycle_ViaCtrlT(t *testing.T) {
	m := withVariants()
	m, _ = step(t, m, key("ctrl+t"))
	if m.model.Variant != "default" {
		t.Fatalf("ctrl+t should cycle the variant, got %q", m.model.Variant)
	}
}

// --- Stash (plan 08b §6) ---

func TestStashDraft(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("a half-written prompt")
	m = m.stashDraft()
	if len(m.stash) != 1 || m.stash[0] != "a half-written prompt" {
		t.Fatalf("stash = %v, want the parked draft", m.stash)
	}
	if m.input.Value() != "" {
		t.Fatal("stashing should clear the composer")
	}
}

func TestStashDraft_EmptyNoop(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("   ")
	if m = m.stashDraft(); len(m.stash) != 0 {
		t.Fatalf("an empty/whitespace draft should not stash, got %v", m.stash)
	}
}

func TestPopStash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.stash = []string{"newest", "older"}
	m = m.popStash(0)
	if m.input.Value() != "newest" {
		t.Fatalf("pop should load the draft, got %q", m.input.Value())
	}
	if len(m.stash) != 1 || m.stash[0] != "older" {
		t.Fatalf("pop should remove the restored draft, stash = %v", m.stash)
	}
}

func TestDeleteStash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.stash = []string{"a", "b", "c"}
	m = m.deleteStash(1)
	if len(m.stash) != 2 || m.stash[0] != "a" || m.stash[1] != "c" {
		t.Fatalf("delete idx 1 → %v, want [a c]", m.stash)
	}
}

func TestStashCap(t *testing.T) {
	m := New(Config{URL: "http://x"})
	for i := 0; i < stashMax+5; i++ {
		m.input.SetValue("draft-" + string(rune('a'+i%26)) + string(rune('0'+i/26)))
		m = m.stashDraft()
	}
	if len(m.stash) != stashMax {
		t.Fatalf("stash should cap at %d, got %d", stashMax, len(m.stash))
	}
}

func TestStashSave_ViaLeader(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("park me")
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("w"))
	if len(m.stash) != 1 || m.stash[0] != "park me" {
		t.Fatalf("ctrl+x w should stash the draft, got %v", m.stash)
	}
}

func TestStashDelete_ViaModal(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.stash = []string{"a", "b"}
	m.modal, m.modalSel = modalStash, 0
	m, _ = step(t, m, key("ctrl+d"))
	if len(m.stash) != 1 || m.stash[0] != "b" {
		t.Fatalf("ctrl+d in the stash modal should delete the selected draft, got %v", m.stash)
	}
}
