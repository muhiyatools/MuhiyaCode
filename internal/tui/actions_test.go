package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

func TestDisplayHonestyUnavailableAndReportedZero(t *testing.T) {
	unavailable := formatContextReport(orchestrator.ContextReport{ContextLimit: 128000, UsageAggregate: contract.SessionUsageAggregate{Requests: 1, UnavailableRequests: 1}})
	if !strings.Contains(unavailable, "Cache read / uncached: unavailable") || strings.Contains(unavailable, "Cache read / uncached: 0 / 0") {
		t.Fatalf("unavailable report=%s", unavailable)
	}
	zero := 0
	tag := cacheTag(contract.Usage{CacheReadTokens: &zero, CacheMissTokens: &zero, MissDerived: true})
	if !strings.Contains(tag, "0 read") || !strings.Contains(tag, "0 new derived") || strings.Contains(tag, "unavailable") {
		t.Fatalf("zero tag=%q", tag)
	}
	if tag := cacheTag(contract.Usage{}); tag != "" {
		t.Fatalf("cache-less endpoint fabricated tag %q", tag)
	}
}

func TestContextReportShowsRatesAndInvalidations(t *testing.T) {
	session, steady := .75, .9
	report := formatContextReport(orchestrator.ContextReport{
		HistoryTokens: 10, ContextLimit: 100, PressureTokens: 20, PressurePercent: 20,
		UsageAggregate: contract.SessionUsageAggregate{SumPrompt: 100, SumCompletion: 10, SumCacheRead: 90, SumCacheMiss: 10, CacheAvailable: 1, SessionHitRate: &session, SteadyStateHitRate: &steady},
		Invalidations:  []contract.InvalidationEvent{{At: time.Now(), Cause: contract.InvalidationToolsetChange, Scope: "mcp add demo"}},
	})
	for _, expected := range []string{"Cache read / uncached: 90 / 10", "Session hit rate: 75.00%", "Steady-state hit rate: 90.00%", "toolset-change: mcp add demo"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report missing %q:\n%s", expected, report)
		}
	}
}
