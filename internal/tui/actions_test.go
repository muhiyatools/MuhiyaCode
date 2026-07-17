package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

func TestDisplayHonestyUnavailableAndReportedZero(t *testing.T) {
	// The headline cache-tag honesty cases (reported zero vs. absent metrics)
	// moved to TestHeadlineTokens in usage_display_test.go when 008 T019
	// replaced cacheTag's read/new split with the percentage-only headline tag.
	unavailable := formatContextReport(orchestrator.ContextReport{ContextLimit: 128000, UsageAggregate: contract.SessionUsageAggregate{Requests: 1, UnavailableRequests: 1}})
	if !strings.Contains(unavailable, "Cache read / uncached: unavailable") || strings.Contains(unavailable, "Cache read / uncached: 0 / 0") {
		t.Fatalf("unavailable report=%s", unavailable)
	}
}

// TestFormatUsageCreditsOnly locks in the credits-only /usage modal: plan
// windows convert USD budgets at the plan rate (2,500 credits = $25) and render
// total / used / remaining / percentage with a progress bar — never dollars.
func TestFormatUsageCreditsOnly(t *testing.T) {
	out := formatUsage(&UsageData{
		PlanName:       "Pro",
		Windows:        []UsageWindow{{Name: "monthly", BudgetUSD: 25, CurrentSpentUSD: 10, DurationSeconds: 2592000}},
		ExtraTotal:     500,
		ExtraRemaining: 400,
		SpendTodayUSD:  1.25,
	})
	for _, expected := range []string{"Plan: Pro", "Total:     2500 credits", "Used:      1000 credits", "Remaining: 1500 credits", "40.0% used", "[", "█", "░", "Extra credits", "Remaining: 400 credits"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("usage modal missing %q:\n%s", expected, out)
		}
	}
	if strings.Contains(out, "$") {
		t.Fatalf("usage modal displayed dollars:\n%s", out)
	}
}

func TestContextReportShowsRatesAndInvalidations(t *testing.T) {
	session, steady := .75, .9
	report := formatContextReport(orchestrator.ContextReport{
		HistoryTokens: 10, ContextLimit: 100, PressureTokens: 20, PressurePercent: 20,
		UsageAggregate: contract.SessionUsageAggregate{SumPrompt: 100, SumCompletion: 10, SumCacheRead: 90, SumCacheMiss: 10, CacheAvailable: 1, SessionHitRate: &session, SteadyStateHitRate: &steady},
		Invalidations:  []contract.InvalidationEvent{{At: time.Now(), Cause: contract.InvalidationToolsetChange, Scope: "mcp add demo"}},
	})
	for _, expected := range []string{"Cache read / uncached: 90 / 10", "Session hit rate:      75.00%", "Steady-state hit rate: 90.00%", "toolset-change: mcp add demo"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report missing %q:\n%s", expected, report)
		}
	}
}
