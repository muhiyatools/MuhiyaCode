package state

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestPlanStateSidecarRoundTrip (P2) verifies the plan_state.json sidecar is
// written, read back, and removed by the state layer, mirroring the goal
// sidecar. The sidecar carries only the plan-mode and pending-plan flags.
func TestPlanStateSidecarRoundTrip(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, "F:\\work", "plan-state-sidecar")
	if err != nil {
		t.Fatal(err)
	}
	// No sidecar yet → ReadPlanState reports absent without error.
	if _, ok, err := sessions.ReadPlanState(session.ID); err != nil || ok {
		t.Fatalf("expected absent plan-state sidecar, got ok=%v err=%v", ok, err)
	}
	// Write plan-mode on and read it back verbatim.
	snapshot := contract.PlanStateSnapshot{PlanMode: true, PendingPlan: false}
	if err := sessions.WritePlanState(session.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := sessions.ReadPlanState(session.ID)
	if err != nil || !ok || loaded != snapshot {
		t.Fatalf("sidecar round-trip mismatch: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	// Flip to pending-plan on; plan-mode off.
	pending := contract.PlanStateSnapshot{PlanMode: false, PendingPlan: true}
	if err := sessions.WritePlanState(session.ID, pending); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err = sessions.ReadPlanState(session.ID)
	if err != nil || !ok || loaded != pending {
		t.Fatalf("pending sidecar mismatch: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
	// ClearPlanState removes it; a second clear is a no-op (not an error).
	if err := sessions.ClearPlanState(session.ID); err != nil {
		t.Fatalf("ClearPlanState failed: %v", err)
	}
	if err := sessions.ClearPlanState(session.ID); err != nil {
		t.Fatalf("second ClearPlanState should be a no-op, got: %v", err)
	}
	if _, ok, err := sessions.ReadPlanState(session.ID); err != nil || ok {
		t.Fatalf("sidecar survived ClearPlanState: ok=%v err=%v", ok, err)
	}
}

// TestPlanStateSidecarMalformedIsAbsent (P2) ensures a corrupt plan_state.json
// is treated as absent so a bad sidecar never blocks session resume.
func TestPlanStateSidecarMalformedIsAbsent(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, "F:\\work", "plan-state-sidecar-corrupt")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := SessionDir(paths, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "plan_state.json"), []byte("{not json"), false); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := sessions.ReadPlanState(session.ID); err != nil || ok {
		t.Fatalf("malformed sidecar should read as absent: ok=%v err=%v", ok, err)
	}
}
