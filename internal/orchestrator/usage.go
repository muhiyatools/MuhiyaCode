package orchestrator

import (
	"context"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func (e *Engine) addTaskAgentUsage(usage contract.Usage) {
	e.taskMu.Lock()
	e.taskAgentUsage = e.taskAgentUsage.Add(usage)
	e.taskMu.Unlock()
}

func (e *Engine) recordMainUsage(ctx context.Context, observation mainUsageObservation) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	previous := lastMainUsageRecord(e.usageRecords)
	newTail := estimatedNewTail(previous, observation.usage)
	attribution := e.mainCacheAttribution(cacheMissContext{previous: previous, usage: observation.usage, newTail: newTail, previousMessageCount: e.lastSentMessageCount, currentMessageCount: observation.messageCount}, observation.changeReasons)
	record := usageRecord(usageRecordInput{model: observation.model, stream: contract.UsageStreamMain, usage: observation.usage, reasons: observation.changeReasons, attribution: attribution, durationMS: observation.durationMS})
	if previous != nil && (observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0) {
		record.NewTailTokens = intPointer(newTail)
	}
	if err := e.appendUsageLocked(ctx, &record); err != nil {
		return err
	}
	if observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0 {
		e.latestPromptTokens = observation.usage.PromptTokens
		e.latestPromptAvailable = true
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

func (e *Engine) recordAuxUsage(ctx context.Context, model string, usage contract.Usage, durationMS *int64) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	record := usageRecord(usageRecordInput{model: model, stream: contract.UsageStreamAux, usage: usage, attribution: contract.CacheAttributionNA, durationMS: durationMS})
	return e.appendUsageLocked(ctx, &record)
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

func (e *Engine) recordIsolatedUsage(ctx context.Context, model string, usage contract.Usage, coldStart bool, reasons []string, durationMS *int64) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	attribution := contract.CacheAttributionNA
	if usage.CacheReadTokens != nil && usage.CacheMissTokens != nil {
		switch {
		case coldStart:
			attribution = contract.CacheAttributionColdStart
		case len(reasons) > 0:
			attribution = contract.CacheAttributionAgent
		default:
			attribution = contract.CacheAttributionProvider
		}
	}
	record := usageRecord(usageRecordInput{model: model, stream: contract.UsageStreamSubagent, usage: usage, reasons: reasons, attribution: attribution, durationMS: durationMS})
	return e.appendUsageLocked(ctx, &record)
}

func (e *Engine) appendUsageLocked(ctx context.Context, record *contract.UsageRecord) error {
	record.Seq = e.requestSeq + 1
	if record.At.IsZero() {
		record.At = time.Now().UTC()
	}
	if e.persistence.AppendUsage != nil {
		if err := e.persistence.AppendUsage(ctx, *record); err != nil {
			return err
		}
	}
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
		At:                 time.Now().UTC(),
		Model:              input.model,
		Stream:             input.stream,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
		CacheReadTokens:    read,
		CacheMissTokens:    miss,
		MissDerived:        input.usage.MissDerived,
		HitRate:            contract.HitRate(read, miss),
		PrefixChanged:      len(input.reasons) > 0,
		ChangeReasons:      append([]string{}, input.reasons...),
		Attribution:        input.attribution,
		UsageContradictory: input.usage.Contradictory,
		Diagnostic:         input.usage.Diagnostic,
		CostUSD:            cloneFloatPointer(input.usage.CostUSD),
		CostEstimated:      input.usage.CostEstimated,
		LogID:              input.usage.CostLogID,
		DurationMS:         input.durationMS,
	}
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
	value.SessionHitRate = cloneFloatPointer(value.SessionHitRate)
	value.SteadyStateHitRate = cloneFloatPointer(value.SteadyStateHitRate)
	value.PrefixStabilityRate = cloneFloatPointer(value.PrefixStabilityRate)
	return value
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

func cloneUsageRecord(record contract.UsageRecord) contract.UsageRecord {
	record.PromptTokens = cloneIntPointer(record.PromptTokens)
	record.CompletionTokens = cloneIntPointer(record.CompletionTokens)
	record.CacheReadTokens = cloneIntPointer(record.CacheReadTokens)
	record.CacheMissTokens = cloneIntPointer(record.CacheMissTokens)
	record.NewTailTokens = cloneIntPointer(record.NewTailTokens)
	record.HitRate = cloneFloatPointer(record.HitRate)
	record.CostUSD = cloneFloatPointer(record.CostUSD)
	record.ChangeReasons = append([]string{}, record.ChangeReasons...)
	return record
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
	return result
}
