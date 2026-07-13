package state

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestGoalSidecarRoundTrip (G4) verifies the goal.json sidecar is written,
// read back, and removed by the state layer, mirroring the WritePlan pattern.
func TestGoalSidecarRoundTrip(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, "F:\\work", "goal-sidecar")
	if err != nil {
		t.Fatal(err)
	}
	// No sidecar yet → ReadGoal reports absent without error.
	if _, ok, err := sessions.ReadGoal(session.ID); err != nil || ok {
		t.Fatalf("expected absent goal sidecar, got ok=%v err=%v", ok, err)
	}
	// Write an active goal and read it back verbatim.
	snapshot := contract.GoalSnapshot{Text: "ship the release notes", Status: "active", AutoTurns: 3}
	if err := sessions.WriteGoal(session.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := sessions.ReadGoal(session.ID)
	if err != nil || !ok || loaded != snapshot {
		t.Fatalf("sidecar round-trip mismatch: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	// ClearGoal removes it; a second clear is a no-op (not an error).
	if err := sessions.ClearGoal(session.ID); err != nil {
		t.Fatalf("ClearGoal failed: %v", err)
	}
	if err := sessions.ClearGoal(session.ID); err != nil {
		t.Fatalf("second ClearGoal should be a no-op, got: %v", err)
	}
	if _, ok, err := sessions.ReadGoal(session.ID); err != nil || ok {
		t.Fatalf("sidecar survived ClearGoal: ok=%v err=%v", ok, err)
	}
}

// TestGoalSidecarMalformedIsAbsent (G4) ensures a corrupt goal.json is treated
// as absent so a bad sidecar never blocks session resume.
func TestGoalSidecarMalformedIsAbsent(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, "F:\\work", "goal-sidecar-corrupt")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := SessionDir(paths, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "goal.json"), []byte("{not json"), false); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sessions.ReadGoal(session.ID); err != nil || ok {
		t.Fatalf("malformed sidecar should read as absent: ok=%v err=%v", ok, err)
	}
}
