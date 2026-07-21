package contract

import "testing"

// A-7: a degenerate record (NewTailTokens >= PromptTokens, eligible = 0) must not
// contribute its cache reads to the numerator while contributing nothing to the
// denominator — that inflated PrefixStabilityRate past 100%.
func TestPrefixStabilityRateNeverExceeds100(t *testing.T) {
	p := func(n int) *int { return &n }
	records := []UsageRecord{
		{Stream: UsageStreamMain, PromptTokens: p(1000), NewTailTokens: p(100), CacheReadTokens: p(800), CacheMissTokens: p(200), Attribution: CacheAttributionProvider},
		{Stream: UsageStreamMain, PromptTokens: p(500), NewTailTokens: p(500), CacheReadTokens: p(500), CacheMissTokens: p(0), Attribution: CacheAttributionProvider},
	}
	agg := AggregateUsage(records)
	if agg.PrefixStabilityRate == nil {
		t.Fatal("expected a prefix-stability rate")
	}
	if *agg.PrefixStabilityRate > 1.0 {
		t.Fatalf("PrefixStabilityRate = %.3f, must not exceed 100%% — a clamped record inflated it", *agg.PrefixStabilityRate)
	}
}
