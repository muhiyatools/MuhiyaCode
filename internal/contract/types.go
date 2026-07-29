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

// BenchmarkStatus describes the adapter lifecycle, not the external task
// grader's verdict. A benchmark harness remains responsible for judging the
// repository state after the agent exits.
type BenchmarkStatus string

const (
	BenchmarkPass    BenchmarkStatus = "pass"
	BenchmarkFail    BenchmarkStatus = "fail"
	BenchmarkTimeout BenchmarkStatus = "timeout"
	BenchmarkBlocked BenchmarkStatus = "blocked"
	BenchmarkError   BenchmarkStatus = "error"
)

// VerificationResult captures the agent's generic project-marker verification
// stage. It is intentionally separate from BenchmarkStatus because an external
// benchmark grader remains the objective source of task success.
type VerificationResult struct {
	Ran             bool   `json:"ran"`
	Command         string `json:"command,omitempty"`
	Result          string `json:"result"`
	FailureEffect   string `json:"failure_effect,omitempty"`
	OutputTruncated string `json:"output_truncated,omitempty"`
}

type ReviewFinding struct {
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

type ReviewCoverage struct {
	Reviewed []string `json:"reviewed,omitempty"`
	Skipped  []string `json:"skipped,omitempty"`
}

// ReviewResult is the bounded, machine-readable output of the isolated
// completion reviewer. Unlike ReviewTier, it records what the reviewer found.
type ReviewResult struct {
	Ran       bool            `json:"ran"`
	Verdict   string          `json:"verdict,omitempty"`
	Model     string          `json:"model,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Findings  []ReviewFinding `json:"findings,omitempty"`
	Coverage  ReviewCoverage  `json:"coverage,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"createdAt,omitempty"`
}

type ReasoningTier string

const (
	ReasoningLow    ReasoningTier = "low"
	ReasoningMedium ReasoningTier = "medium"
	ReasoningHigh   ReasoningTier = "high"
	ReasoningMax    ReasoningTier = "max"
)

type Model struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	RecordID                 string            `json:"recordId,omitempty"`
	TargetModel              string            `json:"targetModel,omitempty"`
	Tags                     []string          `json:"tags,omitempty"`
	ProviderFamily           string            `json:"providerFamily,omitempty"`
	AdapterVersion           string            `json:"adapterVersion,omitempty"`
	CompatibilityEpoch       int               `json:"compatibilityEpoch,omitempty"`
	CacheContract            json.RawMessage   `json:"cacheContract,omitempty"`
	SupportedParameters      []string          `json:"supportedParameters,omitempty"`
	PricingRuleSetID         string            `json:"pricingRuleSetId,omitempty"`
	InputNanoPerMillion      int64             `json:"inputNanoUsdPerMillion,omitempty"`
	OutputNanoPerMillion     int64             `json:"outputNanoUsdPerMillion,omitempty"`
	CacheReadNanoPerMillion  int64             `json:"cacheReadNanoUsdPerMillion,omitempty"`
	CacheWriteNanoPerMillion int64             `json:"cacheWriteNanoUsdPerMillion,omitempty"`
	PricingTiers             []PricingTier     `json:"pricingTiers,omitempty"`
	Capabilities             ModelCapabilities `json:"capabilities,omitempty"`
	ContextLimit             int               `json:"contextLimit"`
	MaxOutput                int               `json:"maxOutputTokens,omitempty"`
	// CodingTier is an operator/provider supplied capability rank from 1 to 5.
	// Zero means unknown and never justifies an automatic capability switch.
	CodingTier           int     `json:"codingTier,omitempty"`
	InputCostPerMillion  float64 `json:"inputCostPerMillion,omitempty"`
	OutputCostPerMillion float64 `json:"outputCostPerMillion,omitempty"`
	Health               string  `json:"health,omitempty"`
	Provider             string  `json:"provider,omitempty"`
	Source               string  `json:"source,omitempty"`
	Description          string  `json:"description,omitempty"`
	CreatedAt            string  `json:"createdAt,omitempty"`
}

type PricingRates struct {
	InputNanoPerMillion      int64 `json:"input_nano_usd_per_million"`
	OutputNanoPerMillion     int64 `json:"output_nano_usd_per_million"`
	CacheReadNanoPerMillion  int64 `json:"cache_read_nano_usd_per_million"`
	CacheWriteNanoPerMillion int64 `json:"cache_write_nano_usd_per_million"`
}

type PricingTier struct {
	MinInputTokensExclusive int64        `json:"min_input_tokens_exclusive"`
	Rates                   PricingRates `json:"rates"`
}

type ModelCapabilities struct {
	Vision            bool     `json:"vision,omitempty"`
	Thinking          bool     `json:"thinking,omitempty"`
	Audio             bool     `json:"audio,omitempty"`
	Video             bool     `json:"video,omitempty"`
	Documents         bool     `json:"documents,omitempty"`
	MaxAttachmentMB   int      `json:"max_attachment_mb,omitempty"`
	AcceptedMIMETypes []string `json:"accepted_mime_types,omitempty"`
	InputModalities   []string `json:"input_modalities,omitempty"`
}

type Settings struct {
	Version  int `json:"version"`
	Provider struct {
		Type    string `json:"type"`
		BaseURL string `json:"baseUrl"`
		// ActiveModelID is the default for new sessions. Existing sessions persist
		// their own explicit model and change only through `/model`.
		ActiveModelID string  `json:"activeModelId"`
		Models        []Model `json:"models"`
		// ModelsRefreshedAt is when the gateway catalog was last discovered
		// (RFC3339). It drives the TTL refresh that keeps the model list current
		// now that the interactive refresh command is gone; empty means never.
		ModelsRefreshedAt string `json:"modelsRefreshedAt,omitempty"`
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
	ArchivedAt    time.Time `json:"archivedAt,omitempty"`
	ParentID      string    `json:"parentId,omitempty"`
	// ModelID and CacheEpoch are loaded from the session runtime sidecar. They
	// are not global defaults and therefore survive resume and model switches.
	ModelID       string                  `json:"modelId,omitempty"`
	CacheEpoch    uint64                  `json:"cacheEpoch,omitempty"`
	ModelLineages map[string]ModelLineage `json:"modelLineages,omitempty"`
	// PermissionMode is the durable session-scoped permission authority (UMI-06).
	// Global settings supply only the default for a new session; an existing
	// session restores its saved mode on resume. Empty means Normal (safe default).
	PermissionMode PermissionMode `json:"permissionMode,omitempty"`
}

type ModelLineage struct {
	CompatibilityEpoch int       `json:"compatibilityEpoch,omitempty"`
	CacheEpoch         uint64    `json:"cacheEpoch"`
	PrefixHash         string    `json:"prefixHash,omitempty"`
	RouteAffinity      string    `json:"routeAffinity,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

type CheckpointInfo struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"sessionId"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	EventCursor int64     `json:"eventCursor"`
}

type BackgroundProcess struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

type Event struct {
	Role    string `json:"role"`
	Type    string `json:"type"`
	Content string `json:"content"`
	// Target is the tool call's display target (file/command/query) for tool
	// events, persisted so a resumed transcript row names WHAT the call acted on
	// exactly as it did live. Empty for non-tool events and for tool events from
	// sessions predating the events.target column.
	Target    string    `json:"target,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
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
	ReasoningTokens           *int   `json:"reasoningTokens,omitempty"`
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
	// Upstream is the provider a routing layer (OpenRouter) actually served the
	// request from; empty on a direct connection. Prefix caches are per-upstream,
	// so a flip between turns cold-starts a cache that every byte we send says
	// should still be warm. Recorded per request so that becomes visible instead
	// of looking like unexplained drift. Not folded by Add: it identifies one
	// request, it does not accumulate.
	Upstream string `json:"-"`
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
		ReasoningTokens:           addNullableInt(u.ReasoningTokens, next.ReasoningTokens),
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
	Seed        *int
	Purpose     RequestPurpose
	CacheEpoch  uint64
	// SessionID is a routing pin derived once per session per stream and sent
	// as the X-Muhiya-Session header. Long-lived providers key their cache by
	// routing identity; without it, an upstream model flip silently invalidates
	// the entire cached prefix. Must not be serialized into the JSON body.
	SessionID string
	// RequestID is the per-request correlation ID (P0-W5, UMI-26). It is
	// generated by the client once per logical turn and sent as the
	// X-Muhiya-Request-ID header. The gateway groups independently logged retry
	// attempts beneath this ID; replaying one attempt is idempotent.
	// Empty means the gateway generates its own logical ID.
	RequestID        string
	OnToken          func(string)
	OnReasoningToken func(string)
	// OnStreamReset retracts visible deltas from a failed stream attempt before
	// the provider retries the identical request. Without it, the successful
	// retry is appended after the abandoned partial text and duplicates output.
	OnStreamReset func()
}

type RequestPurpose string

const (
	RequestPurposeMain RequestPurpose = "main"
)

type ChatResponse struct {
	Content          string
	Reasoning        string
	ReasoningDetails json.RawMessage
	ToolCalls        []ToolCall
	Usage            Usage
	// FinishReason is the provider's stop reason ("stop", "tool_calls",
	// "length", …). "length" means the answer was cut at the output cap and is
	// incomplete — the caller surfaces that instead of treating it as done (B-3).
	FinishReason string
	// TruncatedCalls names the IDs of tool calls whose arguments were cut off at
	// the output cap. Such a call must NOT be dispatched: its JSON is incomplete,
	// so it would fail validation, burn a turn, and (before TB03) re-bill its
	// half-written payload on every later request.
	TruncatedCalls []string
}

type Provider interface {
	Chat(context.Context, ChatRequest) (ChatResponse, error)
	ListModels(context.Context) ([]Model, error)
}

type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, json.RawMessage) ToolResult
}

type ToolExecutionState string

const (
	ToolExecutionCompleted     ToolExecutionState = "completed"
	ToolExecutionNotStarted    ToolExecutionState = "not_started"
	ToolExecutionIndeterminate ToolExecutionState = "indeterminate"
)

type ToolResult struct {
	Status ToolOutcomeStatus
	State  ToolExecutionState
	Output string
	Err    error
}

func AdaptToolResult(output string, err error) ToolResult {
	if err == nil {
		return ToolResult{Status: ToolOutcomeSucceeded, State: ToolExecutionCompleted, Output: output}
	}
	if errors.Is(err, ErrToolNotStarted) {
		return ToolResult{Status: ToolOutcomeRejected, State: ToolExecutionNotStarted, Output: output, Err: err}
	}
	status := ToolOutcomeFailed
	if errors.Is(err, context.Canceled) {
		status = ToolOutcomeCancelled
	}
	return ToolResult{Status: status, State: ToolExecutionIndeterminate, Output: output, Err: err}
}

// ReadOnlyDeclaring is the OPTIONAL half of Tool: a tool that can say it does
// not change anything. Only MCP tools implement it today, from their server's
// readOnlyHint annotation — the built-in workspace tools are classified
// structurally instead (rolesplit.go), which is stronger than a declaration.
//
// The plan/execute role gate is the consumer: it refuses main-loop mutations,
// and an MCP doc-search or log-reader is not a mutation. A server that lies
// here gains nothing dangerous — the tool runs either way, via the executor;
// the only difference is which model invokes it. Absent or false means
// "treat as mutating", so an unannotated server stays fail-closed.
type ReadOnlyDeclaring interface {
	DeclaresReadOnly() bool
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

// AgentEvent was the subagent lifecycle event (start / tool_start / tool_end /
// text / usage / done) a delegated run emitted so the TUI could render its card.
// It was removed with the subagent system: one session emits ordinary tool
// events, and there are no agent cards left to feed.

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
	Status     TaskStatus  `json:"status"`
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
	ReviewCeilingHit bool         `json:"reviewCeilingHit,omitempty"`
	ReviewCoverage   string       `json:"reviewCoverage,omitempty"`
	ReviewResult     ReviewResult `json:"reviewResult,omitempty"`
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
	// Verification records the outcome of the bounded verification stage.
	Verification VerificationResult `json:"verification,omitempty"`
}

type TaskStatus string

const (
	TaskStatusSucceeded     TaskStatus = "succeeded"
	TaskStatusIncomplete    TaskStatus = "incomplete"
	TaskStatusIndeterminate TaskStatus = "indeterminate"
	TaskStatusFailed        TaskStatus = "failed"
	TaskStatusCancelled     TaskStatus = "cancelled"
)

// StopCause values for TaskStats.StopCause (003, FR-013a).
const (
	StopCauseUserStop   = "user stop"
	StopCauseError      = "error"
	StopCauseDisconnect = "disconnect"
	StopCauseTimeout    = "timeout"
	StopCauseBudget     = "budget"
)

type Callbacks struct {
	Status func(string)
	// Notice carries a transient user-facing announcement (e.g. "compaction
	// freed 40k tokens") that should outlive the next status update; the TUI
	// shows it as a flash notice instead of the one-line status.
	Notice         func(string)
	Token          func(string)
	ReasoningToken func(string)
	StreamReset    func()
	ToolStart      func(name string, input json.RawMessage)
	ToolOutput     func(name, chunk string)
	ToolEnd        func(name, output string)
	PlanUpdate     func(Plan)
	Usage          func(Usage)
	Context        func(ContextInfo)
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
