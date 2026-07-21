package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) Compact(ctx context.Context) (string, error) {
	if e.IsBusy() {
		return "", errors.New("cannot compact while a task is running")
	}
	pressure := e.contextPressure()
	before := e.history.EstimatedTokens()
	if err := e.compact(ctx, "manual"); err != nil {
		return "", err
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: contract.InvalidationUserCompact, Trigger: contract.InvalidationUserAction,
		Scope: "user requested /compact", Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return "", err
	}
	e.resetMaintenanceLatch()
	e.emitContext(0)
	return "Conversation compacted — " + describeFreed(before, e.history.EstimatedTokens(), e.contextLimit()), nil
}

// describeFreed reports how much context a compaction reclaimed, from the
// estimated history size before and after and the model's context limit.
func describeFreed(before, after, limit int) string {
	freed := max(0, before-after)
	if freed == 0 {
		return "no additional context could be freed."
	}
	percent := float64(freed) / float64(max(1, limit)) * 100
	return fmt.Sprintf("freed %s tokens (%.0f%% of the context window).", contract.FullTokens(freed), percent)
}

func (e *Engine) compact(ctx context.Context, reason string) error {
	messages := e.history.All()
	start := max(0, len(messages)-60)
	var lines []string
	for _, message := range messages[start:] {
		content := contract.Digest(message.Content, 500)
		lines = append(lines, fmt.Sprintf("- %s: %s", message.Role, content))
	}
	// T042: digests accumulate, so the summarizer covers ONLY the region being
	// folded — prior digests are preserved verbatim (via CompactTo accumulation)
	// and carried forward in the request prefix, never re-summarized.
	request := []contract.Message{{Role: contract.RoleSystem, Content: "Compress a coding-agent session under headings GOAL, STATE, FILES, DECISIONS, COMMANDS, PENDING. Preserve all durable facts and exact paths; use terse bullets."}, {Role: contract.RoleUser, Content: "Reason: " + reason + "\nDurable facts:\n" + strings.Join(e.knowledge.CompactionFacts(), "\n") + "\nConversation:\n" + strings.Join(lines, "\n")}}
	temperature := .1
	// Compaction shares the main stream's pin: it uses ActiveModelID, so it
	// belongs under the same gateway routing pin as the main loop (C1).
	// C3/T011: this is an intentionally COLD, one-shot isolated stream — a single
	// request built from a fresh system+user pair, never appended to. It is not
	// covered by the main-loop prefix-shape guard (there is no prior shape to
	// compare against and no settled prefix to preserve), and that is correct:
	// its cache miss is a deliberate, once-per-compaction cost.
	// T042: bound the summarizer to 90s and allow one retry on a non-timeout
	// failure; a timeout or a second failure falls through to the mechanical
	// fallback below so compaction always frees context and never loops.
	summaryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var response contract.ChatResponse
	var err error
	summaryStart := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		response, err = e.provider.Chat(summaryCtx, contract.ChatRequest{SessionID: e.session.ID + ":main", Messages: request, ModelID: e.settings.Provider.ActiveModelID, MaxTokens: 1600, Temperature: &temperature, Reasoning: contract.ReasoningLow, PinUpstream: e.upstreamPin()})
		if err == nil || summaryCtx.Err() != nil {
			break
		}
	}
	summary := ""
	if usageErr := e.recordUsageAndEmit(func() error {
		return e.recordAuxUsage(ctx, e.settings.Provider.ActiveModelID, ":aux", response.Usage, elapsedMS(summaryStart))
	}); usageErr != nil {
		return fmt.Errorf("persist compaction usage: %w", usageErr)
	}
	if err == nil {
		summary = strings.TrimSpace(response.Content)
	}
	if summary == "" {
		summary = strings.Join(lines, "\n")
	}
	// T042: pass just the digest (accumulated by CompactTo). The workspace path
	// already lives in the cache-stable system prompt, so repeating it per
	// accumulated digest would only bloat the summary.
	e.history.CompactTo(summary, 2)
	if e.persistence.AddEvent != nil {
		_ = e.persistence.AddEvent(ctx, "system", "compact_summary", e.redact(summary), "")
	}
	return nil
}

func (e *Engine) needsCompact(profile EffortProfile) bool {
	return e.contextPressure().Ratio >= profile.CompactThreshold
}

func (e *Engine) contextPressure() pressureSnapshot {
	e.taskMu.Lock()
	reported, available := e.latestPromptTokens, e.latestPromptAvailable
	e.taskMu.Unlock()
	input := e.history.PressureInput(reported, available)
	usable := max(8000, e.contextLimit()-outputReserveTokens)
	return pressureSnapshot{Tokens: input.Tokens, Estimated: input.Estimated, Ratio: float64(input.Tokens) / float64(usable)}
}

func (e *Engine) runMaintenanceBoundary(ctx context.Context, profile EffortProfile) (MaintenanceResult, error) {
	pressure := e.contextPressure()
	e.taskMu.Lock()
	if pressure.Ratio < maintenanceFloorRatio {
		// C4: do NOT reset the maintenance latch on transient pressure dips.
		// Oscillating 0.59 ↔ 0.61 used to refill the latch reset budget and
		// pay repeated full-prefix resets. The latch now flips only on a
		// genuine compaction (resetMaintenanceLatch) or session restart.
		latched := e.maintenanceLatched
		passes := e.maintenancePasses
		e.taskMu.Unlock()
		_ = passes
		if latched {
			return MaintenanceResult{}, nil
		}
		return MaintenanceResult{}, nil
	}
	latched := e.maintenanceLatched
	e.taskMu.Unlock()
	if latched {
		return MaintenanceResult{}, nil
	}

	// A1/T008: estimate the reclaimable yield BEFORE mutating history. Below the
	// minimum-yield floor (5% of the usable window) we skip entirely — no
	// Maintain call, so no settled bytes change, no invalidation event is owed,
	// and no latch slot is consumed. This closes the REV A1 trap: previously
	// Maintain ran first and a trim-only change below the hard-fold threshold
	// returned WITHOUT recording an event, so the next request tripped the
	// prefix-shape guard with an unexplained change and hard-failed the session.
	// Near the hard-fold threshold we always reclaim (compaction is imminent
	// anyway), matching Reasonix's force semantics.
	usable := max(8000, e.contextLimit()-outputReserveTokens)
	minYield := usable * maintenanceMinYieldPercent / 100
	if e.history.EstimateMaintainYield(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4) < minYield &&
		pressure.Ratio < maintenanceHardFoldRatio {
		return MaintenanceResult{}, nil
	}

	result := e.history.Maintain(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4)
	if !result.Changed {
		// Estimate said there was yield but a concurrent change consumed it;
		// nothing mutated, so nothing is owed. Any actual change below falls
		// through to a recorded invalidation — mutation and event are now
		// inseparable.
		return result, nil
	}
	cause := contract.InvalidationTrim
	parts := make([]string, 0, 2)
	if result.Folded {
		cause = contract.InvalidationFold
		parts = append(parts, fmt.Sprintf("folded completed-task context; reduced %d estimated token(s)", result.FoldedTokens))
	}
	if result.TrimmedTools > 0 {
		parts = append(parts, fmt.Sprintf("trimmed %d aged tool result(s)", result.TrimmedTools))
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: cause, Trigger: contract.InvalidationPressure, Scope: strings.Join(parts, "; "),
		Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return result, err
	}
	e.taskMu.Lock()
	e.maintenancePasses++
	if e.maintenancePasses >= 2 {
		e.maintenanceLatched = true
	}
	e.taskMu.Unlock()
	return result, nil
}

func (e *Engine) applyBoundaryToolChange(ctx context.Context) error {
	if e.boundaryTools == nil {
		return nil
	}
	change, changed, err := e.boundaryTools()
	if err != nil || !changed {
		return err
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: contract.InvalidationToolsetChange, Trigger: contract.InvalidationBoundary,
		Scope: change.Scope, RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return err
	}
	e.registry.ReplacePrefix("mcp__", change.Tools...)
	return nil
}

func (e *Engine) recordInvalidation(ctx context.Context, event contract.InvalidationEvent) error {
	if e.invalidations == nil {
		return errors.New("invalidation ledger is unavailable")
	}
	return e.invalidations.Record(ctx, event)
}

func (e *Engine) resetMaintenanceLatch() {
	e.taskMu.Lock()
	e.maintenancePasses = 0
	e.maintenanceLatched = false
	e.softNoticeShown = false   // T039: allow the soft advisory to fire again next cycle
	e.routedWindowNoted = false // a compaction is exactly what resolves the routed-window risk
	e.taskMu.Unlock()
}

// recordDegradedPrefixGuard writes once-per-session a usage-record marker so
// the prefix-shape guard's reduced fidelity is visible in benchmark logs and
// could trip a downstream alert. C2: never silently hash pre-replay bytes.
var degradedPrefixGuardLogged = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

func (e *Engine) recordDegradedPrefixGuard(_ context.Context, reason string) {
	detail := "degraded-prefix-guard:" + reason
	degradedPrefixGuardLogged.Lock()
	first := !degradedPrefixGuardLogged.seen[detail]
	degradedPrefixGuardLogged.seen[detail] = true
	degradedPrefixGuardLogged.Unlock()
	if first {
		// Keep this lossy, single-line, and unambiguous; it must never carry
		// request bytes (which would mutate cached content) and is operator
		// facing in cache logs only.
		fmt.Fprintf(degradedGuardSink(), "prefix-guard degraded: %s (session=%s)\n", reason, e.session.ID)
	}
	e.taskMu.Lock()
	e.lastShape = nil // force the next-guard loop into the relaxed branch
	e.taskMu.Unlock()
}

// testWriterOrStdout is exported in tests so the degraded guard message lands
// on the test output rather than disappearing into process stderr.
var degradedGuardWriter func() io.Writer

// degradedGuardSink is where the prefix-guard degradation marker goes: stderr in
// production, a test writer when one is installed. (Named testWriterOrStdout
// until v1.1.0, which was wrong twice over — it is not test-only, and it returns
// stderr.)
func degradedGuardSink() io.Writer {
	if degradedGuardWriter != nil {
		return degradedGuardWriter()
	}
	return os.Stderr
}

func floatPointer(value float64) *float64 {
	copy := value
	return &copy
}
