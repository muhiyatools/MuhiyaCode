package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type wireRequestNormalizer interface {
	StableRequestMessages(contract.ChatRequest) ([]contract.Message, error)
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
	// T013: snapshot the harness-event ring length so the summary marks only the
	// friction THIS task produced.
	harnessStart := e.harnessEventRingLen()
	e.taskMu.Lock()
	// The live-usage baseline is captured in the same locked section so every
	// request this task makes — including onboarding, which runs before the
	// main per-task counters reset below — lands in the emitted task delta.
	e.taskUsageStart = e.sessionUsage
	e.taskMu.Unlock()
	eventStart := len(e.InvalidationEvents())
	profile := Profile(e.effort())
	assessment := Classify(userPrompt, e.previous)
	e.previous = assessment.Class
	verdict := NeedsPlan(assessment)
	l := e.Lifecycle()
	// P2: a natural-language "discard the plan" while a plan is pending/interrupted
	// drops it and stops — the replacement for /plan clear. Checked before the proceed
	// route so "forget the plan" never executes it.
	if l.State.InvitesProceed() && planDiscardRE.MatchString(userPrompt) {
		e.DiscardPlan()
		e.callbacks.EmitNotice("Plan discarded.")
		return e.finalize(ctx, "Discarded the saved plan. Tell me what you'd like to do next."), stats, nil
	}
	proceed := continuationRE.MatchString(userPrompt) || planProceedRE.MatchString(userPrompt)
	// Does this task resume the existing lifecycle rather than re-route? An
	// orchestrated pipeline mid-flight (research/planning/implementing/validating)
	// always resumes; a plan-mode execution resumes too; a plan awaiting approval
	// or saved pending/interrupted resumes only on an explicit proceed/continuation.
	resume := false
	switch {
	case l.Orchestrated() && l.State.IsPipelineResumable():
		resume = true
	case l.State == contract.LifecycleImplementing || l.State == contract.LifecycleValidating:
		resume = true
	case l.State.IsApprovalPause():
		resume = proceed
	case l.State.InvitesProceed():
		resume = proceed
	}
	switch {
	case resume:
		// The existing lifecycle drives this task; no re-routing.
	case verdict.NeedsPlan:
		e.beginPipeline(ctx, verdict)
	case !l.Orchestrated() || l.State == contract.LifecycleFinished:
		e.recordDirectVerdict(ctx, verdict)
	case l.State.IsApprovalPause():
		// An unrelated direct task arrived while an orchestrated plan sits at the
		// approval pause. Park it: the saved plan stays pending and resumable with
		// "proceed" while this task runs direct — an abandoned approval gate must
		// never inject its gate text into, or block the edits of, unrelated work.
		e.parkPipelineForDirectTask(ctx, verdict)
	}
	budget := BudgetFor(assessment, profile)
	// Plan-mode floor: the plan block advertises explore/plan/review subagents,
	// so a plan-mode task must never carry agents=0 — the model would be told
	// to delegate and then denied ("budget exhausted (0 run(s))"). One run is
	// always available for delegated investigation; the brief is rebuilt so the
	// advertised budget matches the enforced cap.
	if e.PlanMode() || e.pipelineActive() {
		budget = budget.WithAgentFloor(1, assessment)
	}
	modelPrompt := userPrompt
	if profile.Onboarding && ShouldConsiderOnboarding(userPrompt) && e.callbacks.Ask != nil {
		e.callbacks.EmitStatus("Clarifying the task...")
		// Onboarding uses the SubagentModelID and shares the sub stream's gateway
		// routing pin (C1). sessionPinSub is computed later in the same Run; for
		// clarity we re-derive the same identity here (it must stay identical to
		// the one main-loop uses).
		onboardingStart := time.Now()
		questions, usage := GenerateOnboardingQuestions(ctx, e.provider, e.settings.Provider.SubagentModelID, userPrompt, e.session.ID+":sub")
		if err := e.recordUsageAndEmit(func() error {
			return e.recordAuxUsage(ctx, e.settings.Provider.SubagentModelID, usage, elapsedMS(onboardingStart))
		}); err != nil {
			return "", stats, fmt.Errorf("persist onboarding usage: %w", err)
		}
		if len(questions) > 0 {
			if answers, err := e.callbacks.Ask(ctx, questions); err == nil {
				modelPrompt = PromptWithAnswers(userPrompt, answers)
			}
		}
	}
	e.taskMu.Lock()
	e.taskAgentUsage = contract.Usage{}
	e.taskAgentRuns, e.taskAgentReused, e.taskDuplicates, e.taskOverBudget = 0, 0, 0, 0
	e.taskAgentDenied = 0
	e.taskOversizedPlanRejected = false
	e.taskAgentCap = budget.MaxAgentRuns
	e.taskPhaseAgentRuns = make(map[contract.LifecycleState]int)
	e.taskPeakContext = 0
	e.taskCounters = newCallCounters()
	e.taskFailures = nil // H5: reset the per-task failure window
	e.taskMu.Unlock()
	// G5: goal autonomous-continuation budget is per task, not per goal.
	e.ResetGoalTaskCounter()
	if err := e.applyBoundaryToolChange(ctx); err != nil {
		return "", stats, err
	}
	filesChanged := make(map[string]bool)
	toolCalls, checksRun, turns, folded := 0, 0, 0, 0
	taskLinesAdded, taskLinesRemoved := 0, 0 // UD-6: Σ diff adds/removes of applied file-changing calls this task
	doneCriteria := ""
	planReady := false    // P2: set when a plan-mode task ends via exit_plan_mode or a free-text plan finish
	terminateReason := "" // H5: reason a task was force-finalized (token breaker / failure terminator), surfaced to the TUI as a warn
	defer func() {
		// Computed before the taskMu lock below — harnessEventsSince takes taskMu,
		// and sync.Mutex is not reentrant.
		_, harnessVisible := e.harnessEventsSince(harnessStart)
		e.taskMu.Lock()
		stats = contract.TaskStats{DurationMS: time.Since(started).Milliseconds(), Effort: profile.Level, TaskClass: string(assessment.Class), Usage: subtractUsage(e.sessionUsage, usageStart), AgentUsage: e.taskAgentUsage, PeakContextPercent: e.taskPeakContext, ToolCalls: toolCalls, AgentRuns: e.taskAgentRuns, AgentRunsReused: e.taskAgentReused, Turns: turns, ChecksRun: checksRun, FoldedTokens: folded, DisciplineScore: max(0, 100-min(24, e.taskDuplicates*8)-min(16, e.taskOverBudget*2)), DoneCriteria: doneCriteria, PlanReady: planReady, TerminatedReason: terminateReason, LinesAdded: taskLinesAdded, LinesRemoved: taskLinesRemoved, HarnessEvents: harnessVisible}
		// UD-6/UD-9 (feature 008): session accumulators for the usage panel —
		// active time and lines± are session-scoped (reset on resume, labeled
		// "this session"); API time derives from persisted record durations.
		e.sessionActiveMS += stats.DurationMS
		e.sessionLinesAdded += taskLinesAdded
		e.sessionLinesRemoved += taskLinesRemoved
		// T043: attach the cumulative session hit-rate (provider-fields-only
		// denominator) so the persistent footer can show session hit-rate, not
		// just this task's cache tag. Computed under taskMu with usageRecords.
		stats.SessionHitRate = contract.AggregateUsage(e.usageRecords).SessionHitRate
		// The credits figure is the FULL SESSION cost — every main-loop, subagent,
		// and auxiliary request the gateway priced (user directive: the end-of-task
		// total is the whole session, not just this task; the token figure above
		// stays per-task via stats.Usage). Member-set honesty still holds: a single
		// unpriced eligible request collapses the sum to "unavailable" rather than
		// under-reporting a partial total.
		sessionCredits := contract.SumCreditsUSD(e.usageRecords)
		stats.CreditsUSD = sessionCredits.USD
		stats.CreditsEstimated = sessionCredits.Estimated
		e.taskMu.Unlock()
		// 003 (FR-013a): mark an interrupted task so the summary can show it.
		// H5 TerminatedReason keeps its own notice and does not set StopCause.
		// ctx (not parent) is what user-stop cancels via e.Cancel.
		switch {
		case ctx.Err() != nil || (runErr != nil && errors.Is(runErr, context.Canceled)):
			stats.StopCause = contract.StopCauseUserStop
		case runErr != nil:
			stats.StopCause = contract.StopCauseError
		}
		// 004 US2 (T10/T11): stamp the plan lifecycle at every task exit. An
		// executing plan resolves to finished (all steps done) or interrupted (any
		// step still open, resumable). Pre-execution and terminal phases are
		// untouched, so the plan-ready finalize paths and non-plan tasks never
		// misfire. Placed here so user-stop, error, and breaker exits stamp too.
		e.stampPlanCompletionPhase()
		allEvents := e.InvalidationEvents()
		if eventStart < len(allEvents) {
			stats.Invalidations = append([]contract.InvalidationEvent(nil), allEvents[eventStart:]...)
		}
		for file := range filesChanged {
			stats.FilesChanged = append(stats.FilesChanged, file)
		}
		sort.Strings(stats.FilesChanged)
		if e.callbacks.TaskComplete != nil {
			e.callbacks.TaskComplete(stats)
		}
	}()

	if err := e.persistMessage(ctx, "user", "message", userPrompt, "", map[string]any{"role": "user", "content": userPrompt, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return "", stats, err
	}
	// Establish the boundary before maintenance so every completed prior task
	// is eligible. Folding and trimming then commit as one rewrite/event.
	e.history.MarkTaskStart()
	// C7: on a long-idle resume of a large session, prune before the first request —
	// the server cache is already gone, so this is free and cuts the cold re-read.
	if err := e.staleResumePruneIfNeeded(ctx, profile); err != nil {
		return "", stats, err
	}
	maintenance, err := e.runMaintenanceBoundary(ctx, profile)
	if err != nil {
		return "", stats, err
	}
	folded = maintenance.FoldedTokens
	brief := budget.Brief
	// P3(a): the user pointed at an existing plan document — teach the model to read
	// it, mirror it into the to-dos, and execute it (no re-planning). Inline dynamic
	// tail block (like the goal/breaker nudges), so it never touches the cached prefix.
	if assessment.PlanDoc {
		brief = planDocBlock(assessment.PlanDocPath) + "\n" + brief
	}
	executingPlanText := ""
	// P2 step 4: a bare "proceed" (or any continuationRE match) against a saved
	// pending plan executes it. Inject the persisted plan content into the task
	// brief (cache-safe — it rides the user-message tail) and clear the flag so
	// the plan executes exactly once. The plan content is the in-memory plan
	// (kept in sync with plan.md via update_plan/WritePlan).
	// 004 US2 (T7): resume a saved plan when the user gives the go-ahead. Fires
	// for a pending or interrupted plan (the interrupted case resumes a partially
	// executed plan) and matches both the bare continuation tokens and the wider
	// "proceed with the plan" phrasing. Step-progress detection (T8) in
	// updatePlan is the phrasing-independent backstop for anything this misses.
	if e.LifecycleState().InvitesProceed() {
		if continuationRE.MatchString(userPrompt) || planProceedRE.MatchString(userPrompt) {
			e.beginExecution(ctx)
			planText := e.executionPlanMarkdown(e.CurrentPlan())
			if strings.TrimSpace(planText) != "" {
				executingPlanText = planText
			}
		}
	}
	if pipelinePrelude, pipelineErr := e.preparePipelinePhase(ctx, userPrompt, budget, profile); pipelineErr != nil {
		return "", stats, pipelineErr
	} else if strings.TrimSpace(pipelinePrelude) != "" {
		brief = pipelinePrelude + "\n" + brief
	}
	// The full pipeline executes approved steps and validation inside the
	// harness before the main model is called. Never also inject the legacy
	// "executing saved plan" payload after that work reached done: doing so asks
	// the main agent to repeat completed edits and checks. Light pipelines and
	// degraded full runs still receive the plan for direct execution/recovery.
	pipelineAfterPrepare := e.Lifecycle()
	if executingPlanText != "" && (pipelineAfterPrepare.Depth != PipelineDepthFull || pipelineAfterPrepare.State != contract.LifecycleFinished) {
		brief = "[executing saved plan]\n" + executingPlanText + "\n" + brief
	}
	plan := e.planBlock()
	goal := e.goalBlock()
	pipeline := e.pipelineBlock()
	// C1/T030 + DG2: defensive backstop for the plan/pipeline ⇄ goal exclusion (G3).
	// The setters keep at most one active, but if a plan OR an orchestration-pipeline
	// block is somehow live alongside a goal, their instructions contradict (plan/
	// pipeline: follow the current phase gate, ask to proceed; goal: work
	// autonomously, end with a marker). The plan/pipeline is the more restrictive
	// mode, so it wins and the goal block is dropped — this also stops a goal from
	// silently self-blocking on missing markers mid-pipeline.
	if (plan != "" || pipeline != "") && goal != "" {
		log.Printf("[mode] plan/pipeline and goal both active during brief assembly — dropping goal block")
		goal = ""
	}
	if plan != "" {
		brief = plan + "\n" + brief
	}
	if pipeline != "" {
		brief = pipeline + "\n" + brief
	}
	if goal != "" {
		brief = goal + "\n" + brief
	}
	// 005 US3 / 006: append one-shot project-context updates to the newest user tail
	// in canonical order (instructions-update, then memory-update, above the goal/
	// plan blocks and below the user's text). Baked into the message BEFORE first
	// transmission and frozen verbatim into history, so it is ordinary append-only
	// tail growth — never a settled-prefix mutation, and no invalidation is owed.
	// Both files are gated on their content HASH, so an unchanged (even empty)
	// MUHIYA.md/MEMORY.md never injects a block; an edit — by the agent's own file
	// tools or by the user — surfaces exactly once, then folds into the next
	// session's cached prefix at no per-turn cost.
	if e.projectContextProbe != nil {
		if probe, probeErr := e.projectContextProbe(ctx); probeErr == nil {
			if probe.MemoryHash != e.appliedMemoryHash {
				if block := RenderMemoryUpdate(probe.MemoryHash, e.appliedMemoryHash, probe.MemoryContent); block != "" {
					brief = block + "\n" + brief
				}
				e.appliedMemoryHash = probe.MemoryHash
			}
			if probe.InstructionsHash != e.appliedInstructionsHash {
				brief = RenderInstructionsUpdate(probe.InstructionsHash, e.appliedInstructionsHash, probe.InstructionsContent) + "\n" + brief
				e.appliedInstructionsHash = probe.InstructionsHash
			}
		}
	}
	e.history.Append(contract.Message{Role: contract.RoleUser, Content: modelPrompt + "\n\n" + brief})
	e.persistProjectCursor(ctx)
	e.emitContext(0)
	// Per-stream session pin for the main loop (C1). The subagent / compact /
	// onboarding paths each derive their own identical suffix locally so the
	// gateway-side routing pin never drifts within a session.
	sessionPinMain := e.session.ID + ":main"

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
	// UD-10/T025 (feature 008): record the component byte sizes behind the
	// /context usage-by-category estimate. Measured once per task from values
	// already in hand — read-side (contextCategories) never recomputes them.
	e.recordAssemblySizes(promptText, promptContext, definitions)

	currentClass := assessment.Class
	turnCap := budget.MaxTurns
	escalated, convergeNoted, finalNoted := false, false, false
	sawToolCall, emptyFinalRetries, planContinues, consecutiveFailures := false, 0, 0, 0
	intentFinalRetries := 0   // FR-004b: bounded retries when a turn narrates an action without calling a tool
	autoReviewNudged := false // DG-7: the max-effort review nudge fires at most once per task
	allFailedTurnStreak := 0  // B7: consecutive turns where EVERY tool call failed (reset at Run start via this local)
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
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Final step: do not call tools. Give the complete factual final answer now: outcome, verification, and genuine remaining work."})
		} else if !isFinal && turns >= turnCap-2 && !convergeNoted {
			convergeNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Two steps remain. Batch only essential edits, verify, and finish."})
		}
		// T039 ladder: soft advisory band [0.50, 0.60) mutates NOTHING — it just
		// warns once per compaction cycle that context is filling (Reasonix
		// compact.go:94-99). The reclaim band [0.60, 0.80) is handled by the
		// task-boundary runMaintenanceBoundary (A1). Here in the turn loop we only
		// act at the compaction threshold.
		if pr := e.contextPressure(); pr.Ratio >= softNoticeRatio && pr.Ratio < maintenanceFloorRatio {
			e.taskMu.Lock()
			shown := e.softNoticeShown
			e.softNoticeShown = true
			e.taskMu.Unlock()
			if !shown {
				e.callbacks.EmitStatus("Context is filling; earlier turns will be summarized when needed.")
			}
		}
		if e.needsCompact(live) {
			pressure := e.contextPressure()
			// T039 prune-before-compact: below the force ratio, try reclamation
			// first; if folding/trimming clears the compaction trigger, skip the
			// paid structured-summary compaction entirely. At/above the force
			// ratio (0.90) we compact regardless.
			skipCompact := false
			if pressure.Ratio < compactForceRatio {
				if _, err := e.runMaintenanceBoundary(ctx, profile); err != nil {
					return "", stats, err
				}
				// Reclamation shrank history, but the last real provider token count
				// is now stale (it reflects the pre-reclamation request), so
				// needsCompact would still see the old value and never skip. Decide
				// on the POST-reclamation estimate instead: if reclamation dropped
				// the estimated pressure below the compaction threshold, skip the
				// paid summarization (one rewrite instead of two). A slight
				// under-estimate here only risks skipping one turn early; the next
				// turn's real token count re-triggers compaction if still needed.
				usable := max(8000, e.contextLimit()-outputReserveTokens)
				postRatio := float64(e.history.EstimatedTokens()) / float64(usable)
				if postRatio < profile.CompactThreshold {
					skipCompact = true
				}
			}
			if !skipCompact {
				e.callbacks.EmitStatus("Compacting context...")
				e.callbacks.EmitNotice("Context is nearly full — compacting the conversation…")
				pressure = e.contextPressure()
				beforeCompact := e.history.EstimatedTokens()
				if err := e.compact(ctx, "automatic: context nearly full"); err != nil {
					// C5: a tail-placed user message with a bracketed marker. Same
					// cache-safe behavior as before (no mid-history edit), but it
					// becomes a user turn instead of a system turn so providers do
					// not need to special-case it.
					e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Automatic compaction failed; continue using the bounded recent context."})
					e.callbacks.EmitNotice("Compaction failed — continuing with the recent context.")
				} else if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
					Cause: contract.InvalidationCompact, Trigger: contract.InvalidationPressure,
					Scope: "automatic structured-summary compaction", Pressure: floatPointer(pressure.Ratio),
					RequestSeq: e.nextRequestSeq(),
				}); err != nil {
					return "", stats, err
				} else {
					e.resetMaintenanceLatch()
					e.callbacks.EmitNotice("Compaction complete — " + describeFreed(beforeCompact, e.history.EstimatedTokens(), e.contextLimit()))
				}
			}
		}
		// Trim aged tool payloads only when the window is genuinely filling.
		// Trimming rewrites history and busts the prefix cache, so it must not
		// run on every turn — only once pressure crosses the threshold.
		e.callbacks.EmitStatus("Thinking...")
		built := e.history.BuildRequestWithMetadata(promptText, e.contextLimit(), outputReserveTokens)
		if built.WindowDropped {
			pressure := e.contextPressure()
			trigger := contract.InvalidationPressure
			if pressure.Ratio < maintenanceFloorRatio {
				trigger = contract.InvalidationBoundary
			}
			if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
				Cause: contract.InvalidationWindowDrop, Trigger: trigger,
				Scope:    fmt.Sprintf("request window dropped %d previously transmitted unit(s)", built.DroppedUnits),
				Pressure: floatPointer(pressure.Ratio), RequestSeq: e.nextRequestSeq(),
			}); err != nil {
				return "", stats, err
			}
		}
		messages := built.Messages
		// Feature 006: project memory is a file the agent edits with its ordinary
		// tools, so answers no longer carry a <project-memory> trailer to hide — live
		// tokens stream straight through to display.
		request := contract.ChatRequest{SessionID: sessionPinMain, Messages: messages, Tools: definitions, ModelID: e.settings.Provider.ActiveModelID, ToolChoice: "auto", Reasoning: ReasoningForEffort(e.effort()), OnToken: e.callbacks.Token, OnReasoningToken: e.callbacks.ReasoningToken}
		shapeRequest := request
		if normalizer, ok := e.provider.(wireRequestNormalizer); ok {
			normalizedMessages, normalizeErr := normalizer.StableRequestMessages(request)
			if normalizeErr != nil {
				return "", stats, fmt.Errorf("normalize request for prefix shape: %w", normalizeErr)
			}
			shapeRequest.Messages = normalizedMessages
		} else {
			// C2: do not silently hash pre-replay bytes — that would let a real
			// settled-byte change pass undetected by the prefix-shape guard. Mark
			// the usage record so the operator/benchmark notices, log once per
			// session, and continue (the unknown-normalizer provider may still be
			// correct in practice).
			e.recordDegradedPrefixGuard(ctx, "no wireRequestNormalizer on provider")
		}
		shape, err := NewWirePrefixShape(shapeRequest, e.lastSentMessageCount, e.history.RewriteVersion())
		if err != nil {
			return "", stats, fmt.Errorf("compute request prefix shape: %w", err)
		}
		// C3: on the FIRST request of a resumed session, attribute any system/tools/
		// model change against the prior session's persisted shape, so a resume cold
		// start is never silent (fixes the turn-1-blind guard, DC2).
		e.checkResumeDrift(ctx, shape)
		var changeReasons []string
		if e.lastShape != nil {
			changeReasons = CompareShape(*e.lastShape, shape)
		}
		if len(changeReasons) > 0 && len(e.invalidations.EventsForRequest(e.nextRequestSeq())) == 0 {
			detail := strings.Join(changeReasons, ", ")
			return "", stats, fmt.Errorf("stable request prefix changed without an invalidation event: %s", detail)
		}
		// Reasoning effort is the user's chosen level, sent raw; the gateway
		// maps it onto each provider's supported thinking ladder. It is a
		// request parameter, not a message, so it never affects the prefix cache.
		requestStart := time.Now()
		response, err := e.provider.Chat(ctx, request)
		if err != nil {
			// Provider errors reach here only after the gateway's own retries are
			// exhausted; record the friction (T011). The error string already
			// carries the HTTP status; T051 renders it into user guidance.
			e.recordHarnessEvent(ctx, contract.HarnessProvider, "chat-error", err.Error())
			return "", stats, err
		}
		observation := mainUsageObservation{model: e.settings.Provider.ActiveModelID, usage: response.Usage, changeReasons: changeReasons, messageCount: len(messages), durationMS: elapsedMS(requestStart)}
		if err := e.recordUsageAndEmit(func() error { return e.recordMainUsage(ctx, observation) }); err != nil {
			return "", stats, fmt.Errorf("persist request usage: %w", err)
		}
		e.emitColdStartNoticeIfPending(ctx) // C6: honest resume cold-start notice, off the usage lock
		e.lastShape = &shape
		e.lastSentMessageCount = len(messages)
		e.persistPrefixShapeOnce(ctx, shape) // C3: baseline for the next resume's drift check
		// Emit the task-cumulative usage (all streams), not this response's
		// single-request usage: the live tokens/cache tag must describe the
		// whole task so far, matching the end-of-task summary's semantics.
		e.emitContext(response.Usage.PromptTokens)
		// T038: calibrate the token estimator from this real provider usage so the
		// pressure estimate (used when provider tokens are unavailable, e.g. the
		// bootstrap turn) tracks the actual tokenizer rather than a fixed 0.25
		// chars/token guess. promptText is the system prompt just sent; its chars
		// count toward promptTokens but are not stored in the message log.
		if response.Usage.PromptTokensAvailable {
			e.history.Calibrate(response.Usage.PromptTokens, len(promptText))
		}
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
			// G1/T020: scan the goal marker on the FINAL governed turn too. Without
			// this, a [goal:complete] / [goal:blocked] emitted on the last allowed
			// turn is dropped and the goal wrongly stays active into the next task.
			if e.scanGoalMarker(assistantText) {
				e.clearGoalSidecar()
			}
			// P2 belt-and-suspenders: a plan-mode task that hit the turn cap
			// with a plan in place is also plan-ready (the model may have
			// free-text asked to proceed instead of calling exit_plan_mode).
			e.maybeSignalPlanReady(&planReady)
			if e.pipelineActive() && e.LifecycleState() != contract.LifecycleFinished && !planReady {
				e.SetLifecycleState(contract.LifecycleInterrupted)
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
			// Feature 008 follow-up (FR-004b, the "Let me fix:" stop): DeepSeek
			// sometimes ends a mid-task turn NARRATING its next action ("Let me
			// fix:", "Now update the CSS:") without emitting the tool call. Before
			// this guard, that narration was accepted as the final answer and the
			// task silently finalized mid-thought. Mirror the empty-final retry:
			// bounded, tail-only, mid-work only (sawToolCall), so a genuine final
			// summary that happens to end in a colon can still land on the retry
			// exhausting.
			if sawToolCall && trailingIntent(trimmed) && intentFinalRetries < 2 {
				intentFinalRetries++
				_ = e.persistAssistant(ctx, trimmed)
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[continue] You announced the next action but made no tool call. Make that tool call now, or give the final result instead."})
				continue
			}
			// Fault-injection finding (010 US2/FI-13): this nudge must only fire
			// while a plan is actually being EXECUTED (LifecycleImplementing) —
			// mirroring the LifecycleValidating scope its sibling check uses
			// immediately below. Before this guard it fired for ANY non-empty
			// e.plan.Steps regardless of lifecycle state, so a plan that was
			// discarded (DiscardPlan clears lifecycle.State/Depth but not
			// e.plan) or parked for an unrelated task (parkPipelineForDirectTask,
			// same) left stale pending steps that hijacked every later plain-text
			// reply with "finish your incomplete plan" for up to maxPlanContinues
			// turns — live incidents this pinned catalog row now guards.
			if l := e.Lifecycle(); l.State == contract.LifecycleImplementing && e.hasIncompletePlan() && planContinues < maxPlanContinues {
				planContinues++
				turnCap = min(hardTurnCeiling, max(turnCap, turns+8))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[continue] Your plan has incomplete steps. Do not ask whether to continue; finish them now, or mark completed steps and give the final summary."})
				continue
			}
			if l := e.Lifecycle(); l.State == contract.LifecycleValidating && !l.Validated && planContinues < maxPlanContinues {
				planContinues++
				turnCap = min(hardTurnCeiling, max(turnCap, turns+6))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[validation gate] Completion is blocked until validation is confirmed. Run the plan's verification command(s), address any findings, and keep the plan complete."})
				continue
			}
			// An active goal keeps the agent working autonomously until it emits
			// a completion or blocked marker (or hits the auto-turn cap).
			// G1/G5: also scan the marker here so a [goal:complete] / blocked
			// marker on the final assistant text terminates the task even when
			// the auto-continue logic below would otherwise force a continue.
			if e.scanGoalMarker(trimmed) {
				// G4: a terminal marker cleared the active goal — drop the sidecar
				// so it does not resurrect on resume.
				e.clearGoalSidecar()
			}
			if next := e.advanceGoal(trimmed, false); next != "" {
				turnCap = min(hardTurnCeiling, max(turnCap, turns+8))
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: next})
				continue
			}
			// G4: advanceGoal returns "" when the goal ended via the idle/cap
			// guards too — drop the sidecar before finalizing so the finished
			// goal does not resurrect. Idempotent with the scanGoalMarker clear.
			e.clearGoalSidecar()
			// DG-7 (feature 008): AutoReview — at max effort, when substantial
			// file-changing work is about to finalize with agent allowance left,
			// nudge ONCE for an independent review pass. A dynamic tail rider
			// (never prefix), fired at most once per task, skipped in plan mode
			// (read-only) and when nothing meaningful changed.
			if profile.AutoReview && !autoReviewNudged && !e.PlanMode() && len(filesChanged) >= 2 && e.taskAgentRuns < e.taskAgentCap {
				autoReviewNudged = true
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[review] Before finishing: run one review subagent over the files you changed and fix only verified findings, then give the final answer."})
				continue
			}
			// P2 belt-and-suspenders: a plan-mode task that ended with free
			// text (no exit_plan_mode) and a plan in place is plan-ready.
			e.maybeSignalPlanReady(&planReady)
			return e.finalize(ctx, fallbackAnswer(trimmed, filesChanged)), stats, nil
		}

		sawToolCall = true
		// G1: also scan the goal marker on text accompanying tool calls. Without
		// this, [goal:complete] emitted in the same turn as a tool call is dropped.
		if e.scanGoalMarker(assistantText) {
			// G4: a terminal marker on a tool-call turn clears the sidecar too.
			e.clearGoalSidecar()
		} else {
			// B4: a turn that made tool calls is forward motion — reset the
			// consecutive-idle counter so it never accumulates across productive
			// turns.
			e.markGoalToolProgress()
		}
		if strings.TrimSpace(assistantText) != "" {
			if err := e.persistMessage(ctx, "assistant", "message", assistantText, "", map[string]any{"role": "assistant", "content": assistantText, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
		}
		e.history.Append(assistantReplayMessage(response, assistantText, calls))
		outcomes := e.executeBatch(ctx, calls, definitions, live)
		planExited := false
		for _, outcome := range outcomes {
			toolCalls++
			// P2: exit_plan_mode surfaces a sentinel so we can break out of the turn
			// loop without polluting the assistant text with a fake result. We still
			// record the (sentinel) outcome for context fidelity and finalize.
			if errors.Is(outcome.Err, contract.ErrPlanModeExited) {
				// T018/REV B2b: mark plan-ready but DO NOT early-return here. Every
				// announced tool_call in this batch must still receive a tool result
				// below, or the persisted history holds an assistant tool_calls turn
				// with fewer results than calls — a malformed sequence the provider
				// rejects (400) on the next request. The exit sentinel is not a real
				// failure; the finalize happens AFTER the loop appends every result.
				planExited = true
			} else if outcome.Failed {
				consecutiveFailures++
				// H5: record the failed call's turn in the sliding window for the
				// distinct-failure terminator. The windowed count (not the
				// reset-on-success consecutive counter) is what stops a model
				// alternating between distinct failing actions from looping to
				// the 120-turn ceiling.
				e.recordTaskFailure(turns)
			} else {
				consecutiveFailures = 0
			}
			if isCheckCall(outcome.Call) && !outcome.Failed {
				checksRun++
				l := e.Lifecycle()
				if (l.State == contract.LifecycleValidating && !e.hasIncompletePlan()) || (l.State == contract.LifecycleImplementing && l.Depth == PipelineDepthLight) {
					_ = e.ConfirmPipelineValidation(ctx, "successful main-loop verification command")
				}
			}
			trackChanged(outcome, filesChanged)
			// UD-6 (feature 008): accumulate lines± from applied (successful)
			// file-changing calls via the SAME shared diff parser the TUI rows use,
			// so the session panel and the per-row counts can never drift.
			if !outcome.Failed {
				if add, remove, ok := contract.DiffCounts(outcome.Output); ok {
					taskLinesAdded += add
					taskLinesRemoved += remove
				}
			}
			e.trackKnowledge(outcome)
			// Resume fidelity: persist the tool's display target (file/command/query)
			// derived from its input args — the same derivation the live TUI does —
			// so a reopened transcript row names WHAT the call acted on. Redacted
			// like every persisted field.
			toolTarget := e.redact(contract.ToolTarget(outcome.Call.ToolName(), []byte(outcome.Call.ArgumentsJSON())))
			if err := e.persistMessage(ctx, "tool", outcome.Call.ToolName(), outcome.Output, toolTarget, map[string]any{"role": "tool", "name": outcome.Call.ToolName(), "input": outcome.Call.ArgumentsJSON(), "output": outcome.Output, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
			e.history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: outcome.Call.ID, Content: outcome.Output})
		}
		if planExited {
			// All tool results for this batch are appended, so the assistant/tool
			// pairing is well-formed. P2: signal plan-ready so the TUI opens the
			// Proceed now / Proceed later / Keep planning modal. Interactive runs
			// (TaskComplete wired) leave plan mode ON for the modal; one-shot runs
			// resolve to proceed-later so a later "proceed" executes the saved plan.
			planReady = true
			e.SetLifecycleState(contract.LifecycleApproval) // 004 US2 (T2): plan ready for a decision
			if e.callbacks.TaskComplete == nil {
				e.SetLifecycleState(contract.LifecyclePending) // T6: one-shot resolves to proceed-later
				return e.finalize(ctx, "Plan ready — saved. Say 'proceed' (or 'go ahead') to execute it."), stats, nil
			}
			return e.finalize(ctx, "Plan ready — awaiting Proceed now / Proceed later / Keep planning."), stats, nil
		}
		// B7: all-failed-turns detector. A turn in which EVERY tool call failed is
		// a strong loop signal the per-call storm breaker can miss (e.g. the model
		// alternates between distinct failing tools, so no single (tool,error)
		// class reaches 3). Two consecutive all-failed turns inject one loop-guard
		// notice; any success resets the streak.
		if allFailed(outcomes) {
			allFailedTurnStreak++
			if allFailedTurnStreak == 2 {
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[loop guard] Every tool call has failed for two turns running. Stop repeating this approach; take a fundamentally different action, or give the final factual status and the genuine blocker."})
			}
		} else {
			allFailedTurnStreak = 0
		}
		if consecutiveFailures >= 3 {
			consecutiveFailures = 0
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "Several tools failed. Re-read their errors, change approach, and explain a genuine blocker if progress is impossible."})
		}
		// H5: distinct-failure terminator. N failed tool calls (any signatures)
		// within the last taskFailureWindow turns force-finalize instead of
		// nudging forever. The nudge-and-reset above could loop to the 120-turn
		// ceiling when a model alternated between distinct failing actions; this
		// windowed count is not reset by interspersed successes or the nudge.
		if failures := e.taskFailureWindowCount(turns); failures >= maxTaskFailures {
			terminateReason = fmt.Sprintf("repeated tool failures — %d failures in the last %d turns; stopping to report", failures, taskFailureWindow)
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:tool-failures", terminateReason)
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Stop retrying; give the final factual status and the genuine blocker."})
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
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

func assistantReplayMessage(response contract.ChatResponse, content string, calls []contract.ToolCall) contract.Message {
	message := contract.Message{Role: contract.RoleAssistant, Content: content, ToolCalls: calls}
	if strings.TrimSpace(response.Reasoning) != "" {
		reasoning := response.Reasoning
		message.ReasoningContent = &reasoning
	}
	if len(response.ReasoningDetails) > 0 {
		message.ReasoningDetails = append(json.RawMessage(nil), response.ReasoningDetails...)
	}
	return message
}
