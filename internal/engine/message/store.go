package message

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rotemmiz/forge/internal/storage"
)

// ErrNotFound is returned when a message or part id does not exist.
var ErrNotFound = errors.New("message not found")

// Store persists messages and parts in the plan-01 schema (message/part tables,
// data JSON columns). Writes are serialized by mu over the single-connection
// storage layer, matching the session store's discipline.
type Store struct {
	db *storage.DB
	mu sync.Mutex
}

// NewStore returns a message store backed by db.
func NewStore(db *storage.DB) *Store { return &Store{db: db} }

// PutMessage upserts a message, preserving time_created across updates.
func (s *Store) PutMessage(ctx context.Context, info Info) error {
	data, err := info.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	now := time.Now().UnixMilli()
	created := infoCreated(info)
	if created == 0 {
		created = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO message (id, session_id, role, data, time_created, time_updated)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   role=excluded.role, data=excluded.data, time_updated=excluded.time_updated`,
		info.ID(), sessionOf(info), info.Role(), string(data), created, now)
	if err != nil {
		return fmt.Errorf("put message: %w", err)
	}
	return nil
}

// PutPart upserts a part, preserving time_created across updates.
func (s *Store) PutPart(ctx context.Context, p Part) error {
	data, err := MarshalPart(p)
	if err != nil {
		return fmt.Errorf("marshal part: %w", err)
	}
	b := p.base()
	now := time.Now().UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO part (id, message_id, session_id, type, data, time_created, time_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   type=excluded.type, data=excluded.data, time_updated=excluded.time_updated`,
		b.ID, b.MessageID, b.SessionID, p.partType(), string(data), now, now)
	if err != nil {
		return fmt.Errorf("put part: %w", err)
	}
	return nil
}

// DeletePart removes a part and reports whether a row existed.
func (s *Store) DeletePart(ctx context.Context, partID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, "DELETE FROM part WHERE id = ?", partID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteMessage removes a message and all its parts, scoped to (sessionID,
// messageID), and reports whether the message row existed. It mirrors
// opencode's message.removed projector, which deletes the MessageTable row and
// its PartTable rows (projectors.ts:145-169). The two deletes run under mu so a
// concurrent put can't interleave; a missing message reports ok=false without
// error so the handler can 404 the same way opencode's requireSession does for
// the parent session.
func (s *Store) DeleteMessage(ctx context.Context, sessionID, messageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM part WHERE message_id = ? AND session_id = ?", messageID, sessionID); err != nil {
		return false, fmt.Errorf("delete parts: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM message WHERE id = ? AND session_id = ?", messageID, sessionID)
	if err != nil {
		return false, fmt.Errorf("delete message: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetPart returns a single part by id, or ErrNotFound.
func (s *Store) GetPart(ctx context.Context, partID string) (Part, error) {
	var data string
	err := s.db.QueryRowContext(ctx, "SELECT data FROM part WHERE id = ?", partID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return UnmarshalPart([]byte(data))
}

// Parts returns a message's parts ordered by id (message-v2.ts:984-987).
func (s *Store) Parts(ctx context.Context, messageID string) ([]Part, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM part WHERE message_id = ? ORDER BY id", messageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	parts := []Part{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		p, err := UnmarshalPart([]byte(data))
		if err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

// GetMessage returns one message with its parts, or ErrNotFound.
func (s *Store) GetMessage(ctx context.Context, sessionID, messageID string) (WithParts, error) {
	var data string
	err := s.db.QueryRowContext(ctx,
		"SELECT data FROM message WHERE id = ? AND session_id = ?", messageID, sessionID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return WithParts{}, ErrNotFound
	}
	if err != nil {
		return WithParts{}, err
	}
	info, err := UnmarshalInfo([]byte(data))
	if err != nil {
		return WithParts{}, err
	}
	parts, err := s.Parts(ctx, messageID)
	if err != nil {
		return WithParts{}, err
	}
	return WithParts{Info: info, Parts: parts}, nil
}

// List returns a session's messages with parts, oldest-first (chronological) —
// the order the REST GET /session/:id/message endpoint serves.
func (s *Store) List(ctx context.Context, sessionID string) ([]WithParts, error) {
	return s.load(ctx, sessionID, true)
}

// Stream returns a session's messages newest-first, matching opencode's
// MessageV2.stream ordering — the input FilterCompacted expects.
func (s *Store) Stream(ctx context.Context, sessionID string) ([]WithParts, error) {
	return s.load(ctx, sessionID, false)
}

func (s *Store) load(ctx context.Context, sessionID string, asc bool) ([]WithParts, error) {
	order := "DESC"
	if asc {
		order = "ASC"
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created "+order+", id "+order, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type rec struct {
		id   string
		info Info
	}
	var recs []rec
	for rows.Next() {
		var id, data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		info, err := UnmarshalInfo([]byte(data))
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec{id: id, info: info})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]WithParts, 0, len(recs))
	for _, r := range recs {
		parts, err := s.Parts(ctx, r.id)
		if err != nil {
			return nil, err
		}
		out = append(out, WithParts{Info: r.info, Parts: parts})
	}
	return out, nil
}

func sessionOf(info Info) string {
	if info.User != nil {
		return info.User.SessionID
	}
	if info.Assistant != nil {
		return info.Assistant.SessionID
	}
	return ""
}

func infoCreated(info Info) int64 {
	if info.User != nil {
		return info.User.Time.Created
	}
	if info.Assistant != nil {
		return info.Assistant.Time.Created
	}
	return 0
}
