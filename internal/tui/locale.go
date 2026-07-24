package tui

import "strconv"

// Locale helpers matching opencode packages/tui/src/util/locale.ts (plan 08f H15 / G.17).

// formatNumber mirrors Locale.number: K/M suffix with one decimal for ≥1000.
func formatNumber(num int) string {
	switch {
	case num >= 1_000_000:
		return strconv.FormatFloat(float64(num)/1_000_000, 'f', 1, 64) + "M"
	case num >= 1000:
		return strconv.FormatFloat(float64(num)/1000, 'f', 1, 64) + "K"
	default:
		return strconv.Itoa(num)
	}
}

// truncateMiddle mirrors Locale.truncateMiddle: keep head+tail with a middle "…".
// Default maxLength matches locale.ts (35) when callers pass ≤0.
func truncateMiddle(str string, maxLength int) string {
	if maxLength <= 0 {
		maxLength = 35
	}
	r := []rune(str)
	if len(r) <= maxLength {
		return str
	}
	const ellipsis = "…"
	ellLen := len([]rune(ellipsis))
	keepStart := (maxLength - ellLen + 1) / 2 // Math.ceil
	keepEnd := (maxLength - ellLen) / 2       // Math.floor
	if keepStart < 0 {
		keepStart = 0
	}
	if keepEnd < 0 {
		keepEnd = 0
	}
	if keepStart+keepEnd > len(r) {
		return str
	}
	return string(r[:keepStart]) + ellipsis + string(r[len(r)-keepEnd:])
}

// formatDuration mirrors Locale.duration (util/locale.ts:39-59):
//
//	<1000ms          → "<n>ms"
//	<60s             → "<s.s>s"
//	<60m             → "<m>m <s>s"
//	<24h             → "<h>h <m>m"
//	otherwise        → "<d>d <h>h"
//
// Returns "" for non-positive input (the caller gates on Time.Done() so this
// is defensive).
func formatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	switch {
	case ms < 1000:
		return strconv.FormatInt(ms, 10) + "ms"
	case ms < 60_000:
		return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
	case ms < 3_600_000:
		mins := ms / 60_000
		secs := (ms % 60_000) / 1000
		return strconv.FormatInt(mins, 10) + "m " + strconv.FormatInt(secs, 10) + "s"
	case ms < 86_400_000:
		hrs := ms / 3_600_000
		mins := (ms % 3_600_000) / 60_000
		return strconv.FormatInt(hrs, 10) + "h " + strconv.FormatInt(mins, 10) + "m"
	default:
		d := ms / 86_400_000
		hrs := (ms % 86_400_000) / 3_600_000
		return strconv.FormatInt(d, 10) + "d " + strconv.FormatInt(hrs, 10) + "h"
	}
}
