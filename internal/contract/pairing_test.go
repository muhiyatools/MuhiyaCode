package contract

import "testing"

// Feature 011 D8: per-(model, pin) aggregation excludes each pairing's first
// cache-reporting record (the cold write) and never fabricates rates for
// pairings whose provider reported no cache fields.
func TestPerPairingRates(t *testing.T) {
	read := func(v int) *int { return &v }
	records := []UsageRecord{
		{Model: "minimax-m3", Pin: ":main", CacheReadTokens: read(0), CacheMissTokens: read(1000)},       // cold — excluded
		{Model: "minimax-m3", Pin: ":main", CacheReadTokens: read(900), CacheMissTokens: read(100)},      // steady
		{Model: "minimax-m3", Pin: ":main", CacheReadTokens: read(950), CacheMissTokens: read(50)},       // steady
		{Model: "deepseek-v4", Pin: ":sub:review", CacheReadTokens: read(0), CacheMissTokens: read(400)}, // cold — excluded
		{Model: "deepseek-v4", Pin: ":sub:review", CacheReadTokens: read(380), CacheMissTokens: read(20)},
		{Model: "deepseek-v4", Pin: ":sub:explore"}, // no cache fields at all
	}
	rates := PerPairingRates(records)
	if len(rates) != 3 {
		t.Fatalf("pairings = %d, want 3: %+v", len(rates), rates)
	}
	main := rates[0]
	if main.Model != "minimax-m3" || main.Pin != ":main" || main.Requests != 3 || !main.Reported {
		t.Fatalf("main pairing = %+v", main)
	}
	if main.SteadyStateHitRate == nil || *main.SteadyStateHitRate != 1850.0/2000.0 {
		t.Fatalf("main steady-state rate = %v, want 0.925 (cold excluded)", main.SteadyStateHitRate)
	}
	review := rates[1]
	if review.Pin != ":sub:review" || review.SteadyStateHitRate == nil || *review.SteadyStateHitRate != 0.95 {
		t.Fatalf("review pairing = %+v", review)
	}
	explore := rates[2]
	if explore.Reported || explore.SteadyStateHitRate != nil {
		t.Fatalf("unreported pairing must stay unreported, got %+v", explore)
	}
	// Pre-011 records without a pin group under their stream label.
	legacy := PerPairingRates([]UsageRecord{{Model: "m", Stream: UsageStreamMain}})
	if len(legacy) != 1 || legacy[0].Pin != "main" {
		t.Fatalf("legacy grouping = %+v", legacy)
	}
}
