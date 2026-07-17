package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// Feature 008 T026 — /context panel rewrite (contracts/usage-display.md §2–§4).

// contextPanelReport builds a hand-rolled fixture: two per-model rows (one with
// a nil cost), categories summing to the context limit, API/active time and
// lines± set, and session credits under the priced/eligible fallback.
func contextPanelReport() orchestrator.ContextReport {
	session, steady := 0.75, 0.9
	cost := 0.004 // 0.40 credits at 100 credits/USD
	return orchestrator.ContextReport{
		HistoryTokens: 90_000, ContextLimit: 100_000, Percent: 90,
		PressureTokens: 90_000, PressurePercent: 90,
		SessionCreditsPriced: 1, SessionCreditsEligible: 3,
		APITimeMS: 83_000, ActiveMS: 754_000, LinesAdded: 120, LinesRemoved: 45,
		UsageAggregate: contract.SessionUsageAggregate{
			Requests: 15, MainRequests: 12, SubagentRequests: 3,
			SumPrompt: 945_000, SumCompletion: 9_200,
			SumCacheRead: 900_000, SumCacheMiss: 45_000, CacheAvailable: 12,
			SessionHitRate: &session, SteadyStateHitRate: &steady,
		},
		ByModel: []contract.ModelUsageRow{
			{Model: "reasonix-pro", Requests: 12, UncachedIn: 40_000, Output: 8_000, CacheRead: 900_000, CacheAvailable: true, CostUSD: &cost},
			{Model: "flash", Requests: 3, UncachedIn: 5_000, Output: 1_200, CacheRead: 0, CacheAvailable: true, CostUSD: nil},
		},
		Categories: []orchestrator.ContextCategory{
			{Name: "system prompt", Tokens: 3_000, Percent: 3},
			{Name: "tool definitions", Tokens: 9_000, Percent: 9},
			{Name: "project memory & skills", Tokens: 8_000, Percent: 8},
			{Name: "conversation", Tokens: 70_000, Percent: 70},
			{Name: "free", Tokens: 10_000, Percent: 10},
		},
	}
}

// sectionIndex fails the test when the section header is absent.
func sectionIndex(t *testing.T, report, header string) int {
	t.Helper()
	index := strings.Index(report, header)
	if index < 0 {
		t.Fatalf("report missing section %q:\n%s", header, report)
	}
	return index
}

// UD-14: the session panel renders first, then window, models, categories,
// streams, cache health — so clipping drops detail before headlines.
func TestContextReportSectionOrder(t *testing.T) {
	report := formatContextReport(contextPanelReport())
	order := []string{"This session", "Context window", "By model", "Context by category (estimated)", "Streams", "Cache health"}
	last := -1
	for _, header := range order {
		index := sectionIndex(t, report, header)
		if index <= last {
			t.Fatalf("section %q out of order:\n%s", header, report)
		}
		last = index
	}
}

// UD-6: cost, API time, active time (session-labeled), lines±, and both
// cache-hit rates render inside the session panel.
func TestContextReportSessionPanelContent(t *testing.T) {
	report := formatContextReport(contextPanelReport())
	for _, expected := range []string{
		"  Credits used: unavailable (1 of 3 requests priced)",
		"  API time:    1m23s",
		"  Active time: 12m34s (this session)",
		"  Lines: +120 −45",
		"  Prompt / output tokens: 945.0k / 9.2k",
		"  Cache read / uncached: 900.0k / 45.0k",
		"  Session hit rate:      75.00%",
		"  Steady-state hit rate: 90.00%",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("session panel missing %q:\n%s", expected, report)
		}
	}
}

// UD-7/UD-8: one row per model plus a Total; a nil row cost renders
// "unavailable" and suppresses the Total cost (member-set rule).
func TestContextReportByModelRowsAndTotal(t *testing.T) {
	report := formatContextReport(contextPanelReport())
	var modelLines []string
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "in ") && strings.Contains(line, "cost ") {
			modelLines = append(modelLines, line)
		}
	}
	if len(modelLines) != 3 {
		t.Fatalf("by-model lines = %d, want 3 (2 rows + Total):\n%s", len(modelLines), report)
	}
	for i, expected := range []string{
		"reasonix-pro", "flash", "Total",
	} {
		if !strings.Contains(modelLines[i], expected) {
			t.Fatalf("row %d missing %q: %q", i, expected, modelLines[i])
		}
	}
	if !strings.Contains(modelLines[0], "in 40.0k · out 8.0k · read 900.0k · cost 0.40") {
		t.Fatalf("priced row = %q", modelLines[0])
	}
	if !strings.Contains(modelLines[1], "cost unavailable") {
		t.Fatalf("nil-cost row did not render unavailable: %q", modelLines[1])
	}
	if !strings.Contains(modelLines[2], "in 45.0k · out 9.2k · read 900.0k · cost unavailable") {
		t.Fatalf("Total row = %q (cost must be unavailable when any row's is)", modelLines[2])
	}
}

// The Total cost sums only when every row carries one.
func TestContextReportTotalCostWhenAllRowsPriced(t *testing.T) {
	fixture := contextPanelReport()
	other := 0.001 // 0.10 credits
	fixture.ByModel[1].CostUSD = &other
	report := formatContextReport(fixture)
	if !strings.Contains(report, "Total") || !strings.Contains(report, "cost 0.50") {
		t.Fatalf("all-priced Total cost missing (0.40 + 0.10 = 0.50 credits):\n%s", report)
	}
	if strings.Contains(report, "cost unavailable") {
		t.Fatalf("all-priced table still rendered an unavailable cost:\n%s", report)
	}
}

// UD-10: the category table renders name / tokens / percent per row and the
// single "(estimated)" marker lives in the section header.
func TestContextReportCategories(t *testing.T) {
	report := formatContextReport(contextPanelReport())
	for _, expected := range []string{
		"Context by category (estimated)",
		"system prompt",
		"project memory & skills",
		"conversation",
		"free",
		"70.0k",
		"70.0%",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("categories missing %q:\n%s", expected, report)
		}
	}
	if strings.Count(report, "(estimated)") != 1 {
		t.Fatalf("\"(estimated)\" must appear exactly once (in the header):\n%s", report)
	}
}

// UD-13: every rendered line fits the modal's 72 usable columns — including
// the by-model table with a pathologically long model ID, which is clamped.
func TestContextReportLinesFitModalWidth(t *testing.T) {
	fixture := contextPanelReport()
	fixture.ByModel[0].Model = strings.Repeat("very-long-model-id-", 4) // 76 chars
	report := formatContextReport(fixture)
	for _, line := range strings.Split(report, "\n") {
		if width := lipgloss.Width(line); width > 72 {
			t.Fatalf("line exceeds 72 columns (%d): %q", width, line)
		}
	}
	if !strings.Contains(report, "…") {
		t.Fatalf("over-wide model ID was not clamped:\n%s", report)
	}
}

// UD-8: absent sources render "unavailable" — never zeros — and zero lines±
// renders "Lines: none". Active time restarts at zero on resume (UD-9), so
// zero is a true value there and renders as a duration.
func TestContextReportUnavailableStates(t *testing.T) {
	report := formatContextReport(orchestrator.ContextReport{ContextLimit: 128_000})
	for _, expected := range []string{
		"  Credits used: unavailable",
		"  API time:    unavailable",
		"  Active time: 0.0s (this session)",
		"  Lines: none",
		"  Cache read / uncached: unavailable",
		"  Session hit rate:      unavailable",
		"  Steady-state hit rate: unavailable",
		"  Prefix stability rate: unavailable",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("empty report missing %q:\n%s", expected, report)
		}
	}
	// No by-model or category sections when the engine reported none.
	for _, absent := range []string{"By model", "Context by category"} {
		if strings.Contains(report, absent) {
			t.Fatalf("empty report rendered %q:\n%s", absent, report)
		}
	}
}
