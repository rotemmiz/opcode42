package server

import (
	"sort"
	"strings"
)

// rankFuzzy returns the items that fuzzily match query, best first, capped at
// limit. It is a pragmatic stand-in for opencode's fuzzysort (whose exact
// ranking can't be reproduced); the goal is that the most relevant paths sort
// to the top for the TUI's @-mention picker, not byte-identical ordering.
func rankFuzzy(query string, items []string, limit int) []string {
	q := strings.ToLower(query)
	type scored struct {
		item  string
		score int
	}
	matches := make([]scored, 0, len(items))
	for _, it := range items {
		if s, ok := fuzzyScore(q, it); ok {
			matches = append(matches, scored{it, s})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score // higher score first
		}
		if len(matches[i].item) != len(matches[j].item) {
			return len(matches[i].item) < len(matches[j].item) // shorter first
		}
		return matches[i].item < matches[j].item // stable lexicographic tiebreak
	})
	out := make([]string, 0, min(limit, len(matches)))
	for _, m := range matches {
		if len(out) == limit {
			break
		}
		out = append(out, m.item)
	}
	return out
}

// basenameBonus is added when the whole query matches within the final path
// segment, so an exact basename hit (server.go → internal/server/server.go)
// outranks one whose chars are spread across the directory prefix.
const basenameBonus = 30

// fuzzyScore reports whether the lowercased query is a subsequence of target
// (case-insensitive) and, if so, a relevance score. Higher is better. It scores
// the query against the basename and against the full path and takes the better
// of the two; the basename match carries a bonus. query must already be
// lowercased.
func fuzzyScore(query, target string) (int, bool) {
	if query == "" {
		return 0, true
	}
	best := 0
	matched := false

	// Prefer a match contained entirely in the final path segment.
	base := target[strings.LastIndex(target, "/")+1:]
	if s, ok := subseqScore(query, base); ok {
		best = s + basenameBonus
		matched = true
	}
	// Fall back to a match spanning the whole path.
	if s, ok := subseqScore(query, target); ok && (!matched || s > best) {
		best = s
		matched = true
	}
	if !matched {
		return 0, false
	}
	// Prefer shorter targets (less leading noise) once scores tie.
	return best - len(target)/8, true
}

// subseqScore matches query (already lowercased) as a case-insensitive
// subsequence of text, scoring consecutive runs and word-boundary starts.
func subseqScore(query, text string) (int, bool) {
	lower := strings.ToLower(text)
	score, qi, prevMatch := 0, 0, -2
	for ti := 0; ti < len(lower) && qi < len(query); ti++ {
		if lower[ti] != query[qi] {
			continue
		}
		gain := 1
		if ti == prevMatch+1 {
			gain += 5 // consecutive run
		}
		if isBoundary(text, ti) {
			gain += 8 // start of a word / segment
		}
		score += gain
		prevMatch = ti
		qi++
	}
	if qi != len(query) {
		return 0, false
	}
	return score, true
}

// isBoundary reports whether the char at index i in s begins a "word": it is the
// first char, follows a separator/punctuation, or is an uppercase char after a
// lowercase one (camelCase).
func isBoundary(s string, i int) bool {
	if i == 0 {
		return true
	}
	prev := s[i-1]
	switch prev {
	case '/', '\\', '.', '-', '_', ' ':
		return true
	}
	cur := s[i]
	return cur >= 'A' && cur <= 'Z' && prev >= 'a' && prev <= 'z'
}
