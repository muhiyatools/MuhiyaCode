package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// Feature 008 T026 built a nine-section diagnostic panel here. 013 FR-023
// replaced it with an essentials card, so these tests now pin what the card
// SHOWS and — just as importantly — what it no longer shows.

// contextPanelReport is a full engine report: the card must ignore most of it.
func contextPanelReport() orchestrator.ContextReport {
	allStream, session, steady := 0.95, 0.75, 0.9
	cost := 0.004 // 0.40 credits at 100 credits/USD
	return orchestrator.ContextReport{
		HistoryTokens: 90_000, ContextLimit: 100_000, Percent: 90,
		PressureTokens: 90_000, PressurePercent: 90,
		SessionCreditsPriced: 1, SessionCreditsEligible: 3,
		APITimeMS: 83_000, ActiveMS: 754_000, LinesAdded: 120, LinesRemoved: 45,
		UsageAggregate: contract.SessionUsageAggregate{
			Requests: 15, MainRequests: 12, AuxRequests: 3,
			SumPrompt: 945_000, SumCompletion: 9_200,
			SumCacheRead: 900_000, SumCacheMiss: 45_000, CacheAvailable: 12,
			AllStreamHitRate: &allStream, SessionHitRate: &session, SteadyStateHitRate: &steady,
		},
		ByModel: []contract.ModelUsageRow{
			{Model: "reasonix-pro", Requests: 12, UncachedIn: 40_000, Output: 8_000, CacheRead: 900_000, CacheAvailable: true, CostUSD: &cost},
			{Model: "flash", Requests: 3, UncachedIn: 5_000, Output: 1_200, CacheRead: 0, CacheAvailable: true, CostUSD: nil},
		},
		WarmModels: []string{"Reasonix Pro"},
		Categories: []orchestrator.ContextCategory{
			{Name: "system prompt", Tokens: 3_000, Percent: 3},
			{Name: "conversation", Tokens: 70_000, Percent: 70},
		},
	}
}

// The card is three groups in a fixed order, so a user always finds a figure in
// the same place.
func TestContextCardGroupOrder(t *testing.T) {
	report := formatContextReport(contextPanelReport(), "Reasonix Pro")
	last := -1
	for _, header := range []string{"Context", "Session", "Model"} {
		index := strings.Index(report, "\n"+header)
		if header == "Context" {
			index = strings.Index(report, header)
		}
		if index < 0 {
			t.Fatalf("card missing group %q:\n%s", header, report)
		}
		if index <= last {
			t.Fatalf("group %q out of order:\n%s", header, report)
		}
		last = index
	}
}

// FR-019/FR-020: complete counts, comma-grouped, never abbreviated.
func TestContextCardShowsFullCacheInclusiveNumbers(t *testing.T) {
	report := formatContextReport(contextPanelReport(), "Reasonix Pro")
	for _, expected := range []string{
		"  In use:  90,000 of 100,000 tokens (90.0%)",
		"  Free:    10,000 tokens",
		"  Tokens:    945,000 in / 9,200 out",
		"  Cache:     900,000 read / 45,000 uncached",
		"  Hit rate:  95.00%",
		"  Running:   Reasonix Pro",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("card missing %q:\n%s", expected, report)
		}
	}
	for _, abbreviated := range []string{"945.0k", "900.0k", "9.2k", "90.0k"} {
		if strings.Contains(report, abbreviated) {
			t.Fatalf("card still abbreviates (%q):\n%s", abbreviated, report)
		}
	}
}

// FR-023: everything else is gone. This is the test that keeps the modal from
// silently re-accumulating diagnostics.
func TestContextCardDropsDiagnostics(t *testing.T) {
	report := formatContextReport(contextPanelReport(), "Reasonix Pro")
	for _, absent := range []string{
		"By model", "reasonix-pro", "Context by category", "system prompt", "conversation",
		"Streams", "Cache health", "Prefix stability", "Steady-state", "Pressure",
		"API time", "Active time", "Lines:", "Requests:",
	} {
		if strings.Contains(report, absent) {
			t.Fatalf("card still renders dropped detail %q:\n%s", absent, report)
		}
	}
}

// FR-023 bounds the card: it must stay scannable at a glance.
func TestContextCardStaysCompact(t *testing.T) {
	report := formatContextReport(contextPanelReport(), "Reasonix Pro")
	lines := strings.Split(report, "\n")
	content := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			content++
		}
		if width := lipgloss.Width(line); width > 72 {
			t.Fatalf("line exceeds the modal's 72 columns (%d): %q", width, line)
		}
	}
	if content > 14 {
		t.Fatalf("card has %d content lines, contract caps it at 14:\n%s", content, report)
	}
}

// Cost follows the established member-set honesty rule: priced, unavailable
// with counts, or (when nothing is eligible) omitted entirely.
func TestContextCardCostHonesty(t *testing.T) {
	priced := contextPanelReport()
	cost := 0.05
	priced.SessionCreditsUSD, priced.SessionCreditsEstimated = &cost, true
	if got := formatContextReport(priced, "m"); !strings.Contains(got, "Cost:      ~5 credits") {
		t.Fatalf("estimated cost should carry the ~ marker:\n%s", got)
	}

	partial := formatContextReport(contextPanelReport(), "m")
	if !strings.Contains(partial, "Cost:      unavailable (1 of 3 requests priced)") {
		t.Fatalf("partial pricing must say so:\n%s", partial)
	}

	none := formatContextReport(orchestrator.ContextReport{ContextLimit: 128_000}, "m")
	if strings.Contains(none, "Cost:") {
		t.Fatalf("with nothing priced the cost line is omitted:\n%s", none)
	}
}

// FR-022: absent sources read "unavailable", never a stand-in zero.
func TestContextCardUnavailableStates(t *testing.T) {
	report := formatContextReport(orchestrator.ContextReport{ContextLimit: 128_000}, "")
	if !strings.Contains(report, "Hit rate:  unavailable") {
		t.Fatalf("empty session must report an unavailable rate:\n%s", report)
	}
	if strings.Contains(report, "Models") {
		t.Fatalf("no model names means no Models group:\n%s", report)
	}
	if !strings.Contains(report, "In use:  0 of 128,000 tokens") {
		t.Fatalf("a genuine zero is still a real value:\n%s", report)
	}
}
