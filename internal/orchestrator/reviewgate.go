package orchestrator

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// reviewLegacyMode (MUHIYA_BENCH_LEGACY_REVIEW=1) restores the PRE-feature-011
// review behavior — unconditional validation reviews, ungated max-effort nudge,
// single-signal Large classification — while keeping every measurement path
// identical. This exists for one purpose: honest baselines. Constitution X
// requires before/after runs to vary ONLY the change under test; the frozen
// pre-011 binary cannot emit bench summaries, so instead both runs use THIS
// binary and this switch is the single variable. Never set it outside the
// benchmark harness.
var reviewLegacyMode = os.Getenv("MUHIYA_BENCH_LEGACY_REVIEW") == "1"

// Review gating (feature 011, D1/D6 — contracts/review-gating.md). The
// automatic review trigger — the AutoReview nudge at the end of a
// file-changing task — consults Decide before it fires, so a trivial two-file
// change does not spend a turn on ceremony just because the effort is max.
//
// The gate governs the AUTOMATIC trigger only. A user who asks for a review in
// their prompt gets one: that is ordinary work the session does directly, and
// no gating mode, including "off", suppresses an explicit request.
//
// Decide is deterministic and side-effect-free: same profile, same decision,
// zero model calls, zero token cost.

type ReviewTier string

const (
	ReviewTierSkip    ReviewTier = "skip"
	ReviewTierFocused ReviewTier = "focused"
	ReviewTierDeep    ReviewTier = "deep"
)

type ReviewGatingMode string

const (
	ReviewGatingOff          ReviewGatingMode = "off"
	ReviewGatingConservative ReviewGatingMode = "conservative"
	ReviewGatingDefault      ReviewGatingMode = "default"
)

// NormalizeReviewGating parses a user-supplied gating mode; empty means default.
func NormalizeReviewGating(value string) (ReviewGatingMode, bool) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "default":
		return ReviewGatingDefault, true
	case "conservative":
		return ReviewGatingConservative, true
	case "off":
		return ReviewGatingOff, true
	}
	return "", false
}

// RepoSizeBucket mirrors the dynamic-fanout lens buckets so review complexity
// scaling and research scaling agree on what "small repo" means.
type RepoSizeBucket string

const (
	RepoBucketTiny   RepoSizeBucket = "tiny"   // < 3 source files — the greenfield signal
	RepoBucketSmall  RepoSizeBucket = "small"  // <= 6
	RepoBucketMedium RepoSizeBucket = "medium" // <= 20
	RepoBucketLarge  RepoSizeBucket = "large"  // > 20 (probe-capped)
)

// RepoBucketForFileCount converts the dynamic-fanout source-file probe count
// into a bucket. Thresholds are kept in lockstep with dynamicResearchLensCount.
func RepoBucketForFileCount(count int) RepoSizeBucket {
	switch {
	case count < 3:
		return RepoBucketTiny
	case count <= 6:
		return RepoBucketSmall
	case count <= 20:
		return RepoBucketMedium
	}
	return RepoBucketLarge
}

// Risk-area keyword tables (T014). Tunable DATA, not logic: matched
// case-insensitively against changed file paths and, when available, diff hunk
// text plus the task prompt. A single hit sets the area; risk outranks size
// (contract H4 — a one-line auth change still reviews).
var riskAreaPatterns = map[string]*regexp.Regexp{
	"auth":            regexp.MustCompile(`(?i)auth|login|logout|token|session|oauth|jwt|password|credential|\bacl\b|permission|rbac`),
	"billing":         regexp.MustCompile(`(?i)billing|payment|invoice|charge|credit|price|subscription|stripe|paymob|refund`),
	"concurrency":     regexp.MustCompile(`(?i)mutex|sync\.|atomic|goroutine|\bchan\b|WaitGroup|context\.Cancel`),
	"security-config": regexp.MustCompile(`(?i)\.pem\b|\.key\b|\bcert|secret|\bcors\b|\bcsrf\b|\btls\b|firewall`),
	"migration":       regexp.MustCompile(`(?i)migrations?[/\\]|\.sql\b`),
}

// MatchRiskAreas returns the sorted set of risk areas triggered by the changed
// paths and optional content samples (diff hunks / prompt). Sorted so the
// decision — and its rationale string — is deterministic.
func MatchRiskAreas(paths []string, contentSamples ...string) []string {
	hits := map[string]bool{}
	probe := func(s string) {
		for area, re := range riskAreaPatterns {
			if !hits[area] && re.MatchString(s) {
				hits[area] = true
			}
		}
	}
	for _, p := range paths {
		probe(p)
	}
	for _, s := range contentSamples {
		probe(s)
	}
	areas := make([]string, 0, len(hits))
	for area := range hits {
		areas = append(areas, area)
	}
	sort.Strings(areas)
	return areas
}

// Task-type classification from changed file paths. Comment/format/rename
// intent is carried separately as TrivialIntent (the classifier's ClassTiny
// verdict: "typo/rename/comment/one-line"), because those cannot be told apart
// from logic edits by path alone.
var (
	docsExtRE   = regexp.MustCompile(`(?i)\.(md|txt|rst|adoc)$`)
	configExtRE = regexp.MustCompile(`(?i)\.(json|ya?ml|toml|ini|env\.example|gitignore|gitattributes|editorconfig)$|(^|[/\\])\.[\w-]+ignore$`)
)

func reviewTaskType(paths []string) string {
	docs, config, logic := 0, 0, 0
	for _, p := range paths {
		base := filepath.Base(p)
		switch {
		case docsExtRE.MatchString(base) || strings.Contains(strings.ToLower(p), "docs/"):
			docs++
		case configExtRE.MatchString(base):
			config++
		default:
			logic++
		}
	}
	switch {
	case len(paths) == 0:
		return "none"
	case docs == len(paths):
		return "docs"
	case config == len(paths):
		return "config"
	case logic == len(paths):
		return "logic"
	}
	return "mixed"
}

// TaskProfile is the deterministic signal set assembled at review-dispatch
// time (data-model.md §1). All fields derive from state the engine already
// holds; assembly makes no model calls and spends no tokens.
type TaskProfile struct {
	Class        TaskClass
	FilesChanged []string
	LinesAdded   int // 0 with LinesDeleted==0 means "unknown" (diff stats unavailable)
	LinesDeleted int
	// TrivialIntent is true when the classifier judged the request itself
	// trivial (ClassTiny: typo/rename/comment/one-line) — the path-blind stand-in
	// for comment/format/rename detection.
	TrivialIntent bool
	TestOutcome   string // "passed" | "failed" | "not-run"
	RepoBucket    RepoSizeBucket
	// ContentSamples optionally carries diff hunks / the task prompt for the
	// risk matcher; empty is valid (paths alone still match).
	ContentSamples []string
	GatingMode     ReviewGatingMode
	// ExplicitRequest is defense in depth only: an explicitly requested review
	// is work the session simply does, so it does not reach the automatic
	// trigger this gate governs (§6a). Set, it forces a review through anyway.
	ExplicitRequest bool
}

// ReviewDecision is the gating outcome (data-model.md §2). Rationale is always
// user-visible via Line() (SC-009).
type ReviewDecision struct {
	Tier               ReviewTier
	Rationale          string
	TaskType           string
	RiskAreas          []string
	ProportionalCapPct int
	AbsoluteCapTokens  int
	// CeilingHit and Coverage are stamped AFTER the review outcome returns
	// (T022/D6): whether the token ceiling bounded the run, and the reviewer's
	// parsed `Coverage: covered=...; skipped=...` line when present.
	CeilingHit bool
	Coverage   string
}

// Line renders the mandatory one-line rationale: "review: <tier> — <reason>".
func (d ReviewDecision) Line() string {
	return fmt.Sprintf("review: %s — %s", d.Tier, d.Rationale)
}

// Initial absolute ceilings are 0 (uncapped) until T021 derives real values
// from baseline p75 review spend — no arbitrary constant ships unmeasured
// (Constitution VI). The proportional caps and bucket scaling are the
// contract-fixed shape; only the absolute bases await measurement.
var reviewAbsoluteCapBase = map[ReviewTier]int{ReviewTierFocused: 0, ReviewTierDeep: 0}

var repoBucketCeilingScale = map[RepoSizeBucket]float64{
	RepoBucketTiny: 0.5, RepoBucketSmall: 1, RepoBucketMedium: 1.5, RepoBucketLarge: 2,
}

func ceilingsFor(tier ReviewTier, bucket RepoSizeBucket) (proportionalPct, absolute int) {
	switch tier {
	case ReviewTierFocused:
		proportionalPct = 20
	case ReviewTierDeep:
		proportionalPct = 35
	default:
		return 0, 0
	}
	base := reviewAbsoluteCapBase[tier]
	scale := repoBucketCeilingScale[bucket]
	if scale == 0 {
		scale = 1
	}
	return proportionalPct, int(float64(base) * scale)
}

// Decide applies the hard rules H1–H5 and then the tier table + greenfield
// modifier from contracts/review-gating.md §2–§3, exactly and in order.
func Decide(p TaskProfile) ReviewDecision {
	d := ReviewDecision{
		TaskType:  reviewTaskType(p.FilesChanged),
		RiskAreas: MatchRiskAreas(p.FilesChanged, p.ContentSamples...),
	}
	if reviewLegacyMode {
		// Baseline mode: pre-011 behavior reviewed whenever a trigger fired,
		// regardless of triviality or risk — reproduce that verdict verbatim.
		d.Tier, d.Rationale = ReviewTierFocused, "legacy baseline mode (pre-011 unconditional review)"
		return d
	}
	files := len(p.FilesChanged)
	lines := p.LinesAdded + p.LinesDeleted
	linesKnown := lines > 0

	finish := func(tier ReviewTier, rationale string) ReviewDecision {
		d.Tier, d.Rationale = tier, rationale
		// Greenfield cap: initial scaffolding must not trigger heavyweight
		// review — unless it touches a risk area, which overrides the cap.
		if tier == ReviewTierDeep && p.RepoBucket == RepoBucketTiny && len(d.RiskAreas) == 0 {
			d.Tier = ReviewTierFocused
			d.Rationale = rationale + " (greenfield: capped at focused)"
		}
		d.ProportionalCapPct, d.AbsoluteCapTokens = ceilingsFor(d.Tier, p.RepoBucket)
		return d
	}

	// H1 — defense in depth; the real explicit path bypasses Decide entirely.
	if p.ExplicitRequest {
		return finish(ReviewTierFocused, "explicit request")
	}
	// H2
	if p.GatingMode == ReviewGatingOff {
		return finish(ReviewTierSkip, "review gating is off")
	}
	// H3
	if files == 0 {
		return finish(ReviewTierSkip, "no files changed")
	}
	// H4 — risk outranks size; a one-line auth change reviews.
	if len(d.RiskAreas) > 0 {
		tier := ReviewTierFocused
		if p.Class == ClassLarge || p.Class == ClassEpic || files > 8 {
			tier = ReviewTierDeep
		}
		return finish(tier, fmt.Sprintf("risk-area change (%s) in %d file(s)", strings.Join(d.RiskAreas, ", "), files))
	}
	// H5
	if p.TestOutcome == "failed" {
		return finish(ReviewTierFocused, "tests failed this task")
	}

	conservative := p.GatingMode == ReviewGatingConservative

	// Tier table. Row 1: docs/comment/format/rename with tests passing skip in
	// BOTH modes; TrivialIntent stands in for comment/format/rename.
	if d.TaskType == "docs" || (p.TrivialIntent && files <= 2) {
		return finish(ReviewTierSkip, trivialReason(d.TaskType, p.TrivialIntent))
	}
	if d.TaskType == "config" && files <= 2 {
		if conservative {
			return finish(ReviewTierFocused, fmt.Sprintf("config change in %d file(s) (conservative)", files))
		}
		return finish(ReviewTierSkip, "small config-only change")
	}
	// Large/Epic class or wide surface → deep.
	if p.Class == ClassLarge || p.Class == ClassEpic || files > 8 {
		return finish(ReviewTierDeep, fmt.Sprintf("%d-file %s change (%s)", files, d.TaskType, p.Class))
	}
	// Small logic: ≤2 files and ≤40 changed lines skips in default mode. When
	// line counts are unavailable, only a single-file change qualifies for the
	// size exemption — unknown size never widens the skip.
	smallEnough := files <= 2 && ((linesKnown && lines <= 40) || (!linesKnown && files <= 1))
	if smallEnough {
		if conservative {
			return finish(ReviewTierFocused, fmt.Sprintf("small %s change (conservative)", d.TaskType))
		}
		return finish(ReviewTierSkip, fmt.Sprintf("small low-risk %s change (%d file(s), %d lines)", d.TaskType, files, lines))
	}
	return finish(ReviewTierFocused, fmt.Sprintf("%d-file %s change", files, d.TaskType))
}

func trivialReason(taskType string, trivialIntent bool) string {
	if taskType == "docs" {
		return "docs-only change"
	}
	if trivialIntent {
		return "trivial change (comment/rename/format-scale)"
	}
	return "trivial change"
}

// --- engine glue -----------------------------------------------------------

func (e *Engine) reviewGatingMode() ReviewGatingMode {
	mode, ok := NormalizeReviewGating(e.settings.ReviewGating)
	if !ok {
		return ReviewGatingDefault
	}
	return mode
}

func (e *Engine) reviewRepoBucket() RepoSizeBucket {
	return RepoBucketForFileCount(countWorkspaceSourceFiles(strings.TrimSpace(e.session.WorkspacePath), researchLensProbeCeiling))
}

// reviewProfileForTask assembles the TaskProfile for the AutoReview nudge and
// the end-of-task rationale from state the turn loop already tracks — changed
// files, applied diff line counts, the task class — plus the workspace probe.
// Deterministic, no model calls, no token spend.
func (e *Engine) reviewProfileForTask(class TaskClass, filesChanged map[string]bool, linesAdded, linesRemoved int) TaskProfile {
	files := make([]string, 0, len(filesChanged))
	for file := range filesChanged {
		files = append(files, file)
	}
	sort.Strings(files)
	return TaskProfile{
		Class:         class,
		FilesChanged:  files,
		LinesAdded:    linesAdded,
		LinesDeleted:  linesRemoved,
		TrivialIntent: class == ClassTiny,
		TestOutcome:   "not-run",
		RepoBucket:    e.reviewRepoBucket(),
		GatingMode:    e.reviewGatingMode(),
	}
}

// decideValidationReview went with the validation PHASE it gated: there is no
// pipeline to validate now, and review dispatch is the model's call via
// reviewProfileForTask.

// setTaskReviewDecision records the gating decision that governed this task so
// the completion stats and the rationale line can surface it (SC-009). First
// writer wins: a trigger-site decision is authoritative over the informational
// end-of-task evaluation.
func (e *Engine) setTaskReviewDecision(d ReviewDecision) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskReviewDecision == nil {
		copied := d
		e.taskReviewDecision = &copied
	}
}

// reviewCoverageRE parses the reviewer's machine-readable coverage line
// (contracts/review-gating.md §4): lenient about spacing, strict about shape.
var reviewCoverageRE = regexp.MustCompile(`(?mi)^\s*Coverage:\s*(covered=.*)$`)

// updateTaskReviewOutcome stamps post-outcome facts (ceiling hit, coverage
// line) onto the recorded decision so the completion stats and benchmark
// records carry them (SC-004's ceiling_hit, the CoverageReport contract).
func (e *Engine) updateTaskReviewOutcome(output string) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	if e.taskReviewDecision == nil {
		return
	}
	if strings.Contains(output, "partial (token ceiling)") {
		e.taskReviewDecision.CeilingHit = true
	}
	if match := reviewCoverageRE.FindStringSubmatch(output); len(match) > 1 {
		e.taskReviewDecision.Coverage = strings.TrimSpace(match[1])
	}
}

func (e *Engine) takeTaskReviewDecision() *ReviewDecision {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	d := e.taskReviewDecision
	return d
}

// terminalReadCommandRE flags run_shell commands that only read files — work a
// dedicated tool (read_file/grep) does cheaper and cache-tracked (SC-006).
var terminalReadCommandRE = regexp.MustCompile(`(?i)^\s*(cat|type|head|tail|less|more|Get-Content|gc)\b`)

func IsTerminalReadCommand(command string) bool {
	return terminalReadCommandRE.MatchString(command)
}

// countTerminalReadCalls counts shell invocations that merely read a file —
// counted, never blocked; the benchmark's violation rate (SC-006) drives the
// instruction tuning rather than a hard gate.
func (e *Engine) countTerminalReadCalls(calls []contract.ToolCall) {
	for _, call := range calls {
		if call.ToolName() != "run_shell" {
			continue
		}
		var shellArgs struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &shellArgs)
		if IsTerminalReadCommand(shellArgs.Command) {
			e.taskMu.Lock()
			e.taskTerminalReads++
			e.taskMu.Unlock()
		}
	}
}

// finalizeReviewStats stamps the task's review decision and violation counters
// onto the completion stats (SC-009): every file-changing task carries a
// user-visible rationale — from whichever trigger site consulted the gate, or
// evaluated informationally here when no trigger fired. Chat-class tasks
// change nothing and stay silent.
func (e *Engine) finalizeReviewStats(stats *contract.TaskStats, class TaskClass, filesChanged map[string]bool, linesAdded, linesRemoved int) {
	decision := e.takeTaskReviewDecision()
	if decision == nil && len(filesChanged) > 0 && class != ClassChat {
		d := Decide(e.reviewProfileForTask(class, filesChanged, linesAdded, linesRemoved))
		decision = &d
	}
	if decision != nil {
		stats.ReviewTier, stats.ReviewRationale = string(decision.Tier), decision.Rationale
		stats.ReviewCeilingHit, stats.ReviewCoverage = decision.CeilingHit, decision.Coverage
		if e.callbacks.Notice != nil {
			e.callbacks.Notice(decision.Line())
		}
	}
	e.taskMu.Lock()
	stats.TerminalReadViolations, stats.DuplicateReadViolations = e.taskTerminalReads, e.taskDuplicates
	e.taskMu.Unlock()
}

// --- workspace probe -------------------------------------------------------
// The bounded source-file probe the repo-size bucket reads. It lived with the
// research fan-out until feature 013 removed harness dispatch; the review gate
// is its remaining consumer.

const researchLensProbeCeiling = 26 // bounded probe: enough to tell the buckets apart

var researchSourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".py": true, ".rb": true, ".rs": true, ".java": true, ".kt": true, ".cs": true,
	".cpp": true, ".cc": true, ".c": true, ".h": true, ".hpp": true, ".php": true,
	".swift": true, ".scala": true, ".vue": true, ".svelte": true, ".css": true,
	".scss": true, ".html": true, ".sql": true, ".sh": true, ".lua": true, ".dart": true,
}

var researchSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	".next": true, ".nuxt": true, ".venv": true, "venv": true, "__pycache__": true,
	"bin": true, "obj": true, "out": true, "coverage": true, ".turbo": true,
}

// countWorkspaceSourceFiles walks root counting source files, skipping vendor
// and hidden directories, stopping at stopAt so the probe is O(stopAt), never
// O(repo). An unreadable entry is skipped, never fatal.
func countWorkspaceSourceFiles(root string, stopAt int) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if researchSkipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if researchSourceExtensions[strings.ToLower(filepath.Ext(d.Name()))] {
			count++
			if count >= stopAt {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return count
}
