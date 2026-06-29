package normalize

import (
	"encoding/json"
	"testing"
)

func normJSON(t *testing.T, n *Normalizer, in string) map[string]any {
	t.Helper()
	out, err := n.NormalizeJSON([]byte(in))
	if err != nil {
		t.Fatalf("NormalizeJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	return m
}

func TestReplacesULIDsAndPreservesStructure(t *testing.T) {
	n := New()
	in := `{
		"id":"evt_01JCABCDEFGHJKMNPQRSTVWXYZ",
		"type":"message.updated",
		"properties":{
			"sessionID":"ses_01JCABCDEFGHJKMNPQRSTVWXYZ",
			"role":"assistant",
			"status":"completed"
		}
	}`
	m := normJSON(t, n, in)
	if m["id"] != "<id>" {
		t.Errorf("id: want <id>, got %v", m["id"])
	}
	// Structural fields must survive untouched.
	if m["type"] != "message.updated" {
		t.Errorf("type changed: %v", m["type"])
	}
	props := m["properties"].(map[string]any)
	if props["sessionID"] != "<id>" {
		t.Errorf("sessionID: want <id>, got %v", props["sessionID"])
	}
	if props["role"] != "assistant" || props["status"] != "completed" {
		t.Errorf("role/status must be preserved: %+v", props)
	}
}

func TestReplacesEpochAndRFC3339Timestamps(t *testing.T) {
	n := New()
	in := `{"time":{"created":1735689600000,"completed":1735689601000},"createdAt":"2026-05-29T00:00:00Z"}`
	m := normJSON(t, n, in)
	tm := m["time"].(map[string]any)
	if tm["created"] != "<ts>" || tm["completed"] != "<ts>" {
		t.Errorf("nested time not normalized: %+v", tm)
	}
	if m["createdAt"] != "<ts>" {
		t.Errorf("createdAt: want <ts>, got %v", m["createdAt"])
	}
}

func TestDoesNotClobberSmallIntegerFieldsNamedLikeTimes(t *testing.T) {
	n := New()
	// "start" here is a small int (e.g. a text range offset), not an epoch ms.
	in := `{"start":12,"end":48,"cursor":0}`
	m := normJSON(t, n, in)
	if m["start"] != float64(12) || m["end"] != float64(48) {
		t.Errorf("small ints named start/end must be preserved: %+v", m)
	}
}

func TestReplacesAbsolutePaths(t *testing.T) {
	n := New("/tmp/opcode42-test-project")
	in := `{"directory":"/tmp/opcode42-test-project/src","worktree":"/tmp/opcode42-test-project"}`
	m := normJSON(t, n, in)
	if m["directory"] != "<path>/src" {
		t.Errorf("path prefix not replaced: %v", m["directory"])
	}
	if m["worktree"] != "<path>" {
		t.Errorf("worktree: want <path>, got %v", m["worktree"])
	}
}

func TestPreservesNonULIDStringIDs(t *testing.T) {
	n := New()
	// providerID is a stable slug, not a ULID — must NOT be normalized. (It is
	// also not in idFields, but guard the value check regardless.)
	in := `{"id":"anthropic","name":"Anthropic"}`
	m := normJSON(t, n, in)
	if m["id"] != "anthropic" {
		t.Errorf("non-ULID id must be preserved, got %v", m["id"])
	}
}

func TestReplacesRandomSlug(t *testing.T) {
	n := New()
	m := normJSON(t, n, `{"slug":"happy-eagle","title":"My session"}`)
	if m["slug"] != "<slug>" {
		t.Errorf("slug: want <slug>, got %v", m["slug"])
	}
	if m["title"] != "My session" {
		t.Errorf("a custom title must be preserved, got %v", m["title"])
	}
}

func TestReplacesSemverVersionButNotArbitraryVersionField(t *testing.T) {
	n := New()
	m := normJSON(t, n, `{"version":"1.15.11","model":{"version":"my-custom-tag"}}`)
	if m["version"] != "<ver>" {
		t.Errorf("semver version: want <ver>, got %v", m["version"])
	}
	// A non-semver "version" value is real data and must be compared, not masked.
	model := m["model"].(map[string]any)
	if model["version"] != "my-custom-tag" {
		t.Errorf("non-semver version must be preserved, got %v", model["version"])
	}
}

func TestReplacesTimestampEmbeddedInString(t *testing.T) {
	n := New()
	// opencode auto-titles sessions "New session - <RFC3339>".
	m := normJSON(t, n, `{"title":"New session - 2026-05-29T07:46:26.356Z"}`)
	if m["title"] != "New session - <ts>" {
		t.Errorf("embedded timestamp not normalized: %v", m["title"])
	}
}

func TestReplacesULIDEmbeddedInErrorMessage(t *testing.T) {
	n := New()
	m := normJSON(t, n, `{"name":"NotFoundError","data":{"message":"Session not found: ses_18d45416fffew1LNglYKqi08Ms"}}`)
	data := m["data"].(map[string]any)
	if data["message"] != "Session not found: <id>" {
		t.Errorf("embedded ULID not normalized: %v", data["message"])
	}
}

func TestPathReplacementIsDeterministicAcrossSymlinkForms(t *testing.T) {
	// /tmp/x is a substring of /private/tmp/x; longest-first must win so both
	// forms normalize identically regardless of registration order.
	n := New("/tmp/opcode42-conf-abc", "/private/tmp/opcode42-conf-abc")
	m := normJSON(t, n, `{"directory":"/private/tmp/opcode42-conf-abc","other":"/tmp/opcode42-conf-abc"}`)
	if m["directory"] != "<path>" {
		t.Errorf("directory: want <path>, got %v", m["directory"])
	}
	if m["other"] != "<path>" {
		t.Errorf("other: want <path>, got %v", m["other"])
	}
}

func TestPathReplacementCoversRelativeForm(t *testing.T) {
	// opencode's session "path" is the cwd with the leading "/" stripped. The
	// normalizer registers only the absolute dir, so the relative form must be
	// derived automatically — otherwise the random temp suffix diffs run-to-run.
	n := New("/tmp/opcode42-conf-abc")
	m := normJSON(t, n, `{"directory":"/tmp/opcode42-conf-abc","path":"tmp/opcode42-conf-abc"}`)
	if m["directory"] != "<path>" {
		t.Errorf("directory: want <path>, got %v", m["directory"])
	}
	if m["path"] != "<path>" {
		t.Errorf("relative path: want <path>, got %v", m["path"])
	}
}

func TestConfDirScrubbedWhenNotRegistered(t *testing.T) {
	// GET /session returns a global list spanning sibling scenarios' temp dirs,
	// which this client never registered. They must still be scrubbed so two runs
	// don't diff on the random opcode42-conf suffix.
	n := New("/tmp/opcode42-conf-100")
	m := normJSON(t, n, `{"directory":"/private/tmp/claude-501/opcode42-conf-999","path":"private/tmp/claude-501/opcode42-conf-999"}`)
	if m["directory"] != "<path>" || m["path"] != "<path>" {
		t.Errorf("unregistered conf dir not scrubbed: %v", m)
	}
}

func TestConfHomeScrubbedInPermissionPattern(t *testing.T) {
	// opencode bakes the per-run temp HOME into agent permission patterns; the
	// volatile mktemp prefix must collapse while the stable data-dir tail stays.
	n := New("/tmp/opcode42-conf-1")
	for _, home := range []string{
		"/tmp/tmp.24TAFncWtJ",
		"tmp/tmp.24TAFncWtJ", // leading-slash-stripped relative form
		"/var/folders/q5/abc123/T/tmp.iLVyqTr56n",
		"/private/var/folders/q5/abc123/T/tmp.iLVyqTr56n",
		"var/folders/q5/abc123/T/tmp.iLVyqTr56n",
	} {
		got := n.replacePaths(home + "/.local/share/opencode/tool-output/*")
		if got != "<path>/.local/share/opencode/tool-output/*" {
			t.Errorf("home %q not scrubbed: got %q", home, got)
		}
	}
}

func TestPermissionArrayIsOrderInsensitive(t *testing.T) {
	// opencode globs ~/.claude/skills/ in map-iteration order, so two runs emit
	// the same permission patterns in different positions. They must normalize
	// identically (E1).
	n := New()
	a := normJSON(t, n, `{"permission":[{"action":"allow","pattern":"a"},{"action":"allow","pattern":"b"}]}`)
	b := normJSON(t, n, `{"permission":[{"action":"allow","pattern":"b"},{"action":"allow","pattern":"a"}]}`)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("reordered permission arrays diverge:\n a=%s\n b=%s", ja, jb)
	}
}

func TestPermissionArrayMissingEntryStillDiffers(t *testing.T) {
	// Sorting must not mask a genuinely missing/extra entry — the multiset must
	// still change.
	n := New()
	full := normJSON(t, n, `{"permission":[{"pattern":"a"},{"pattern":"b"}]}`)
	short := normJSON(t, n, `{"permission":[{"pattern":"a"}]}`)
	jf, _ := json.Marshal(full)
	js, _ := json.Marshal(short)
	if string(jf) == string(js) {
		t.Error("a missing permission entry must still produce a difference")
	}
}

func TestNormalizeSSE(t *testing.T) {
	n := New()
	body := "event: message\n" +
		"data: {\"id\":\"evt_01JCABCDEFGHJKMNPQRSTVWXYZ\",\"type\":\"server.connected\",\"properties\":{}}\n\n" +
		"data: {\"id\":\"evt_01JCABCDEFGHJKMNPQRSTVWXY0\",\"type\":\"server.heartbeat\",\"properties\":{}}\n\n"
	events, err := n.NormalizeSSE(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d: %v", len(events), events)
	}
	want0 := `{"id":"<id>","properties":{},"type":"server.connected"}`
	if events[0] != want0 {
		t.Errorf("event 0:\n want %s\n got  %s", want0, events[0])
	}
}

func TestNormalizeSetJSONDedupsIdenticalSessionEntries(t *testing.T) {
	n := New()
	// A global session list whose entries differ only by volatile fields (id,
	// slug, created) — after normalization they are identical, so the set
	// collapses to one entry regardless of how many sessions accumulated.
	twoEntries := `[
		{"id":"ses_01JCABCDEFGHJKMNPQRSTVWXYZ","slug":"happy-eagle","time":{"created":1717000000000}},
		{"id":"ses_01JCABCDEFGHJKMNPQRSTVWXY0","slug":"brave-otter","time":{"created":1717000001000}}
	]`
	sevenEntries := `[
		{"id":"ses_01JD000000000000000000000A","slug":"a-a","time":{"created":1717000002000}},
		{"id":"ses_01JD000000000000000000000B","slug":"b-b","time":{"created":1717000003000}},
		{"id":"ses_01JD000000000000000000000C","slug":"c-c","time":{"created":1717000004000}}
	]`
	a, err := n.NormalizeSetJSON([]byte(twoEntries))
	if err != nil {
		t.Fatalf("NormalizeSetJSON(two): %v", err)
	}
	b, err := n.NormalizeSetJSON([]byte(sevenEntries))
	if err != nil {
		t.Fatalf("NormalizeSetJSON(three): %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("different-count lists of identical entries must normalize equal:\n %s\n %s", a, b)
	}
	// And the collapsed set is a single entry (the placeholder shape).
	var arr []any
	if err := json.Unmarshal(a, &arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("set must collapse identical entries to 1, got %d", len(arr))
	}
}

func TestNormalizeSetJSONKeepsDistinctEntries(t *testing.T) {
	n := New()
	// Two entries that differ in a STRUCTURAL field (title) stay distinct, so a
	// genuine shape difference is not masked by the set normalization.
	in := `[
		{"id":"ses_01JCABCDEFGHJKMNPQRSTVWXYZ","title":"alpha"},
		{"id":"ses_01JCABCDEFGHJKMNPQRSTVWXY0","title":"beta"}
	]`
	out, err := n.NormalizeSetJSON([]byte(in))
	if err != nil {
		t.Fatalf("NormalizeSetJSON: %v", err)
	}
	var arr []any
	if err := json.Unmarshal(out, &arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("distinct entries must be preserved, got %d", len(arr))
	}
}
