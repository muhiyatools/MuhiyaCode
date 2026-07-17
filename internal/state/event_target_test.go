package state

import (
	"context"
	"database/sql"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestEventTargetRoundTrip pins that a tool event's display target survives a
// write→read cycle through both readers (resume fidelity, Fix R1): the target the
// live row showed is the target a reopened row shows.
func TestEventTargetRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, `F:\w`, "target")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(ctx, session.ID, "tool", "run_shell", "exit code: 0\nok", "go build ./..."); err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(ctx, session.ID, "assistant", "message", "done", ""); err != nil {
		t.Fatal(err)
	}

	events, err := db.Events(ctx, session.ID, 0)
	if err != nil || len(events) != 2 {
		t.Fatalf("events: n=%d err=%v", len(events), err)
	}
	if events[0].Target != "go build ./..." {
		t.Fatalf("tool event target = %q, want the shell command", events[0].Target)
	}
	if events[1].Target != "" {
		t.Fatalf("assistant event target = %q, want empty", events[1].Target)
	}

	page, err := db.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: session.ID, Direction: contract.PageInitialTail})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Target != "go build ./..." {
		t.Fatalf("paged tool target lost: %+v", page.Entries)
	}
}

// TestEventsTargetColumnMigration pins the in-place migration for a session
// created before the events.target column existed: Open adds the column, old
// rows read back with an empty target (no crash), new writes carry it, and a
// second Open is idempotent.
func TestEventsTargetColumnMigration(t *testing.T) {
	ctx := context.Background()
	p, err := EnsurePaths(testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	// Seed a legacy DB: the pre-target events schema plus one tool row.
	raw, err := sql.Open("sqlite", p.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, workspace_path TEXT NOT NULL, title TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, role TEXT NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`INSERT INTO sessions(id, workspace_path, title, created_at, updated_at) VALUES('s1','F:\w','t','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO events(session_id, role, type, content, created_at) VALUES('s1','tool','read_file','old output','2026-01-01T00:00:00Z')`,
	} {
		if _, err := raw.ExecContext(ctx, stmt); err != nil {
			raw.Close()
			t.Fatalf("legacy seed: %v", err)
		}
	}
	raw.Close()

	db, err := Open(ctx, p) // runs migrate → ALTER TABLE events ADD COLUMN target
	if err != nil {
		t.Fatalf("migrating open failed: %v", err)
	}
	events, err := db.Events(ctx, "s1", 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("legacy events after migration: n=%d err=%v", len(events), err)
	}
	if events[0].Target != "" {
		t.Fatalf("legacy row target = %q, want empty", events[0].Target)
	}
	if err := db.AddEvent(ctx, "s1", "tool", "run_shell", "exit code: 0\nok", "make test"); err != nil {
		t.Fatal(err)
	}
	events, _ = db.Events(ctx, "s1", 0)
	if events[len(events)-1].Target != "make test" {
		t.Fatalf("post-migration write lost its target: %+v", events[len(events)-1])
	}
	db.Close()

	// Idempotent: opening again re-runs migrate; the column is already present.
	db2, err := Open(ctx, p)
	if err != nil {
		t.Fatalf("second open must be idempotent, got %v", err)
	}
	db2.Close()
}
