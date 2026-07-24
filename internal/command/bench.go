package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcore "github.com/muhiya/muhiyacode/internal/app"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/spf13/cobra"
)


const maxBenchmarkTaskBytes = 1 << 20

// benchOptions configures a single benchmark execution.
// Benchmark runs are strictly isolated to one process/state root per task.
// There is no cross-task state bleed: each task gets a fresh session, clean
// memory, and an isolated provider cache namespace.
type benchOptions struct {
	taskFile     string
	outFile      string
	model        string
	baseURL      string
	apiKey       string
	effort       string
	advisorMode  string
	contextLimit int
	timeoutSec   int
	seed         int
}

// benchmarkResult is the container-facing one-task record. It intentionally
// describes agent execution rather than claiming the external grader's verdict.
type benchmarkResult struct {
	Status           contract.BenchmarkStatus    `json:"status"`
	Completed        bool                        `json:"completed"`
	StopCause        string                      `json:"stop_cause,omitempty"`
	TerminatedReason string                      `json:"terminated_reason,omitempty"`
	SessionID        string                      `json:"session_id,omitempty"`
	TrajectoryPath   string                      `json:"trajectory_path,omitempty"`
	DurationMS       int64                       `json:"duration_ms"`
	Turns            int                         `json:"turns"`
	ToolCalls        int                         `json:"tool_calls"`
	ChecksRun        int                         `json:"checks_run"`
	FilesChanged     []string                    `json:"files_changed,omitempty"`
	Usage            benchUsage                  `json:"usage"`
	CostUSD          float64                     `json:"cost_usd"`
	CostEstimated    bool                        `json:"cost_estimated"`
	ModelsUsed       []benchPairing              `json:"models_used,omitempty"`
	Config           map[string]string           `json:"config"`
	Verification     contract.VerificationResult `json:"verification"`
	Errors           []string                    `json:"errors,omitempty"`
}

type benchmarkExitError struct {
	status contract.BenchmarkStatus
	cause  error
}

func (e benchmarkExitError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return "benchmark run ended with status " + string(e.status)
}

func (e benchmarkExitError) Unwrap() error { return e.cause }

func (e benchmarkExitError) ExitCode() int {
	switch e.status {
	case contract.BenchmarkPass:
		return 0
	case contract.BenchmarkFail:
		return 2
	case contract.BenchmarkTimeout:
		return 3
	case contract.BenchmarkBlocked:
		return 4
	default:
		return 1
	}
}

// ExitCode maps command errors to process exit codes. Non-benchmark command
// failures preserve the historical exit status of one.
func ExitCode(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 1
}

func newBenchCommand() *cobra.Command {
	var options benchOptions
	command := &cobra.Command{
		Use:   "bench",
		Short: "Run one non-interactive benchmark task and emit a JSON result",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBench(cmd, inheritedWorkspace(cmd), options)
		},
	}
	command.Flags().StringVar(&options.taskFile, "task", "", "read the benchmark task from a file")
	command.Flags().StringVar(&options.outFile, "out", "", "write the final JSON result to a file")
	command.Flags().StringVar(&options.model, "model", "", "override main model")
	command.Flags().StringVar(&options.baseURL, "base-url", "", "override API base URL")
	command.Flags().StringVar(&options.apiKey, "api-key", "", "override API key")
	command.Flags().StringVar(&options.effort, "effort", "", "override effort level")
	command.Flags().StringVar(&options.advisorMode, "advisor", "", "override advisor mode (off, pinned, routed)")
	command.Flags().IntVar(&options.contextLimit, "context-limit", 0, "override model context limit")
	command.Flags().IntVar(&options.timeoutSec, "timeout", 0, "timeout in seconds for benchmark run")
	command.Flags().IntVar(&options.seed, "seed", 0, "optional random seed for provider requests")
	return command
}

func runBench(cmd *cobra.Command, cwd string, options benchOptions) (err error) {
	if options.model == "" {
		options.model = os.Getenv("MUHIYACODE_BENCH_MODEL")
	}
	if options.baseURL == "" {
		options.baseURL = os.Getenv("MUHIYACODE_BENCH_BASE_URL")
	}
	if options.apiKey == "" {
		options.apiKey = os.Getenv("MUHIYACODE_BENCH_API_KEY")
	}
	if options.effort == "" {
		options.effort = os.Getenv("MUHIYACODE_BENCH_EFFORT")
	}
	if options.advisorMode == "" {
		options.advisorMode = os.Getenv("MUHIYACODE_BENCH_ADVISOR")
	}

	task, err := readBenchmarkTask(cmd.InOrStdin(), options.taskFile)
	if err != nil {
		return err
	}
	callbacks := newConsoleCallbacks(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	
	overrides := &BenchmarkOverrides{
		Model:        options.model,
		BaseURL:      options.baseURL,
		APIKey:       options.apiKey,
		Effort:       options.effort,
		AdvisorMode:  options.advisorMode,
		ContextLimit: options.contextLimit,
	}
	if options.seed != 0 {
		overrides.Seed = &options.seed
	}

	app, err := OpenApplication(ApplicationOptions{
		Context: cmd.Context(), Workspace: cwd, NewSession: true, Title: promptTitle(task), Callbacks: callbacks, DisableMCP: true, BenchmarkMode: true, Overrides: overrides,
	})

	var result benchmarkResult
	var runErr error

	defer func() {
		var secrets contract.Secrets
		if app != nil {
			secrets = app.secrets
		}
		if emitErr := emitBenchmarkResult(cmd.OutOrStdout(), options.outFile, result, secrets, runErr); emitErr != nil && err == nil {
			err = emitErr
		} else if err == nil {
			err = runErr
		}
	}()

	if err != nil {
		result = benchmarkResult{
			Status:       contract.BenchmarkError,
			Config:       map[string]string{},
			Verification: contract.VerificationResult{Result: "none"},
			Errors:       []string{err.Error()},
		}
		runErr = err
		return
	}
	defer app.Close()

	hasConfigOverride := options.apiKey != "" || options.baseURL != "" || options.model != "" || options.effort != "" || options.advisorMode != ""
	if notice := configurationNotice(*app.Settings(), app.secrets); notice != "" && !hasConfigOverride {
		result = benchmarkResult{
			Status:       contract.BenchmarkBlocked,
			SessionID:    app.session.ID,
			Config:       benchmarkConfig(*app.Settings()),
			Verification: contract.VerificationResult{Result: "none"},
			Errors:       []string{notice},
		}
		runErr = benchmarkExitError{status: result.Status, cause: errors.New(notice)}
		return
	}

	runCtx := cmd.Context()
	if runCtx == nil {
		runCtx = context.Background()
	}
	var cancel context.CancelFunc
	if options.timeoutSec > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, time.Duration(options.timeoutSec)*time.Second)
		defer cancel()
	}

	if preErr := app.PreTrustWorkspace(runCtx); preErr != nil {
		result = benchmarkResult{
			Status:       contract.BenchmarkError,
			SessionID:    app.session.ID,
			Config:       benchmarkConfig(*app.Settings()),
			Verification: contract.VerificationResult{Result: "none"},
			Errors:       []string{preErr.Error()},
		}
		runErr = preErr
		return
	}

	_, stats, rawRunErr := app.Runtime().Engine.Run(runCtx, appcore.AssemblePrompt(runCtx, task, nil, nil, nil))
	runErr = rawRunErr
	result = newBenchmarkResult(app, stats, runErr)
	if runErr == nil && result.Status != contract.BenchmarkPass {
		runErr = benchmarkExitError{status: result.Status}
	}
	return
}

func readBenchmarkTask(input io.Reader, taskFile string) (string, error) {
	var (
		data []byte
		err  error
	)
	if strings.TrimSpace(taskFile) != "" {
		data, err = os.ReadFile(taskFile)
	} else {
		data, err = io.ReadAll(io.LimitReader(input, maxBenchmarkTaskBytes+1))
	}
	if err != nil {
		return "", fmt.Errorf("read benchmark task: %w", err)
	}
	if len(data) > maxBenchmarkTaskBytes {
		return "", fmt.Errorf("benchmark task exceeds %d-byte limit", maxBenchmarkTaskBytes)
	}
	task := strings.TrimSpace(string(data))
	if task == "" {
		return "", errors.New("benchmark task is empty; provide --task or standard input")
	}
	return task, nil
}

func newBenchmarkResult(app *Application, stats contract.TaskStats, runErr error) benchmarkResult {
	sessionDir, _ := state.SessionDir(app.paths, app.session.ID)
	result := benchmarkResult{
		Status:           benchmarkStatus(stats, runErr),
		Completed:        runErr == nil && stats.StopCause == "" && stats.TerminatedReason == "",
		StopCause:        stats.StopCause,
		TerminatedReason: stats.TerminatedReason,
		SessionID:        app.session.ID,
		TrajectoryPath:   filepath.Join(sessionDir, "transcript.jsonl"),
		DurationMS:       stats.DurationMS,
		Turns:            stats.Turns,
		ToolCalls:        stats.ToolCalls,
		ChecksRun:        stats.ChecksRun,
		FilesChanged:     append([]string(nil), stats.FilesChanged...),
		Usage: benchUsage{
			PromptTokens:     stats.Usage.PromptTokens,
			CompletionTokens: stats.Usage.CompletionTokens,
			TotalTokens:      stats.Usage.TotalTokens,
			Reported:         stats.Usage.PromptTokensAvailable || stats.Usage.CacheReadTokens != nil,
		},
		CostEstimated: stats.CreditsEstimated,
		Config:        benchmarkConfig(*app.Settings()),
		Verification:  contract.VerificationResult{Result: "none"},
	}
	if stats.Usage.CacheReadTokens != nil {
		result.Usage.CacheReadTokens = *stats.Usage.CacheReadTokens
	}
	if stats.Usage.CacheMissTokens != nil {
		result.Usage.CacheMissTokens = *stats.Usage.CacheMissTokens
	}
	if stats.CreditsUSD != nil {
		result.CostUSD = *stats.CreditsUSD
	}
	for _, pairing := range stats.PerPairing {
		result.ModelsUsed = append(result.ModelsUsed, benchPairing{
			Model: pairing.Model, Pin: pairing.Pin, SteadyStateHitRate: pairing.SteadyStateHitRate, Reported: pairing.Reported,
			PromptTokens: pairing.PromptTokens, CompletionTokens: pairing.CompletionTokens,
		})
	}
	if runErr != nil {
		result.Errors = []string{redactBenchmarkError(runErr.Error(), app.secrets)}
	}
	return result
}

func benchmarkConfig(settings contract.Settings) map[string]string {
	return map[string]string{
		"main_model": settings.Provider.ActiveModelID,
		"effort":     string(settings.Effort),
		"advisor":    settings.Provider.Advisor,
	}
}

func redactBenchmarkError(value string, secrets contract.Secrets) string {
	return state.Redact(value, secrets)
}

func emitBenchmarkResult(output io.Writer, outFile string, result benchmarkResult, secrets contract.Secrets, runErr error) error {
	payloadBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode benchmark result: %w", err)
	}
	payload := state.Redact(string(payloadBytes), secrets)
	if strings.TrimSpace(outFile) != "" {
		if err := os.MkdirAll(filepath.Dir(outFile), 0o700); err != nil {
			return fmt.Errorf("create benchmark result directory: %w", err)
		}
		if err := os.WriteFile(outFile, []byte(payload+"\n"), 0o600); err != nil {
			return fmt.Errorf("write benchmark result: %w", err)
		}
	}
	if _, err := fmt.Fprintln(output, payload); err != nil {
		return fmt.Errorf("write benchmark result: %w", err)
	}
	if runErr == nil || result.Status == contract.BenchmarkPass {
		return nil
	}
	var exitErr benchmarkExitError
	if errors.As(runErr, &exitErr) {
		return exitErr
	}
	return benchmarkExitError{status: result.Status, cause: runErr}
}
