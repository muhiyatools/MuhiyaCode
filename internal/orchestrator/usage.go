package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) recordMainUsage(ctx context.Context, observation mainUsageObservation) error {
	e.usageWriteMu.Lock()
	defer e.usageWriteMu.Unlock()

	e.taskMu.Lock()
	previous := lastMainUsageRecord(e.usageRecords)
	newTail := estimatedNewTail(previous, observation.usage)
	attribution := e.mainCacheAttribution(cacheMissContext{previous: previous, usage: observation.usage, newTail: newTail, previousMessageCount: e.lastSentMessageCount, currentMessageCount: observation.messageCount}, observation.changeReasons)
	record := usageRecord(usageRecordInput{model: observation.model, stream: contract.UsageStreamMain, pin: ":main", usage: observation.usage, reasons: observation.changeReasons, attribution: attribution, durationMS: observation.durationMS})
	record.HistoryRewriteVersion = observation.rewriteVersion
	record.ManifestHash = observation.manifestHash
	record.FinishReason = observation.finishReason
	record.Transport = optionalStringPointer(observation.transport)
	record.TaskEpochID = cloneStringPointer(observation.taskEpochID)
	record.Phase = cloneStringPointer(observation.phase)
	record.RetryOf = cloneIntPointer(observation.retryOf)
	record.BudgetDecision = observation.budgetDecision
	record.ReasoningTier = optionalStringPointer(string(observation.reasoningTier))
	if observation.outputCap > 0 {
		record.OutputCap = intPointer(observation.outputCap)
	}
	record.TruncationEscalated = observation.truncationEscalated
	record.OutputBudgetOverride = optionalStringPointer(observation.outputBudgetOverride)
	if previous != nil && (observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0) {
		record.NewTailTokens = intPointer(newTail)
	}
	e.taskMu.Unlock()

	if err := e.appendUsage(ctx, &record); err != nil {
		return err
	}
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	// This model has now seen the conversation at this revision, so a later task
	// may return to it cheaply (switchcost.go).
	e.noteModelWarm(observation.model, observation.rewriteVersion)
	// An upstream flip invalidates that warmth for real, whatever our own bytes
	// say — retire it so the next switch decision is not priced against a cache
	// that no longer exists, and queue the explanation for the caller to emit.
	if notice := e.noteUpstreamFlip(observation.usage.Upstream); notice != "" {
		e.warmPrefix = nil
		e.pendingUpstreamNotice = notice
		// Re-arm the shape sidecar so a later resume pins to where we ended up,
		// not where we started. Same re-arming the model switch already does.
		e.prefixShapeSaved = false
	}
	if observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0 {
		e.latestPromptTokens = observation.usage.PromptTokens
		e.latestPromptAvailable = true
		e.latestPromptRewriteVersion = observation.rewriteVersion
	}
	// C6: a resumed session's cold miss on a large prefix is flagged here (under
	// taskMu) and surfaced once by the request-loop caller, outside the lock.
	e.flagColdStartIfNeeded(previous, observation.usage)
	e.firstAfterStart = false
	return nil
}

func estimatedNewTail(previous *contract.UsageRecord, usage contract.Usage) int {
	if previous == nil || previous.PromptTokens == nil || (!usage.PromptTokensAvailable && usage.PromptTokens == 0) {
		return 0
	}
	return max(0, usage.PromptTokens-*previous.PromptTokens)
}

func (e *Engine) mainCacheAttribution(missContext cacheMissContext, changeReasons []string) contract.CacheAttribution {
	if missContext.usage.CacheReadTokens == nil || missContext.usage.CacheMissTokens == nil {
		return contract.CacheAttributionNA
	}
	if e.firstAfterStart {
		return contract.CacheAttributionColdStart
	}
	if len(changeReasons) > 0 {
		return contract.CacheAttributionAgent
	}
	if suspiciousCacheMiss(missContext) {
		return contract.CacheAttributionAgentSuspect
	}
	return contract.CacheAttributionProvider
}

type auxUsageObservation struct {
	model        string
	pin          string
	usage        contract.Usage
	durationMS   *int64
	phase        contract.ExecutionPhase
	decisionCode string
}

func (e *Engine) recordAuxUsage(ctx context.Context, model, pin string, usage contract.Usage, durationMS *int64) error {
	return e.recordAttributedAuxUsage(ctx, auxUsageObservation{model: model, pin: pin, usage: usage, durationMS: durationMS})
}

func (e *Engine) recordAttributedAuxUsage(ctx context.Context, observation auxUsageObservation) error {
	e.usageWriteMu.Lock()
	defer e.usageWriteMu.Unlock()
	record := usageRecord(usageRecordInput{model: observation.model, stream: contract.UsageStreamAux, pin: observation.pin, usage: observation.usage, attribution: contract.CacheAttributionNA, durationMS: observation.durationMS})
	record.Phase = optionalStringPointer(string(observation.phase))
	if e.settings != nil {
		record.Transport = optionalStringPointer(e.settings.Provider.Type)
	}
	record.BudgetDecision = observation.decisionCode
	if err := e.appendUsage(ctx, &record); err != nil {
		return err
	}
	// Compaction is auxiliary work for accounting but deliberately rides the
	// main affinity route. If fallback routing moves it, adopt that upstream so
	// the next main turn does not keep pinning the stale provider.
	if strings.HasPrefix(observation.pin, ":main") && observation.usage.Upstream != "" {
		e.taskMu.Lock()
		if notice := e.noteUpstreamFlip(observation.usage.Upstream); notice != "" {
			e.warmPrefix = nil
			e.pendingUpstreamNotice = notice
			e.prefixShapeSaved = false
		}
		e.taskMu.Unlock()
	}
	return nil
}

// elapsedMS returns the whole milliseconds since start as a nullable pointer —
// the shape UsageRecord.DurationMS wants (feature 008 UD-6).
func elapsedMS(start time.Time) *int64 {
	ms := time.Since(start).Milliseconds()
	return &ms
}

// taskUsageSnapshot is the cumulative usage of the current task across main,
// subagent, and auxiliary streams.
func (e *Engine) taskUsageSnapshot() contract.Usage {
	e.taskMu.Lock()
	usage := subtractUsage(e.sessionUsage, e.taskUsageStart)
	e.taskMu.Unlock()
	return usage
}

// recordUsageAndEmit orders ledger updates from concurrent pipeline workers.
// Callbacks run outside the mutex; a re-entrant update joins the queue instead
// of deadlocking on application code.
func (e *Engine) recordUsageAndEmit(record func() error) error {
	e.usageEmitMu.Lock()
	if err := record(); err != nil {
		e.usageEmitMu.Unlock()
		return err
	}
	if e.callbacks.Usage == nil {
		e.usageEmitMu.Unlock()
		return nil
	}
	e.usageEmissionQueue = append(e.usageEmissionQueue, e.taskUsageSnapshot())
	if e.usageEmitting {
		e.usageEmitMu.Unlock()
		return nil
	}
	e.usageEmitting = true
	e.usageEmitMu.Unlock()
	e.drainUsageEmissions()
	return nil
}

func (e *Engine) drainUsageEmissions() {
	for {
		e.usageEmitMu.Lock()
		if len(e.usageEmissionQueue) == 0 {
			e.usageEmitting = false
			e.usageEmitMu.Unlock()
			return
		}
		usage := e.usageEmissionQueue[0]
		e.usageEmissionQueue = e.usageEmissionQueue[1:]
		e.usageEmitMu.Unlock()
		e.callbacks.Usage(usage)
	}
}

func (e *Engine) appendUsage(ctx context.Context, record *contract.UsageRecord) error {
	e.taskMu.Lock()
	record.Seq = e.requestSeq + 1
	if record.At.IsZero() {
		record.At = time.Now().UTC()
	}
	e.taskMu.Unlock()

	if e.persistence.AppendUsage != nil {
		if err := e.persistence.AppendUsage(ctx, *record); err != nil {
			return err
		}
	}
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	e.requestSeq = record.Seq
	e.usageRecords = append(e.usageRecords, cloneUsageRecord(*record))
	e.usageAggregate = contract.AggregateUsage(e.usageRecords)
	e.sessionUsage = usageFromAggregate(e.usageAggregate)
	return nil
}

func usageRecord(input usageRecordInput) contract.UsageRecord {
	prompt := nullableUsageValue(input.usage.PromptTokens, input.usage.PromptTokensAvailable)
	completion := nullableUsageValue(input.usage.CompletionTokens, input.usage.CompletionTokensAvailable)
	read := cloneIntPointer(input.usage.CacheReadTokens)
	miss := cloneIntPointer(input.usage.CacheMissTokens)
	return contract.UsageRecord{
		At:                   time.Now().UTC(),
		Model:                input.model,
		Stream:               input.stream,
		Pin:                  input.pin,
		PromptTokens:         prompt,
		CompletionTokens:     completion,
		CacheReadTokens:      read,
		CacheMissTokens:      miss,
		CacheWriteTokens:     cloneIntPointer(input.usage.CacheWriteTokens),
		CacheCreationTokens:  cloneIntPointer(input.usage.CacheCreationTokens),
		UncachedInputTokens:  cloneIntPointer(input.usage.UncachedInputTokens),
		CacheUsageSchema:     input.usage.CacheUsageSchema,
		CacheUsageDerivation: input.usage.CacheUsageDerivation,
		MissDerived:          input.usage.MissDerived,
		HitRate:              contract.HitRate(read, miss),
		PrefixChanged:        len(input.reasons) > 0,
		ChangeReasons:        append([]string{}, input.reasons...),
		Attribution:          input.attribution,
		UsageContradictory:   input.usage.Contradictory,
		Diagnostic:           input.usage.Diagnostic,
		CostUSD:              cloneFloatPointer(input.usage.CostUSD),
		CostEstimated:        input.usage.CostEstimated,
		LogID:                input.usage.CostLogID,
		Upstream:             input.usage.Upstream,
		DurationMS:           input.durationMS,
	}
}

// upstreamPin is the upstream provider this session has already warmed, sent on
// every later request so a routing layer sends us back to it (see
// contract.ChatRequest.PinUpstream). Empty until the first response names one,
// and empty forever on a direct connection — both correctly meaning "no
// preference".
//
// Learning the value instead of configuring it is the point: the set of
// upstreams behind a model slug changes without notice, so any hardcoded list
// would rot, and a wrong name is worse than none. Whoever served us first is by
// definition both available and holding our prefix.
func (e *Engine) upstreamPin() string {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.lastUpstream
}

// noteUpstreamFlip surfaces a change in the provider a routing layer served
// this session from. Prefix caches are per-upstream, so a flip cold-starts a
// cache that every byte we send says should still be warm — the miss looks like
// unexplained drift and there is nothing on our side to find. Saying it once,
// when it happens, is the difference between a diagnosable event and a mystery.
//
// It also re-pins: whoever served this request is the one holding our prefix
// from here on, so a fallback during an outage self-heals into the new affinity
// rather than fighting it every turn.
//
// Silent when the field is absent, which is every direct provider connection.
// Called under taskMu from recordMainUsage.
func (e *Engine) noteUpstreamFlip(upstream string) string {
	if upstream == "" {
		return ""
	}
	previous := e.lastUpstream
	e.lastUpstream = upstream
	if previous == "" || previous == upstream {
		return ""
	}
	return fmt.Sprintf("Upstream changed: %s → %s. Prefix caches are per-upstream, so this request started cold.", previous, upstream)
}

func lastMainUsageRecord(records []contract.UsageRecord) *contract.UsageRecord {
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Stream == "" || records[index].Stream == contract.UsageStreamMain {
			return &records[index]
		}
	}
	return nil
}

func suspiciousCacheMiss(missContext cacheMissContext) bool {
	previous, usage := missContext.previous, missContext.usage
	if previous == nil || previous.PromptTokens == nil || usage.CacheReadTokens == nil || usage.CacheMissTokens == nil {
		return false
	}
	if (usage.PromptTokensAvailable || usage.PromptTokens != 0) && usage.PromptTokens < *previous.PromptTokens {
		return true
	}
	minimumRead := (*previous.PromptTokens/64)*64 - 2*64
	if missContext.currentMessageCount >= missContext.previousMessageCount && *usage.CacheReadTokens < max(0, minimumRead) {
		return true
	}
	return *usage.CacheMissTokens > missContext.newTail+2*64
}

func intPointer(value int) *int { return &value }

func nullableUsageValue(value int, available bool) *int {
	if !available && value == 0 {
		return nil
	}
	copy := value
	return &copy
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func usageFromAggregate(aggregate contract.SessionUsageAggregate) contract.Usage {
	usage := contract.Usage{
		PromptTokens:              aggregate.SumPrompt,
		CompletionTokens:          aggregate.SumCompletion,
		TotalTokens:               aggregate.SumPrompt + aggregate.SumCompletion,
		CachedTokens:              aggregate.SumCacheRead,
		PromptTokensAvailable:     aggregate.PromptAvailable > 0,
		CompletionTokensAvailable: aggregate.CompletionAvailable > 0,
	}
	if aggregate.CacheWriteAvailable > 0 {
		usage.CacheWriteTokens = cloneIntPointer(&aggregate.SumCacheWrite)
	}
	if aggregate.CacheCreateAvailable > 0 {
		usage.CacheCreationTokens = cloneIntPointer(&aggregate.SumCacheCreation)
	}
	if aggregate.UncachedAvailable > 0 {
		usage.UncachedInputTokens = cloneIntPointer(&aggregate.SumUncachedInput)
	}
	if aggregate.CacheAvailable > 0 {
		// Paired sums, not the one-sided display sums: sessionUsage feeds rate
		// arithmetic (the per-task delta divided by the TUI summary), and a rate
		// operand must only ever come from records that reported both sides.
		usage.CacheReadTokens = cloneIntPointer(&aggregate.PairedCacheRead)
		usage.CacheMissTokens = cloneIntPointer(&aggregate.PairedCacheMiss)
	}
	return usage
}

func cloneUsageAggregate(value contract.SessionUsageAggregate) contract.SessionUsageAggregate {
	value.AllStreamHitRate = cloneFloatPointer(value.AllStreamHitRate)
	value.SessionHitRate = cloneFloatPointer(value.SessionHitRate)
	value.SteadyStateHitRate = cloneFloatPointer(value.SteadyStateHitRate)
	value.PrefixStabilityRate = cloneFloatPointer(value.PrefixStabilityRate)
	value.MaxMainPromptTokens = cloneIntPointer(value.MaxMainPromptTokens)
	value.ReplayAmplification = cloneFloatPointer(value.ReplayAmplification)
	value.MaxOutputCap = cloneIntPointer(value.MaxOutputCap)
	value.RequestsByPhase = cloneStringIntMap(value.RequestsByPhase)
	value.RequestsByReasoning = cloneStringIntMap(value.RequestsByReasoning)
	return value
}

func cloneStringIntMap(value map[string]int) map[string]int {
	if value == nil {
		return nil
	}
	result := make(map[string]int, len(value))
	for key, count := range value {
		result[key] = count
	}
	return result
}

func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneUsageRecords(records []contract.UsageRecord) []contract.UsageRecord {
	result := make([]contract.UsageRecord, len(records))
	for i, record := range records {
		result[i] = cloneUsageRecord(record)
	}
	return result
}

func economyStatsFromRecords(records []contract.UsageRecord) contract.TaskEconomyStats {
	stats := contract.TaskEconomyStats{
		Aggregate: contract.AggregateUsage(records), RequestsByPhase: map[string]int{}, RequestsByTransport: map[string]int{},
	}
	for _, record := range records {
		phase := "unavailable"
		if record.Phase != nil && strings.TrimSpace(*record.Phase) != "" {
			phase = *record.Phase
		}
		stats.RequestsByPhase[phase]++
		transport := "unavailable"
		if record.Transport != nil && strings.TrimSpace(*record.Transport) != "" {
			transport = *record.Transport
		}
		stats.RequestsByTransport[transport]++
		if record.RetryOf != nil {
			stats.Retries = append(stats.Retries, contract.RetryUsageRow{Seq: record.Seq, RetryOf: *record.RetryOf, Phase: phase, Transport: transport})
		}
		row := contract.PhasePlanUsageRow{Seq: record.Seq, Phase: phase, Decision: record.BudgetDecision}
		if record.ReasoningTier != nil {
			row.Reasoning = *record.ReasoningTier
		}
		if record.OutputCap != nil {
			row.OutputCap = *record.OutputCap
		}
		stats.PhasePlans = append(stats.PhasePlans, row)
		if record.TruncationEscalated {
			stats.TruncationEscalations++
		}
		if record.OutputBudgetOverride != nil {
			stats.OutputBudgetOverrides = append(stats.OutputBudgetOverrides, *record.OutputBudgetOverride)
		}
	}
	return stats
}

func cloneUsageRecord(record contract.UsageRecord) contract.UsageRecord {
	record.PromptTokens = cloneIntPointer(record.PromptTokens)
	record.CompletionTokens = cloneIntPointer(record.CompletionTokens)
	record.CacheReadTokens = cloneIntPointer(record.CacheReadTokens)
	record.CacheMissTokens = cloneIntPointer(record.CacheMissTokens)
	record.CacheWriteTokens = cloneIntPointer(record.CacheWriteTokens)
	record.CacheCreationTokens = cloneIntPointer(record.CacheCreationTokens)
	record.UncachedInputTokens = cloneIntPointer(record.UncachedInputTokens)
	record.NewTailTokens = cloneIntPointer(record.NewTailTokens)
	record.TaskEpochID = cloneStringPointer(record.TaskEpochID)
	record.Phase = cloneStringPointer(record.Phase)
	record.Transport = cloneStringPointer(record.Transport)
	record.RetryOf = cloneIntPointer(record.RetryOf)
	record.ReasoningTier = cloneStringPointer(record.ReasoningTier)
	record.OutputCap = cloneIntPointer(record.OutputCap)
	record.OutputBudgetOverride = cloneStringPointer(record.OutputBudgetOverride)
	record.HitRate = cloneFloatPointer(record.HitRate)
	record.CostUSD = cloneFloatPointer(record.CostUSD)
	record.ChangeReasons = append([]string{}, record.ChangeReasons...)
	return record
}

func optionalStringPointer(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	copy := value
	return &copy
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func subtractUsage(total, before contract.Usage) contract.Usage {
	result := contract.Usage{
		PromptTokens: max(0, total.PromptTokens-before.PromptTokens), CompletionTokens: max(0, total.CompletionTokens-before.CompletionTokens),
		TotalTokens: max(0, total.TotalTokens-before.TotalTokens), CachedTokens: max(0, total.CachedTokens-before.CachedTokens),
		PromptTokensAvailable: total.PromptTokensAvailable, CompletionTokensAvailable: total.CompletionTokensAvailable,
	}
	if total.CacheReadTokens != nil {
		value := *total.CacheReadTokens
		if before.CacheReadTokens != nil {
			value -= *before.CacheReadTokens
		}
		result.CacheReadTokens = &value
	}
	if total.CacheMissTokens != nil {
		value := *total.CacheMissTokens
		if before.CacheMissTokens != nil {
			value -= *before.CacheMissTokens
		}
		result.CacheMissTokens = &value
	}
	if total.CacheWriteTokens != nil {
		value := *total.CacheWriteTokens
		if before.CacheWriteTokens != nil {
			value -= *before.CacheWriteTokens
		}
		result.CacheWriteTokens = &value
	}
	if total.CacheCreationTokens != nil {
		value := *total.CacheCreationTokens
		if before.CacheCreationTokens != nil {
			value -= *before.CacheCreationTokens
		}
		result.CacheCreationTokens = &value
	}
	if total.UncachedInputTokens != nil {
		value := *total.UncachedInputTokens
		if before.UncachedInputTokens != nil {
			value -= *before.UncachedInputTokens
		}
		result.UncachedInputTokens = &value
	}
	return result
}
