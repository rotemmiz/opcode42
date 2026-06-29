package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/storage"
)

func user(id string) message.WithParts {
	u := &message.UserMessage{ID: id, SessionID: "s", Role: message.RoleUser}
	return message.WithParts{Info: message.Info{User: u},
		Parts: []message.Part{&message.TextPart{PartBase: message.PartBase{ID: "p" + id, SessionID: "s", MessageID: id}, Type: "text", Text: "hi"}}}
}

func assistant(id, parent string) message.WithParts {
	a := &message.AssistantMessage{ID: id, SessionID: "s", Role: message.RoleAssistant, ParentID: parent, Finish: "stop"}
	return message.WithParts{Info: message.Info{Assistant: a}}
}

func TestSelectTail(t *testing.T) {
	// 4 user turns (u1..u4); keep last 2 -> head is turns u1,u2.
	history := []message.WithParts{
		user("u1"), assistant("a1", "u1"),
		user("u2"), assistant("a2", "u2"),
		user("u3"), assistant("a3", "u3"),
		user("u4"), assistant("a4", "u4"),
	}
	head, tail := selectTail(history, 2)
	if tail != "u3" {
		t.Fatalf("tail start = %q, want u3", tail)
	}
	if len(head) != 4 { // u1,a1,u2,a2
		t.Fatalf("head len = %d, want 4", len(head))
	}
	if head[0].Info.ID() != "u1" || head[3].Info.ID() != "a2" {
		t.Fatalf("head wrong: %v", []string{head[0].Info.ID(), head[3].Info.ID()})
	}
}

func TestSelectTail_NotEnoughTurns(t *testing.T) {
	history := []message.WithParts{user("u1"), assistant("a1", "u1")}
	head, tail := selectTail(history, 2)
	if head != nil || tail != "" {
		t.Fatalf("too-short history should yield no head, got head=%d tail=%q", len(head), tail)
	}
}

func TestSelectTail_ZeroTurnsDisabled(t *testing.T) {
	history := []message.WithParts{user("u1"), assistant("a1", "u1"), user("u2"), assistant("a2", "u2"), user("u3")}
	if head, tail := selectTail(history, 0); head != nil || tail != "" {
		t.Fatalf("tailTurns=0 disables compaction, got head=%d tail=%q", len(head), tail)
	}
}

// seedToolTurn inserts a user turn plus an assistant message carrying one
// completed tool part with the given output.
func seedToolTurn(t *testing.T, store *message.Store, sid, uid, aid, output string, created int64) {
	t.Helper()
	ctx := context.Background()
	u := &message.UserMessage{ID: uid, SessionID: sid, Role: message.RoleUser, Agent: "build"}
	u.Time.Created = created
	if err := store.PutMessage(ctx, message.Info{User: u}); err != nil {
		t.Fatal(err)
	}
	a := &message.AssistantMessage{ID: aid, SessionID: sid, Role: message.RoleAssistant, ParentID: uid,
		ProviderID: "openai", ModelID: "gpt-4o", Finish: "stop"}
	a.Time.Created = created + 1
	if err := store.PutMessage(ctx, message.Info{Assistant: a}); err != nil {
		t.Fatal(err)
	}
	st := message.ToolStateCompleted{Status: message.ToolCompleted, Input: map[string]any{}, Output: output, Title: "Bash", Metadata: map[string]any{}}
	st.Time.End = 1
	state, _ := json.Marshal(st)
	if err := store.PutPart(ctx, &message.ToolPart{
		PartBase: message.PartBase{ID: "prt_" + aid, SessionID: sid, MessageID: aid}, Type: "tool", CallID: aid, Tool: "bash", State: state}); err != nil {
		t.Fatal(err)
	}
}

func TestPrune_MarksOldToolOutputsProtectsRecent(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sid = "ses_prune"
	if _, err := db.Exec(`INSERT INTO project (id, worktree, time_created, time_updated) VALUES ('p','/tmp',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated)
		VALUES (?, 'p','s','/tmp','1',0,0)`, sid); err != nil {
		t.Fatal(err)
	}
	store := message.NewStore(db)
	big := strings.Repeat("x", 100_000) // ~25k tokens each

	// Four older turns with big tool outputs, then two recent small turns.
	seedToolTurn(t, store, sid, "msg_01u", "msg_01a", big, 1)
	seedToolTurn(t, store, sid, "msg_02u", "msg_02a", big, 3)
	seedToolTurn(t, store, sid, "msg_03u", "msg_03a", big, 5)
	seedToolTurn(t, store, sid, "msg_04u", "msg_04a", big, 7)
	seedToolTurn(t, store, sid, "msg_05u", "msg_05a", "small", 9)
	seedToolTurn(t, store, sid, "msg_06u", "msg_06a", "small", 11)

	New(Config{Store: store}).prune(context.Background(), sid)

	compacted, kept := 0, 0
	for _, aid := range []string{"msg_01a", "msg_02a", "msg_03a", "msg_04a", "msg_05a", "msg_06a"} {
		parts, _ := store.Parts(context.Background(), aid)
		var st message.ToolStateCompleted
		_ = json.Unmarshal(parts[0].(*message.ToolPart).State, &st)
		if st.Time.Compacted != nil {
			compacted++
		} else {
			kept++
		}
	}
	if compacted == 0 {
		t.Fatalf("expected old tool outputs pruned, got 0")
	}
	if kept < 2 {
		t.Fatalf("prune must protect recent turns, kept=%d", kept)
	}
	// The two most recent turns' outputs must NOT be pruned.
	for _, aid := range []string{"msg_05a", "msg_06a"} {
		parts, _ := store.Parts(context.Background(), aid)
		var st message.ToolStateCompleted
		_ = json.Unmarshal(parts[0].(*message.ToolPart).State, &st)
		if st.Time.Compacted != nil {
			t.Fatalf("%s (recent) must not be pruned", aid)
		}
	}
}
