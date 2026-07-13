package main

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

// TestFixtureDeterminism verifies that the same seed yields the same fixture
// hash both at the logical level and after materializing to a real database and
// sidecars (contract §7: fixed-seed fixture; harness test: same seed ⇒ same
// fixture hash).
func TestFixtureDeterminism(t *testing.T) {
	const seed = 42
	const events = 400

	first := buildFixtureData(seed, events)
	second := buildFixtureData(seed, events)
	if hashFixtureData(first) != hashFixtureData(second) {
		t.Fatalf("logical fixture hash differs for identical seed")
	}

	ctx := context.Background()
	one, err := GenerateFixture(ctx, t.TempDir(), seed, events)
	if err != nil {
		t.Fatalf("generate fixture one: %v", err)
	}
	two, err := GenerateFixture(ctx, t.TempDir(), seed, events)
	if err != nil {
		t.Fatalf("generate fixture two: %v", err)
	}
	if one.Hash != two.Hash {
		t.Fatalf("materialized fixture hash differs: %s vs %s", one.Hash, two.Hash)
	}
	if one.Hash != hashFixtureData(first) {
		t.Fatalf("materialized hash %s != logical hash %s", one.Hash, hashFixtureData(first))
	}
	if one.EventCount != events || one.PayloadBytes <= 0 {
		t.Fatalf("unexpected fixture size: events=%d payload=%d", one.EventCount, one.PayloadBytes)
	}
	// A different seed must produce a different hash.
	if diff := buildFixtureData(seed+1, events); hashFixtureData(diff) == one.Hash {
		t.Fatalf("different seed produced identical hash")
	}
}

// TestFixtureContainsRequiredContent verifies the fixture populates the mixed
// event roles and oversized entries the contract §7 requires.
func TestFixtureContainsRequiredContent(t *testing.T) {
	data := buildFixtureData(7, 5000)
	if len(data.Events) != 5000 {
		t.Fatalf("event count = %d, want 5000", len(data.Events))
	}
	roles := map[string]bool{}
	large := false
	for _, event := range data.Events {
		roles[event.Role] = true
		if event.Large {
			large = true
		}
	}
	for _, want := range []string{"user", "assistant", "tool", "agent", "status", "error"} {
		if !roles[want] {
			t.Fatalf("fixture missing role %q", want)
		}
	}
	if !large {
		t.Fatalf("fixture contains no oversized entries")
	}
	if len(data.History.Messages) == 0 || len(data.Knowledge.Facts) == 0 || len(data.Usage) == 0 || len(data.Invalidations) == 0 {
		t.Fatalf("fixture missing a required sidecar: history=%d knowledge=%d usage=%d invalidations=%d",
			len(data.History.Messages), len(data.Knowledge.Facts), len(data.Usage), len(data.Invalidations))
	}
	// history.json must carry tool-call / tool-result pairs.
	pairedCall := false
	for _, message := range data.History.Messages {
		if len(message.ToolCalls) > 0 {
			pairedCall = true
		}
	}
	if !pairedCall {
		t.Fatalf("history missing tool-call/result pairs")
	}
}

// TestResultSchemaCompleteness verifies every contract §7 field is present in the
// serialized Result and populated (or honestly zero/empty/"n/a").
func TestResultSchemaCompleteness(t *testing.T) {
	opts := options{scenario: "input", runs: 5, seed: 3, eventCount: 200, buildLabel: "baseline", outDir: t.TempDir()}
	result, err := runScenario(context.Background(), opts, "input")
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	required := []string{
		"scenario", "run_count",
		"fixture_seed", "fixture_hash", "event_count", "payload_bytes",
		"build_label", "commit", "dirty", "go_version", "os", "arch",
		"machine_cpu_count", "machine_ram_bytes",
		"terminal_name", "terminal_version", "terminal_width", "terminal_height",
		"theme", "color_mode",
		"raw_samples_ms", "p50_ms", "p95_ms", "p99_ms",
		"frame_count", "dirty_pane_count", "rendered_bytes", "completed_row_hash_violations",
		"process_cpu_seconds", "working_set_samples_bytes", "rss_samples_bytes",
		"go_heap_samples_bytes", "go_allocation_samples_bytes",
		"errors", "crashes", "stale_page_drops", "duplicate_event_count", "missing_event_count",
	}
	for _, field := range required {
		if _, ok := generic[field]; !ok {
			t.Fatalf("result JSON missing required contract §7 field %q", field)
		}
	}

	if result.RunCount != 5 || len(result.RawSamplesMS) != 5 {
		t.Fatalf("expected 5 samples, got run_count=%d samples=%d", result.RunCount, len(result.RawSamplesMS))
	}
	if result.FixtureHash == "" || result.EventCount != 200 || result.PayloadBytes <= 0 {
		t.Fatalf("fixture identity not populated: %+v", result)
	}
	if result.GoVersion == "" || result.OS == "" || result.Arch == "" || result.MachineCPUCount <= 0 {
		t.Fatalf("environment metadata not populated: %+v", result)
	}
	if len(result.WorkingSetSamples) == 0 || len(result.GoHeapSamples) == 0 || len(result.GoAllocationSamples) == 0 {
		t.Fatalf("memory samples not populated")
	}
	if result.DuplicateEvents != 0 || result.MissingEvents != 0 {
		t.Fatalf("clean fixture reported dup=%d missing=%d", result.DuplicateEvents, result.MissingEvents)
	}
	if !result.BehaviorNeutral || len(result.UnmeasuredFields) == 0 {
		t.Fatalf("behavior-neutral honesty markers missing")
	}
}

// TestStreamPreservesCompletedRowHashes verifies the stream scenario's
// completed-row-hash invariant holds (contract §4/§8): a streaming update to the
// active block must not change any completed entry's row hash.
func TestStreamScenarioNoHashViolations(t *testing.T) {
	result, err := runScenario(context.Background(), options{runs: 3, seed: 9, eventCount: 500, buildLabel: "improved", outDir: t.TempDir()}, "stream")
	if err != nil {
		t.Fatalf("run stream scenario: %v", err)
	}
	if result.CompletedRowHashViolations != 0 {
		t.Fatalf("stream scenario reported %d completed-row-hash violations", result.CompletedRowHashViolations)
	}
}

// TestPercentileCorrectness verifies the p50/p95/p99 helper against known inputs.
func TestPercentileCorrectness(t *testing.T) {
	if got := percentile(nil, 95); got != 0 {
		t.Fatalf("empty percentile = %v, want 0", got)
	}
	single := []float64{4.2}
	if got := percentile(single, 99); got != 4.2 {
		t.Fatalf("single percentile = %v, want 4.2", got)
	}
	// Sorted 1..100: linear-interpolation ranks.
	sorted := make([]float64, 100)
	for i := range sorted {
		sorted[i] = float64(i + 1)
	}
	cases := []struct {
		p    float64
		want float64
	}{
		{50, 50.5},
		{95, 95.05},
		{99, 99.01},
		{0, 1},
		{100, 100},
	}
	for _, c := range cases {
		if got := percentile(sorted, c.p); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

// TestReconcileEventsDetectsDuplicatesAndGaps guards the integrity counters.
func TestReconcileEventsDetectsDuplicatesAndGaps(t *testing.T) {
	clean := []genEvent{{Index: 0}, {Index: 1}, {Index: 2}}
	if dup, missing := reconcileEvents(clean); dup != 0 || missing != 0 {
		t.Fatalf("clean events reported dup=%d missing=%d", dup, missing)
	}
	broken := []genEvent{{Index: 0}, {Index: 0}, {Index: 2}}
	if dup, missing := reconcileEvents(broken); dup != 1 || missing != 1 {
		t.Fatalf("broken events reported dup=%d missing=%d, want 1/1", dup, missing)
	}
}

// TestAllScenariosRun exercises every registered scenario end to end.
func TestAllScenariosRun(t *testing.T) {
	for _, scenario := range allScenarios {
		result, err := runScenario(context.Background(), options{runs: 2, seed: 5, eventCount: 150, buildLabel: "baseline", outDir: t.TempDir()}, scenario)
		if err != nil {
			t.Fatalf("scenario %s: %v", scenario, err)
		}
		if result.Scenario != scenario {
			t.Fatalf("scenario mismatch: %s != %s", result.Scenario, scenario)
		}
		if len(result.RawSamplesMS) != 2 {
			t.Fatalf("scenario %s: expected 2 samples, got %d", scenario, len(result.RawSamplesMS))
		}
	}
}

// TestParseOptionsValidation guards flag parsing invariants.
func TestParseOptionsValidation(t *testing.T) {
	if _, err := parseOptions([]string{"-out", "x"}); err == nil {
		t.Fatalf("expected error for missing build-label")
	}
	if _, err := parseOptions([]string{"-build-label", "baseline"}); err == nil {
		t.Fatalf("expected error for missing out")
	}
	if _, err := parseOptions([]string{"-build-label", "nonsense", "-out", "x"}); err == nil {
		t.Fatalf("expected error for invalid build-label")
	}
	opts, err := parseOptions([]string{"-build-label", "improved", "-out", "x", "-scenario", "stream", "-seed", "11"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if opts.seed != 11 || opts.scenario != "stream" {
		t.Fatalf("unexpected parsed options: %+v", opts)
	}
}

// TestUnmeasuredFieldsAreHonest verifies the harness names its behavior-neutral
// gaps rather than leaving them implicit.
func TestUnmeasuredFieldsAreHonest(t *testing.T) {
	result := newResult(options{runs: 1, buildLabel: "baseline"}, "input", Fixture{Seed: 1, Hash: "h", EventCount: 1, PayloadBytes: 1})
	joined := strings.Join(result.UnmeasuredFields, " | ")
	for _, want := range []string{"frame_count", "dirty_pane_count", "rendered_bytes"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("unmeasured fields missing %q: %s", want, joined)
		}
	}
	// The behavior-neutral markers must never claim a measured value was faked.
	if reflect.DeepEqual(result.UnmeasuredFields, []string{}) {
		t.Fatalf("expected non-empty unmeasured fields")
	}
}
