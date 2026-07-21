package scrollregion

import (
	"reflect"
	"testing"
)

func lines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

func TestMaxOffset(t *testing.T) {
	cases := []struct{ total, height, want int }{
		{10, 4, 6},
		{4, 4, 0},
		{2, 4, 0}, // body fits → no scrollback
		{0, 4, 0},
	}
	for _, c := range cases {
		if got := MaxOffset(c.total, c.height); got != c.want {
			t.Errorf("MaxOffset(%d,%d)=%d want %d", c.total, c.height, got, c.want)
		}
	}
}

func TestBackForwardFloorsAndAccumulates(t *testing.T) {
	var r Region
	r.Back(3)
	r.Back(2)
	if r.Offset != 5 {
		t.Fatalf("after Back(3)+Back(2): Offset=%d want 5", r.Offset)
	}
	r.Forward(2)
	if r.Offset != 3 {
		t.Fatalf("after Forward(2): Offset=%d want 3", r.Offset)
	}
	r.Forward(100) // past the tail
	if r.Offset != 0 || !r.AtTail() {
		t.Fatalf("Forward past tail: Offset=%d AtTail=%v want 0/true", r.Offset, r.AtTail())
	}
	// Non-positive moves are ignored.
	r.Back(0)
	r.Forward(-5)
	if r.Offset != 0 {
		t.Fatalf("no-op moves changed Offset to %d", r.Offset)
	}
}

func TestClampAndToTop(t *testing.T) {
	r := Region{Offset: 999}
	r.Clamp(10, 4) // max = 6
	if r.Offset != 6 {
		t.Fatalf("Clamp: Offset=%d want 6", r.Offset)
	}
	r.ToTail()
	r.ToTop(10, 4)
	if r.Offset != 6 {
		t.Fatalf("ToTop: Offset=%d want 6", r.Offset)
	}
}

func TestWindow_TailAndScrolled(t *testing.T) {
	body := lines(10) // a..j
	// Tail: last 4 lines.
	if got := (Region{}).Window(body, 4); !reflect.DeepEqual(got, []string{"g", "h", "i", "j"}) {
		t.Fatalf("tail window=%v", got)
	}
	// Scrolled back 2: lines shifted up by two.
	if got := (Region{Offset: 2}).Window(body, 4); !reflect.DeepEqual(got, []string{"e", "f", "g", "h"}) {
		t.Fatalf("offset=2 window=%v", got)
	}
	// Over-scroll is clamped to the top without mutating the Region.
	r := Region{Offset: 999}
	if got := r.Window(body, 4); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("over-scroll window=%v", got)
	}
	if r.Offset != 999 {
		t.Fatalf("Window mutated Offset to %d (should be pure)", r.Offset)
	}
}

func TestWindow_ShortBodyPadsToHeight(t *testing.T) {
	got := (Region{}).Window([]string{"x", "y"}, 5)
	want := []string{"x", "y", "", "", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("short body window=%v want %v", got, want)
	}
}
