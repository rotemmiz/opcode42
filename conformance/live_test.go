package conformance

import (
	"testing"

	"github.com/rotemmiz/opcode42/conformance/result"
)

func TestProviderErrorNameDetectsTransientByName(t *testing.T) {
	steps := []result.Step{
		{Name: "prompt", Body: `{"info":{"error":{"name":"APIError","data":{"message":"boom"}}},"parts":[]}`},
	}
	if got := providerErrorName(steps); got != "APIError" {
		t.Errorf("want APIError, got %q", got)
	}
}

func TestProviderErrorNameDetectsTransientByMessage(t *testing.T) {
	steps := []result.Step{
		{Name: "prompt", Body: `{"info":{"error":{"name":"SomeError","data":{"message":"code 429: quota exceeded"}}}}`},
	}
	if got := providerErrorName(steps); got != "SomeError" {
		t.Errorf("429 message should mark transient, got %q", got)
	}
}

func TestProviderErrorNameIgnoresCleanRun(t *testing.T) {
	steps := []result.Step{
		{Name: "prompt", Body: `{"info":{"finish":"stop"},"parts":[{"type":"text","text":"pong"}]}`},
	}
	if got := providerErrorName(steps); got != "" {
		t.Errorf("clean run must not be flagged, got %q", got)
	}
}

func TestProviderErrorNameIgnoresNonProviderError(t *testing.T) {
	// A modeled assistant error that is NOT an upstream provider failure must
	// still be compared (not skipped), so it is not flagged transient.
	steps := []result.Step{
		{Name: "prompt", Body: `{"info":{"error":{"name":"MessageAbortedError","data":{"message":"aborted by user"}}}}`},
	}
	if got := providerErrorName(steps); got != "" {
		t.Errorf("non-provider error must not be flagged transient, got %q", got)
	}
}

func TestIsTransientProviderError(t *testing.T) {
	cases := []struct {
		name, msg string
		want      bool
	}{
		{"APIError", "", true},
		{"ProviderError", "", true},
		{"X", "HTTP 503 overloaded", true},
		{"X", "RESOURCE_EXHAUSTED", true},
		{"MessageAbortedError", "user aborted", false},
		{"ValidationError", "bad input", false},
	}
	for _, c := range cases {
		if got := isTransientProviderError(c.name, c.msg); got != c.want {
			t.Errorf("isTransientProviderError(%q,%q) = %v, want %v", c.name, c.msg, got, c.want)
		}
	}
}
