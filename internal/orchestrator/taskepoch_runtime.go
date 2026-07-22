package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type TaskEpochLedgerSnapshot struct {
	Version  int         `json:"version"`
	ActiveID string      `json:"activeId,omitempty"`
	Records  []TaskEpoch `json:"records"`
}

type EpochTaskResult struct {
	Goal         string
	Answer       string
	Class        TaskClass
	Success      bool
	FilesChanged []string
	ChecksRun    int
	LinesAdded   int
	LinesRemoved int
}

type EpochTransitionObservation struct {
	Decision             RelatednessDecision `json:"decision"`
	Reasons              []string            `json:"reasons"`
	KeepEstimatedTokens  int                 `json:"keepEstimatedTokens"`
	ResetEstimatedTokens int                 `json:"resetEstimatedTokens"`
	PredictedSavings     int                 `json:"predictedSavings"`
	Enforced             bool                `json:"enforced"`
}

func cloneTaskEpochLedger(value TaskEpochLedgerSnapshot) TaskEpochLedgerSnapshot {
	value.Records = append([]TaskEpoch(nil), value.Records...)
	if value.Version == 0 {
		value.Version = 1
	}
	return value
}

func (e *Engine) prepareTaskEpoch(ctx context.Context, prompt string, assessment Assessment) error {
	mode := strings.ToLower(strings.TrimSpace(e.settings.TokenEconomyMode))
	if mode == "off" {
		return nil
	}
	modelID := e.settings.Provider.ActiveModelID
	upstreamPin := e.session.ID + ":main"
	if strings.TrimSpace(e.session.ID) == "" || strings.TrimSpace(modelID) == "" || strings.TrimSpace(e.workspaceID) == "" {
		return nil
	}
	if e.taskEpochs.ActiveID == "" {
		epoch, err := NewTaskEpoch(NewTaskEpochInput{SessionID: e.session.ID, ModelID: modelID,
			UpstreamPin: upstreamPin, Ordinal: nextEpochOrdinal(e.taskEpochs.Records), Goal: prompt, Class: assessment.Class})
		if err != nil {
			return err
		}
		e.taskEpochs.Records = append(e.taskEpochs.Records, epoch)
		e.taskEpochs.ActiveID = epoch.ID
		if e.persistence.WriteTaskEpochs != nil {
			if err := e.persistence.WriteTaskEpochs(ctx, cloneTaskEpochLedger(e.taskEpochs)); err != nil {
				e.taskEpochs.Records = e.taskEpochs.Records[:len(e.taskEpochs.Records)-1]
				e.taskEpochs.ActiveID = ""
				return fmt.Errorf("persist initial task epoch: %w", err)
			}
		}
		return nil
	}
	activeIndex := activeEpochIndex(e.taskEpochs)
	if activeIndex < 0 {
		return errors.New("active task epoch is missing")
	}
	active := e.taskEpochs.Records[activeIndex]
	if err := active.ValidateIdentity(modelID, upstreamPin); err != nil {
		return err
	}
	if active.Ordinal == 0 {
		first, err := NewTaskEpoch(NewTaskEpochInput{SessionID: e.session.ID, ModelID: modelID,
			UpstreamPin: upstreamPin, Ordinal: 1, Goal: prompt, Class: assessment.Class})
		if err != nil {
			return err
		}
		e.taskEpochs.Records = append(e.taskEpochs.Records, first)
		e.taskEpochs.ActiveID = first.ID
		if e.persistence.WriteTaskEpochs != nil {
			if err := e.persistence.WriteTaskEpochs(ctx, cloneTaskEpochLedger(e.taskEpochs)); err != nil {
				e.taskEpochs.Records = e.taskEpochs.Records[:len(e.taskEpochs.Records)-1]
				e.taskEpochs.ActiveID = active.ID
				return fmt.Errorf("persist first task epoch: %w", err)
			}
		}
		return nil
	}
	if e.lastEpochTask == nil {
		return nil
	}
	result := e.epochHysteresis.Decide(RelatednessInput{
		Prompt: prompt, CurrentGoal: active.Goal, CurrentSettled: e.lastEpochTask.Success,
		KnownPaths: e.lastEpochTask.FilesChanged,
	})
	observation := EpochTransitionObservation{Decision: result.Decision, Reasons: append([]string(nil), result.Reasons...),
		KeepEstimatedTokens: e.history.EstimatedTokens()}
	if result.Decision != RelatedNewEpoch {
		e.epochObservations = append(e.epochObservations, observation)
		return nil
	}
	selected, capsule, err := e.prepareEpochCapsule(prompt, active)
	if err != nil {
		return err
	}
	resetMessages := renderCapsuleMessages(selected)
	observation.ResetEstimatedTokens = estimateMessagesTokens(resetMessages)
	observation.PredictedSavings = max(0, observation.KeepEstimatedTokens-observation.ResetEstimatedTokens)
	if mode == "observe" {
		e.epochObservations = append(e.epochObservations, observation)
		return nil
	}
	if mode != "balanced" && mode != "aggressive" {
		e.epochObservations = append(e.epochObservations, observation)
		return nil
	}
	if e.persistence.WriteCapsule == nil || e.persistence.WriteTaskEpochs == nil {
		observation.Reasons = append(observation.Reasons, "durable_store_unavailable")
		e.epochObservations = append(e.epochObservations, observation)
		return nil
	}
	if err := e.persistence.WriteCapsule(ctx, capsule); err != nil {
		return fmt.Errorf("persist task capsule before epoch reset: %w", err)
	}
	settled := active
	if err := settled.Transition(EpochSettling, capsule.CreatedAt); err != nil {
		return err
	}
	if err := settled.Transition(EpochSettled, capsule.CreatedAt); err != nil {
		return err
	}
	newEpoch, err := NewTaskEpoch(NewTaskEpochInput{
		SessionID: e.session.ID, ModelID: modelID, UpstreamPin: upstreamPin,
		Ordinal: nextEpochOrdinal(e.taskEpochs.Records), Goal: prompt, Class: assessment.Class,
		RelatedCapsules: capsuleRefs(selected),
	})
	if err != nil {
		return err
	}
	candidate := cloneTaskEpochLedger(e.taskEpochs)
	candidate.Records[activeIndex] = settled
	candidate.Records = append(candidate.Records, newEpoch)
	// Precommit records the settled capsule and candidate epoch while leaving the
	// old active ID authoritative. History replacement then commits the new ID.
	if err := e.persistence.WriteTaskEpochs(ctx, candidate); err != nil {
		return fmt.Errorf("persist epoch checkpoint: %w", err)
	}
	commit := func() error {
		if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
			Cause: contract.InvalidationTaskEpoch, Trigger: contract.InvalidationBoundary,
			Scope:      "task epoch " + active.ID + " -> " + newEpoch.ID,
			RequestSeq: e.nextRequestSeq(),
		}); err != nil {
			return err
		}
		candidate.ActiveID = newEpoch.ID
		return e.persistence.WriteTaskEpochs(ctx, candidate)
	}
	if err := e.history.ReplaceForEpoch(newEpoch.ID, resetMessages, commit); err != nil {
		return fmt.Errorf("replace task epoch history: %w", err)
	}
	e.taskEpochs = candidate
	e.taskEpochs.ActiveID = newEpoch.ID
	e.capsules = append(e.capsules, capsule)
	e.lastEpochTask = nil
	observation.Enforced = true
	e.epochObservations = append(e.epochObservations, observation)
	return nil
}

func (e *Engine) prepareEpochCapsule(prompt string, active TaskEpoch) ([]TaskCapsule, TaskCapsule, error) {
	last := e.lastEpochTask
	deltas := make([]FileDeltaRef, 0, len(last.FilesChanged))
	for _, path := range last.FilesChanged {
		deltas = append(deltas, FileDeltaRef{Path: path, AfterFingerprint: fileFingerprint(e.session.WorkspacePath, path),
			LinesAdded: last.LinesAdded, LinesRemoved: last.LinesRemoved})
	}
	checks := []CheckResultRef{}
	if last.ChecksRun > 0 {
		checks = append(checks, CheckResultRef{Name: "runtime checks", Status: "passed"})
	}
	key := sha256.Sum256([]byte(e.session.ID + "\x00" + e.workspaceID))
	capsule, err := BuildTaskCapsule(CapsuleBuildInput{
		EpochID: active.ID, SessionID: e.session.ID, WorkspaceID: e.workspaceID,
		Goal: active.Goal, Outcome: CapsuleCompleted, OutcomeDetail: last.Answer,
		ChangedFiles: deltas, Checks: checks, Paths: last.FilesChanged,
	}, key[:], nil)
	if err != nil {
		return nil, TaskCapsule{}, err
	}
	index := BuildCapsuleIndex(append(append([]TaskCapsule(nil), e.capsules...), capsule))
	selected, _ := index.Retrieve(RetrievalInput{Goal: prompt, WorkspaceID: e.workspaceID,
		Paths: extractPaths(prompt), TokenBudget: 1_200, MaxItems: 6, SigningKey: key[:],
		Validity: CapsuleWorkspaceValidity(e.session.WorkspacePath)})
	return selected, capsule, nil
}

func fileFingerprint(root, relative string) string {
	target := filepath.Join(root, filepath.FromSlash(relative))
	content, err := os.ReadFile(target)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash[:])
}

func (e *Engine) recordEpochTaskResult(prompt, answer string, stats contract.TaskStats, runErr error) {
	if strings.EqualFold(e.settings.TokenEconomyMode, "off") {
		return
	}
	e.lastEpochTask = &EpochTaskResult{Goal: prompt, Answer: boundText(answer, 2_000),
		Class: TaskClass(stats.TaskClass), Success: runErr == nil, FilesChanged: append([]string(nil), stats.FilesChanged...),
		ChecksRun: stats.ChecksRun, LinesAdded: stats.LinesAdded, LinesRemoved: stats.LinesRemoved}
}

func (e *Engine) EpochTransitionObservations() []EpochTransitionObservation {
	return append([]EpochTransitionObservation(nil), e.epochObservations...)
}

func (e *Engine) attachEpochStats(stats *contract.TaskStats) {
	stats.TaskEpochCount = len(e.taskEpochs.Records)
	stats.CapsuleCount = len(e.capsules)
	if index := activeEpochIndex(e.taskEpochs); index >= 0 {
		for _, ref := range e.taskEpochs.Records[index].RelatedCapsules {
			stats.SelectedCapsuleRefs = append(stats.SelectedCapsuleRefs, ref.ID)
		}
	}
	if count := len(e.epochObservations); count > 0 {
		last := e.epochObservations[count-1]
		stats.EpochResetReasons = append(stats.EpochResetReasons, last.Reasons...)
		stats.EpochFirstPromptSavings = last.PredictedSavings
	}
}

func (e *Engine) activeTaskEpochPointer() *string {
	if e.taskEpochs.ActiveID == "" {
		return nil
	}
	value := e.taskEpochs.ActiveID
	return &value
}

func nextEpochOrdinal(records []TaskEpoch) uint64 {
	var maximum uint64
	for _, epoch := range records {
		if epoch.Ordinal > maximum {
			maximum = epoch.Ordinal
		}
	}
	return maximum + 1
}

func activeEpochIndex(ledger TaskEpochLedgerSnapshot) int {
	for index := range ledger.Records {
		if ledger.Records[index].ID == ledger.ActiveID {
			return index
		}
	}
	return -1
}

func capsuleRefs(capsules []TaskCapsule) []CapsuleRef {
	refs := make([]CapsuleRef, 0, len(capsules))
	for _, capsule := range capsules {
		refs = append(refs, CapsuleRef{ID: capsule.ID, Reason: "hybrid_retrieval", EstimatedTokens: capsule.EstimatedTokens()})
	}
	return refs
}

func renderCapsuleMessages(capsules []TaskCapsule) []contract.Message {
	if len(capsules) == 0 {
		return nil
	}
	data, _ := json.Marshal(capsules)
	return []contract.Message{{Role: contract.RoleUser, Content: "[relevant prior task capsules]\n" + string(data)}}
}
