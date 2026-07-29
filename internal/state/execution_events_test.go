package state

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestExecutionEventsCursorAndPendingIntentProjection(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "execution events")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	intent1 := contract.MutationIntent{IntentID: "task:c1", IdempotencyKey: "c1", ToolName: "write_file", ArgumentsHash: "a", CreatedAt: at}
	intent2 := contract.MutationIntent{IntentID: "task:c2", IdempotencyKey: "c2", ToolName: "run_shell", ArgumentsHash: "b", CreatedAt: at.Add(time.Millisecond)}
	events := []contract.ExecutionEvent{
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionMutationIntent, Intent: &intent1, CreatedAt: at},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionMutationOutcome, Outcome: &contract.MutationOutcome{IntentID: intent1.IntentID, Status: contract.ToolOutcomeSucceeded, Certainty: contract.MutationCommitted, CreatedAt: at.Add(time.Millisecond)}, CreatedAt: at.Add(time.Millisecond)},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionMutationIntent, Intent: &intent2, CreatedAt: at.Add(2 * time.Millisecond)},
	}
	var written []contract.ExecutionEvent
	for _, event := range events {
		saved, appendErr := db.AppendExecutionEvent(ctx, event)
		if appendErr != nil {
			t.Fatal(appendErr)
		}
		written = append(written, saved)
	}
	if written[0].Sequence >= written[1].Sequence || written[1].Sequence >= written[2].Sequence {
		t.Fatalf("execution sequence is not monotonic: %+v", written)
	}

	replayed, err := db.ExecutionEventsAfter(ctx, session.ID, written[0].Sequence, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 2 || replayed[0].Sequence != written[1].Sequence {
		t.Fatalf("cursor replay mismatch: %+v", replayed)
	}
	all, err := db.ExecutionEventsAfter(ctx, session.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	pending := contract.PendingMutationIntents(all)
	if len(pending) != 1 || pending[0].IntentID != intent2.IntentID {
		t.Fatalf("crash-prefix projection lost the incomplete intent: %+v", pending)
	}
}

func TestForkSessionRewritesExecutionPayloadIdentity(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := db.CreateSession(ctx, t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := db.AppendExecutionEvent(ctx, contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, SessionID: source.ID,
		TaskID: "session:" + source.ID + ":task-graph", Kind: contract.ExecutionTaskStarted,
		Started: &contract.TaskStarted{PromptHash: "prompt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	forked, err := db.ForkSessionAt(ctx, source.ID, "fork", saved.Sequence, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	events, err := db.ExecutionEventsAfter(ctx, forked.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one forked execution event, got %d", len(events))
	}
	if events[0].SessionID != forked.ID {
		t.Fatalf("payload retained source session identity: %+v", events[0])
	}
	wantTaskID := "session:" + forked.ID + ":task-graph"
	if events[0].TaskID != wantTaskID {
		t.Fatalf("projection task ID = %q, want %q", events[0].TaskID, wantTaskID)
	}
}

func TestLatestProjectionCheckpointValidatesChecksumAndName(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "projection")
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{"usage", "history", "history"} {
		payload, marshalErr := json.Marshal(map[string]int{"revision": index})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		checkpoint := contract.ProjectionCheckpoint{
			Name: name, Version: 1, Payload: payload,
			Checksum: contract.ProjectionChecksum(payload), CreatedAt: time.Now().UTC(),
		}
		event := contract.ExecutionEvent{
			Version: 1, SessionID: session.ID, TaskID: "task",
			Kind: contract.ExecutionProjectionSaved, Checkpoint: &checkpoint,
		}
		if _, err := db.AppendExecutionEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	latest, exists, err := db.LatestProjectionCheckpoint(ctx, session.ID, "history")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || string(latest.Checkpoint.Payload) != `{"revision":2}` {
		t.Fatalf("latest named checkpoint mismatch: %+v", latest)
	}

	badPayload := json.RawMessage(`{"revision":3}`)
	bad := contract.ExecutionEvent{
		Version: 1, SessionID: session.ID, TaskID: "task",
		Kind: contract.ExecutionProjectionSaved,
		Checkpoint: &contract.ProjectionCheckpoint{
			Name: "history", Version: 1, Payload: badPayload, Checksum: "wrong",
		},
	}
	if _, err := db.AppendExecutionEvent(ctx, bad); err == nil {
		t.Fatal("corrupt projection checkpoint was accepted")
	}
}

func TestAppendExecutionEventsIsAtomic(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "atomic batch")
	if err != nil {
		t.Fatal(err)
	}
	started := contract.ExecutionEvent{
		Version: 1, SessionID: session.ID, TaskID: "task",
		Kind:    contract.ExecutionTaskStarted,
		Started: &contract.TaskStarted{PromptHash: "hash"},
	}
	invalid := contract.ExecutionEvent{
		Version: 1, SessionID: session.ID, TaskID: "task",
		Kind: contract.ExecutionTaskTerminal,
	}
	if _, err := db.AppendExecutionEvents(ctx, []contract.ExecutionEvent{started, invalid}); err == nil {
		t.Fatal("invalid terminal batch was accepted")
	}
	events, err := db.ExecutionEventsAfter(ctx, session.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("partial terminal batch became visible: %+v", events)
	}
}

func TestAppendExecutionEventRejectsUnknownVersion(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "bad event")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AppendExecutionEvent(ctx, contract.ExecutionEvent{Version: 99, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionMutationIntent})
	if err == nil {
		t.Fatal("unknown execution event version was accepted")
	}
}

func TestExecutionTaskLifecycleProjectionSurvivesCrashPrefix(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "task lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	events := []contract.ExecutionEvent{
		{Version: 1, SessionID: session.ID, TaskID: "complete", Kind: contract.ExecutionTaskStarted, Started: &contract.TaskStarted{PromptHash: "a", CreatedAt: at}},
		{Version: 1, SessionID: session.ID, TaskID: "complete", Kind: contract.ExecutionTaskTerminal, Terminal: &contract.TaskTerminal{Status: contract.TaskStatusSucceeded, AnswerHash: "b", CreatedAt: at.Add(time.Millisecond)}},
		{Version: 1, SessionID: session.ID, TaskID: "crashed", Kind: contract.ExecutionTaskStarted, Started: &contract.TaskStarted{PromptHash: "c", CreatedAt: at.Add(2 * time.Millisecond)}},
	}
	for _, event := range events {
		if _, err := db.AppendExecutionEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	incomplete, err := db.IncompleteTaskIDs(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(incomplete) != 1 || incomplete[0] != "crashed" {
		t.Fatalf("crash-prefix task projection mismatch: %v", incomplete)
	}
}

func TestPendingApprovalProjectionKeepsOnlyUnresolvedRequests(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "approvals")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	first := contract.ApprovalRequestRecord{RequestID: "first", Action: "write", Target: "a.go", CreatedAt: at}
	second := contract.ApprovalRequestRecord{RequestID: "second", Action: "shell", Target: "shell:hash", CreatedAt: at.Add(time.Millisecond)}
	events := []contract.ExecutionEvent{
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionApprovalAsked, ApprovalRequest: &first},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionApprovalDecided, ApprovalResolution: &contract.ApprovalResolutionRecord{RequestID: first.RequestID, Approved: false}},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionApprovalAsked, ApprovalRequest: &second},
	}
	for _, event := range events {
		if _, err := db.AppendExecutionEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := db.PendingApprovalRequests(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RequestID != second.RequestID {
		t.Fatalf("pending approval projection mismatch: %+v", pending)
	}
}

func TestExecutionJournalHealthReadsToolLifecycleWithOtherProjections(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "journal health")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	complete := contract.ToolStartedRecord{
		ExecutionID: "complete", CallID: "c1", ToolName: "read_file",
		ArgumentsHash: "hash1", CreatedAt: at,
	}
	incomplete := contract.ToolStartedRecord{
		ExecutionID: "incomplete", CallID: "c2", ToolName: "write_file",
		ArgumentsHash: "hash2", Mutating: true, CreatedAt: at.Add(time.Millisecond),
	}
	events := []contract.ExecutionEvent{
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionToolStarted, ToolStarted: &complete},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionToolTerminal, ToolTerminal: &contract.ToolTerminalRecord{
			ExecutionID: complete.ExecutionID, Status: contract.ToolOutcomeSucceeded,
			MutationCertainty: contract.MutationNotApplicable, OutputHash: "output",
		}},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionToolStarted, ToolStarted: &incomplete},
	}
	if _, err := db.AppendExecutionEvents(ctx, events); err != nil {
		t.Fatal(err)
	}
	health, err := db.ExecutionJournalHealth(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(health.IncompleteTools) != 1 || health.IncompleteTools[0].ExecutionID != incomplete.ExecutionID {
		t.Fatalf("incomplete tool projection mismatch: %+v", health)
	}
}

func TestManualReconciliationResolvesPendingMutationAndApproval(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	session, err := db.CreateSession(ctx, t.TempDir(), "reconcile")
	if err != nil {
		t.Fatal(err)
	}
	intent := contract.MutationIntent{
		IntentID: "intent", IdempotencyKey: "key", ToolName: "write_file",
		ArgumentsHash: "hash", CreatedAt: time.Now().UTC(),
	}
	approval := contract.ApprovalRequestRecord{RequestID: "approval", Action: "write"}
	if _, err := db.AppendExecutionEvents(ctx, []contract.ExecutionEvent{
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionMutationIntent, Intent: &intent},
		{Version: 1, SessionID: session.ID, TaskID: "task", Kind: contract.ExecutionApprovalAsked, ApprovalRequest: &approval},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileMutation(ctx, session.ID, intent.IntentID, contract.MutationCommitted); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileApproval(ctx, session.ID, approval.RequestID, false); err != nil {
		t.Fatal(err)
	}
	health, err := db.ExecutionJournalHealth(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(health.PendingMutations) != 0 || len(health.PendingApprovals) != 0 {
		t.Fatalf("manual reconciliation did not resolve journal state: %+v", health)
	}
	events, err := db.ExecutionEventsAfter(ctx, session.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !events[2].Outcome.Reconciled || !events[3].ApprovalResolution.Reconciled {
		t.Fatalf("manual reconciliation was not explicit in durable records: %+v", events)
	}
	if err := db.ReconcileMutation(ctx, session.ID, intent.IntentID, contract.MutationCommitted); err == nil {
		t.Fatal("already resolved mutation was reconciled twice")
	}
}
