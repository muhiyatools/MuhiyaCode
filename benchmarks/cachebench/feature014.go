package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func envFloat(name string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	return value
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// feature014Fixtures is the complete, versioned fixture catalog used by the
// token-economy benchmark runners. Loading is strict: schema drift, an empty
// matrix, or a task without an executable rubric is an error rather than an
// implicit zero-result run.
type feature014Fixtures struct {
	TicTacToe       fixtureSuite
	SmallTasks      fixtureSuite
	ContextEpochs   fixtureSuite
	EvidenceResults fixtureSuite
}

type fixtureSuite struct {
	SchemaVersion int                     `json:"schema_version"`
	Suite         string                  `json:"suite"`
	Description   string                  `json:"description,omitempty"`
	ResetScripts  map[string]fixtureReset `json:"reset_scripts,omitempty"`
	Tasks         []fixtureTask           `json:"tasks,omitempty"`
	Sequences     []fixtureSequence       `json:"sequences,omitempty"`
	Cases         []evidenceFixture       `json:"cases,omitempty"`
	EconomyLimits json.RawMessage         `json:"economy_limits,omitempty"`
}

type fixtureTask struct {
	ID            string            `json:"id"`
	Category      string            `json:"category,omitempty"`
	TaskClass     string            `json:"task_class"`
	Risk          string            `json:"risk,omitempty"`
	Prompt        string            `json:"prompt"`
	Reset         string            `json:"reset,omitempty"`
	Workspace     *fixtureWorkspace `json:"workspace,omitempty"`
	Rubric        *executionRubric  `json:"rubric"`
	EconomyLimits json.RawMessage   `json:"economy_limits,omitempty"`
}

type fixtureWorkspace struct {
	Reset          fixtureReset `json:"reset"`
	AllowedGlobs   []string     `json:"allowed_globs,omitempty"`
	ForbiddenGlobs []string     `json:"forbidden_globs,omitempty"`
	MustPreserve   []string     `json:"must_preserve,omitempty"`
}

type fixtureReset struct {
	Strategy string            `json:"strategy"`
	Source   string            `json:"source,omitempty"`
	Files    map[string]string `json:"files,omitempty"`
}

type executionRubric struct {
	Required bool          `json:"required,omitempty"`
	Checks   []rubricCheck `json:"checks"`
}

type rubricCheck struct {
	ID       string   `json:"id,omitempty"`
	Kind     string   `json:"kind"`
	Path     string   `json:"path,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Text     string   `json:"text,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Selector string   `json:"selector,omitempty"`
	Minimum  *int     `json:"minimum,omitempty"`
	Globs    []string `json:"globs,omitempty"`
	Value    *int     `json:"value,omitempty"`
	Argv     []string `json:"argv,omitempty"`
}

type fixtureSequence struct {
	ID     string          `json:"id"`
	Reset  fixtureReset    `json:"reset"`
	Events []fixtureEvent  `json:"events,omitempty"`
	Turns  []fixtureTurn   `json:"turns"`
	Rubric json.RawMessage `json:"rubric"`
}

type fixtureEvent struct {
	AfterTurn int    `json:"after_turn"`
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
}

type fixtureTurn struct {
	Prompt                string   `json:"prompt"`
	ExpectedRelation      string   `json:"expected_relation"`
	ExpectedEpoch         int      `json:"expected_epoch"`
	RequiredFacts         []string `json:"required_facts,omitempty"`
	RequiredCapsuleFacts  []string `json:"required_capsule_facts,omitempty"`
	ForbiddenContextFacts []string `json:"forbidden_context_facts,omitempty"`
	RequiredBehavior      string   `json:"required_behavior,omitempty"`
}

type evidenceFixture struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Adapter   string          `json:"adapter"`
	Command   []string        `json:"command,omitempty"`
	Status    string          `json:"status"`
	ExitCode  *int            `json:"exit_code"`
	Complete  bool            `json:"complete"`
	TimedOut  bool            `json:"timed_out,omitempty"`
	Encoding  string          `json:"encoding,omitempty"`
	RawText   string          `json:"raw_text,omitempty"`
	RawBase64 string          `json:"raw_base64,omitempty"`
	RawJSON   json.RawMessage `json:"raw_json,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Expect    json.RawMessage `json:"expect"`
}

type economySample struct {
	TaskID        string  `json:"task_id"`
	TaskClass     string  `json:"task_class"`
	MainRequests  int     `json:"main_requests"`
	RetryRequests int     `json:"retry_requests"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	Credits       float64 `json:"credits"`
	Completed     bool    `json:"completed"`
	Correct       bool    `json:"correct"`
	TicTacToe     bool    `json:"tic_tac_toe,omitempty"`
}

type economyAcceptance struct {
	SC001            bool    `json:"sc_001"`
	SC002            bool    `json:"sc_002"`
	SC005            bool    `json:"sc_005"`
	Correctness      bool    `json:"correctness"`
	TinyMedian       float64 `json:"tiny_median"`
	TinyP95          int     `json:"tiny_p95"`
	SmallMedian      float64 `json:"small_median"`
	SmallP95         int     `json:"small_p95"`
	EvaluatedSamples int     `json:"evaluated_samples"`
}

type outputEconomySample struct {
	TaskID                string `json:"task_id"`
	BaselineOutputTokens  int64  `json:"baseline_output_tokens"`
	CandidateOutputTokens int64  `json:"candidate_output_tokens"`
	BaselineTruncated     bool   `json:"baseline_truncated,omitempty"`
	CandidateTruncated    bool   `json:"candidate_truncated,omitempty"`
	BaselineIncomplete    bool   `json:"baseline_incomplete,omitempty"`
	CandidateIncomplete   bool   `json:"candidate_incomplete,omitempty"`
	Correct               bool   `json:"correct"`
}

type outputEconomyAcceptance struct {
	SC004                   bool    `json:"sc_004"`
	NoTruncationRegression  bool    `json:"no_truncation_regression"`
	Correctness             bool    `json:"correctness"`
	BaselineMedianOutput    float64 `json:"baseline_median_output"`
	CandidateMedianOutput   float64 `json:"candidate_median_output"`
	OutputReductionFraction float64 `json:"output_reduction_fraction"`
}

func evaluateOutputEconomy(samples []outputEconomySample) outputEconomyAcceptance {
	baseline, candidate := make([]int64, 0, len(samples)), make([]int64, 0, len(samples))
	result := outputEconomyAcceptance{Correctness: len(samples) > 0, NoTruncationRegression: true}
	baselineFailures, candidateFailures := 0, 0
	for _, sample := range samples {
		baseline = append(baseline, max(int64(0), sample.BaselineOutputTokens))
		candidate = append(candidate, max(int64(0), sample.CandidateOutputTokens))
		if sample.BaselineTruncated || sample.BaselineIncomplete {
			baselineFailures++
		}
		if sample.CandidateTruncated || sample.CandidateIncomplete {
			candidateFailures++
		}
		if !sample.Correct {
			result.Correctness = false
		}
	}
	result.BaselineMedianOutput = int64Median(baseline)
	result.CandidateMedianOutput = int64Median(candidate)
	result.NoTruncationRegression = candidateFailures <= baselineFailures
	if result.BaselineMedianOutput > 0 {
		result.OutputReductionFraction = 1 - result.CandidateMedianOutput/result.BaselineMedianOutput
	}
	result.SC004 = result.Correctness && result.NoTruncationRegression && result.OutputReductionFraction >= .5
	return result
}

func int64Median(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return float64(ordered[middle])
	}
	return float64(ordered[middle-1])/2 + float64(ordered[middle])/2
}

func evaluateEconomyAcceptance(samples []economySample) economyAcceptance {
	tiny, small := make([]int, 0, len(samples)), make([]int, 0, len(samples))
	result := economyAcceptance{Correctness: len(samples) > 0, SC005: true, EvaluatedSamples: len(samples)}
	foundTicTacToe := false
	for _, sample := range samples {
		effectiveRequests := max(0, sample.MainRequests-sample.RetryRequests)
		switch sample.TaskClass {
		case "tiny", "trivial":
			tiny = append(tiny, effectiveRequests)
		case "small":
			small = append(small, effectiveRequests)
		}
		if !sample.Completed || !sample.Correct {
			result.Correctness = false
		}
		if sample.TicTacToe {
			foundTicTacToe = true
			if !sample.Completed || !sample.Correct || effectiveRequests > 6 || sample.InputTokens > 120_000 || sample.OutputTokens > 6_000 || sample.Credits > 2 {
				result.SC005 = false
			}
		}
	}
	result.TinyMedian, result.TinyP95 = integerDistribution(tiny)
	result.SmallMedian, result.SmallP95 = integerDistribution(small)
	result.SC001 = len(tiny) > 0 && result.TinyMedian <= 3 && result.TinyP95 <= 4 && result.Correctness
	result.SC002 = len(small) > 0 && result.SmallMedian <= 5 && result.SmallP95 <= 7 && result.Correctness
	result.SC005 = foundTicTacToe && result.SC005 && result.Correctness
	return result
}

func integerDistribution(values []int) (float64, int) {
	if len(values) == 0 {
		return 0, 0
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	middle := len(ordered) / 2
	median := float64(ordered[middle])
	if len(ordered)%2 == 0 {
		median = float64(ordered[middle-1]+ordered[middle]) / 2
	}
	nearestRank := (95*len(ordered)+99)/100 - 1
	return median, ordered[nearestRank]
}

type rubricResult struct {
	CheckID string `json:"check_id"`
	Kind    string `json:"kind"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail,omitempty"`
}

type runValidity struct {
	Valid             bool     `json:"valid"`
	InvalidReasons    []string `json:"invalid_reasons,omitempty"`
	UsageAvailable    bool     `json:"usage_available"`
	RubricAvailable   bool     `json:"rubric_available"`
	ConfigurationHash string   `json:"configuration_hash,omitempty"`
}

// workloadTurn is one scripted prompt plus its optional completion check.
type workloadTurn struct {
	Prompt string
	Expect string
}

type runResult struct {
	Scenario            string                         `json:"scenario"`
	Build               string                         `json:"build"`
	GitCommit           string                         `json:"git_commit"`
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
	// WorkloadChecks/WorkloadCheckFailures count per-prompt completion checks.
	WorkloadChecks        int            `json:"workload_checks"`
	WorkloadCheckFailures int            `json:"workload_check_failures"`
	FixtureSuite          string         `json:"fixture_suite,omitempty"`
	FixtureSchemaVersion  *int           `json:"fixture_schema_version,omitempty"`
	ExpectedFixtureTasks  *int           `json:"expected_fixture_tasks,omitempty"`
	RubricResults         []rubricResult `json:"rubric_results,omitempty"`
	Validity              *runValidity   `json:"validity,omitempty"`
}

func loadFeature014Fixtures(root string) (feature014Fixtures, error) {
	files := []struct {
		name string
		dst  *fixtureSuite
	}{
		{"tic-tac-toe.json", nil},
		{"small-tasks.json", nil},
		{"context-epochs.json", nil},
		{"evidence-results.json", nil},
	}
	var result feature014Fixtures
	files[0].dst = &result.TicTacToe
	files[1].dst = &result.SmallTasks
	files[2].dst = &result.ContextEpochs
	files[3].dst = &result.EvidenceResults
	for _, item := range files {
		suite, err := loadFixtureSuite(filepath.Join(root, item.name))
		if err != nil {
			return feature014Fixtures{}, fmt.Errorf("load %s: %w", item.name, err)
		}
		*item.dst = suite
	}
	return result, nil
}

func loadFixtureSuite(path string) (fixtureSuite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fixtureSuite{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var suite fixtureSuite
	if err := decoder.Decode(&suite); err != nil {
		return fixtureSuite{}, fmt.Errorf("decode: %w", err)
	}
	if decoder.More() {
		return fixtureSuite{}, errors.New("multiple JSON values")
	}
	if err := validateFixtureSuite(suite); err != nil {
		return fixtureSuite{}, err
	}
	return suite, nil
}

func validateFixtureSuite(suite fixtureSuite) error {
	if suite.SchemaVersion != 1 {
		return fmt.Errorf("schema_version=%d, want 1", suite.SchemaVersion)
	}
	if strings.TrimSpace(suite.Suite) == "" {
		return errors.New("suite is required")
	}
	if len(suite.Tasks)+len(suite.Sequences)+len(suite.Cases) == 0 {
		return errors.New("fixture matrix is empty")
	}
	seen := map[string]struct{}{}
	for index, task := range suite.Tasks {
		if err := validateFixtureTask(task, suite.ResetScripts); err != nil {
			return fmt.Errorf("tasks[%d]: %w", index, err)
		}
		if _, exists := seen[task.ID]; exists {
			return fmt.Errorf("duplicate fixture id %q", task.ID)
		}
		seen[task.ID] = struct{}{}
	}
	for index, sequence := range suite.Sequences {
		if strings.TrimSpace(sequence.ID) == "" || len(sequence.Turns) == 0 || len(sequence.Rubric) == 0 {
			return fmt.Errorf("sequences[%d] requires id, turns, and rubric", index)
		}
		for turnIndex, turn := range sequence.Turns {
			if strings.TrimSpace(turn.Prompt) == "" || strings.TrimSpace(turn.ExpectedRelation) == "" || turn.ExpectedEpoch < 1 {
				return fmt.Errorf("sequences[%d].turns[%d] is incomplete", index, turnIndex)
			}
		}
	}
	for index, item := range suite.Cases {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.Status) == "" || len(item.Expect) == 0 {
			return fmt.Errorf("cases[%d] requires id, kind, status, and expect", index)
		}
		if item.RawText == "" && item.RawBase64 == "" && len(item.RawJSON) == 0 {
			return fmt.Errorf("cases[%d] has no raw payload", index)
		}
	}
	return nil
}

func validateFixtureTask(task fixtureTask, resets map[string]fixtureReset) error {
	if strings.TrimSpace(task.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(task.Prompt) == "" {
		return fmt.Errorf("task %q prompt is required", task.ID)
	}
	if strings.TrimSpace(task.TaskClass) == "" {
		return fmt.Errorf("task %q task_class is required", task.ID)
	}
	if task.Rubric == nil || len(task.Rubric.Checks) == 0 {
		return fmt.Errorf("task %q executable rubric is required", task.ID)
	}
	if task.Reset != "" {
		if _, ok := resets[task.Reset]; !ok {
			return fmt.Errorf("task %q references unknown reset %q", task.ID, task.Reset)
		}
	}
	for index, check := range task.Rubric.Checks {
		if err := validateRubricCheck(check); err != nil {
			return fmt.Errorf("task %q rubric[%d]: %w", task.ID, index, err)
		}
	}
	return nil
}

func validateRubricCheck(check rubricCheck) error {
	switch check.Kind {
	case "file_exists", "file_contains", "file_not_contains":
		if strings.TrimSpace(check.Path) == "" {
			return errors.New("path is required")
		}
	case "file_exists_any", "source_regex":
		if len(check.Paths) == 0 {
			return errors.New("paths are required")
		}
	case "html_selector_count":
		if check.Path == "" || check.Selector == "" || check.Minimum == nil {
			return errors.New("path, selector, and minimum are required")
		}
	case "glob_absent":
		if len(check.Globs) == 0 {
			return errors.New("globs are required")
		}
	case "max_changed_files":
		if check.Value == nil {
			return errors.New("value is required")
		}
	case "command":
		if len(check.Argv) == 0 {
			return errors.New("argv is required")
		}
	default:
		return fmt.Errorf("unsupported kind %q", check.Kind)
	}
	if check.Kind == "source_regex" && strings.TrimSpace(check.Pattern) == "" {
		return errors.New("pattern is required")
	}
	return nil
}
