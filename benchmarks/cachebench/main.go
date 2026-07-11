package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/command"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/state"
)

type options struct {
	scenario   string
	runs       int
	buildLabel string
	outDir     string
	fixtureDir string
	model      string
	effort     string
	turnLimit  time.Duration
	compare    bool
	prices     priceTable
}

type priceTable struct {
	UncachedInputPerMillion float64 `json:"uncached_input_per_million"`
	CacheReadPerMillion     float64 `json:"cache_read_per_million"`
	OutputPerMillion        float64 `json:"output_per_million"`
	Source                  string  `json:"source"`
}

type turnResult struct {
	Index      int    `json:"index"`
	Prompt     string `json:"prompt"`
	Answer     string `json:"answer,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type runResult struct {
	Scenario            string                         `json:"scenario"`
	Build               string                         `json:"build"`
	RunIndex            int                            `json:"run_index"`
	Model               string                         `json:"model"`
	Effort              contract.EffortLevel           `json:"effort"`
	StartedAt           time.Time                      `json:"started_at"`
	WallClockMS         int64                          `json:"wall_clock_ms"`
	CompletedTurns      int                            `json:"completed_turns"`
	Turns               []turnResult                   `json:"turns"`
	UsageRecords        []contract.UsageRecord         `json:"usage_records"`
	Aggregate           contract.SessionUsageAggregate `json:"aggregate"`
	DerivedCost         *float64                       `json:"derived_cost"`
	PriceSource         string                         `json:"price_source"`
	UnattributedMisses  int                            `json:"unattributed_misses"`
	InvalidationEvents  []contract.InvalidationEvent   `json:"invalidation_events"`
	InvalidationByCause map[string]int                 `json:"invalidation_by_cause"`
	RawProviderLog      string                         `json:"raw_provider_log"`
}

type comparison struct {
	GeneratedAt time.Time                  `json:"generated_at"`
	BaselineDir string                     `json:"baseline_dir"`
	ImprovedDir string                     `json:"improved_dir"`
	Scenarios   map[string]scenarioCompare `json:"scenarios"`
}

type scenarioCompare struct {
	Model                    string    `json:"model"`
	Effort                   string    `json:"effort"`
	BaselineRuns             int       `json:"baseline_runs"`
	ImprovedRuns             int       `json:"improved_runs"`
	BaselineSteadyStateRates []float64 `json:"baseline_steady_state_rates"`
	ImprovedSteadyStateRates []float64 `json:"improved_steady_state_rates"`
	BaselineMean             *float64  `json:"baseline_mean"`
	ImprovedMean             *float64  `json:"improved_mean"`
	ImprovedVariancePP       *float64  `json:"improved_variance_percentage_points"`
	BaselineCost             *float64  `json:"baseline_total_cost"`
	ImprovedCost             *float64  `json:"improved_total_cost"`
	CostDelta                *float64  `json:"cost_delta"`
	UnattributedMisses       int       `json:"unattributed_misses"`
	MeetsSteadyStateTarget   bool      `json:"meets_steady_state_target"`
	MeetsVarianceTarget      bool      `json:"meets_variance_target"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "cachebench:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, positional, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.compare {
		if len(positional) != 2 {
			return errors.New("-compare requires BASELINE_DIR and IMPROVED_DIR")
		}
		return compareDirectories(positional[0], positional[1])
	}
	if len(positional) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(positional, " "))
	}
	return executeRuns(opts)
}

func parseOptions(args []string) (options, []string, error) {
	var opts options
	flags := flag.NewFlagSet("cachebench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.scenario, "scenario", "all", "scenario name (all or coding-session)")
	flags.IntVar(&opts.runs, "runs", 3, "number of repetitions")
	flags.StringVar(&opts.buildLabel, "build-label", "", "baseline or improved")
	flags.StringVar(&opts.outDir, "out", "", "result directory")
	flags.StringVar(&opts.fixtureDir, "fixture", filepath.FromSlash("benchmarks/cachebench/fixture"), "fixture directory")
	flags.StringVar(&opts.model, "model", "", "pinned concrete model ID (defaults to active model)")
	flags.StringVar(&opts.effort, "effort", "", "effort level (defaults to configured effort)")
	flags.DurationVar(&opts.turnLimit, "turn-timeout", 12*time.Minute, "timeout per scripted turn")
	flags.BoolVar(&opts.compare, "compare", false, "compare two result directories")
	flags.Float64Var(&opts.prices.UncachedInputPerMillion, "input-price", envFloat("CACHEBENCH_INPUT_PRICE_PER_MILLION"), "uncached input price per million tokens")
	flags.Float64Var(&opts.prices.CacheReadPerMillion, "cache-read-price", envFloat("CACHEBENCH_CACHE_READ_PRICE_PER_MILLION"), "cache-read price per million tokens")
	flags.Float64Var(&opts.prices.OutputPerMillion, "output-price", envFloat("CACHEBENCH_OUTPUT_PRICE_PER_MILLION"), "output price per million tokens")
	flags.StringVar(&opts.prices.Source, "price-source", os.Getenv("CACHEBENCH_PRICE_SOURCE"), "price-table source label")
	if err := flags.Parse(args); err != nil {
		return options{}, nil, err
	}
	if opts.compare {
		return opts, flags.Args(), nil
	}
	if opts.runs < 1 {
		return options{}, nil, errors.New("-runs must be at least 1")
	}
	if opts.buildLabel != "baseline" && opts.buildLabel != "improved" {
		return options{}, nil, errors.New("-build-label must be baseline or improved")
	}
	if strings.TrimSpace(opts.outDir) == "" {
		return options{}, nil, errors.New("-out is required")
	}
	if opts.scenario != "all" && opts.scenario != "coding-session" {
		return options{}, nil, fmt.Errorf("unknown scenario %q", opts.scenario)
	}
	return opts, flags.Args(), nil
}

func executeRuns(opts options) error {
	fixture, err := filepath.Abs(opts.fixtureDir)
	if err != nil {
		return err
	}
	turns, err := loadWorkload(filepath.Join(fixture, "workload.md"))
	if err != nil {
		return err
	}
	if len(turns) < 20 {
		return fmt.Errorf("scenario must contain at least 20 turns, got %d", len(turns))
	}
	outDir, err := filepath.Abs(opts.outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	sourcePaths, err := state.DefaultPaths()
	if err != nil {
		return err
	}
	settings, err := state.LoadSettings(sourcePaths)
	if err != nil {
		return fmt.Errorf("load benchmark settings: %w", err)
	}
	secrets, err := state.LoadSecrets(sourcePaths)
	if err != nil {
		return fmt.Errorf("load benchmark secrets: %w", err)
	}
	if strings.TrimSpace(secrets.ProviderAPIKey) == "" {
		return errors.New("live gateway API key is unavailable")
	}
	if opts.model != "" {
		settings.Provider.ActiveModelID = opts.model
	}
	if opts.effort != "" {
		settings.Effort = contract.EffortLevel(opts.effort)
	}
	if err := validatePinnedModel(settings); err != nil {
		return err
	}
	settings.PermissionMode = contract.PermissionAutoAccept

	for runIndex := 1; runIndex <= opts.runs; runIndex++ {
		result, err := executeOne(opts, fixture, outDir, sourcePaths, settings, secrets, turns, runIndex)
		if writeErr := writeRunResult(outDir, result); writeErr != nil {
			return writeErr
		}
		printRunSummary(result)
		if err != nil {
			return err
		}
	}
	return nil
}

func executeOne(opts options, fixture, outDir string, sourcePaths state.Paths, settings contract.Settings, secrets contract.Secrets, prompts []string, runIndex int) (runResult, error) {
	started := time.Now().UTC()
	result := runResult{
		Scenario: "coding-session", Build: opts.buildLabel, RunIndex: runIndex,
		Model: settings.Provider.ActiveModelID, Effort: settings.Effort, StartedAt: started,
		PriceSource: strings.TrimSpace(opts.prices.Source), InvalidationByCause: map[string]int{},
	}
	workspace, err := os.MkdirTemp("", "cachebench-workspace-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workspace)
	if err := copyTree(fixture, workspace); err != nil {
		return result, err
	}
	benchmarkHome, err := os.MkdirTemp("", "cachebench-home-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(benchmarkHome)

	rawName := fmt.Sprintf("%s-run-%02d-raw-usage.jsonl", result.Scenario, runIndex)
	rawPath := filepath.Join(outDir, rawName)
	logger, err := newRawLogger(rawPath)
	if err != nil {
		return result, err
	}
	defer logger.Close()
	result.RawProviderLog = rawName

	restoreHome := setMuhiyaHome(benchmarkHome)
	defer restoreHome()
	benchmarkPaths, err := state.EnsurePaths()
	if err != nil {
		return result, err
	}
	if err := state.SaveSettings(settings, benchmarkPaths); err != nil {
		return result, err
	}
	if err := state.SaveSecrets(secrets, benchmarkPaths); err != nil {
		return result, err
	}
	for _, pair := range [][2]string{{sourcePaths.MCPFile, benchmarkPaths.MCPFile}, {sourcePaths.MCPSecretsFile, benchmarkPaths.MCPSecretsFile}} {
		if err := copyOptionalFile(pair[0], pair[1]); err != nil {
			return result, err
		}
	}

	app, err := command.OpenApplication(command.ApplicationOptions{
		Context: context.Background(), Workspace: workspace, NewSession: true,
		Title:       fmt.Sprintf("cachebench %s run %d", opts.buildLabel, runIndex),
		Callbacks:   contract.Callbacks{Confirm: func(context.Context, string) (bool, error) { return true, nil }},
		MCPDeadline: 5 * time.Second, RawUsageObserver: logger.Append,
	})
	if err != nil {
		return result, err
	}
	defer app.Close()

	for index, prompt := range prompts {
		turnStarted := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), opts.turnLimit)
		answer, _, runErr := app.Runtime().Engine.Run(ctx, prompt)
		cancel()
		turn := turnResult{Index: index + 1, Prompt: prompt, Answer: answer, DurationMS: time.Since(turnStarted).Milliseconds()}
		if runErr != nil {
			turn.Error = runErr.Error()
			result.Turns = append(result.Turns, turn)
			finalizeResult(&result, app, opts.prices, started)
			return result, fmt.Errorf("scenario turn %d failed: %w", index+1, runErr)
		}
		result.CompletedTurns++
		result.Turns = append(result.Turns, turn)
	}
	finalizeResult(&result, app, opts.prices, started)
	return result, nil
}

func finalizeResult(result *runResult, app *command.Application, prices priceTable, started time.Time) {
	engine := app.Runtime().Engine
	result.WallClockMS = time.Since(started).Milliseconds()
	result.UsageRecords = engine.UsageRecords()
	result.Aggregate = engine.UsageAggregate()
	result.InvalidationEvents = engine.InvalidationEvents()
	for _, event := range result.InvalidationEvents {
		result.InvalidationByCause[string(event.Cause)]++
	}
	result.UnattributedMisses = countUnattributed(result.UsageRecords, result.InvalidationEvents)
	result.DerivedCost = deriveCost(result.Aggregate, prices)
	if result.PriceSource == "" {
		result.PriceSource = "unavailable (configure cachebench price flags)"
	}
}

func countUnattributed(records []contract.UsageRecord, events []contract.InvalidationEvent) int {
	eventSeq := make(map[int]bool, len(events))
	for _, event := range events {
		eventSeq[event.RequestSeq] = true
	}
	count := 0
	for _, record := range records {
		if record.CacheMissTokens == nil || *record.CacheMissTokens <= 0 {
			continue
		}
		switch record.Attribution {
		case contract.CacheAttributionColdStart, contract.CacheAttributionProvider:
			continue
		case contract.CacheAttributionAgent:
			if eventSeq[record.Seq] {
				continue
			}
		}
		count++
	}
	return count
}

func deriveCost(aggregate contract.SessionUsageAggregate, prices priceTable) *float64 {
	if strings.TrimSpace(prices.Source) == "" || (prices.UncachedInputPerMillion == 0 && prices.CacheReadPerMillion == 0 && prices.OutputPerMillion == 0) {
		return nil
	}
	cost := (float64(aggregate.SumCacheMiss)*prices.UncachedInputPerMillion +
		float64(aggregate.SumCacheRead)*prices.CacheReadPerMillion +
		float64(aggregate.SumCompletion)*prices.OutputPerMillion) / 1_000_000
	return &cost
}

var workloadLine = regexp.MustCompile(`^\s*\d+\.\s+(.+?)\s*$`)

func loadWorkload(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var prompts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := workloadLine.FindStringSubmatch(scanner.Text())
		if len(match) == 2 {
			prompts = append(prompts, match[1])
		}
	}
	return prompts, scanner.Err()
}

func writeRunResult(outDir string, result runResult) error {
	name := fmt.Sprintf("%s-run-%02d.json", result.Scenario, result.RunIndex)
	return writeJSON(filepath.Join(outDir, name), result)
}

func printRunSummary(result runResult) {
	rate := "unavailable"
	if result.Aggregate.SteadyStateHitRate != nil {
		rate = fmt.Sprintf("%.2f%%", *result.Aggregate.SteadyStateHitRate*100)
	}
	cost := "unavailable"
	if result.DerivedCost != nil {
		cost = fmt.Sprintf("%.6f", *result.DerivedCost)
	}
	fmt.Printf("%s run %d: turns=%d requests=%d steady=%s cost=%s unattributed=%d\n", result.Scenario, result.RunIndex, result.CompletedTurns, result.Aggregate.Requests, rate, cost, result.UnattributedMisses)
}

func validatePinnedModel(settings contract.Settings) error {
	id := strings.TrimSpace(settings.Provider.ActiveModelID)
	if id == "" {
		return errors.New("active model is empty")
	}
	lower := strings.ToLower(id)
	if strings.Contains(lower, "router") || strings.Contains(lower, "auto") {
		return fmt.Errorf("model %q is not a pinned concrete model", id)
	}
	for _, model := range settings.Provider.Models {
		if model.ID == id {
			if model.ContextLimit <= 0 {
				return fmt.Errorf("model %q has no context limit", id)
			}
			return nil
		}
	}
	return fmt.Errorf("model %q is not in configured models", id)
}

type rawLogger struct {
	mu   sync.Mutex
	file *os.File
}

func newRawLogger(path string) (*rawLogger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &rawLogger{file: file}, nil
}

func (l *rawLogger) Append(payload gateway.RawUsagePayload) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := l.file.Write(append(line, '\n')); err != nil {
		return err
	}
	return l.file.Sync()
}

func (l *rawLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func setMuhiyaHome(home string) func() {
	previous, existed := os.LookupEnv(state.HomeEnvironment)
	_ = os.Setenv(state.HomeEnvironment, home)
	return func() {
		if existed {
			_ = os.Setenv(state.HomeEnvironment, previous)
		} else {
			_ = os.Unsetenv(state.HomeEnvironment)
		}
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyOptionalFile(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return copyFile(source, destination)
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func compareDirectories(baselineDir, improvedDir string) error {
	baseline, err := loadResults(baselineDir)
	if err != nil {
		return err
	}
	improved, err := loadResults(improvedDir)
	if err != nil {
		return err
	}
	result, err := buildComparison(baselineDir, improvedDir, baseline, improved)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(improvedDir, "comparison.json"), result); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(improvedDir, "comparison.md"), []byte(comparisonMarkdown(result)), 0o644)
}

func loadResults(dir string) ([]runResult, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*-run-*.json"))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no run JSON files in %s", dir)
	}
	var results []runResult
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var result runResult
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func buildComparison(baselineDir, improvedDir string, baseline, improved []runResult) (comparison, error) {
	result := comparison{GeneratedAt: time.Now().UTC(), BaselineDir: baselineDir, ImprovedDir: improvedDir, Scenarios: map[string]scenarioCompare{}}
	names := map[string]bool{}
	for _, run := range baseline {
		names[run.Scenario] = true
	}
	for _, run := range improved {
		names[run.Scenario] = true
	}
	for name := range names {
		baseRuns := filterScenario(baseline, name)
		newRuns := filterScenario(improved, name)
		if len(baseRuns) == 0 || len(newRuns) == 0 {
			return comparison{}, fmt.Errorf("scenario %q is missing an arm", name)
		}
		if baseRuns[0].Model != newRuns[0].Model || baseRuns[0].Effort != newRuns[0].Effort {
			return comparison{}, fmt.Errorf("scenario %q model/effort changed between arms", name)
		}
		entry := scenarioCompare{Model: baseRuns[0].Model, Effort: string(baseRuns[0].Effort), BaselineRuns: len(baseRuns), ImprovedRuns: len(newRuns)}
		entry.BaselineSteadyStateRates = availableRates(baseRuns)
		entry.ImprovedSteadyStateRates = availableRates(newRuns)
		entry.BaselineMean = mean(entry.BaselineSteadyStateRates)
		entry.ImprovedMean = mean(entry.ImprovedSteadyStateRates)
		entry.ImprovedVariancePP = rangePercentagePoints(entry.ImprovedSteadyStateRates)
		entry.BaselineCost = totalCost(baseRuns)
		entry.ImprovedCost = totalCost(newRuns)
		if entry.BaselineCost != nil && entry.ImprovedCost != nil {
			delta := *entry.ImprovedCost - *entry.BaselineCost
			entry.CostDelta = &delta
		}
		for _, run := range newRuns {
			entry.UnattributedMisses += run.UnattributedMisses
		}
		entry.MeetsSteadyStateTarget = len(entry.ImprovedSteadyStateRates) == len(newRuns)
		for _, rate := range entry.ImprovedSteadyStateRates {
			entry.MeetsSteadyStateTarget = entry.MeetsSteadyStateTarget && rate >= 0.99
		}
		entry.MeetsVarianceTarget = entry.ImprovedVariancePP != nil && *entry.ImprovedVariancePP <= 1.0
		result.Scenarios[name] = entry
	}
	return result, nil
}

func filterScenario(results []runResult, name string) []runResult {
	var filtered []runResult
	for _, result := range results {
		if result.Scenario == name {
			filtered = append(filtered, result)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].RunIndex < filtered[j].RunIndex })
	return filtered
}

func availableRates(results []runResult) []float64 {
	var values []float64
	for _, result := range results {
		if result.Aggregate.SteadyStateHitRate != nil {
			values = append(values, *result.Aggregate.SteadyStateHitRate)
		}
	}
	return values
}

func mean(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	var total float64
	for _, value := range values {
		total += value
	}
	value := total / float64(len(values))
	return &value
}

func rangePercentagePoints(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	low, high := values[0], values[0]
	for _, value := range values[1:] {
		low = min(low, value)
		high = max(high, value)
	}
	result := (high - low) * 100
	return &result
}

func totalCost(results []runResult) *float64 {
	var total float64
	for _, result := range results {
		if result.DerivedCost == nil {
			return nil
		}
		total += *result.DerivedCost
	}
	return &total
}

func comparisonMarkdown(result comparison) string {
	var names []string
	for name := range result.Scenarios {
		names = append(names, name)
	}
	sort.Strings(names)
	var output strings.Builder
	output.WriteString("# Cachebench comparison\n\n")
	output.WriteString("| Scenario | Baseline mean | Improved mean | Variance (pp) | Cost delta | Unattributed | SC-001 | SC-005 |\n")
	output.WriteString("|---|---:|---:|---:|---:|---:|---|---|\n")
	for _, name := range names {
		entry := result.Scenarios[name]
		fmt.Fprintf(&output, "| %s | %s | %s | %s | %s | %d | %t | %t |\n", name, formatRate(entry.BaselineMean), formatRate(entry.ImprovedMean), formatFloat(entry.ImprovedVariancePP), formatFloat(entry.CostDelta), entry.UnattributedMisses, entry.MeetsSteadyStateTarget, entry.MeetsVarianceTarget)
	}
	return output.String()
}

func formatRate(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}

func formatFloat(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.6f", *value)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func envFloat(name string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	return value
}
