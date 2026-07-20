// Package contract contains the stable values and ports shared by MuhiyaCode's
// independent runtime layers. It deliberately has no infrastructure imports.
package contract

import (
	"context"
	"encoding/json"
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
	// ContextLinking (feature 012 FR-017) controls subagent context reuse:
	// "off" | "default" (empty = default). "off" restores pre-012 dispatch
	// behavior exactly — every subagent starts fresh and the phase read gate
	// is disabled.
	ContextLinking string `json:"contextLinking,omitempty"`
	Theme          string `json:"theme"`
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

type AgentEvent struct {
	Kind  string
	RunID string
	Agent string
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
	// Links (feature 012 FR-015) records every subagent dispatch's context-link
	// decision this task — including declines with their reason — so the summary
	// and benchmark records always explain what was reused and what was not.
	Links []LinkOutcome `json:"links,omitempty"`
}

// LinkOutcome (feature 012, data-model.md ContextLink) is the per-dispatch
// record of the context-link decision and its provider-verified outcome. Cache
// figures are provider-reported or absent — never estimated (Constitution VI).
type LinkOutcome struct {
	RunID       string `json:"runId"`
	Kind        string `json:"kind"`
	Predecessor string `json:"predecessor,omitempty"`
	// Decision: "continued" | "digest-seeded" | "fresh".
	Decision string `json:"decision"`
	// Form is set when Decision=="continued": "same-kind" | "review-after-implement".
	Form string `json:"form,omitempty"`
	// Reason is the machine-readable first-failing (or passing) criterion from
	// contracts/context-linking.md CL-1, e.g. "eligible", "no-candidate",
	// "terminal-shape:failed", "stale:60%", "window-overflow", "disabled".
	Reason string `json:"reason"`
	// CacheShare is the first continuation request's paired cache-read share
	// (read/(read+miss)) from provider-reported usage; nil when the provider
	// reported no cache fields (rendered "unavailable", never estimated).
	CacheShare    *float64 `json:"cacheShare,omitempty"`
	CacheReported bool     `json:"cacheReported,omitempty"`
	// InheritedFiles/RereadFiles: touched-set members carried current vs
	// staleness-directed re-reads named in the successor handoff.
	InheritedFiles int `json:"inheritedFiles,omitempty"`
	RereadFiles    int `json:"rereadFiles,omitempty"`
	// OutboundChars/ReturnChars feed the SC-006 communication-overhead share.
	OutboundChars int `json:"outboundChars,omitempty"`
	ReturnChars   int `json:"returnChars,omitempty"`
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
