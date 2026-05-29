// Package normalize strips the legitimately-volatile fields from captured
// responses and SSE events so two daemon runs can be diffed structurally
// (plan 12 §d). It removes ULIDs, epoch timestamps, and absolute filesystem
// paths, and canonicalizes JSON key order.
//
// It deliberately does NOT touch the fields that carry meaning — type, role,
// status, tool, output, HTTP status, and the response body's field names and
// nesting — because differences there are real conformance failures.
package normalize

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

const (
	idPlaceholder   = "<id>"
	tsPlaceholder   = "<ts>"
	pathPlaceholder = "<path>"
	slugPlaceholder = "<slug>"
)

// slugFields hold server-generated random slugs (e.g. session "slug":"happy-eagle"),
// which differ run-to-run and must be normalized.
var slugFields = map[string]bool{"slug": true}

// idFields are object keys whose string values are ULIDs (prefixed or raw) and
// therefore differ run-to-run. Plan 12 §d names id/sessionID/messageID/partID/
// permissionID/questionID; the rest are the same scheme and equally volatile.
var idFields = map[string]bool{
	"id": true, "sessionID": true, "messageID": true, "partID": true,
	"permissionID": true, "questionID": true, "callID": true,
	"parentID": true, "projectID": true, "requestID": true,
}

// tsFields are object keys whose values are epoch-ms timestamps or RFC3339
// strings. start/end/created/completed/updated are only treated as timestamps
// when their value actually looks like one (guarded below), so a numeric field
// that happens to share the name is not clobbered.
var tsFields = map[string]bool{
	"timestamp": true, "createdAt": true, "updatedAt": true,
	"created": true, "completed": true, "start": true, "end": true, "updated": true,
}

// A ULID is 26 Crockford base32 chars; opencode ids are usually prefixed
// (e.g. ses_01J..., msg_..., evt_...). Match either form.
var (
	ulidRe       = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{26}$`)
	prefixedIDRe = regexp.MustCompile(`^[a-z]+_[0-9A-Za-z]{20,}$`)
	rfc3339Re    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	// rfc3339SubRe matches an RFC3339 timestamp anywhere inside a string, e.g.
	// the timestamp embedded in an auto session title "New session - 2026-...Z".
	rfc3339SubRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?`)
	// prefixedIDSubRe matches a prefixed ULID anywhere inside a string, e.g. the
	// id embedded in an error message "Session not found: ses_01J…".
	prefixedIDSubRe = regexp.MustCompile(`[a-z]+_[0-9A-Za-z]{20,}`)
)

// Normalizer replaces volatile values. PathReplacements maps absolute path
// prefixes (e.g. a temp project dir) to a stable placeholder.
type Normalizer struct {
	PathReplacements map[string]string
}

// New returns a Normalizer. Each path in paths is replaced with "<path>".
func New(paths ...string) *Normalizer {
	repl := make(map[string]string, len(paths))
	for _, p := range paths {
		repl[p] = pathPlaceholder
	}
	return &Normalizer{PathReplacements: repl}
}

// Normalize walks a decoded JSON value, replacing volatile fields in place, and
// returns it.
func (n *Normalizer) Normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch {
			case idFields[k] && isVolatileID(child):
				t[k] = idPlaceholder
			case tsFields[k] && isTimestamp(child):
				t[k] = tsPlaceholder
			case slugFields[k] && isString(child):
				t[k] = slugPlaceholder
			default:
				t[k] = n.Normalize(child)
			}
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = n.Normalize(child)
		}
		return t
	case string:
		return n.replacePaths(t)
	default:
		return v
	}
}

// NormalizeJSON decodes, normalizes, and re-encodes as canonical JSON (Go sorts
// object keys), so the output is directly comparable across runs.
func (n *Normalizer) NormalizeJSON(data []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	v = n.Normalize(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// NormalizeSSE parses an SSE response body (a sequence of "data: {...}" lines)
// and returns the normalized, canonical JSON for each event in order. Non-data
// lines (event:, id:, comments, blanks) are ignored.
func (n *Normalizer) NormalizeSSE(body string) ([]string, error) {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		norm, err := n.NormalizeJSON([]byte(data))
		if err != nil {
			return nil, err
		}
		out = append(out, string(norm))
	}
	return out, nil
}

// Path normalizes an API path: ULID-like segments (e.g. /session/ses_01J…)
// become /session/<id>, and filesystem path replacements + embedded timestamps
// are applied too.
func (n *Normalizer) Path(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if isVolatileID(s) {
			segs[i] = idPlaceholder
		}
	}
	return n.replacePaths(strings.Join(segs, "/"))
}

func (n *Normalizer) replacePaths(s string) string {
	// Replace longer paths first: a temp dir like /tmp/x is a substring of its
	// symlink-resolved form /private/tmp/x, so resolving the longer one first
	// keeps the result deterministic regardless of map iteration order.
	keys := make([]string, 0, len(n.PathReplacements))
	for p := range n.PathReplacements {
		if p != "" {
			keys = append(keys, p)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, p := range keys {
		s = strings.ReplaceAll(s, p, n.PathReplacements[p])
	}
	// Replace timestamps and prefixed ULIDs embedded inside strings (e.g. the
	// auto title "New session - 2026-...Z" or "Session not found: ses_01J…").
	s = rfc3339SubRe.ReplaceAllString(s, tsPlaceholder)
	return prefixedIDSubRe.ReplaceAllString(s, idPlaceholder)
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

func isVolatileID(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return ulidRe.MatchString(s) || prefixedIDRe.MatchString(s)
}

func isTimestamp(v any) bool {
	switch t := v.(type) {
	case float64:
		return t >= 1e12 // epoch milliseconds (~2001 and later); skips small ints
	case string:
		return rfc3339Re.MatchString(t)
	default:
		return false
	}
}
