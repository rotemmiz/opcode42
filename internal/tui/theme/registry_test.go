package theme

import "testing"

func TestPalettes_OrderedWithDarkFirst(t *testing.T) {
	ps := Palettes()
	if len(ps) < 3 {
		t.Fatalf("expected at least 3 themes, got %d", len(ps))
	}
	if ps[0].Name != "forge-dark" {
		t.Fatalf("first theme should be the default forge-dark, got %q", ps[0].Name)
	}
	// Names are distinct and palettes differ (so switching is observable).
	seen := map[string]bool{}
	for _, n := range ps {
		if seen[n.Name] {
			t.Fatalf("duplicate theme name %q", n.Name)
		}
		seen[n.Name] = true
	}
	if Default().Bg == Light().Bg {
		t.Fatal("light theme should differ from dark")
	}
}

func TestByName(t *testing.T) {
	if p, ok := ByName("monochrome"); !ok || p.Bg != Mono().Bg {
		t.Fatalf("ByName(monochrome) should resolve, ok=%v", ok)
	}
	if _, ok := ByName("does-not-exist"); ok {
		t.Fatal("unknown theme should return ok=false")
	}
}
