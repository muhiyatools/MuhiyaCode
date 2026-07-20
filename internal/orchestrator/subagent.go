package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

type subagentSpec struct {
	Name, Description, System string
	Allowed                   map[string]bool
	MaxTurns                  int
}

type subagentInput struct {
	Agent string `json:"agent"`
	// Role is the model's own name for this dispatch ("auth-flow-mapper").
	// DISPLAY AND HANDOFF ONLY: cache identity — the pin, the stable system
	// message, the context record's Kind — stays keyed on Agent, so a novel role
	// name can never fragment the provider cache.
	Role  string `json:"role"`
	Title string `json:"title"`
	Task  string `json:"task"`
	// TokenCeiling (feature 011 D3, contracts/subagent-handoff.md §3) bounds this
	// run's total provider-reported token consumption. 0 = uncapped. Harness-set
	// only (review-tier dispatches copy the gate's absolute cap here); the json
	// tag keeps it out of the model-facing tool schema so a model call can never
	// raise its own budget.
	TokenCeiling int `json:"-"`
}

type subagentResult struct {
	RunID, Agent, Title, Report, Status string
	Turns, ToolCalls                    int
	Usage                               contract.Usage
	// TerminalShape (feature 012 R-D4) distinguishes clean completions from
	// wrap-up/ceiling/failed/cancelled endings in every durable record.
	TerminalShape string
}

type HandoffContract struct {
	Role         string
	Scope        string
	Context      string
	Deliverable  string
	OutputFormat string
}

func (h HandoffContract) Render() string {
	context := strings.TrimSpace(h.Context)
	if context == "" {
		context = instructions.HandoffContractNoOverlapNote
	}
	return instructions.HandoffContractHeader + "\n" +
		"Role: " + h.Role + "\n" +
		"Scope: " + h.Scope + "\n" +
		"Context: " + context + "\n" +
		"Deliverable: " + h.Deliverable + "\n" +
		"OutputFormat: " + h.OutputFormat
}

func handoffRole(agent string) string {
	switch agent {
	case "general":
		return "implement-step"
	case "review":
		return "review"
	default:
		return "research-scope"
	}
}

func handoffFor(input subagentInput, context string) HandoffContract {
	role := handoffRole(input.Agent)
	deliverable := instructions.HandoffDeliverableResearch
	format := instructions.ReportFormatResearch
	switch role {
	case "implement-step":
		deliverable = instructions.HandoffDeliverableImplementation
		format = instructions.ReportFormatImplementation
	case "review":
		deliverable = instructions.HandoffDeliverableReview
		format = instructions.ReportFormatReview
	}
	// The model's own name for this dispatch leads the Role line when it gave
	// one — it tells the agent what it IS here, which the generic class cannot.
	// This rides the per-run user message, never the stable system prefix.
	if named := strings.TrimSpace(input.Role); named != "" {
		role = named + " (" + role + ")"
	}
	return HandoffContract{Role: role, Scope: contract.TruncateEllipsis(input.Task, 1200), Context: context, Deliverable: deliverable, OutputFormat: format}
}

// displayRole is what the transcript chip shows: the model's role name, then
// its free-form title, and only as a last resort the internal capability class
// — a user should never see a bare "general" as an agent's identity.
func displayRole(input subagentInput) string {
	if named := strings.TrimSpace(input.Role); named != "" {
		return named
	}
	if title := strings.TrimSpace(input.Title); title != "" {
		return title
	}
	return input.Agent
}

func (e *Engine) subagentSpecs() map[string]subagentSpec {
	read := map[string]bool{"list_files": true, "read_file": true, "grep": true, "search_text": true, "glob": true, "git_status": true, "git_diff": true, "run_shell": true}
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
		"explore": {Name: "explore", Description: instructions.SubagentExploreDescription, Allowed: clone(read), MaxTurns: 12, System: instructions.SubagentExploreSystem},
		"review":  {Name: "review", Description: instructions.SubagentReviewDescription, Allowed: clone(read), MaxTurns: 14, System: instructions.SubagentReviewSystem},
		"general": {Name: "general", Description: instructions.SubagentGeneralDescription, Allowed: all, MaxTurns: 24, System: instructions.SubagentGeneralSystem},
	}
}

func (e *Engine) runSubagentTool(ctx context.Context, raw json.RawMessage) (string, error) {
	var input subagentInput
	if err := decodeToolArgs(raw, &input); err != nil {
		return "", fmt.Errorf("invalid run_subagent arguments: %w", err)
	}
	return e.runSubagentInput(ctx, input)
}

// runSubagentInput is the typed core behind run_subagent. The model's tool
// call enters through runSubagentTool's JSON boundary; harness-driven pipeline
// launches call this directly with the struct (no marshal/unmarshal round-trip).
func (e *Engine) runSubagentInput(ctx context.Context, input subagentInput) (string, error) {
	input.Agent, input.Task, input.Title = strings.TrimSpace(input.Agent), strings.TrimSpace(input.Task), strings.TrimSpace(input.Title)
	spec, ok := e.subagentSpecs()[input.Agent]
	if !ok || input.Task == "" {
		return "", fmt.Errorf("agent must be explore, review, or general and task is required")
	}
	if input.Agent == "explore" && e.knowledge != nil {
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
	used := e.taskAgentRuns
	if limit <= 0 || used >= limit {
		e.taskAgentDenied++
		denied := e.taskAgentDenied
		e.taskMu.Unlock()
		// The denial must teach the model to stop delegating: state the reason,
		// state the only useful next action, and escalate on repeat calls so a
		// retry loop converges instead of burning turns on a closed door.
		reason := fmt.Sprintf(instructions.GateSubagentBudgetExhaustedTmpl, used, limit)
		if limit <= 0 {
			reason = instructions.GateSubagentBudgetZeroBody
		}
		// 004 US3 (T035): when the task has NO subagent budget at all (agents=0),
		// the door is closed from the very first call — say so immediately instead
		// of inviting a second attempt. A mid-task exhaustion (limit>0) still gives
		// the softer message first and escalates only on a repeat.
		if denied > 1 || limit <= 0 {
			return "", fmt.Errorf(instructions.GateSubagentBudgetClosedTmpl, reason)
		}
		return "", fmt.Errorf(instructions.GateSubagentBudgetSoftTmpl, reason)
	}
	e.taskAgentRuns++
	e.runCounter++
	runID := fmt.Sprintf("a%d-%x", e.runCounter, time.Now().UnixMilli())
	e.taskMu.Unlock()
	if input.Title == "" {
		input.Title = deriveTitle(input.Task)
	}
	result := e.executeSubagent(ctx, runID, input, spec)
	// Every usable report banks with its role provenance so scoped briefings and
	// the reuse fast path can find it later.
	if (result.Status == "done" || strings.HasPrefix(result.Status, "partial")) && len(result.Report) >= 80 && e.knowledge != nil {
		e.knowledge.AddPhaseReport(input.Agent, "", handoffRole(input.Agent), input.Title, input.Task, result.Report)
	}
	e.addTaskAgentUsage(result.Usage)
	// Every review dispatch stamps its outcome onto the recorded review decision
	// (ceiling hit, coverage line) so completion stats and benchmark records
	// carry them, whoever launched the reviewer. Idempotent: re-parsing the same
	// report writes the same fields.
	if input.Agent == "review" {
		e.updateTaskReviewOutcome(result.Report)
	}
	if e.persistence.AddEvent != nil {
		summary, _ := json.Marshal(map[string]any{"runId": result.RunID, "agent": result.Agent, "title": result.Title, "status": result.Status, "terminalShape": result.TerminalShape, "turns": result.Turns, "toolCalls": result.ToolCalls, "usage": result.Usage, "task": contract.TruncateEllipsis(input.Task, 2000), "report": contract.TruncateEllipsis(result.Report, 4000)})
		_ = e.persistence.AddEvent(ctx, "agent", "run_summary", e.redact(string(summary)), "")
	}
	status := ""
	if result.Status != "done" {
		status = " [status: " + result.Status + "]"
	}
	// Report the remaining budget with every run so the model can track it and
	// switch to direct work BEFORE hitting the exhaustion error.
	e.taskMu.Lock()
	used = e.taskAgentRuns
	remaining := max(0, e.taskAgentCap-used)
	e.taskMu.Unlock()
	budgetNote := fmt.Sprintf("%d subagent run(s) remaining", remaining)
	if remaining == 0 {
		budgetNote = "subagent budget now exhausted — do the remaining work directly"
	}
	// Full reports are durable in the agent event/knowledge sidecars. Return a
	// bounded handoff to the parent so phase results do not become a second
	// transcript inside the main conversation. D7: when the bound truncates, say
	// so explicitly and point at the banked full report — the main context never
	// receives content above the bound, and never silently loses it either.
	parentReport := contract.TruncateEllipsis(result.Report, 2200)
	truncNote := ""
	if len(result.Report) > 2200 {
		truncNote = fmt.Sprintf("\n[digest: %d-char full report banked to knowledge; later phases receive it via their briefing]", len(result.Report))
	}
	return fmt.Sprintf("Subagent %q report%s (%d turns, %d tool calls; %s):\n%s%s", result.Agent, status, result.Turns, result.ToolCalls, budgetNote, parentReport, truncNote), nil
}

// subagentSystemMessage composes the per-kind-per-session STABLE system
// message (feature 012 R-D6, contracts/phase-handoff.md PH-1): kind identity,
// spec system text, workspace, and capability statement — never any per-run
// content. Every dispatch of a kind therefore shares the provider-cached
// system+tools prefix; the per-run handoff rides the first user message.
func (e *Engine) subagentSystemMessage(spec subagentSpec) string {
	return "You are the " + spec.Name + " subagent inside MuhiyaCode. " + spec.System + "\nWorkspace: " + e.session.WorkspacePath + "\n" + capabilityStatement(spec)
}

// subagentUserMessage joins the per-run handoff with the task as the dispatch
// user message (PH-1 placement).
func subagentUserMessage(handoff HandoffContract, task string) string {
	return handoff.Render() + "\n\nTASK:\n" + task
}

func (e *Engine) executeSubagent(ctx context.Context, runID string, input subagentInput, spec subagentSpec) subagentResult {
	result := subagentResult{RunID: runID, Agent: input.Agent, Title: input.Title, Status: "done"}
	modelID := e.subagentModelID()
	modelName := modelID
	for _, model := range e.settings.Provider.Models {
		if model.ID == modelID {
			modelName = model.Name
			break
		}
	}
	// The event carries the DISPLAY role (the model's name for this agent); the
	// internal class stays on AgentEvent.Agent for anything keying on capability.
	role := displayRole(input)
	plan := e.planDispatch(input, spec, modelID)
	decision, streamSpec, capture := plan.decision, plan.streamSpec, plan.capture
	system, definitions, messages, handoff := plan.system, plan.definitions, plan.messages, plan.handoff
	e.emitAgent(contract.AgentEvent{Kind: "start", RunID: runID, Agent: input.Agent, Role: role, Title: input.Title, Task: input.Task, Handoff: handoff.Render(), Model: modelName})
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
		readOnly: input.Agent != "general",
		onStart: func(call contract.ToolCall) {
			e.emitAgent(contract.AgentEvent{Kind: "tool_start", RunID: runID, Tool: call.ToolName(), Arguments: call.ArgumentsJSON()})
		},
		onEnd: func(call contract.ToolCall, output string) {
			e.emitAgent(contract.AgentEvent{Kind: "tool_end", RunID: runID, Tool: call.ToolName(), Output: output})
		},
		escalate: func(notice string) {
			messages = append(messages, contract.Message{Role: contract.RoleUser, Content: notice})
		},
		dispatch: func(c context.Context, call contract.ToolCall) (string, error) {
			return e.registry.Execute(c, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), streamSpec.Allowed)
		},
		postDispatch: func(call contract.ToolCall, output string, failed bool, dispatchErr error) {
			name := call.ToolName()
			if !failed {
				// Feature 012 R-D5: per-run read/write evidence for the
				// continuation record (end-of-run fingerprints at finalize).
				capture.note(name, call.ArgumentsJSON())
			}
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
	// D3: token-ceiling enforcement uses provider-REPORTED usage accumulated per
	// turn (never estimates — Constitution VI). Breaching the ceiling triggers the
	// same graceful wrap-up as turn exhaustion: one instruction to report what was
	// covered, then the next response is final and the result is marked partial.
	consumedTokens := 0
	ceilingHit := false
	// Feature 012: the wire pin and reasoning tier belong to the STREAM. A
	// continuation reuses the predecessor's exact pin (gateway model routing +
	// OpenRouter stickiness, R-F11) and — on EffortPinned families — inherits
	// its reasoning tier so top-level body params cannot cold the prefix (P3).
	streamPin := ":sub:" + streamSpec.Name
	reasoning := Profile(e.effort()).AgentReasoning
	if decision.Decision == linkContinued {
		if profile := gateway.ResolveModelProfile(modelID); profile.EffortPinned && decision.Predecessor.Reasoning != "" {
			reasoning = decision.Predecessor.Reasoning
		}
	}
	lastPromptTokens, lastCompletionTokens := 0, 0
	var firstTurnShare *float64
	firstTurnCacheReported := false
	maxTurns := max(2, int(math.Ceil(float64(spec.MaxTurns)*Profile(e.effort()).AgentTurnScale)))
	// Exhausting the turn budget is NOT a hard failure: the subagent is told to
	// stop calling tools and gets up to wrapUpTurns extra provider turns to
	// return whatever it found (verified or partial) as its report. Only a
	// subagent that returns nothing even then is reported as failed.
	const wrapUpTurns = 2
	wrapUp := false
	lastText := "" // last interim note, for the partial report if the budget is exhausted
	for turn := 1; turn <= maxTurns+wrapUpTurns; turn++ {
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
		requestStart := time.Now()
		// D4: per-KIND session pin (":sub:explore", ":sub:review", ...) so each
		// kind's distinct sidecar prefix keeps its own provider prefix-cache
		// identity warm instead of all kinds churning one shared ":sub" pin
		// (contracts/subagent-handoff.md §2). Feature 012: continuations ride the
		// predecessor's stream pin so provider routing and caches stay warm.
		response, err := e.provider.Chat(ctx, contract.ChatRequest{SessionID: e.session.ID + streamPin, Messages: messages, Tools: definitions, ModelID: modelID, Reasoning: reasoning})
		if usageErr := e.recordUsageAndEmit(func() error {
			return e.recordIsolatedUsage(ctx, modelID, streamPin, response.Usage, previousShape == nil, reasons, elapsedMS(requestStart))
		}); usageErr != nil {
			result.Status = "failed"
			result.Report = "persist subagent usage: " + usageErr.Error()
			break
		}
		if turn == 1 {
			// CL-4 verification: the continuation's first response carries the
			// provider-reported cache truth for the replayed prefix.
			firstTurnShare = pairedCacheShare(response.Usage)
			firstTurnCacheReported = firstTurnShare != nil
		}
		if response.Usage.PromptTokens > 0 || response.Usage.CompletionTokens > 0 {
			lastPromptTokens, lastCompletionTokens = response.Usage.PromptTokens, response.Usage.CompletionTokens
		}
		// Keep the parent's live task-usage display in step with subagent
		// spending — the task summary already includes it, so the live line
		// must too.
		previousShape = &shape
		if err != nil {
			// Feature 014: subagent reports surface the shared friendly mapping,
			// never a raw transport error ("Post .../chat/completions: context
			// canceled" was a live user-visible incident).
			result.Status = statusFromContext(ctx, "failed")
			result.Report = gateway.FriendlyRequestError(err)
			break
		}
		result.Usage = result.Usage.Add(response.Usage)
		e.emitAgent(contract.AgentEvent{Kind: "usage", RunID: runID, Usage: result.Usage})
		turnTokens := response.Usage.TotalTokens
		if turnTokens == 0 {
			turnTokens = response.Usage.PromptTokens + response.Usage.CompletionTokens
		}
		consumedTokens += turnTokens
		calls := response.ToolCalls
		text := response.Content
		// During wrap-up the text IS the report — never let the rescue path
		// reinterpret it as tool calls.
		if len(calls) == 0 && e.rescue != nil && !wrapUp {
			calls, text = e.rescue(text, toolNames(definitions))
		}
		// A no-tool-call response is the report as always; in wrap-up any
		// non-empty text is also accepted as the report even if the model kept
		// calling tools alongside it.
		if strings.TrimSpace(text) != "" {
			lastText = text
		}
		if len(calls) == 0 || (wrapUp && strings.TrimSpace(text) != "") {
			result.Report = strings.TrimSpace(text)
			if result.Report == "" {
				result.Report = "(subagent returned no report)"
				if wrapUp {
					// INV-3: a turn-budget exhaustion is a bounded degradation, NEVER a
					// hard failure with a scary "bounded turn limit" string. Return a
					// guided partial so the parent absorbs the unfinished work.
					result.Status = "failed"
					result.Report = turnBudgetPartialReport(lastText)
					e.recordHarnessEvent(ctx, contract.HarnessRecovery, "subagent-turn-budget", input.Agent)
				}
			}
			break
		}
		messages = append(messages, assistantReplayMessage(response, text, calls))
		if strings.TrimSpace(text) != "" {
			e.emitAgent(contract.AgentEvent{Kind: "text", RunID: runID, Content: text})
		}
		if wrapUp {
			// The wrap-up prompt forbids further tool work: pair each call with
			// a synthetic refusal (keeping the transcript well-formed for the
			// next turn) instead of executing tools past the budget.
			for _, call := range calls {
				messages = append(messages, contract.Message{Role: contract.RoleTool, ToolCallID: call.ID, Content: "Turn budget exhausted — tool not executed. Return your complete report now."})
			}
		} else {
			for _, call := range calls {
				result.ToolCalls++
				// B6/T025: one shared gate. Plan-mode blocking (P3 defense in depth),
				// H1 validation, the failed-cache, the repeat limiter, the storm
				// breaker, and H7 read-only-shell invalidation all live in gatedExecute
				// / the sub scope now, so this loop only dispatches and pairs the result.
				outcome := e.gatedExecute(ctx, call, definitions, Profile(e.effort()), sub)
				messages = append(messages, contract.Message{Role: contract.RoleTool, ToolCallID: call.ID, Content: outcome.Output})
			}
		}
		if turn == maxTurns+wrapUpTurns {
			// Both wrap-up turns burned on tool calls with no text (INV-3): return a
			// guided partial, never the forbidden "bounded turn limit" string.
			result.Status = "failed"
			result.Report = turnBudgetPartialReport(lastText)
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "subagent-turn-budget", input.Agent)
			break
		}
		if !wrapUp && input.TokenCeiling > 0 && consumedTokens >= input.TokenCeiling {
			// Ceiling breach → graceful wrap-up, never silent truncation or
			// overspend: the subagent reports what it covered and what remains.
			wrapUp = true
			ceilingHit = true
			messages = append(messages, contract.Message{Role: contract.RoleUser, Content: "Token budget reached: stop calling tools and return your report NOW — state exactly what you covered and what you did NOT get to."})
		}
		if !wrapUp && turn >= maxTurns {
			wrapUp = true
			messages = append(messages, contract.Message{Role: contract.RoleUser, Content: "Turn budget reached: stop calling tools and return your complete report NOW with everything you found, verified or partial."})
		}
	}
	if ceilingHit && result.Status == "done" {
		// A ceiling-bounded run that still reported is a PARTIAL result, labeled
		// so the parent (and the benchmark record's ceiling_hit flag) never
		// mistake bounded coverage for complete coverage.
		result.Status = "partial (token ceiling)"
	}
	// Feature 012 R-D4: stamp the terminal shape, then bank the run's context
	// record (any shape — non-linkable shapes still feed the digest fallback
	// and diagnostics) and its link-ledger outcome (FR-003/FR-015).
	result.TerminalShape = terminalShapeFor(result.Status, wrapUp, ceilingHit)
	if e.contextLinkingEnabled() {
		record := &SubagentContextRecord{
			RunID: runID, Kind: streamSpec.Name, ModelID: modelID,
			Pin:               e.session.ID + streamPin,
			Transcript:        messages,
			TerminalShape:     result.TerminalShape,
			Reasoning:         reasoning,
			FinalPromptTokens: lastPromptTokens, FinalCompletionTokens: lastCompletionTokens,
			Result: result.Report,
		}
		if previousShape != nil {
			record.SystemHash, record.ToolsHash = previousShape.SystemHash, previousShape.ToolsHash
		}
		e.taskMu.Lock()
		record.TaskLineage = e.taskSeq
		e.taskMu.Unlock()
		e.finalizeAgentRecord(ctx, record, capture)
		outcome := contract.LinkOutcome{
			RunID: runID, Kind: input.Agent,
			Decision: decision.Decision, Form: decision.Form, Reason: decision.Reason,
			CacheShare: firstTurnShare, CacheReported: firstTurnCacheReported,
			InheritedFiles: decision.Inherited, RereadFiles: len(decision.Reread),
			OutboundChars: len(handoff.Render()) + len(input.Task),
			ReturnChars:   len(result.Report),
		}
		if decision.Predecessor != nil {
			outcome.Predecessor = decision.Predecessor.RunID
		}
		e.recordLinkOutcome(outcome)
		if line := linkNoticeLine(outcome); line != "" {
			e.callbacks.EmitNotice(line)
		}
		if decision.Decision == linkContinued && firstTurnShare != nil && *firstTurnShare < 0.20 {
			// CL-4 honesty: a continuation whose replayed prefix missed is
			// surfaced, never displayed as a silent win (US3-AS2).
			e.callbacks.EmitNotice("linked but cold (provider cache miss) — the predecessor's cache likely expired or routing changed; this run paid a cold write.")
		}
	}
	e.emitAgent(contract.AgentEvent{Kind: "done", RunID: runID, Agent: input.Agent, Role: role, Title: input.Title, Status: result.Status, Report: result.Report, Usage: result.Usage})
	return result
}

func (e *Engine) emitAgent(event contract.AgentEvent) {
	if e.callbacks.Agent != nil {
		e.callbacks.Agent(event)
	}
}

// turnBudgetPartialReport is the INV-3-compliant report for a subagent that
// exhausted its turn budget: a guided partial that tells the parent to finish the
// work directly. It NEVER contains the forbidden "bounded turn limit" phrasing.
func turnBudgetPartialReport(lastText string) string {
	base := "Subagent exhausted its turn budget before finishing; returning partial progress. The remaining work is unfinished — continue it directly."
	if s := strings.TrimSpace(lastText); s != "" {
		return base + " Last interim note: " + contract.Digest(s, 300)
	}
	return base
}

func deriveTitle(task string) string {
	line := strings.TrimSpace(strings.SplitN(task, "\n", 2)[0])
	line = strings.Join(strings.Fields(strings.Trim(line, "#>*`-")), " ")
	return contract.TruncateEllipsis(line, 56)
}

func statusFromContext(ctx context.Context, fallback string) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return fallback
}

func isMutation(name string) bool {
	// save_memory and edit_memory mutate the durable memory store (feature 008
	// US5, Memory Parity N2), so they take the same plan-mode read-only block as
	// the file edits they parallel (memory-tool.md MT-9 — no gate weaker than
	// edit_file's). recall_memory is read-only and stays open.
	return name == "edit_file" || name == "multi_edit" || name == "write_file" || name == "apply_patch" || name == "run_shell" || name == "save_memory" || name == "edit_memory" || strings.HasPrefix(name, "mcp__")
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
	return instructions.CapabilityStatementPrefix + strings.Join(names, ", ") + instructions.CapabilityStatementSuffix
}
