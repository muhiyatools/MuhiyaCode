package state

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/secrecy"
)

const SessionBundleVersion = 1

type SessionBundle struct {
	Version         int                       `json:"version"`
	ExportedAt      time.Time                 `json:"exportedAt"`
	Session         contract.Session          `json:"session"`
	Events          []contract.Event          `json:"events"`
	ExecutionEvents []contract.ExecutionEvent `json:"executionEvents"`
}

func (s *DB) ExportSession(ctx context.Context, id string) (SessionBundle, error) {
	session, ok, err := s.Session(ctx, id)
	if err != nil {
		return SessionBundle{}, err
	}
	if !ok {
		return SessionBundle{}, fmt.Errorf("session not found: %s", id)
	}
	events, err := s.Events(ctx, id, 0)
	if err != nil {
		return SessionBundle{}, err
	}
	executionEvents, err := s.allExecutionEvents(ctx, id)
	if err != nil {
		return SessionBundle{}, err
	}
	return SessionBundle{
		Version: SessionBundleVersion, ExportedAt: time.Now().UTC(),
		Session: session, Events: events, ExecutionEvents: executionEvents,
	}, nil
}

func SanitizeSessionBundle(bundle SessionBundle, values ...string) SessionBundle {
	redact := func(value string) string { return secrecy.Redact(value, values...) }
	bundle.Session.WorkspacePath = redact(bundle.Session.WorkspacePath)
	for index := range bundle.Events {
		bundle.Events[index].Content = redact(bundle.Events[index].Content)
		bundle.Events[index].Target = redact(bundle.Events[index].Target)
	}
	for index := range bundle.ExecutionEvents {
		sanitizeExecutionEvent(&bundle.ExecutionEvents[index], redact)
	}
	return bundle
}

func sanitizeExecutionEvent(event *contract.ExecutionEvent, redact func(string) string) {
	if event.Terminal != nil {
		event.Terminal.Error = redact(event.Terminal.Error)
	}
	if event.Message != nil {
		event.Message.Message.Content = redact(event.Message.Message.Content)
		event.Message.Target = redact(event.Message.Target)
	}
	if event.ApprovalRequest != nil {
		event.ApprovalRequest.Target = redact(event.ApprovalRequest.Target)
		event.ApprovalRequest.Reason = redact(event.ApprovalRequest.Reason)
	}
	if event.ApprovalResolution != nil {
		event.ApprovalResolution.Error = redact(event.ApprovalResolution.Error)
	}
	if event.Verification != nil {
		event.Verification.Result.OutputTruncated = redact(event.Verification.Result.OutputTruncated)
	}
	if event.ToolTerminal != nil {
		event.ToolTerminal.Error = redact(event.ToolTerminal.Error)
	}
	if event.Outcome != nil {
		event.Outcome.Error = redact(event.Outcome.Error)
	}
}

func (s *DB) ImportSession(ctx context.Context, bundle SessionBundle, workspace string) (contract.Session, error) {
	if bundle.Version != SessionBundleVersion {
		return contract.Session{}, fmt.Errorf("unsupported session bundle version %d", bundle.Version)
	}
	if len(bundle.Events) > 1_000_000 || len(bundle.ExecutionEvents) > 1_000_000 {
		return contract.Session{}, fmt.Errorf("session bundle exceeds event limits")
	}
	id, err := randomID(6)
	if err != nil {
		return contract.Session{}, err
	}
	if strings.TrimSpace(workspace) == "" {
		workspace = bundle.Session.WorkspacePath
	}
	now := time.Now().UTC()
	session := contract.Session{
		ID: id, WorkspacePath: workspace, Title: bundle.Session.Title + " (imported)",
		CreatedAt: now, UpdatedAt: now, ParentID: bundle.Session.ID,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contract.Session{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions(id, workspace_path, title, created_at, updated_at, parent_session_id) VALUES(?,?,?,?,?,?)`,
		session.ID, session.WorkspacePath, session.Title, formatTime(now), formatTime(now), session.ParentID,
	); err != nil {
		return contract.Session{}, err
	}
	for _, event := range bundle.Events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(session_id, role, type, content, target, created_at) VALUES(?,?,?,?,?,?)`,
			session.ID, event.Role, event.Type, event.Content, event.Target, formatTime(event.CreatedAt),
		); err != nil {
			return contract.Session{}, err
		}
	}
	for _, event := range bundle.ExecutionEvents {
		event.Sequence = 0
		event.SessionID = session.ID
		event.TaskID = rewriteImportedTaskID(event.TaskID, bundle.Session.ID, session.ID)
		prepared, err := prepareExecutionEvent(event)
		if err != nil {
			return contract.Session{}, fmt.Errorf("invalid imported execution event: %w", err)
		}
		if _, err := insertExecutionEvent(ctx, tx, prepared); err != nil {
			return contract.Session{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return contract.Session{}, err
	}
	return session, nil
}

func rewriteImportedTaskID(taskID, oldSessionID, newSessionID string) string {
	if strings.HasPrefix(taskID, oldSessionID+":") {
		return newSessionID + strings.TrimPrefix(taskID, oldSessionID)
	}
	return "import:" + newSessionID + ":" + taskID
}
