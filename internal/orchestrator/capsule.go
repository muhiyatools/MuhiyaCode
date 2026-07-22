package orchestrator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const TaskCapsuleSchemaVersion = 1

type CapsuleOutcome string

const (
	CapsuleCompleted CapsuleOutcome = "completed"
	CapsulePartial   CapsuleOutcome = "partial"
	CapsuleFailed    CapsuleOutcome = "failed"
	CapsuleAborted   CapsuleOutcome = "aborted"
)

type FileDeltaRef struct {
	Path, BeforeFingerprint, AfterFingerprint string
	LinesAdded, LinesRemoved                  int
}

type CheckResultRef struct {
	Name, Status, ArtifactID string
}

type DecisionFact struct {
	Key, Value string
	Historical bool
}

type RiskOrBlocker struct {
	Code, Detail string
}

type TaskCapsule struct {
	SchemaVersion     int
	ID, Signature     string
	EpochID           string
	SessionID         string
	WorkspaceID       string
	Goal              string
	Outcome           CapsuleOutcome
	OutcomeDetail     string
	ChangedFiles      []FileDeltaRef
	Checks            []CheckResultRef
	Decisions         []DecisionFact
	Unresolved        []RiskOrBlocker
	Symbols           []string
	Paths             []string
	Terms             []string
	EvidenceRefs      []string
	DependsOnCapsules []string
	CreatedAt         time.Time
}

type CapsuleBuildInput struct {
	EpochID, SessionID, WorkspaceID, Goal, OutcomeDetail string
	Outcome                                              CapsuleOutcome
	ChangedFiles                                         []FileDeltaRef
	Checks                                               []CheckResultRef
	Decisions                                            []DecisionFact
	Unresolved                                           []RiskOrBlocker
	Symbols, Paths, Terms, EvidenceRefs, DependsOn       []string
	CreatedAt                                            time.Time
}

func BuildTaskCapsule(input CapsuleBuildInput, signingKey []byte, evidenceExists func(string) bool) (TaskCapsule, error) {
	if strings.TrimSpace(input.EpochID) == "" || strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.WorkspaceID) == "" {
		return TaskCapsule{}, errors.New("epoch, session, and workspace ownership are required")
	}
	if input.Outcome == "" {
		return TaskCapsule{}, errors.New("capsule outcome is required")
	}
	evidence := sortedUnique(input.EvidenceRefs, 64)
	if evidenceExists != nil {
		for _, ref := range evidence {
			if !evidenceExists(ref) {
				return TaskCapsule{}, fmt.Errorf("required evidence is missing: %s", ref)
			}
		}
	}
	if input.CreatedAt.IsZero() {
		input.CreatedAt = time.Now().UTC()
	}
	capsule := TaskCapsule{
		SchemaVersion: TaskCapsuleSchemaVersion, EpochID: strings.TrimSpace(input.EpochID),
		SessionID: strings.TrimSpace(input.SessionID), WorkspaceID: strings.TrimSpace(input.WorkspaceID),
		Goal: boundText(input.Goal, 2_000), Outcome: input.Outcome, OutcomeDetail: boundText(input.OutcomeDetail, 2_000),
		ChangedFiles: canonicalFileDeltas(input.ChangedFiles), Checks: canonicalChecks(input.Checks),
		Decisions: canonicalDecisions(input.Decisions), Unresolved: canonicalBlockers(input.Unresolved),
		Symbols: sortedUnique(input.Symbols, 128), Paths: normalizePaths(input.Paths),
		Terms: sortedUnique(input.Terms, 256), EvidenceRefs: evidence,
		DependsOnCapsules: sortedUnique(input.DependsOn, 64), CreatedAt: input.CreatedAt.UTC(),
	}
	canonical, err := capsule.canonicalBytes()
	if err != nil {
		return TaskCapsule{}, err
	}
	id := sha256.Sum256(canonical)
	capsule.ID = hex.EncodeToString(id[:])
	capsule.Signature = signCapsule(canonical, signingKey)
	return capsule, nil
}

func (c TaskCapsule) Verify(signingKey []byte) bool {
	canonical, err := c.canonicalBytes()
	if err != nil {
		return false
	}
	id := sha256.Sum256(canonical)
	if !hmac.Equal([]byte(c.ID), []byte(hex.EncodeToString(id[:]))) {
		return false
	}
	return hmac.Equal([]byte(c.Signature), []byte(signCapsule(canonical, signingKey)))
}

func (c TaskCapsule) EstimatedTokens() int {
	data, _ := c.canonicalBytes()
	return EstimateTokens(string(data))
}

func (c TaskCapsule) canonicalBytes() ([]byte, error) {
	copy := c
	copy.ID, copy.Signature = "", ""
	return json.Marshal(copy)
}

func signCapsule(canonical, key []byte) string {
	if len(key) == 0 {
		sum := sha256.Sum256(canonical)
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

func canonicalFileDeltas(values []FileDeltaRef) []FileDeltaRef {
	values = append([]FileDeltaRef(nil), values...)
	for i := range values {
		values[i].Path = filepathSlash(values[i].Path)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Path < values[j].Path })
	if len(values) > 128 {
		values = values[:128]
	}
	return values
}

func canonicalChecks(values []CheckResultRef) []CheckResultRef {
	values = append([]CheckResultRef(nil), values...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Name != values[j].Name {
			return values[i].Name < values[j].Name
		}
		return values[i].ArtifactID < values[j].ArtifactID
	})
	if len(values) > 64 {
		values = values[:64]
	}
	return values
}

func canonicalDecisions(values []DecisionFact) []DecisionFact {
	values = append([]DecisionFact(nil), values...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Key != values[j].Key {
			return values[i].Key < values[j].Key
		}
		return values[i].Value < values[j].Value
	})
	if len(values) > 64 {
		values = values[:64]
	}
	return values
}

func canonicalBlockers(values []RiskOrBlocker) []RiskOrBlocker {
	values = append([]RiskOrBlocker(nil), values...)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Code != values[j].Code {
			return values[i].Code < values[j].Code
		}
		return values[i].Detail < values[j].Detail
	})
	if len(values) > 64 {
		values = values[:64]
	}
	return values
}
