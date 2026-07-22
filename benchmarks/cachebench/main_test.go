package main

import (
	"os"
	"path/filepath"
	"strings"
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
		Validity:            &runValidity{Valid: true, UsageAvailable: true, RubricAvailable: true, ConfigurationHash: "frozen-config"},
	}
}

func TestComparisonRejectsInvalidEvidenceAndConfigurationDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runResult, *runResult)
		want   string
	}{
		{
			name: "missing provider usage",
			mutate: func(_, improved *runResult) {
				improved.Validity.UsageAvailable = false
			},
			want: "provider usage",
		},
		{
			name: "missing rubric output",
			mutate: func(_, improved *runResult) {
				improved.Validity.RubricAvailable = false
			},
			want: "rubric",
		},
		{
			name: "configuration drift",
			mutate: func(_, improved *runResult) {
				improved.Validity.ConfigurationHash = "different-config"
			},
			want: "configuration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseline := armRun("test", "off", 1, 0.98)
			improved := armRun("test", "observe", 1, 0.99)
			test.mutate(&baseline, &improved)
			_, err := compareScenario("test", []runResult{baseline}, []runResult{improved})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("error=%v, want rejection containing %q", err, test.want)
			}
		})
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

func TestLoadFeature014FixtureMatricesAndRubrics(t *testing.T) {
	root := filepath.Join("..", "..", "specs", "014-token-economy-overhaul", "benchmarks", "fixtures")
	fixtures, err := loadFeature014Fixtures(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(fixtures.TicTacToe.Tasks); got != 2 {
		t.Fatalf("tic-tac-toe tasks=%d, want 2", got)
	}
	if got := len(fixtures.SmallTasks.Tasks); got != 20 {
		t.Fatalf("small tasks=%d, want 20", got)
	}
	if got := len(fixtures.ContextEpochs.Sequences); got != 5 {
		t.Fatalf("context sequences=%d, want 5", got)
	}
	if got := len(fixtures.EvidenceResults.Cases); got != 12 {
		t.Fatalf("evidence cases=%d, want 12", got)
	}
	for _, task := range append(append([]fixtureTask(nil), fixtures.TicTacToe.Tasks...), fixtures.SmallTasks.Tasks...) {
		if task.Rubric == nil || len(task.Rubric.Checks) == 0 {
			t.Fatalf("task %q has no executable rubric", task.ID)
		}
		for _, check := range task.Rubric.Checks {
			if strings.TrimSpace(check.Kind) == "" {
				t.Fatalf("task %q has rubric check without kind", task.ID)
			}
		}
	}
}

func TestFixtureLoaderRejectsUnknownAndIncompleteSchemas(t *testing.T) {
	tests := map[string]string{
		"unknown field":   `{"schema_version":1,"suite":"x","tasks":[],"surprise":true}`,
		"missing version": `{"suite":"x","tasks":[]}`,
		"missing suite":   `{"schema_version":1,"tasks":[]}`,
		"missing task id": `{"schema_version":1,"suite":"x","tasks":[{"prompt":"do it","task_class":"tiny","rubric":{"checks":[{"kind":"file_exists","path":"x"}]}}]}`,
		"missing prompt":  `{"schema_version":1,"suite":"x","tasks":[{"id":"x","task_class":"tiny","rubric":{"checks":[{"kind":"file_exists","path":"x"}]}}]}`,
		"missing rubric":  `{"schema_version":1,"suite":"x","tasks":[{"id":"x","prompt":"do it","task_class":"tiny"}]}`,
		"empty rubric":    `{"schema_version":1,"suite":"x","tasks":[{"id":"x","prompt":"do it","task_class":"tiny","rubric":{"checks":[]}}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadFixtureSuite(path); err == nil {
				t.Fatal("expected invalid fixture to be rejected")
			}
		})
	}
}

func TestRubricLoaderPreservesTypedCommandsAndRegex(t *testing.T) {
	body := `{"schema_version":1,"suite":"x","tasks":[{"id":"x","prompt":"do it","task_class":"small","rubric":{"required":true,"checks":[{"id":"test","kind":"command","argv":["go","test","./..."]},{"id":"source","kind":"source_regex","paths":["a.go"],"pattern":"Name\\(\\)"}]}}]}`
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	suite, err := loadFixtureSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	checks := suite.Tasks[0].Rubric.Checks
	if got := strings.Join(checks[0].Argv, " "); got != "go test ./..." {
		t.Fatalf("command argv=%q", got)
	}
	if checks[1].Pattern != `Name\(\)` || len(checks[1].Paths) != 1 {
		t.Fatalf("regex check=%+v", checks[1])
	}
}

func TestEconomyAcceptanceTargetsExcludeRetriesAndRequireCorrectness(t *testing.T) {
	samples := []economySample{
		{TaskID: "tiny-1", TaskClass: "tiny", MainRequests: 4, RetryRequests: 1, Completed: true, Correct: true},
		{TaskID: "tiny-2", TaskClass: "tiny", MainRequests: 3, Completed: true, Correct: true},
		{TaskID: "tiny-3", TaskClass: "tiny", MainRequests: 4, Completed: true, Correct: true},
		{TaskID: "small-1", TaskClass: "small", MainRequests: 6, RetryRequests: 1, Completed: true, Correct: true},
		{TaskID: "small-2", TaskClass: "small", MainRequests: 5, Completed: true, Correct: true},
		{TaskID: "tic-tac-toe", TaskClass: "small", MainRequests: 6, Completed: true, Correct: true, TicTacToe: true},
	}
	acceptance := evaluateEconomyAcceptance(samples)
	if !acceptance.SC001 || !acceptance.SC002 || !acceptance.SC005 || !acceptance.Correctness {
		t.Fatalf("valid economy matrix rejected: %+v", acceptance)
	}

	samples[len(samples)-1].Correct = false
	regressed := evaluateEconomyAcceptance(samples)
	if regressed.SC005 || regressed.Correctness {
		t.Fatalf("incorrect tic-tac-toe run passed: %+v", regressed)
	}
}

func TestEconomyAcceptanceRejectsRequestRegression(t *testing.T) {
	samples := []economySample{
		{TaskID: "tiny-1", TaskClass: "tiny", MainRequests: 5, Completed: true, Correct: true},
		{TaskID: "tiny-2", TaskClass: "tiny", MainRequests: 5, Completed: true, Correct: true},
		{TaskID: "small-1", TaskClass: "small", MainRequests: 8, Completed: true, Correct: true},
	}
	acceptance := evaluateEconomyAcceptance(samples)
	if acceptance.SC001 || acceptance.SC002 {
		t.Fatalf("request regression passed: %+v", acceptance)
	}
}

func TestOutputEconomyAcceptanceRequiresHalfReductionWithoutMoreIncompleteResponses(t *testing.T) {
	samples := []outputEconomySample{
		{TaskID: "a", BaselineOutputTokens: 4000, CandidateOutputTokens: 1800, Correct: true},
		{TaskID: "b", BaselineOutputTokens: 2000, CandidateOutputTokens: 900, Correct: true},
	}
	accepted := evaluateOutputEconomy(samples)
	if !accepted.SC004 || !accepted.NoTruncationRegression || !accepted.Correctness {
		t.Fatalf("valid output reduction rejected: %+v", accepted)
	}
	samples[1].CandidateTruncated = true
	regressed := evaluateOutputEconomy(samples)
	if regressed.SC004 || regressed.NoTruncationRegression {
		t.Fatalf("truncation regression accepted: %+v", regressed)
	}
}
