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
	//
	// 013: with no cache-reporting request the card omits the cache line entirely
	// and the hit rate reads "unavailable" — never a fabricated zero.
	unavailable := formatContextReport(orchestrator.ContextReport{ContextLimit: 128000, UsageAggregate: contract.SessionUsageAggregate{Requests: 1, UnavailableRequests: 1}}, "")
	if !strings.Contains(unavailable, "Hit rate:  unavailable") {
		t.Fatalf("hit rate must read unavailable:\n%s", unavailable)
	}
	if strings.Contains(unavailable, "Cache:") || strings.Contains(unavailable, "0 / 0") {
		t.Fatalf("no cache line should render without cache-reporting requests:\n%s", unavailable)
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

// TestContextCardShowsTheAllStreamRate (013 FR-021/FR-023): the displayed rate
// is the all-stream one, and the diagnostics that used to crowd this modal —
// invalidation history above all — are gone.
func TestContextCardShowsTheAllStreamRate(t *testing.T) {
	allStream, mainOnly, steady := .62, .75, .9
	report := formatContextReport(orchestrator.ContextReport{
		HistoryTokens: 10, ContextLimit: 100, PressureTokens: 20, PressurePercent: 20,
		UsageAggregate: contract.SessionUsageAggregate{
			SumPrompt: 100, SumCompletion: 10, SumCacheRead: 90, SumCacheMiss: 10, CacheAvailable: 1,
			AllStreamHitRate: &allStream, SessionHitRate: &mainOnly, SteadyStateHitRate: &steady,
		},
		Invalidations: []contract.InvalidationEvent{{At: time.Now(), Cause: contract.InvalidationToolsetChange, Scope: "mcp add demo"}},
		WarmModels:    []string{"Session Model", "Other Model"},
	}, "Session Model")
	for _, expected := range []string{"Cache:     90 read / 10 uncached", "Hit rate:  62.00%", "Running:   Session Model", "Warm:      Session Model, Other Model"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("card missing %q:\n%s", expected, report)
		}
	}
	// The main-only and steady-state rates are benchmark KPIs, not user-facing.
	for _, gone := range []string{"75.00%", "90.00%", "toolset-change", "Steady-state", "Pressure"} {
		if strings.Contains(report, gone) {
			t.Fatalf("card still renders %q:\n%s", gone, report)
		}
	}
}
