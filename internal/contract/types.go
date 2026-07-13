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
	Theme          string         `json:"theme"`
	Shell          struct {
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
		Density    string `json:"density"`
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
	Role      string
	Type      string
	Content   string
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
	Role             Role       `json:"role"`
	Content          string     `json:"content"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent *string    `json:"reasoning_content,omitempty"`
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
	Content   string
	Reasoning string
	ToolCalls []ToolCall
	Usage     Usage
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
	Kind      string
	RunID     string
	Agent     string
	Title     string
	Task      string
	Model     string
	CallID    string
	Tool      string
	Arguments string
	Output    string
	Content   string
	Status    string
	Report    string
	Turns     int
	ToolCalls int
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
