package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type ContextStability string

const (
	StabilityGlobal  ContextStability = "global"
	StabilitySession ContextStability = "session"
	StabilityEpoch   ContextStability = "epoch"
	StabilityRequest ContextStability = "request"
)

type ContextSegmentKind string

const (
	SegmentCoreTools          ContextSegmentKind = "core_tools"
	SegmentDeferredDescriptor ContextSegmentKind = "deferred_descriptor"
	SegmentSystem             ContextSegmentKind = "system"
	SegmentProjectRoot        ContextSegmentKind = "project_root"
	SegmentCurrentGoal        ContextSegmentKind = "current_goal"
	SegmentCurrentPlan        ContextSegmentKind = "current_plan"
	SegmentPriorCapsule       ContextSegmentKind = "prior_capsule"
	SegmentAssistantReasoning ContextSegmentKind = "assistant_reasoning"
	SegmentObservation        ContextSegmentKind = "observation"
	SegmentGovernor           ContextSegmentKind = "governor"
	SegmentUser               ContextSegmentKind = "user"
	SegmentSummary            ContextSegmentKind = "summary"
)

type ContextDropReason string

const (
	DropDuplicate  ContextDropReason = "duplicate"
	DropStale      ContextDropReason = "stale"
	DropIrrelevant ContextDropReason = "irrelevant"
	DropBudget     ContextDropReason = "budget"
	DropNewEpoch   ContextDropReason = "new_epoch"
)

type ContextSegment struct {
	ID              string
	Kind            ContextSegmentKind
	SourceRef       string
	Stability       ContextStability
	ByteStart       *int
	ByteEnd         *int
	Bytes           int
	EstimatedTokens int
	Fingerprint     string
	Required        bool
	RelevanceScore  *float64
	DropReason      ContextDropReason
}

type ContextManifest struct {
	RequestSeq           uint64
	WireHash             string
	StablePrefixHash     string
	MessagePrefixHash    string
	Segments             []ContextSegment
	EstimatedTokens      int
	ProviderPromptTokens *int
	ResidualTokens       *int
	MeasurementKind      contract.MeasurementKind
}

func NewContextSegment(id string, kind ContextSegmentKind, sourceRef string, stability ContextStability, payload []byte, estimatedTokens int, required bool) ContextSegment {
	return ContextSegment{
		ID: id, Kind: kind, SourceRef: sourceRef, Stability: stability,
		Bytes: len(payload), EstimatedTokens: max(0, estimatedTokens),
		Fingerprint: fingerprintBytes(payload), Required: required,
	}
}

func (manifest *ContextManifest) Add(segment ContextSegment) error {
	if manifest == nil {
		return fmt.Errorf("nil context manifest")
	}
	if strings.TrimSpace(segment.ID) == "" || segment.Kind == "" || segment.Stability == "" || segment.Bytes < 0 || segment.EstimatedTokens < 0 || segment.Fingerprint == "" {
		return fmt.Errorf("invalid context segment %+v", segment)
	}
	for _, existing := range manifest.Segments {
		if existing.ID == segment.ID {
			return fmt.Errorf("duplicate context segment id %q", segment.ID)
		}
	}
	manifest.Segments = append(manifest.Segments, segment)
	manifest.EstimatedTokens = saturatingIntAdd(manifest.EstimatedTokens, segment.EstimatedTokens)
	return nil
}

func (manifest *ContextManifest) Reconcile(providerPromptTokens *int) {
	if manifest == nil {
		return
	}
	manifest.ProviderPromptTokens = cloneIntLocal(providerPromptTokens)
	manifest.ResidualTokens = nil
	if providerPromptTokens == nil {
		manifest.MeasurementKind = contract.MeasurementCalibratedEstimate
		return
	}
	residual := *providerPromptTokens - manifest.EstimatedTokens
	manifest.ResidualTokens = &residual
	manifest.MeasurementKind = contract.MeasurementProviderExact
}

func (manifest ContextManifest) LogicalHash() string {
	type hashSegment struct {
		ID, Kind, Source, Stability, Fingerprint string
		Bytes, EstimatedTokens                   int
		Required                                 bool
	}
	encoded := make([]hashSegment, 0, len(manifest.Segments))
	for _, segment := range manifest.Segments {
		encoded = append(encoded, hashSegment{
			ID: segment.ID, Kind: string(segment.Kind), Source: segment.SourceRef,
			Stability: string(segment.Stability), Fingerprint: segment.Fingerprint,
			Bytes: segment.Bytes, EstimatedTokens: segment.EstimatedTokens, Required: segment.Required,
		})
	}
	raw, _ := json.Marshal(encoded)
	return fingerprintBytes(raw)
}

func (manifest ContextManifest) Clone() ContextManifest {
	clone := manifest
	clone.Segments = append([]ContextSegment(nil), manifest.Segments...)
	for index := range clone.Segments {
		clone.Segments[index].ByteStart = cloneIntLocal(manifest.Segments[index].ByteStart)
		clone.Segments[index].ByteEnd = cloneIntLocal(manifest.Segments[index].ByteEnd)
		if manifest.Segments[index].RelevanceScore != nil {
			value := *manifest.Segments[index].RelevanceScore
			clone.Segments[index].RelevanceScore = &value
		}
	}
	clone.ProviderPromptTokens = cloneIntLocal(manifest.ProviderPromptTokens)
	clone.ResidualTokens = cloneIntLocal(manifest.ResidualTokens)
	return clone
}

// DebugRender deliberately excludes segment content and raw source values. It
// is safe for benchmark/debug logs because only bounded identities, sizes, and
// fingerprints are rendered.
func (manifest ContextManifest) DebugRender() string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "request=%d logical=%s wire=%s estimated=%d", manifest.RequestSeq, manifest.LogicalHash(), manifest.WireHash, manifest.EstimatedTokens)
	for _, segment := range manifest.Segments {
		fmt.Fprintf(&builder, "\nsegment id=%s kind=%s stability=%s bytes=%d estimated=%d required=%t fingerprint=%s", sanitizeDebugID(segment.ID), segment.Kind, segment.Stability, segment.Bytes, segment.EstimatedTokens, segment.Required, segment.Fingerprint)
	}
	return builder.String()
}

func fingerprintBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func cloneIntLocal(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func saturatingIntAdd(left, right int) int {
	if right > 0 && left > int(^uint(0)>>1)-right {
		return int(^uint(0) >> 1)
	}
	return left + right
}

func sanitizeDebugID(value string) string {
	value = strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-/", char) {
			return char
		}
		return '_'
	}, value)
	if len(value) > 96 {
		return value[:96]
	}
	return value
}
