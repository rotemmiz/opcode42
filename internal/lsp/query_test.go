package lsp

import (
	"encoding/json"
	"strconv"
	"testing"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestFlatFilter_DropsNullAndFlattensArrays(t *testing.T) {
	in := []json.RawMessage{
		raw(`null`),
		raw(`{"a":1}`),
		raw(`[{"b":2},null,{"c":3}]`),
		raw(``),
		raw(`[]`),
	}
	out := flatFilter(in)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3 (%v)", len(out), out)
	}
	if string(out[0]) != `{"a":1}` || string(out[1]) != `{"b":2}` || string(out[2]) != `{"c":3}` {
		t.Fatalf("out = %v", out)
	}
}

func TestFlatFilter_Empty(t *testing.T) {
	if got := flatFilter(nil); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestSymbolKindIncluded(t *testing.T) {
	cases := []struct {
		kind int
		want bool
	}{
		{5, true},   // Class
		{6, true},   // Method
		{10, true},  // Enum
		{11, true},  // Interface
		{12, true},  // Function
		{13, true},  // Variable
		{14, true},  // Constant
		{23, true},  // Struct
		{1, false},  // File
		{2, false},  // Module
		{9, false},  // Constructor
		{26, false}, // TypeParameter
	}
	for _, c := range cases {
		s := raw(`{"name":"x","kind":` + strconv.Itoa(c.kind) + `}`)
		if got := symbolKindIncluded(s); got != c.want {
			t.Fatalf("kind %d: got %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestSymbolKindIncluded_BadJSON(t *testing.T) {
	if symbolKindIncluded(raw(`not json`)) {
		t.Fatal("bad json should not be included")
	}
}
