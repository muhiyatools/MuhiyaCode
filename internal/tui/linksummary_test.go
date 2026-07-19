package tui

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 012 US5: the task-summary reuse roll-up renders continued counts
// with the mean provider-verified share, omits the share when unreported, and
// adds nothing when no dispatch continued.
func TestLinkSummaryPart(t *testing.T) {
	share := 0.9
	links := []contract.LinkOutcome{
		{Decision: "continued", CacheShare: &share},
		{Decision: "digest-seeded"},
		{Decision: "continued"},
	}
	part := linkSummaryPart(links)
	if !strings.Contains(part, "links 2/3 continued") || !strings.Contains(part, "cache 90%") {
		t.Fatalf("roll-up wrong: %q", part)
	}
	if linkSummaryPart([]contract.LinkOutcome{{Decision: "fresh"}}) != "" {
		t.Fatal("no continued links must render nothing")
	}
	if part := linkSummaryPart([]contract.LinkOutcome{{Decision: "continued"}}); strings.Contains(part, "cache") {
		t.Fatalf("unreported shares must be omitted, never estimated: %q", part)
	}
}
