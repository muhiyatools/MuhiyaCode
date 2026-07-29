package command

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

const historyProjectionName = "history"
const usageProjectionName = "usage"
const recentProjectionName = "recent-transcript"
const recentProjectionLimit = 300
const recentProjectionByteLimit = 8 << 20

func (a *Application) loadJournalHistory(
	ctx context.Context,
	session contract.Session,
	legacy orchestrator.HistorySnapshot,
	legacyErr error,
) (orchestrator.HistorySnapshot, error) {
	checkpointEvent, exists, err := a.db.LatestProjectionCheckpoint(ctx, session.ID, historyProjectionName)
	if err != nil {
		return orchestrator.HistorySnapshot{}, fmt.Errorf("load history checkpoint: %w", err)
	}
	if !exists {
		return a.bootstrapJournalHistory(ctx, session.ID, legacy, legacyErr)
	}
	snapshot, err := decodeHistoryCheckpoint(checkpointEvent)
	if err != nil {
		return orchestrator.HistorySnapshot{}, err
	}
	return a.replayHistoryEvents(ctx, session.ID, checkpointEvent.Sequence, snapshot)
}

func (a *Application) bootstrapJournalHistory(
	ctx context.Context,
	sessionID string,
	legacy orchestrator.HistorySnapshot,
	legacyErr error,
) (orchestrator.HistorySnapshot, error) {
	if legacyErr != nil {
		return orchestrator.HistorySnapshot{}, legacyErr
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		return orchestrator.HistorySnapshot{}, fmt.Errorf("encode bootstrap history: %w", err)
	}
	if err := a.bootstrapProjection(ctx, sessionID, historyProjectionName, payload); err != nil {
		return orchestrator.HistorySnapshot{}, err
	}
	return legacy, nil
}

func (a *Application) bootstrapProjection(ctx context.Context, sessionID, name string, payload []byte) error {
	checkpoint := contract.ProjectionCheckpoint{
		Name:      name,
		Version:   1,
		Payload:   payload,
		Checksum:  contract.ProjectionChecksum(payload),
		CreatedAt: time.Now().UTC(),
	}
	event := contract.ExecutionEvent{
		Version:    contract.ExecutionEventVersion,
		SessionID:  sessionID,
		TaskID:     "session:" + sessionID + ":bootstrap",
		Kind:       contract.ExecutionProjectionSaved,
		Checkpoint: &checkpoint,
	}
	if _, err := a.db.AppendExecutionEvent(ctx, event); err != nil {
		return fmt.Errorf("persist bootstrap %s: %w", name, err)
	}
	return nil
}

func (a *Application) loadJournalUsage(
	ctx context.Context,
	session contract.Session,
	legacy []contract.UsageRecord,
	legacyErr error,
) ([]contract.UsageRecord, error) {
	checkpoint, exists, err := a.db.LatestProjectionCheckpoint(ctx, session.ID, usageProjectionName)
	if err != nil {
		return nil, fmt.Errorf("load usage checkpoint: %w", err)
	}
	if !exists {
		return a.bootstrapJournalUsage(ctx, session.ID, legacy, legacyErr)
	}
	return a.decodeAndReplayUsage(ctx, session.ID, checkpoint)
}

func (a *Application) bootstrapJournalUsage(
	ctx context.Context,
	sessionID string,
	legacy []contract.UsageRecord,
	legacyErr error,
) ([]contract.UsageRecord, error) {
	if legacyErr != nil {
		return nil, legacyErr
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		return nil, fmt.Errorf("encode bootstrap usage: %w", err)
	}
	if err := a.bootstrapProjection(ctx, sessionID, usageProjectionName, payload); err != nil {
		return nil, err
	}
	return legacy, nil
}

func (a *Application) decodeAndReplayUsage(
	ctx context.Context,
	sessionID string,
	checkpoint contract.ExecutionEvent,
) ([]contract.UsageRecord, error) {
	var records []contract.UsageRecord
	if checkpoint.Checkpoint.Version != 1 {
		return nil, fmt.Errorf("unsupported usage checkpoint version %d", checkpoint.Checkpoint.Version)
	}
	if err := json.Unmarshal(checkpoint.Checkpoint.Payload, &records); err != nil {
		return nil, fmt.Errorf("decode usage checkpoint: %w", err)
	}
	return a.replayUsageEvents(ctx, sessionID, checkpoint.Sequence, records)
}

func (a *Application) replayUsageEvents(
	ctx context.Context,
	sessionID string,
	cursor int64,
	records []contract.UsageRecord,
) ([]contract.UsageRecord, error) {
	for {
		events, err := a.db.ExecutionEventsAfter(ctx, sessionID, cursor, 1_000)
		if err != nil {
			return nil, fmt.Errorf("replay usage events: %w", err)
		}
		if len(events) == 0 {
			return records, nil
		}
		for _, event := range events {
			if event.Kind == contract.ExecutionUsageRecorded {
				records = append(records, *event.Usage)
			}
		}
		cursor = events[len(events)-1].Sequence
	}
}

func decodeHistoryCheckpoint(event contract.ExecutionEvent) (orchestrator.HistorySnapshot, error) {
	if event.Checkpoint.Version != 1 {
		return orchestrator.HistorySnapshot{}, fmt.Errorf("unsupported history checkpoint version %d", event.Checkpoint.Version)
	}
	var snapshot orchestrator.HistorySnapshot
	if err := json.Unmarshal(event.Checkpoint.Payload, &snapshot); err != nil {
		return orchestrator.HistorySnapshot{}, fmt.Errorf("decode history checkpoint: %w", err)
	}
	if snapshot.Version != 1 {
		return orchestrator.HistorySnapshot{}, fmt.Errorf("unsupported history snapshot version %d", snapshot.Version)
	}
	return snapshot, nil
}

func (a *Application) replayHistoryEvents(
	ctx context.Context,
	sessionID string,
	cursor int64,
	snapshot orchestrator.HistorySnapshot,
) (orchestrator.HistorySnapshot, error) {
	for {
		events, err := a.db.ExecutionEventsAfter(ctx, sessionID, cursor, 1_000)
		if err != nil {
			return orchestrator.HistorySnapshot{}, fmt.Errorf("replay history events: %w", err)
		}
		if len(events) == 0 {
			return snapshot, nil
		}
		for _, event := range events {
			applyHistoryEvent(&snapshot, event)
		}
		cursor = events[len(events)-1].Sequence
	}
}

func applyHistoryEvent(snapshot *orchestrator.HistorySnapshot, event contract.ExecutionEvent) {
	switch event.Kind {
	case contract.ExecutionTaskStarted:
		snapshot.LastTaskStart = len(snapshot.Messages)
	case contract.ExecutionMessageRecorded:
		snapshot.Messages = append(snapshot.Messages, event.Message.Message)
	}
}

func (a *Application) loadJournalRecent(
	ctx context.Context,
	session contract.Session,
	legacy []contract.Event,
	legacyErr error,
) ([]contract.Event, error) {
	checkpoint, exists, err := a.db.LatestProjectionCheckpoint(ctx, session.ID, recentProjectionName)
	if err != nil {
		return nil, fmt.Errorf("load recent transcript checkpoint: %w", err)
	}
	if !exists {
		return a.bootstrapJournalRecent(ctx, session.ID, legacy, legacyErr)
	}
	recent, err := decodeRecentCheckpoint(checkpoint)
	if err != nil {
		return nil, err
	}
	recent, cursor, err := a.replayRecentEvents(ctx, session.ID, checkpoint.Sequence, recent)
	if err != nil {
		return nil, err
	}
	if cursor > checkpoint.Sequence {
		if err := a.persistRecentCheckpoint(ctx, session.ID, recent); err != nil {
			return nil, err
		}
	}
	return recent, nil
}

func (a *Application) bootstrapJournalRecent(
	ctx context.Context,
	sessionID string,
	legacy []contract.Event,
	legacyErr error,
) ([]contract.Event, error) {
	if legacyErr != nil {
		return nil, legacyErr
	}
	recent := trimRecentEvents(legacy)
	if err := a.persistRecentCheckpoint(ctx, sessionID, recent); err != nil {
		return nil, err
	}
	return recent, nil
}

func (a *Application) persistRecentCheckpoint(ctx context.Context, sessionID string, recent []contract.Event) error {
	payload, err := json.Marshal(recent)
	if err != nil {
		return fmt.Errorf("encode recent transcript checkpoint: %w", err)
	}
	if err := a.bootstrapProjection(ctx, sessionID, recentProjectionName, payload); err != nil {
		return fmt.Errorf("persist recent transcript checkpoint: %w", err)
	}
	return nil
}

func decodeRecentCheckpoint(event contract.ExecutionEvent) ([]contract.Event, error) {
	if event.Checkpoint.Version != 1 {
		return nil, fmt.Errorf("unsupported recent transcript checkpoint version %d", event.Checkpoint.Version)
	}
	var recent []contract.Event
	if err := json.Unmarshal(event.Checkpoint.Payload, &recent); err != nil {
		return nil, fmt.Errorf("decode recent transcript checkpoint: %w", err)
	}
	return trimRecentEvents(recent), nil
}

func (a *Application) replayRecentEvents(
	ctx context.Context,
	sessionID string,
	cursor int64,
	recent []contract.Event,
) ([]contract.Event, int64, error) {
	for {
		events, err := a.db.ExecutionEventsAfter(ctx, sessionID, cursor, 1_000)
		if err != nil {
			return nil, cursor, fmt.Errorf("replay recent transcript events: %w", err)
		}
		if len(events) == 0 {
			return trimRecentEvents(recent), cursor, nil
		}
		for _, event := range events {
			if visible, ok := visibleRecentEvent(event); ok {
				recent = append(recent, visible)
			}
		}
		recent = trimRecentEvents(recent)
		cursor = events[len(events)-1].Sequence
	}
}

func visibleRecentEvent(event contract.ExecutionEvent) (contract.Event, bool) {
	if event.Kind != contract.ExecutionMessageRecorded || event.Message == nil {
		return contract.Event{}, false
	}
	message := event.Message.Message
	if message.Role == contract.RoleAssistant && strings.TrimSpace(message.Content) == "" {
		return contract.Event{}, false
	}
	return contract.Event{
		Role:      string(message.Role),
		Type:      event.Message.Kind,
		Content:   message.Content,
		Target:    event.Message.Target,
		CreatedAt: event.Message.CreatedAt,
	}, true
}

func trimRecentEvents(events []contract.Event) []contract.Event {
	start := 0
	if len(events) > recentProjectionLimit {
		start = len(events) - recentProjectionLimit
	}
	totalBytes := 0
	for index := len(events) - 1; index >= start; index-- {
		eventBytes := len(events[index].Content) + len(events[index].Target)
		if totalBytes+eventBytes > recentProjectionByteLimit && index < len(events)-1 {
			start = index + 1
			break
		}
		totalBytes += eventBytes
	}
	return append([]contract.Event(nil), events[start:]...)
}
