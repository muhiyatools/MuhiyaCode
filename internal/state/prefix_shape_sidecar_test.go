package state

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestPrefixShapeSidecarRoundTrip (Ultimate Polish C3) verifies the prefix_shape.json
// sidecar is written and read back verbatim, and absent on a fresh session.
func TestPrefixShapeSidecarRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, "F:\\work", "prefix-shape-sidecar")
	if err != nil {
		t.Fatal(err)
	}
	// Fresh session → absent, no error.
	if _, ok, err := sessions.ReadPrefixShape(session.ID); err != nil || ok {
		t.Fatalf("expected absent prefix-shape sidecar, got ok=%v err=%v", ok, err)
	}
	snapshot := contract.PrefixShapeSnapshot{Version: contract.PrefixShapeSnapshotVersion, SystemHash: "sys123", ToolsHash: "tools456", ModelID: "deepseek-v4-pro"}
	if err := sessions.WritePrefixShape(session.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := sessions.ReadPrefixShape(session.ID)
	if err != nil || !ok || loaded != snapshot {
		t.Fatalf("prefix-shape round-trip mismatch: loaded=%+v ok=%v err=%v", loaded, ok, err)
	}
}
