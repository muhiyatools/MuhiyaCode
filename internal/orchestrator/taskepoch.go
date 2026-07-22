package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const TaskEpochSchemaVersion = 1

type EpochState string

const (
	EpochOpen     EpochState = "open"
	EpochSettling EpochState = "settling"
	EpochSettled  EpochState = "settled"
	EpochFailed   EpochState = "failed"
	EpochAborted  EpochState = "aborted"
)

type CapsuleRef struct {
	ID              string `json:"id"`
	Reason          string `json:"reason,omitempty"`
	EstimatedTokens int    `json:"estimatedTokens,omitempty"`
}

type TaskEpoch struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	ID              string                  `json:"id"`
	SessionID       string                  `json:"sessionId"`
	Ordinal         uint64                  `json:"ordinal"`
	ModelID         string                  `json:"modelId"`
	UpstreamPin     string                  `json:"upstreamPin"`
	Goal            string                  `json:"goal"`
	Class           TaskClass               `json:"class"`
	Risk            []string                `json:"risk,omitempty"`
	State           EpochState              `json:"state"`
	ParentEpochID   string                  `json:"parentEpochId,omitempty"`
	RelatedCapsules []CapsuleRef            `json:"relatedCapsules,omitempty"`
	StartedAt       time.Time               `json:"startedAt"`
	SettledAt       *time.Time              `json:"settledAt,omitempty"`
	ContextRevision uint64                  `json:"contextRevision"`
	PrefixRevision  uint64                  `json:"prefixRevision"`
	LastPhase       contract.ExecutionPhase `json:"lastPhase,omitempty"`
}

type NewTaskEpochInput struct {
	SessionID, ModelID, UpstreamPin, Goal, ParentEpochID string
	Ordinal, PrefixRevision                              uint64
	Class                                                TaskClass
	Risk                                                 []string
	RelatedCapsules                                      []CapsuleRef
	StartedAt                                            time.Time
}

func NewTaskEpoch(input NewTaskEpochInput) (TaskEpoch, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.UpstreamPin = strings.TrimSpace(input.UpstreamPin)
	if input.SessionID == "" || input.ModelID == "" || input.UpstreamPin == "" || input.Ordinal == 0 {
		return TaskEpoch{}, errors.New("session, model, upstream pin, and positive ordinal are required")
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now().UTC()
	}
	goal := boundText(input.Goal, 4_000)
	identity := fmt.Sprintf("%s\x00%d\x00%s", input.SessionID, input.Ordinal, input.StartedAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(identity))
	return TaskEpoch{
		SchemaVersion: TaskEpochSchemaVersion, ID: hex.EncodeToString(sum[:16]),
		SessionID: input.SessionID, Ordinal: input.Ordinal, ModelID: input.ModelID,
		UpstreamPin: input.UpstreamPin, Goal: goal, Class: input.Class,
		Risk: sortedUnique(input.Risk, 32), State: EpochOpen,
		ParentEpochID:   strings.TrimSpace(input.ParentEpochID),
		RelatedCapsules: canonicalCapsuleRefs(input.RelatedCapsules),
		StartedAt:       input.StartedAt.UTC(), PrefixRevision: input.PrefixRevision,
		LastPhase: contract.ExecutionPhaseOrient,
	}, nil
}

func LegacyTaskEpoch(sessionID, modelID, upstreamPin string) TaskEpoch {
	return TaskEpoch{SchemaVersion: TaskEpochSchemaVersion, ID: "legacy-0", SessionID: sessionID,
		ModelID: modelID, UpstreamPin: upstreamPin, State: EpochOpen, LastPhase: contract.ExecutionPhaseOrient}
}

func (e TaskEpoch) ValidateIdentity(modelID, upstreamPin string) error {
	if e.ModelID != modelID {
		return fmt.Errorf("task epoch model is immutable: %q != %q", e.ModelID, modelID)
	}
	if e.UpstreamPin != upstreamPin {
		return fmt.Errorf("task epoch upstream is immutable: %q != %q", e.UpstreamPin, upstreamPin)
	}
	return nil
}

func (e *TaskEpoch) Transition(next EpochState, at time.Time) error {
	allowed := map[EpochState]map[EpochState]bool{
		EpochOpen:     {EpochSettling: true, EpochFailed: true, EpochAborted: true},
		EpochSettling: {EpochSettled: true, EpochFailed: true},
	}
	if !allowed[e.State][next] {
		return fmt.Errorf("invalid task epoch transition %s -> %s", e.State, next)
	}
	e.State = next
	if next == EpochSettled {
		if at.IsZero() {
			at = time.Now().UTC()
		}
		at = at.UTC()
		e.SettledAt = &at
	}
	return nil
}

func (e *TaskEpoch) ReplaceContext() error {
	if e.State != EpochOpen {
		return fmt.Errorf("cannot replace context for %s epoch", e.State)
	}
	e.ContextRevision++
	return nil
}

func canonicalCapsuleRefs(refs []CapsuleRef) []CapsuleRef {
	byID := map[string]CapsuleRef{}
	for _, ref := range refs {
		ref.ID = strings.TrimSpace(ref.ID)
		if ref.ID != "" {
			byID[ref.ID] = ref
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sortStrings(ids)
	out := make([]CapsuleRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func boundText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit > 0 && len(value) > limit {
		value = value[:limit]
	}
	return value
}
