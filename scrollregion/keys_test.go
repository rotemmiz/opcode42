package scrollregion

import "testing"

func TestDecode(t *testing.T) {
	cases := map[string]Action{
		"up":     Up, // also what the wheel sends under alternate scroll
		"down":   Down,
		"pgup":   PageUp,
		"pgdown": PageDown,
		"pgdn":   PageDown,
		"home":   Top,
		"end":    Bottom,
		"x":      None,
		"":       None,
	}
	for key, want := range cases {
		if got := Decode(key); got != want {
			t.Errorf("Decode(%q)=%v want %v", key, got, want)
		}
	}
}

func TestApply(t *testing.T) {
	var r Region
	r.Apply(Up, 3, 10)
	r.Apply(Up, 3, 10)
	if r.Offset != 6 {
		t.Fatalf("two Up steps: Offset=%d want 6", r.Offset)
	}
	r.Apply(PageUp, 3, 10)
	if r.Offset != 16 {
		t.Fatalf("PageUp: Offset=%d want 16", r.Offset)
	}
	r.Apply(PageDown, 3, 10)
	if r.Offset != 6 {
		t.Fatalf("PageDown: Offset=%d want 6", r.Offset)
	}
	r.Apply(Down, 3, 10)
	if r.Offset != 3 {
		t.Fatalf("Down: Offset=%d want 3", r.Offset)
	}
	r.Apply(Bottom, 3, 10)
	if !r.AtTail() {
		t.Fatalf("Bottom: not at tail (Offset=%d)", r.Offset)
	}
	r.Apply(Top, 3, 10)
	// Top sets a large offset that Window/Clamp will bound; verify it's large.
	if r.Offset < 1<<20 {
		t.Fatalf("Top: Offset=%d want a large sentinel", r.Offset)
	}
	r.Clamp(20, 5) // max = 15
	if r.Offset != 15 {
		t.Fatalf("Top then Clamp(20,5): Offset=%d want 15", r.Offset)
	}
	// None is a no-op.
	before := r.Offset
	r.Apply(None, 3, 10)
	if r.Offset != before {
		t.Fatalf("None changed Offset from %d to %d", before, r.Offset)
	}
}
