package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	outputReserveTokens      = 12_000
	hardTurnCeiling          = 120
	maxPlanContinues         = 8
	maintenanceFloorRatio    = 0.60
	maintenanceHardFoldRatio = 0.80 // C4: hard fold boundary
	// maintenanceMinYieldPercent (A1/T008) is the minimum reclaimable yield, as a
	// percentage of the usable window, below which a maintenance pass is skipped
	// entirely rather than paying a full prefix reset for a trivial reclaim.
	maintenanceMinYieldPercent = 5
	// T039 ladder thresholds (Reasonix compact.go): a soft advisory band that
	// mutates nothing, and a force band where reclamation can no longer defer
	// compaction. softNoticeRatio sits just below the reclaim floor.
	softNoticeRatio   = 0.50
	compactForceRatio = 0.90
	// H5: distinct-failure terminator. N failed tool calls (any signatures)
	// within the last taskFailureWindow turns force-finalize the task instead
	// of nudging forever. Tuned for batches with multiple failing calls.
	maxTaskFailures   = 8
	taskFailureWindow = 6
)

type Persistence struct {
	AddEvent           func(context.Context, string, string, string) error
	AppendTranscript   func(context.Context, map[string]any) error
	AppendUsage        func(context.Context, contract.UsageRecord) error
	AppendInvalidation func(context.Context, contract.InvalidationEvent) error
	WritePlan          func(context.Context, string) error
	// WriteGoal (G4) persists the active-goal sidecar; ClearGoal removes it.
	// Both are invoked off the modeMu hot path, serialized by writeMu.
	WriteGoal func(context.Context, contract.GoalSnapshot) error
	ClearGoal func(context.Context) error
	// WritePlanState (P2) persists the plan-mode + pending-plan flags;
	// ClearPlanState removes the sidecar when both flags go false. Both run
	// off the modeMu hot path, serialized by writeMu.
	WritePlanState func(context.Context, contract.PlanStateSnapshot) error
	ClearPlanState func(context.Context) error
	// WriteProjectContext (005 US3) atomically persists the typed per-session
	// project-context sidecar, mirroring WriteGoal/WritePlanState. Used to persist
	// the applied instruction/memory cursors alongside history growth.
	WriteProjectContext func(context.Context, contract.ProjectContextSnapshot) error
}

type RescueFunc func(string, []string) ([]contract.ToolCall, string)

type BoundaryToolChange struct {
	Tools []contract.Tool
	Scope string
}

type BoundaryToolSource func() (BoundaryToolChange, bool, error)

type EngineConfig struct {
	Settings             *contract.Settings
	Secrets              contract.Secrets
	Session              contract.Session
	Provider             contract.Provider
	Registry             *Registry
	History              *History
	Inspection           *InspectionLedger
	Knowledge            *Knowledge
	Callbacks            contract.Callbacks
	Persistence          Persistence
	Prompt               PromptContext
	InitialPlan          contract.Plan
	InitialUsageRecords  []contract.UsageRecord
	InitialInvalidations []contract.InvalidationEvent
	BoundaryTools        BoundaryToolSource
	Rescue               RescueFunc
	Redact               func(string) string
	// InitialGoal (G4) restores an active goal from the goal.json sidecar on
	// session resume. Only snapshots with Status "active" are restored.
	InitialGoal *contract.GoalSnapshot
	// InitialPlanState (P2) restores the plan-mode and pending-plan flags from
	// the plan_state.json sidecar on session resume, so a mid-plan restart or a
	// saved-but-not-yet-executed plan survives. Plan CONTENT is restored via
	// InitialPlan (plan.md); this carries only the two flags.
	InitialPlanState *contract.PlanStateSnapshot
	// InitialProjectContext restores the boot snapshot and applied hashes on resume.
	// ProjectContextProbe re-reads the current MUHIYA.md / MEMORY.md at each submit
	// boundary to decide one-shot tail updates (006). Both optional: nil leaves
	// mid-session update surfacing dormant so existing NewEngine callers are safe.
	InitialProjectContext *contract.ProjectContextSnapshot
	ProjectContextProbe   func(context.Context) (contract.ProjectContextProbe, error)
}

type Engine struct {
	settings      *contract.Settings
	secrets       contract.Secrets
	session       contract.Session
	provider      contract.Provider
	registry      *Registry
	history       *History
	inspection    *InspectionLedger
	knowledge     *Knowledge
	callbacks     contract.Callbacks
	persistence   Persistence
	prompt        PromptContext
	rescue        RescueFunc
	redactFn      func(string) string
	boundaryTools BoundaryToolSource

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	steering []string
	plan     contract.Plan
	previous TaskClass
	effortMu sync.RWMutex

	// modeMu is the single mutex owning ALL goal/plan mode state: goal,
	// lastGoal, planMode, pendingPlan (T004/REV B1). A prior revision split
	// these across modeMu (setters) and a separate goalMu (loop reads), which
	// left e.goal guarded by two locks — no mutual exclusion, a real data race
	// reachable because /goal is allowed while a task runs. One mutex fixes it;
	// locks are never nested (the mode setters never call the loop-read helpers
	// while holding modeMu, and vice versa), so there is no deadlock risk.
	modeMu      sync.Mutex
	goal        *Goal // active objective; nil when cleared/completed
	lastGoal    *Goal // G2: tombstone of the most recent completed/blocked goal
	planMode    bool
	pendingPlan bool               // P2: a saved plan awaiting a bare "proceed" to execute it
	planPhase   contract.PlanPhase // 004 US2: whole-plan lifecycle state; single truth for plan affordances
	// writeMu (G4/P2) serializes off-hot-path goal/plan-state sidecar writes so
	// a slow disk sync never blocks modeMu. restoredGoalNotice and
	// restoredPlanNotice are set once in NewEngine and surfaced (then cleared)
	// by RestoredGoalNotice / RestoredPlanNotice.
	writeMu            sync.Mutex
	restoredGoalNotice string
	restoredPlanNotice string
	// Project-context state (006). These run on the single task goroutine (finalize
	// + brief composition), so plain reads/writes are race-free; only the sidecar
	// write in persistProjectCursor takes writeMu, matching the goal/plan-state
	// sidecar pattern. appliedMemoryHash / appliedInstructionsHash are the file
	// hashes already surfaced, so a mid-session edit emits exactly one update block.
	appliedMemoryHash       string
	appliedInstructionsHash string
	projectContext          *contract.ProjectContextSnapshot
	projectContextProbe     func(context.Context) (contract.ProjectContextProbe, error)

	taskMu                sync.Mutex
	sessionUsage          contract.Usage
	usageRecords          []contract.UsageRecord
	usageAggregate        contract.SessionUsageAggregate
	requestSeq            int
	firstAfterStart       bool
	lastShape             *PrefixShape
	lastSentMessageCount  int
	invalidations         *InvalidationLedger
	latestPromptTokens    int
	latestPromptAvailable bool
	maintenancePasses     int
	maintenanceLatched    bool
	softNoticeShown       bool // T039: soft-band advisory fires at most once per compaction cycle
	taskAgentUsage        contract.Usage
	taskAgentRuns         int
	taskAgentReused       int
	taskAgentCap          int
	// taskAgentDenied counts run_subagent calls rejected because the budget was
	// exhausted (or zero). It escalates the denial message so the model stops
	// retrying and finishes the work directly.
	taskAgentDenied int
	// taskUsageStart snapshots sessionUsage at task start. Live Usage callbacks
	// emit subtractUsage(sessionUsage, taskUsageStart) — the cumulative usage of
	// EVERY request this task made (main + subagent + aux) — so the activity
	// line and the end-of-task summary always agree on task-lifecycle numbers.
	taskUsageStart  contract.Usage
	taskPeakContext float64
	taskDuplicates  int
	taskOverBudget  int
	runCounter      int
	// taskCounters holds the per-scope dispatch-gate state (repeats, failed
	// cache, storm classes, plan violations). One instance per task for the main
	// loop; subagents get a fresh instance so their gate state never pollutes the
	// parent's. (B6/T024.)
	taskCounters *callCounters
	taskTokens   int   // H5: cumulative token count for the per-task breaker
	taskFailures []int // H5: turn numbers of failed tool calls (sliding window for the distinct-failure terminator)
}

type toolOutcome struct {
	Call   contract.ToolCall
	Output string
	Failed bool
	// Err carries the underlying dispatch error when Failed is true. P2 uses
	// this to notify the main loop that exit_plan_mode was called.
	Err error
}

type pressureSnapshot struct {
	Tokens    int
	Estimated bool
	Ratio     float64
}

type mainUsageObservation struct {
	model         string
	usage         contract.Usage
	changeReasons []string
	messageCount  int
}

type usageRecordInput struct {
	model       string
	stream      contract.UsageStream
	usage       contract.Usage
	reasons     []string
	attribution contract.CacheAttribution
}

type cacheMissContext struct {
	previous             *contract.UsageRecord
	usage                contract.Usage
	newTail              int
	previousMessageCount int
	currentMessageCount  int
}

type wireRequestNormalizer interface {
	StableRequestMessages(contract.ChatRequest) ([]contract.Message, error)
}

func NewEngine(config EngineConfig) (*Engine, error) {
	if config.Settings == nil || config.Provider == nil || config.Registry == nil {
		return nil, errors.New("orchestrator requires settings, provider, and registry")
	}
	if config.History == nil {
		config.History = NewHistory(HistorySnapshot{Version: 1}, nil)
	}
	if config.Inspection == nil {
		config.Inspection = NewInspection(InspectionSnapshot{Version: 3}, nil)
	}
	if config.Knowledge == nil {
		config.Knowledge = NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	}
	usageRecords := append([]contract.UsageRecord(nil), config.InitialUsageRecords...)
	aggregate := contract.AggregateUsage(usageRecords)
	requestSeq := 0
	latestPromptTokens, latestPromptAvailable := 0, false
	for _, record := range usageRecords {
		requestSeq = max(requestSeq, record.Seq)
		if record.PromptTokens != nil {
			latestPromptTokens, latestPromptAvailable = *record.PromptTokens, true
		}
	}
	ledger := NewInvalidationLedger(config.InitialInvalidations, config.Persistence.AppendInvalidation)
	engine := &Engine{
		settings:              config.Settings,
		secrets:               config.Secrets,
		session:               config.Session,
		provider:              config.Provider,
		registry:              config.Registry,
		history:               config.History,
		inspection:            config.Inspection,
		knowledge:             config.Knowledge,
		callbacks:             config.Callbacks,
		persistence:           config.Persistence,
		prompt:                config.Prompt,
		plan:                  config.InitialPlan,
		rescue:                config.Rescue,
		redactFn:              config.Redact,
		boundaryTools:         config.BoundaryTools,
		sessionUsage:          usageFromAggregate(aggregate),
		usageRecords:          usageRecords,
		usageAggregate:        aggregate,
		requestSeq:            requestSeq,
		firstAfterStart:       true,
		invalidations:         ledger,
		latestPromptTokens:    latestPromptTokens,
		latestPromptAvailable: latestPromptAvailable,
		projectContextProbe:   config.ProjectContextProbe,
	}
	// 005 US3: restore the project-context boot snapshot and applied cursors so a
	// resumed session never re-emits a <memory-update> for events it already
	// surfaced and reuses the exact persisted boot bytes (not a recompile).
	if config.InitialProjectContext != nil {
		snapshot := *config.InitialProjectContext
		engine.projectContext = &snapshot
		engine.appliedMemoryHash = snapshot.AppliedMemoryHash
		engine.appliedInstructionsHash = snapshot.AppliedInstructionHash
	}
	// G4: restore an active goal from the persisted sidecar. Only an active
	// goal resurrects — completed/blocked goals are intentionally dropped so a
	// finished objective never re-activates. The restored AutoTurns is reset at
	// the next task boundary (ResetGoalTaskCounter, G5), so a resumed goal gets
	// a fresh per-task autonomous budget. Surface a one-shot TUI notice.
	if config.InitialGoal != nil && config.InitialGoal.Status == string(GoalActive) && strings.TrimSpace(config.InitialGoal.Text) != "" {
		engine.goal = &Goal{Text: config.InitialGoal.Text, Status: GoalActive, AutoTurns: config.InitialGoal.AutoTurns}
		engine.restoredGoalNotice = "Restored active goal: " + oneLineGoal(config.InitialGoal.Text) + " (/goal clear to drop)"
	}
	// P2: restore plan-mode and pending-plan flags from the persisted sidecar
	// so a mid-plan restart or a saved-but-not-yet-executed plan survives. Plan
	// content was already restored via InitialPlan (plan.md); here we only carry
	// the two flags. Surface distinct one-shot TUI notices for each.
	if config.InitialPlanState != nil {
		engine.planMode = config.InitialPlanState.PlanMode
		engine.pendingPlan = config.InitialPlanState.PendingPlan
		// 004 US2: resolve the restored lifecycle phase. A legacy sidecar (pre-004,
		// no phase) is derived from the two booleans. Then two load-time
		// corrections keep resume truthful: a "pending" plan whose steps are all
		// complete was actually executed under an older build — mark it finished so
		// no stale hint resurrects; a plan caught mid-execution by a crash resumes
		// interrupted rather than as if untouched.
		phase := config.InitialPlanState.Phase
		if phase == contract.PlanPhaseNone {
			switch {
			case engine.pendingPlan:
				phase = contract.PlanPhasePending
			case engine.planMode:
				phase = contract.PlanPhaseDrafting
			}
		}
		if phase == contract.PlanPhasePending && len(engine.plan.Steps) > 0 && !planHasIncompleteSteps(engine.plan) {
			phase = contract.PlanPhaseFinished
			engine.pendingPlan = false
		}
		if phase == contract.PlanPhaseExecuting {
			if planHasIncompleteSteps(engine.plan) {
				phase = contract.PlanPhaseInterrupted
			} else {
				phase = contract.PlanPhaseFinished
			}
		}
		engine.planPhase = phase
		engine.restoredPlanNotice = planRestoreNotice(phase, engine.plan)
	}
	return engine, nil
}

func (e *Engine) IsBusy() bool {
	e.mu.Lock()
	busy := e.cancel != nil
	e.mu.Unlock()
	return busy
}

// SetEffort updates the live effort ceiling safely between model turns.
func (e *Engine) SetEffort(level contract.EffortLevel) {
	e.effortMu.Lock()
	e.settings.Effort = level
	e.effortMu.Unlock()
}

// SwitchModel applies a user-requested model change only at an idle task
// boundary, refreshes the model-dependent prompt fields, and records the
// invalidation before the next request can transmit the new prefix.
func (e *Engine) SwitchModel(ctx context.Context, role, id, name, addendum string) error {
	e.mu.Lock()
	if e.cancel != nil {
		e.mu.Unlock()
		return errors.New("cannot switch models while a task is running")
	}
	oldPrompt := e.prompt
	oldMain, oldSubagent := e.settings.Provider.ActiveModelID, e.settings.Provider.SubagentModelID
	old := oldMain
	if role == "subagent" {
		old = oldSubagent
		if old == id {
			e.mu.Unlock()
			return nil
		}
		e.settings.Provider.SubagentModelID = id
		e.prompt.SubagentModel = name
	} else {
		role = "main"
		if old == id {
			e.mu.Unlock()
			return nil
		}
		e.settings.Provider.ActiveModelID = id
		e.prompt.Model = name
		e.prompt.ModelAddendum = addendum
	}
	e.mu.Unlock()

	event := contract.InvalidationEvent{
		Cause: contract.InvalidationModelSwitch, Trigger: contract.InvalidationUserAction,
		Scope: fmt.Sprintf("%s model changed from %s to %s", role, old, id), RequestSeq: e.nextRequestSeq(),
	}
	if err := e.recordInvalidation(ctx, event); err != nil {
		e.mu.Lock()
		e.settings.Provider.ActiveModelID = oldMain
		e.settings.Provider.SubagentModelID = oldSubagent
		e.prompt = oldPrompt
		e.mu.Unlock()
		return err
	}
	return nil
}

func (e *Engine) effort() contract.EffortLevel {
	e.effortMu.RLock()
	defer e.effortMu.RUnlock()
	return e.settings.Effort
}

func (e *Engine) QueueUserMessage(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel == nil {
		return false
	}
	e.steering = append(e.steering, text)
	return true
}

func (e *Engine) Cancel() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	e.mu.Unlock()
}

// WaitIdle waits until the active task has unwound all persistence and
// callback work. Callers use this before closing databases or MCP sessions.
func (e *Engine) WaitIdle(ctx context.Context) error {
	e.mu.Lock()
	done := e.done
	e.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) Usage() contract.Usage {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.sessionUsage
}

func (e *Engine) UsageAggregate() contract.SessionUsageAggregate {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return cloneUsageAggregate(e.usageAggregate)
}

func (e *Engine) UsageRecords() []contract.UsageRecord {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return cloneUsageRecords(e.usageRecords)
}

func (e *Engine) InvalidationEvents() []contract.InvalidationEvent {
	if e.invalidations == nil {
		return nil
	}
	return e.invalidations.Events()
}

func (e *Engine) CurrentPlan() contract.Plan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plan
}

func (e *Engine) Compact(ctx context.Context) (string, error) {
	if e.IsBusy() {
		return "", errors.New("cannot compact while a task is running")
	}
	pressure := e.contextPressure()
	before := e.history.EstimatedTokens()
	if err := e.compact(ctx, "manual"); err != nil {
		return "", err
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: contract.InvalidationUserCompact, Trigger: contract.InvalidationUserAction,
		Scope: "user requested /compact", Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return "", err
	}
	e.resetMaintenanceLatch()
	e.emitContext(0)
	return "Conversation compacted — " + describeFreed(before, e.history.EstimatedTokens(), e.contextLimit()), nil
}

// describeFreed reports how much context a compaction reclaimed, from the
// estimated history size before and after and the model's context limit.
func describeFreed(before, after, limit int) string {
	freed := max(0, before-after)
	if freed == 0 {
		return "no additional context could be freed."
	}
	percent := float64(freed) / float64(max(1, limit)) * 100
	return fmt.Sprintf("freed %s tokens (%.0f%% of the context window).", humanTokens(freed), percent)
}

// humanTokens renders a token count compactly (1.2k / 3.4m) for notices.
func humanTokens(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

type ContextReport struct {
	HistoryTokens      int
	ContextLimit       int
	Percent            float64
	Usage              contract.Usage
	UsageAggregate     contract.SessionUsageAggregate
	Invalidations      []contract.InvalidationEvent
	PressureTokens     int
	PressureEstimated  bool
	PressurePercent    float64
	MaintenanceLatched bool
	// 003 (T032): session credits scanned from all usage records under the
	// member-set rules. SessionCreditsUSD is nil when unavailable; the priced/
	// eligible counts drive the honesty line in the context modal.
	SessionCreditsUSD       *float64
	SessionCreditsEstimated bool
	SessionCreditsPriced    int
	SessionCreditsEligible  int
}

func (e *Engine) ContextReport() ContextReport {
	history := e.history.EstimatedTokens()
	limit := e.contextLimit()
	pressure := e.contextPressure()
	e.taskMu.Lock()
	latched := e.maintenanceLatched
	credits := contract.SumCreditsUSD(e.usageRecords)
	e.taskMu.Unlock()
	return ContextReport{
		HistoryTokens:           history,
		ContextLimit:            limit,
		Percent:                 float64(history) / float64(max(1, limit)) * 100,
		Usage:                   e.Usage(),
		UsageAggregate:          e.UsageAggregate(),
		Invalidations:           e.InvalidationEvents(),
		PressureTokens:          pressure.Tokens,
		PressureEstimated:       pressure.Estimated,
		PressurePercent:         pressure.Ratio * 100,
		MaintenanceLatched:      latched,
		SessionCreditsUSD:       credits.USD,
		SessionCreditsEstimated: credits.Estimated,
		SessionCreditsPriced:    credits.Priced,
		SessionCreditsEligible:  credits.Eligible,
	}
}

func (e *Engine) Run(parent context.Context, userPrompt string) (answer string, stats contract.TaskStats, runErr error) {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return "", stats, errors.New("prompt is empty")
	}
	e.mu.Lock()
	if e.cancel != nil {
		e.mu.Unlock()
		return "", stats, errors.New("an agent task is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	done := make(chan struct{})
	e.done = done
	e.mu.Unlock()
	defer func() {
		cancel()
		e.mu.Lock()
		if e.done == done {
			e.cancel = nil
			e.done = nil
			e.steering = nil
		}
		close(done)
		e.mu.Unlock()
	}()
	started := time.Now()
	usageStart := e.Usage()
	// 003: capture the usage-record boundary at task start so per-task credits
	// scan exactly this task's records (member-set rules) rather than a poisoned
	// session-wide delta. A costless record then affects only its own task.
	e.taskMu.Lock()
	recordStart := len(e.usageRecords)
	// The live-usage baseline is captured in the same locked section so every
	// request this task makes — including onboarding, which runs before the
	// main per-task counters reset below — lands in the emitted task delta.
	e.taskUsageStart = e.sessionUsage
	e.taskMu.Unlock()
	eventStart := len(e.InvalidationEvents())
	profile := Profile(e.effort())
	assessment := Classify(userPrompt, e.previous)
	e.previous = assessment.Class
	budget := BudgetFor(assessment, profile)
	// Plan-mode floor: the plan block advertises explore/plan/review subagents,
	// so a plan-mode task must never carry agents=0 — the model would be told
	// to delegate and then denied ("budget exhausted (0 run(s))"). One run is
	// always available for delegated investigation; the brief is rebuilt so the
	// advertised budget matches the enforced cap.
	if e.PlanMode() {
		budget = budget.WithAgentFloor(1, assessment)
	}
	modelPrompt := userPrompt
	if profile.Onboarding && ShouldConsiderOnboarding(userPrompt) && e.callbacks.Ask != nil {
		e.callbacks.EmitStatus("Clarifying the task...")
		// Onboarding uses the SubagentModelID and shares the sub stream's gateway
		// routing pin (C1). sessionPinSub is computed later in the same Run; for
		// clarity we re-derive the same identity here (it must stay identical to
		// the one main-loop uses).
		questions, usage := GenerateOnboardingQuestions(ctx, e.provider, e.settings.Provider.SubagentModelID, userPrompt, e.session.ID+":sub")
		if err := e.recordAuxUsage(ctx, e.settings.Provider.SubagentModelID, usage); err != nil {
			return "", stats, fmt.Errorf("persist onboarding usage: %w", err)
		}
		e.emitTaskUsage()
		if len(questions) > 0 {
			if answers, err := e.callbacks.Ask(ctx, questions); err == nil {
				modelPrompt = PromptWithAnswers(userPrompt, answers)
			}
		}
	}
	e.taskMu.Lock()
	e.taskAgentUsage = contract.Usage{}
	e.taskAgentRuns, e.taskAgentReused, e.taskDuplicates, e.taskOverBudget = 0, 0, 0, 0
	e.taskAgentDenied = 0
	e.taskAgentCap = budget.MaxAgentRuns
	e.taskPeakContext = 0
	e.taskCounters = newCallCounters()
	e.taskTokens = 0
	e.taskFailures = nil // H5: reset the per-task failure window
	e.taskMu.Unlock()
	// G5: goal autonomous-continuation budget is per task, not per goal.
	e.ResetGoalTaskCounter()
	if err := e.applyBoundaryToolChange(ctx); err != nil {
		return "", stats, err
	}
	filesChanged := make(map[string]bool)
	toolCalls, checksRun, turns, folded := 0, 0, 0, 0
	doneCriteria := ""
	planReady := false    // P2: set when a plan-mode task ends via exit_plan_mode or a free-text plan finish
	terminateReason := "" // H5: reason a task was force-finalized (token breaker / failure terminator), surfaced to the TUI as a warn
	defer func() {
		e.taskMu.Lock()
		stats = contract.TaskStats{DurationMS: time.Since(started).Milliseconds(), Effort: profile.Level, TaskClass: string(assessment.Class), Usage: subtractUsage(e.sessionUsage, usageStart), AgentUsage: e.taskAgentUsage, PeakContextPercent: e.taskPeakContext, ToolCalls: toolCalls, AgentRuns: e.taskAgentRuns, AgentRunsReused: e.taskAgentReused, Turns: turns, ChecksRun: checksRun, FoldedTokens: folded, DisciplineScore: max(0, 100-min(24, e.taskDuplicates*8)-min(16, e.taskOverBudget*2)), DoneCriteria: doneCriteria, PlanReady: planReady, TerminatedReason: terminateReason}
		// T043: attach the cumulative session hit-rate (provider-fields-only
		// denominator) so the persistent footer can show session hit-rate, not
		// just this task's cache tag. Computed under taskMu with usageRecords.
		stats.SessionHitRate = contract.AggregateUsage(e.usageRecords).SessionHitRate
		// 003: per-task credits from this task's record range (member-set rules),
		// never the poisoned session delta.
		if recordStart <= len(e.usageRecords) {
			credits := contract.SumCreditsUSD(e.usageRecords[recordStart:])
			stats.CreditsUSD = credits.USD
			stats.CreditsEstimated = credits.Estimated
		}
		e.taskMu.Unlock()
		// 003 (FR-013a): mark an interrupted task so the summary can show it.
		// H5 TerminatedReason keeps its own notice and does not set StopCause.
		// ctx (not parent) is what user-stop cancels via e.Cancel.
		switch {
		case ctx.Err() != nil || (runErr != nil && errors.Is(runErr, context.Canceled)):
			stats.StopCause = contract.StopCauseUserStop
		case runErr != nil:
			stats.StopCause = contract.StopCauseError
		}
		// 004 US2 (T10/T11): stamp the plan lifecycle at every task exit. An
		// executing plan resolves to finished (all steps done) or interrupted (any
		// step still open, resumable). Pre-execution and terminal phases are
		// untouched, so the plan-ready finalize paths and non-plan tasks never
		// misfire. Placed here so user-stop, error, and breaker exits stamp too.
		e.stampPlanCompletionPhase()
		allEvents := e.InvalidationEvents()
		if eventStart < len(allEvents) {
			stats.Invalidations = append([]contract.InvalidationEvent(nil), allEvents[eventStart:]...)
		}
		for file := range filesChanged {
			stats.FilesChanged = append(stats.FilesChanged, file)
		}
		sort.Strings(stats.FilesChanged)
		if e.callbacks.TaskComplete != nil {
			e.callbacks.TaskComplete(stats)
		}
	}()

	if err := e.persistMessage(ctx, "user", "message", userPrompt, map[string]any{"role": "user", "content": userPrompt, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return "", stats, err
	}
	// Establish the boundary before maintenance so every completed prior task
	// is eligible. Folding and trimming then commit as one rewrite/event.
	e.history.MarkTaskStart()
	maintenance, err := e.runMaintenanceBoundary(ctx, profile)
	if err != nil {
		return "", stats, err
	}
	folded = maintenance.FoldedTokens
	brief := budget.Brief
	// P2 step 4: a bare "proceed" (or any continuationRE match) against a saved
	// pending plan executes it. Inject the persisted plan content into the task
	// brief (cache-safe — it rides the user-message tail) and clear the flag so
	// the plan executes exactly once. The plan content is the in-memory plan
	// (kept in sync with plan.md via update_plan/WritePlan).
	// 004 US2 (T7): resume a saved plan when the user gives the go-ahead. Fires
	// for a pending or interrupted plan (the interrupted case resumes a partially
	// executed plan) and matches both the bare continuation tokens and the wider
	// "proceed with the plan" phrasing. Step-progress detection (T8) in
	// updatePlan is the phrasing-independent backstop for anything this misses.
	if phase := e.PlanPhase(); phase == contract.PlanPhasePending || phase == contract.PlanPhaseInterrupted {
		if continuationRE.MatchString(userPrompt) || planProceedRE.MatchString(userPrompt) {
			e.SetPendingPlan(false)
			e.SetPlanPhase(contract.PlanPhaseExecuting)
			planText := planMarkdown(e.CurrentPlan())
			if strings.TrimSpace(planText) != "" {
				brief = "[executing saved plan]\n" + planText + "\n" + brief
			}
		}
	}
	plan := e.planBlock()
	goal := e.goalBlock()
	// C1/T030: defensive backstop for the plan⇄goal mutual exclusion (G3). The
	// setters already keep at most one active, but if both blocks are somehow
	// non-empty their instructions contradict (plan: read-only, ask to proceed;
	// goal: work autonomously, end with a marker). Plan is the more restrictive
	// mode, so it wins and the goal block is dropped.
	if plan != "" && goal != "" {
		log.Printf("[mode] plan and goal both active during brief assembly — dropping goal block (plan wins)")
		goal = ""
	}
	if plan != "" {
		brief = plan + "\n" + brief
	}
	if goal != "" {
		brief = goal + "\n" + brief
	}
	// 005 US3 / 006: append one-shot project-context updates to the newest user tail
	// in canonical order (instructions-update, then memory-update, above the goal/
	// plan blocks and below the user's text). Baked into the message BEFORE first
	// transmission and frozen verbatim into history, so it is ordinary append-only
	// tail growth — never a settled-prefix mutation, and no invalidation is owed.
	// Both files are gated on their content HASH, so an unchanged (even empty)
	// MUHIYA.md/MEMORY.md never injects a block; an edit — by the agent's own file
	// tools or by the user — surfaces exactly once, then folds into the next
	// session's cached prefix at no per-turn cost.
	if e.projectContextProbe != nil {
		if probe, probeErr := e.projectContextProbe(ctx); probeErr == nil {
			if probe.MemoryHash != e.appliedMemoryHash {
				if block := RenderMemoryUpdate(probe.MemoryHash, e.appliedMemoryHash, probe.MemoryContent); block != "" {
					brief = block + "\n" + brief
				}
				e.appliedMemoryHash = probe.MemoryHash
			}
			if probe.InstructionsHash != e.appliedInstructionsHash {
				brief = RenderInstructionsUpdate(probe.InstructionsHash, e.appliedInstructionsHash, probe.InstructionsContent) + "\n" + brief
				e.appliedInstructionsHash = probe.InstructionsHash
			}
		}
	}
	e.history.Append(contract.Message{Role: contract.RoleUser, Content: modelPrompt + "\n\n" + brief})
	e.persistProjectCursor(ctx)
	e.emitContext(0)
	// Per-stream session pin for the main loop (C1). The subagent / compact /
	// onboarding paths each derive their own identical suffix locally so the
	// gateway-side routing pin never drifts within a session.
	sessionPinMain := e.session.ID + ":main"

	// The system prompt and tool schemas are session-stable: identical bytes
	// on every turn and every task, so they stay in the cached prefix. Per-turn
	// variation lives only in the task brief on the newest user message.
	definitions := e.sessionDefinitions()
	promptContext := e.prompt
	promptContext.Workspace = e.session.WorkspacePath
	promptContext.OS = runtime.GOOS
	promptContext.HasWeb = hasDefinition(definitions, "web_search")
	promptContext.HasSubagents = hasDefinition(definitions, "run_subagent")
	promptText := SystemPrompt(promptContext)

	currentClass := assessment.Class
	turnCap := budget.MaxTurns
	escalated, convergeNoted, finalNoted := false, false, false
	sawToolCall, emptyFinalRetries, planContinues, consecutiveFailures := false, 0, 0, 0
	allFailedTurnStreak := 0 // B7: consecutive turns where EVERY tool call failed (reset at Run start via this local)
	overBudgetNoted := false
	for {
		turns++
		if err := ctx.Err(); err != nil {
			return "", stats, fmt.Errorf("task stopped: %w", err)
		}
		live := Profile(e.effort())
		liveBudget := BudgetFor(Assessment{Class: currentClass, Risky: assessment.Risky, ScopeGuard: assessment.ScopeGuard}, live)
		turnCap = min(hardTurnCeiling, max(turnCap, liveBudget.MaxTurns))
		if e.drainSteering(ctx) {
			turnCap = min(hardTurnCeiling, max(turnCap, turns-1+liveBudget.MaxTurns))
			convergeNoted, finalNoted = false, false
		}
		if turns >= turnCap && len(filesChanged) > 0 && !escalated && currentClass != ClassEpic {
			escalated = true
			currentClass = EscalateClass(currentClass)
			bigger := BudgetFor(Assessment{Class: currentClass, Risky: assessment.Risky}, live)
			turnCap = min(hardTurnCeiling, max(turnCap+6, bigger.MaxTurns))
			e.taskMu.Lock()
			if bigger.MaxAgentRuns > e.taskAgentCap {
				e.taskAgentCap = bigger.MaxAgentRuns
			}
			e.taskMu.Unlock()
			convergeNoted, finalNoted = false, false
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: fmt.Sprintf("[governor] Task outgrew its brief; class=%s and runway extended once. Complete, verify once, and report.", currentClass)})
		}
		isFinal := turns >= turnCap
		if isFinal && !finalNoted {
			finalNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Final step: do not call tools. Give the complete factual final answer now: outcome, verification, and genuine remaining work."})
		} else if !isFinal && turns >= turnCap-2 && !convergeNoted {
			convergeNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Two steps remain. Batch only essential edits, verify, and finish."})
		}
		// T039 ladder: soft advisory band [0.50, 0.60) mutates NOTHING — it just
		// warns once per compaction cycle that context is filling (Reasonix
		// compact.go:94-99). The reclaim band [0.60, 0.80) is handled by the
		// task-boundary runMaintenanceBoundary (A1). Here in the turn loop we only
		// act at the compaction threshold.
		if pr := e.contextPressure(); pr.Ratio >= softNoticeRatio && pr.Ratio < maintenanceFloorRatio {
			e.taskMu.Lock()
			shown := e.softNoticeShown
			e.softNoticeShown = true
			e.taskMu.Unlock()
			if !shown {
				e.callbacks.EmitStatus("Context is filling; earlier turns will be summarized when needed.")
			}
		}
		if e.needsCompact(live) {
			pressure := e.contextPressure()
			// T039 prune-before-compact: below the force ratio, try reclamation
			// first; if folding/trimming clears the compaction trigger, skip the
			// paid structured-summary compaction entirely. At/above the force
			// ratio (0.90) we compact regardless.
			skipCompact := false
			if pressure.Ratio < compactForceRatio {
				if _, err := e.runMaintenanceBoundary(ctx, profile); err != nil {
					return "", stats, err
				}
				// Reclamation shrank history, but the last real provider token count
				// is now stale (it reflects the pre-reclamation request), so
				// needsCompact would still see the old value and never skip. Decide
				// on the POST-reclamation estimate instead: if reclamation dropped
				// the estimated pressure below the compaction threshold, skip the
				// paid summarization (one rewrite instead of two). A slight
				// under-estimate here only risks skipping one turn early; the next
				// turn's real token count re-triggers compaction if still needed.
				usable := max(8000, e.contextLimit()-outputReserveTokens)
				postRatio := float64(e.history.EstimatedTokens()) / float64(usable)
				if postRatio < profile.CompactThreshold {
					skipCompact = true
				}
			}
			if !skipCompact {
				e.callbacks.EmitStatus("Compacting context...")
				e.callbacks.EmitNotice("Context is nearly full — compacting the conversation…")
				pressure = e.contextPressure()
				beforeCompact := e.history.EstimatedTokens()
				if err := e.compact(ctx, "automatic: context nearly full"); err != nil {
					// C5: a tail-placed user message with a bracketed marker. Same
					// cache-safe behavior as before (no mid-history edit), but it
					// becomes a user turn instead of a system turn so providers do
					// not need to special-case it.
					e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Automatic compaction failed; continue using the bounded recent context."})
					e.callbacks.EmitNotice("Compaction failed — continuing with the recent context.")
				} else if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
					Cause: contract.InvalidationCompact, Trigger: contract.InvalidationPressure,
					Scope: "automatic structured-summary compaction", Pressure: floatPointer(pressure.Ratio),
					RequestSeq: e.nextRequestSeq(),
				}); err != nil {
					return "", stats, err
				} else {
					e.resetMaintenanceLatch()
					e.callbacks.EmitNotice("Compaction complete — " + describeFreed(beforeCompact, e.history.EstimatedTokens(), e.contextLimit()))
				}
			}
		}
		// Trim aged tool payloads only when the window is genuinely filling.
		// Trimming rewrites history and busts the prefix cache, so it must not
		// run on every turn — only once pressure crosses the threshold.
		e.callbacks.EmitStatus("Thinking...")
		built := e.history.BuildRequestWithMetadata(promptText, e.contextLimit(), outputReserveTokens)
		if built.WindowDropped {
			pressure := e.contextPressure()
			trigger := contract.InvalidationPressure
			if pressure.Ratio < maintenanceFloorRatio {
				trigger = contract.InvalidationBoundary
			}
			if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
				Cause: contract.InvalidationWindowDrop, Trigger: trigger,
				Scope:    fmt.Sprintf("request window dropped %d previously transmitted unit(s)", built.DroppedUnits),
				Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
			}); err != nil {
				return "", stats, err
			}
		}
		messages := built.Messages
		// Feature 006: project memory is a file the agent edits with its ordinary
		// tools, so answers no longer carry a <project-memory> trailer to hide — live
		// tokens stream straight through to display.
		request := contract.ChatRequest{SessionID: sessionPinMain, Messages: messages, Tools: definitions, ModelID: e.settings.Provider.ActiveModelID, ToolChoice: "auto", Reasoning: ReasoningForEffort(e.effort()), OnToken: e.callbacks.Token, OnReasoningToken: e.callbacks.ReasoningToken}
		shapeRequest := request
		if normalizer, ok := e.provider.(wireRequestNormalizer); ok {
			normalizedMessages, normalizeErr := normalizer.StableRequestMessages(request)
			if normalizeErr != nil {
				return "", stats, fmt.Errorf("normalize request for prefix shape: %w", normalizeErr)
			}
			shapeRequest.Messages = normalizedMessages
		} else {
			// C2: do not silently hash pre-replay bytes — that would let a real
			// settled-byte change pass undetected by the prefix-shape guard. Mark
			// the usage record so the operator/benchmark notices, log once per
			// session, and continue (the unknown-normalizer provider may still be
			// correct in practice).
			e.recordDegradedPrefixGuard(ctx, "no wireRequestNormalizer on provider")
		}
		shape, err := NewWirePrefixShape(shapeRequest, e.lastSentMessageCount, e.history.RewriteVersion())
		if err != nil {
			return "", stats, fmt.Errorf("compute request prefix shape: %w", err)
		}
		var changeReasons []string
		if e.lastShape != nil {
			changeReasons = CompareShape(*e.lastShape, shape)
		}
		if len(changeReasons) > 0 && len(e.invalidations.EventsForRequest(e.nextRequestSeq())) == 0 {
			detail := strings.Join(changeReasons, ", ")
			return "", stats, fmt.Errorf("stable request prefix changed without an invalidation event: %s", detail)
		}
		// Reasoning effort is the user's chosen level, sent raw; the gateway
		// maps it onto each provider's supported thinking ladder. It is a
		// request parameter, not a message, so it never affects the prefix cache.
		response, err := e.provider.Chat(ctx, request)
		if err != nil {
			return "", stats, err
		}
		observation := mainUsageObservation{model: e.settings.Provider.ActiveModelID, usage: response.Usage, changeReasons: changeReasons, messageCount: len(messages)}
		if err := e.recordMainUsage(ctx, observation); err != nil {
			return "", stats, fmt.Errorf("persist request usage: %w", err)
		}
		e.lastShape = &shape
		e.lastSentMessageCount = len(messages)
		// Emit the task-cumulative usage (all streams), not this response's
		// single-request usage: the live tokens/cache tag must describe the
		// whole task so far, matching the end-of-task summary's semantics.
		e.emitTaskUsage()
		e.emitContext(response.Usage.PromptTokens)
		// T038: calibrate the token estimator from this real provider usage so the
		// pressure estimate (used when provider tokens are unavailable, e.g. the
		// bootstrap turn) tracks the actual tokenizer rather than a fixed 0.25
		// chars/token guess. promptText is the system prompt just sent; its chars
		// count toward promptTokens but are not stored in the message log.
		if response.Usage.PromptTokensAvailable {
			e.history.Calibrate(response.Usage.PromptTokens, len(promptText))
		}
		// H5: cumulative token breaker. Add this turn's tokens to the task
		// total; if the task exceeds the per-effort ceiling, force isFinal so
		// the next iteration takes the finalize path with a partial answer.
		e.taskMu.Lock()
		e.taskTokens += response.Usage.TotalTokens
		tokens := e.taskTokens
		e.taskMu.Unlock()
		if tokens > tokenCapForEffort(e.settings.Effort) && isFinal == false {
			isFinal = true
			terminateReason = "cumulative token budget exceeded for this task"
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] Cumulative token budget exceeded for this task; producing final summary now."})
		}
		if doneCriteria == "" {
			doneCriteria = extractDoneCriteria(response.Content)
		}
		calls, assistantText := response.ToolCalls, response.Content
		if !isFinal && len(calls) == 0 && e.rescue != nil {
			calls, assistantText = e.rescue(response.Content, toolNames(definitions))
		}
		if isFinal {
			if e.hasSteering() {
				if strings.TrimSpace(assistantText) != "" {
					_ = e.persistAssistant(ctx, assistantText)
				}
				continue
			}
			// G1/T020: scan the goal marker on the FINAL governed turn too. Without
			// this, a [goal:complete] / [goal:blocked] emitted on the last allowed
			// turn is dropped and the goal wrongly stays active into the next task.
			if e.scanGoalMarker(assistantText) {
				e.clearGoalSidecar()
			}
			// P2 belt-and-suspenders: a plan-mode task that hit the turn cap
			// with a plan in place is also plan-ready (the model may have
			// free-text asked to proceed instead of calling exit_plan_mode).
			e.maybeSignalPlanReady(&planReady)
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if len(calls) == 0 {
			trimmed := strings.TrimSpace(assistantText)
			if e.hasSteering() {
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				continue
			}
			if trimmed == "" && sawToolCall && emptyFinalRetries < 2 {
				emptyFinalRetries++
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "Tool results are available. Give the final answer now; call another tool only if essential."})
				continue
			}
			if e.hasIncompletePlan() && planContinues < maxPlanContinues {
				planContinues++
				turnCap = min(hardTurnCeiling, max(turnCap, turns+8))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[continue] Your plan has incomplete steps. Do not ask whether to continue; finish them now, or mark completed steps and give the final summary."})
				continue
			}
			// An active goal keeps the agent working autonomously until it emits
			// a completion or blocked marker (or hits the auto-turn cap).
			// G1/G5: also scan the marker here so a [goal:complete] / blocked
			// marker on the final assistant text terminates the task even when
			// the auto-continue logic below would otherwise force a continue.
			if e.scanGoalMarker(trimmed) {
				// G4: a terminal marker cleared the active goal — drop the sidecar
				// so it does not resurrect on resume.
				e.clearGoalSidecar()
			}
			if next := e.advanceGoal(trimmed, false); next != "" {
				turnCap = min(hardTurnCeiling, max(turnCap, turns+8))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: next})
				continue
			}
			// G4: advanceGoal returns "" when the goal ended via the idle/cap
			// guards too — drop the sidecar before finalizing so the finished
			// goal does not resurrect. Idempotent with the scanGoalMarker clear.
			e.clearGoalSidecar()
			// P2 belt-and-suspenders: a plan-mode task that ended with free
			// text (no exit_plan_mode) and a plan in place is plan-ready.
			e.maybeSignalPlanReady(&planReady)
			return e.finalize(ctx, fallbackAnswer(trimmed, filesChanged)), stats, nil
		}

		sawToolCall = true
		// G1: also scan the goal marker on text accompanying tool calls. Without
		// this, [goal:complete] emitted in the same turn as a tool call is dropped.
		if e.scanGoalMarker(assistantText) {
			// G4: a terminal marker on a tool-call turn clears the sidecar too.
			e.clearGoalSidecar()
		} else {
			// B4: a turn that made tool calls is forward motion — reset the
			// consecutive-idle counter so it never accumulates across productive
			// turns.
			e.markGoalToolProgress()
		}
		if strings.TrimSpace(assistantText) != "" {
			if err := e.persistMessage(ctx, "assistant", "message", assistantText, map[string]any{"role": "assistant", "content": assistantText, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
		}
		e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: assistantText, ToolCalls: calls})
		outcomes := e.executeBatch(ctx, calls, definitions, live)
		planExited := false
		for _, outcome := range outcomes {
			toolCalls++
			// P2: exit_plan_mode surfaces a sentinel so we can break out of the turn
			// loop without polluting the assistant text with a fake result. We still
			// record the (sentinel) outcome for context fidelity and finalize.
			if errors.Is(outcome.Err, contract.ErrPlanModeExited) {
				// T018/REV B2b: mark plan-ready but DO NOT early-return here. Every
				// announced tool_call in this batch must still receive a tool result
				// below, or the persisted history holds an assistant tool_calls turn
				// with fewer results than calls — a malformed sequence the provider
				// rejects (400) on the next request. The exit sentinel is not a real
				// failure; the finalize happens AFTER the loop appends every result.
				planExited = true
			} else if outcome.Failed {
				consecutiveFailures++
				// H5: record the failed call's turn in the sliding window for the
				// distinct-failure terminator. The windowed count (not the
				// reset-on-success consecutive counter) is what stops a model
				// alternating between distinct failing actions from looping to
				// the 120-turn ceiling.
				e.recordTaskFailure(turns)
			} else {
				consecutiveFailures = 0
			}
			if isCheckCall(outcome.Call) && !outcome.Failed {
				checksRun++
			}
			trackChanged(outcome, filesChanged)
			e.trackKnowledge(outcome)
			if err := e.persistMessage(ctx, "tool", outcome.Call.ToolName(), outcome.Output, map[string]any{"role": "tool", "name": outcome.Call.ToolName(), "input": outcome.Call.ArgumentsJSON(), "output": outcome.Output, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
			e.history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: outcome.Call.ID, Content: outcome.Output})
		}
		if planExited {
			// All tool results for this batch are appended, so the assistant/tool
			// pairing is well-formed. P2: signal plan-ready so the TUI opens the
			// Proceed now / Proceed later / Keep planning modal. Interactive runs
			// (TaskComplete wired) leave plan mode ON for the modal; one-shot runs
			// resolve to proceed-later so a later "proceed" executes the saved plan.
			planReady = true
			e.SetPlanPhase(contract.PlanPhaseReady) // 004 US2 (T2): plan ready for a decision
			if e.callbacks.TaskComplete == nil {
				e.SetPendingPlan(true)
				e.SetPlanMode(false)
				e.SetPlanPhase(contract.PlanPhasePending) // T6: one-shot resolves to proceed-later
				return e.finalize(ctx, "Plan ready — saved. Say 'proceed' (or 'go ahead') to execute it."), stats, nil
			}
			return e.finalize(ctx, "Plan ready — awaiting Proceed now / Proceed later / Keep planning."), stats, nil
		}
		// B7: all-failed-turns detector. A turn in which EVERY tool call failed is
		// a strong loop signal the per-call storm breaker can miss (e.g. the model
		// alternates between distinct failing tools, so no single (tool,error)
		// class reaches 3). Two consecutive all-failed turns inject one loop-guard
		// notice; any success resets the streak.
		if allFailed(outcomes) {
			allFailedTurnStreak++
			if allFailedTurnStreak == 2 {
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[loop guard] Every tool call has failed for two turns running. Stop repeating this approach; take a fundamentally different action, or give the final factual status and the genuine blocker."})
			}
		} else {
			allFailedTurnStreak = 0
		}
		if consecutiveFailures >= 3 {
			consecutiveFailures = 0
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "Several tools failed. Re-read their errors, change approach, and explain a genuine blocker if progress is impossible."})
		}
		// H5: distinct-failure terminator. N failed tool calls (any signatures)
		// within the last taskFailureWindow turns force-finalize instead of
		// nudging forever. The nudge-and-reset above could loop to the 120-turn
		// ceiling when a model alternated between distinct failing actions; this
		// windowed count is not reset by interspersed successes or the nudge.
		if failures := e.taskFailureWindowCount(turns); failures >= maxTaskFailures {
			terminateReason = fmt.Sprintf("repeated tool failures — %d failures in the last %d turns; stopping to report", failures, taskFailureWindow)
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Stop retrying; give the final factual status and the genuine blocker."})
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if toolCalls > budget.ToolCalls && !overBudgetNoted {
			overBudgetNoted = true
			e.taskMu.Lock()
			e.taskOverBudget = toolCalls - budget.ToolCalls
			e.taskMu.Unlock()
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] The estimated tool budget is passed. Keep required work moving, but converge: avoid new exploration, finish edits, verify once, and report."})
		}
	}
}

// sessionDefinitions returns the full, session-stable tool schema set. It never
// varies by task class or effort, so the tool block stays byte-identical across
// turns and remains inside DeepSeek's cached prefix. Per-turn permission comes
// from the task brief (e.g. agents<=0) and is enforced at execution time, not
// by adding or removing schemas.
func (e *Engine) sessionDefinitions() []contract.ToolDefinition {
	definitions := e.registry.BaseDefinitions(nil)
	definitions = append(definitions, updatePlanDefinition(), askUserDefinition(), proposeChangesDefinition())
	definitions = append(definitions, runSubagentDefinition(e.subagentSpecs()))
	// P2: exit_plan_mode lives in the byte-stable tool block regardless of
	// plan-mode state, so toggling plan mode never changes the schema set
	// (preserves the prime invariant — the cached prefix stays put).
	definitions = append(definitions, exitPlanModeDefinition())
	definitions = append(definitions, e.registry.MCPDefinitions(nil)...)
	return definitions
}

func exitPlanModeDefinition() contract.ToolDefinition {
	return definition("exit_plan_mode", "Call when your plan is complete and you are ready to hand off for execution. Only meaningful in plan mode.", map[string]any{
		"summary": map[string]any{"type": "string", "description": "One-paragraph recap of the approved plan."},
	}, []string{"summary"})
}

// subagentKind reads the "agent" field out of a run_subagent call's raw
// arguments and returns the kind. H3: malformed JSON must fail closed —
// parallel-batch decisions (engine.go:executeBatch) treat non-"general"
// kinds as parallelizable, and a malformed/unknown general-subagent call
// would otherwise be RUN IN PARALLEL while still having full mutation
// permission. Treat any decode failure or empty value as "general".
func subagentKind(call contract.ToolCall) string {
	var input struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(call.ArgumentsJSON()), &input); err != nil {
		return "general"
	}
	kind := strings.TrimSpace(input.Agent)
	if kind == "" {
		return "general"
	}
	return kind
}

// recordPlanViolationAndBlock increments the per-task violation counter and
// returns a blocked toolOutcome whose output escalates from a soft notice
// (counts 1–2) to a hard governance message (count ≥ 3 with the storm
// indicator). (P4.)
// failedCallLastError returns the cached error for an identical prior
// verbatim call, or ("", false) when no such failure has been recorded.
func failedCallLastError(c *callCounters, signature string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.failedCalls[signature]
	return v, ok
}

// normalizeError reduces an error to its first line and strips paths/numbers
// so the storm breaker keys on the failure class, not cosmetic variation.
// (H2.)
func normalizeError(msg string) string {
	msg = strings.SplitN(strings.TrimSpace(msg), "\n", 2)[0]
	// Strip absolute-looking paths and any hex-looking numbers so cosmetic
	// tweaks (different `line 32` mentions, different mtimes) collapse.
	tokens := strings.Fields(msg)
	for index, token := range tokens {
		if strings.HasSuffix(token, ":") {
			tokens[index] = token
			continue
		}
		if _, err := fmt.Sscanf(token, "%d", new(int)); err == nil {
			tokens[index] = "<num>"
			continue
		}
		if strings.HasPrefix(token, "/") || strings.Contains(token, "\\") {
			tokens[index] = "<path>"
		}
	}
	return strings.Join(tokens, " ")
}

// failedClassKey is the H2 storm-breaker key. Pure function so it's safe
// from any goroutine.
func failedClassKey(name, message string) string {
	return name + "|" + normalizeError(message)
}

// validateCallArgs (H1) checks the supplied raw JSON against the matching
// tool definition's parameter schema. Implements the four checks the
// plan requires (object parse, required keys, primitive type, enum) without
// pulling in a JSON-Schema library. ~80 lines covers the four cases.
func validateCallArgs(call contract.ToolCall, definitions []contract.ToolDefinition) error {
	raw := call.ArgumentsJSON()
	if raw == "" {
		raw = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return fmt.Errorf("%s: arguments were not valid JSON (%v)", call.ToolName(), err)
	}
	schema := findDefinition(definitions, call.ToolName())
	if schema == nil {
		return nil // unknown to the orchestrator; the registry will surface it
	}
	parameters, _ := schema.Function.Parameters["properties"].(map[string]any)
	required := stringListFromAny(schema.Function.Parameters["required"])
	for _, key := range required {
		if _, present := args[key]; !present {
			return fmt.Errorf("%s: missing required field %q (per definition)", call.ToolName(), key)
		}
	}
	for propName, propSchemaAny := range parameters {
		propSchema, ok := propSchemaAny.(map[string]any)
		if !ok {
			continue
		}
		if value, present := args[propName]; present {
			if err := checkPrimitiveType(call.ToolName(), propName, value, propSchema); err != nil {
				return err
			}
		}
	}
	return nil
}

func findDefinition(definitions []contract.ToolDefinition, name string) *contract.ToolDefinition {
	for index, definition := range definitions {
		if definition.Function.Name == name {
			return &definitions[index]
		}
	}
	return nil
}

func stringListFromAny(value any) []string {
	if values, ok := value.([]any); ok {
		result := make([]string, 0, len(values))
		for _, item := range values {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	}
	if values, ok := value.([]string); ok {
		return values
	}
	return nil
}

func checkPrimitiveType(tool, key string, value any, schema map[string]any) error {
	want := schema["type"]
	if allowed, ok := schema["enum"].([]any); ok {
		for _, candidate := range allowed {
			if enumEqual(candidate, value) {
				return nil
			}
		}
		return fmt.Errorf("%s: field %q must be one of %v", tool, key, allowed)
	}
	if want == nil {
		return nil
	}
	switch want {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: field %q must be a string, got %T", tool, key, value)
		}
	case "number", "integer":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s: field %q must be a number, got %T", tool, key, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: field %q must be a boolean, got %T", tool, key, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("%s: field %q must be an array, got %T", tool, key, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("%s: field %q must be an object, got %T", tool, key, value)
		}
	}
	return nil
}

// enumEqual (B8/T028) compares a JSON enum element against a decoded argument
// value BY TYPE: numbers as float64, strings as strings, bools as bools. Both
// sides of a JSON number decode to float64, so a numeric enum like {1,2,3}
// matches a numeric argument. The previous code stringified non-strings and
// compared against the raw element (a float64), so numeric/boolean enums
// rejected every value — including valid ones.
func enumEqual(candidate, value any) bool {
	switch c := candidate.(type) {
	case string:
		v, ok := value.(string)
		return ok && v == c
	case float64:
		v, ok := value.(float64)
		return ok && v == c
	case bool:
		v, ok := value.(bool)
		return ok && v == c
	default:
		return candidate == value
	}
}

func (e *Engine) recordPlanViolationAndBlock(sc dispatchScope, call contract.ToolCall, kind, soft string) toolOutcome {
	sc.counters.mu.Lock()
	sc.counters.planViolations++
	n := sc.counters.planViolations
	sc.counters.mu.Unlock()
	out := soft
	if n >= 3 {
		out = fmt.Sprintf("[loop guard] You have attempted %d mutations in plan mode across %s class. STOP attempting changes. Finish the plan now and call exit_plan_mode.", n, kind)
	} else {
		out = soft + " No mutating tool will succeed in plan mode."
	}
	if sc.onEnd != nil {
		sc.onEnd(call, out)
	}
	return toolOutcome{Call: call, Output: out, Failed: true}
}

func (e *Engine) executeBatch(ctx context.Context, calls []contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) []toolOutcome {
	// Only read-only subagent kinds (explore/plan/review) may run
	// concurrently. "general" subagents get the full mutating tool registry,
	// so running more than one at once risks unordered, interleaved edits to
	// the same workspace with no locking anywhere in the tool registry.
	allAgents := len(calls) > 1 && effort.ParallelAgents
	for _, call := range calls {
		allAgents = allAgents && call.ToolName() == "run_subagent" && subagentKind(call) != "general"
	}
	result := make([]toolOutcome, len(calls))
	if allAgents {
		var wait sync.WaitGroup
		for i, call := range calls {
			wait.Add(1)
			go func(index int, value contract.ToolCall) {
				defer wait.Done()
				result[index] = e.executeCall(ctx, value, definitions, effort)
			}(i, call)
		}
		wait.Wait()
		return result
	}
	for i, call := range calls {
		result[i] = e.executeCall(ctx, call, definitions, effort)
	}
	return result
}

// callCounters holds the per-scope dispatch-gate state: identical-call repeats,
// the verbatim failed-call cache, the storm-breaker class counts, and the
// plan-mode violation count. One instance per task (main loop) and one per
// subagent RUN, guarded by its own mutex so parallel subagents never share or
// race gate state. (B6.)
type callCounters struct {
	mu                sync.Mutex
	callCounts        map[string]int
	failedCalls       map[string]string
	failedClassCounts map[string]int
	planViolations    int
}

func newCallCounters() *callCounters {
	return &callCounters{
		callCounts:        map[string]int{},
		failedCalls:       map[string]string{},
		failedClassCounts: map[string]int{},
	}
}

// dispatchScope carries the scope-specific wiring for gatedExecute so ONE gate
// serves both the main loop and subagent runs without divergence. (B6.)
type dispatchScope struct {
	counters     *callCounters
	dedupe       bool // consult the inspection ledger (main loop only)
	trackStats   bool // increment engine-level discipline stats (main loop only)
	onStart      func(contract.ToolCall)
	onEnd        func(contract.ToolCall, string)
	escalate     func(string)
	dispatch     func(context.Context, contract.ToolCall) (string, error)
	postDispatch func(contract.ToolCall, string, bool, error)
}

func (e *Engine) executeCall(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) toolOutcome {
	return e.gatedExecute(ctx, call, definitions, effort, e.mainScope(definitions))
}

// mainScope is the dispatch scope for the primary agent loop: per-task counters,
// the duplicate-read guard + discipline stats, ToolStart/ToolEnd callbacks,
// loop-guard notices on the main history tail, dispatch through the synthetic-
// tool switch, and read-coverage / supersede bookkeeping. (B6.)
func (e *Engine) mainScope(definitions []contract.ToolDefinition) dispatchScope {
	return dispatchScope{
		counters:   e.taskCounters,
		dedupe:     true,
		trackStats: true,
		onStart: func(call contract.ToolCall) {
			if e.callbacks.ToolStart != nil {
				e.callbacks.ToolStart(call.ToolName(), json.RawMessage(call.ArgumentsJSON()))
			}
		},
		onEnd:    func(call contract.ToolCall, output string) { e.endTool(call.ToolName(), output) },
		escalate: func(notice string) { e.history.Append(contract.Message{Role: contract.RoleUser, Content: notice}) },
		dispatch: func(c context.Context, call contract.ToolCall) (string, error) {
			return e.executeOne(c, call, definitions)
		},
		postDispatch: func(call contract.ToolCall, output string, failed bool, dispatchErr error) {
			name := call.ToolName()
			if !failed && readonlyTools[name] {
				e.inspection.Record(call, output)
			}
			if isMutation(name) && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
				e.history.MarkSuperseded(e.inspection.InvalidateFor(call))
			}
		},
	}
}

// gatedExecute is the SINGLE shared tool-dispatch gate used by both the main
// loop and every subagent run (contracts/dispatch-gate.md). Scope-specific
// behaviour is carried by sc, so subagents no longer bypass validation, the
// failed-call cache, the repeat limiter, or the storm breaker. (B6/T024/T025.)
func (e *Engine) gatedExecute(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile, sc dispatchScope) toolOutcome {
	name := call.ToolName()
	if sc.onStart != nil {
		sc.onStart(call)
	}
	end := func(output string) {
		if sc.onEnd != nil {
			sc.onEnd(call, output)
		}
	}
	// H1: pre-dispatch argument validation against the definitions offered this turn.
	if err := validateCallArgs(call, definitions); err != nil {
		output := err.Error() + " Re-emit the call with well-formed arguments."
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true, Err: err}
	}
	// H2: verbatim failed-call short-circuit.
	signature := callSignature(call)
	if prior, had := failedCallLastError(sc.counters, signature); had {
		output := fmt.Sprintf("This exact call already failed: %s. Do not repeat verbatim — change your approach (e.g. re-read the file first).", prior)
		end(output)
		e.recordFailedCall(sc, signature, output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	// Plan-mode gate.
	if e.PlanMode() {
		if name == "run_shell" {
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(call.ArgumentsJSON()), &args) == nil && IsReadOnlyShell(args.Command) {
				// Allow read-only shell probes through.
			} else {
				return e.recordPlanViolationAndBlock(sc, call, "shell", "run_shell blocked in plan mode. Only read-only shell probes (git status/diff/log, ls, grep, cat, head, etc.) are allowed.")
			}
		} else if isMutation(name) {
			if strings.HasPrefix(name, "mcp__") {
				// 004 US3 (T033): name the allowed alternative instead of a bare block.
				return e.recordPlanViolationAndBlock(sc, call, "mcp", "MCP tools are unavailable in plan mode. Plan with the workspace read tools (read_file, grep, glob, git_status, git_diff) instead.")
			}
			return e.recordPlanViolationAndBlock(sc, call, "mutation", "Plan mode is read-only — finish planning and call exit_plan_mode.")
		}
	}
	// Repeat limiter.
	sc.counters.mu.Lock()
	sc.counters.callCounts[signature]++
	repeats := sc.counters.callCounts[signature]
	sc.counters.mu.Unlock()
	if repeats > 3 {
		output := "Blocked: identical call repeated three times; take a different action or finish."
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	// Duplicate-read guard (main scope only; the inspection ledger is main-task state).
	if sc.dedupe {
		if entry, duplicate := e.inspection.Duplicate(call, e.history.IsToolResultIntact); duplicate {
			if sc.trackStats {
				e.taskMu.Lock()
				e.taskDuplicates++
				e.taskMu.Unlock()
			}
			output := fmt.Sprintf("Blocked: unchanged result already in context from call %s. Use that result; do not re-read.", entry.CallID)
			end(output)
			return toolOutcome{Call: call, Output: output, Failed: false}
		}
	}
	// H4: cancel-safe pairing before dispatch.
	if ctx.Err() != nil {
		output := "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	output, dispatchErr := sc.dispatch(ctx, call)
	if dispatchErr != nil && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		output = fmt.Sprintf("Tool %s failed: %v", name, dispatchErr)
	}
	// Re-check after dispatch in case cancellation landed during execution.
	if ctx.Err() != nil && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		output = "[cancelled by user before execution]"
		end(output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	output = CapToolOutput(output, effort.ToolOutputCap)
	failed := errors.Is(dispatchErr, contract.ErrPlanModeExited) || IsToolFailure(output, dispatchErr)
	if sc.postDispatch != nil {
		sc.postDispatch(call, output, failed, dispatchErr)
	}
	end(output)
	if failed && !errors.Is(dispatchErr, contract.ErrPlanModeExited) {
		e.recordFailedCall(sc, signature, output)
	} else if !failed {
		sc.counters.mu.Lock()
		delete(sc.counters.failedCalls, signature)
		sc.counters.mu.Unlock()
	}
	return toolOutcome{Call: call, Output: output, Failed: failed, Err: dispatchErr}
}

// recordFailedCall caches the last error for a verbatim signature and bumps
// the storm-breaker counter keyed on (toolName, normalizedError). At 3 hits
// on the same class within a task, appends a loop-guard governor notice.
func (e *Engine) recordFailedCall(sc dispatchScope, signature, output string) {
	class := failedClassKey(callName(signature), output)
	sc.counters.mu.Lock()
	sc.counters.failedCalls[signature] = output
	sc.counters.failedClassCounts[class]++
	hits := sc.counters.failedClassCounts[class]
	sc.counters.mu.Unlock()
	if hits == 3 && sc.escalate != nil {
		sc.escalate(fmt.Sprintf("[loop guard] The same failure keeps recurring with %s. Stop retrying variations; take a fundamentally different action or finish.", callName(signature)))
	}
}

// allFailed reports whether a non-empty batch of outcomes all failed. (B7.)
func allFailed(outcomes []toolOutcome) bool {
	if len(outcomes) == 0 {
		return false
	}
	for _, outcome := range outcomes {
		if !outcome.Failed {
			return false
		}
	}
	return true
}

// callName extracts the leading tool-name from a signature.
func callName(signature string) string {
	for index := 0; index < len(signature); index++ {
		if signature[index] == ' ' {
			return signature[:index]
		}
	}
	return signature
}

func (e *Engine) executeOne(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition) (string, error) {
	name := call.ToolName()
	switch name {
	case "update_plan":
		return e.updatePlan(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "ask_user":
		return e.askUser(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "propose_changes":
		return e.proposeChanges(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "run_subagent":
		return e.runSubagentTool(ctx, json.RawMessage(call.ArgumentsJSON()))
	case "exit_plan_mode":
		// T017/REV B2a: only meaningful in plan mode. A stray exit_plan_mode call
		// while plan mode is OFF must be a harmless no-op — it must NOT end the
		// task or set a phantom pending plan. Only when plan mode is on do we
		// raise the finalize sentinel.
		if !e.PlanMode() {
			return "not in plan mode — continue with the task", nil
		}
		return "", contract.ErrPlanModeExited // P2: a soft signal that the task should finalize
	default:
		allowed := make(map[string]bool)
		for _, definition := range definitions {
			allowed[definition.Function.Name] = true
		}
		return e.registry.Execute(ctx, name, json.RawMessage(call.ArgumentsJSON()), allowed)
	}
}

func (e *Engine) endTool(name, output string) {
	if e.callbacks.ToolEnd != nil {
		e.callbacks.ToolEnd(name, output)
	}
}

func (e *Engine) updatePlan(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Steps []contract.PlanStep `json:"steps"`
		Note  string              `json:"note"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Steps) == 0 || len(input.Steps) > 12 {
		return "", errors.New("plan requires 1-12 steps")
	}
	sawActive := false
	for i := range input.Steps {
		input.Steps[i].Title = strings.TrimSpace(input.Steps[i].Title)
		if input.Steps[i].Title == "" {
			return "", errors.New("plan step title is empty")
		}
		if input.Steps[i].Status == contract.PlanInProgress {
			if sawActive {
				input.Steps[i].Status = contract.PlanPending
			}
			sawActive = true
		}
		if input.Steps[i].Status != contract.PlanPending && input.Steps[i].Status != contract.PlanInProgress && input.Steps[i].Status != contract.PlanCompleted {
			return "", fmt.Errorf("invalid plan status %q", input.Steps[i].Status)
		}
	}
	plan := contract.Plan{Steps: input.Steps, Note: truncateEllipsis(input.Note, 400), UpdatedAt: time.Now().UTC()}
	e.mu.Lock()
	e.plan = plan
	e.mu.Unlock()
	// 004 US2: lifecycle transitions driven by plan edits. In plan mode the model
	// is drafting (this also moves a just-superseded plan on to drafting). Out of
	// plan mode, the first step that starts or completes means a saved plan is
	// being executed — the phrasing-independent backstop (T8) for a go-ahead that
	// the continuation matchers (T7) did not catch.
	if e.PlanMode() {
		e.SetPlanPhase(contract.PlanPhaseDrafting)
	} else if phase := e.PlanPhase(); phase == contract.PlanPhasePending || phase == contract.PlanPhaseInterrupted || phase == contract.PlanPhaseReady {
		if planHasProgressingStep(input.Steps) {
			e.SetPendingPlan(false)
			e.SetPlanPhase(contract.PlanPhaseExecuting)
		}
	}
	if e.persistence.WritePlan != nil {
		if err := e.persistence.WritePlan(ctx, planMarkdown(plan)); err != nil {
			return "", err
		}
	}
	if e.callbacks.PlanUpdate != nil {
		e.callbacks.PlanUpdate(plan)
	}
	completed := 0
	for _, step := range plan.Steps {
		if step.Status == contract.PlanCompleted {
			completed++
		}
	}
	return fmt.Sprintf("Plan updated: %d/%d complete.", completed, len(plan.Steps)), nil
}

func (e *Engine) askUser(ctx context.Context, raw json.RawMessage) (string, error) {
	var input struct {
		Questions []contract.Question `json:"questions"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	if len(input.Questions) == 0 || len(input.Questions) > 3 || e.callbacks.Ask == nil {
		return "", errors.New("ask_user requires 1-3 questions and an interactive client")
	}
	// Each question must carry at least two choices (the tool schema requires it,
	// but the payload is model-controlled and smaller models sometimes omit them).
	// Reject a choiceless question with a tool error the model can act on, rather
	// than passing it to the modal where indexing an empty choice slice would panic.
	for i, q := range input.Questions {
		if len(q.Choices) < 2 {
			return "", fmt.Errorf("ask_user question %d (%q) must include at least 2 choices", i+1, q.Question)
		}
	}
	answers, err := e.callbacks.Ask(ctx, input.Questions)
	if err != nil {
		return "", err
	}
	return encodeAnswers(answers), nil
}

func (e *Engine) proposeChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.callbacks.Ask == nil {
		// 004 US3 (T036): label the non-interactive verdict honestly — no human
		// reviewed this proposal, so the model must not read it as human approval.
		return `{"verdict":"auto_approved","note":"auto-approved by non-interactive policy — no human reviewed this proposal; proceed minimally and verify each change yourself"}`, nil
	}
	var input struct {
		Summary        string `json:"summary"`
		EstimatedSteps int    `json:"estimatedSteps"`
		Files          []any  `json:"files"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", err
	}
	answers, err := e.callbacks.Ask(ctx, []contract.Question{{Question: fmt.Sprintf("Approve this change plan? (%d files, ~%d steps)\n%s", len(input.Files), input.EstimatedSteps, input.Summary), Choices: []contract.QuestionChoice{{Label: "Proceed", Description: "Apply and verify the plan", Recommended: true}, {Label: "Proceed carefully", Description: "Minimize each edit and stop on surprises"}, {Label: "Stop", Description: "Do not edit"}}}})
	if err != nil {
		return "", err
	}
	index := 0
	if len(answers) > 0 {
		index = answers[0].Index
	}
	if index == 2 {
		return `{"verdict":"rejected","instruction":"Do not edit; summarize the plan and wait."}`, nil
	}
	if index == 1 {
		return `{"verdict":"approved_with_caution","instruction":"Make the smallest changes and verify each file."}`, nil
	}
	return `{"verdict":"approved","instruction":"Execute and verify the plan."}`, nil
}

func (e *Engine) hasIncompletePlan() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, step := range e.plan.Steps {
		if step.Status != contract.PlanCompleted {
			return true
		}
	}
	return false
}

// appendCompletionDisclosure (004 US1, T014/FR-017) appends an honest note when
// the model finalizes a plan that is executing (or resumed-interrupted) with
// steps still open, so the final answer never implies work it did not do. A
// completed plan, a pre-execution plan, or a non-plan task is untouched — the
// note is driven entirely by recorded step state, not the model's self-report
// (research B3: the launch model tends to over-claim completion).
func (e *Engine) appendCompletionDisclosure(content string) string {
	phase := e.PlanPhase()
	if phase != contract.PlanPhaseExecuting && phase != contract.PlanPhaseInterrupted {
		return content
	}
	plan := e.CurrentPlan()
	done, total := planStepProgress(plan)
	if total == 0 || done >= total {
		return content
	}
	var open []string
	for _, step := range plan.Steps {
		if step.Status != contract.PlanCompleted {
			open = append(open, step.Title)
			if len(open) == 3 {
				break
			}
		}
	}
	return content + fmt.Sprintf("\n\n— %d of %d plan steps incomplete: %s", total-done, total, strings.Join(open, "; "))
}

func (e *Engine) drainSteering(ctx context.Context) bool {
	e.mu.Lock()
	queued := append([]string(nil), e.steering...)
	e.steering = nil
	e.mu.Unlock()
	for _, text := range queued {
		_ = e.persistMessage(ctx, "user", "message", text, map[string]any{"role": "user", "content": text, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)})
		e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[Mid-task message from the user; incorporate now and keep valid completed work]\n" + text})
	}
	if len(queued) > 0 {
		e.callbacks.EmitStatus("Incorporating your message...")
	}
	return len(queued) > 0
}

func (e *Engine) hasSteering() bool {
	e.mu.Lock()
	has := len(e.steering) > 0
	e.mu.Unlock()
	return has
}

func (e *Engine) compact(ctx context.Context, reason string) error {
	messages := e.history.All()
	start := max(0, len(messages)-60)
	var lines []string
	for _, message := range messages[start:] {
		content := truncateEllipsis(strings.Join(strings.Fields(message.Content), " "), 500)
		lines = append(lines, fmt.Sprintf("- %s: %s", message.Role, content))
	}
	// T042: digests accumulate, so the summarizer covers ONLY the region being
	// folded — prior digests are preserved verbatim (via CompactTo accumulation)
	// and carried forward in the request prefix, never re-summarized.
	request := []contract.Message{{Role: contract.RoleSystem, Content: "Compress a coding-agent session under headings GOAL, STATE, FILES, DECISIONS, COMMANDS, PENDING. Preserve all durable facts and exact paths; use terse bullets."}, {Role: contract.RoleUser, Content: "Reason: " + reason + "\nDurable facts:\n" + strings.Join(e.knowledge.CompactionFacts(), "\n") + "\nConversation:\n" + strings.Join(lines, "\n")}}
	temperature := .1
	// Compaction shares the main stream's pin: it uses ActiveModelID, so it
	// belongs under the same gateway routing pin as the main loop (C1).
	// C3/T011: this is an intentionally COLD, one-shot isolated stream — a single
	// request built from a fresh system+user pair, never appended to. It is not
	// covered by the main-loop prefix-shape guard (there is no prior shape to
	// compare against and no settled prefix to preserve), and that is correct:
	// its cache miss is a deliberate, once-per-compaction cost.
	// T042: bound the summarizer to 90s and allow one retry on a non-timeout
	// failure; a timeout or a second failure falls through to the mechanical
	// fallback below so compaction always frees context and never loops.
	summaryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var response contract.ChatResponse
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		response, err = e.provider.Chat(summaryCtx, contract.ChatRequest{SessionID: e.session.ID + ":main", Messages: request, ModelID: e.settings.Provider.ActiveModelID, MaxTokens: 1600, Temperature: &temperature, Reasoning: contract.ReasoningLow})
		if err == nil || summaryCtx.Err() != nil {
			break
		}
	}
	summary := ""
	if usageErr := e.recordAuxUsage(ctx, e.settings.Provider.ActiveModelID, response.Usage); usageErr != nil {
		return fmt.Errorf("persist compaction usage: %w", usageErr)
	}
	e.emitTaskUsage()
	if err == nil {
		summary = strings.TrimSpace(response.Content)
	}
	if summary == "" {
		summary = strings.Join(lines, "\n")
	}
	// T042: pass just the digest (accumulated by CompactTo). The workspace path
	// already lives in the cache-stable system prompt, so repeating it per
	// accumulated digest would only bloat the summary.
	e.history.CompactTo(summary, 2)
	if e.persistence.AddEvent != nil {
		_ = e.persistence.AddEvent(ctx, "system", "compact_summary", e.redact(summary))
	}
	return nil
}

func (e *Engine) needsCompact(profile EffortProfile) bool {
	return e.contextPressure().Ratio >= profile.CompactThreshold
}

func (e *Engine) contextPressure() pressureSnapshot {
	e.taskMu.Lock()
	reported, available := e.latestPromptTokens, e.latestPromptAvailable
	e.taskMu.Unlock()
	input := e.history.PressureInput(reported, available)
	usable := max(8000, e.contextLimit()-outputReserveTokens)
	return pressureSnapshot{Tokens: input.Tokens, Estimated: input.Estimated, Ratio: float64(input.Tokens) / float64(usable)}
}

func (e *Engine) runMaintenanceBoundary(ctx context.Context, profile EffortProfile) (MaintenanceResult, error) {
	pressure := e.contextPressure()
	e.taskMu.Lock()
	if pressure.Ratio < maintenanceFloorRatio {
		// C4: do NOT reset the maintenance latch on transient pressure dips.
		// Oscillating 0.59 ↔ 0.61 used to refill the latch reset budget and
		// pay repeated full-prefix resets. The latch now flips only on a
		// genuine compaction (resetMaintenanceLatch) or session restart.
		latched := e.maintenanceLatched
		passes := e.maintenancePasses
		e.taskMu.Unlock()
		_ = passes
		if latched {
			return MaintenanceResult{}, nil
		}
		return MaintenanceResult{}, nil
	}
	latched := e.maintenanceLatched
	e.taskMu.Unlock()
	if latched {
		return MaintenanceResult{}, nil
	}

	// A1/T008: estimate the reclaimable yield BEFORE mutating history. Below the
	// minimum-yield floor (5% of the usable window) we skip entirely — no
	// Maintain call, so no settled bytes change, no invalidation event is owed,
	// and no latch slot is consumed. This closes the REV A1 trap: previously
	// Maintain ran first and a trim-only change below the hard-fold threshold
	// returned WITHOUT recording an event, so the next request tripped the
	// prefix-shape guard with an unexplained change and hard-failed the session.
	// Near the hard-fold threshold we always reclaim (compaction is imminent
	// anyway), matching Reasonix's force semantics.
	usable := max(8000, e.contextLimit()-outputReserveTokens)
	minYield := usable * maintenanceMinYieldPercent / 100
	if e.history.EstimateMaintainYield(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4) < minYield &&
		pressure.Ratio < maintenanceHardFoldRatio {
		return MaintenanceResult{}, nil
	}

	result := e.history.Maintain(profile.KeepFullToolOutputs, profile.TrimmedToolOutputChars, 4)
	if !result.Changed {
		// Estimate said there was yield but a concurrent change consumed it;
		// nothing mutated, so nothing is owed. Any actual change below falls
		// through to a recorded invalidation — mutation and event are now
		// inseparable.
		return result, nil
	}
	cause := contract.InvalidationTrim
	parts := make([]string, 0, 2)
	if result.Folded {
		cause = contract.InvalidationFold
		parts = append(parts, fmt.Sprintf("folded completed-task context; reduced %d estimated token(s)", result.FoldedTokens))
	}
	if result.TrimmedTools > 0 {
		parts = append(parts, fmt.Sprintf("trimmed %d aged tool result(s)", result.TrimmedTools))
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: cause, Trigger: contract.InvalidationPressure, Scope: strings.Join(parts, "; "),
		Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return result, err
	}
	e.taskMu.Lock()
	e.maintenancePasses++
	if e.maintenancePasses >= 2 {
		e.maintenanceLatched = true
	}
	e.taskMu.Unlock()
	return result, nil
}

func (e *Engine) applyBoundaryToolChange(ctx context.Context) error {
	if e.boundaryTools == nil {
		return nil
	}
	change, changed, err := e.boundaryTools()
	if err != nil || !changed {
		return err
	}
	if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
		Cause: contract.InvalidationToolsetChange, Trigger: contract.InvalidationBoundary,
		Scope: change.Scope, RequestSeq: e.nextRequestSeq(),
	}); err != nil {
		return err
	}
	e.registry.ReplacePrefix("mcp__", change.Tools...)
	return nil
}

func (e *Engine) recordInvalidation(ctx context.Context, event contract.InvalidationEvent) error {
	if e.invalidations == nil {
		return errors.New("invalidation ledger is unavailable")
	}
	return e.invalidations.Record(ctx, event)
}

func (e *Engine) nextRequestSeq() int {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.requestSeq + 1
}

func (e *Engine) resetMaintenanceLatch() {
	e.taskMu.Lock()
	e.maintenancePasses = 0
	e.maintenanceLatched = false
	e.softNoticeShown = false // T039: allow the soft advisory to fire again next cycle
	e.taskMu.Unlock()
}

// recordDegradedPrefixGuard writes once-per-session a usage-record marker so
// the prefix-shape guard's reduced fidelity is visible in benchmark logs and
// could trip a downstream alert. C2: never silently hash pre-replay bytes.
var degradedPrefixGuardLogged = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

func (e *Engine) recordDegradedPrefixGuard(_ context.Context, reason string) {
	detail := "degraded-prefix-guard:" + reason
	degradedPrefixGuardLogged.Lock()
	first := !degradedPrefixGuardLogged.seen[detail]
	degradedPrefixGuardLogged.seen[detail] = true
	degradedPrefixGuardLogged.Unlock()
	if first {
		// Keep this lossy, single-line, and unambiguous; it must never carry
		// request bytes (which would mutate cached content) and is operator
		// facing in cache logs only.
		fmt.Fprintf(testWriterOrStdout(), "prefix-guard degraded: %s (session=%s)\n", reason, e.session.ID)
	}
	e.taskMu.Lock()
	e.lastShape = nil // force the next-guard loop into the relaxed branch
	e.taskMu.Unlock()
}

// testWriterOrStdout is exported in tests so the degraded guard message lands
// on the test output rather than disappearing into process stderr.
var degradedGuardWriter func() io.Writer

func testWriterOrStdout() io.Writer {
	if degradedGuardWriter != nil {
		return degradedGuardWriter()
	}
	return os.Stderr
}

func floatPointer(value float64) *float64 {
	copy := value
	return &copy
}

func (e *Engine) contextLimit() int {
	for _, model := range e.settings.Provider.Models {
		if model.ID == e.settings.Provider.ActiveModelID && model.ContextLimit > 0 {
			return model.ContextLimit
		}
	}
	return 128000
}

func (e *Engine) emitContext(lastRequest int) {
	history := e.history.EstimatedTokens()
	used := max(history, lastRequest)
	percent := float64(used) / float64(max(1, e.contextLimit())) * 100
	e.taskMu.Lock()
	e.taskPeakContext = max(e.taskPeakContext, percent)
	e.taskMu.Unlock()
	if e.callbacks.Context != nil {
		e.callbacks.Context(contract.ContextInfo{HistoryTokens: history, LastRequestTokens: lastRequest, ContextLimit: e.contextLimit(), Percent: min(100, percent)})
	}
}

func (e *Engine) persistMessage(ctx context.Context, role, kind, content string, transcript map[string]any) error {
	if e.persistence.AddEvent != nil {
		if err := e.persistence.AddEvent(ctx, role, kind, e.redact(content)); err != nil {
			return err
		}
	}
	if e.persistence.AppendTranscript != nil {
		if err := e.persistence.AppendTranscript(ctx, transcript); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) persistAssistant(ctx context.Context, content string) error {
	if err := e.persistMessage(ctx, "assistant", "message", content, map[string]any{"role": "assistant", "content": content, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return err
	}
	e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: content})
	return nil
}

func (e *Engine) finalize(ctx context.Context, content string) string {
	if strings.TrimSpace(content) == "" {
		content = "Done."
	}
	// 004 US1 (T014/FR-017): reconcile the answer against recorded plan state so a
	// finalized-but-incomplete plan discloses its open steps instead of implying
	// the work is done. Uses only recorded state — no extra model call, no turns.
	content = e.appendCompletionDisclosure(content)
	// G1/T020 last-chance sweep: a terminal goal marker in the final answer must
	// terminate the goal even if no earlier branch scanned it.
	if e.scanGoalMarker(content) {
		e.clearGoalSidecar()
	}
	// The task is ending regardless: the user must still see the answer even
	// if it could not be persisted, so a persistence failure here is
	// surfaced as a warning rather than turned into a task error (which
	// would discard the successful answer along with it).
	if err := e.persistAssistant(ctx, content); err != nil {
		e.callbacks.EmitStatus("Warning: failed to save the final answer to session history: " + err.Error())
	}
	// G1/T020: goal control markers are for the engine's parser, not the user.
	// Strip them from the DISPLAYED answer while leaving persisted history intact
	// for parsing on resume.
	return StripGoalMarkers(content)
}

func (e *Engine) redact(value string) string {
	if e.redactFn != nil {
		return e.redactFn(value)
	}
	return value
}

// persistProjectCursor (005 US3 / 006) atomically persists the advanced applied
// instruction/memory hashes into the project-context sidecar, preserving the boot
// snapshot's RenderedBootContext and other fields. It writes only when a hash
// actually changed, off the hot path under writeMu like the goal sidecar.
func (e *Engine) persistProjectCursor(ctx context.Context) {
	if e.persistence.WriteProjectContext == nil || e.projectContext == nil {
		return
	}
	if e.projectContext.AppliedMemoryHash == e.appliedMemoryHash && e.projectContext.AppliedInstructionHash == e.appliedInstructionsHash {
		return
	}
	snapshot := *e.projectContext
	snapshot.AppliedMemoryHash = e.appliedMemoryHash
	snapshot.AppliedInstructionHash = e.appliedInstructionsHash
	e.writeMu.Lock()
	_ = e.persistence.WriteProjectContext(ctx, snapshot)
	e.writeMu.Unlock()
	e.projectContext = &snapshot
}

func (e *Engine) addTaskAgentUsage(usage contract.Usage) {
	e.taskMu.Lock()
	e.taskAgentUsage = e.taskAgentUsage.Add(usage)
	e.taskMu.Unlock()
}

// tokenCapForEffort (H5) maps the user's chosen effort tier to a per-task
// cumulative token ceiling. Defaults scaled by effort to match Reasonix's
// per-tier scale loosely. The breaker prevents runaway tasks from bringing
// the user back to a cache-cold prompt.
func tokenCapForEffort(effort contract.EffortLevel) int {
	switch effort {
	case contract.EffortLow:
		return 400_000
	case contract.EffortMedium:
		return 1_000_000
	case contract.EffortHigh:
		return 2_500_000
	default:
		return 4_000_000 // EffortMax
	}
}

// recordTaskFailure (H5) appends the current turn to the per-task failure
// window. Called once per failed tool outcome. The slice is pruned of stale
// entries by taskFailureWindowCount on the threshold check.
func (e *Engine) recordTaskFailure(turn int) {
	e.taskMu.Lock()
	e.taskFailures = append(e.taskFailures, turn)
	e.taskMu.Unlock()
}

// taskFailureWindowCount (H5) returns the count of failed tool calls within
// the last taskFailureWindow turns (current turn inclusive), pruning entries
// that have aged out of the window as it goes.
func (e *Engine) taskFailureWindowCount(currentTurn int) int {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	cutoff := currentTurn - taskFailureWindow
	kept := e.taskFailures[:0]
	for _, turn := range e.taskFailures {
		if turn > cutoff {
			kept = append(kept, turn)
		}
	}
	e.taskFailures = kept
	return len(e.taskFailures)
}

func (e *Engine) recordMainUsage(ctx context.Context, observation mainUsageObservation) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	previous := lastMainUsageRecord(e.usageRecords)
	newTail := estimatedNewTail(previous, observation.usage)
	attribution := e.mainCacheAttribution(cacheMissContext{previous: previous, usage: observation.usage, newTail: newTail, previousMessageCount: e.lastSentMessageCount, currentMessageCount: observation.messageCount}, observation.changeReasons)
	record := usageRecord(usageRecordInput{model: observation.model, stream: contract.UsageStreamMain, usage: observation.usage, reasons: observation.changeReasons, attribution: attribution})
	if previous != nil && (observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0) {
		record.NewTailTokens = intPointer(newTail)
	}
	if err := e.appendUsageLocked(ctx, &record); err != nil {
		return err
	}
	if observation.usage.PromptTokensAvailable || observation.usage.PromptTokens != 0 {
		e.latestPromptTokens = observation.usage.PromptTokens
		e.latestPromptAvailable = true
	}
	e.firstAfterStart = false
	return nil
}

func estimatedNewTail(previous *contract.UsageRecord, usage contract.Usage) int {
	if previous == nil || previous.PromptTokens == nil || (!usage.PromptTokensAvailable && usage.PromptTokens == 0) {
		return 0
	}
	return max(0, usage.PromptTokens-*previous.PromptTokens)
}

func (e *Engine) mainCacheAttribution(missContext cacheMissContext, changeReasons []string) contract.CacheAttribution {
	if missContext.usage.CacheReadTokens == nil || missContext.usage.CacheMissTokens == nil {
		return contract.CacheAttributionNA
	}
	if e.firstAfterStart {
		return contract.CacheAttributionColdStart
	}
	if len(changeReasons) > 0 {
		return contract.CacheAttributionAgent
	}
	if suspiciousCacheMiss(missContext) {
		return contract.CacheAttributionAgentSuspect
	}
	return contract.CacheAttributionProvider
}

func (e *Engine) recordAuxUsage(ctx context.Context, model string, usage contract.Usage) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	record := usageRecord(usageRecordInput{model: model, stream: contract.UsageStreamAux, usage: usage, attribution: contract.CacheAttributionNA})
	return e.appendUsageLocked(ctx, &record)
}

// emitTaskUsage pushes the cumulative usage of the CURRENT task — every
// request since the task's start across the main, subagent, and aux streams —
// to the Usage callback. Called after each recorded request so the TUI's live
// tokens count and cache tag always describe the full task lifecycle and land
// on the same numbers the end-of-task summary reports.
func (e *Engine) emitTaskUsage() {
	if e.callbacks.Usage == nil {
		return
	}
	e.taskMu.Lock()
	usage := subtractUsage(e.sessionUsage, e.taskUsageStart)
	e.taskMu.Unlock()
	e.callbacks.Usage(usage)
}

func (e *Engine) recordIsolatedUsage(ctx context.Context, model string, usage contract.Usage, coldStart bool, reasons []string) error {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	// B9: subagent usage counts toward the per-task token breaker. The main loop
	// accumulates only its own response usage (~line 750), so without this a
	// runaway subagent's token burn is invisible to the breaker. Same taskMu.
	e.taskTokens += usage.TotalTokens
	attribution := contract.CacheAttributionNA
	if usage.CacheReadTokens != nil && usage.CacheMissTokens != nil {
		switch {
		case coldStart:
			attribution = contract.CacheAttributionColdStart
		case len(reasons) > 0:
			attribution = contract.CacheAttributionAgent
		default:
			attribution = contract.CacheAttributionProvider
		}
	}
	record := usageRecord(usageRecordInput{model: model, stream: contract.UsageStreamSubagent, usage: usage, reasons: reasons, attribution: attribution})
	return e.appendUsageLocked(ctx, &record)
}

func (e *Engine) appendUsageLocked(ctx context.Context, record *contract.UsageRecord) error {
	record.Seq = e.requestSeq + 1
	if record.At.IsZero() {
		record.At = time.Now().UTC()
	}
	if e.persistence.AppendUsage != nil {
		if err := e.persistence.AppendUsage(ctx, *record); err != nil {
			return err
		}
	}
	e.requestSeq = record.Seq
	e.usageRecords = append(e.usageRecords, cloneUsageRecord(*record))
	e.usageAggregate = contract.AggregateUsage(e.usageRecords)
	e.sessionUsage = usageFromAggregate(e.usageAggregate)
	return nil
}

func usageRecord(input usageRecordInput) contract.UsageRecord {
	prompt := nullableUsageValue(input.usage.PromptTokens, input.usage.PromptTokensAvailable)
	completion := nullableUsageValue(input.usage.CompletionTokens, input.usage.CompletionTokensAvailable)
	read := cloneIntPointer(input.usage.CacheReadTokens)
	miss := cloneIntPointer(input.usage.CacheMissTokens)
	return contract.UsageRecord{
		At:                 time.Now().UTC(),
		Model:              input.model,
		Stream:             input.stream,
		PromptTokens:       prompt,
		CompletionTokens:   completion,
		CacheReadTokens:    read,
		CacheMissTokens:    miss,
		MissDerived:        input.usage.MissDerived,
		HitRate:            contract.HitRate(read, miss),
		PrefixChanged:      len(input.reasons) > 0,
		ChangeReasons:      append([]string{}, input.reasons...),
		Attribution:        input.attribution,
		UsageContradictory: input.usage.Contradictory,
		Diagnostic:         input.usage.Diagnostic,
		CostUSD:            cloneFloatPointer(input.usage.CostUSD),
		CostEstimated:      input.usage.CostEstimated,
		LogID:              input.usage.CostLogID,
	}
}

func lastMainUsageRecord(records []contract.UsageRecord) *contract.UsageRecord {
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Stream == "" || records[index].Stream == contract.UsageStreamMain {
			return &records[index]
		}
	}
	return nil
}

func suspiciousCacheMiss(missContext cacheMissContext) bool {
	previous, usage := missContext.previous, missContext.usage
	if previous == nil || previous.PromptTokens == nil || usage.CacheReadTokens == nil || usage.CacheMissTokens == nil {
		return false
	}
	if (usage.PromptTokensAvailable || usage.PromptTokens != 0) && usage.PromptTokens < *previous.PromptTokens {
		return true
	}
	minimumRead := (*previous.PromptTokens/64)*64 - 2*64
	if missContext.currentMessageCount >= missContext.previousMessageCount && *usage.CacheReadTokens < max(0, minimumRead) {
		return true
	}
	return *usage.CacheMissTokens > missContext.newTail+2*64
}

func intPointer(value int) *int { return &value }

func nullableUsageValue(value int, available bool) *int {
	if !available && value == 0 {
		return nil
	}
	copy := value
	return &copy
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func usageFromAggregate(aggregate contract.SessionUsageAggregate) contract.Usage {
	usage := contract.Usage{
		PromptTokens:              aggregate.SumPrompt,
		CompletionTokens:          aggregate.SumCompletion,
		TotalTokens:               aggregate.SumPrompt + aggregate.SumCompletion,
		CachedTokens:              aggregate.SumCacheRead,
		PromptTokensAvailable:     aggregate.PromptAvailable > 0,
		CompletionTokensAvailable: aggregate.CompletionAvailable > 0,
	}
	if aggregate.CacheAvailable > 0 {
		// Paired sums, not the one-sided display sums: sessionUsage feeds rate
		// arithmetic (the per-task delta divided by the TUI summary), and a rate
		// operand must only ever come from records that reported both sides.
		usage.CacheReadTokens = cloneIntPointer(&aggregate.PairedCacheRead)
		usage.CacheMissTokens = cloneIntPointer(&aggregate.PairedCacheMiss)
	}
	return usage
}

func cloneUsageAggregate(value contract.SessionUsageAggregate) contract.SessionUsageAggregate {
	value.SessionHitRate = cloneFloatPointer(value.SessionHitRate)
	value.SteadyStateHitRate = cloneFloatPointer(value.SteadyStateHitRate)
	value.PrefixStabilityRate = cloneFloatPointer(value.PrefixStabilityRate)
	return value
}

func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneUsageRecords(records []contract.UsageRecord) []contract.UsageRecord {
	result := make([]contract.UsageRecord, len(records))
	for i, record := range records {
		result[i] = cloneUsageRecord(record)
	}
	return result
}

func cloneUsageRecord(record contract.UsageRecord) contract.UsageRecord {
	record.PromptTokens = cloneIntPointer(record.PromptTokens)
	record.CompletionTokens = cloneIntPointer(record.CompletionTokens)
	record.CacheReadTokens = cloneIntPointer(record.CacheReadTokens)
	record.CacheMissTokens = cloneIntPointer(record.CacheMissTokens)
	record.NewTailTokens = cloneIntPointer(record.NewTailTokens)
	record.HitRate = cloneFloatPointer(record.HitRate)
	record.CostUSD = cloneFloatPointer(record.CostUSD)
	record.ChangeReasons = append([]string{}, record.ChangeReasons...)
	return record
}

func (e *Engine) trackKnowledge(outcome toolOutcome) {
	name := outcome.Call.ToolName()
	if !outcome.Failed && name == "read_file" {
		e.knowledge.NoteFile(e.workspaceCallPath(outcome.Call), summarizeRead(outcome.Output))
	}
	if isMutation(name) {
		if path := e.workspaceCallPath(outcome.Call); path != "" && (name == "edit_file" || name == "multi_edit" || name == "write_file") {
			e.knowledge.NoteFile(path, "edited")
		} else {
			e.knowledge.MarkWorkspaceChanged()
		}
	}
}

func (e *Engine) workspaceCallPath(call contract.ToolCall) string {
	path := pathArgument(call)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.session.WorkspacePath, filepath.FromSlash(path))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func updatePlanDefinition() contract.ToolDefinition {
	return definition("update_plan", "Create or update the real task plan.", map[string]any{"steps": map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "items": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}}}, "required": []string{"title", "status"}}}, "note": map[string]any{"type": "string"}}, []string{"steps"})
}

func askUserDefinition() contract.ToolDefinition {
	choice := map[string]any{"type": "object", "properties": map[string]any{"label": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "recommended": map[string]any{"type": "boolean"}}, "required": []string{"label"}}
	question := map[string]any{"type": "object", "properties": map[string]any{"question": map[string]any{"type": "string"}, "choices": map[string]any{"type": "array", "minItems": 2, "maxItems": 5, "items": choice}}, "required": []string{"question", "choices"}}
	return definition("ask_user", "Ask 1-3 blocking multiple-choice questions.", map[string]any{"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": question}}, []string{"questions"})
}

func proposeChangesDefinition() contract.ToolDefinition {
	file := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"}, "risk": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}}}, "required": []string{"path", "reason", "risk"}}
	return definition("propose_changes", "Request approval before broad or risky edits.", map[string]any{"summary": map[string]any{"type": "string"}, "files": map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": file}, "testPlan": map[string]any{"type": "string"}, "estimatedSteps": map[string]any{"type": "integer", "minimum": 1, "maximum": 40}}, []string{"summary", "files", "testPlan", "estimatedSteps"})
}

func runSubagentDefinition(specs map[string]subagentSpec) contract.ToolDefinition {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return definition("run_subagent", "Delegate one independent task to a bounded specialist.", map[string]any{"agent": map[string]any{"type": "string", "enum": names}, "title": map[string]any{"type": "string"}, "task": map[string]any{"type": "string"}}, []string{"agent", "task"})
}

func definition(name, description string, properties map[string]any, required []string) contract.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return contract.ToolDefinition{Type: "function", Function: contract.FunctionDefinition{Name: name, Description: description, Parameters: parameters}}
}

func hasDefinition(definitions []contract.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Function.Name == name {
			return true
		}
	}
	return false
}

func planMarkdown(plan contract.Plan) string {
	var lines []string
	for _, step := range plan.Steps {
		mark := " "
		if step.Status == contract.PlanCompleted {
			mark = "x"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s (%s)", mark, step.Title, step.Status))
	}
	if plan.Note != "" {
		lines = append(lines, "", plan.Note)
	}
	return strings.Join(lines, "\n") + "\n"
}

func encodeAnswers(answers []contract.Answer) string {
	values := make([]map[string]any, 0, len(answers))
	for _, answer := range answers {
		values = append(values, map[string]any{"question": answer.Question, "selected_index": answer.Index, "selected_label": answer.Choice.Label, "selected_description": answer.Choice.Description, "recommended": answer.Choice.Recommended})
	}
	payload, _ := json.MarshalIndent(map[string]any{"type": "user_answers", "answers": values}, "", "  ")
	return string(payload)
}

func fallbackAnswer(content string, changed map[string]bool) string {
	if strings.TrimSpace(content) != "" {
		return strings.TrimSpace(content)
	}
	if len(changed) > 0 {
		return fmt.Sprintf("Work completed with %d changed file(s).", len(changed))
	}
	return "Done."
}

var doneRE = regexp.MustCompile(`(?i)\bDONE\s*[:=]\s*(.{5,300})`)

func extractDoneCriteria(content string) string {
	match := doneRE.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return truncateEllipsis(strings.TrimSpace(strings.SplitN(match[1], "\n", 2)[0]), 160)
}

var checkRE = regexp.MustCompile(`(?i)\b(test|typecheck|tsc|lint|build|check|vet|pytest|vitest|jest|mypy|ruff)\b`)

func isCheckCall(call contract.ToolCall) bool {
	if call.ToolName() != "run_shell" {
		return false
	}
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &args)
	return checkRE.MatchString(args.Command)
}

func trackChanged(outcome toolOutcome, files map[string]bool) {
	if outcome.Failed {
		return
	}
	name := outcome.Call.ToolName()
	if name == "edit_file" || name == "multi_edit" || name == "write_file" {
		var args struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(outcome.Call.ArgumentsJSON()), &args)
		if args.Path != "" {
			files[filepathSlash(args.Path)] = true
		}
	} else if name == "apply_patch" {
		if match := regexp.MustCompile(`Applied patch to \d+ file\(s\): (.+)\.`).FindStringSubmatch(outcome.Output); len(match) > 1 {
			for _, path := range strings.Split(match[1], ", ") {
				files[filepathSlash(strings.TrimSpace(path))] = true
			}
		}
	}
}

func summarizeRead(output string) string {
	if match := regexp.MustCompile(`\(lines (\d+-\d+) of (\d+)\)`).FindStringSubmatch(output); len(match) > 2 {
		if outline := regexp.MustCompile(`(?m)^Outline: (.+)$`).FindStringSubmatch(output); len(outline) > 1 {
			return truncateEllipsis("read "+match[1]+"/"+match[2]+" · map: "+outline[1], 240)
		}
		return "read " + match[1] + "/" + match[2]
	}
	return "read"
}

func filepathSlash(value string) string { return strings.ReplaceAll(value, "\\", "/") }

func subtractUsage(total, before contract.Usage) contract.Usage {
	result := contract.Usage{
		PromptTokens: max(0, total.PromptTokens-before.PromptTokens), CompletionTokens: max(0, total.CompletionTokens-before.CompletionTokens),
		TotalTokens: max(0, total.TotalTokens-before.TotalTokens), CachedTokens: max(0, total.CachedTokens-before.CachedTokens),
		PromptTokensAvailable: total.PromptTokensAvailable, CompletionTokensAvailable: total.CompletionTokensAvailable,
	}
	if total.CacheReadTokens != nil {
		value := *total.CacheReadTokens
		if before.CacheReadTokens != nil {
			value -= *before.CacheReadTokens
		}
		result.CacheReadTokens = &value
	}
	if total.CacheMissTokens != nil {
		value := *total.CacheMissTokens
		if before.CacheMissTokens != nil {
			value -= *before.CacheMissTokens
		}
		result.CacheMissTokens = &value
	}
	return result
}
