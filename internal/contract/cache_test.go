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
