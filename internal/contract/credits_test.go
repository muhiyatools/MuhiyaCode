package contract

import "testing"

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

// TestSumCreditsUSDMemberSetRules (003 T007) exercises the member-set rules:
// empty-usage records excluded; any remaining nil cost ⇒ unavailable; estimated
// ORs; a costless record only affects its own set.
func TestSumCreditsUSDMemberSetRules(t *testing.T) {
	t.Run("all priced sums", func(t *testing.T) {
		records := []UsageRecord{
			{PromptTokens: ip(10), CostUSD: fp(0.01)},
			{PromptTokens: ip(20), CostUSD: fp(0.02), CostEstimated: true},
		}
		got := SumCreditsUSD(records)
		if got.USD == nil || *got.USD != 0.03 {
			t.Fatalf("USD = %v, want 0.03", got.USD)
		}
		if !got.Estimated {
			t.Fatal("Estimated should OR true")
		}
		if got.Priced != 2 || got.Eligible != 2 {
			t.Fatalf("priced/eligible = %d/%d, want 2/2", got.Priced, got.Eligible)
		}
	})

	t.Run("empty-usage record excluded, not poisoning", func(t *testing.T) {
		records := []UsageRecord{
			{PromptTokens: ip(10), CostUSD: fp(0.05)},
			{}, // failed aux call: no provider usage → excluded entirely
		}
		got := SumCreditsUSD(records)
		if got.USD == nil || *got.USD != 0.05 {
			t.Fatalf("USD = %v, want 0.05 (empty record must not poison)", got.USD)
		}
		if got.Eligible != 1 {
			t.Fatalf("eligible = %d, want 1", got.Eligible)
		}
	})

	t.Run("usage-bearing record without cost ⇒ unavailable", func(t *testing.T) {
		records := []UsageRecord{
			{PromptTokens: ip(10), CostUSD: fp(0.05)},
			{PromptTokens: ip(20)}, // real usage, no cost → whole set unavailable
		}
		got := SumCreditsUSD(records)
		if got.USD != nil {
			t.Fatalf("USD = %v, want nil (nil cost poisons the set)", *got.USD)
		}
	})

	t.Run("no eligible members ⇒ unavailable", func(t *testing.T) {
		got := SumCreditsUSD([]UsageRecord{{}, {}})
		if got.USD != nil {
			t.Fatalf("USD = %v, want nil", *got.USD)
		}
	})
}
