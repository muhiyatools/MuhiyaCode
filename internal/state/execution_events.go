package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const maxExecutionEventBytes = 32 << 20

func (s *DB) AppendExecutionEvent(ctx context.Context, event contract.ExecutionEvent) (contract.ExecutionEvent, error) {
	saved, err := s.AppendExecutionEvents(ctx, []contract.ExecutionEvent{event})
	if err != nil {
		return contract.ExecutionEvent{}, err
	}
	return saved[0], nil
}

type preparedExecutionEvent struct {
	event   contract.ExecutionEvent
	payload []byte
}

func (s *DB) AppendExecutionEvents(ctx context.Context, events []contract.ExecutionEvent) ([]contract.ExecutionEvent, error) {
	prepared, err := prepareExecutionEvents(events)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for index := range prepared {
		sequence, err := insertExecutionEvent(ctx, tx, prepared[index])
		if err != nil {
			return nil, err
		}
		prepared[index].event.Sequence = sequence
	}
	sessionID := prepared[0].event.SessionID
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE id=?`, nowText(), sessionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	saved := make([]contract.ExecutionEvent, len(prepared))
	for index := range prepared {
		saved[index] = prepared[index].event
	}
	return saved, nil
}

func prepareExecutionEvents(events []contract.ExecutionEvent) ([]preparedExecutionEvent, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("execution event batch is empty")
	}
	prepared := make([]preparedExecutionEvent, 0, len(events))
	sessionID := events[0].SessionID
	for _, event := range events {
		if event.SessionID != sessionID {
			return nil, fmt.Errorf("execution event batch spans multiple sessions")
		}
		preparedEvent, err := prepareExecutionEvent(event)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, preparedEvent)
	}
	return prepared, nil
}

func prepareExecutionEvent(event contract.ExecutionEvent) (preparedExecutionEvent, error) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := event.Validate(); err != nil {
		return preparedExecutionEvent{}, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return preparedExecutionEvent{}, fmt.Errorf("encode execution event: %w", err)
	}
	if len(payload) > maxExecutionEventBytes {
		return preparedExecutionEvent{}, fmt.Errorf("execution event exceeds %d-byte limit", maxExecutionEventBytes)
	}
	return preparedExecutionEvent{event: event, payload: payload}, nil
}

func insertExecutionEvent(ctx context.Context, tx *sql.Tx, prepared preparedExecutionEvent) (int64, error) {
	event := prepared.event
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO execution_events(session_id, task_id, version, kind, payload, created_at) VALUES(?,?,?,?,?,?)`,
		event.SessionID, event.TaskID, event.Version, event.Kind, prepared.payload, formatTime(event.CreatedAt),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *DB) ExecutionEventsAfter(ctx context.Context, sessionID string, cursor int64, limit int) ([]contract.ExecutionEvent, error) {
	if limit <= 0 || limit > 1_000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT sequence, payload, created_at FROM execution_events WHERE session_id=? AND sequence>? ORDER BY sequence ASC LIMIT ?`,
		sessionID,
		cursor,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]contract.ExecutionEvent, 0, limit)
	for rows.Next() {
		event, err := scanExecutionEvent(rows, sessionID)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *DB) LatestExecutionCursor(ctx context.Context, sessionID string) (int64, error) {
	var cursor int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) FROM execution_events WHERE session_id=?`,
		sessionID,
	).Scan(&cursor)
	return cursor, err
}

func (s *DB) LatestProjectionCheckpoint(ctx context.Context, sessionID, name string) (contract.ExecutionEvent, bool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT sequence, payload, created_at FROM execution_events WHERE session_id=? AND kind=? ORDER BY sequence DESC`,
		sessionID,
		contract.ExecutionProjectionSaved,
	)
	if err != nil {
		return contract.ExecutionEvent{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		event, decodeErr := scanExecutionEvent(rows, sessionID)
		if decodeErr != nil {
			return contract.ExecutionEvent{}, false, decodeErr
		}
		if event.Checkpoint.Name == name {
			return event, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return contract.ExecutionEvent{}, false, err
	}
	return contract.ExecutionEvent{}, false, nil
}

type executionEventScanner interface {
	Scan(...any) error
}

func scanExecutionEvent(scanner executionEventScanner, sessionID string) (contract.ExecutionEvent, error) {
	var sequence int64
	var payload []byte
	var created string
	if err := scanner.Scan(&sequence, &payload, &created); err != nil {
		return contract.ExecutionEvent{}, err
	}
	if len(payload) > maxExecutionEventBytes {
		return contract.ExecutionEvent{}, fmt.Errorf("execution event %d exceeds %d-byte limit", sequence, maxExecutionEventBytes)
	}
	return decodeExecutionEvent(payload, sessionID, sequence, created)
}

func decodeExecutionEvent(payload []byte, sessionID string, sequence int64, created string) (contract.ExecutionEvent, error) {
	var event contract.ExecutionEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return contract.ExecutionEvent{}, fmt.Errorf("decode execution event %d: %w", sequence, err)
	}
	event.Sequence = sequence
	event.SessionID = sessionID
	event.CreatedAt, _ = parseTime(created)
	if err := event.Validate(); err != nil {
		return contract.ExecutionEvent{}, fmt.Errorf("validate execution event %d: %w", sequence, err)
	}
	return event, nil
}

func (s *DB) PendingMutationIntents(ctx context.Context, sessionID string) ([]contract.MutationIntent, error) {
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return contract.PendingMutationIntents(events), nil
}

func (s *DB) IncompleteTaskIDs(ctx context.Context, sessionID string) ([]string, error) {
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return contract.IncompleteTaskIDs(events), nil
}

func (s *DB) PendingApprovalRequests(ctx context.Context, sessionID string) ([]contract.ApprovalRequestRecord, error) {
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return contract.PendingApprovalRequests(events), nil
}

type ExecutionJournalHealth struct {
	PendingMutations []contract.MutationIntent
	IncompleteTasks  []string
	PendingApprovals []contract.ApprovalRequestRecord
	IncompleteTools  []contract.ToolStartedRecord
}

func (s *DB) ExecutionJournalHealth(ctx context.Context, sessionID string) (ExecutionJournalHealth, error) {
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return ExecutionJournalHealth{}, err
	}
	return ExecutionJournalHealth{
		PendingMutations: contract.PendingMutationIntents(events),
		IncompleteTasks:  contract.IncompleteTaskIDs(events),
		PendingApprovals: contract.PendingApprovalRequests(events),
		IncompleteTools:  contract.IncompleteToolExecutions(events),
	}, nil
}

func (s *DB) ReconcileMutation(
	ctx context.Context,
	sessionID string,
	intentID string,
	certainty contract.MutationCertainty,
) error {
	status, err := reconciliationStatus(certainty)
	if err != nil {
		return err
	}
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	source, pending := pendingMutationEvent(events, intentID)
	if !pending {
		return fmt.Errorf("mutation intent %q is missing or already resolved", intentID)
	}
	outcome := contract.MutationOutcome{
		IntentID: intentID, Status: status, Certainty: certainty,
		Reconciled: true, CreatedAt: time.Now().UTC(),
	}
	_, err = s.AppendExecutionEvent(ctx, contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, SessionID: sessionID, TaskID: source.TaskID,
		Kind: contract.ExecutionMutationOutcome, Outcome: &outcome,
	})
	return err
}

func reconciliationStatus(certainty contract.MutationCertainty) (contract.ToolOutcomeStatus, error) {
	switch certainty {
	case contract.MutationCommitted:
		return contract.ToolOutcomeSucceeded, nil
	case contract.MutationNotStarted:
		return contract.ToolOutcomeFailed, nil
	case contract.MutationIndeterminate:
		return contract.ToolOutcomeIndeterminate, nil
	default:
		return "", fmt.Errorf("reconciliation certainty must be committed, not_started, or indeterminate")
	}
}

func pendingMutationEvent(events []contract.ExecutionEvent, intentID string) (contract.ExecutionEvent, bool) {
	var source contract.ExecutionEvent
	resolved := false
	for _, event := range events {
		if event.Kind == contract.ExecutionMutationIntent && event.Intent.IntentID == intentID {
			source = event
		}
		if event.Kind == contract.ExecutionMutationOutcome && event.Outcome.IntentID == intentID {
			resolved = true
		}
	}
	return source, source.Intent != nil && !resolved
}

func (s *DB) ReconcileApproval(ctx context.Context, sessionID, requestID string, approved bool) error {
	events, err := s.allExecutionEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	source, pending := pendingApprovalEvent(events, requestID)
	if !pending {
		return fmt.Errorf("approval request %q is missing or already resolved", requestID)
	}
	resolution := contract.ApprovalResolutionRecord{
		RequestID: requestID, Approved: approved, Reconciled: true,
		CreatedAt: time.Now().UTC(),
	}
	_, err = s.AppendExecutionEvent(ctx, contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, SessionID: sessionID, TaskID: source.TaskID,
		Kind: contract.ExecutionApprovalDecided, ApprovalResolution: &resolution,
	})
	return err
}

func pendingApprovalEvent(events []contract.ExecutionEvent, requestID string) (contract.ExecutionEvent, bool) {
	var source contract.ExecutionEvent
	resolved := false
	for _, event := range events {
		if event.Kind == contract.ExecutionApprovalAsked && event.ApprovalRequest.RequestID == requestID {
			source = event
		}
		if event.Kind == contract.ExecutionApprovalDecided && event.ApprovalResolution.RequestID == requestID {
			resolved = true
		}
	}
	return source, source.ApprovalRequest != nil && !resolved
}

func (s *DB) allExecutionEvents(ctx context.Context, sessionID string) ([]contract.ExecutionEvent, error) {
	var all []contract.ExecutionEvent
	var cursor int64
	for {
		page, err := s.ExecutionEventsAfter(ctx, sessionID, cursor, 1_000)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		cursor = page[len(page)-1].Sequence
		if len(page) < 1_000 {
			break
		}
	}
	return all, nil
}
