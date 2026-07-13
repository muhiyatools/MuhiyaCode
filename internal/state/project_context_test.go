package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func newSessionForContext(t *testing.T) (*Sessions, contract.Session, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	sessions := &Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	if err := db.Trust(ctx, `F:\work`); err != nil {
		t.Fatal(err)
	}
	session, err := sessions.New(ctx, `F:\work`, "test")
	if err != nil {
		t.Fatal(err)
	}
	return sessions, session, ctx
}

func TestProjectContextRoundTrip(t *testing.T) {
	sessions, session, _ := newSessionForContext(t)
	snap := contract.ProjectContextSnapshot{
		WorkspaceKey:        "ws-a",
		RenderedBootContext: "## PROJECT CONTEXT\n...",
		InstructionsHash:    "abc",
		InstructionsState:   "loaded",
		MemoryHash:          "mem7",
		MemoryState:         "loaded",
		SkillsSnapshot:      []string{"skill-a", "skill-b"},
	}
	if err := sessions.WriteProjectContext(session.ID, snap); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok, err := sessions.ReadProjectContext(session.ID, "ws-a")
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got.Version != contract.ProjectContextVersion {
		t.Fatalf("version = %d, want %d", got.Version, contract.ProjectContextVersion)
	}
	if got.RenderedBootContext != snap.RenderedBootContext || got.MemoryHash != "mem7" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestProjectContextMissing(t *testing.T) {
	sessions, session, _ := newSessionForContext(t)
	_, ok, err := sessions.ReadProjectContext(session.ID, "ws-a")
	if err != nil || ok {
		t.Fatalf("missing sidecar should be ok=false err=nil, got ok=%v err=%v", ok, err)
	}
}

func TestProjectContextWorkspaceMismatchRejected(t *testing.T) {
	sessions, session, _ := newSessionForContext(t)
	if err := sessions.WriteProjectContext(session.ID, contract.ProjectContextSnapshot{WorkspaceKey: "ws-a", RenderedBootContext: "x"}); err != nil {
		t.Fatal(err)
	}
	_, ok, err := sessions.ReadProjectContext(session.ID, "ws-b")
	if err != nil || ok {
		t.Fatalf("foreign workspace must not be borrowed: ok=%v err=%v", ok, err)
	}
	dir, _ := SessionDir(sessions.DB.paths, session.ID)
	if _, statErr := os.Stat(filepath.Join(dir, projectContextSidecar)); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched sidecar should be moved aside")
	}
}

func TestProjectContextCorruptRecovery(t *testing.T) {
	sessions, session, _ := newSessionForContext(t)
	dir, _ := SessionDir(sessions.DB.paths, session.ID)
	if err := os.WriteFile(filepath.Join(dir, projectContextSidecar), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, err := sessions.ReadProjectContext(session.ID, "ws-a")
	if err != nil || ok {
		t.Fatalf("corrupt sidecar should bootstrap: ok=%v err=%v", ok, err)
	}
}
