package message

import (
	"context"
	"errors"
	"testing"

	"github.com/rotemmiz/forge/internal/storage"
)

// seedSession inserts the project+session rows the message FK requires.
func seedSession(t *testing.T, db *storage.DB, sessionID string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO project (id, worktree, time_created, time_updated) VALUES ('p', '/tmp', 0, 0)`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated)
		 VALUES (?, 'p', 's', '/tmp', '1', 0, 0)`, sessionID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sessionID = "ses_test"
	seedSession(t, db, sessionID)
	return NewStore(db), sessionID
}

func TestStore_MessageAndPartRoundTrip(t *testing.T) {
	s, sessionID := newTestStore(t)
	ctx := context.Background()

	u := &UserMessage{ID: "msg_001", SessionID: sessionID, Role: RoleUser, Agent: "build",
		Model: Model{ProviderID: "openai", ModelID: "gpt-4o"}}
	u.Time.Created = 100
	if err := s.PutMessage(ctx, Info{User: u}); err != nil {
		t.Fatalf("put user: %v", err)
	}
	text := &TextPart{PartBase: PartBase{ID: "prt_1", SessionID: sessionID, MessageID: "msg_001"}, Type: "text", Text: "hello"}
	if err := s.PutPart(ctx, text); err != nil {
		t.Fatalf("put part: %v", err)
	}

	got, err := s.GetMessage(ctx, sessionID, "msg_001")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if got.Info.User == nil || got.Info.User.Agent != "build" || got.Info.User.Model.ModelID != "gpt-4o" {
		t.Fatalf("user round-trip mismatch: %+v", got.Info.User)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(got.Parts))
	}
	tp, ok := got.Parts[0].(*TextPart)
	if !ok || tp.Text != "hello" {
		t.Fatalf("text part round-trip mismatch: %+v", got.Parts[0])
	}
}

func TestStore_PutPartUpsertPreservesCreated(t *testing.T) {
	s, sessionID := newTestStore(t)
	ctx := context.Background()
	u := &UserMessage{ID: "msg_001", SessionID: sessionID, Role: RoleUser, Agent: "build"}
	if err := s.PutMessage(ctx, Info{User: u}); err != nil {
		t.Fatal(err)
	}
	part := &TextPart{PartBase: PartBase{ID: "prt_1", SessionID: sessionID, MessageID: "msg_001"}, Type: "text", Text: "a"}
	if err := s.PutPart(ctx, part); err != nil {
		t.Fatal(err)
	}
	var created1 int64
	if err := s.db.QueryRow("SELECT time_created FROM part WHERE id='prt_1'").Scan(&created1); err != nil {
		t.Fatal(err)
	}
	part.Text = "ab"
	if err := s.PutPart(ctx, part); err != nil {
		t.Fatal(err)
	}
	var created2 int64
	if err := s.db.QueryRow("SELECT time_created FROM part WHERE id='prt_1'").Scan(&created2); err != nil {
		t.Fatal(err)
	}
	if created1 != created2 {
		t.Fatalf("time_created changed on update: %d -> %d", created1, created2)
	}
	got, _ := s.GetPart(ctx, "prt_1")
	if got.(*TextPart).Text != "ab" {
		t.Fatalf("update not persisted: %q", got.(*TextPart).Text)
	}
}

func TestStore_ListAndStreamOrdering(t *testing.T) {
	s, sessionID := newTestStore(t)
	ctx := context.Background()
	for i, id := range []string{"msg_001", "msg_002", "msg_003"} {
		u := &UserMessage{ID: id, SessionID: sessionID, Role: RoleUser, Agent: "build"}
		u.Time.Created = int64((i + 1) * 100)
		if err := s.PutMessage(ctx, Info{User: u}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Info.ID() != "msg_001" || list[2].Info.ID() != "msg_003" {
		t.Fatalf("List should be oldest-first: %v", idsOf(list))
	}
	stream, err := s.Stream(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if stream[0].Info.ID() != "msg_003" || stream[2].Info.ID() != "msg_001" {
		t.Fatalf("Stream should be newest-first: %v", idsOf(stream))
	}
}

func TestStore_NotFound(t *testing.T) {
	s, sessionID := newTestStore(t)
	if _, err := s.GetMessage(context.Background(), sessionID, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.GetPart(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func idsOf(ms []WithParts) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Info.ID()
	}
	return out
}
