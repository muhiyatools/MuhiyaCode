package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	cap := e.taskAgentCap
	if cap <= 0 || e.taskAgentRuns >= cap {
		e.taskMu.Unlock()
		return "", fmt.Errorf("subagent budget exhausted (%d run(s)); complete the work directly", cap)
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
	e.addAgentUsage(result.Usage)
	if e.persistence.AddEvent != nil {
		summary, _ := json.Marshal(map[string]any{"runId": result.RunID, "agent": result.Agent, "title": result.Title, "status": result.Status, "turns": result.Turns, "toolCalls": result.ToolCalls, "usage": result.Usage, "task": truncateEllipsis(input.Task, 2000), "report": truncateEllipsis(result.Report, 4000)})
		_ = e.persistence.AddEvent(ctx, "agent", "run_summary", e.redact(string(summary)))
	}
	status := ""
	if result.Status != "done" {
		status = " [status: " + result.Status + "]"
	}
	return fmt.Sprintf("Subagent %q report%s (%d turns, %d tool calls):\n%s", result.Agent, status, result.Turns, result.ToolCalls, result.Report), nil
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
	system := "You are the " + spec.Name + " subagent inside MuhiyaCode. " + spec.System + "\nWorkspace: " + e.session.WorkspacePath
	if shared != "" {
		system += "\n\nShared session memory:\n" + shared
	}
	messages := []contract.Message{{Role: contract.RoleSystem, Content: system}, {Role: contract.RoleUser, Content: input.Task}}
	definitions := e.registry.Definitions(spec.Allowed)
	maxTurns := max(2, int(math.Ceil(float64(spec.MaxTurns)*Profile(e.effort()).AgentTurnScale)))
	for turn := 1; turn <= maxTurns; turn++ {
		result.Turns = turn
		response, err := e.provider.Chat(ctx, contract.ChatRequest{Messages: messages, Tools: definitions, ModelID: modelID, Reasoning: Profile(e.effort()).AgentReasoning})
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
			e.emitAgent(contract.AgentEvent{Kind: "tool_start", RunID: runID, CallID: call.ID, Tool: call.ToolName(), Arguments: call.ArgumentsJSON()})
			output, err := e.registry.Execute(ctx, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), spec.Allowed)
			if err != nil {
				output = fmt.Sprintf("Tool %s failed: %v", call.ToolName(), err)
			}
			output = CapToolOutput(output, Profile(e.effort()).ToolOutputCap)
			messages = append(messages, contract.Message{Role: contract.RoleTool, ToolCallID: call.ID, Content: output})
			e.emitAgent(contract.AgentEvent{Kind: "tool_end", RunID: runID, CallID: call.ID, Tool: call.ToolName(), Output: output})
			if isMutation(call.ToolName()) {
				if e.inspection != nil {
					e.history.MarkSuperseded(e.inspection.InvalidateFor(call))
				}
				if e.knowledge != nil {
					e.knowledge.MarkWorkspaceChanged()
				}
			}
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
