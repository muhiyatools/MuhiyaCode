package tui

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The per-model display table left with the context card (013 FR-023), but the
// aggregate behind it still matters: a provider that reports no cache fields
// must never have availability fabricated for it.
func TestMixedProviderUsageRowsAndUnavailableCache(t *testing.T) {
	prompt, output := 1000, 100
	deepRead, deepMiss := 800, 200
	miniPrompt, miniOutput := 511, 50
	deepCost, miniCost := 0.002, 0.001
	records := []contract.UsageRecord{
		{Model: "deepseek-v4-pro", Stream: contract.UsageStreamMain, PromptTokens: &prompt, CompletionTokens: &output, CacheReadTokens: &deepRead, CacheMissTokens: &deepMiss, CostUSD: &deepCost},
		{Model: "minimax-m3", Stream: contract.UsageStreamSubagent, PromptTokens: &miniPrompt, CompletionTokens: &miniOutput, CacheReadTokens: nil, CacheMissTokens: nil, CostUSD: &miniCost},
	}
	rows := contract.AggregateUsageByModel(records)
	if len(rows) != 2 || rows[0].Model != "deepseek-v4-pro" || rows[1].Model != "minimax-m3" {
		t.Fatalf("mixed-provider rows = %+v", rows)
	}
	if !rows[0].CacheAvailable || rows[1].CacheAvailable {
		t.Fatalf("cache availability was fabricated: %+v", rows)
	}
	if rows[1].CacheRead != 0 || rows[1].UncachedIn != 0 {
		t.Fatalf("a non-reporting provider must contribute no cache tokens: %+v", rows[1])
	}
}
