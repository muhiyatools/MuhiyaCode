// Package contract contains the stable values and ports shared by MuhiyaCode's
// independent runtime layers. It deliberately has no infrastructure imports.
package contract

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type PermissionMode string

const (
	PermissionNormal     PermissionMode = "normal"
	PermissionAutoAccept PermissionMode = "auto-accept"
)

type EffortLevel string

const (
	EffortLow    EffortLevel = "low"
	EffortMedium EffortLevel = "medium"
	EffortHigh   EffortLevel = "high"
	EffortMax    EffortLevel = "max"
)

type ReasoningTier string

const (
	ReasoningLow    ReasoningTier = "low"
	ReasoningMedium ReasoningTier = "medium"
	ReasoningHigh   ReasoningTier = "high"
	ReasoningMax    ReasoningTier = "max"
)

type Model struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextLimit int    `json:"contextLimit"`
	MaxOutput    int    `json:"maxOutputTokens,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Source       string `json:"source,omitempty"`
	Description  string `json:"description,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`
}

type Settings struct {
	Version  int `json:"version"`
	Provider struct {
		Type            string  `json:"type"`
		BaseURL         string  `json:"baseUrl"`
		ActiveModelID   string  `json:"activeModelId"`
		SubagentModelID string  `json:"subagentModelId"`
		Models          []Model `json:"models"`
	} `json:"provider"`
	PermissionMode PermissionMode `json:"permissionMode"`
	Effort         EffortLevel    `json:"effort"`
	// ReviewGating controls the automatic review triggers (feature 011):
	// "off" | "conservative" | "default" (empty = default). Explicit review
	// requests always run regardless of this setting.
	ReviewGating string `json:"reviewGating,omitempty"`
	Theme        string `json:"theme"`
	Shell        struct {
		Preferred   string `json:"preferred"`
		TimeoutMS   int    `json:"timeoutMs"`
		OutputLimit int    `json:"outputLimit"`
	} `json:"shell"`
	RTL struct {
		Mode  string `json:"mode"`
		Align string `json:"align"`
	} `json:"rtl"`
	UI struct {
		BorderMode string `json:"borderMode"`
	} `json:"ui"`
}

type Secrets struct {
	ProviderAPIKey string `json:"providerApiKey"`
}

type Session struct {
	ID            string    `json:"id"`
	WorkspacePath string    `json:"workspacePath"`
	Title         string    `json:"title"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Event struct {
	Role    string
	Type    string
	Content string
	// Target is the tool call's display target (file/command/query) for tool
	// events, persisted so a resumed transcript row names WHAT the call acted on
	// exactly as it did live. Empty for non-tool events and for tool events from
	// sessions predating the events.target column.
	Target    string
	CreatedAt time.Time
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role             Role            `json:"role"`
	Content          string          `json:"content"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall      `json:"tool_calls,omitempty"`
	ReasoningContent *string         `json:"reasoning_content,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoning_details,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Type      string          `json:"type,omitempty"`
	Function  *ToolFunction   `json:"function,omitempty"`
	Name      string          `json:"-"`
	Arguments json.RawMessage `json:"-"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (c ToolCall) ToolName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.Function != nil {
		return c.Function.Name
	}
	return ""
}

func (c ToolCall) ArgumentsJSON() string {
	if len(c.Arguments) != 0 {
		return string(c.Arguments)
	}
	if c.Function != nil {
		return c.Function.Arguments
	}
	return "{}"
}

func NewToolCall(id, name, arguments string) ToolCall {
	return ToolCall{ID: id, Type: "function", Function: &ToolFunction{Name: name, Arguments: arguments}, Name: name, Arguments: json.RawMessage(arguments)}
}

type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Usage struct {
	PromptTokens              int    `json:"promptTokens,omitempty"`
	CompletionTokens          int    `json:"completionTokens,omitempty"`
	TotalTokens               int    `json:"totalTokens,omitempty"`
	CachedTokens              int    `json:"cachedTokens,omitempty"`
	CacheReadTokens           *int   `json:"cacheReadTokens,omitempty"`
	CacheMissTokens           *int   `json:"cacheMissTokens,omitempty"`
	MissDerived               bool   `json:"missDerived,omitempty"`
	Contradictory             bool   `json:"contradictory,omitempty"`
	Diagnostic                string `json:"diagnostic,omitempty"`
	PromptTokensAvailable     bool   `json:"-"`
	CompletionTokensAvailable bool   `json:"-"`
	// CostUSD is the gateway-reported cost for one request, parsed from the
	// muhiya_log SSE chunk (USD). nil = not reported (generic gateway, older
	// gateway, or a non-streaming path) — credits displays are then omitted
	// rather than estimated. Deliberately NOT folded by Add/subtractUsage:
	// credits are computed by scanning the task's UsageRecords (see
	// TaskCreditsUSD), not from the token delta.
	CostUSD *float64 `json:"costUsd,omitempty"`
	// CostEstimated mirrors the gateway's usage_estimated flag: true when the
	// upstream disconnected and tokens/cost came from the gateway's local
	// heuristic. Surfaced as a "~" marker, never presented as measured.
	CostEstimated bool `json:"costEstimated,omitempty"`
	// CostLogID carries muhiya_log.log_id (the gateway request_logs.id) so the
	// benchmark credits cross-check can match TUI credits against the ledger.
	CostLogID string `json:"-"`
}

func (u Usage) Add(next Usage) Usage {
	total := next.TotalTokens
	if total == 0 {
		total = next.PromptTokens + next.CompletionTokens
	}
	return Usage{
		PromptTokens:              u.PromptTokens + next.PromptTokens,
		CompletionTokens:          u.CompletionTokens + next.CompletionTokens,
		TotalTokens:               u.TotalTokens + total,
		CachedTokens:              u.CachedTokens + next.CachedTokens,
		CacheReadTokens:           addNullableInt(u.CacheReadTokens, next.CacheReadTokens),
		CacheMissTokens:           addNullableInt(u.CacheMissTokens, next.CacheMissTokens),
		MissDerived:               u.MissDerived || next.MissDerived,
		Contradictory:             u.Contradictory || next.Contradictory,
		Diagnostic:                joinDiagnostic(u.Diagnostic, next.Diagnostic),
		PromptTokensAvailable:     u.PromptTokensAvailable || next.PromptTokensAvailable || u.PromptTokens != 0 || next.PromptTokens != 0,
		CompletionTokensAvailable: u.CompletionTokensAvailable || next.CompletionTokensAvailable || u.CompletionTokens != 0 || next.CompletionTokens != 0,
	}
}

func addNullableInt(left, right *int) *int {
	if left == nil && right == nil {
		return nil
	}
	total := 0
	if left != nil {
		total += *left
	}
	if right != nil {
		total += *right
	}
	return &total
}

func joinDiagnostic(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" || left == right {
		return left
	}
	return left + "; " + right
}

type ChatRequest struct {
	Messages    []Message
	Tools       []ToolDefinition
	ModelID     string
	Temperature *float64
	MaxTokens   int
	ToolChoice  string
	Reasoning   ReasoningTier
	// SessionID is a routing pin derived once per session per stream and sent
	// as the X-Muhiya-Session header. Long-lived providers key their cache by
	// routing identity; without it, an upstream model flip silently invalidates
	// the entire cached prefix. Must not be serialized into the JSON body.
	SessionID        string
	OnToken          func(string)
	OnReasoningToken func(string)
}

type ChatResponse struct {
	Content          string
	Reasoning        string
	ReasoningDetails json.RawMessage
	ToolCalls        []ToolCall
	Usage            Usage
}

type Provider interface {
	Chat(context.Context, ChatRequest) (ChatResponse, error)
	ListModels(context.Context) ([]Model, error)
}

type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, json.RawMessage) (string, error)
}

type PlanStatus string

const (
	PlanPending    PlanStatus = "pending"
	PlanInProgress PlanStatus = "in_progress"
	PlanCompleted  PlanStatus = "completed"
)

type PlanStep struct {
	Title  string     `json:"title"`
	Status PlanStatus `json:"status"`
}

type Plan struct {
	Steps     []PlanStep `json:"steps"`
	Note      string     `json:"note,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// PlanPhase (004 US2) is the whole-plan lifecycle state, distinct from the
// per-step PlanStatus. It is the single source of truth for every plan
// affordance the UI shows: an executable hint appears ONLY in PlanPhasePending
// (and, as a resumable-partial variant, PlanPhaseInterrupted). Terminal phases
// (finished/superseded/discarded) never advertise executability again, even
// across session resume. See specs/004-deepseek-agent-polish/contracts/plan-lifecycle.md.
type PlanPhase string

const (
	PlanPhaseNone        PlanPhase = ""            // no plan for the session
	PlanPhaseDrafting    PlanPhase = "drafting"    // plan mode on; plan being written/refined
	PlanPhaseReady       PlanPhase = "ready"       // plan-ready signaled; awaiting the proceed/save/keep decision
	PlanPhasePending     PlanPhase = "pending"     // saved for later; the ONLY phase that invites execution
	PlanPhaseExecuting   PlanPhase = "executing"   // a run is actively working the plan's steps
	PlanPhaseFinished    PlanPhase = "finished"    // all steps completed; terminal
	PlanPhaseInterrupted PlanPhase = "interrupted" // execution ended with incomplete steps; resumable
	PlanPhaseSuperseded  PlanPhase = "superseded"  // replaced by a newer plan; terminal
	PlanPhaseDiscarded   PlanPhase = "discarded"   // explicitly cleared by the user; terminal
)

// IsTerminal reports whether the phase is an end state a plan never leaves
// except by starting a fresh lifecycle (a new plan re-enters drafting).
func (p PlanPhase) IsTerminal() bool {
	return p == PlanPhaseFinished || p == PlanPhaseSuperseded || p == PlanPhaseDiscarded
}

// PipelinePhase is the harness-enforced orchestration phase for one task. It
// intentionally stays distinct from PlanPhase: the pipeline drives the
// existing plan lifecycle while also sequencing research and validation.
// The empty value is legacy-compatible and is interpreted as direct work.
type PipelinePhase string

const (
	PipelinePhaseDirect    PipelinePhase = "direct"
	PipelinePhaseResearch  PipelinePhase = "research"
	PipelinePhasePlan      PipelinePhase = "plan"
	PipelinePhaseApprove   PipelinePhase = "approve"
	PipelinePhaseImplement PipelinePhase = "implement"
	PipelinePhaseValidate  PipelinePhase = "validate"
	PipelinePhaseDone      PipelinePhase = "done"
)

// LifecycleState is the feature-010 unified task lifecycle: the SINGLE source
// of truth that replaces the legacy plan-mode flags (planMode/pendingPlan) and
// the two phase enums (PlanPhase drives affordances, PipelinePhase drives
// orchestration). The 11 states are the disjoint union of both machines; every
// consumer reads them through the pure predicates below rather than comparing
// raw states, so the two-truth desync class becomes unrepresentable.
// PlanPhase/PipelinePhase remain ONLY as legacy sidecar-migration inputs.
type LifecycleState string

const (
	LifecycleDirect       LifecycleState = ""                  // no plan; direct work (also the zero value / legacy-absent)
	LifecycleResearch     LifecycleState = "research"          // pipeline research; read-only
	LifecyclePlanning     LifecycleState = "planning"          // plan being written; read-only
	LifecycleApproval     LifecycleState = "awaiting-approval" // plan written; awaiting the user's go-ahead; read-only
	LifecyclePending      LifecycleState = "pending"           // saved for later; a bare "proceed" executes it
	LifecycleImplementing LifecycleState = "implementing"      // executing the approved plan's steps
	LifecycleValidating   LifecycleState = "validating"        // reviewing changes before completion
	LifecycleInterrupted  LifecycleState = "interrupted"       // execution ended with open steps; resumable
	LifecycleFinished     LifecycleState = "finished"          // all steps done; terminal
	LifecycleSuperseded   LifecycleState = "superseded"        // replaced by a newer plan; terminal
	LifecycleDiscarded    LifecycleState = "discarded"         // explicitly cleared by the user; terminal
)

// IsReadOnly reports whether the state blocks every mutating tool — the
// research/planning/approval investigation window. It is identical to
// BlocksMutation by design: the feature-009 audit proved the two separate
// mutation gates guarded the same set, so they collapse into one predicate.
func (s LifecycleState) IsReadOnly() bool {
	return s == LifecycleResearch || s == LifecyclePlanning || s == LifecycleApproval
}

// BlocksMutation is the gate predicate; identical set to IsReadOnly.
func (s LifecycleState) BlocksMutation() bool { return s.IsReadOnly() }

// InvitesProceed reports whether a bare "proceed" should execute a saved plan.
func (s LifecycleState) InvitesProceed() bool {
	return s == LifecyclePending || s == LifecycleInterrupted
}

// IsApprovalPause reports the one state where the approval flow gate is shown.
func (s LifecycleState) IsApprovalPause() bool { return s == LifecycleApproval }

// IsPipelineResumable reports whether a restart resumes an in-flight pipeline
// phase (as opposed to a proceed-hint state, which resumes via InvitesProceed).
func (s LifecycleState) IsPipelineResumable() bool {
	return s == LifecycleResearch || s == LifecyclePlanning ||
		s == LifecycleImplementing || s == LifecycleValidating
}

// IsTerminal reports an end state a plan never leaves except by starting a
// fresh lifecycle (a new plan re-enters planning/research).
func (s LifecycleState) IsTerminal() bool {
	return s == LifecycleFinished || s == LifecycleSuperseded || s == LifecycleDiscarded
}

// IsActive reports whether the task is inside the orchestration pipeline (any
// non-direct state) — replaces the old pipelineActive() flag check.
func (s LifecycleState) IsActive() bool { return s != LifecycleDirect }

// GoalSnapshot (G4) is the persisted shape of a goal stored in the per-session
// goal.json sidecar next to the session files. Only goals with Status "active"
// are restored on resume; completed/blocked goals are not persisted (the G2
// tombstone is in-memory only, so a finished objective does not resurrect).
// AutoTurns is restored for fidelity but resets at the next task boundary
// (ResetGoalTaskCounter, G5), so a resumed goal gets a fresh per-task budget.
type GoalSnapshot struct {
	Text      string `json:"text"`
	Status    string `json:"status"`
	AutoTurns int    `json:"autoTurns"`
	Blocked   string `json:"blocked,omitempty"`
}

// PlanStateSnapshot (P2) persists the plan-mode and pending-plan flags in the
// per-session plan_state.json sidecar so they survive a restart. planMode
// restores read-only planning; pendingPlan makes a bare "proceed" execute the
// saved plan (P2 step 4). The plan CONTENT already persists via plan.md/tasks.md
// (WritePlan); this sidecar carries only the two flags the flow needs.
type PlanStateSnapshot struct {
	PlanMode    bool `json:"planMode"`
	PendingPlan bool `json:"pendingPlan"`
	// Phase (004 US2) is the whole-plan lifecycle state. Additive and
	// omitempty: a legacy sidecar without it is derived from the two booleans
	// on load (pendingPlan→pending, planMode→drafting, else none). PlanMode and
	// PendingPlan stay authoritative for their existing consumers and remain
	// consistent with Phase.
	Phase PlanPhase `json:"phase,omitempty"`
	// PipelinePhase is an additive feature-009 sidecar field. Legacy snapshots
	// omit it and therefore resume through the existing direct/plan lifecycle.
	PipelinePhase PipelinePhase `json:"pipeline_phase,omitempty"`
	// PipelineDepth persists alongside PipelinePhase so a light pipeline
	// resumes light. Additive: legacy snapshots omit it and restore as full
	// (the pre-existing behavior).
	PipelineDepth string `json:"pipeline_depth,omitempty"`
	// State (feature 010) is the unified lifecycle — the canonical field going
	// forward. When present it is authoritative and the migration loader
	// ignores the legacy fields above; when absent (older sidecars) the loader
	// derives it from PipelinePhase/Phase/PlanMode/PendingPlan. Depth rides
	// alongside via the existing PipelineDepth field.
	State LifecycleState `json:"state,omitempty"`
}

type QuestionChoice struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

type Question struct {
	Question string           `json:"question"`
	Choices  []QuestionChoice `json:"choices"`
}

type Answer struct {
	Question string
	Choice   QuestionChoice
	Index    int
}

// ErrPlanModeExited (P2) is returned by the orchestrator's exit_plan_mode
// dispatch to signal that the current task should finalize while leaving
// plan-mode state to the TUI/handler that picks up after the task ends.
var ErrPlanModeExited = errors.New("plan ready: awaiting user choice")

type AgentEvent struct {
	Kind  string
	RunID string
	Agent string
	Phase string
	Role  string
	Title string
	Task  string
	// Handoff carries the rendered launch contract for audit consumers (the
	// delegation benchmark's SC-004 handoff audit); the TUI does not render it.
	Handoff   string
	Model     string
	Tool      string
	Arguments string
	Output    string
	Content   string
	Status    string
	Report    string
	Usage     Usage
}

type ContextInfo struct {
	HistoryTokens     int
	LastRequestTokens int
	ContextLimit      int
	Percent           float64
}

// PrunedRecord (T041) archives a tool result BEFORE reclamation shortens it, so
// the original is recoverable for debugging. Appended to the session's
// pruned.jsonl before any fold/trim mutates history.
type PrunedRecord struct {
	ToolCallID      string `json:"toolCallId"`
	ToolName        string `json:"toolName"`
	Reason          string `json:"reason"` // "fold" | "trim"
	OriginalBytes   int    `json:"originalBytes"`
	ReducedToBytes  int    `json:"reducedToBytes"`
	OriginalContent string `json:"originalContent"`
}

type TaskStats struct {
	DurationMS int64       `json:"durationMs"`
	Effort     EffortLevel `json:"effort"`
	TaskClass  string      `json:"taskClass"`
	Usage      Usage       `json:"usage"`
	AgentUsage Usage       `json:"agentUsage"`
	// SessionHitRate (T043) is the cumulative session cache-hit rate from the
	// usage aggregate (provider-fields-only denominator), surfaced in the
	// persistent usage footer alongside the per-task cache tag.
	SessionHitRate     *float64            `json:"sessionHitRate,omitempty"`
	PeakContextPercent float64             `json:"peakContextPercent"`
	ToolCalls          int                 `json:"toolCalls"`
	AgentRuns          int                 `json:"agentRuns"`
	AgentRunsReused    int                 `json:"agentRunsReused"`
	FilesChanged       []string            `json:"filesChanged"`
	Turns              int                 `json:"turns"`
	ChecksRun          int                 `json:"checksRun"`
	FoldedTokens       int                 `json:"foldedTokens"`
	DisciplineScore    int                 `json:"disciplineScore"`
	DoneCriteria       string              `json:"doneCriteria,omitempty"`
	Invalidations      []InvalidationEvent `json:"invalidations,omitempty"`
	// PlanReady (P2) is set when a plan-mode task ended via exit_plan_mode or a
	// free-text plan finish. The TUI opens the Proceed now / Proceed later / Keep
	// planning modal when it sees this on the task-complete stats.
	PlanReady bool `json:"planReady,omitempty"`
	// TerminatedReason (H5) is set when a task was force-finalized by the token
	// circuit breaker or the distinct-failure terminator. The TUI surfaces it as
	// a warn notice so the user knows why the task stopped early.
	TerminatedReason string `json:"terminatedReason,omitempty"`
	// ReviewTier/ReviewRationale (feature 011 SC-009) record the review-gating
	// decision for this task — including "skip" — so the completion summary and
	// the benchmark records always explain what the reviewer did and why.
	ReviewTier      string `json:"reviewTier,omitempty"`
	ReviewRationale string `json:"reviewRationale,omitempty"`
	// ReviewCeilingHit/ReviewCoverage (T022/D6): whether the review's token
	// ceiling bounded the run, and the reviewer's parsed coverage line.
	ReviewCeilingHit bool   `json:"reviewCeilingHit,omitempty"`
	ReviewCoverage   string `json:"reviewCoverage,omitempty"`
	// Violation counters (feature 011 SC-006, measured per task): shell commands
	// that read files where a dedicated tool sufficed, and duplicate reads the
	// inspection ledger blocked.
	TerminalReadViolations  int `json:"terminalReadViolations,omitempty"`
	DuplicateReadViolations int `json:"duplicateReadViolations,omitempty"`
	// PerPairing (feature 011 D8) is the session's per-(model, pin) cache health
	// — the mixed-model measurement SC-005 reads.
	PerPairing []PairingRate `json:"perPairing,omitempty"`
	// StopCause (003) marks a task that ended early from an interruption rather
	// than a natural finish: user stop (Esc/cancel), a provider/stream error, or
	// a connection loss. Empty means the task completed normally. It drives the
	// dimmed "interrupted" marker on the task summary and is independent of the
	// H5-only TerminatedReason (which keeps its own "Task terminated" notice).
	StopCause string `json:"stopCause,omitempty"`
	// CreditsUSD (003) is the task's cost, summed from the CostUSD of the usage
	// records inside the task's record range under the member-set rules (empty-
	// usage records excluded; any remaining nil ⇒ nil, i.e. credits unavailable).
	// nil ⇒ the summary omits credits. Display converts via USDToCredits
	// (2,500 credits per $25 of budget).
	CreditsUSD *float64 `json:"creditsUsd,omitempty"`
	// CreditsEstimated is true when any priced member of the task's record range
	// was cost-estimated; the summary prefixes credits with "~".
	CreditsEstimated bool `json:"creditsEstimated,omitempty"`
	// LinesAdded/LinesRemoved sum the diff adds/removes of applied (successful)
	// file-changing calls in this task, counted by the shared contract.DiffCounts
	// parser (feature 008 UD-6) — the same counts the TUI shows per tool row.
	LinesAdded   int `json:"linesAdded,omitempty"`
	LinesRemoved int `json:"linesRemoved,omitempty"`
	// HarnessEvents (Stability Overhaul T013) is the count of user-visible harness
	// friction (gate/tool/ui classes) recorded during THIS task. >0 appends a
	// dimmed "⚠ N harness" marker to the task summary; 0 adds no noise.
	HarnessEvents int `json:"harnessEvents,omitempty"`
}

// StopCause values for TaskStats.StopCause (003, FR-013a).
const (
	StopCauseUserStop   = "user stop"
	StopCauseError      = "error"
	StopCauseDisconnect = "disconnect"
)

type Callbacks struct {
	Status func(string)
	// Notice carries a transient user-facing announcement (e.g. "compaction
	// freed 40k tokens") that should outlive the next status update; the TUI
	// shows it as a flash notice instead of the one-line status.
	Notice         func(string)
	Token          func(string)
	ReasoningToken func(string)
	ToolStart      func(name string, input json.RawMessage)
	ToolOutput     func(name, chunk string)
	ToolEnd        func(name, output string)
	PlanUpdate     func(Plan)
	Usage          func(Usage)
	Context        func(ContextInfo)
	Agent          func(AgentEvent)
	MCPStatus      func(string)
	TaskComplete   func(TaskStats)
	Confirm        func(context.Context, string) (bool, error)
	Ask            func(context.Context, []Question) ([]Answer, error)
}

func (c Callbacks) EmitStatus(value string) {
	if c.Status != nil {
		c.Status(value)
	}
}

func (c Callbacks) EmitNotice(value string) {
	if c.Notice != nil {
		c.Notice(value)
	}
}
