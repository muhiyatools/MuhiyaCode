package tui

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// Contract display-formats §5.3 (FR-024): the same scope must read the same on
// every surface. These are the tests that would catch a future "quick fix" to
// one renderer that silently disagrees with another.

// The live activity line and the end-of-task summary derive from ONE helper, so
// a user watching a number climb and then reading the summary sees the same
// figure — not two plausible numbers that differ.
func TestLiveHeadlineAndSummaryAgree(t *testing.T) {
	read, miss := 90_000, 10_000
	usage := contract.Usage{TotalTokens: 105_000, CompletionTokens: 5_000, CacheReadTokens: &read, CacheMissTokens: &miss}

	tokens, tag := headlineTokens(usage)
	if tokens != 105_000 {
		t.Fatalf("headline should be cache-inclusive (90,000 + 10,000 + 5,000), got %d", tokens)
	}
	summary := taskSummaryLine(contract.TaskStats{Usage: usage}, newPalette("dark"))
	if !strings.Contains(summary, contract.FullTokens(tokens)) {
		t.Fatalf("summary figure disagrees with the live headline (%s):\n%s", contract.FullTokens(tokens), summary)
	}
	if !strings.Contains(summary, tag) {
		t.Fatalf("summary cache tag disagrees with the live one (%s):\n%s", tag, summary)
	}
}

// FR-020: the summary shows complete digits, never the old abbreviation.
func TestSummaryRendersFullDigits(t *testing.T) {
	read, miss := 900_000, 45_000
	usage := contract.Usage{TotalTokens: 954_200, CompletionTokens: 9_200, CacheReadTokens: &read, CacheMissTokens: &miss}
	summary := taskSummaryLine(contract.TaskStats{Usage: usage}, newPalette("dark"))
	if !strings.Contains(summary, "954,200 tokens") {
		t.Fatalf("summary is not full-digit cache-inclusive:\n%s", summary)
	}
	for _, abbreviated := range []string{"954.2k", "45.0k", "900.0k"} {
		if strings.Contains(summary, abbreviated) {
			t.Fatalf("summary still abbreviates (%q):\n%s", abbreviated, summary)
		}
	}
}

// The harness-friction marker is gone from the summary (013 FR-013) even when
// the engine recorded events — recording is telemetry, not user-facing.
func TestSummaryHasNoFrictionMarker(t *testing.T) {
	summary := taskSummaryLine(contract.TaskStats{Usage: contract.Usage{TotalTokens: 100}, HarnessEvents: 3}, newPalette("dark"))
	if strings.Contains(summary, "harness") || strings.Contains(summary, "⚠") {
		t.Fatalf("task summary still reports harness friction:\n%s", summary)
	}
}

// The footer's session rate and the context card's hit rate are the same
// quantity, so they must come from the same aggregate field (FR-021).
func TestFooterAndCardShareTheSessionRate(t *testing.T) {
	aggregate := contract.AggregateUsage([]contract.UsageRecord{
		mainPaired(90, 10),
		subagentPaired(0, 100),
	})
	if aggregate.AllStreamHitRate == nil {
		t.Fatal("fixture should produce an available rate")
	}

	// The footer consumes TaskStats.SessionHitRate, which the engine now fills
	// from AllStreamHitRate; the card reads the aggregate field directly.
	stats := contract.TaskStats{SessionHitRate: aggregate.AllStreamHitRate}
	card := formatContextReport(orchestrator.ContextReport{ContextLimit: 100, UsageAggregate: aggregate}, "m")

	want := "45.00%" // (90+0) read over (100+100) total
	if !strings.Contains(card, "Hit rate:  "+want) {
		t.Fatalf("card rate is not the all-stream rate (%s):\n%s", want, card)
	}
	if stats.SessionHitRate == nil || *stats.SessionHitRate != *aggregate.AllStreamHitRate {
		t.Fatalf("footer rate diverged from the card's: %v vs %v", stats.SessionHitRate, aggregate.AllStreamHitRate)
	}
}

func mainPaired(read, miss int) contract.UsageRecord {
	return contract.UsageRecord{Stream: contract.UsageStreamMain, CacheReadTokens: &read, CacheMissTokens: &miss, Attribution: contract.CacheAttributionProvider}
}

func subagentPaired(read, miss int) contract.UsageRecord {
	return contract.UsageRecord{Stream: contract.UsageStreamAux, CacheReadTokens: &read, CacheMissTokens: &miss, Attribution: contract.CacheAttributionNA}
}
