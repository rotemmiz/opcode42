package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/worktree"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "forge.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db)
}

func TestCreateWireShape(t *testing.T) {
	store := newTestStore(t)
	dir := worktree.Resolve(t.TempDir())

	info, err := store.Create(context.Background(), dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.HasPrefix(info.ID, "ses_") {
		t.Errorf("id %q missing ses_ prefix", info.ID)
	}
	if info.ProjectID != "global" {
		t.Errorf("projectID = %q, want global (non-git dir)", info.ProjectID)
	}
	if info.Directory != dir {
		t.Errorf("directory = %q, want %q", info.Directory, dir)
	}
	if !strings.HasPrefix(info.Title, titlePrefix) {
		t.Errorf("title = %q, want %q prefix", info.Title, titlePrefix)
	}
	if info.Version != DefaultCompatVersion {
		t.Errorf("version = %q, want %q", info.Version, DefaultCompatVersion)
	}
	if info.Time.Created == 0 || info.Time.Updated == 0 {
		t.Errorf("timestamps not set: %+v", info.Time)
	}
	// path is dir relative to the worktree root; for a non-git temp dir the root
	// is "/" so path is the dir without its leading slash.
	if strings.HasPrefix(info.Path, "/") || info.Path == "" {
		t.Errorf("path = %q, want non-empty and not absolute", info.Path)
	}
}

func TestGetListDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := worktree.Resolve(t.TempDir())

	created, err := store.Create(ctx, dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID || got.Title != created.Title {
		t.Errorf("Get mismatch: %+v vs %+v", got, created)
	}

	list, err := store.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v len=%d, want 1", err, len(list))
	}

	ok, err := store.Delete(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("Delete: ok=%v err=%v", ok, err)
	}
	if _, err := store.Get(ctx, created.ID); err != ErrNotFound {
		t.Errorf("Get after delete: err=%v, want ErrNotFound", err)
	}
}

func TestForkTitleAndNoParent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := worktree.Resolve(t.TempDir())

	parent, err := store.Create(ctx, dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	forked, err := store.Fork(ctx, parent.ID)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if forked.ID == parent.ID {
		t.Error("fork must have a new id")
	}
	if forked.ParentID != "" {
		t.Errorf("parentID = %q, want empty (opencode quirk)", forked.ParentID)
	}
	if want := parent.Title + " (fork #1)"; forked.Title != want {
		t.Errorf("fork title = %q, want %q", forked.Title, want)
	}
	// children of parent is empty (fork does not link back).
	kids, err := store.Children(ctx, parent.ID)
	if err != nil || len(kids) != 0 {
		t.Errorf("Children = %v (len %d), want empty", kids, len(kids))
	}
}

func TestForkIncrementsCounter(t *testing.T) {
	if got := forkedTitle("New session - X"); got != "New session - X (fork #1)" {
		t.Errorf("forkedTitle = %q", got)
	}
	if got := forkedTitle("New session - X (fork #1)"); got != "New session - X (fork #2)" {
		t.Errorf("forkedTitle increment = %q", got)
	}
	if got := forkedTitle("New session - X (fork #9)"); got != "New session - X (fork #10)" {
		t.Errorf("forkedTitle two-digit = %q", got)
	}
}

func TestForkMissingParent(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Fork(context.Background(), "ses_nonexistent00000000000000"); err != ErrNotFound {
		t.Errorf("Fork(missing) err=%v, want ErrNotFound", err)
	}
}

func TestUpdateTitleAndArchived(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := worktree.Resolve(t.TempDir())

	created, err := store.Create(ctx, dir)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Time.Archived != nil {
		t.Fatalf("new session already archived: %+v", created.Time)
	}

	// Title-only update leaves archived untouched and returns the full session.
	newTitle := "renamed session"
	got, err := store.Update(ctx, created.ID, UpdateParams{Title: &newTitle})
	if err != nil {
		t.Fatalf("Update(title): %v", err)
	}
	if got.Title != newTitle {
		t.Errorf("title = %q, want %q", got.Title, newTitle)
	}
	if got.Time.Archived != nil {
		t.Errorf("title-only update set archived: %+v", got.Time)
	}
	// Persisted: a fresh Get sees the new title.
	if reread, _ := store.Get(ctx, created.ID); reread.Title != newTitle {
		t.Errorf("persisted title = %q, want %q", reread.Title, newTitle)
	}

	// Archive: set time.archived to an epoch-ms, title preserved.
	ts := int64(1717000000000)
	got, err = store.Update(ctx, created.ID, UpdateParams{Archived: &ts})
	if err != nil {
		t.Fatalf("Update(archive): %v", err)
	}
	if got.Time.Archived == nil || *got.Time.Archived != ts {
		t.Errorf("archived = %v, want %d", got.Time.Archived, ts)
	}
	if got.Title != newTitle {
		t.Errorf("archive update clobbered title: %q", got.Title)
	}
	if reread, _ := store.Get(ctx, created.ID); reread.Time.Archived == nil || *reread.Time.Archived != ts {
		t.Errorf("archived not persisted: %+v", reread.Time)
	}

	// A title-only update after archiving leaves the archived timestamp intact
	// (opencode has no un-archive path; only a number ever sets time.archived).
	newer := "renamed again"
	got, err = store.Update(ctx, created.ID, UpdateParams{Title: &newer})
	if err != nil {
		t.Fatalf("Update(title after archive): %v", err)
	}
	if got.Time.Archived == nil || *got.Time.Archived != ts {
		t.Errorf("title-only update cleared archived: %+v", got.Time)
	}
}

func TestUpdateMissingSession(t *testing.T) {
	store := newTestStore(t)
	title := "x"
	if _, err := store.Update(context.Background(), "ses_nonexistent00000000000000", UpdateParams{Title: &title}); err != ErrNotFound {
		t.Errorf("Update(missing) err=%v, want ErrNotFound", err)
	}
}

func TestUpdateNoFieldsReturnsCurrent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	created, err := store.Create(ctx, worktree.Resolve(t.TempDir()))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Update(ctx, created.ID, UpdateParams{})
	if err != nil {
		t.Fatalf("Update(no fields): %v", err)
	}
	if got.ID != created.ID || got.Title != created.Title {
		t.Errorf("no-op update changed session: %+v", got)
	}
	// A no-op patch on a missing session still 404s.
	if _, err := store.Update(ctx, "ses_nonexistent00000000000000", UpdateParams{}); err != ErrNotFound {
		t.Errorf("no-op Update(missing) err=%v, want ErrNotFound", err)
	}
}
