package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
)

const (
	outputReserveTokens      = 12_000
	hardTurnCeiling          = 120
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
	AddEvent           func(context.Context, string, string, string, string) error
	AppendTranscript   func(context.Context, map[string]any) error
	AppendUsage        func(context.Context, contract.UsageRecord) error
	AppendInvalidation func(context.Context, contract.InvalidationEvent) error
	// WriteProjectContext (005 US3) atomically persists the typed per-session
	// project-context sidecar. Used to persist the applied instruction/memory
	// cursors alongside history growth.
	WriteProjectContext func(context.Context, contract.ProjectContextSnapshot) error
	// WritePrefixShape (Ultimate Polish C3) persists the session-stable prefix shape
	// so the next resume can attribute a skills/tools/model change vs a silent cold
	// start. Optional: a nil hook leaves resume attribution dormant.
	WritePrefixShape func(context.Context, contract.PrefixShapeSnapshot) error
	// WriteAgentRecord (feature 012 R-D3) persists one completed subagent run's
	// context record as a session sidecar (agents/<runID>.json). Optional and
	// best-effort: a nil hook keeps records in-memory for the session only.
	WriteAgentRecord func(context.Context, string, any) error
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
	InitialUsageRecords  []contract.UsageRecord
	InitialInvalidations []contract.InvalidationEvent
	BoundaryTools        BoundaryToolSource
	Rescue               RescueFunc
	Redact               func(string) string
	// InitialProjectContext restores the boot snapshot and applied hashes on resume.
	// ProjectContextProbe re-reads the current MUHIYA.md / MEMORY.md at each submit
	// boundary to decide one-shot tail updates (006). Both optional: nil leaves
	// mid-session update surfacing dormant so existing NewEngine callers are safe.
	InitialProjectContext *contract.ProjectContextSnapshot
	ProjectContextProbe   func(context.Context) (contract.ProjectContextProbe, error)
	// MemoryDir is the per-project memory store directory
	// (~/.muhiya/projects/<id>/memory) that save_memory/recall_memory read and
	// write (Experience Overhaul B1). Empty falls back to the session workspace
	// root, preserving the legacy behavior for callers that do not set it.
	MemoryDir string
	// InitialPrefixShape (Ultimate Polish C3) restores the prior session's persisted
	// prefix shape so the first request of a resumed session can attribute a
	// system/tools/model change. Nil on a fresh session.
	InitialPrefixShape *contract.PrefixShapeSnapshot
}

type Engine struct {
	settings      *contract.Settings
	secrets       contract.Secrets
	session       contract.Session
	memoryDir     string
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
	// checklist mirrors the workspace tasks.md checklist for the to-do panel;
	// it is parsed from the file whenever a tool call writes it, never authored
	// by the harness.
	checklist contract.Plan
	previous  TaskClass
	effortMu  sync.RWMutex

	// writeMu serializes off-hot-path sidecar writes (project context, prefix
	// shape, agent records) so a slow disk sync never blocks a task turn.
	writeMu sync.Mutex
	// Project-context state (006). These run on the single task goroutine (finalize
	// + brief composition), so plain reads/writes are race-free; only the sidecar
	// write in persistProjectCursor takes writeMu, matching the goal/plan-state
	// sidecar pattern. appliedMemoryHash / appliedInstructionsHash are the file
	// hashes already surfaced, so a mid-session edit emits exactly one update block.
	appliedMemoryHash       string
	appliedInstructionsHash string
	projectContext          *contract.ProjectContextSnapshot
	projectContextProbe     func(context.Context) (contract.ProjectContextProbe, error)

	taskMu               sync.Mutex
	usageEmitMu          sync.Mutex // serializes usage-ledger append + ordered live emission queue
	usageEmitting        bool
	usageEmissionQueue   []contract.Usage
	sessionUsage         contract.Usage
	usageRecords         []contract.UsageRecord
	usageAggregate       contract.SessionUsageAggregate
	requestSeq           int
	firstAfterStart      bool
	lastShape            *PrefixShape
	lastSentMessageCount int
	// Ultimate Polish Part C — cross-resume cache resilience.
	// priorSessionShape is the prefix shape the PREVIOUS session persisted
	// (prefix_shape.json), restored on resume so the first request can attribute a
	// skills/tools/model change instead of cold-starting silently (C3, fixes DC1/DC2).
	priorSessionShape     *PrefixShape
	prefixShapeSaved      bool   // C3: the current shape has been persisted this session
	lastResumeCause       string // C3→C6: why a resumed turn cold-started ("" once consumed / server-side)
	coldStartPending      bool   // C6: recordMainUsage flagged a resume cold miss to notice
	coldStartTokens       int    // C6: prompt tokens billed uncached on that miss
	resumePruneDone       bool   // C7: the stale-resume prune ran (once per engine)
	invalidations         *InvalidationLedger
	latestPromptTokens    int
	latestPromptAvailable bool
	maintenancePasses     int
	maintenanceLatched    bool
	softNoticeShown       bool // T039: soft-band advisory fires at most once per compaction cycle
	taskAgentUsage        contract.Usage
	// Session accumulators for the usage panel (feature 008 UD-6/UD-9):
	// in-memory, reset on resume, rendered with the "this session" label.
	sessionActiveMS     int64
	sessionLinesAdded   int
	sessionLinesRemoved int
	// Request-assembly component sizes (feature 008 T025) for the /context
	// usage-by-category estimate; recorded once per task at assembly.
	assemblyPromptChars  int
	assemblyToolDefChars int
	assemblyProjectChars int
	taskAgentRuns        int
	taskAgentReused      int
	taskAgentCap         int
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
	// taskReviewDecision (feature 011) is the review-gating outcome that governed
	// this task — set by whichever trigger site consulted the gate first, or by
	// the informational end-of-task evaluation; surfaced on TaskStats (SC-009).
	taskReviewDecision *ReviewDecision
	// taskTerminalReads counts run_shell invocations that merely read a file
	// where a dedicated tool sufficed (feature 011 SC-006 violation counter).
	taskTerminalReads int
	runCounter        int
	// harnessEvents is the bounded (harnessEventRingCap) in-memory ring of
	// harness-caused friction events (T010), guarded by taskMu. It backs /errors
	// and the task-summary friction marker without a DB read; the same events are
	// also persisted via AddEvent for durable telemetry.
	harnessEvents []contract.HarnessEvent
	// taskCounters holds the per-scope dispatch-gate state (repeats, failed
	// cache, storm classes, plan violations). One instance per task for the main
	// loop; subagents get a fresh instance so their gate state never pollutes the
	// parent's. (B6/T024.)
	taskCounters *callCounters
	taskFailures []int // H5: turn numbers of failed tool calls (sliding window for the distinct-failure terminator)
	// Feature 012 context linking (guarded by taskMu): completed-run records,
	// their completion order, the per-dispatch link ledger for the current
	// task, and the task ordinal that stamps record lineage.
	agentRecords     map[string]*SubagentContextRecord
	agentRecordOrder []string
	taskLinks        []contract.LinkOutcome
	taskSeq          int
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
	durationMS    *int64 // provider-request wall time (feature 008 UD-6); nil = unknown
}

type usageRecordInput struct {
	model       string
	stream      contract.UsageStream
	pin         string
	usage       contract.Usage
	reasons     []string
	attribution contract.CacheAttribution
	durationMS  *int64
}

type cacheMissContext struct {
	previous             *contract.UsageRecord
	usage                contract.Usage
	newTail              int
	previousMessageCount int
	currentMessageCount  int
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
		memoryDir:             config.MemoryDir,
		provider:              config.Provider,
		registry:              config.Registry,
		history:               config.History,
		inspection:            config.Inspection,
		knowledge:             config.Knowledge,
		callbacks:             config.Callbacks,
		persistence:           config.Persistence,
		prompt:                config.Prompt,
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
	// C3: restore the prior session's prefix shape so the first request of this
	// resumed session can attribute a system/tools/model change (fixes DC1/DC2).
	if config.InitialPrefixShape != nil {
		engine.priorSessionShape = &PrefixShape{
			SystemHash: config.InitialPrefixShape.SystemHash,
			ToolsHash:  config.InitialPrefixShape.ToolsHash,
			ModelID:    config.InitialPrefixShape.ModelID,
		}
	}
	return engine, nil
}

// resetTaskState clears every per-task counter and ledger at task start. One
// home for the whole reset so a new per-task field cannot be forgotten in the
// turn loop's prologue.
func (e *Engine) resetTaskState(budget Budget) {
	e.taskMu.Lock()
	e.taskAgentUsage = contract.Usage{}
	e.taskAgentRuns, e.taskAgentReused, e.taskDuplicates, e.taskOverBudget = 0, 0, 0, 0
	e.taskAgentDenied = 0
	e.taskReviewDecision, e.taskTerminalReads = nil, 0
	e.resetLinkTaskState()
	e.taskAgentCap = budget.MaxAgentRuns
	e.taskPeakContext = 0
	e.taskCounters = newCallCounters()
	e.taskFailures = nil // H5: reset the per-task failure window
	e.taskMu.Unlock()
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
	// C3: the main model changed, so the persisted prefix shape is now stale —
	// re-arm the one-shot persist so the next request records the new stable shape.
	if role == "main" {
		e.prefixShapeSaved = false
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

// CurrentChecklist returns the last parsed tasks.md checklist for the to-do
// panel. It is empty until a tool call writes the workspace checklist.
func (e *Engine) CurrentChecklist() contract.Plan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.checklist
}

func (e *Engine) nextRequestSeq() int {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	return e.requestSeq + 1
}

func (e *Engine) contextLimit() int {
	limit := 128000
	for _, model := range e.settings.Provider.Models {
		if model.ID == e.settings.Provider.ActiveModelID && model.ContextLimit > 0 {
			limit = model.ContextLimit
			break
		}
	}
	// Capability guard (feature 007 CP-4/FR-004): the operational context budget must
	// never exceed the provider's documented context window, or the pipeline could
	// build an over-limit request. A no-op for the default 128k; a guard against a
	// misconfigured ContextLimit.
	if ceiling := gateway.ResolveModelProfile(e.settings.Provider.ActiveModelID).ContextWindowLimit; ceiling > 0 && limit > ceiling {
		limit = ceiling
	}
	return limit
}
