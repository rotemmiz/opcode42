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
	verPlaceholder  = "<ver>"
)

// slugFields hold server-generated random slugs (e.g. session "slug":"happy-eagle"),
// which differ run-to-run and must be normalized.
var slugFields = map[string]bool{"slug": true}

// verFields hold the daemon's wire-version string (e.g. session "version":
// "1.15.11"). It is environment/build-specific — opencode stamps its own release
// and Forge stamps its opencode-compat target — so it is normalized to keep the
// dual diff build-independent (plan: "compat constant + normalize"). The value
// is only collapsed when it is semver-shaped (see isVersion), so an unrelated
// "version" field carrying arbitrary data is still compared, not masked.
var verFields = map[string]bool{"version": true}

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
	// semverRe matches a semver-shaped version string (optional pre-release /
	// build suffix), so only genuine daemon versions are collapsed.
	semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)
	// confDirRe matches the conformance harness's per-scenario temp working dir
	// (runner.go: os.MkdirTemp("", "forge-conf-")), in any form: absolute,
	// symlink-resolved, or the leading-slash-stripped relative form opencode puts
	// in a session "path". GET /session returns a GLOBAL list spanning every
	// scenario's dir, so a per-client path registration can't cover the sibling
	// scenarios' dirs — this pattern scrubs them all. The prefix is harness-owned,
	// so it never appears in real API payloads.
	confDirRe = regexp.MustCompile(`[/\w.-]*forge-conf-\d+`)
	// confHomeRe matches the per-run temp HOME the harness gives each opencode
	// process (run-conformance.sh: HOME="$(mktemp -d)"). opencode bakes this
	// absolute HOME into agent permission patterns (e.g.
	// "$HOME/.local/share/opencode/tool-output/*"), so two runs diff on the
	// random suffix. mktemp's signature is a "tmp.<random>" segment under /tmp
	// (Linux) or $TMPDIR (/var/folders/.../T on macOS); the leading slash is
	// optional because opencode also emits the slash-stripped relative form. Only
	// the volatile HOME prefix is collapsed, leaving the stable
	// ".../.local/share/opencode/…" tail.
	confHomeRe = regexp.MustCompile(`/?((private/)?var/folders/[^/]+/[^/]+/T|tmp)/tmp\.[A-Za-z0-9]+`)
)

// Normalizer replaces volatile values. PathReplacements maps absolute path
// prefixes (e.g. a temp project dir) to a stable placeholder.
type Normalizer struct {
	PathReplacements map[string]string
}

// New returns a Normalizer. Each path in paths is replaced with "<path>". The
// leading-slash-trimmed form of every path is also registered, because some
// fields carry the working directory as a relative path — opencode's session
// "path" is the cwd with its leading "/" stripped (e.g. an absolute cwd of
// /tmp/forge-conf-123 surfaces as "tmp/forge-conf-123") — which the absolute
// prefix would otherwise never match.
func New(paths ...string) *Normalizer {
	repl := make(map[string]string, len(paths)*2)
	for _, p := range paths {
		if p == "" {
			continue
		}
		repl[p] = pathPlaceholder
		if rel := strings.TrimPrefix(p, "/"); rel != p && rel != "" {
			repl[rel] = pathPlaceholder
		}
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
			case verFields[k] && isVersion(child):
				t[k] = verPlaceholder
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
	// Scrub any conformance temp dir not in the explicit replacement set (a
	// sibling scenario's dir surfacing in the global session list) and the
	// per-run temp HOME baked into agent permission patterns.
	s = confDirRe.ReplaceAllString(s, pathPlaceholder)
	s = confHomeRe.ReplaceAllString(s, pathPlaceholder)
	// Replace timestamps and prefixed ULIDs embedded inside strings (e.g. the
	// auto title "New session - 2026-...Z" or "Session not found: ses_01J…").
	s = rfc3339SubRe.ReplaceAllString(s, tsPlaceholder)
	return prefixedIDSubRe.ReplaceAllString(s, idPlaceholder)
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

// isVersion reports whether v is a semver-shaped string (e.g. "1.15.11"), the
// only form of a "version" field that is volatile across daemons/builds.
func isVersion(v any) bool {
	s, ok := v.(string)
	return ok && semverRe.MatchString(s)
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
