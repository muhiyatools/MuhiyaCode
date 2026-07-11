package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	outputReserveTokens = 12_000
	hardTurnCeiling     = 120
	maxPlanContinues    = 8
	// History-rewrite pressure gates. Below these fractions of the context
	// window, settled history is never rewritten so the DeepSeek prefix cache
	// keeps hitting across turns and tasks.
	foldPressureRatio = 0.55
	trimPressureRatio = 0.65
)

type Persistence struct {
	AddEvent         func(context.Context, string, string, string) error
	AppendTranscript func(context.Context, map[string]any) error
	WritePlan        func(context.Context, string) error
}

type RescueFunc func(string, []string) ([]contract.ToolCall, string)

type EngineConfig struct {
	Settings    *contract.Settings
	Secrets     contract.Secrets
	Session     contract.Session
	Provider    contract.Provider
	Registry    *Registry
	History     *History
	Inspection  *InspectionLedger
	Knowledge   *Knowledge
	Callbacks   contract.Callbacks
	Persistence Persistence
	Prompt      PromptContext
	InitialPlan contract.Plan
	Rescue      RescueFunc
	Redact      func(string) string
}

type Engine struct {
	settings    *contract.Settings
	secrets     contract.Secrets
	session     contract.Session
	provider    contract.Provider
	registry    *Registry
	history     *History
	inspection  *InspectionLedger
	knowledge   *Knowledge
	callbacks   contract.Callbacks
	persistence Persistence
	prompt      PromptContext
	rescue      RescueFunc
	redactFn    func(string) string

	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	steering []string
	plan     contract.Plan
	previous TaskClass
	effortMu sync.RWMutex

	goalMu sync.Mutex
	goal   *Goal

	planMu   sync.Mutex
	planMode bool

	taskMu          sync.Mutex
	sessionUsage    contract.Usage
	taskAgentUsage  contract.Usage
	taskAgentRuns   int
	taskAgentReused int
	taskAgentCap    int
	taskPeakContext float64
	taskDuplicates  int
	taskOverBudget  int
	runCounter      int
	callCounts      map[string]int
}

type toolOutcome struct {
	Call   contract.ToolCall
	Output string
	Failed bool
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
	return &Engine{settings: config.Settings, secrets: config.Secrets, session: config.Session, provider: config.Provider, registry: config.Registry, history: config.History, inspection: config.Inspection, knowledge: config.Knowledge, callbacks: config.Callbacks, persistence: config.Persistence, prompt: config.Prompt, plan: config.InitialPlan, rescue: config.Rescue, redactFn: config.Redact}, nil
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

func (e *Engine) CurrentPlan() contract.Plan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plan
}

func (e *Engine) Compact(ctx context.Context) (string, error) {
	if e.IsBusy() {
		return "", errors.New("cannot compact while a task is running")
	}
	if err := e.compact(ctx, "manual"); err != nil {
		return "", err
	}
	e.emitContext(0)
	return "Conversation compacted.", nil
}

type ContextReport struct {
	HistoryTokens int
	ContextLimit  int
	Percent       float64
	Usage         contract.Usage
}

func (e *Engine) ContextReport() ContextReport {
	history := e.history.EstimatedTokens()
	limit := e.contextLimit()
	return ContextReport{HistoryTokens: history, ContextLimit: limit, Percent: float64(history) / float64(max(1, limit)) * 100, Usage: e.Usage()}
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
	profile := Profile(e.effort())
	assessment := Classify(userPrompt, e.previous)
	e.previous = assessment.Class
	budget := BudgetFor(assessment, profile)
	modelPrompt := userPrompt
	if profile.Onboarding && ShouldConsiderOnboarding(userPrompt) && e.callbacks.Ask != nil {
		e.callbacks.EmitStatus("Clarifying the task...")
		questions, usage := GenerateOnboardingQuestions(ctx, e.provider, e.settings.Provider.SubagentModelID, userPrompt)
		e.addUsage(usage)
		if len(questions) > 0 {
			if answers, err := e.callbacks.Ask(ctx, questions); err == nil {
				modelPrompt = PromptWithAnswers(userPrompt, answers)
			}
		}
	}
	e.taskMu.Lock()
	e.taskAgentUsage = contract.Usage{}
	e.taskAgentRuns, e.taskAgentReused, e.taskDuplicates, e.taskOverBudget = 0, 0, 0, 0
	e.taskAgentCap = budget.MaxAgentRuns
	e.taskPeakContext = 0
	e.callCounts = make(map[string]int)
	e.taskMu.Unlock()
	filesChanged := make(map[string]bool)
	toolCalls, checksRun, turns, folded := 0, 0, 0, 0
	doneCriteria := ""
	defer func() {
		e.taskMu.Lock()
		stats = contract.TaskStats{DurationMS: time.Since(started).Milliseconds(), Effort: profile.Level, TaskClass: string(assessment.Class), Usage: subtractUsage(e.sessionUsage, usageStart), AgentUsage: e.taskAgentUsage, PeakContextPercent: e.taskPeakContext, ToolCalls: toolCalls, AgentRuns: e.taskAgentRuns, AgentRunsReused: e.taskAgentReused, Turns: turns, ChecksRun: checksRun, FoldedTokens: folded, DisciplineScore: max(0, 100-min(24, e.taskDuplicates*8)-min(16, e.taskOverBudget*2)), DoneCriteria: doneCriteria}
		e.taskMu.Unlock()
		for file := range filesChanged {
			stats.FilesChanged = append(stats.FilesChanged, file)
		}
		sort.Strings(stats.FilesChanged)
		if e.callbacks.TaskComplete != nil {
			e.callbacks.TaskComplete(stats)
		}
	}()

	// Fold prior tasks only under real context pressure. Rewriting settled
	// history busts DeepSeek's prefix cache, so a session that stays well
	// under its window keeps every earlier message byte-for-byte and reuses
	// the whole cached prefix across tasks.
	if e.history.EstimatedTokens() > int(float64(e.contextLimit())*foldPressureRatio) {
		folded = e.history.FoldCompletedTasks()
	}
	if err := e.persistMessage(ctx, "user", "message", userPrompt, map[string]any{"role": "user", "content": userPrompt, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return "", stats, err
	}
	e.history.MarkTaskStart()
	brief := budget.Brief
	if plan := e.planBlock(); plan != "" {
		brief = plan + "\n" + brief
	}
	if goal := e.goalBlock(); goal != "" {
		brief = goal + "\n" + brief
	}
	e.history.Append(contract.Message{Role: contract.RoleUser, Content: modelPrompt + "\n\n" + brief})
	e.emitContext(0)

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
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Final step: tools are disabled. Give the complete factual final answer now: outcome, verification, and genuine remaining work."})
		} else if !isFinal && turns >= turnCap-2 && !convergeNoted {
			convergeNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Two steps remain. Batch only essential edits, verify, and finish."})
		}
		if e.needsCompact(live) {
			e.callbacks.EmitStatus("Compacting context...")
			if err := e.compact(ctx, "automatic: context nearly full"); err != nil {
				e.history.Append(contract.Message{Role: contract.RoleSystem, Content: "Automatic compaction failed; continue using the bounded recent context."})
			}
		}
		// Trim aged tool payloads only when the window is genuinely filling.
		// Trimming rewrites history and busts the prefix cache, so it must not
		// run on every turn — only once pressure crosses the threshold.
		if e.history.EstimatedTokens() > int(float64(e.contextLimit())*trimPressureRatio) {
			e.history.TrimAged(live.KeepFullToolOutputs, live.TrimmedToolOutputChars, 4)
		}
		e.callbacks.EmitStatus("Thinking...")
		messages := e.history.BuildRequest(promptText, e.contextLimit(), outputReserveTokens)
		toolChoice := "auto"
		if isFinal {
			toolChoice = "none"
		}
		// Reasoning effort is the user's chosen level, sent raw; the gateway
		// maps it onto each provider's supported thinking ladder. It is a
		// request parameter, not a message, so it never affects the prefix cache.
		response, err := e.provider.Chat(ctx, contract.ChatRequest{Messages: messages, Tools: definitions, ModelID: e.settings.Provider.ActiveModelID, ToolChoice: toolChoice, Reasoning: ReasoningForEffort(e.effort()), OnToken: e.callbacks.Token, OnReasoningToken: e.callbacks.ReasoningToken})
		if err != nil {
			return "", stats, err
		}
		e.addUsage(response.Usage)
		if e.callbacks.Usage != nil {
			e.callbacks.Usage(e.Usage())
		}
		e.emitContext(response.Usage.PromptTokens)
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
			if next := e.advanceGoal(trimmed); next != "" {
				turnCap = min(hardTurnCeiling, max(turnCap, turns+8))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: next})
				continue
			}
			return e.finalize(ctx, fallbackAnswer(trimmed, filesChanged)), stats, nil
		}

		sawToolCall = true
		if strings.TrimSpace(assistantText) != "" {
			if err := e.persistMessage(ctx, "assistant", "message", assistantText, map[string]any{"role": "assistant", "content": assistantText, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
		}
		e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: assistantText, ToolCalls: calls})
		outcomes := e.executeBatch(ctx, calls, definitions, live)
		for _, outcome := range outcomes {
			toolCalls++
			if outcome.Failed {
				consecutiveFailures++
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
		if consecutiveFailures >= 3 {
			consecutiveFailures = 0
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "Several tools failed. Re-read their errors, change approach, and explain a genuine blocker if progress is impossible."})
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
	definitions := e.registry.Definitions(nil)
	definitions = append(definitions, updatePlanDefinition(), askUserDefinition(), proposeChangesDefinition())
	definitions = append(definitions, runSubagentDefinition(e.subagentSpecs()))
	return definitions
}

// subagentKind reads the "agent" field out of a run_subagent call's raw
// arguments without fully decoding/validating them; executeSubagent performs
// the authoritative parse. Used only to decide whether a batch is safe to
// parallelize before any tool actually runs.
func subagentKind(call contract.ToolCall) string {
	var input struct {
		Agent string `json:"agent"`
	}
	_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &input)
	return strings.TrimSpace(input.Agent)
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

func (e *Engine) executeCall(ctx context.Context, call contract.ToolCall, definitions []contract.ToolDefinition, effort EffortProfile) toolOutcome {
	name := call.ToolName()
	if e.callbacks.ToolStart != nil {
		e.callbacks.ToolStart(name, json.RawMessage(call.ArgumentsJSON()))
	}
	// Plan mode is read-only: refuse mutating tools so the agent can research
	// and propose a plan without touching the workspace.
	if e.PlanMode() && isMutation(name) {
		output := "Blocked: plan mode is on (read-only). Finish planning; the user will run /plan to allow edits."
		e.endTool(name, output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	signature := callSignature(call)
	e.taskMu.Lock()
	e.callCounts[signature]++
	repeats := e.callCounts[signature]
	e.taskMu.Unlock()
	if repeats > 3 {
		output := "Blocked: identical call repeated three times; take a different action or finish."
		e.endTool(name, output)
		return toolOutcome{Call: call, Output: output, Failed: true}
	}
	if entry, duplicate := e.inspection.Duplicate(call, e.history.IsToolResultIntact); duplicate {
		e.taskMu.Lock()
		e.taskDuplicates++
		e.taskMu.Unlock()
		output := fmt.Sprintf("Blocked: unchanged result already in context from call %s. Use that result; do not re-read.", entry.CallID)
		e.endTool(name, output)
		return toolOutcome{Call: call, Output: output, Failed: false}
	}
	output, err := e.executeOne(ctx, call, definitions)
	if err != nil {
		output = fmt.Sprintf("Tool %s failed: %v", name, err)
	}
	output = CapToolOutput(output, effort.ToolOutputCap)
	failed := IsToolFailure(output, err)
	if !failed && readonlyTools[name] {
		e.inspection.Record(call, output)
	}
	if isMutation(name) {
		e.history.MarkSuperseded(e.inspection.InvalidateFor(call))
	}
	e.endTool(name, output)
	return toolOutcome{Call: call, Output: output, Failed: failed}
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
	answers, err := e.callbacks.Ask(ctx, input.Questions)
	if err != nil {
		return "", err
	}
	return encodeAnswers(answers), nil
}

func (e *Engine) proposeChanges(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.callbacks.Ask == nil {
		return `{"verdict":"approved","note":"non-interactive client; proceed minimally and verify"}`, nil
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
	request := []contract.Message{{Role: contract.RoleSystem, Content: "Compress a coding-agent session under headings GOAL, STATE, FILES, DECISIONS, COMMANDS, PENDING. Preserve all durable facts and exact paths; use terse bullets."}, {Role: contract.RoleUser, Content: "Reason: " + reason + "\nEarlier summary:\n" + e.history.CompactSummary() + "\nDurable facts:\n" + strings.Join(e.knowledge.CompactionFacts(), "\n") + "\nConversation:\n" + strings.Join(lines, "\n")}}
	temperature := .1
	response, err := e.provider.Chat(ctx, contract.ChatRequest{Messages: request, ModelID: e.settings.Provider.ActiveModelID, MaxTokens: 1600, Temperature: &temperature, Reasoning: contract.ReasoningLow})
	summary := ""
	if err == nil {
		e.addUsage(response.Usage)
		summary = strings.TrimSpace(response.Content)
	}
	if summary == "" {
		summary = strings.Join(lines, "\n")
	}
	e.history.CompactTo("Workspace: "+e.session.WorkspacePath+"\n"+summary, 2)
	if e.persistence.AddEvent != nil {
		_ = e.persistence.AddEvent(ctx, "system", "compact_summary", e.redact(summary))
	}
	return nil
}

func (e *Engine) needsCompact(profile EffortProfile) bool {
	usable := max(8000, e.contextLimit()-outputReserveTokens)
	return float64(e.history.EstimatedTokens()) > float64(usable)*profile.CompactThreshold
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
	// The task is ending regardless: the user must still see the answer even
	// if it could not be persisted, so a persistence failure here is
	// surfaced as a warning rather than turned into a task error (which
	// would discard the successful answer along with it).
	if err := e.persistAssistant(ctx, content); err != nil {
		e.callbacks.EmitStatus("Warning: failed to save the final answer to session history: " + err.Error())
	}
	return content
}

func (e *Engine) redact(value string) string {
	if e.redactFn != nil {
		return e.redactFn(value)
	}
	return value
}

func (e *Engine) addUsage(usage contract.Usage) {
	e.taskMu.Lock()
	e.sessionUsage = e.sessionUsage.Add(usage)
	e.taskMu.Unlock()
}

func (e *Engine) addAgentUsage(usage contract.Usage) {
	e.taskMu.Lock()
	e.sessionUsage = e.sessionUsage.Add(usage)
	e.taskAgentUsage = e.taskAgentUsage.Add(usage)
	e.taskMu.Unlock()
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
	return contract.Usage{PromptTokens: max(0, total.PromptTokens-before.PromptTokens), CompletionTokens: max(0, total.CompletionTokens-before.CompletionTokens), TotalTokens: max(0, total.TotalTokens-before.TotalTokens), CachedTokens: max(0, total.CachedTokens-before.CachedTokens)}
}
