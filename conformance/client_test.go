package conformance

import (
	"net/http"
	"testing"

	"github.com/rotemmiz/forge/conformance/normalize"
)

func TestOrderInsensitiveListPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/session", true},
		{"/session?directory=/tmp/x", true},
		{"/command", true},
		{"/command?directory=/tmp/x", true},
		{"/agent", false},
		{"/provider", false},
		{"/session/ses_1/message", false},
		{"/config", false},
	}
	for _, c := range cases {
		if got := orderInsensitiveListPath(c.path); got != c.want {
			t.Errorf("orderInsensitiveListPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestCommandListOrderInsensitive asserts the harness normalizes GET /command
// order-insensitively, so opencode's non-deterministic (map/glob) order and
// Forge's name-sorted order produce the same captured body (masterplan decision
// #6). A reordering of the same command set must compare equal; a genuinely
// different set must not.
func TestCommandListOrderInsensitive(t *testing.T) {
	c := &Client{Norm: normalize.New()}

	a := []byte(`[{"name":"deploy","source":"command","template":"a","hints":[]},
		{"name":"build","source":"command","template":"b","hints":[]}]`)
	reordered := []byte(`[{"name":"build","source":"command","template":"b","hints":[]},
		{"name":"deploy","source":"command","template":"a","hints":[]}]`)
	different := []byte(`[{"name":"deploy","source":"command","template":"a","hints":[]},
		{"name":"ship","source":"command","template":"c","hints":[]}]`)

	na := c.normalizeBody(http.MethodGet, "/command", a)
	nReordered := c.normalizeBody(http.MethodGet, "/command", reordered)
	nDifferent := c.normalizeBody(http.MethodGet, "/command", different)

	if na != nReordered {
		t.Errorf("reordered command list should normalize equal:\n a=%s\n b=%s", na, nReordered)
	}
	if na == nDifferent {
		t.Error("a genuinely different command set must NOT normalize equal (would mask a real diff)")
	}
}
