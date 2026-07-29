package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) callMayMutate(call contract.ToolCall) bool {
	name := call.ToolName()
	switch {
	case name == "run_shell":
		var input struct {
			Command string `json:"command"`
		}
		return json.Unmarshal([]byte(call.ArgumentsJSON()), &input) != nil || !IsReadOnlyShell(input.Command)
	case strings.HasPrefix(name, "mcp__"):
		return !e.registry.DeclaresReadOnly(name)
	default:
		return isMutation(name)
	}
}

func (e *Engine) executionTaskID() string {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.taskExecutionID
}

func (e *Engine) persistMutationIntent(ctx context.Context, call contract.ToolCall) (contract.MutationIntent, error) {
	taskID := e.executionTaskID()
	argumentsHash := hashExecutionValue(call.ArgumentsJSON())
	callID := call.ID
	if callID == "" {
		callID = argumentsHash[:16]
	}
	idempotencyKey := callID + ":" + argumentsHash[:16]
	intent := contract.MutationIntent{
		IntentID:       taskID + ":" + idempotencyKey,
		IdempotencyKey: idempotencyKey,
		ToolName:       call.ToolName(),
		ArgumentsHash:  argumentsHash,
		Target:         e.executionTarget(call, argumentsHash),
		CreatedAt:      time.Now().UTC(),
	}
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion,
		TaskID:  taskID,
		Kind:    contract.ExecutionMutationIntent,
		Intent:  &intent,
	}
	if err := e.appendExecutionEvent(ctx, event, "persist mutation intent"); err != nil {
		return contract.MutationIntent{}, err
	}
	return intent, nil
}

func (e *Engine) persistToolStarted(ctx context.Context, call contract.ToolCall, mutating bool) (contract.ToolStartedRecord, error) {
	argumentsHash := hashExecutionValue(call.ArgumentsJSON())
	callID := call.ID
	if callID == "" {
		callID = argumentsHash[:16]
	}
	createdAt := time.Now().UTC()
	record := contract.ToolStartedRecord{
		ExecutionID:   hashExecutionValue(fmt.Sprintf("%s\x00%s\x00%s\x00%d", e.executionTaskID(), callID, argumentsHash, createdAt.UnixNano()))[:32],
		CallID:        callID,
		ToolName:      call.ToolName(),
		ArgumentsHash: argumentsHash,
		Target:        e.executionTarget(call, argumentsHash),
		Mutating:      mutating,
		CreatedAt:     createdAt,
	}
	event := contract.ExecutionEvent{
		Version:     contract.ExecutionEventVersion,
		TaskID:      e.executionTaskID(),
		Kind:        contract.ExecutionToolStarted,
		ToolStarted: &record,
	}
	if err := e.appendExecutionEvent(ctx, event, "persist tool start"); err != nil {
		return contract.ToolStartedRecord{}, err
	}
	return record, nil
}

func (e *Engine) executionTarget(call contract.ToolCall, argumentsHash string) string {
	switch {
	case call.ToolName() == "run_shell":
		return "shell:" + argumentsHash[:16]
	case strings.HasPrefix(call.ToolName(), "mcp__"):
		return ""
	default:
		return e.redact(contract.ToolTarget(call.ToolName(), []byte(call.ArgumentsJSON())))
	}
}

func (e *Engine) persistTaskStarted(ctx context.Context, prompt string) error {
	started := contract.TaskStarted{
		PromptHash: hashExecutionValue(prompt),
		CreatedAt:  time.Now().UTC(),
	}
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion,
		TaskID:  e.executionTaskID(),
		Kind:    contract.ExecutionTaskStarted,
		Started: &started,
	}
	return e.appendExecutionEvent(ctx, event, "persist task start")
}

func (e *Engine) persistTaskTerminal(ctx context.Context, answer string, stats contract.TaskStats, runErr error) error {
	events, err := e.taskTerminalEvents(answer, stats, runErr)
	if err != nil {
		return err
	}
	return e.appendExecutionEvents(ctx, events, "persist task terminal")
}

func (e *Engine) taskTerminalEvents(answer string, stats contract.TaskStats, runErr error) ([]contract.ExecutionEvent, error) {
	events := make([]contract.ExecutionEvent, 0, 3)
	if verification, ok := e.verificationEvent(stats.Verification); ok {
		events = append(events, verification)
	}
	checkpoint, err := e.historyCheckpointEvent()
	if err != nil {
		return nil, err
	}
	events = append(events, checkpoint, e.taskTerminalEvent(answer, stats, runErr))
	return events, nil
}

func (e *Engine) taskTerminalEvent(answer string, stats contract.TaskStats, runErr error) contract.ExecutionEvent {
	terminal := contract.TaskTerminal{
		Status:           stats.Status,
		AnswerHash:       hashExecutionValue(answer),
		TerminatedReason: stats.TerminatedReason,
		ToolCalls:        stats.ToolCalls,
		Turns:            stats.Turns,
		FilesChanged:     append([]string(nil), stats.FilesChanged...),
		CreatedAt:        time.Now().UTC(),
	}
	if runErr != nil {
		terminal.Error = e.redact(runErr.Error())
	}
	return contract.ExecutionEvent{
		Version:  contract.ExecutionEventVersion,
		TaskID:   e.executionTaskID(),
		Kind:     contract.ExecutionTaskTerminal,
		Terminal: &terminal,
	}
}

func (e *Engine) BeginApproval(ctx context.Context, action, target, reason string) (contract.ApprovalRequestRecord, error) {
	record := e.approvalRequestRecord(action, target, reason)
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, TaskID: e.executionTaskID(),
		Kind: contract.ExecutionApprovalAsked, ApprovalRequest: &record,
	}
	if err := e.appendExecutionEvent(ctx, event, "persist approval request"); err != nil {
		return contract.ApprovalRequestRecord{}, err
	}
	return record, nil
}

func (e *Engine) approvalRequestRecord(action, target, reason string) contract.ApprovalRequestRecord {
	now := time.Now().UTC()
	requestID := hashExecutionValue(fmt.Sprintf("%s\x00%s\x00%s\x00%d", action, target, reason, now.UnixNano()))[:24]
	return contract.ApprovalRequestRecord{
		RequestID: requestID,
		Action:    action,
		Target:    e.approvalTarget(action, target),
		Reason:    e.redact(reason),
		CreatedAt: now,
	}
}

func (e *Engine) ResolveApproval(
	ctx context.Context,
	request contract.ApprovalRequestRecord,
	approved bool,
	approvalErr error,
) error {
	resolution := e.approvalResolutionRecord(request.RequestID, approved, approvalErr)
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, TaskID: e.executionTaskID(),
		Kind: contract.ExecutionApprovalDecided, ApprovalResolution: &resolution,
	}
	return e.appendExecutionEvent(ctx, event, "persist approval resolution")
}

func (e *Engine) approvalResolutionRecord(requestID string, approved bool, approvalErr error) contract.ApprovalResolutionRecord {
	record := contract.ApprovalResolutionRecord{
		RequestID: requestID,
		Approved:  approved,
		CreatedAt: time.Now().UTC(),
	}
	if approvalErr != nil {
		record.Error = e.redact(approvalErr.Error())
	}
	return record
}

func (e *Engine) approvalTarget(action, target string) string {
	if action == "shell" {
		return "shell:" + hashExecutionValue(target)[:16]
	}
	return e.redact(target)
}

func (e *Engine) persistUsageEvent(ctx context.Context, usage contract.UsageRecord) error {
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion,
		TaskID:  e.executionTaskID(),
		Kind:    contract.ExecutionUsageRecorded,
		Usage:   &usage,
	}
	return e.appendExecutionEvent(ctx, event, "persist usage event")
}

func (e *Engine) verificationEvent(result contract.VerificationResult) (contract.ExecutionEvent, bool) {
	if !result.Ran {
		return contract.ExecutionEvent{}, false
	}
	result.OutputTruncated = e.redact(result.OutputTruncated)
	record := contract.VerificationRecord{Result: result, CreatedAt: time.Now().UTC()}
	return contract.ExecutionEvent{
		Version:      contract.ExecutionEventVersion,
		TaskID:       e.executionTaskID(),
		Kind:         contract.ExecutionVerification,
		Verification: &record,
	}, true
}

func (e *Engine) appendExecutionEvent(ctx context.Context, event contract.ExecutionEvent, operation string) error {
	if e.persistence.AppendExecutionEvent != nil {
		if err := e.persistence.AppendExecutionEvent(ctx, event); err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
		return nil
	}
	if e.persistence.AppendExecutionEvents != nil {
		if err := e.persistence.AppendExecutionEvents(ctx, []contract.ExecutionEvent{event}); err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
	}
	return nil
}

func (e *Engine) appendExecutionEvents(ctx context.Context, events []contract.ExecutionEvent, operation string) error {
	if e.persistence.AppendExecutionEvents != nil {
		if err := e.persistence.AppendExecutionEvents(ctx, events); err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
		return nil
	}
	for _, event := range events {
		if err := e.appendExecutionEvent(ctx, event, operation); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) persistMessageEvent(ctx context.Context, message contract.Message, kind, target string) error {
	record := contract.MessageRecord{
		Message:   e.redactedConversationMessage(message),
		Kind:      kind,
		Target:    e.redact(target),
		CreatedAt: time.Now().UTC(),
	}
	event := contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion,
		TaskID:  e.executionTaskID(),
		Kind:    contract.ExecutionMessageRecorded,
		Message: &record,
	}
	return e.appendExecutionEvent(ctx, event, "persist conversation message")
}

func (e *Engine) historyCheckpointEvent() (contract.ExecutionEvent, error) {
	snapshot, err := json.Marshal(e.history.Snapshot())
	if err != nil {
		return contract.ExecutionEvent{}, fmt.Errorf("encode history checkpoint: %w", err)
	}
	checkpoint := contract.ProjectionCheckpoint{
		Name:      "history",
		Version:   1,
		Payload:   snapshot,
		Checksum:  contract.ProjectionChecksum(snapshot),
		CreatedAt: time.Now().UTC(),
	}
	return contract.ExecutionEvent{
		Version:    contract.ExecutionEventVersion,
		TaskID:     e.executionTaskID(),
		Kind:       contract.ExecutionProjectionSaved,
		Checkpoint: &checkpoint,
	}, nil
}

func (e *Engine) redactedConversationMessage(message contract.Message) contract.Message {
	redacted := contract.Message{
		Role:       message.Role,
		Content:    e.redact(message.Content),
		ToolCallID: message.ToolCallID,
	}
	if message.ReasoningContent != nil {
		reasoning := e.redact(*message.ReasoningContent)
		redacted.ReasoningContent = &reasoning
	}
	if len(message.ReasoningDetails) > 0 {
		redacted.ReasoningDetails = json.RawMessage(e.redact(string(message.ReasoningDetails)))
	}
	redacted.ToolCalls = e.redactedToolCalls(message.ToolCalls)
	return redacted
}

func (e *Engine) redactedToolCalls(calls []contract.ToolCall) []contract.ToolCall {
	redacted := make([]contract.ToolCall, 0, len(calls))
	for _, call := range calls {
		redacted = append(redacted, contract.NewToolCall(
			call.ID,
			call.ToolName(),
			e.redact(call.ArgumentsJSON()),
		))
	}
	return redacted
}

func (e *Engine) finalizeTaskJournal(parent context.Context, answer string, stats *contract.TaskStats, runErr *error) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	err := e.persistTaskTerminal(persistCtx, answer, *stats, *runErr)
	cancel()
	if err == nil {
		return
	}
	*runErr = errors.Join(*runErr, err)
	stats.StopCause = contract.StopCauseError
	if len(stats.FilesChanged) > 0 {
		stats.Status = contract.TaskStatusIndeterminate
	} else {
		stats.Status = contract.TaskStatusFailed
	}
	if stats.TerminatedReason == "" {
		stats.TerminatedReason = "Task terminal could not be persisted"
	} else {
		stats.TerminatedReason += "; task terminal could not be persisted"
	}
}

func (e *Engine) mutationOutcomeEvent(intent contract.MutationIntent, outcome toolOutcome) contract.ExecutionEvent {
	mutationOutcome := contract.MutationOutcome{
		IntentID:   intent.IntentID,
		Status:     outcome.Status,
		Certainty:  outcome.MutationCertainty,
		Error:      e.redact(outcome.ErrorText),
		OutputHash: hashExecutionValue(outcome.Output),
		CreatedAt:  time.Now().UTC(),
	}
	return contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion,
		TaskID:  e.executionTaskID(),
		Kind:    contract.ExecutionMutationOutcome,
		Outcome: &mutationOutcome,
	}
}

func (e *Engine) persistDispatchTerminal(
	ctx context.Context,
	started contract.ToolStartedRecord,
	outcome toolOutcome,
	intent *contract.MutationIntent,
) error {
	terminal := contract.ToolTerminalRecord{
		ExecutionID:       started.ExecutionID,
		Status:            outcome.Status,
		MutationCertainty: outcome.MutationCertainty,
		OutputHash:        hashExecutionValue(outcome.Output),
		Error:             e.redact(outcome.ErrorText),
		DurationMS:        max(0, time.Since(started.CreatedAt).Milliseconds()),
		CreatedAt:         time.Now().UTC(),
	}
	events := make([]contract.ExecutionEvent, 0, 2)
	if intent != nil {
		events = append(events, e.mutationOutcomeEvent(*intent, outcome))
	}
	events = append(events, contract.ExecutionEvent{
		Version:      contract.ExecutionEventVersion,
		TaskID:       e.executionTaskID(),
		Kind:         contract.ExecutionToolTerminal,
		ToolTerminal: &terminal,
	})
	return e.appendExecutionEvents(ctx, events, "persist tool terminal")
}

func (e *Engine) finalizeDispatchJournal(
	parent context.Context,
	started contract.ToolStartedRecord,
	outcome toolOutcome,
	intent *contract.MutationIntent,
) toolOutcome {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), 2*time.Second)
	err := e.persistDispatchTerminal(persistCtx, started, outcome, intent)
	cancel()
	if err == nil {
		return outcome
	}
	outcome.Status = contract.ToolOutcomeIndeterminate
	outcome.Err = errors.Join(outcome.Err, err)
	outcome.ErrorText = outcome.Err.Error()
	if intent != nil {
		outcome.MutationCertainty = contract.MutationIndeterminate
		outcome.Output += "\nmutation outcome: indeterminate because dispatch completion could not be recorded; inspect before retrying: " + err.Error()
	} else {
		outcome.Output += "\ntool outcome: indeterminate because dispatch completion could not be recorded: " + err.Error()
	}
	return outcome
}

func hashExecutionValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
