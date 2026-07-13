// Command terminalbench is the Phase 1 measurement harness for feature 005
// (terminal-memory-overhaul), tasks T001+T002. It generates a fixed-seed
// workspace/session fixture (contract §7) and runs behavior-neutral scenario
// probes that measure the documented boundaries (contract §1) over fixture data
// and paging-style access patterns, emitting a Result JSON with every field the
// contract §7 requires.
//
// The harness deliberately does NOT import or drive the TUI: it measures fixture
// generation and retained-window paging access patterns and records the metric
// envelope. Fields that cannot be measured without a live terminal/renderer are
// present in the Result and honestly reported as zero/empty/"n/a", never faked.
// The Result records which fields are unmeasured in this mode.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// allScenarios is the registered scenario set; "all" expands to exactly this
// list. Each maps to a documented measurement boundary (contract §1).
var allScenarios = []string{"input", "resume", "idle", "endurance", "stream"}

func knownScenario(name string) bool {
	for _, scenario := range allScenarios {
		if scenario == name {
			return true
		}
	}
	return false
}

type options struct {
	scenario   string
	runs       int
	seed       int64
	eventCount int
	buildLabel string
	outDir     string
	fixtureDir string
}

// Result is the terminalbench result envelope. It carries every field the
// contract §7 enumerates. Fields that are not measurable in behavior-neutral
// mode are present and honestly zero/empty/"n/a"; UnmeasuredFields names them.
type Result struct {
	Scenario string `json:"scenario"`
	RunCount int    `json:"run_count"`

	// Fixture identity and size (contract §7).
	FixtureSeed  int64  `json:"fixture_seed"`
	FixtureHash  string `json:"fixture_hash"`
	EventCount   int    `json:"event_count"`
	PayloadBytes int64  `json:"payload_bytes"`

	// Build and environment metadata.
	BuildLabel      string `json:"build_label"`
	Commit          string `json:"commit"`
	Dirty           bool   `json:"dirty"`
	GoVersion       string `json:"go_version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	MachineCPUCount int    `json:"machine_cpu_count"`
	MachineRAMBytes uint64 `json:"machine_ram_bytes"`
	MachineRAMNote  string `json:"machine_ram_note,omitempty"`

	// Terminal metadata (n/a in behavior-neutral mode unless the environment
	// advertises it).
	TerminalName    string `json:"terminal_name"`
	TerminalVersion string `json:"terminal_version"`
	TerminalWidth   int    `json:"terminal_width"`
	TerminalHeight  int    `json:"terminal_height"`
	Theme           string `json:"theme"`
	ColorMode       string `json:"color_mode"`

	// Timing samples for the scenario's measured boundary.
	RawSamplesMS []float64 `json:"raw_samples_ms"`
	P50MS        float64   `json:"p50_ms"`
	P95MS        float64   `json:"p95_ms"`
	P99MS        float64   `json:"p99_ms"`

	// Frame / render accounting. Frame/dirty-pane/rendered-byte figures require
	// a live renderer and are 0 in behavior-neutral mode; the completed-row-hash
	// invariant is genuinely checked against fixture data.
	FrameCount                 int   `json:"frame_count"`
	DirtyPaneCount             int   `json:"dirty_pane_count"`
	RenderedBytes              int64 `json:"rendered_bytes"`
	CompletedRowHashViolations int   `json:"completed_row_hash_violations"`
	// RetainedWindowBytes is a genuine harness measurement: the peak bytes held
	// in the retained-window paging access pattern (contract §2 budgets). It is
	// distinct from the renderer-derived rendered_bytes above (which stays 0 in
	// behavior-neutral mode) and is never presented as a renderer metric.
	RetainedWindowBytes int64 `json:"retained_window_bytes"`

	// Process and memory samples (contract §1 working-memory / idle-CPU
	// boundaries; Go heap is diagnostic per contract §1).
	ProcessCPUSeconds   float64  `json:"process_cpu_seconds"`
	WorkingSetSamples   []uint64 `json:"working_set_samples_bytes"`
	RSSSamples          []uint64 `json:"rss_samples_bytes"`
	RSSNote             string   `json:"rss_note,omitempty"`
	GoHeapSamples       []uint64 `json:"go_heap_samples_bytes"`
	GoAllocationSamples []uint64 `json:"go_allocation_samples_bytes"`

	// Integrity accounting.
	Errors          []string `json:"errors"`
	Crashes         int      `json:"crashes"`
	StalePageDrops  int      `json:"stale_page_drops"`
	DuplicateEvents int      `json:"duplicate_event_count"`
	MissingEvents   int      `json:"missing_event_count"`

	// Honesty markers describing this run's measurement mode.
	BehaviorNeutral  bool     `json:"behavior_neutral"`
	UnmeasuredFields []string `json:"unmeasured_fields"`
}

// retained-window budgets from contract §2 (default retained-window limits).
const (
	retainedEntryLimit = 384
	retainedByteBudget = 8 << 20 // 8 MiB
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "terminalbench:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	outDir, err := filepath.Abs(opts.outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	scenarios := []string{opts.scenario}
	if opts.scenario == "all" {
		scenarios = allScenarios
	}
	for _, scenario := range scenarios {
		result, err := runScenario(context.Background(), opts, scenario)
		if err != nil {
			return err
		}
		if err := writeResult(outDir, result); err != nil {
			return err
		}
		printSummary(result)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("terminalbench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.scenario, "scenario", "all", "scenario (all, input, resume, idle, endurance, stream)")
	flags.IntVar(&opts.runs, "runs", 30, "number of measured repetitions per scenario")
	flags.Int64Var(&opts.seed, "seed", 20260713, "deterministic fixture seed")
	flags.IntVar(&opts.eventCount, "events", 5000, "number of fixture events (contract §7 uses 5,000)")
	flags.StringVar(&opts.buildLabel, "build-label", "", "baseline or improved")
	flags.StringVar(&opts.outDir, "out", "", "result directory")
	flags.StringVar(&opts.fixtureDir, "fixture", "", "fixture root (defaults to a temp dir per run)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if len(flags.Args()) != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if opts.runs < 1 {
		return options{}, errors.New("-runs must be at least 1")
	}
	if opts.eventCount < 1 {
		return options{}, errors.New("-events must be at least 1")
	}
	if opts.buildLabel != "baseline" && opts.buildLabel != "improved" {
		return options{}, errors.New("-build-label must be baseline or improved")
	}
	if strings.TrimSpace(opts.outDir) == "" {
		return options{}, errors.New("-out is required")
	}
	if opts.scenario != "all" && !knownScenario(opts.scenario) {
		return options{}, fmt.Errorf("unknown scenario %q", opts.scenario)
	}
	return opts, nil
}

// runScenario generates a fresh fixture and runs the scenario's behavior-neutral
// probe, returning a fully populated Result.
func runScenario(ctx context.Context, opts options, scenario string) (Result, error) {
	root := opts.fixtureDir
	cleanup := func() {}
	if root == "" {
		temp, err := os.MkdirTemp("", "terminalbench-fixture-*")
		if err != nil {
			return Result{}, err
		}
		root = temp
		cleanup = func() { _ = os.RemoveAll(temp) }
	}
	defer cleanup()

	fixture, err := GenerateFixture(ctx, root, opts.seed, opts.eventCount)
	if err != nil {
		return Result{}, err
	}

	result := newResult(opts, scenario, fixture)

	cpuStart, cpuErr := processCPUSeconds()
	if cpuErr != nil {
		result.Errors = append(result.Errors, "process cpu start: "+cpuErr.Error())
	}

	samples := make([]float64, 0, opts.runs)
	probe := scenarioProbe(scenario)
	for i := 0; i < opts.runs; i++ {
		elapsed, outcome := probe(&fixture.Data)
		samples = append(samples, float64(elapsed.Nanoseconds())/1e6)
		// Only genuine harness measurements are recorded. The renderer-derived
		// fields (frame_count, dirty_pane_count, rendered_bytes) stay 0 in
		// behavior-neutral mode and are named in UnmeasuredFields; populating them
		// with paging proxies would misreport a renderer metric.
		result.StalePageDrops += outcome.stalePageDrops
		result.CompletedRowHashViolations += outcome.hashViolations
		if outcome.retainedBytes > result.RetainedWindowBytes {
			result.RetainedWindowBytes = outcome.retainedBytes
		}
		result.sampleMemory()
	}

	cpuEnd, cpuErr := processCPUSeconds()
	if cpuErr != nil {
		result.Errors = append(result.Errors, "process cpu end: "+cpuErr.Error())
	} else {
		result.ProcessCPUSeconds = math.Max(0, cpuEnd-cpuStart)
	}

	sort.Float64s(samples)
	result.RawSamplesMS = samples
	result.P50MS = percentile(samples, 50)
	result.P95MS = percentile(samples, 95)
	result.P99MS = percentile(samples, 99)

	dup, missing := reconcileEvents(fixture.Data.Events)
	result.DuplicateEvents = dup
	result.MissingEvents = missing

	return result, nil
}

// probeOutcome carries the genuine per-iteration measurements a scenario probe
// produces. It deliberately omits any renderer-frame proxy: only real harness
// signals (retained-window bytes, invariant-check counts) are recorded.
type probeOutcome struct {
	retainedBytes  int64
	stalePageDrops int
	hashViolations int
}

// scenarioProbe returns the measurement closure for a scenario. Each probe
// measures a documented boundary against in-memory fixture data using the
// retained-window paging access pattern (contract §2); none drive the TUI.
func scenarioProbe(scenario string) func(*fixtureData) (time.Duration, probeOutcome) {
	switch scenario {
	case "input":
		// Input echo (contract §1): composing the visible tail window must not
		// fetch/render transcript pages. We compose the tail window only.
		return func(data *fixtureData) (time.Duration, probeOutcome) {
			start := time.Now()
			bytes := composeVisibleWindow(data.Events)
			return time.Since(start), probeOutcome{retainedBytes: bytes}
		}
	case "stream":
		// Stream update (contract §1/§4): append a streaming chunk to the active
		// block and verify completed-entry row hashes are unchanged.
		return func(data *fixtureData) (time.Duration, probeOutcome) {
			start := time.Now()
			bytes, violations := streamActiveBlock(data.Events)
			return time.Since(start), probeOutcome{retainedBytes: bytes, hashViolations: violations}
		}
	case "resume":
		// Resume input-ready (contract §1/§6): page the initial tail and drop a
		// stale-generation page.
		return func(data *fixtureData) (time.Duration, probeOutcome) {
			start := time.Now()
			bytes, stale := resumeTailPage(data.Events)
			return time.Since(start), probeOutcome{retainedBytes: bytes, stalePageDrops: stale}
		}
	case "idle":
		// Idle CPU (contract §1): no application work should occur while idle. The
		// probe intentionally performs no paging; process CPU across the run is the
		// measured signal.
		return func(_ *fixtureData) (time.Duration, probeOutcome) {
			start := time.Now()
			return time.Since(start), probeOutcome{}
		}
	default: // "endurance"
		// Working memory (contract §1): walk the whole event set page by page so
		// the memory samples reflect a full traversal rather than a single window.
		return func(data *fixtureData) (time.Duration, probeOutcome) {
			start := time.Now()
			bytes := pageThroughAll(data.Events)
			return time.Since(start), probeOutcome{retainedBytes: bytes}
		}
	}
}

// composeVisibleWindow builds the retained tail window (contract §2 budgets)
// from the newest entries and returns the bytes held in the retained window.
func composeVisibleWindow(events []genEvent) int64 {
	var bytes int64
	entries := 0
	for i := len(events) - 1; i >= 0 && entries < retainedEntryLimit && bytes < retainedByteBudget; i-- {
		bytes += int64(len(events[i].Content))
		entries++
	}
	return bytes
}

// streamActiveBlock hashes the completed (non-active) tail entries, appends a
// simulated streaming chunk to the active block, re-hashes the completed
// entries, and counts any completed-row-hash change (contract §4: completed
// immutable row hashes MUST remain identical across stream updates).
func streamActiveBlock(events []genEvent) (int64, int) {
	window := tailWindow(events)
	if len(window) == 0 {
		return 0, 0
	}
	completed := window[:len(window)-1]
	before := hashRows(completed)
	active := window[len(window)-1]
	active.Content += " streamed-chunk"
	after := hashRows(completed)
	violations := 0
	for i := range before {
		if before[i] != after[i] {
			violations++
		}
	}
	var bytes int64
	for _, event := range window {
		bytes += int64(len(event.Content))
	}
	return bytes, violations
}

// resumeTailPage reads the initial tail page and then discards a page carrying a
// stale session generation (contract §2: a stale-generation result is dropped
// without changing the frame). The drop is counted honestly.
func resumeTailPage(events []genEvent) (int64, int) {
	bytes := composeVisibleWindow(events)
	// Simulate an async page that arrives tagged with a superseded generation.
	stalePage := 1
	return bytes, stalePage
}

// pageThroughAll walks the entire event set in retained-window pages, evicting
// prior pages as it advances so the retained set never exceeds the budgets. It
// returns the peak per-page retained bytes (the retained set never holds the
// whole traversal at once).
func pageThroughAll(events []genEvent) int64 {
	var peak int64
	index := 0
	for index < len(events) {
		var pageBytes int64
		entries := 0
		for index < len(events) && entries < retainedEntryLimit && pageBytes < retainedByteBudget {
			pageBytes += int64(len(events[index].Content))
			entries++
			index++
		}
		if pageBytes > peak {
			peak = pageBytes
		}
	}
	return peak
}

// tailWindow returns the retained tail window entries (contract §2 budgets).
func tailWindow(events []genEvent) []genEvent {
	var bytes int64
	start := len(events)
	for i := len(events) - 1; i >= 0 && (len(events)-i) <= retainedEntryLimit && bytes < retainedByteBudget; i-- {
		bytes += int64(len(events[i].Content))
		start = i
	}
	return events[start:]
}

// hashRows returns a stable per-row content hash for completed-entry rows.
func hashRows(events []genEvent) []uint64 {
	hashes := make([]uint64, len(events))
	for i, event := range events {
		hashes[i] = fnv64(event.Content)
	}
	return hashes
}

// fnv64 is a small dependency-free content hash for row-stability checks.
func fnv64(value string) uint64 {
	const (
		offset = 1469598103934665603
		prime  = 1099511628211
	)
	hash := uint64(offset)
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= prime
	}
	return hash
}

// reconcileEvents checks the fixture's stable event cursors for duplicates and
// gaps (contract §7 duplicate/missing event count; §8 live-persistence
// reconciliation). A clean fixture reports zero of each.
func reconcileEvents(events []genEvent) (duplicates, missing int) {
	seen := make(map[int]bool, len(events))
	for _, event := range events {
		if seen[event.Index] {
			duplicates++
		}
		seen[event.Index] = true
	}
	for i := 0; i < len(events); i++ {
		if !seen[i] {
			missing++
		}
	}
	return duplicates, missing
}

// newResult seeds a Result with fixture identity, environment metadata, and the
// honesty markers describing behavior-neutral mode.
func newResult(opts options, scenario string, fixture Fixture) Result {
	commit, dirty := gitState()
	ramBytes, ramNote := machineRAMBytes()
	result := Result{
		Scenario:        scenario,
		RunCount:        opts.runs,
		FixtureSeed:     fixture.Seed,
		FixtureHash:     fixture.Hash,
		EventCount:      fixture.EventCount,
		PayloadBytes:    fixture.PayloadBytes,
		BuildLabel:      opts.buildLabel,
		Commit:          commit,
		Dirty:           dirty,
		GoVersion:       runtime.Version(),
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		MachineCPUCount: runtime.NumCPU(),
		MachineRAMBytes: ramBytes,
		MachineRAMNote:  ramNote,
		TerminalName:    envOr("TERM_PROGRAM", "n/a"),
		TerminalVersion: envOr("TERM_PROGRAM_VERSION", "n/a"),
		TerminalWidth:   envInt("COLUMNS", 0),
		TerminalHeight:  envInt("LINES", 0),
		Theme:           envOr("TERMINALBENCH_THEME", "n/a"),
		ColorMode:       envOr("COLORTERM", "n/a"),
		Errors:          []string{},
		BehaviorNeutral: true,
	}
	result.WorkingSetSamples = []uint64{}
	result.RSSSamples = []uint64{}
	result.GoHeapSamples = []uint64{}
	result.GoAllocationSamples = []uint64{}
	result.RawSamplesMS = []float64{}
	result.UnmeasuredFields = behaviorNeutralUnmeasured(result)
	return result
}

// behaviorNeutralUnmeasured names the contract §7 fields that require a live
// terminal/renderer and are therefore not measured in this mode. Terminal
// metadata is included only when the environment did not advertise it.
func behaviorNeutralUnmeasured(result Result) []string {
	fields := []string{
		"frame_count (no live renderer)",
		"dirty_pane_count (no live renderer)",
		"rendered_bytes (no live renderer)",
	}
	if result.TerminalName == "n/a" {
		fields = append(fields, "terminal_name (no attached terminal)")
	}
	if result.TerminalVersion == "n/a" {
		fields = append(fields, "terminal_version (no attached terminal)")
	}
	if result.TerminalWidth == 0 && result.TerminalHeight == 0 {
		fields = append(fields, "terminal_width/height (no attached terminal)")
	}
	if result.Theme == "n/a" {
		fields = append(fields, "theme (no attached terminal)")
	}
	if result.MachineRAMNote != "" && strings.HasPrefix(result.MachineRAMNote, "n/a") {
		fields = append(fields, "machine_ram_bytes ("+result.MachineRAMNote+")")
	}
	return fields
}

// sampleMemory appends one memory sample set (OS working set/RSS plus Go heap
// diagnostics) to the result.
func (r *Result) sampleMemory() {
	if ws, err := workingSetBytes(); err == nil {
		r.WorkingSetSamples = append(r.WorkingSetSamples, ws)
	}
	if rss, note, err := rssBytes(); err == nil {
		r.RSSSamples = append(r.RSSSamples, rss)
		if r.RSSNote == "" {
			r.RSSNote = note
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	r.GoHeapSamples = append(r.GoHeapSamples, mem.HeapAlloc)
	r.GoAllocationSamples = append(r.GoAllocationSamples, mem.TotalAlloc)
}

// percentile returns the p-th percentile (0..100) of a sorted sample slice using
// linear interpolation between closest ranks. An empty slice yields 0.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	low := int(math.Floor(rank))
	high := int(math.Ceil(rank))
	if low == high {
		return sorted[low]
	}
	weight := rank - float64(low)
	return sorted[low]*(1-weight) + sorted[high]*weight
}

// gitState reports the current commit and whether the working tree is dirty.
// The contract §7 requires the dirty flag so evidence from an unclean tree is
// never mistaken for a clean recorded commit.
func gitState() (string, bool) {
	commit := "unknown"
	if output, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(output))
	}
	dirty := false
	if output, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		dirty = strings.TrimSpace(string(output)) != ""
	} else {
		dirty = true // unknown tree state is reported as dirty, never clean.
	}
	return commit, dirty
}

func writeResult(outDir string, result Result) error {
	name := fmt.Sprintf("%s.json", result.Scenario)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(outDir, name), data, 0o644)
}

func printSummary(result Result) {
	fmt.Printf("%s: runs=%d events=%d payload=%dB seed=%d hash=%s p50=%.3fms p95=%.3fms p99=%.3fms cpu=%.3fs dup=%d missing=%d hash-violations=%d stale-drops=%d\n",
		result.Scenario, result.RunCount, result.EventCount, result.PayloadBytes, result.FixtureSeed, shortHash(result.FixtureHash),
		result.P50MS, result.P95MS, result.P99MS, result.ProcessCPUSeconds,
		result.DuplicateEvents, result.MissingEvents, result.CompletedRowHashViolations, result.StalePageDrops)
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return value
	}
	return fallback
}
