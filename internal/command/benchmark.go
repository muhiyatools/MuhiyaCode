package command

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/processenv"
	"github.com/muhiya/muhiyacode/internal/state"
	"github.com/spf13/cobra"
)

const (
	benchmarkManifestVersion = 1
	maxBenchmarkManifest     = 2 << 20
	maxBenchmarkCapture      = 16 << 20
	maxBenchmarkFiles        = 100_000
	maxBenchmarkBytes        = 2 << 30
)

type benchmarkManifest struct {
	Version          int             `json:"version"`
	Revision         string          `json:"revision"`
	EnvironmentImage string          `json:"environmentImage"`
	ModelID          string          `json:"modelId"`
	Provider         string          `json:"provider"`
	Seed             int             `json:"seed"`
	Sandbox          string          `json:"sandbox"`
	Permission       string          `json:"permission"`
	AgentExecutable  string          `json:"agentExecutable,omitempty"`
	Cases            []benchmarkCase `json:"cases"`
}

type benchmarkCase struct {
	Name           string `json:"name"`
	Fixture        string `json:"fixture"`
	Prompt         string `json:"prompt"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	GraderCommand  string `json:"graderCommand"`
}

type benchmarkReport struct {
	Version      int                   `json:"version"`
	ManifestHash string                `json:"manifestHash"`
	StartedAt    time.Time             `json:"startedAt"`
	FinishedAt   time.Time             `json:"finishedAt"`
	Runtime      string                `json:"runtime"`
	OS           string                `json:"os"`
	Architecture string                `json:"architecture"`
	Manifest     benchmarkManifest     `json:"manifest"`
	Results      []benchmarkCaseResult `json:"results"`
}

type benchmarkCaseResult struct {
	Name            string              `json:"name"`
	Passed          bool                `json:"passed"`
	AgentExitCode   int                 `json:"agentExitCode"`
	GraderExitCode  int                 `json:"graderExitCode"`
	DurationMS      int64               `json:"durationMs"`
	TerminalStatus  contract.TaskStatus `json:"terminalStatus,omitempty"`
	TerminalError   string              `json:"terminalError,omitempty"`
	AgentStderr     string              `json:"agentStderr,omitempty"`
	GraderOutput    string              `json:"graderOutput,omitempty"`
	Events          []json.RawMessage   `json:"events,omitempty"`
	WorkspaceDigest string              `json:"workspaceDigest"`
	Metrics         benchmarkMetrics    `json:"metrics"`
}

type benchmarkMetrics struct {
	Turns            int      `json:"turns"`
	ToolCalls        int      `json:"toolCalls"`
	ChecksRun        int      `json:"checksRun"`
	PromptTokens     int      `json:"promptTokens"`
	CompletionTokens int      `json:"completionTokens"`
	ReasoningTokens  int      `json:"reasoningTokens"`
	CacheReadTokens  int      `json:"cacheReadTokens"`
	CacheMissTokens  int      `json:"cacheMissTokens"`
	CacheHitRate     *float64 `json:"cacheHitRate,omitempty"`
	CreditsUSD       *float64 `json:"creditsUsd,omitempty"`
	FilesChanged     int      `json:"filesChanged"`
	LinesAdded       int      `json:"linesAdded"`
	LinesRemoved     int      `json:"linesRemoved"`
}

func newBenchmarkCommand() *cobra.Command {
	var manifestPath, outputPath string
	var validateOnly bool
	command := &cobra.Command{
		Use: "benchmark", Aliases: []string{"bench"},
		Short: "Run a pinned benchmark manifest against the typed JSONL protocol",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manifestBytes, manifest, err := loadBenchmarkManifest(manifestPath)
			if err != nil {
				return err
			}
			if validateOnly {
				for _, benchmarkCase := range manifest.Cases {
					if info, statErr := os.Stat(benchmarkCase.Fixture); statErr != nil || !info.IsDir() {
						return fmt.Errorf("benchmark fixture %s is not a readable directory", benchmarkCase.Fixture)
					}
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Benchmark manifest is valid.")
				return err
			}
			if err := validateBenchmarkEnvironment(cmd.Context(), manifest); err != nil {
				return err
			}
			report, err := runBenchmarkManifest(cmd.Context(), manifestBytes, manifest)
			if err != nil {
				return err
			}
			return writeBenchmarkReport(outputPath, report)
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "pinned benchmark manifest JSON (required)")
	command.Flags().StringVarP(&outputPath, "output", "o", "benchmark-report.json", "new report path (refuses overwrite)")
	command.Flags().BoolVar(&validateOnly, "validate-only", false, "validate manifest and fixtures without running a model")
	_ = command.MarkFlagRequired("manifest")
	return command
}

func loadBenchmarkManifest(path string) ([]byte, benchmarkManifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, benchmarkManifest{}, err
	}
	if info.Size() > maxBenchmarkManifest {
		return nil, benchmarkManifest{}, errors.New("benchmark manifest exceeds 2 MiB")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, benchmarkManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest benchmarkManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, benchmarkManifest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, benchmarkManifest{}, errors.New("benchmark manifest contains trailing JSON")
	}
	if err := validateBenchmarkManifest(manifest); err != nil {
		return nil, benchmarkManifest{}, err
	}
	base := filepath.Dir(path)
	for index := range manifest.Cases {
		if !filepath.IsAbs(manifest.Cases[index].Fixture) {
			manifest.Cases[index].Fixture = filepath.Join(base, manifest.Cases[index].Fixture)
		}
	}
	return payload, manifest, nil
}

func validateBenchmarkManifest(manifest benchmarkManifest) error {
	if manifest.Version != benchmarkManifestVersion {
		return fmt.Errorf("unsupported benchmark manifest version %d", manifest.Version)
	}
	required := []string{
		manifest.Revision, manifest.EnvironmentImage, manifest.ModelID,
		manifest.Provider, manifest.Sandbox, manifest.Permission,
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return errors.New("benchmark manifest is missing a required environment pin")
		}
	}
	if manifest.Permission != "normal" && manifest.Permission != "unsafe-full-access" {
		return errors.New("benchmark permission must be normal or unsafe-full-access")
	}
	if len(manifest.Cases) == 0 || len(manifest.Cases) > 100 {
		return errors.New("benchmark manifest requires 1-100 cases")
	}
	known := make(map[string]bool)
	for _, benchmarkCase := range manifest.Cases {
		if benchmarkCase.Name == "" || benchmarkCase.Fixture == "" || benchmarkCase.Prompt == "" ||
			benchmarkCase.GraderCommand == "" || benchmarkCase.TimeoutSeconds < 1 || benchmarkCase.TimeoutSeconds > 7_200 {
			return fmt.Errorf("benchmark case %q is incomplete or outside limits", benchmarkCase.Name)
		}
		if known[benchmarkCase.Name] {
			return fmt.Errorf("duplicate benchmark case %q", benchmarkCase.Name)
		}
		known[benchmarkCase.Name] = true
	}
	return nil
}

func validateBenchmarkEnvironment(ctx context.Context, manifest benchmarkManifest) error {
	paths, err := state.EnsurePaths()
	if err != nil {
		return err
	}
	settings, err := state.LoadSettings(paths)
	if err != nil {
		return err
	}
	if settings.Provider.ActiveModelID != manifest.ModelID {
		return fmt.Errorf("active model %q does not match pinned benchmark model %q", settings.Provider.ActiveModelID, manifest.ModelID)
	}
	if settings.Provider.Type != manifest.Provider {
		return fmt.Errorf("provider %q does not match pinned benchmark provider %q", settings.Provider.Type, manifest.Provider)
	}
	if manifest.Revision != "working-tree" {
		output, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
		if err != nil || strings.TrimSpace(string(output)) != manifest.Revision {
			return errors.New("current repository revision does not match the benchmark pin")
		}
	}
	return nil
}

func runBenchmarkManifest(ctx context.Context, payload []byte, manifest benchmarkManifest) (benchmarkReport, error) {
	started := time.Now().UTC()
	executable := manifest.AgentExecutable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return benchmarkReport{}, err
		}
	}
	report := benchmarkReport{
		Version: benchmarkManifestVersion, ManifestHash: hashBytes(payload),
		StartedAt: started, Runtime: runtime.Version(), OS: runtime.GOOS,
		Architecture: runtime.GOARCH, Manifest: manifest,
	}
	for _, benchmarkCase := range manifest.Cases {
		result, err := runBenchmarkCase(ctx, executable, manifest, benchmarkCase)
		if err != nil {
			return report, fmt.Errorf("benchmark case %s: %w", benchmarkCase.Name, err)
		}
		report.Results = append(report.Results, result)
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

func runBenchmarkCase(
	parent context.Context,
	executable string,
	manifest benchmarkManifest,
	benchmarkCase benchmarkCase,
) (benchmarkCaseResult, error) {
	workspaceRoot, err := os.MkdirTemp("", "muhiyacode-benchmark-")
	if err != nil {
		return benchmarkCaseResult{}, err
	}
	defer os.RemoveAll(workspaceRoot)
	if err := copyBenchmarkWorkspace(benchmarkCase.Fixture, workspaceRoot); err != nil {
		return benchmarkCaseResult{}, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(benchmarkCase.TimeoutSeconds)*time.Second)
	defer cancel()
	args := []string{
		"--cwd", workspaceRoot, "--print", benchmarkCase.Prompt,
		"--jsonl", "--new", "--no-mcp", "--seed", fmt.Sprint(manifest.Seed),
	}
	if manifest.Permission == "unsafe-full-access" {
		args = append(args, "--unsafe-full-access")
	}
	var stdout, stderr limitedBuffer
	stdout.limit, stderr.limit = maxBenchmarkCapture, maxBenchmarkCapture
	started := time.Now()
	process := exec.CommandContext(ctx, executable, args...)
	process.Env = processenv.Sanitized(os.Environ())
	process.Stdout, process.Stderr = &stdout, &stderr
	runErr := process.Run()
	agentExit := processExitCode(runErr)
	events, terminalStats, terminalError := parseBenchmarkEvents(stdout.Bytes())
	terminalStatus := contract.TaskStatus("")
	metrics := benchmarkMetrics{}
	if terminalStats != nil {
		terminalStatus = terminalStats.Status
		metrics = metricsFromTaskStats(*terminalStats)
	}
	graderExit, graderOutput := runBenchmarkGrader(ctx, workspaceRoot, benchmarkCase.GraderCommand)
	digest, err := benchmarkWorkspaceDigest(workspaceRoot)
	if err != nil {
		return benchmarkCaseResult{}, err
	}
	passed := agentExit == 0 && graderExit == 0 && terminalStatus == contract.TaskStatusSucceeded
	return benchmarkCaseResult{
		Name: benchmarkCase.Name, Passed: passed, AgentExitCode: agentExit,
		GraderExitCode: graderExit, DurationMS: time.Since(started).Milliseconds(),
		TerminalStatus: terminalStatus, TerminalError: terminalError,
		AgentStderr: stderr.String(), GraderOutput: graderOutput,
		Events: events, WorkspaceDigest: digest, Metrics: metrics,
	}, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		_, _ = buffer.buffer.Write(value[:min(remaining, len(value))])
	}
	return original, nil
}

func (buffer *limitedBuffer) Bytes() []byte  { return buffer.buffer.Bytes() }
func (buffer *limitedBuffer) String() string { return buffer.buffer.String() }

func parseBenchmarkEvents(payload []byte) ([]json.RawMessage, *contract.TaskStats, string) {
	var events []json.RawMessage
	var stats *contract.TaskStats
	var terminalError string
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if !json.Valid(line) {
			continue
		}
		events = append(events, json.RawMessage(line))
		var event headlessEvent
		if json.Unmarshal(line, &event) == nil && event.Type == "task_terminal" && event.Stats != nil {
			copy := *event.Stats
			stats, terminalError = &copy, event.Error
		}
	}
	return events, stats, terminalError
}

func metricsFromTaskStats(stats contract.TaskStats) benchmarkMetrics {
	metrics := benchmarkMetrics{
		Turns: stats.Turns, ToolCalls: stats.ToolCalls, ChecksRun: stats.ChecksRun,
		PromptTokens: stats.Usage.PromptTokens, CompletionTokens: stats.Usage.CompletionTokens,
		CacheReadTokens: nullableInt(stats.Usage.CacheReadTokens),
		CacheMissTokens: nullableInt(stats.Usage.CacheMissTokens),
		CacheHitRate:    stats.SessionHitRate, CreditsUSD: stats.CreditsUSD,
		FilesChanged: len(stats.FilesChanged), LinesAdded: stats.LinesAdded, LinesRemoved: stats.LinesRemoved,
	}
	metrics.ReasoningTokens = nullableInt(stats.Usage.ReasoningTokens)
	return metrics
}

func nullableInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func runBenchmarkGrader(ctx context.Context, workspaceRoot, command string) (int, string) {
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	} else {
		process = exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	}
	process.Dir = workspaceRoot
	process.Env = processenv.Sanitized(os.Environ())
	var output limitedBuffer
	output.limit = maxBenchmarkCapture
	process.Stdout, process.Stderr = &output, &output
	err := process.Run()
	return processExitCode(err), output.String()
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

func copyBenchmarkWorkspace(source, destination string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !sourceInfo.IsDir() {
		return errors.New("benchmark fixture must be a directory")
	}
	files, total := 0, int64(0)
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("benchmark fixture contains symlink %s", relative)
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".muhiya") {
			return filepath.SkipDir
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		files++
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if files > maxBenchmarkFiles || total > maxBenchmarkBytes {
			return errors.New("benchmark fixture exceeds copy limits")
		}
		return copyBenchmarkFile(path, target, info.Mode())
	})
}

func copyBenchmarkFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	return errors.Join(copyErr, output.Close())
}

func benchmarkWorkspaceDigest(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, _ := filepath.Rel(root, path)
		_, _ = io.WriteString(hash, filepath.ToSlash(relative)+"\x00")
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		return errors.Join(copyErr, file.Close())
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}

func hashBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func writeBenchmarkReport(path string, report benchmarkReport) error {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(payload, '\n')); err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}
