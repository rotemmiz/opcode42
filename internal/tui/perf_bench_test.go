package tui

import "testing"

// BenchmarkComposeCanvas_Scroll measures composeCanvas cost on a long
// session under pure scroll (plan 20 Layer 4 gate / 08f perf). If this
// stays hot (large B/op from NewCanvas + O(w×h) fill), adopt canvas reuse.
func BenchmarkComposeCanvas_Scroll(b *testing.B) {
	m := longSessionModel(b)
	m.width, m.height = 100, 40
	m = m.ensureFrameCanvas()
	m = m.rerenderFull()
	if m.composeCanvas() == nil {
		b.Fatal("composeCanvas nil")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scroll.Back(scrollStep)
		if i%20 == 19 {
			m.scroll.ToTail()
		}
		_ = m.composeCanvas()
	}
}
