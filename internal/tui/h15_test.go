package tui

import (
	"strings"
	"testing"
)

func TestH15_FormatNumber(t *testing.T) {
	cases := map[int]string{
		0:         "0",
		42:        "42",
		999:       "999",
		1000:      "1.0K",
		1500:      "1.5K",
		1_000_000: "1.0M",
		2_500_000: "2.5M",
	}
	for in, want := range cases {
		if got := formatNumber(in); got != want {
			t.Errorf("formatNumber(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestH15_TruncateMiddle(t *testing.T) {
	if got := truncateMiddle("short", 35); got != "short" {
		t.Fatalf("short = %q", got)
	}
	got := truncateMiddle("abcdefghijklmnopqrstuvwxyz0123456789", 10)
	// keepStart = ceil((10-1)/2)=5, keepEnd=floor(9/2)=4 → "abcde…6789"
	if got != "abcde…6789" {
		t.Fatalf("truncateMiddle = %q, want abcde…6789", got)
	}
	if got := truncateMiddle("abcdefghij", 0); got != "abcdefghij" {
		// default 35 > len
		t.Fatalf("default max = %q", got)
	}
}

func TestH15_FormatDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, ""},
		{500, "500ms"},
		{1500, "1.5s"},
		{65_000, "1m 5s"},
		{3_700_000, "1h 1m"},
		{90_000_000, "1d 1h"},
	}
	for _, tc := range cases {
		if got := formatDuration(tc.ms); got != tc.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestH15_UsageChip_UsesFormatNumber(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "p", Model: "m"})
	m.store.messages["ses_1"] = []Message{{
		ID: "m1", Role: "assistant",
		Tokens: MessageTokens{Input: 1000, Output: 500},
	}}
	chip := m.usageChip()
	if chip == "" || !strings.Contains(chip, "1.5K") {
		t.Fatalf("usageChip = %q, want Locale.number form (1.5K)", chip)
	}
}
