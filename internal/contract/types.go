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
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
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
	Messages         []Message
	Tools            []ToolDefinition
	ModelID          string
	Temperature      *float64
	MaxTokens        int
	ToolChoice       string
	Reasoning        ReasoningTier
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

type TaskStats struct {
	DurationMS         int64       `json:"durationMs"`
	Effort             EffortLevel `json:"effort"`
	TaskClass          string      `json:"taskClass"`
	Usage              Usage       `json:"usage"`
	AgentUsage         Usage       `json:"agentUsage"`
	PeakContextPercent float64     `json:"peakContextPercent"`
	ToolCalls          int         `json:"toolCalls"`
	AgentRuns          int         `json:"agentRuns"`
	AgentRunsReused    int         `json:"agentRunsReused"`
	FilesChanged       []string    `json:"filesChanged"`
	Turns              int         `json:"turns"`
	ChecksRun          int         `json:"checksRun"`
	FoldedTokens       int         `json:"foldedTokens"`
	DisciplineScore    int         `json:"disciplineScore"`
	DoneCriteria       string      `json:"doneCriteria,omitempty"`
}

type Callbacks struct {
	Status         func(string)
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
