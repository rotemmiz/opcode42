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
	n := New("/tmp/forge-test-project")
	in := `{"directory":"/tmp/forge-test-project/src","worktree":"/tmp/forge-test-project"}`
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
	n := New("/tmp/forge-conf-abc", "/private/tmp/forge-conf-abc")
	m := normJSON(t, n, `{"directory":"/private/tmp/forge-conf-abc","other":"/tmp/forge-conf-abc"}`)
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
	n := New("/tmp/forge-conf-abc")
	m := normJSON(t, n, `{"directory":"/tmp/forge-conf-abc","path":"tmp/forge-conf-abc"}`)
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
	// don't diff on the random forge-conf suffix.
	n := New("/tmp/forge-conf-100")
	m := normJSON(t, n, `{"directory":"/private/tmp/claude-501/forge-conf-999","path":"private/tmp/claude-501/forge-conf-999"}`)
	if m["directory"] != "<path>" || m["path"] != "<path>" {
		t.Errorf("unregistered conf dir not scrubbed: %v", m)
	}
}

func TestConfHomeScrubbedInPermissionPattern(t *testing.T) {
	// opencode bakes the per-run temp HOME into agent permission patterns; the
	// volatile mktemp prefix must collapse while the stable data-dir tail stays.
	n := New("/tmp/forge-conf-1")
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
