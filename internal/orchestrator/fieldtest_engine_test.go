package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// fieldTestEngine builds a scripted engine over a REAL workspace: the file
// tools actually read and write, so a scenario proves work landed on disk
// rather than that a stub was called. It is the shared fixture for the
// end-to-end scenarios (snake build, truncation recovery, checklist paths).
//
// It outlived the delegation-shaped fieldtests it was written for — those
// scripted a plan/execute handoff that no longer exists — because the fixture
// itself is about a real workspace, not about who does the work.
func fieldTestEngine(t *testing.T, responses ...contract.ChatResponse) (*Engine, *scriptedProvider, string) {
	t.Helper()
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{responses: responses}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "fieldtest", WorkspacePath: dir},
		Provider: provider, Registry: NewRegistry(service.Tools()...),
		Knowledge: NewKnowledge(KnowledgeSnapshot{Version: 1}, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, provider, dir
}
