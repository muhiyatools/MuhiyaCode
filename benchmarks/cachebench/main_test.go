package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestLoadWorkloadAndDerivedCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workload.md")
	body := "# test\n\n1. first\n2. second\n   expect: done\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	turns, err := loadWorkload(path)
	if err != nil || len(turns) != 2 || turns[1].Prompt != "second" {
		t.Fatalf("turns=%+v err=%v", turns, err)
	}
	// The expect: line attaches a completion check to its preceding prompt only.
	if turns[1].Expect != "done" || turns[0].Expect != "" {
		t.Fatalf("expect parsing: turns=%+v", turns)
	}
	aggregate := contract.SessionUsageAggregate{SumCacheMiss: 1_000_000, SumCacheRead: 2_000_000, SumCompletion: 500_000}
	cost := deriveCost(aggregate, priceTable{UncachedInputPerMillion: 2, CacheReadPerMillion: .2, OutputPerMillion: 4, Source: "test"})
	if cost == nil || *cost != 4.4 {
		t.Fatalf("cost=%v", cost)
	}
}

func TestCountUnattributedRequiresLedgerForAgentChange(t *testing.T) {
	miss := 5
	records := []contract.UsageRecord{
		{Seq: 1, CacheMissTokens: &miss, Attribution: contract.CacheAttributionColdStart},
		{Seq: 2, CacheMissTokens: &miss, Attribution: contract.CacheAttributionAgent},
		{Seq: 3, CacheMissTokens: &miss, Attribution: contract.CacheAttributionProvider},
	}
	if got := countUnattributed(records, nil); got != 1 {
		t.Fatalf("unattributed=%d", got)
	}
	events := []contract.InvalidationEvent{{RequestSeq: 2}}
	if got := countUnattributed(records, events); got != 0 {
		t.Fatalf("unattributed with event=%d", got)
	}
}

// armRun builds a minimal runResult for one A/B arm with the fields the
// comparison logic reads: prefix-stability rate and invalidation causes.
func armRun(scenario, arm string, runIndex int, prefix float64) runResult {
	rate := prefix
	return runResult{
		Scenario: scenario, Model: "m", Effort: contract.EffortLow, RunIndex: runIndex,
		Aggregate:           contract.SessionUsageAggregate{PrefixStabilityRate: &rate},
		InvalidationByCause: map[string]int{},
	}
}

func TestSC005RequiresImprovementAndLowVariance(t *testing.T) {
	baseline := []runResult{armRun("test", "off", 1, 0.98)}
	improved := []runResult{armRun("test", "on", 1, 0.9995)}
	entry, err := compareScenario("test", baseline, improved)
	if err != nil || !entry.MeetsSC005 {
		t.Fatalf("improvement did not meet SC-005: entry=%+v err=%v", entry, err)
	}

	// A regressed improved arm (below the steady-state target) must fail SC-005.
	regressedImproved := []runResult{armRun("test", "on", 1, 0.95)}
	regressed, err := compareScenario("test", baseline, regressedImproved)
	if err != nil {
		t.Fatal(err)
	}
	if regressed.MeetsSC005 {
		t.Fatalf("regression passed SC-005: %+v", regressed)
	}
}

func TestParseOptionsRunValidation(t *testing.T) {
	cases := map[string][]string{
		"missing out":      {"-build-label", "improved"},
		"bad build-label":  {"-build-label", "nope", "-out", "out"},
		"runs below one":   {"-build-label", "improved", "-out", "out", "-runs", "0"},
		"unknown scenario": {"-build-label", "improved", "-out", "out", "-scenario", "mystery"},
	}
	for name, args := range cases {
		if _, _, err := parseOptions(args); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}
