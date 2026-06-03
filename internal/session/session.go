package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/rotemmiz/forge/internal/id"
	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/worktree"
)

// DefaultCompatVersion is the opencode wire version Forge stamps into the
// session "version" field. It is the version of the frozen contract Forge
// targets, NOT Forge's own build version (which /global/health reports). The
// conformance normalizer collapses this field so dual diffs stay
// build-independent (see the user-approved "compat constant + normalize"
// decision in the plan).
const DefaultCompatVersion = "1.15.11"

// globalProjectID is the project id opencode assigns to sessions whose
// directory is not inside a git worktree (worktree resolves to "/").
const globalProjectID = "global"

// titlePrefix is opencode's default session title prefix; the timestamp that
// follows is an ISO-8601 (RFC3339 millis) string (session/session.ts:46,50).
const titlePrefix = "New session - "

// ErrNotFound is returned by Get/Fork when no session matches the id.
var ErrNotFound = errors.New("session not found")

// forkedTitleRe matches an already-forked title so the fork counter increments
// (session/session.ts:148).
var forkedTitleRe = regexp.MustCompile(`^(.+) \(fork #(\d+)\)$`)

// defaultTitleRe matches a freshly-created default title (titlePrefix + RFC3339
// millis). Title generation only overwrites a title still matching this, the
// way opencode's isDefaultTitle gates auto-titling (session/session.ts:53-57).
var defaultTitleRe = regexp.MustCompile(`^New session - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// Info is the session wire shape. Optional fields are omitted when empty to
// match opencode's optionalOmitUndefined schema (session/session.ts:208-224).
type Info struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	ProjectID string `json:"projectID"`
	Directory string `json:"directory"`
	// Path is always present on the wire (opencode emits "" rather than omitting;
	// optionalOmitUndefined drops only undefined, and Forge always sets a value).
	Path     string  `json:"path"`
	ParentID string  `json:"parentID,omitempty"`
	Title    string  `json:"title"`
	Agent    string  `json:"agent,omitempty"`
	Version  string  `json:"version"`
	Cost     float64 `json:"cost"`
	Tokens   Tokens  `json:"tokens"`
	Time     Time    `json:"time"`
}

// Tokens mirrors opencode's token accounting block.
type Tokens struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	Cache     Cache   `json:"cache"`
}

// Cache holds prompt-cache token counters.
type Cache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// Time holds the session timestamps (epoch milliseconds).
type Time struct {
	Created    int64  `json:"created"`
	Updated    int64  `json:"updated"`
	Compacting *int64 `json:"compacting,omitempty"`
	Archived   *int64 `json:"archived,omitempty"`
}

// Store persists sessions. Writes are serialized by mu on top of the
// single-connection storage layer.
type Store struct {
	db            *storage.DB
	mu            sync.Mutex
	CompatVersion string
}

// NewStore returns a session store backed by db.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db, CompatVersion: DefaultCompatVersion}
}

// Create makes a new session rooted at the (already symlink-resolved) directory
// dir, computing projectID/path from the enclosing worktree the way opencode
// does (session/session.ts:157-158,669-670).
func (s *Store) Create(ctx context.Context, dir string) (Info, error) {
	now := time.Now().UnixMilli()
	root := worktree.Root(dir)
	info := Info{
		ID:        id.Descending(id.Session),
		Slug:      randomSlug(),
		ProjectID: projectID(root),
		Directory: dir,
		Path:      worktree.RelPath(root, dir),
		Title:     titlePrefix + time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Version:   s.CompatVersion,
		Time:      Time{Created: now, Updated: now},
	}
	if err := s.insert(ctx, info, root); err != nil {
		return Info{}, err
	}
	return info, nil
}

// CreateChild makes a session like Create but linked to a parent (the subagent
// task tool spawns these; GET /children returns them). opencode's subagent
// sessions set parentID (session/session.ts).
func (s *Store) CreateChild(ctx context.Context, dir, parentID string) (Info, error) {
	now := time.Now().UnixMilli()
	root := worktree.Root(dir)
	info := Info{
		ID:        id.Descending(id.Session),
		ParentID:  parentID,
		Slug:      randomSlug(),
		ProjectID: projectID(root),
		Directory: dir,
		Path:      worktree.RelPath(root, dir),
		Title:     titlePrefix + time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Version:   s.CompatVersion,
		Time:      Time{Created: now, Updated: now},
	}
	if err := s.insert(ctx, info, root); err != nil {
		return Info{}, err
	}
	return info, nil
}

// Fork creates a new session derived from an existing one: same directory,
// path, and project, a new id, and a "(fork #N)" title. It deliberately does
// NOT set parentID — matching opencode 1.15.x's observed behavior, where forked
// children do not link back and GET /children returns [] (verify.md finding).
func (s *Store) Fork(ctx context.Context, parentID string) (Info, error) {
	parent, err := s.Get(ctx, parentID)
	if err != nil {
		return Info{}, err
	}
	now := time.Now().UnixMilli()
	info := Info{
		ID:        id.Descending(id.Session),
		Slug:      randomSlug(),
		ProjectID: parent.ProjectID,
		Directory: parent.Directory,
		Path:      parent.Path,
		Title:     forkedTitle(parent.Title),
		Version:   s.CompatVersion,
		Time:      Time{Created: now, Updated: now},
	}
	if err := s.insert(ctx, info, worktree.Root(parent.Directory)); err != nil {
		return Info{}, err
	}
	return info, nil
}

// Get returns the session by id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, sessionID string) (Info, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+" WHERE id = ?", sessionID)
	info, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, ErrNotFound
	}
	return info, err
}

// Title returns the current title of the session, or ErrNotFound.
func (s *Store) Title(ctx context.Context, sessionID string) (string, error) {
	info, err := s.Get(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return info.Title, nil
}

// SetTitle updates the session's title and bumps time_updated. It is a no-op for
// a missing session (RowsAffected == 0); callers fire it as best-effort during
// title generation.
func (s *Store) SetTitle(ctx context.Context, sessionID, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		"UPDATE session SET title = ?, time_updated = ? WHERE id = ?",
		title, time.Now().UnixMilli(), sessionID)
	return err
}

// IsDefaultTitle reports whether title is a still-untouched auto-generated
// default (the prefix + RFC3339-millis timestamp Create stamps).
func (s *Store) IsDefaultTitle(title string) bool {
	return defaultTitleRe.MatchString(title)
}

// List returns sessions newest-first, matching opencode's default page:
// ORDER BY time_updated DESC, LIMIT 100 (session.ts:927,934). The id DESC
// tie-break makes equal-timestamp ordering deterministic.
func (s *Store) List(ctx context.Context) ([]Info, error) {
	rows, err := s.db.QueryContext(ctx,
		selectColumns+" ORDER BY time_updated DESC, id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Info{}
	for rows.Next() {
		info, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// Delete removes the session and reports whether a row existed.
func (s *Store) Delete(ctx context.Context, sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, "DELETE FROM session WHERE id = ?", sessionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Children returns the sessions whose parentID is sessionID. With fork not
// linking parents (see Fork), this is currently always empty — matching
// opencode's observed GET /children behavior.
func (s *Store) Children(ctx context.Context, sessionID string) ([]Info, error) {
	rows, err := s.db.QueryContext(ctx,
		selectColumns+" WHERE parent_id = ? ORDER BY time_updated DESC, id DESC", sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []Info{}
	for rows.Next() {
		info, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

// insert writes a session row, first ensuring its project row exists (the
// session.project_id foreign key requires it).
func (s *Store) insert(ctx context.Context, info Info, root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO project (id, worktree, time_created, time_updated)
		 VALUES (?, ?, ?, ?)`,
		info.ProjectID, root, now, now); err != nil {
		return fmt.Errorf("ensure project: %w", err)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session
		 (id, project_id, slug, directory, path, parent_id, title, version,
		  cost, tokens_input, tokens_output, tokens_reasoning,
		  tokens_cache_read, tokens_cache_write, time_created, time_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, ?, ?)`,
		info.ID, info.ProjectID, info.Slug, info.Directory, nullString(info.Path),
		nullString(info.ParentID), info.Title, info.Version,
		info.Time.Created, info.Time.Updated)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// selectColumns is the canonical column list for reads, kept in sync with scan.
const selectColumns = `SELECT id, project_id, slug, directory, path, parent_id, title, version,
	cost, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write,
	time_created, time_updated, time_archived FROM session`

type scanner interface {
	Scan(dest ...any) error
}

func scan(s scanner) (Info, error) {
	var (
		info     Info
		path     sql.NullString
		parent   sql.NullString
		archived sql.NullInt64
	)
	err := s.Scan(
		&info.ID, &info.ProjectID, &info.Slug, &info.Directory, &path, &parent,
		&info.Title, &info.Version, &info.Cost,
		&info.Tokens.Input, &info.Tokens.Output, &info.Tokens.Reasoning,
		&info.Tokens.Cache.Read, &info.Tokens.Cache.Write,
		&info.Time.Created, &info.Time.Updated, &archived,
	)
	if err != nil {
		return Info{}, err
	}
	if path.Valid {
		info.Path = path.String
	}
	if parent.Valid {
		info.ParentID = parent.String
	}
	if archived.Valid {
		info.Time.Archived = &archived.Int64
	}
	return info, nil
}

func projectID(root string) string {
	if root == "/" {
		return globalProjectID
	}
	return root
}

// forkedTitle increments a "(fork #N)" suffix, or appends "(fork #1)"
// (session/session.ts:147-154).
func forkedTitle(title string) string {
	if m := forkedTitleRe.FindStringSubmatch(title); m != nil {
		var n int
		_, _ = fmt.Sscanf(m[2], "%d", &n)
		return fmt.Sprintf("%s (fork #%d)", m[1], n+1)
	}
	return title + " (fork #1)"
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const slugAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// randomSlug returns a short random slug. opencode uses word-based slugs, but
// the value is server-generated and the conformance normalizer collapses it to
// <slug>, so any unique token suffices.
func randomSlug() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("session: crypto/rand failed: %v", err))
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = slugAlphabet[int(b)%len(slugAlphabet)]
	}
	return string(out)
}
