package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Cross-resume cache resilience (Ultimate Polish Part C). The incident forensics
// showed a resumed session's first request cold-misses when DeepSeek's implicit
// server cache expired during downtime — a real 0% hit on a byte-identical prefix —
// and that the client recorded nothing about it. These helpers make a resume cold
// start VISIBLE (C6), ATTRIBUTABLE (C3), and CHEAPER (C7).

const (
	// coldNoticeMinTokens is the prompt-token floor below which a cold miss is not
	// worth a notice (a small fresh request is unavoidably cold).
	coldNoticeMinTokens = 8_000
	// staleResumeAfter / staleResumeMinTokens gate the free prune-first: a session
	// idle longer than this with a history this large almost certainly lost its
	// server-side cache, so shrinking history before the first request costs nothing
	// extra and cuts the uncached re-read (C7).
	staleResumeAfter     = 6 * time.Hour
	staleResumeMinTokens = 20_000
)

// flagColdStartIfNeeded records (under taskMu, from recordMainUsage) that a RESUMED
// session's request cold-missed a large prefix, so the caller can surface one honest
// notice. previous != nil means a prior main request existed (a resume), which
// distinguishes an avoidable resume cold read from an unavoidable fresh first request.
func (e *Engine) flagColdStartIfNeeded(previous *contract.UsageRecord, usage contract.Usage) {
	if previous == nil || usage.CacheReadTokens == nil || *usage.CacheReadTokens != 0 {
		return
	}
	if !usage.PromptTokensAvailable && usage.PromptTokens == 0 {
		return
	}
	if usage.PromptTokens < coldNoticeMinTokens {
		return
	}
	e.coldStartPending = true
	e.coldStartTokens = usage.PromptTokens
}

// takeColdStartNotice reads and clears the pending cold-start flag, returning the
// billed tokens and the best-known cause. Called at the request-loop call site,
// OUTSIDE taskMu, so the notice/telemetry callbacks run off the usage lock.
func (e *Engine) takeColdStartNotice() (tokens int, cause string, ok bool) {
	e.taskMu.Lock()
	pending, billed, cause := e.coldStartPending, e.coldStartTokens, e.lastResumeCause
	e.coldStartPending, e.coldStartTokens, e.lastResumeCause = false, 0, ""
	e.taskMu.Unlock()
	if !pending {
		return 0, "", false
	}
	if cause == "" {
		cause = "the provider cache expired while idle, or a config change since the last run"
	}
	return billed, cause, true
}

// emitColdStartNoticeIfPending surfaces one honest cold-start notice + telemetry for
// the just-recorded main request. Safe to call with nil callbacks.
func (e *Engine) emitColdStartNoticeIfPending(ctx context.Context) {
	tokens, cause, ok := e.takeColdStartNotice()
	if !ok {
		return
	}
	e.callbacks.EmitNotice(fmt.Sprintf("Cache restarted cold — ~%s re-read uncached this turn (%s). Normal on a resumed session; the next turns reuse the warm cache.", contract.HumanTokens(tokens), cause))
	e.recordHarnessEvent(ctx, contract.HarnessProvider, "cache-cold-start", cause)
}

// checkResumeDrift, on the FIRST request of a resumed session, compares the
// would-be prefix against the prior session's persisted shape and attributes any
// system/tools/model change — fixing the DC1/DC2/DC3 gap where skills/MCP/model
// drift used to cold-start with zero ledger trace. It sets lastResumeCause so C6's
// notice can name the reason. History is NOT compared here: it legitimately grows,
// and its replay determinism is guarded separately by the per-turn lastShape guard.
// Runs at most once per engine (priorSessionShape is consumed).
func (e *Engine) checkResumeDrift(ctx context.Context, shape PrefixShape) {
	if e.priorSessionShape == nil {
		return
	}
	prior := *e.priorSessionShape
	e.priorSessionShape = nil
	var changed []string
	if prior.SystemHash != shape.SystemHash {
		changed = append(changed, "the system prompt")
		_ = e.recordInvalidation(ctx, contract.InvalidationEvent{
			Cause: contract.InvalidationPromptRebuild, Trigger: contract.InvalidationConfigChange,
			Scope: "system prompt changed since the last session (workspace skills, model, or project context)", RequestSeq: e.nextRequestSeq(),
		})
	}
	if prior.ToolsHash != shape.ToolsHash {
		changed = append(changed, "the tool set")
		_ = e.recordInvalidation(ctx, contract.InvalidationEvent{
			Cause: contract.InvalidationToolsetChange, Trigger: contract.InvalidationBoundary,
			Scope: "tool set changed since the last session (MCP servers)", RequestSeq: e.nextRequestSeq(),
		})
	}
	if prior.ModelID != shape.ModelID && len(changed) == 0 {
		changed = append(changed, "the model")
	}
	e.taskMu.Lock()
	if len(changed) > 0 {
		e.lastResumeCause = strings.Join(changed, " and ") + " changed since the last run"
	} else {
		e.lastResumeCause = "the provider cache expired while the session was idle"
	}
	e.taskMu.Unlock()
}

// lastRequestTime returns the timestamp of the newest usage record (the last
// request of the prior session, once restored on resume), and whether one exists.
func (e *Engine) lastRequestTime() (time.Time, bool) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	for i := len(e.usageRecords) - 1; i >= 0; i-- {
		if !e.usageRecords[i].At.IsZero() {
			return e.usageRecords[i].At, true
		}
	}
	return time.Time{}, false
}

// staleResumePruneIfNeeded runs the FREE prune (fold/trim, no model call) once at
// the start of a resumed session that has been idle long enough that the provider's
// implicit cache has almost certainly expired — so shrinking history before the
// first request costs no cache and directly cuts the uncached re-read (C7, the
// incident's cost fix). Conservative gates (idle > staleResumeAfter AND history
// large AND yield ≥10%) avoid busting a cache that might still be warm.
func (e *Engine) staleResumePruneIfNeeded(ctx context.Context, profile EffortProfile) error {
	if e.resumePruneDone {
		return nil
	}
	e.resumePruneDone = true
	last, ok := e.lastRequestTime()
	if !ok || time.Since(last) < staleResumeAfter {
		return nil
	}
	pressure := e.contextPressure()
	if pressure.Tokens < staleResumeMinTokens {
		return nil
	}
	// Skip a trivial reclaim (<10% of current history) — not worth folding detail.
	if e.history.EstimateMaintainYield(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4) < pressure.Tokens/10 {
		return nil
	}
	result := e.history.Maintain(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4)
	if !result.Changed {
		return nil
	}
	cause := contract.InvalidationTrim
	parts := make([]string, 0, 2)
	if result.Folded {
		cause = contract.InvalidationFold
		parts = append(parts, fmt.Sprintf("stale-resume prune: folded completed-task context, reduced ~%d token(s)", result.FoldedTokens))
	}
	if result.TrimmedTools > 0 {
		parts = append(parts, fmt.Sprintf("trimmed %d aged tool result(s)", result.TrimmedTools))
	}
	return e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: cause, Trigger: contract.InvalidationBoundary, Scope: strings.Join(parts, "; "), RequestSeq: e.nextRequestSeq(),
	})
}

// persistPrefixShapeOnce writes this session's stable prefix shape to the sidecar
// once, after the first successful request, so the NEXT resume has a baseline to
// compare against. It re-arms whenever the model switches (SwitchModel resets the
// flag) so the sidecar always reflects the current stable shape.
func (e *Engine) persistPrefixShapeOnce(ctx context.Context, shape PrefixShape) {
	if e.prefixShapeSaved || e.persistence.WritePrefixShape == nil {
		return
	}
	e.prefixShapeSaved = true
	_ = e.persistence.WritePrefixShape(ctx, contract.PrefixShapeSnapshot{
		Version:    contract.PrefixShapeSnapshotVersion,
		SystemHash: shape.SystemHash,
		ToolsHash:  shape.ToolsHash,
		ModelID:    shape.ModelID,
	})
}
