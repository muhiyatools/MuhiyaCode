package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
)

// Feature 012 linker (contracts/context-linking.md CL-1): every subagent
// dispatch gets a recorded link decision. Continuation-first: extend the
// predecessor's conversation stream so the provider serves the shared prefix
// from cache; fall back to a digest-seeded fresh start when any criterion
// fails; record the first failing criterion as the reason either way.

const (
	linkContinued    = "continued"
	linkDigestSeeded = "digest-seeded"
	linkFresh        = "fresh"

	linkFormSameKind    = "same-kind"
	linkFormReviewChain = "review-after-implement"
)

// staleDeclineFraction is the majority threshold (Clarification Q5): continue
// and re-read while at most half the predecessor's touched set changed since
// it ran; decline beyond that.
const staleDeclineFraction = 0.5

// continuationHeadroom reserves response + growth room inside the model
// window when judging whether a continuation fits (R-D10).
const continuationHeadroom = 0.20

// linkDecision is the dispatch-time outcome; it becomes a contract.LinkOutcome
// on the task ledger once the run exists.
type linkDecision struct {
	Decision    string
	Form        string
	Reason      string
	Predecessor *SubagentContextRecord
	// Reread is the staleness-directed re-read list (changed touched-set
	// members) named in the successor handoff and exempted from the
	// duplicate-read block for the run.
	Reread []string
	// Inherited counts touched-set members carried current.
	Inherited int
	// CarryForward is the digest-fallback context: predecessor result digest +
	// touched list with changed markers (CL-3). Empty for fresh starts.
	CarryForward string
}

// contextLinkingEnabled is the feature kill switch (FR-017): "off" restores
// pre-012 dispatch behavior exactly.
func (e *Engine) contextLinkingEnabled() bool {
	return e.settings.ContextLinking != "off"
}

// decideLink evaluates CL-1 in order and returns the recorded decision.
func (e *Engine) decideLink(input subagentInput, spec subagentSpec, modelID string, pin string) linkDecision {
	if !e.contextLinkingEnabled() {
		return linkDecision{Decision: linkFresh, Reason: "disabled"}
	}
	// 1. Candidate: same-task phase lineage first, then the session's most
	// recent linkable chain passing the relatedness predicate (R-D9).
	e.taskMu.Lock()
	taskSeq := e.taskSeq
	e.taskMu.Unlock()
	sameTask := true
	candidate := e.latestLinkableRecord(func(r *SubagentContextRecord) bool { return r.TaskLineage == taskSeq })
	if candidate == nil {
		sameTask = false
		candidate = e.latestLinkableRecord(func(r *SubagentContextRecord) bool { return r.TaskLineage != taskSeq })
	}
	if candidate == nil {
		return linkDecision{Decision: linkFresh, Reason: "no-candidate"}
	}
	fallback := func(reason string) linkDecision {
		return linkDecision{Decision: linkDigestSeeded, Reason: reason, Predecessor: candidate, CarryForward: digestCarryForward(candidate, e.session.WorkspacePath)}
	}
	if !sameTask && !relatedFollowUp(input.Task, candidate) {
		return linkDecision{Decision: linkFresh, Reason: "relatedness-miss"}
	}
	// 2. Kind pair (Clarification Q3): same-kind chains, plus review
	// continuing the implementer whose work it reviews.
	form := ""
	switch {
	case input.Agent == candidate.Kind:
		form = linkFormSameKind
	case input.Agent == "review" && candidate.Kind == "general":
		form = linkFormReviewChain
	default:
		return fallback("kind-pair-unsupported")
	}
	// 3. Terminal shape (R-D4) is part of Linkable; latestLinkableRecord
	// already filtered, so reaching here means clean-done with complete
	// pairings.
	// 4. Stream identity intact (R-F11): same model still configured for the
	// predecessor's kind, and the pin string derivable identically.
	continuationModel := e.subagentModelID()
	if continuationModel != candidate.ModelID {
		return fallback("model-changed")
	}
	if expected := e.session.ID + ":sub:" + candidate.Kind; expected != candidate.Pin {
		return fallback("pin-mismatch")
	}
	_ = pin
	// 5. Staleness: changed fraction of the touched set vs END-OF-RUN
	// fingerprints (R-D5 — the predecessor's own edits are never stale).
	changed, total := staleness(candidate, e.session.WorkspacePath)
	if total > 0 && float64(len(changed))/float64(total) > staleDeclineFraction {
		return fallback(fmt.Sprintf("stale:%d%%", int(100*float64(len(changed))/float64(total))))
	}
	// 6. Window fit (R-D10): predecessor final prompt + handoff estimate +
	// headroom within the model's context limit.
	window := e.modelContextLimit(continuationModel)
	if window > 0 {
		estimate := candidate.FinalPromptTokens + len(input.Task)/3 + 2048
		if float64(estimate) > float64(window)*(1-continuationHeadroom) {
			return fallback("window-overflow")
		}
	}
	// 7. Provider support: continuation requires a family whose caching
	// rewards byte-identical replay (R-F19/R-F20); others degrade to digest.
	if profile := gateway.ResolveModelProfile(continuationModel); profile.ContinuationLinking != gateway.ContinuationSupported {
		return fallback("provider:" + profile.Family)
	}
	return linkDecision{
		Decision:    linkContinued,
		Form:        form,
		Reason:      "eligible",
		Predecessor: candidate,
		Reread:      changed,
		Inherited:   total - len(changed),
	}
}

// staleness compares the candidate's touched set against current disk state.
// Returns the changed paths (sorted, bounded) and the touched-set size.
func staleness(record *SubagentContextRecord, workspace string) ([]string, int) {
	paths := record.Touched.touchedPaths()
	var changed []string
	fingerprintFor := func(path string) (FileFingerprint, bool) {
		if fp, ok := record.Touched.Writes[path]; ok {
			return fp, true
		}
		fp, ok := record.Touched.Reads[path]
		return fp, ok
	}
	for _, path := range paths {
		recorded, ok := fingerprintFor(path)
		if !ok {
			continue
		}
		info, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path)))
		current := FileFingerprint{}
		if err == nil {
			current = FileFingerprint{MTimeMS: float64(info.ModTime().UnixNano()) / 1e6, Size: info.Size()}
		}
		if current.Size != recorded.Size || absFloat(current.MTimeMS-recorded.MTimeMS) > 0.01 {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	if len(changed) > 20 {
		changed = changed[:20]
	}
	return changed, len(paths)
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// relatedFollowUp is the R-D9 predicate for cross-task linking: at least one
// ≥4-char term of the new task (knowledge.go scopeTerms — the BriefingForScope
// convention) matches a touched path of the candidate. The paraphrase weakness
// is accepted — a miss costs one warm-prefix opportunity, never correctness.
func relatedFollowUp(task string, candidate *SubagentContextRecord) bool {
	terms := scopeTerms(task)
	if len(terms) == 0 {
		return false
	}
	for _, path := range candidate.Touched.touchedPaths() {
		lower := strings.ToLower(path)
		for term := range terms {
			if strings.Contains(lower, term) {
				return true
			}
		}
	}
	return false
}

// digestCarryForward assembles the CL-3 fallback context: the predecessor's
// result digest plus its touched files with changed markers. Bounded like the
// knowledge briefing it rides beside.
func digestCarryForward(record *SubagentContextRecord, workspace string) string {
	var b strings.Builder
	b.WriteString("Predecessor ")
	b.WriteString(record.Kind)
	b.WriteString(" result (digest): ")
	b.WriteString(contract.Digest(record.Result, 700))
	changed, _ := staleness(record, workspace)
	changedSet := make(map[string]bool, len(changed))
	for _, path := range changed {
		changedSet[path] = true
	}
	paths := record.Touched.touchedPaths()
	sort.Strings(paths)
	if len(paths) > 0 {
		b.WriteString("\nFiles it touched:")
		for index, path := range paths {
			if index >= 12 {
				b.WriteString(fmt.Sprintf(" …+%d more", len(paths)-index))
				break
			}
			marker := ""
			if changedSet[path] {
				marker = " (changed since — re-read)"
			}
			b.WriteString(" " + path + marker + ";")
		}
	}
	return contract.TruncateEllipsis(b.String(), 1400)
}

// subagentModelID resolves the model a subagent dispatch will use.
func (e *Engine) subagentModelID() string {
	modelID := e.settings.Provider.SubagentModelID
	if modelID == "" {
		modelID = e.settings.Provider.ActiveModelID
	}
	return modelID
}

// modelContextLimit returns the configured context window for a model, 0 when
// unknown (unknown never blocks — the provider will enforce its own limit).
func (e *Engine) modelContextLimit(modelID string) int {
	for _, model := range e.settings.Provider.Models {
		if model.ID == modelID {
			return model.ContextLimit
		}
	}
	return 0
}

// pairedCacheShare computes read/(read+miss) from provider-reported paired
// cache fields; nil when either side is unreported (Constitution VI — shown
// as unavailable, never estimated).
func pairedCacheShare(usage contract.Usage) *float64 {
	if usage.CacheReadTokens == nil || usage.CacheMissTokens == nil {
		return nil
	}
	read, miss := float64(*usage.CacheReadTokens), float64(*usage.CacheMissTokens)
	if read+miss <= 0 {
		return nil
	}
	share := read / (read + miss)
	return &share
}

// recordLinkOutcome appends the dispatch's outcome to the task ledger.
func (e *Engine) recordLinkOutcome(outcome contract.LinkOutcome) {
	e.taskMu.Lock()
	e.taskLinks = append(e.taskLinks, outcome)
	e.taskMu.Unlock()
}

// linkNoticeLine renders the user-visible one-liner for a dispatch decision.
func linkNoticeLine(outcome contract.LinkOutcome) string {
	switch outcome.Decision {
	case linkContinued:
		share := "cache: unavailable"
		if outcome.CacheShare != nil {
			share = fmt.Sprintf("cache: %d%% reused", int(*outcome.CacheShare*100))
		}
		return fmt.Sprintf("link: continued (%s) — %s", outcome.Form, share)
	case linkDigestSeeded:
		return "link: digest-seeded — " + outcome.Reason
	default:
		return ""
	}
}
