package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	_ "modernc.org/sqlite"
)

type DB struct {
	db    *sql.DB
	paths Paths
}

func Open(ctx context.Context, paths ...Paths) (*DB, error) {
	p, err := EnsurePaths(paths...)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", p.DBFile)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	store := &DB{db: database, paths: p}
	if err := store.migrate(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *DB) Close() error { return s.db.Close() }
func (s *DB) Paths() Paths { return s.paths }

func (s *DB) migrate(ctx context.Context) error {
	for _, statement := range []string{`PRAGMA journal_mode = WAL`, `PRAGMA foreign_keys = ON`, `PRAGMA busy_timeout = 5000`} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure state database: %w", err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS trusted_workspaces (path TEXT PRIMARY KEY, created_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, workspace_path TEXT NOT NULL, title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, role TEXT NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL, target TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS checkpoints (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, description TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_workspace_updated ON sessions(workspace_path, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id, id)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate state database: %w", err)
		}
	}
	// Resume-fidelity: a session created before events.target existed keeps the
	// column-less table (CREATE IF NOT EXISTS above is a no-op for it), so add the
	// column in place. Idempotent: skipped when the column is already present.
	if err := ensureEventsTargetColumn(ctx, tx); err != nil {
		return fmt.Errorf("migrate events.target: %w", err)
	}
	return tx.Commit()
}

// ensureEventsTargetColumn adds the events.target column to a pre-existing DB
// that lacks it. It inspects the table schema first (rather than catching the
// "duplicate column name" error) so it never poisons the surrounding migration
// transaction on the common already-present path.
func ensureEventsTargetColumn(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(events)`)
	if err != nil {
		return err
	}
	present := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "target" {
			present = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if present {
		return nil
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE events ADD COLUMN target TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *DB) IsTrusted(ctx context.Context, path string) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM trusted_workspaces WHERE path = ?`, path).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *DB) Trust(ctx context.Context, path string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO trusted_workspaces(path, created_at) VALUES(?, ?) ON CONFLICT(path) DO UPDATE SET created_at=excluded.created_at`, path, nowText())
	return err
}

func (s *DB) CreateSession(ctx context.Context, workspace, title string) (contract.Session, error) {
	id, err := randomID(6)
	if err != nil {
		return contract.Session{}, err
	}
	now := time.Now().UTC()
	if len(title) > 80 {
		title = title[:80]
	}
	if title == "" {
		title = "MuhiyaCode session"
	}
	session := contract.Session{ID: id, WorkspacePath: workspace, Title: title, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(id, workspace_path, title, created_at, updated_at) VALUES(?,?,?,?,?)`, session.ID, session.WorkspacePath, session.Title, formatTime(now), formatTime(now))
	if err != nil {
		return contract.Session{}, err
	}
	return session, nil
}

func (s *DB) Session(ctx context.Context, id string) (contract.Session, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, workspace_path, title, created_at, updated_at FROM sessions WHERE id=?`, id)
	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return contract.Session{}, false, nil
	}
	return session, err == nil, err
}

func (s *DB) LatestSession(ctx context.Context, workspace string) (contract.Session, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, workspace_path, title, created_at, updated_at FROM sessions WHERE workspace_path=? ORDER BY updated_at DESC LIMIT 1`, workspace)
	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return contract.Session{}, false, nil
	}
	return session, err == nil, err
}

func (s *DB) ListSessions(ctx context.Context, workspace string, limit int) ([]contract.Session, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := `SELECT id, workspace_path, title, created_at, updated_at FROM sessions`
	args := []any{}
	if workspace != "" {
		query += ` WHERE workspace_path=?`
		args = append(args, workspace)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []contract.Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, rows.Err()
}

func (s *DB) UpdateSessionTitle(ctx context.Context, id, title string) error {
	if len(title) > 80 {
		title = title[:80]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET title=?, updated_at=? WHERE id=?`, title, nowText(), id)
	return err
}

func (s *DB) AddEvent(ctx context.Context, sessionID, role, kind, content, target string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(session_id, role, type, content, target, created_at) VALUES(?,?,?,?,?,?)`, sessionID, role, kind, content, target, nowText()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE id=?`, nowText(), sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DB) Events(ctx context.Context, sessionID string, limit int) ([]contract.Event, error) {
	query := `SELECT role, type, content, target, created_at FROM events WHERE session_id=? ORDER BY id ASC`
	args := []any{sessionID}
	if limit > 0 {
		query = `SELECT role, type, content, target, created_at FROM (SELECT id, role, type, content, target, created_at FROM events WHERE session_id=? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []contract.Event
	for rows.Next() {
		var event contract.Event
		var created string
		if err := rows.Scan(&event.Role, &event.Type, &event.Content, &event.Target, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = parseTime(created)
		result = append(result, event)
	}
	return result, rows.Err()
}

const defaultPageByteBudget = 8 << 20 // 8 MiB (terminal-performance contract §2)

// TranscriptPage returns a keyset-paginated slice of a session's durable events
// in ascending ID order (feature 005 US1, terminal-performance contract §2). It
// leaves the existing Events callers untouched. Offset pagination is never used;
// paging is by exclusive stable-ID cursor. One oversized entry may occupy a page
// by itself. hasOlder/hasNewer report whether another keyset page exists.
func (s *DB) TranscriptPage(ctx context.Context, req contract.TranscriptPageRequest) (contract.TranscriptPage, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 200
	}
	budget := req.ByteBudget
	if budget <= 0 {
		budget = defaultPageByteBudget
	}
	page := contract.TranscriptPage{SessionID: req.SessionID, Generation: req.Generation}
	var query string
	var args []any
	switch req.Direction {
	case contract.PageBefore:
		query = `SELECT id, session_id, role, type, content, target, created_at FROM (SELECT id, session_id, role, type, content, target, created_at FROM events WHERE session_id=? AND id<? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`
		args = []any{req.SessionID, req.Cursor, limit + 1}
	case contract.PageAfter:
		query = `SELECT id, session_id, role, type, content, target, created_at FROM events WHERE session_id=? AND id>? ORDER BY id ASC LIMIT ?`
		args = []any{req.SessionID, req.Cursor, limit + 1}
	default: // initial-tail
		query = `SELECT id, session_id, role, type, content, target, created_at FROM (SELECT id, session_id, role, type, content, target, created_at FROM events WHERE session_id=? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`
		args = []any{req.SessionID, limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	var fetched []contract.TranscriptEvent
	for rows.Next() {
		var event contract.TranscriptEvent
		var created string
		if err := rows.Scan(&event.ID, &event.SessionID, &event.Role, &event.Kind, &event.Content, &event.Target, &created); err != nil {
			return page, err
		}
		event.CreatedAt, _ = parseTime(created)
		fetched = append(fetched, event)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}

	// The extra (limit+1) row signals more entries exist beyond the window edge.
	more := len(fetched) > limit
	entries := fetched
	if more {
		if req.Direction == contract.PageAfter {
			entries = fetched[:limit] // drop the newest extra (last, ascending)
		} else {
			entries = fetched[len(fetched)-limit:] // drop the oldest extra (first)
		}
	}
	switch req.Direction {
	case contract.PageAfter:
		page.HasNewer, page.HasOlder = more, true
	case contract.PageBefore:
		page.HasOlder, page.HasNewer = more, true
	default:
		page.HasOlder, page.HasNewer = more, false
	}

	entries, trimmed := trimToByteBudget(entries, budget, req.Direction)
	if trimmed {
		if req.Direction == contract.PageAfter {
			page.HasNewer = true
		} else {
			page.HasOlder = true
		}
	}

	page.Entries = entries
	for _, event := range entries {
		page.RawBytes += len(event.Content)
	}
	if len(entries) > 0 {
		page.OldestID = entries[0].ID
		page.NewestID = entries[len(entries)-1].ID
	}
	return page, nil
}

// trimToByteBudget keeps entries within the raw-byte budget by dropping from the
// edge farthest from the viewport (oldest for tail/before, newest for after),
// always keeping at least one entry so a single oversized entry still loads.
func trimToByteBudget(entries []contract.TranscriptEvent, budget int, direction contract.PageDirection) ([]contract.TranscriptEvent, bool) {
	total := 0
	for _, event := range entries {
		total += len(event.Content)
	}
	if total <= budget || len(entries) <= 1 {
		return entries, false
	}
	trimmed := false
	if direction == contract.PageAfter {
		for total > budget && len(entries) > 1 {
			total -= len(entries[len(entries)-1].Content)
			entries = entries[:len(entries)-1]
			trimmed = true
		}
	} else {
		for total > budget && len(entries) > 1 {
			total -= len(entries[0].Content)
			entries = entries[1:]
			trimmed = true
		}
	}
	return entries, trimmed
}

func (s *DB) AddCheckpoint(ctx context.Context, id, sessionID, description string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO checkpoints(id, session_id, description, created_at) VALUES(?,?,?,?)`, id, sessionID, description, nowText())
	return err
}

func (s *DB) LatestCheckpoint(ctx context.Context, sessionID string) (id, description string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT id, description FROM checkpoints WHERE session_id=? ORDER BY created_at DESC LIMIT 1`, sessionID).Scan(&id, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return id, description, err == nil, err
}

type rowScanner interface{ Scan(...any) error }

func scanSession(row rowScanner) (contract.Session, error) {
	var session contract.Session
	var created, updated string
	if err := row.Scan(&session.ID, &session.WorkspacePath, &session.Title, &created, &updated); err != nil {
		return contract.Session{}, err
	}
	session.CreatedAt, _ = parseTime(created)
	session.UpdatedAt, _ = parseTime(updated)
	return session, nil
}

func randomID(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func formatTime(value time.Time) string         { return value.UTC().Format(time.RFC3339Nano) }
func nowText() string                           { return formatTime(time.Now()) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
