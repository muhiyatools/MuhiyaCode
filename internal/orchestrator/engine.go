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
	outputReserveTokens = 12_000
	// hardTurnCeiling is the liveness backstop at MEDIUM effort; the effective
	// bound scales with the effort profile (see Engine.hardTurnCeiling). Tests
	// that assert against this constant run at medium, where the two are equal.
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
	// SkillCatalog (013 US1) is the session's discovered skills, the same slice
	// Prompt.Skills renders. Empty or nil simply means no skills are installed:
	// read_skill is then not advertised at all. LoadSkill reads one skill's body
	// (bounded); it is injected because the file layer lives in a package the
	// orchestrator must not import.
	SkillCatalog []SkillListing
	LoadSkill    SkillLoader
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
	// skills is the session-frozen skill catalog behind read_skill and
	// run_subagent's skills argument (013 US1). Built from the same listing that
	// renders the SKILLS prefix section, so name resolution can never disagree
	// with what the model was shown. Never mutated after construction.
	skills      *SkillCatalog
	skillLoader SkillLoader

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	steering []string
	// checklist mirrors the workspace tasks.md checklist for the to-do panel;
	// it is parsed from the file whenever a tool call writes it, never authored
	// by the harness.
	checklist contract.Plan
	// activeChecklistPath is the tasks.md most recently written this session
	// (TA01). Empty means "none written yet" and the workspace root is used, which
	// preserves the historical behavior for sessions that keep the checklist there.
	activeChecklistPath string
	previous            TaskClass
	// liveSettingsMu guards the settings fields a user can change WHILE a task
	// runs (Effort, PermissionMode). Those live on the *contract.Settings the
	// engine shares with the app/TUI, so a mid-task /effort or /permission would
	// otherwise write a field the task goroutine is reading. Every read and write
	// of those two fields goes through this lock.
	liveSettingsMu sync.RWMutex

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
	usageWriteMu         sync.Mutex // serializes persistence without blocking task-state readers during fsync
	usageEmitMu          sync.Mutex // preserves live usage callback order
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
	// warmPrefix maps a model id to the history revision it last saw on the main
	// stream — the warm-model ledger the task advisor prices switches against
	// (switchcost.go). In-memory by design: a resumed session cannot know what a
	// provider still holds, and treating everything as cold keeps it put.
	warmPrefix map[string]int
	// lastUpstream is the routing-layer provider the previous main request was
	// served from; pendingUpstreamNotice carries the one-line explanation to the
	// request loop when it changes. Both stay empty on a direct connection.
	lastUpstream          string
	pendingUpstreamNotice string
	// routedWindowNoted bounds the routed-window advisory to once per compaction
	// cycle, cleared by resetMaintenanceLatch alongside the soft-pressure notice.
	routedWindowNoted  bool
	maintenancePasses  int
	maintenanceLatched bool
	softNoticeShown    bool // T039: soft-band advisory fires at most once per compaction cycle
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
	// taskFilesChanged is the scope-agnostic tally of files this task changed —
	// the execution agent's writes count exactly like the main loop's would.
	taskFilesChanged map[string]bool
	// freshSessionAdvised bounds the "start a new session" advisory to once per
	// session — it is guidance, not nagging.
	freshSessionAdvised bool
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
	// taskSkillsProvided (013 FR-008) holds the lowercased names of skills whose
	// instructions are already in this task's context — seeded from the manual
	// /skills markers in the submitted prompt, then extended by each read_skill.
	// Guarded by taskMu because read_skill runs on the dispatch path.
	taskSkillsProvided map[string]bool
}

type toolOutcome struct {
	Call   contract.ToolCall
	Output string
	Failed bool
	// GateRejected is true when a PRE-DISPATCH gate refused the call — H1 argument
	// validation, the H2 verbatim-repeat short-circuit, the repeat limiter, or an
	// output-cap truncation — rather than the tool running and failing. Such a
	// refusal is recoverable guidance (fix the arguments, change approach), so it
	// must NOT feed the H5 distinct-failure terminator, which force-finalizes the
	// task with a misleading "genuine blocker" report. The gate's own escalation
	// ladder, the B7 all-failed-turn guard, the consecutive-failure nudge, and the
	// hard turn ceiling remain the backstops.
	GateRejected bool
	// Err carries the underlying dispatch error when Failed is true.
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
	// rewriteVersion is the history revision this request was built from. It is
	// what makes the warm-model ledger honest: a model is only warm for the
	// conversation shape it actually saw, so a compaction or a trim retires
	// every model's warmth instead of leaving a stale claim behind (switchcost.go).
	rewriteVersion int
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
		skills:                NewSkillCatalog(config.SkillCatalog),
		skillLoader:           config.LoadSkill,
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
		// Resume onto the upstream that last served this conversation. Without
		// this the first request of every resume is unpinned and may land on any
		// of the provider's peers, cold-starting a prefix the previous machine
		// still holds — and a resume is precisely when the conversation is at its
		// largest, so it is the most expensive request in the session to lose.
		//
		// Restoring the PIN is safe where restoring warmth would not be: the pin
		// is a preference a router may ignore, and a stale name is skipped
		// harmlessly. warmPrefix stays empty on purpose (switchcost.go) — we
		// cannot know what a provider still holds, so switches stay conservative.
		engine.lastUpstream = config.InitialPrefixShape.Upstream
	}
	// Seed the checklist from the workspace so a resumed session shows the work
	// already in flight. Best-effort: a missing tasks.md just leaves it empty.
	engine.refreshChecklist()
	return engine, nil
}

// resetTaskState clears every per-task counter and ledger at task start. One
// home for the whole reset so a new per-task field cannot be forgotten in the
// turn loop's prologue.
func (e *Engine) resetTaskState(budget Budget) {
	e.taskMu.Lock()
	e.taskDuplicates, e.taskOverBudget = 0, 0
	e.taskReviewDecision, e.taskTerminalReads = nil, 0
	e.taskFilesChanged = nil
	e.taskPeakContext = 0
	e.taskCounters = newCallCounters()
	e.taskFailures = nil // H5: reset the per-task failure window
	// 013 FR-008: the provided-skills set is per task. seedProvidedSkills fills it
	// from the submitted prompt right after this reset.
	e.taskSkillsProvided = nil
	e.taskMu.Unlock()
	// Phase VI F21: clear the soft-notice and routed-window latches so a new
	// task gets its own advisory budget for "context is filling" notices and
	// "this conversation has outgrown some of the providers" advisories.
	// Without this a previous task's shown flag would suppress the new task's
	// first advisory even if the pressure is fresh.
	e.softNoticeShown = false
	e.routedWindowNoted = false
}

func (e *Engine) IsBusy() bool {
	e.mu.Lock()
	busy := e.cancel != nil
	e.mu.Unlock()
	return busy
}

// SetEffort updates the live effort ceiling safely between model turns.
func (e *Engine) SetEffort(level contract.EffortLevel) {
	e.liveSettingsMu.Lock()
	e.settings.Effort = level
	e.liveSettingsMu.Unlock()
}

// SetPermissionMode updates the engine-visible permission mode under the same
// lock its reader uses, so a mid-task /permission does not race the task
// goroutine. The TUI calls this instead of writing the shared settings struct
// directly (F-1). The workspace guard's own live mode is set separately.
func (e *Engine) SetPermissionMode(mode contract.PermissionMode) {
	e.liveSettingsMu.Lock()
	e.settings.PermissionMode = mode
	e.liveSettingsMu.Unlock()
}

// permissionMode reads the live permission mode under liveSettingsMu.
func (e *Engine) permissionMode() contract.PermissionMode {
	e.liveSettingsMu.RLock()
	defer e.liveSettingsMu.RUnlock()
	return e.settings.PermissionMode
}

// SwitchModel applies a user-requested model change from OUTSIDE a task (the
// CLI config path). It refuses while a task runs, then delegates to the shared
// apply path.
func (e *Engine) SwitchModel(ctx context.Context, role, id, name, addendum string) error {
	e.mu.Lock()
	busy := e.cancel != nil
	e.mu.Unlock()
	if busy {
		return errors.New("cannot switch models while a task is running")
	}
	return e.applyModelSwitch(ctx, role, id, name, addendum)
}

// applyModelSwitch carries the whole cache-correctness discipline of a model
// change: refresh the model-dependent prompt fields, record the invalidation
// BEFORE the next request can transmit the new prefix, roll everything back if
// that record fails, and re-arm prefix-shape persistence.
//
// It deliberately omits the busy check. Engine.Run claims the task slot before
// its prologue runs, so a session-start advisor calling through SwitchModel
// would be refused on EVERY session and — under a silent-fallback policy —
// would never switch anything, with no visible symptom. The prologue is safe
// because it executes before the session's first Chat, which is the hazard the
// busy check exists to prevent.
// The role parameter is retained for the CLI's SwitchModel signature but there
// is only one model now; the "subagent" branch went with the subagents.
func (e *Engine) applyModelSwitch(ctx context.Context, role, id, name, addendum string) error {
	e.mu.Lock()
	oldPrompt := e.prompt
	old := e.settings.Provider.ActiveModelID
	if old == id {
		e.mu.Unlock()
		return nil
	}
	e.settings.Provider.ActiveModelID = id
	e.prompt.Model = name
	e.prompt.ModelAddendum = addendum
	e.mu.Unlock()

	event := contract.InvalidationEvent{
		Cause: contract.InvalidationModelSwitch, Trigger: contract.InvalidationUserAction,
		Scope: fmt.Sprintf("model changed from %s to %s", old, id), RequestSeq: e.nextRequestSeq(),
	}
	if err := e.recordInvalidation(ctx, event); err != nil {
		e.mu.Lock()
		e.settings.Provider.ActiveModelID = old
		e.prompt = oldPrompt
		e.mu.Unlock()
		return err
	}
	// C3: the model changed, so the persisted prefix shape is now stale — re-arm
	// the one-shot persist so the next request records the new stable shape.
	e.prefixShapeSaved = false
	return nil
}

func (e *Engine) effort() contract.EffortLevel {
	e.liveSettingsMu.RLock()
	defer e.liveSettingsMu.RUnlock()
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

// outputBudget is the explicit per-request output-token cap (TB02). Before this,
// both agent loops sent no max_tokens at all, so the family default (16k) silently
// governed and a single-file write was cut mid-JSON. The catalog's per-model
// MaxOutput — parsed from the models endpoint and previously unused anywhere —
// takes precedence over the family default; the profile clamps the result.
func (e *Engine) outputBudget(modelID string) int {
	catalog := 0
	for _, model := range e.settings.Provider.Models {
		if model.ID == modelID {
			catalog = model.MaxOutput
			break
		}
	}
	return gateway.ResolveModelProfile(modelID + " " + e.catalogModelName(modelID)).OutputBudget(catalog)
}

// RouteShellOutput streams a run_shell output chunk to the transcript's
// run_shell row. It stays a method (rather than the app calling ToolOutput
// directly) because the shell writer is wired once at workspace construction
// and the engine owns the callback surface.
func (e *Engine) RouteShellOutput(chunk string) {
	if e.callbacks.ToolOutput != nil {
		e.callbacks.ToolOutput("run_shell", chunk)
	}
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
