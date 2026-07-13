package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type subagentSpec struct {
	Name, Description, System string
	Allowed                   map[string]bool
	MaxTurns                  int
}

type subagentInput struct {
	Agent string `json:"agent"`
	Title string `json:"title"`
	Task  string `json:"task"`
}

type subagentResult struct {
	RunID, Agent, Title, Report, Status string
	Turns, ToolCalls                    int
	Usage                               contract.Usage
}

func (e *Engine) subagentSpecs() map[string]subagentSpec {
	read := map[string]bool{"list_files": true, "read_file": true, "grep": true, "search_text": true, "glob": true, "git_status": true, "git_diff": true}
	all := make(map[string]bool)
	for _, name := range e.registry.Names() {
		all[name] = true
	}
	clone := func(value map[string]bool) map[string]bool {
		result := make(map[string]bool, len(value))
		for key, allowed := range value {
			result[key] = allowed
		}
		return result
	}
	return map[string]subagentSpec{
		"explore": {Name: "explore", Description: "Read-only codebase exploration that returns grounded findings.", Allowed: clone(read), MaxTurns: 12, System: "Explore the requested code paths efficiently. Search first, batch reads, cite exact files and symbols, and report only verified findings. You cannot edit."},
		"plan":    {Name: "plan", Description: "Read-only implementation planning grounded in the real code.", Allowed: clone(read), MaxTurns: 14, System: "Inspect the relevant code, then return an ordered implementation plan with exact files/functions, edge cases, and verification. Do not edit."},
		"review":  {Name: "review", Description: "Read-only correctness and security review with ranked findings.", Allowed: clone(read), MaxTurns: 14, System: "Review the diff and surrounding code. Report only verified correctness, security, or reliability issues ranked by severity with file:line and a failure scenario. No style nits; do not edit."},
		"general": {Name: "general", Description: "Full-tool agent for an isolated, self-contained coding subtask.", Allowed: all, MaxTurns: 24, System: "Complete the isolated subtask end to end. Inspect before editing, make focused changes, run the smallest meaningful checks, and report changed files, verification, and remaining risk. Do not ask the user questions."},
	}
}

func (e *Engine) runSubagentTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var input subagentInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid run_subagent arguments: %w", err)
	}
	input.Agent, input.Task, input.Title = strings.TrimSpace(input.Agent), strings.TrimSpace(input.Task), strings.TrimSpace(input.Title)
	// P3: in plan mode, the "general" subagent gets the full mutating registry.
	// Block it at the gate so the UI's read-only invariant cannot be bypassed
	// by spawning a general subagent from inside a plan-mode task.
	if e.PlanMode() && input.Agent == "general" {
		return "", errors.New("blocked: plan mode is read-only; only explore/plan/review subagents are available. Use them to investigate, then finish your plan.")
	}
	spec, ok := e.subagentSpecs()[input.Agent]
	if !ok || input.Task == "" {
		return "", fmt.Errorf("agent must be explore, plan, review, or general and task is required")
	}
	if (input.Agent == "explore" || input.Agent == "plan") && e.knowledge != nil {
		if fact, ok := e.knowledge.Reusable(input.Agent, input.Task); ok {
			e.taskMu.Lock()
			e.taskAgentReused++
			e.taskMu.Unlock()
			body := fact.Full
			if body == "" {
				body = fact.Text
			}
			return fmt.Sprintf("Subagent %q report (reused; workspace unchanged; zero tokens):\n%s", input.Agent, body), nil
		}
	}
	e.taskMu.Lock()
	limit := e.taskAgentCap
	if limit <= 0 || e.taskAgentRuns >= limit {
		e.taskAgentDenied++
		denied, used := e.taskAgentDenied, e.taskAgentRuns
		e.taskMu.Unlock()
		// The denial must teach the model to stop delegating: state the reason,
		// state the only useful next action, and escalate on repeat calls so a
		// retry loop converges instead of burning turns on a closed door.
		reason := fmt.Sprintf("subagent budget exhausted (%d of %d run(s) used)", used, limit)
		if limit <= 0 {
			reason = "no subagent budget for this task (agents=0)"
		}
		// 004 US3 (T035): when the task has NO subagent budget at all (agents=0),
		// the door is closed from the very first call — say so immediately instead
		// of inviting a second attempt. A mid-task exhaustion (limit>0) still gives
		// the softer message first and escalates only on a repeat.
		if denied > 1 || limit <= 0 {
			return "", fmt.Errorf("%s. run_subagent is closed for the rest of this task and every further call will fail — do NOT call it again. Continue the remaining work directly with your own read/edit tools now", reason)
		}
		return "", fmt.Errorf("%s; complete the remaining work directly with your own tools instead of delegating", reason)
	}
	e.taskAgentRuns++
	e.runCounter++
	runID := fmt.Sprintf("a%d-%x", e.runCounter, time.Now().UnixMilli())
	e.taskMu.Unlock()
	if input.Title == "" {
		input.Title = deriveTitle(input.Task)
	}
	result := e.executeSubagent(ctx, runID, input, spec)
	if result.Status == "done" && len(result.Report) >= 80 && (input.Agent == "explore" || input.Agent == "plan") && e.knowledge != nil {
		e.knowledge.AddReport(input.Agent, input.Title, input.Task, result.Report)
	}
	e.addTaskAgentUsage(result.Usage)
	if e.persistence.AddEvent != nil {
		summary, _ := json.Marshal(map[string]any{"runId": result.RunID, "agent": result.Agent, "title": result.Title, "status": result.Status, "turns": result.Turns, "toolCalls": result.ToolCalls, "usage": result.Usage, "task": truncateEllipsis(input.Task, 2000), "report": truncateEllipsis(result.Report, 4000)})
		_ = e.persistence.AddEvent(ctx, "agent", "run_summary", e.redact(string(summary)))
	}
	status := ""
	if result.Status != "done" {
		status = " [status: " + result.Status + "]"
	}
	// Report the remaining budget with every run so the model can track it and
	// switch to direct work BEFORE hitting the exhaustion error.
	e.taskMu.Lock()
	remaining := max(0, e.taskAgentCap-e.taskAgentRuns)
	e.taskMu.Unlock()
	budgetNote := fmt.Sprintf("%d subagent run(s) remaining", remaining)
	if remaining == 0 {
		budgetNote = "subagent budget now exhausted — do the remaining work directly"
	}
	return fmt.Sprintf("Subagent %q report%s (%d turns, %d tool calls; %s):\n%s", result.Agent, status, result.Turns, result.ToolCalls, budgetNote, result.Report), nil
}

func (e *Engine) executeSubagent(ctx context.Context, runID string, input subagentInput, spec subagentSpec) subagentResult {
	result := subagentResult{RunID: runID, Agent: input.Agent, Title: input.Title, Status: "done"}
	modelID := e.settings.Provider.SubagentModelID
	if modelID == "" {
		modelID = e.settings.Provider.ActiveModelID
	}
	modelName := modelID
	for _, model := range e.settings.Provider.Models {
		if model.ID == modelID {
			modelName = model.Name
			break
		}
	}
	e.emitAgent(contract.AgentEvent{Kind: "start", RunID: runID, Agent: input.Agent, Title: input.Title, Task: input.Task, Model: modelName})
	shared := ""
	if e.knowledge != nil {
		shared = e.knowledge.Briefing(1500)
	}
	system := "You are the " + spec.Name + " subagent inside MuhiyaCode. " + spec.System + "\nWorkspace: " + e.session.WorkspacePath + "\n" + capabilityStatement(spec)
	if shared != "" {
		system += "\n\nShared session memory:\n" + shared
	}
	messages := []contract.Message{{Role: contract.RoleSystem, Content: system}, {Role: contract.RoleUser, Content: input.Task}}
	definitions := e.registry.Definitions(spec.Allowed)
	// B6/T025: subagents dispatch through the SAME shared gate as the main loop
	// (validation, failed-cache, repeat limiter, storm breaker, plan-mode gate),
	// but with their OWN per-run counters so their gate state never pollutes the
	// parent's. The duplicate-read guard is main-scope only (the inspection
	// ledger is main-task state), so dedupe/trackStats are off. Loop-guard
	// escalations land in this subagent's OWN transcript, never parent history.
	// Dispatch stays restricted to spec.Allowed via registry.Execute (never the
	// synthetic-tool switch), so a subagent can't reach run_subagent/exit_plan_mode.
	sub := dispatchScope{
		counters: newCallCounters(),
		onStart: func(call contract.ToolCall) {
			e.emitAgent(contract.AgentEvent{Kind: "tool_start", RunID: runID, CallID: call.ID, Tool: call.ToolName(), Arguments: call.ArgumentsJSON()})
		},
		onEnd: func(call contract.ToolCall, output string) {
			e.emitAgent(contract.AgentEvent{Kind: "tool_end", RunID: runID, CallID: call.ID, Tool: call.ToolName(), Output: output})
		},
		escalate: func(notice string) {
			messages = append(messages, contract.Message{Role: contract.RoleUser, Content: notice})
		},
		dispatch: func(c context.Context, call contract.ToolCall) (string, error) {
			return e.registry.Execute(c, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), spec.Allowed)
		},
		postDispatch: func(call contract.ToolCall, output string, failed bool, dispatchErr error) {
			name := call.ToolName()
			if isMutation(name) {
				// H7: a read-only shell probe must not bust caches in the subagent path.
				if name == "run_shell" {
					var args struct {
						Command string `json:"command"`
					}
					_ = json.Unmarshal([]byte(call.ArgumentsJSON()), &args)
					if IsReadOnlyShell(args.Command) {
						return
					}
				}
				if e.inspection != nil {
					e.inspection.InvalidateFor(call)
				}
				if e.knowledge != nil {
					e.knowledge.MarkWorkspaceChanged()
				}
			}
		},
	}
	var previousShape *PrefixShape
	maxTurns := max(2, int(math.Ceil(float64(spec.MaxTurns)*Profile(e.effort()).AgentTurnScale)))
	for turn := 1; turn <= maxTurns; turn++ {
		result.Turns = turn
		shape, shapeErr := NewPrefixShape(system, definitions, 0, modelID)
		if shapeErr != nil {
			result.Status = "failed"
			result.Report = "compute subagent prefix shape: " + shapeErr.Error()
			break
		}
		var reasons []string
		if previousShape != nil {
			reasons = CompareShape(*previousShape, shape)
		}
		// C3: within a subagent run, system + tools are fixed at run start.
		// Any non-empty reasons after the first turn is a real prefix bust, so
		// mirror the main-loop guard by failing the subagent run. The reported
		// Reason yields a parseable error in the parent's transcript.
		if len(reasons) > 0 {
			result.Status = "failed"
			result.Report = "subagent stable prefix changed without invalidation event: " + strings.Join(reasons, ",")
			break
		}
		response, err := e.provider.Chat(ctx, contract.ChatRequest{SessionID: e.session.ID + ":sub", Messages: messages, Tools: definitions, ModelID: modelID, Reasoning: Profile(e.effort()).AgentReasoning})
		if usageErr := e.recordIsolatedUsage(ctx, modelID, response.Usage, previousShape == nil, reasons); usageErr != nil {
			result.Status = "failed"
			result.Report = "persist subagent usage: " + usageErr.Error()
			break
		}
		// Keep the parent's live task-usage display in step with subagent
		// spending — the task summary already includes it, so the live line
		// must too.
		e.emitTaskUsage()
		previousShape = &shape
		if err != nil {
			result.Status = statusFromContext(ctx, "failed")
			result.Report = err.Error()
			break
		}
		result.Usage = result.Usage.Add(response.Usage)
		e.emitAgent(contract.AgentEvent{Kind: "usage", RunID: runID, Usage: result.Usage})
		calls := response.ToolCalls
		text := response.Content
		if len(calls) == 0 && e.rescue != nil {
			calls, text = e.rescue(text, toolNames(definitions))
		}
		if len(calls) == 0 {
			result.Report = strings.TrimSpace(text)
			if result.Report == "" {
				result.Report = "(subagent returned no report)"
			}
			break
		}
		messages = append(messages, contract.Message{Role: contract.RoleAssistant, Content: text, ToolCalls: calls})
		if strings.TrimSpace(text) != "" {
			e.emitAgent(contract.AgentEvent{Kind: "text", RunID: runID, Content: text})
		}
		for _, call := range calls {
			result.ToolCalls++
			// B6/T025: one shared gate. Plan-mode blocking (P3 defense in depth),
			// H1 validation, the failed-cache, the repeat limiter, the storm
			// breaker, and H7 read-only-shell invalidation all live in gatedExecute
			// / the sub scope now, so this loop only dispatches and pairs the result.
			outcome := e.gatedExecute(ctx, call, definitions, Profile(e.effort()), sub)
			messages = append(messages, contract.Message{Role: contract.RoleTool, ToolCallID: call.ID, Content: outcome.Output})
		}
		if turn == maxTurns {
			result.Status = "failed"
			result.Report = "Subagent reached its bounded turn limit before returning a report."
		}
	}
	e.emitAgent(contract.AgentEvent{Kind: "done", RunID: runID, Agent: input.Agent, Title: input.Title, Status: result.Status, Report: result.Report, Turns: result.Turns, ToolCalls: result.ToolCalls, Usage: result.Usage})
	return result
}

func (e *Engine) emitAgent(event contract.AgentEvent) {
	if e.callbacks.Agent != nil {
		e.callbacks.Agent(event)
	}
}

func deriveTitle(task string) string {
	line := strings.TrimSpace(strings.SplitN(task, "\n", 2)[0])
	line = strings.Join(strings.Fields(strings.Trim(line, "#>*`-")), " ")
	return truncateEllipsis(line, 56)
}

func statusFromContext(ctx context.Context, fallback string) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return fallback
}

func isMutation(name string) bool {
	return name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "run_shell" || strings.HasPrefix(name, "mcp__")
}

// capabilityStatement (004 US3, T038) is the explicit boundary a delegated
// subagent is told before it acts: its exact toolset (sorted, deterministic),
// that nothing else is available to it, and — per the DeepSeek worker-mode
// findings (research B7/D10) — that it should surface ambiguity and
// architectural choices back to the caller instead of deciding them. It is
// per-mission message content, never part of the cached prefix.
func capabilityStatement(spec subagentSpec) string {
	names := make([]string, 0, len(spec.Allowed))
	for name, ok := range spec.Allowed {
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return "Tools available to you: " + strings.Join(names, ", ") +
		". Anything not listed is unavailable to you — do not attempt it. Return your findings or diffs; when you hit ambiguity or an architectural choice, report it back to the caller rather than deciding it yourself."
}
