package contract

import "testing"

func TestAggregateUsagePreservesAvailabilityAndColdStart(t *testing.T) {
	zero, ten, two, eight := 0, 10, 2, 8
	records := []UsageRecord{
		{PromptTokens: &ten, CompletionTokens: &zero, CacheReadTokens: &zero, CacheMissTokens: &ten, Attribution: CacheAttributionColdStart},
		{PromptTokens: &ten, CompletionTokens: &two, CacheReadTokens: &eight, CacheMissTokens: &two, Attribution: CacheAttributionProvider},
		{Attribution: CacheAttributionNA},
	}
	aggregate := AggregateUsage(records)
	if aggregate.Requests != 3 || aggregate.UnavailableRequests != 1 || aggregate.SumPrompt != 20 {
		t.Fatalf("unexpected aggregate: %+v", aggregate)
	}
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.4 {
		t.Fatalf("session rate = %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != 0.8 {
		t.Fatalf("steady-state rate = %v", aggregate.SteadyStateHitRate)
	}
}

func TestHitRateZeroDenominatorIsUnavailable(t *testing.T) {
	zero := 0
	if got := HitRate(&zero, &zero); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
}

func TestAggregateUsageSumsPartialValuesWithoutUsingThemInRates(t *testing.T) {
	five, seven, three, one := 5, 7, 3, 1
	records := []UsageRecord{
		{CacheReadTokens: &five, Attribution: CacheAttributionNA},
		{CacheMissTokens: &seven, Attribution: CacheAttributionNA},
		{CacheReadTokens: &three, CacheMissTokens: &one, Attribution: CacheAttributionProvider},
		{CacheReadTokens: &one, CacheMissTokens: &three, Attribution: CacheAttributionColdStart},
	}

	aggregate := AggregateUsage(records)
	if aggregate.SumCacheRead != 9 || aggregate.SumCacheMiss != 11 {
		t.Fatalf("independent sums were not preserved: %+v", aggregate)
	}
	// Rate arithmetic (including the per-task delta) must draw on the paired
	// sums only — a one-sided record cannot contribute to a denominator.
	if aggregate.PairedCacheRead != 4 || aggregate.PairedCacheMiss != 4 {
		t.Fatalf("paired sums must exclude one-sided records: %+v", aggregate)
	}
	if aggregate.CacheAvailable != 2 || aggregate.UnavailableRequests != 2 {
		t.Fatalf("unexpected availability counts: %+v", aggregate)
	}
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.5 {
		t.Fatalf("session rate included a partial record: %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != 0.75 {
		t.Fatalf("steady-state rate = %v", aggregate.SteadyStateHitRate)
	}
}

func TestAggregateUsageHasNoSteadyStateRateWithOnlyColdStarts(t *testing.T) {
	read, miss := 9, 1
	aggregate := AggregateUsage([]UsageRecord{{
		CacheReadTokens: &read,
		CacheMissTokens: &miss,
		Attribution:     CacheAttributionColdStart,
	}})
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.9 {
		t.Fatalf("session rate = %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate != nil {
		t.Fatalf("expected unavailable steady-state rate, got %v", *aggregate.SteadyStateHitRate)
	}
}

func TestAggregateUsageComputesPrefixStabilityFromMainStreamOnly(t *testing.T) {
	zero, hundred, oneHundredTen, ninetyEight, twelve, auxRead, auxMiss := 0, 100, 110, 98, 12, 0, 1000
	tail := 10
	records := []UsageRecord{
		{Stream: UsageStreamMain, PromptTokens: &hundred, CacheReadTokens: &zero, CacheMissTokens: &hundred, Attribution: CacheAttributionColdStart},
		{Stream: UsageStreamAux, PromptTokens: &hundred, CacheReadTokens: &auxRead, CacheMissTokens: &auxMiss, Attribution: CacheAttributionNA},
		{Stream: UsageStreamMain, PromptTokens: &oneHundredTen, CacheReadTokens: &ninetyEight, CacheMissTokens: &twelve, NewTailTokens: &tail, Attribution: CacheAttributionProvider},
	}
	aggregate := AggregateUsage(records)
	if aggregate.PrefixStabilityRate == nil || *aggregate.PrefixStabilityRate != 0.98 {
		t.Fatalf("prefix stability=%v aggregate=%+v", aggregate.PrefixStabilityRate, aggregate)
	}
	if aggregate.MainRequests != 2 || aggregate.AuxRequests != 1 || aggregate.SubagentRequests != 0 {
		t.Fatalf("stream counts=%+v", aggregate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != float64(ninetyEight)/float64(ninetyEight+twelve) {
		t.Fatalf("aux usage polluted main rate: %+v", aggregate)
	}
}
