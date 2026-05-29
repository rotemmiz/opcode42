package message

import "testing"

// Builders for compaction/latest scenarios (ported from message-v2.test.ts).

func compactionUserMsg(id, tailStart string) WithParts {
	return WithParts{Info: Info{User: &UserMessage{ID: id, SessionID: "session", Role: RoleUser, Agent: "user"}},
		Parts: []Part{&CompactionPart{PartBase: base(id, "prt_"+id), Type: "compaction", Auto: true, TailStartID: tailStart}}}
}

func finishedAssistant(id, parentID, finish string, summary bool) WithParts {
	a := &AssistantMessage{ID: id, SessionID: "session", Role: RoleAssistant, ParentID: parentID,
		ProviderID: testProvider, ModelID: testModelID, Agent: "agent", Finish: finish, Summary: summary,
		Path: Path{CWD: "/", Root: "/"}}
	return WithParts{Info: Info{Assistant: a}}
}

const (
	tailUserID    = "msg_001"
	overflowID    = "msg_002"
	compactionID  = "msg_003"
	summaryID     = "msg_004"
	continueID    = "msg_005"
	newCompaction = "msg_006"
)

func scenarioMessages() (tail, overflow, compaction, summary, cont WithParts) {
	tail = userMsg(tailUserID, &TextPart{PartBase: base(tailUserID, "prt_t1"), Type: "text", Text: "original prompt"})
	overflow = finishedAssistant(overflowID, tailUserID, "tool-calls", false)
	compaction = compactionUserMsg(compactionID, tailUserID)
	summary = finishedAssistant(summaryID, compactionID, "stop", true)
	cont = userMsg(continueID, &TextPart{PartBase: base(continueID, "prt_c1"), Type: "text", Text: "Continue...", Synthetic: true})
	return
}

func TestFilterCompacted_ReordersAndLatestPicksSummary(t *testing.T) {
	tail, overflow, compaction, summary, cont := scenarioMessages()
	// Input is newest-first (opencode stream order).
	filtered := FilterCompacted([]WithParts{cont, summary, compaction, overflow, tail})

	ids := make([]string, len(filtered))
	for i, m := range filtered {
		ids[i] = m.Info.ID()
	}
	want := []string{compactionID, summaryID, tailUserID, overflowID, continueID}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("reorder mismatch: got %v want %v", ids, want)
		}
	}

	state := LatestOf(filtered)
	if state.Finished == nil || state.Finished.ID != summaryID || !state.Finished.Summary {
		t.Fatalf("finished should be summary %s, got %+v", summaryID, state.Finished)
	}
	if state.User == nil || state.User.ID != continueID {
		t.Fatalf("user should be %s, got %+v", continueID, state.User)
	}
	if len(state.Tasks) != 0 {
		t.Fatalf("tasks should be empty, got %d", len(state.Tasks))
	}
}

func TestLatest_FreshCompactionSurfacesAsTask(t *testing.T) {
	tail, overflow, compaction, summary, cont := scenarioMessages()
	fresh := WithParts{Info: Info{User: &UserMessage{ID: newCompaction, SessionID: "session", Role: RoleUser, Agent: "user"}},
		Parts: []Part{&CompactionPart{PartBase: base(newCompaction, "prt_n1"), Type: "compaction", Auto: true}}}

	state := LatestOf([]WithParts{tail, overflow, compaction, summary, cont, fresh})
	if state.Finished == nil || state.Finished.ID != summaryID {
		t.Fatalf("finished should be %s, got %+v", summaryID, state.Finished)
	}
	if state.User == nil || state.User.ID != newCompaction {
		t.Fatalf("user should be %s, got %+v", newCompaction, state.User)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(state.Tasks))
	}
	if c, ok := state.Tasks[0].(*CompactionPart); !ok || !c.Auto {
		t.Fatalf("want auto compaction task, got %+v", state.Tasks[0])
	}
}

func TestFilterCompacted_NoCompactionPassthrough(t *testing.T) {
	u := userMsg("msg_001", &TextPart{PartBase: base("msg_001", "prt_1"), Type: "text", Text: "hi"})
	a := finishedAssistant("msg_002", "msg_001", "stop", false)
	// newest-first input -> oldest-first output, unchanged.
	out := FilterCompacted([]WithParts{a, u})
	if len(out) != 2 || out[0].Info.ID() != "msg_001" || out[1].Info.ID() != "msg_002" {
		t.Fatalf("passthrough reorder failed: %v", out)
	}
}
