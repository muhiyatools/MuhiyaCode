package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const ExecutionEventVersion = 1

var ErrToolNotStarted = errors.New("tool dispatch not started")

type ToolNotStartedError struct {
	Cause error
}

func (failure ToolNotStartedError) Error() string {
	return failure.Cause.Error()
}

func (failure ToolNotStartedError) Unwrap() error {
	return ErrToolNotStarted
}

func ToolNotStarted(cause error) error {
	return ToolNotStartedError{Cause: cause}
}

type ToolOutcomeStatus string

const (
	ToolOutcomeSucceeded     ToolOutcomeStatus = "succeeded"
	ToolOutcomeFailed        ToolOutcomeStatus = "failed"
	ToolOutcomeRejected      ToolOutcomeStatus = "rejected"
	ToolOutcomeCancelled     ToolOutcomeStatus = "cancelled"
	ToolOutcomeIndeterminate ToolOutcomeStatus = "indeterminate"
)

type MutationCertainty string

const (
	MutationNotApplicable MutationCertainty = "not_applicable"
	MutationNotStarted    MutationCertainty = "not_started"
	MutationCommitted     MutationCertainty = "committed"
	MutationIndeterminate MutationCertainty = "indeterminate"
)

type ToolOutcome struct {
	Call              ToolCall          `json:"call"`
	Status            ToolOutcomeStatus `json:"status"`
	Output            string            `json:"output,omitempty"`
	ErrorText         string            `json:"error,omitempty"`
	MutationCertainty MutationCertainty `json:"mutationCertainty"`
	Err               error             `json:"-"`
	DurationMS        int64             `json:"durationMs,omitempty"`
}

func (outcome ToolOutcome) Succeeded() bool {
	return outcome.Status == ToolOutcomeSucceeded
}

func (outcome ToolOutcome) IsFailure() bool {
	return outcome.Status != ToolOutcomeSucceeded
}

func (outcome ToolOutcome) WasRejected() bool {
	return outcome.Status == ToolOutcomeRejected
}

func (outcome ToolOutcome) IsIndeterminate() bool {
	return outcome.Status == ToolOutcomeIndeterminate
}

type ExecutionEventKind string

const (
	ExecutionTaskStarted     ExecutionEventKind = "task_started"
	ExecutionTaskTerminal    ExecutionEventKind = "task_terminal"
	ExecutionMessageRecorded ExecutionEventKind = "message_recorded"
	ExecutionProjectionSaved ExecutionEventKind = "projection_saved"
	ExecutionApprovalAsked   ExecutionEventKind = "approval_requested"
	ExecutionApprovalDecided ExecutionEventKind = "approval_resolved"
	ExecutionUsageRecorded   ExecutionEventKind = "usage_recorded"
	ExecutionVerification    ExecutionEventKind = "verification_recorded"
	ExecutionToolStarted     ExecutionEventKind = "tool_started"
	ExecutionToolTerminal    ExecutionEventKind = "tool_terminal"
	ExecutionMutationIntent  ExecutionEventKind = "mutation_intent"
	ExecutionMutationOutcome ExecutionEventKind = "mutation_outcome"
)

type TaskStarted struct {
	PromptHash string    `json:"promptHash"`
	CreatedAt  time.Time `json:"createdAt"`
}

type TaskTerminal struct {
	Status           TaskStatus `json:"status"`
	AnswerHash       string     `json:"answerHash,omitempty"`
	Error            string     `json:"error,omitempty"`
	TerminatedReason string     `json:"terminatedReason,omitempty"`
	ToolCalls        int        `json:"toolCalls"`
	Turns            int        `json:"turns"`
	FilesChanged     []string   `json:"filesChanged,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type MessageRecord struct {
	Message   Message   `json:"message"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ProjectionCheckpoint struct {
	Name      string          `json:"name"`
	Version   int             `json:"version"`
	Payload   json.RawMessage `json:"payload"`
	Checksum  string          `json:"checksum"`
	CreatedAt time.Time       `json:"createdAt"`
}

type ApprovalRequestRecord struct {
	RequestID string    `json:"requestId"`
	Action    string    `json:"action"`
	Target    string    `json:"target,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type ApprovalResolutionRecord struct {
	RequestID  string    `json:"requestId"`
	Approved   bool      `json:"approved"`
	Reconciled bool      `json:"reconciled,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type VerificationRecord struct {
	Result    VerificationResult `json:"result"`
	CreatedAt time.Time          `json:"createdAt"`
}

type ToolStartedRecord struct {
	ExecutionID   string    `json:"executionId"`
	CallID        string    `json:"callId"`
	ToolName      string    `json:"toolName"`
	ArgumentsHash string    `json:"argumentsHash"`
	Target        string    `json:"target,omitempty"`
	Mutating      bool      `json:"mutating"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ToolTerminalRecord struct {
	ExecutionID       string            `json:"executionId"`
	Status            ToolOutcomeStatus `json:"status"`
	MutationCertainty MutationCertainty `json:"mutationCertainty"`
	OutputHash        string            `json:"outputHash"`
	Error             string            `json:"error,omitempty"`
	DurationMS        int64             `json:"durationMs"`
	CreatedAt         time.Time         `json:"createdAt"`
}

type MutationIntent struct {
	IntentID       string    `json:"intentId"`
	IdempotencyKey string    `json:"idempotencyKey"`
	ToolName       string    `json:"toolName"`
	ArgumentsHash  string    `json:"argumentsHash"`
	Target         string    `json:"target,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type MutationOutcome struct {
	IntentID   string            `json:"intentId"`
	Status     ToolOutcomeStatus `json:"status"`
	Certainty  MutationCertainty `json:"certainty"`
	Reconciled bool              `json:"reconciled,omitempty"`
	Error      string            `json:"error,omitempty"`
	OutputHash string            `json:"outputHash,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
}

type ExecutionEvent struct {
	Sequence           int64                     `json:"sequence"`
	Version            int                       `json:"version"`
	SessionID          string                    `json:"sessionId"`
	TaskID             string                    `json:"taskId"`
	Kind               ExecutionEventKind        `json:"kind"`
	Started            *TaskStarted              `json:"started,omitempty"`
	Terminal           *TaskTerminal             `json:"terminal,omitempty"`
	Message            *MessageRecord            `json:"message,omitempty"`
	Checkpoint         *ProjectionCheckpoint     `json:"checkpoint,omitempty"`
	ApprovalRequest    *ApprovalRequestRecord    `json:"approvalRequest,omitempty"`
	ApprovalResolution *ApprovalResolutionRecord `json:"approvalResolution,omitempty"`
	Usage              *UsageRecord              `json:"usage,omitempty"`
	Verification       *VerificationRecord       `json:"verification,omitempty"`
	ToolStarted        *ToolStartedRecord        `json:"toolStarted,omitempty"`
	ToolTerminal       *ToolTerminalRecord       `json:"toolTerminal,omitempty"`
	Intent             *MutationIntent           `json:"intent,omitempty"`
	Outcome            *MutationOutcome          `json:"outcome,omitempty"`
	CreatedAt          time.Time                 `json:"createdAt"`
}

func (event ExecutionEvent) Validate() error {
	if event.Version != ExecutionEventVersion {
		return fmt.Errorf("unsupported execution event version %d", event.Version)
	}
	if event.SessionID == "" || event.TaskID == "" {
		return fmt.Errorf("execution event requires session and task")
	}
	validator, exists := executionEventValidators[event.Kind]
	if !exists {
		return fmt.Errorf("unknown execution event kind %q", event.Kind)
	}
	return validator(event)
}

var executionEventValidators = map[ExecutionEventKind]func(ExecutionEvent) error{
	ExecutionTaskStarted:     validateTaskStartedEvent,
	ExecutionTaskTerminal:    validateTaskTerminalEvent,
	ExecutionMessageRecorded: validateMessageEvent,
	ExecutionProjectionSaved: validateProjectionEvent,
	ExecutionApprovalAsked:   validateApprovalRequestEvent,
	ExecutionApprovalDecided: validateApprovalResolutionEvent,
	ExecutionUsageRecorded:   validateUsageEvent,
	ExecutionVerification:    validateVerificationEvent,
	ExecutionToolStarted:     validateToolStartedEvent,
	ExecutionToolTerminal:    validateToolTerminalEvent,
	ExecutionMutationIntent:  validateMutationIntentEvent,
	ExecutionMutationOutcome: validateMutationOutcomeEvent,
}

func validateTaskStartedEvent(event ExecutionEvent) error {
	if event.Started == nil || event.payloadCount() != 1 {
		return fmt.Errorf("task started event requires only a started payload")
	}
	if event.Started.PromptHash == "" {
		return fmt.Errorf("task started payload is incomplete")
	}
	return nil
}

func validateTaskTerminalEvent(event ExecutionEvent) error {
	if event.Terminal == nil || event.payloadCount() != 1 {
		return fmt.Errorf("task terminal event requires only a terminal payload")
	}
	if !validTaskStatus(event.Terminal.Status) {
		return fmt.Errorf("task terminal payload is incomplete")
	}
	return nil
}

func validateMessageEvent(event ExecutionEvent) error {
	if event.Message == nil || event.payloadCount() != 1 {
		return fmt.Errorf("message event requires only a message payload")
	}
	if !validMessageRecord(*event.Message) {
		return fmt.Errorf("message event payload is incomplete")
	}
	return nil
}

func validateProjectionEvent(event ExecutionEvent) error {
	if event.Checkpoint == nil || event.payloadCount() != 1 {
		return fmt.Errorf("projection event requires only a checkpoint payload")
	}
	if !validProjectionCheckpoint(*event.Checkpoint) {
		return fmt.Errorf("projection checkpoint payload is incomplete or corrupt")
	}
	return nil
}

func validateApprovalRequestEvent(event ExecutionEvent) error {
	if event.ApprovalRequest == nil || event.payloadCount() != 1 {
		return fmt.Errorf("approval request event requires only a request payload")
	}
	if event.ApprovalRequest.RequestID == "" || event.ApprovalRequest.Action == "" {
		return fmt.Errorf("approval request payload is incomplete")
	}
	return nil
}

func validateApprovalResolutionEvent(event ExecutionEvent) error {
	if event.ApprovalResolution == nil || event.payloadCount() != 1 {
		return fmt.Errorf("approval resolution event requires only a resolution payload")
	}
	if event.ApprovalResolution.RequestID == "" {
		return fmt.Errorf("approval resolution payload is incomplete")
	}
	return nil
}

func validateUsageEvent(event ExecutionEvent) error {
	if event.Usage == nil || event.payloadCount() != 1 {
		return fmt.Errorf("usage event requires only a usage payload")
	}
	if event.Usage.Seq <= 0 {
		return fmt.Errorf("usage event payload is incomplete")
	}
	return nil
}

func validateVerificationEvent(event ExecutionEvent) error {
	if event.Verification == nil || event.payloadCount() != 1 {
		return fmt.Errorf("verification event requires only a verification payload")
	}
	if !event.Verification.Result.Ran || event.Verification.Result.Result == "" {
		return fmt.Errorf("verification event payload is incomplete")
	}
	return nil
}

func validateToolStartedEvent(event ExecutionEvent) error {
	if event.ToolStarted == nil || event.payloadCount() != 1 {
		return fmt.Errorf("tool started event requires only a tool-started payload")
	}
	started := event.ToolStarted
	if started.ExecutionID == "" || started.CallID == "" || started.ToolName == "" || started.ArgumentsHash == "" {
		return fmt.Errorf("tool started payload is incomplete")
	}
	return nil
}

func validateToolTerminalEvent(event ExecutionEvent) error {
	if event.ToolTerminal == nil || event.payloadCount() != 1 {
		return fmt.Errorf("tool terminal event requires only a tool-terminal payload")
	}
	terminal := event.ToolTerminal
	if terminal.ExecutionID == "" || terminal.OutputHash == "" ||
		!validToolOutcomeStatus(terminal.Status) ||
		!validMutationCertainty(terminal.MutationCertainty) ||
		terminal.DurationMS < 0 {
		return fmt.Errorf("tool terminal payload is incomplete")
	}
	return nil
}

func validateMutationIntentEvent(event ExecutionEvent) error {
	if event.Intent == nil || event.payloadCount() != 1 {
		return fmt.Errorf("mutation intent event requires only an intent payload")
	}
	intent := event.Intent
	if intent.IntentID == "" || intent.IdempotencyKey == "" || intent.ToolName == "" || intent.ArgumentsHash == "" {
		return fmt.Errorf("mutation intent payload is incomplete")
	}
	return nil
}

func validateMutationOutcomeEvent(event ExecutionEvent) error {
	if event.Outcome == nil || event.payloadCount() != 1 {
		return fmt.Errorf("mutation outcome event requires only an outcome payload")
	}
	outcome := event.Outcome
	if outcome.IntentID == "" || !validToolOutcomeStatus(outcome.Status) || !validMutationCertainty(outcome.Certainty) {
		return fmt.Errorf("mutation outcome payload is incomplete")
	}
	return nil
}

func (event ExecutionEvent) payloadCount() int {
	count := 0
	for _, present := range []bool{
		event.Started != nil,
		event.Terminal != nil,
		event.Message != nil,
		event.Checkpoint != nil,
		event.ApprovalRequest != nil,
		event.ApprovalResolution != nil,
		event.Usage != nil,
		event.Verification != nil,
		event.ToolStarted != nil,
		event.ToolTerminal != nil,
		event.Intent != nil,
		event.Outcome != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func validMessageRecord(record MessageRecord) bool {
	switch record.Message.Role {
	case RoleUser, RoleAssistant:
		return record.Kind != "" && (record.Message.Content != "" || len(record.Message.ToolCalls) > 0)
	case RoleTool:
		return record.Kind != "" && record.Message.ToolCallID != ""
	default:
		return false
	}
}

func validProjectionCheckpoint(checkpoint ProjectionCheckpoint) bool {
	return checkpoint.Name != "" &&
		checkpoint.Version > 0 &&
		len(checkpoint.Payload) > 0 &&
		checkpoint.Checksum == ProjectionChecksum(checkpoint.Payload)
}

func ProjectionChecksum(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func validTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskStatusSucceeded, TaskStatusIncomplete, TaskStatusIndeterminate, TaskStatusFailed, TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func validToolOutcomeStatus(status ToolOutcomeStatus) bool {
	switch status {
	case ToolOutcomeSucceeded, ToolOutcomeFailed, ToolOutcomeRejected, ToolOutcomeCancelled, ToolOutcomeIndeterminate:
		return true
	default:
		return false
	}
}

func validMutationCertainty(certainty MutationCertainty) bool {
	switch certainty {
	case MutationNotApplicable, MutationNotStarted, MutationCommitted, MutationIndeterminate:
		return true
	default:
		return false
	}
}

// PendingMutationIntents projects a valid event prefix into the intents that
// have no durable outcome. It is deterministic and idempotent so startup repair
// and crash-injection tests share one reconstruction rule.
func PendingMutationIntents(events []ExecutionEvent) []MutationIntent {
	pending := make(map[string]MutationIntent)
	for _, event := range events {
		switch event.Kind {
		case ExecutionMutationIntent:
			if event.Intent != nil {
				pending[event.Intent.IntentID] = *event.Intent
			}
		case ExecutionMutationOutcome:
			if event.Outcome != nil {
				delete(pending, event.Outcome.IntentID)
			}
		}
	}
	result := make([]MutationIntent, 0, len(pending))
	for _, intent := range pending {
		result = append(result, intent)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].IntentID < result[j].IntentID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func PendingApprovalRequests(events []ExecutionEvent) []ApprovalRequestRecord {
	pending := make(map[string]ApprovalRequestRecord)
	for _, event := range events {
		switch event.Kind {
		case ExecutionApprovalAsked:
			if event.ApprovalRequest != nil {
				pending[event.ApprovalRequest.RequestID] = *event.ApprovalRequest
			}
		case ExecutionApprovalDecided:
			if event.ApprovalResolution != nil {
				delete(pending, event.ApprovalResolution.RequestID)
			}
		}
	}
	requests := make([]ApprovalRequestRecord, 0, len(pending))
	for _, request := range pending {
		requests = append(requests, request)
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].CreatedAt.Equal(requests[j].CreatedAt) {
			return requests[i].RequestID < requests[j].RequestID
		}
		return requests[i].CreatedAt.Before(requests[j].CreatedAt)
	})
	return requests
}

func IncompleteToolExecutions(events []ExecutionEvent) []ToolStartedRecord {
	pending := make(map[string]ToolStartedRecord)
	for _, event := range events {
		switch event.Kind {
		case ExecutionToolStarted:
			if event.ToolStarted != nil {
				pending[event.ToolStarted.ExecutionID] = *event.ToolStarted
			}
		case ExecutionToolTerminal:
			if event.ToolTerminal != nil {
				delete(pending, event.ToolTerminal.ExecutionID)
			}
		}
	}
	executions := make([]ToolStartedRecord, 0, len(pending))
	for _, started := range pending {
		executions = append(executions, started)
	}
	sort.Slice(executions, func(i, j int) bool {
		if executions[i].CreatedAt.Equal(executions[j].CreatedAt) {
			return executions[i].ExecutionID < executions[j].ExecutionID
		}
		return executions[i].CreatedAt.Before(executions[j].CreatedAt)
	})
	return executions
}

// IncompleteTaskIDs returns started tasks that have no durable terminal event.
// It intentionally accepts any valid event prefix so crash recovery and doctor
// diagnostics use the same deterministic projection.
func IncompleteTaskIDs(events []ExecutionEvent) []string {
	pending := make(map[string]time.Time)
	for _, event := range events {
		switch event.Kind {
		case ExecutionTaskStarted:
			if event.Started != nil {
				pending[event.TaskID] = event.Started.CreatedAt
			}
		case ExecutionTaskTerminal:
			delete(pending, event.TaskID)
		}
	}
	type entry struct {
		id string
		at time.Time
	}
	entries := make([]entry, 0, len(pending))
	for id, at := range pending {
		entries = append(entries, entry{id: id, at: at})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].at.Equal(entries[j].at) {
			return entries[i].id < entries[j].id
		}
		return entries[i].at.Before(entries[j].at)
	})
	result := make([]string, 0, len(entries))
	for _, item := range entries {
		result = append(result, item.id)
	}
	return result
}
