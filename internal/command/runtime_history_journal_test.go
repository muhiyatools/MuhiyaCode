package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

func TestJournalRecentReplaysWhenLegacyProjectionIsUnreadable(t *testing.T) {
	ctx := context.Background()
	t.Setenv(state.HomeEnvironment, t.TempDir())
	paths, err := state.EnsurePaths()
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "recent journal")
	if err != nil {
		t.Fatal(err)
	}
	app := &Application{db: db}
	baseline := []contract.Event{{Role: "user", Type: "message", Content: "before"}}
	if _, err := app.loadJournalRecent(ctx, session, baseline, nil); err != nil {
		t.Fatal(err)
	}
	message := contract.MessageRecord{
		Message:   contract.Message{Role: contract.RoleAssistant, Content: "after"},
		Kind:      "message",
		CreatedAt: time.Now().UTC(),
	}
	if _, err := db.AppendExecutionEvent(ctx, contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, SessionID: session.ID, TaskID: "task",
		Kind: contract.ExecutionMessageRecorded, Message: &message,
	}); err != nil {
		t.Fatal(err)
	}
	recent, err := app.loadJournalRecent(ctx, session, nil, errors.New("legacy events corrupt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].Content != "before" || recent[1].Content != "after" {
		t.Fatalf("journal recent projection mismatch: %+v", recent)
	}
	replayed, err := app.loadJournalRecent(ctx, session, nil, errors.New("legacy events corrupt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 2 {
		t.Fatalf("refreshed checkpoint duplicated events: %+v", replayed)
	}
}
