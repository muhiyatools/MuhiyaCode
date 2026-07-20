// Command delegationbench runs one scripted workload (delegation or control)
// through the real agent engine against the live gateway and records whether
// and how the run delegated to subagents (feature 008, T003).
//
// It reuses the cachebench harness patterns: settings/secrets from the real
// MuhiyaCode home, an isolated benchmark home + throwaway workspace copied
// from the fixture tree, a pinned concrete model, auto-accepted permissions,
// and JSON results plus a raw provider usage log per run.
//
// Tool-call auditing is purely observational (no engine changes): the engine
// already reports every attempted tool call through read-only callbacks —
// contract.Callbacks.ToolStart for the main loop and
// contract.Callbacks.Agent (Kind "tool_start") for subagent runs — both fired
// synchronously by the shared dispatch gate BEFORE the tool executes. The
// auditor stats write_file targets at that instant to classify overwrites of
// pre-existing files, and fingerprints read-only calls to count duplicate
// reads. See README.md in this directory for the mechanism write-up.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/command"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

type options struct {
	workload    string
	buildLabel  string
	outDir      string
	effort      string
	model       string
	workloadDir string
	fixtureDir  string
	timeout     time.Duration
}

// workloadSpec is the parsed workload file: exactly one scripted prompt, an
// optional answer expectation, and the post-run filesystem checks.
type workloadSpec struct {
	Prompt string
	Expect string
	Checks []workloadCheck
}

// workloadCheck is one `check: <path> :: <kind> :: <needle>` line. Kind is
// "contains" (some file under path must contain needle) or "absent" (no file
// under path may contain needle). Matching is case-insensitive.
type workloadCheck struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Needle string `json:"needle"`
}

type runResult struct {
	Workload   string               `json:"workload"`
	Build      string               `json:"build"`
	GitCommit  string               `json:"git_commit"`
	Model      string               `json:"model"`
	Effort     contract.EffortLevel `json:"effort"`
	StartedAt  time.Time            `json:"started_at"`
	DurationMS int64                `json:"duration_ms"`
	Prompt     string               `json:"prompt"`
	Answer     string               `json:"answer,omitempty"`
	Error      string               `json:"error,omitempty"`
	Expect     string               `json:"expect,omitempty"`
	ExpectMet  *bool                `json:"expect_met,omitempty"`

	AgentRuns         int `json:"agentRuns"`
	AgentRunsReused   int `json:"agentRunsReused"`
	Turns             int `json:"turns"`
	ToolCalls         int `json:"toolCalls"` // main + subagent attempted calls (audited)
	MainToolCalls     int `json:"mainToolCalls"`
	SubagentToolCalls int `json:"subagentToolCalls"`

	// FinalMainPromptTokens is the PromptTokens of the LAST main-stream usage
	// record (nil when the provider never reported prompt tokens on main).
	FinalMainPromptTokens *int `json:"finalMainPromptTokens"`
	// BilledTokens is sum(CacheMissTokens) + sum(CompletionTokens) over all
	// records. Records with no CacheMissTokens fall back to PromptTokens and
	// are counted in BilledTokensFallbackRecords with the estimate flag set.
	BilledTokens                int      `json:"billedTokens"`
	BilledTokensFallbackRecords int      `json:"billedTokensFallbackRecords"`
	BilledTokensEstimated       bool     `json:"billedTokensEstimated"`
	SteadyStateHitRate          *float64 `json:"steadyStateHitRate"`

	// FilesChanged is ground truth from a pre/post workspace content hash diff
	// (covers main-loop AND subagent writes). EngineFilesChanged mirrors
	// TaskStats.FilesChanged, which only tracks main-loop mutating calls.
	FilesChanged       []string `json:"filesChanged"`
	FilesDeleted       []string `json:"filesDeleted,omitempty"`
	EngineFilesChanged []string `json:"engineFilesChanged"`

	ChecklistResults  map[string]bool `json:"checklistResults"`
	ChecklistFailures int             `json:"checklistFailures"`

	WriteFileAudit writeFileAudit     `json:"writeFileAudit"`
	DuplicateReads duplicateReadAudit `json:"duplicateReads"`
	PipelineAudit  pipelineAudit      `json:"pipelineAudit"`

	TaskStats        contract.TaskStats             `json:"taskStats"`
	UsageRecords     []contract.UsageRecord         `json:"usage_records"`
	Aggregate        contract.SessionUsageAggregate `json:"aggregate"`
	RawProviderLog   string                         `json:"raw_provider_log"`
	SessionArtifacts string                         `json:"session_artifacts,omitempty"`
	ToolEvents       []toolEvent                    `json:"toolEvents"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "delegationbench:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	workloadPath := filepath.Join(opts.workloadDir, opts.workload+"-workload.md")
	workload, err := loadWorkload(workloadPath)
	if err != nil {
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
	settings.Effort = contract.EffortLevel(opts.effort)
	if err := validatePinnedModel(settings); err != nil {
		return err
	}
	settings.PermissionMode = contract.PermissionAutoAccept

	result, runErr := executeOne(opts, workload, sourcePaths, settings, secrets)
	outPath := filepath.Join(opts.outDir, fmt.Sprintf("%s-%s.json", opts.buildLabel, opts.workload))
	if writeErr := writeJSON(outPath, result); writeErr != nil {
		return errors.Join(runErr, writeErr)
	}
	printSummary(result, outPath)
	return runErr
}

func parseOptions(args []string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("delegationbench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.workload, "workload", "", "workload name (delegation or control)")
	flags.StringVar(&opts.buildLabel, "build-label", "", "before or after")
	flags.StringVar(&opts.outDir, "out", "", "result directory")
	flags.StringVar(&opts.effort, "effort", "", "effort level (default: max for delegation, low for control)")
	flags.StringVar(&opts.model, "model", "", "pinned concrete model ID (defaults to active model)")
	flags.StringVar(&opts.workloadDir, "workload-dir", filepath.FromSlash("specs/008-harness-reliability-overhaul/benchmarks"), "directory containing <workload>-workload.md")
	flags.StringVar(&opts.fixtureDir, "fixture", "", "fixture directory (default: <workload-dir>/fixture)")
	flags.DurationVar(&opts.timeout, "timeout", 45*time.Minute, "timeout for the single workload prompt")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if extra := flags.Args(); len(extra) != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
	}
	if opts.workload != "delegation" && opts.workload != "control" {
		return options{}, errors.New("-workload must be delegation or control")
	}
	if opts.buildLabel != "before" && opts.buildLabel != "after" {
		return options{}, errors.New("-build-label must be before or after")
	}
	if strings.TrimSpace(opts.outDir) == "" {
		return options{}, errors.New("-out is required")
	}
	if opts.effort == "" {
		if opts.workload == "delegation" {
			opts.effort = string(contract.EffortMax)
		} else {
			opts.effort = string(contract.EffortLow)
		}
	}
	switch contract.EffortLevel(opts.effort) {
	case contract.EffortLow, contract.EffortMedium, contract.EffortHigh, contract.EffortMax:
	default:
		return options{}, fmt.Errorf("unknown effort %q", opts.effort)
	}
	if opts.fixtureDir == "" {
		opts.fixtureDir = filepath.Join(opts.workloadDir, "fixture")
	}
	var err error
	if opts.workloadDir, err = filepath.Abs(opts.workloadDir); err != nil {
		return options{}, err
	}
	if opts.fixtureDir, err = filepath.Abs(opts.fixtureDir); err != nil {
		return options{}, err
	}
	if opts.outDir, err = filepath.Abs(opts.outDir); err != nil {
		return options{}, err
	}
	return opts, nil
}

func executeOne(opts options, workload workloadSpec, sourcePaths state.Paths, settings contract.Settings, secrets contract.Secrets) (runResult, error) {
	started := time.Now().UTC()
	result := runResult{
		Workload: opts.workload, Build: opts.buildLabel, GitCommit: currentGitCommit(),
		Model: settings.Provider.ActiveModelID, Effort: settings.Effort,
		StartedAt: started, Prompt: workload.Prompt, ChecklistResults: map[string]bool{},
	}
	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		return result, err
	}

	workspace, err := os.MkdirTemp("", "delegationbench-workspace-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workspace)
	if err := copyTree(opts.fixtureDir, workspace); err != nil {
		return result, err
	}
	preHashes, err := hashTree(workspace)
	if err != nil {
		return result, err
	}

	benchmarkHome, err := os.MkdirTemp("", "delegationbench-home-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(benchmarkHome)
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

	rawName := fmt.Sprintf("%s-%s-raw-usage.jsonl", opts.buildLabel, opts.workload)
	logger, err := newRawLogger(filepath.Join(opts.outDir, rawName))
	if err != nil {
		return result, err
	}
	defer logger.Close()
	result.RawProviderLog = rawName

	auditor := newToolAuditor(workspace)
	observer := newBenchmarkObserver(auditor, benchmarkPaths.SessionsDir)
	app, err := command.OpenApplication(command.ApplicationOptions{
		Context: context.Background(), Workspace: workspace, NewSession: true,
		Title: fmt.Sprintf("delegationbench %s %s", opts.buildLabel, opts.workload),
		Callbacks: contract.Callbacks{
			Confirm: func(context.Context, string) (bool, error) { return true, nil },
			Notice:  observer.observeNotice,
			ToolStart: func(name string, input json.RawMessage) {
				observer.observeMainTool(name, string(input))
			},
			Agent: observer.observeAgent,
		},
		MCPDeadline: 5 * time.Second, RawUsageObserver: logger.Append,
	})
	if err != nil {
		return result, err
	}
	defer app.Close()

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	// The two-step plan-approve flow is gone with the planning pipeline: a task
	// now runs to completion in one Run (the model delegates execution itself).
	answer, stats, runErr := app.Runtime().Engine.Run(ctx, workload.Prompt)
	cancel()
	result.Answer = answer
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if workload.Expect != "" {
		result.Expect = workload.Expect
		met := strings.Contains(strings.ToLower(answer), strings.ToLower(workload.Expect))
		result.ExpectMet = &met
	}
	finalize(&result, stats, app, auditor, observer, benchmarkPaths.SessionsDir, workspace, preHashes, workload.Checks, started)
	archiveName := fmt.Sprintf("%s-%s-session-%s", opts.buildLabel, opts.workload, started.Format("20060102T150405Z"))
	if err := copyTree(benchmarkPaths.SessionsDir, filepath.Join(opts.outDir, archiveName)); err != nil {
		return result, fmt.Errorf("archive benchmark session: %w", err)
	}
	result.SessionArtifacts = archiveName
	if runErr != nil {
		return result, fmt.Errorf("workload prompt failed: %w", runErr)
	}
	return result, nil
}

func combineBenchmarkStats(first, second contract.TaskStats) contract.TaskStats {
	combined := second
	combined.DurationMS += first.DurationMS
	combined.Usage = first.Usage.Add(second.Usage)
	combined.AgentUsage = first.AgentUsage.Add(second.AgentUsage)
	combined.PeakContextPercent = max(first.PeakContextPercent, second.PeakContextPercent)
	combined.ToolCalls += first.ToolCalls
	combined.AgentRuns += first.AgentRuns
	combined.AgentRunsReused += first.AgentRunsReused
	combined.Turns += first.Turns
	combined.ChecksRun += first.ChecksRun
	combined.FoldedTokens += first.FoldedTokens
	combined.LinesAdded += first.LinesAdded
	combined.LinesRemoved += first.LinesRemoved
	combined.Invalidations = append(append([]contract.InvalidationEvent(nil), first.Invalidations...), second.Invalidations...)
	seenFiles := make(map[string]bool)
	combined.FilesChanged = nil
	for _, path := range append(append([]string(nil), first.FilesChanged...), second.FilesChanged...) {
		if !seenFiles[path] {
			seenFiles[path] = true
			combined.FilesChanged = append(combined.FilesChanged, path)
		}
	}
	if combined.TaskClass == "" {
		combined.TaskClass = first.TaskClass
	}
	if combined.Effort == "" {
		combined.Effort = first.Effort
	}
	return combined
}

func finalize(result *runResult, stats contract.TaskStats, app *command.Application, auditor *toolAuditor, observer *benchmarkObserver, sessionsDir, workspace string, preHashes map[string]string, checks []workloadCheck, started time.Time) {
	engine := app.Runtime().Engine
	finishedAt := time.Now().UTC()
	result.DurationMS = finishedAt.Sub(started).Milliseconds()
	result.TaskStats = stats
	result.AgentRuns = stats.AgentRuns
	result.AgentRunsReused = stats.AgentRunsReused
	result.Turns = stats.Turns
	result.EngineFilesChanged = append([]string(nil), stats.FilesChanged...)

	result.UsageRecords = engine.UsageRecords()
	result.Aggregate = engine.UsageAggregate()
	result.SteadyStateHitRate = result.Aggregate.SteadyStateHitRate
	deriveTokenMetrics(result, result.UsageRecords)

	auditor.summarize(result)
	observer.summarize(&result.PipelineAudit, sessionsDir, finishedAt)

	if postHashes, err := hashTree(workspace); err == nil {
		result.FilesChanged, result.FilesDeleted = diffTrees(preHashes, postHashes)
	}
	result.ChecklistResults, result.ChecklistFailures = runChecks(workspace, checks)
}

// deriveTokenMetrics fills FinalMainPromptTokens and BilledTokens from the
// append-only usage log. Empty Stream is legacy main-session data (same rule
// as contract.AggregateUsage).
func deriveTokenMetrics(result *runResult, records []contract.UsageRecord) {
	for _, record := range records {
		mainStream := record.Stream == "" || record.Stream == contract.UsageStreamMain
		if mainStream && record.PromptTokens != nil {
			value := *record.PromptTokens
			result.FinalMainPromptTokens = &value
		}
		switch {
		case record.CacheMissTokens != nil:
			result.BilledTokens += *record.CacheMissTokens
		case record.PromptTokens != nil:
			result.BilledTokens += *record.PromptTokens
			result.BilledTokensFallbackRecords++
			result.BilledTokensEstimated = true
		}
		if record.CompletionTokens != nil {
			result.BilledTokens += *record.CompletionTokens
		}
	}
}
