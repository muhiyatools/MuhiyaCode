package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CapsuleValidity func(TaskCapsule) (valid bool, staleOperational bool, reason string)

type RetrievalInput struct {
	Goal, WorkspaceID string
	Paths, Symbols    []string
	TokenBudget       int
	MaxItems          int
	SigningKey        []byte
	Validity          CapsuleValidity
}

// CapsuleWorkspaceValidity invalidates operational file facts after an
// external mutation. Historical decisions remain selectable but are clearly
// separated by the retrieval policy from stale operational instructions.
func CapsuleWorkspaceValidity(workspaceRoot string) CapsuleValidity {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return func(TaskCapsule) (bool, bool, string) { return false, false, "workspace_unavailable" }
	}
	return func(capsule TaskCapsule) (bool, bool, string) {
		stale := false
		for _, delta := range capsule.ChangedFiles {
			if delta.AfterFingerprint == "" {
				continue
			}
			target, resolveErr := filepath.Abs(filepath.Join(root, filepath.FromSlash(delta.Path)))
			if resolveErr != nil || target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
				return false, false, "path_outside_workspace"
			}
			content, readErr := os.ReadFile(target)
			if readErr != nil {
				stale = true
				continue
			}
			hash := sha256.Sum256(content)
			if hex.EncodeToString(hash[:]) != delta.AfterFingerprint {
				stale = true
			}
		}
		return true, stale, ""
	}
}

type RetrievalCandidate struct {
	Capsule            TaskCapsule
	EstimatedTokens    int
	LexicalScore       float64
	PathSymbolScore    float64
	DependencyScore    float64
	RecencyScore       float64
	CombinedScore      float64
	SimilaritySelected float64
	Selected           bool
	ExclusionReason    string
}

type CapsuleIndex struct {
	Capsules []TaskCapsule
}

func BuildCapsuleIndex(capsules []TaskCapsule) CapsuleIndex {
	copy := append([]TaskCapsule(nil), capsules...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].ID < copy[j].ID })
	return CapsuleIndex{Capsules: copy}
}

func (index CapsuleIndex) Retrieve(input RetrievalInput) ([]TaskCapsule, []RetrievalCandidate) {
	if input.TokenBudget <= 0 {
		input.TokenBudget = 1_200
	}
	if input.MaxItems <= 0 {
		input.MaxItems = 6
	}
	queryTerms := tokenizeTerms(input.Goal)
	queryPaths := normalizePaths(append(extractPaths(input.Goal), input.Paths...))
	querySymbols := sortedUnique(input.Symbols, 128)
	candidates := make([]RetrievalCandidate, 0, len(index.Capsules))
	for _, capsule := range index.Capsules {
		candidate := RetrievalCandidate{Capsule: capsule, EstimatedTokens: capsule.EstimatedTokens()}
		switch {
		case capsule.WorkspaceID != input.WorkspaceID:
			candidate.ExclusionReason = "workspace_mismatch"
		case !capsule.Verify(input.SigningKey):
			candidate.ExclusionReason = "signature_invalid"
		default:
			if input.Validity != nil {
				valid, stale, reason := input.Validity(capsule)
				if !valid {
					candidate.ExclusionReason = reason
				} else if stale && !hasHistoricalDecision(capsule) {
					candidate.ExclusionReason = "stale_operational"
				}
			}
		}
		candidate.LexicalScore = termOverlap(queryTerms, sortedUnique(append(append(tokenizeTerms(capsule.Goal), capsule.Terms...), tokenizeTerms(capsule.OutcomeDetail)...), 512))
		candidate.PathSymbolScore = maxFloat(termOverlap(queryPaths, normalizePaths(capsule.Paths)), termOverlap(querySymbols, capsule.Symbols))
		candidate.DependencyScore = dependencyScore(input.Goal, capsule.DependsOnCapsules)
		candidate.CombinedScore = 0.45*candidate.LexicalScore + 0.45*candidate.PathSymbolScore + 0.10*candidate.DependencyScore
		if candidate.CombinedScore == 0 && candidate.ExclusionReason == "" {
			candidate.ExclusionReason = "no_relevance"
		}
		candidates = append(candidates, candidate)
	}
	selected, used := []TaskCapsule{}, 0
	for len(selected) < input.MaxItems {
		best := -1
		bestMMR := -1.0
		for i := range candidates {
			candidate := &candidates[i]
			if candidate.Selected || candidate.ExclusionReason != "" || used+candidate.EstimatedTokens > input.TokenBudget {
				continue
			}
			candidate.SimilaritySelected = maxCapsuleSimilarity(candidate.Capsule, selected)
			mmr := 0.75*candidate.CombinedScore - 0.25*candidate.SimilaritySelected
			if mmr > bestMMR || mmr == bestMMR && (best < 0 || candidate.Capsule.ID < candidates[best].Capsule.ID) {
				best, bestMMR = i, mmr
			}
		}
		if best < 0 {
			break
		}
		candidates[best].Selected = true
		selected = append(selected, candidates[best].Capsule)
		used += candidates[best].EstimatedTokens
	}
	for i := range candidates {
		if !candidates[i].Selected && candidates[i].ExclusionReason == "" {
			if used+candidates[i].EstimatedTokens > input.TokenBudget {
				candidates[i].ExclusionReason = "token_budget"
			} else {
				candidates[i].ExclusionReason = "mmr_or_item_cap"
			}
		}
	}
	// Highest-value content is placed closest to the current task tail.
	sort.SliceStable(selected, func(i, j int) bool {
		return capsuleScore(selected[i], candidates) < capsuleScore(selected[j], candidates)
	})
	return selected, candidates
}

func termOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, value := range a {
		set[strings.ToLower(value)] = true
	}
	common := 0
	for _, value := range b {
		if set[strings.ToLower(value)] {
			common++
		}
	}
	return float64(common) / float64(max(len(a), len(b)))
}

func maxCapsuleSimilarity(c TaskCapsule, selected []TaskCapsule) float64 {
	terms := sortedUnique(append(append(c.Terms, c.Paths...), c.Symbols...), 512)
	maximum := 0.0
	for _, other := range selected {
		score := termOverlap(terms, sortedUnique(append(append(other.Terms, other.Paths...), other.Symbols...), 512))
		if score > maximum {
			maximum = score
		}
	}
	return maximum
}

func hasHistoricalDecision(c TaskCapsule) bool {
	for _, decision := range c.Decisions {
		if decision.Historical {
			return true
		}
	}
	return false
}

func dependencyScore(goal string, dependencies []string) float64 {
	for _, dependency := range dependencies {
		if dependency != "" && strings.Contains(strings.ToLower(goal), strings.ToLower(dependency)) {
			return 1
		}
	}
	return 0
}

func capsuleScore(c TaskCapsule, candidates []RetrievalCandidate) float64 {
	for _, candidate := range candidates {
		if candidate.Capsule.ID == c.ID {
			return candidate.CombinedScore
		}
	}
	return 0
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
