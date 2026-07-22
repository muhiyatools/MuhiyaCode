package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	usageRecordStart := len(e.UsageRecords())
	harnessStart := e.harnessEventRingLen()
	e.taskMu.Lock()
	// request this task makes â€” including onboarding, which runs before the
	// main per-task counters reset below â€” lands in the emitted task delta.
	e.taskUsageStart = e.sessionUsage
	e.taskMu.Unlock()
	eventStart := len(e.InvalidationEvents())
	profile := Profile(e.effort())
	assessment := Classify(userPrompt, e.previous)
	e.previous = assessment.Class
	budget := BudgetFor(assessment, profile)
	authoritativePhase := phaseGraphAuthoritative(e.settings.TokenEconomyMode, assessment.Class)
	var phaseController *PhaseController
	if e.settings.TokenEconomyMode != "off" {
		phaseController = NewPhaseController(PhaseRequirements{Change: changeRE.MatchString(userPrompt), Verification: changeRE.MatchString(userPrompt)})
	}
	e.reconcileCatalog()
	e.runTaskAdvisor(ctx, userPrompt, e.workspaceSignal())
	if err := e.prepareTaskEpoch(ctx, userPrompt, assessment); err != nil {
		return "", stats, err
	}
	e.maybeAdviseFreshSession(assessment, userPrompt)
	modelPrompt, err := e.prepareTaskPrompt(ctx, userPrompt, profile, assessment)
	if err != nil {
		return "", stats, err
	}
	e.resetTaskState(budget)
	e.seedProvidedSkills(modelPrompt)
	if err := e.applyBoundaryToolChange(ctx); err != nil {
		return "", stats, err
	}
	filesChanged := make(map[string]bool)
	toolCalls, checksRun, turns, folded := 0, 0, 0, 0
	taskLinesAdded, taskLinesRemoved := 0, 0 // UD-6: Î£ diff adds/removes of applied file-changing calls this task
	doneCriteria := ""

	terminateReason := "" // H5: reason a task was force-finalized (token breaker / failure terminator), surfaced to the TUI as a warn
	defer func() {
		// Computed before the taskMu lock below â€” harnessEventsSince takes taskMu,
		_, harnessVisible := e.harnessEventsSince(harnessStart)
		allUsageRecords := e.UsageRecords()
		taskUsageRecords := []contract.UsageRecord(nil)
		if usageRecordStart <= len(allUsageRecords) {
			taskUsageRecords = allUsageRecords[usageRecordStart:]
		}
		e.taskMu.Lock()
		stats = contract.TaskStats{DurationMS: time.Since(started).Milliseconds(), Effort: profile.Level, TaskClass: string(assessment.Class), Usage: subtractUsage(e.sessionUsage, usageStart), Economy: economyStatsFromRecords(taskUsageRecords), PeakContextPercent: e.taskPeakContext, ToolCalls: toolCalls, Turns: turns, ChecksRun: checksRun, FoldedTokens: folded, DisciplineScore: max(0, 100-min(24, e.taskDuplicates*8)-min(16, e.taskOverBudget*2)), DoneCriteria: doneCriteria, TerminatedReason: terminateReason, LinesAdded: taskLinesAdded, LinesRemoved: taskLinesRemoved, HarnessEvents: harnessVisible}
		// UD-6/UD-9 (feature 008): session accumulators for the usage panel â€”
		// active time and linesÂ± are session-scoped (reset on resume, labeled
		e.sessionActiveMS += stats.DurationMS
		e.sessionLinesAdded += taskLinesAdded
		e.sessionLinesRemoved += taskLinesRemoved
		stats.SessionHitRate = contract.AggregateUsage(e.usageRecords).AllStreamHitRate
		stats.PerPairing = contract.PerPairingRates(e.usageRecords)
		// The credits figure is the FULL SESSION cost â€” every session turn and
		sessionCredits := contract.SumCreditsUSD(e.usageRecords)
		stats.CreditsUSD = sessionCredits.USD
		stats.CreditsEstimated = sessionCredits.Estimated
		e.taskMu.Unlock()
		switch {
		case ctx.Err() != nil || (runErr != nil && errors.Is(runErr, context.Canceled)):
			stats.StopCause = contract.StopCauseUserStop
		case runErr != nil:
			stats.StopCause = contract.StopCauseError
		}
		allEvents := e.InvalidationEvents()
		if eventStart < len(allEvents) {
			stats.Invalidations = append([]contract.InvalidationEvent(nil), allEvents[eventStart:]...)
		}
		e.mergeChangedFiles(filesChanged)
		for file := range filesChanged {
			stats.FilesChanged = append(stats.FilesChanged, file)
		}
		sort.Strings(stats.FilesChanged)
		e.finalizeReviewStats(&stats, assessment.Class, filesChanged, taskLinesAdded, taskLinesRemoved)
		e.attachEpochStats(&stats)
		e.recordEpochTaskResult(userPrompt, answer, stats, runErr)
		if e.callbacks.TaskComplete != nil {
			e.callbacks.TaskComplete(stats)
		}
	}()

	if err := e.persistMessage(ctx, "user", "message", userPrompt, "", map[string]any{"role": "user", "content": userPrompt, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return "", stats, err
	}
	e.history.MarkTaskStart()
	if err := e.ensureStatePersisted(); err != nil {
		return "", stats, err
	}
	// C7: on a long-idle resume of a large session, prune before the first request â€”
	if err := e.staleResumePruneIfNeeded(ctx, profile); err != nil {
		return "", stats, err
	}
	maintenance, err := e.runMaintenanceBoundary(ctx, profile)
	if err != nil {
		return "", stats, err
	}
	folded = maintenance.FoldedTokens
	brief := budget.Brief
	// tail growth â€” never a settled-prefix mutation, and no invalidation is owed.
	// MUHIYA.md/MEMORY.md never injects a block; an edit â€” by the agent's own file
	// tools or by the user â€” surfaces exactly once, then folds into the next
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
	taskTailContent := modelPrompt + "\n\n" + brief
	e.history.Append(contract.Message{Role: contract.RoleUser, Content: taskTailContent})
	if err := e.persistProjectCursor(ctx); err != nil {
		return "", stats, err
	}
	e.emitContext(0)
	sessionPinMain := e.session.ID + ":main"

	definitions := e.sessionDefinitions()
	promptContext := e.prompt
	promptContext.Workspace = e.session.WorkspacePath
	promptContext.OS = runtime.GOOS
	promptContext.HasWeb = hasDefinition(definitions, "web_search")
	promptContext.Lean = e.settings.TokenEconomyMode == "balanced" || e.settings.TokenEconomyMode == "aggressive"
	promptText := SystemPrompt(promptContext)
	encodedDefinitions, err := json.Marshal(definitions)
	if err != nil {
		return "", stats, fmt.Errorf("encode tool definitions: %w", err)
	}
	e.recordAssemblySizes(promptText, promptContext, len(encodedDefinitions))
	toolTokens := EstimateTokens(string(encodedDefinitions))

	currentClass := assessment.Class
	turnCap := budget.MaxTurns
	convergeNoted, finalNoted := false, false
	sawToolCall, emptyFinalRetries, consecutiveFailures := false, 0, 0
	intentFinalRetries := 0   // FR-004b: bounded retries when a turn narrates an action without calling a tool
	autoReviewNudged := false // DG-7: the max-effort review nudge fires at most once per task

	allFailedTurnStreak := 0 // B7: consecutive turns where EVERY tool call failed (reset at Run start via this local)
	overBudgetNoted := false
	turnRecovered := false // G2.2: the one provider-error retry this task gets
	forceFinal := false
	var grantedOverrides []contract.BudgetOverrideReason
	productivity := NewProductivityTracker()
	truncationController := NewTruncationController()
	var pendingTruncation *PendingTruncationRetry
	for {
		requestBudgetDecision := ""
		turns++
		if err := ctx.Err(); err != nil {
			return "", stats, fmt.Errorf("task stopped: %w", err)
		}
		requestPhase := contract.ExecutionPhaseOrient
		if phaseController != nil {
			requestPhase = phaseController.Current()
		}
		logicalStepID := fmt.Sprintf("%s:%d", requestPhase, e.nextRequestSeq())
		if phaseController != nil {
			logicalStepID = phaseController.LogicalStepID()
		}
		requestedOutputCap := 0
		var retryOf *int
		if pendingTruncation != nil {
			logicalStepID = pendingTruncation.LogicalStepID
			requestedOutputCap = pendingTruncation.NextCap
			retry := pendingTruncation.RetryOf
			retryOf = &retry
			requestBudgetDecision = pendingTruncation.Code
			pendingTruncation = nil
		}
		live := profile
		liveBudget := BudgetFor(Assessment{Class: currentClass, Risky: assessment.Risky, ScopeGuard: assessment.ScopeGuard}, live)
		ceiling := hardTurnCeilingFor(profile)
		turnCap = min(ceiling, max(turnCap, liveBudget.MaxTurns))
		if e.drainSteering(ctx) {
			turnCap = min(ceiling, max(turnCap, turns-1+liveBudget.MaxTurns))
			convergeNoted, finalNoted = false, false
			forceFinal = false
		}
		// split the main model writes nothing but tasks.md â€” so a task whose real
		e.mergeChangedFiles(filesChanged)
		// The ladder may climb more than once. It used to fire a single time per
		// real work it was doing â€” and "Go" after a plan classifies as chat, which
		if !forceFinal && turns >= turnCap && len(filesChanged) > 0 && currentClass != ClassEpic {
			currentClass = EscalateClass(currentClass)
			bigger := BudgetFor(Assessment{Class: currentClass, Risky: assessment.Risky}, live)
			turnCap = min(ceiling, max(turnCap+6, bigger.MaxTurns))
			convergeNoted, finalNoted = false, false
			e.setTaskControl("governor.scope."+string(currentClass), fmt.Sprintf("Task outgrew its brief; class=%s. Keep going only while required work remains, then verify once and report.", currentClass))
		}
		isFinal := forceFinal || turns >= turnCap
		if isFinal && !finalNoted {
			finalNoted = true
			e.setTaskControl("governor.final", "Do not call tools. Give the factual final answer now: outcome, verification, and genuine remaining work.")
		} else if !isFinal && turns >= turnCap-2 && !convergeNoted {
			convergeNoted = true
			e.setTaskControl("governor.converge", "Two steps remain. Dispatch only what is essential, accept the result, and finish.")
		}
		if authoritativePhase {
			phase := requestPhase
			evaluation, evaluationErr := e.taskBudgetAtBoundary(taskBudgetBoundaryInput{
				UsageRecordStart: usageRecordStart, Assessment: assessment, Phase: phase,
				ToolCalls: toolCalls, Elapsed: time.Since(started),
			})
			if evaluationErr != nil {
				return "", stats, fmt.Errorf("evaluate token economy boundary: %w", evaluationErr)
			}
			decision := EvaluateVerificationBoundary(evaluation, phase, phaseController.NeedsVerification(), grantedOverrides)
			if decision.Override != nil {
				grantedOverrides = append(grantedOverrides, *decision.Override)
				requestBudgetDecision = decision.Code
				e.setTaskControl(decision.Code, "Run only the smallest proving check required for the completed mutation, then finish without another model request.")
			} else if len(evaluation.SoftExceeded) > 0 && decision.AllowRequest {
				e.setTaskControl("budget.converge", "The soft economy budget is reached. Do only the smallest remaining required action, then finish.")
			}
			if decision.Stop {
				return e.finalize(ctx, budgetBoundaryFinal(filesChanged, checksRun, decision.Code)), stats, nil
			}
		}
		// T039 ladder: soft advisory band [0.50, 0.60) mutates NOTHING â€” it just
		if pr := e.contextPressure(); pr.Ratio >= softNoticeRatio && pr.Ratio < maintenanceFloorRatio {
			e.taskMu.Lock()
			shown := e.softNoticeShown
			e.softNoticeShown = true
			e.taskMu.Unlock()
			if !shown {
				e.callbacks.EmitStatus("Context is filling; earlier turns will be summarized when needed.")
			}
		}
		if e.routedWindowRisk() {
			e.taskMu.Lock()
			shown := e.routedWindowNoted
			e.routedWindowNoted = true
			e.taskMu.Unlock()
			if !shown {
				e.callbacks.EmitNotice("This conversation has outgrown some of the providers serving this model. Run /compact to keep it safely routable.")
			}
		}

		if e.needsCompact(live) {
			pressure := e.contextPressure()
			skipCompact := false
			if pressure.Ratio < compactForceRatio {
				if _, err := e.runMaintenanceBoundary(ctx, profile); err != nil {
					return "", stats, err
				}
				// the estimated pressure below the compaction threshold, skip the
				// paid summarization (one rewrite instead of two). A slight
				// under-estimate here only risks skipping one turn early; the next
				// turn's real token count re-triggers compaction if still needed.
				usable := max(1, e.contextBudgetFor(e.settings.Provider.ActiveModelID, toolTokens).UsableInput)
				postRatio := float64(e.history.EstimatedTokens()) / float64(usable)
				if postRatio < profile.CompactThreshold {
					skipCompact = true
				}
			}
			if !skipCompact {
				e.callbacks.EmitStatus("Compacting context...")
				e.callbacks.EmitNotice("Context is nearly full â€” compacting the conversationâ€¦")
				pressure = e.contextPressure()
				beforeCompact := e.history.EstimatedTokens()
				if err := e.compact(ctx, "automatic: context nearly full"); err != nil {
					// C5: a tail-placed user message with a bracketed marker. Same
					// cache-safe behavior as before (no mid-history edit), but it
					// becomes a user turn instead of a system turn so providers do
					// not need to special-case it.
					e.setTaskControl("recovery.compaction", "Automatic compaction failed; continue using the bounded recent context.")
					e.callbacks.EmitNotice("Compaction failed â€” continuing with the recent context.")
				} else if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
					Cause: contract.InvalidationCompact, Trigger: contract.InvalidationPressure,
					Scope: "automatic structured-summary compaction", Pressure: floatPointer(pressure.Ratio),
					RequestSeq: e.nextRequestSeq(),
				}); err != nil {
					return "", stats, err
				} else {
					e.resetMaintenanceLatch()
					e.callbacks.EmitNotice("Compaction complete â€” " + describeFreed(beforeCompact, e.history.EstimatedTokens(), e.contextLimit()))
				}
			}
		}
		// Trim aged tool payloads only when the window is genuinely filling.
		// Trimming rewrites history and busts the prefix cache, so it must not
		// run on every turn â€” only once pressure crosses the threshold.
		e.callbacks.EmitStatus("Thinking...")
		// Capture pressure before assembly. Assembly may advance the persisted
		// window and rewrite version; that invalidates the old provider measurement
		// for future decisions, but must not change why this boundary drop occurred.
		pressureBeforeBuild := e.contextPressure()
		outputCap := e.phaseOutputBudget(e.settings.Provider.ActiveModelID, requestPhase, requestedOutputCap, toolTokens)
		requestPlan := BuildRequestPlan(RequestPlanInput{
			RequestSeq: uint64(e.nextRequestSeq()), Class: assessment.Class, Risky: assessment.Risky,
			Phase: requestPhase, UserEffort: profile.Level, FailureRecovery: requestPhase == contract.ExecutionPhaseRecover,
			MaxOutputTokens: outputCap,
		})
		economyBehaviorActive := e.settings.TokenEconomyMode == "balanced" || e.settings.TokenEconomyMode == "aggressive"
		effectiveReasoning := ReasoningForEffort(profile.Level)
		ctxBudget := e.contextBudgetFor(e.settings.Provider.ActiveModelID, toolTokens)
		if economyBehaviorActive {
			effectiveReasoning = requestPlan.Reasoning
			ctxBudget = e.contextBudgetForOutput(e.settings.Provider.ActiveModelID, toolTokens, requestPlan.MaxOutputTokens)
		}
		built := e.history.BuildRequestWithMetadata(promptText, ctxBudget.Limit, ctxBudget.ReservedTokens)
		if built.OverBudget {
			return "", stats, fmt.Errorf("context cannot fit the newest task safely: estimated input %d tokens exceeds the %d-token input budget (model window %d, output %d, tools %d, protocol margin %d); start a new session or reduce the request", built.EstimatedTokens, built.AvailableTokens, ctxBudget.Limit, ctxBudget.Output, ctxBudget.ToolTokens, protocolMarginTokens)
		}
		if err := e.ensureStatePersisted(); err != nil {
			return "", stats, err
		}
		if built.WindowDropped {
			trigger := contract.InvalidationPressure
			if pressureBeforeBuild.Ratio < maintenanceFloorRatio {
				trigger = contract.InvalidationBoundary
			}
			if err := e.recordInvalidation(ctx, contract.InvalidationEvent{
				Cause: contract.InvalidationWindowDrop, Trigger: trigger,
				Scope:    fmt.Sprintf("request window dropped %d previously transmitted unit(s)", built.DroppedUnits),
				Pressure: floatPointer(pressureBeforeBuild.Ratio), RequestSeq: e.nextRequestSeq(),
			}); err != nil {
				return "", stats, err
			}
		}
		messages := built.Messages
		manifest, err := buildContextManifest(uint64(e.nextRequestSeq()), encodedDefinitions, promptText, promptContext, messages, taskTailContent)
		if err != nil {
			return "", stats, fmt.Errorf("build context manifest: %w", err)
		}
		e.recordContextManifest(manifest)
		// Feature 006: project memory is a file the agent edits with its ordinary
		// tools, so answers no longer carry a <project-memory> trailer to hide â€” live
		// tokens stream straight through to display.
		request := contract.ChatRequest{SessionID: sessionPinMain, Messages: messages, Tools: definitions, ModelID: e.settings.Provider.ActiveModelID, ToolChoice: "auto", Reasoning: effectiveReasoning, MaxTokens: ctxBudget.Output, PinUpstream: e.upstreamPin(), OnToken: e.callbacks.Token, OnReasoningToken: e.callbacks.ReasoningToken, OnStreamReset: e.callbacks.StreamReset}
		request.LogicalManifestHash = manifest.LogicalHash()
		request.OnRequestShape = func(observation contract.RequestShapeObservation) {
			if observation.LogicalManifestHash != request.LogicalManifestHash {
				return
			}
			manifest.WireHash = observation.WireHash
			e.recordContextManifest(manifest)
		}
		shapeRequest := request
		if normalizer, ok := e.provider.(wireRequestNormalizer); ok {
			normalizedMessages, normalizeErr := normalizer.StableRequestMessages(request)
			if normalizeErr != nil {
				return "", stats, fmt.Errorf("normalize request for prefix shape: %w", normalizeErr)
			}
			shapeRequest.Messages = normalizedMessages
		} else {
			// C2: do not silently hash pre-replay bytes â€” that would let a real
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
		if err := e.checkResumeDrift(ctx, shape); err != nil {
			return "", stats, err
		}
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
		requestSeq := e.nextRequestSeq()
		e.taskMu.Lock()
		e.taskControlWireStarted = true
		e.taskMu.Unlock()
		response, err := e.provider.Chat(ctx, request)
		if err != nil {
			if phaseController != nil {
				_ = phaseController.Record(PhaseOutcomeFailed, uint64(e.nextRequestSeq()))
			}
			// Provider errors reach here only after the gateway's own retries are
			// exhausted; record the friction (T011). The error string already
			// carries the HTTP status; T051 renders it into user guidance.
			e.recordHarnessEvent(ctx, contract.HarnessProvider, "chat-error", err.Error())
			// One turn-level recovery before the task dies. Aborting throws away
			// every completed step of a long task over a single failed request â€”
			// and the request is cheap to repeat: identical bytes against a prefix
			// the provider just cached. Bounded at ONE per task so a genuinely
			// down upstream still surfaces quickly, and skipped entirely when the
			// user cancelled or the error is one repeating will not fix.
			// A 429 needs its cause named before anything else: "slow down" and
			// "out of budget" arrive as the same status, and only one of them is
			// worth waiting through.
			if explanation := e.explainRateLimit(ctx, err); explanation != "" {
				e.callbacks.EmitNotice(explanation)
				return "", stats, err
			}
			if !turnRecovered && ctx.Err() == nil && recoverableChatError(err) {
				turnRecovered = true
				e.callbacks.EmitStatus("Provider hiccup â€” retrying this step...")
				e.recordHarnessEvent(ctx, contract.HarnessRecovery, "chat-retry", err.Error())
				if sleepErr := sleepContext(ctx, turnRecoveryDelay); sleepErr != nil {
					return "", stats, err
				}
				turns-- // the retried step is the same step, not a new one
				continue
			}
			return "", stats, err
		}
		// messageCount is the post-normalization wire count (== e.lastSentMessageCount
		// basis) so suspiciousCacheMiss compares this turn's and the prior turn's
		// counts on the same wire basis rather than a normalizer-skewed one.
		promptTokens := nullableUsageValue(response.Usage.PromptTokens, response.Usage.PromptTokensAvailable)
		manifest.Reconcile(promptTokens)
		e.recordContextManifest(manifest)
		budgetDecision := requestBudgetDecision
		if e.settings.TokenEconomyMode != "off" {
			phase := contract.ExecutionPhaseOrient
			if phaseController != nil {
				phase = phaseController.Current()
			}
			evaluation, evaluationErr := e.currentTaskBudget(currentTaskBudgetInput{
				UsageRecordStart: usageRecordStart, Assessment: assessment, Phase: phase,
				CurrentUsage: response.Usage, ToolCalls: toolCalls, Elapsed: time.Since(started),
			})
			if evaluationErr != nil {
				return "", stats, fmt.Errorf("evaluate token economy budget: %w", evaluationErr)
			}
			if budgetDecision == "" {
				budgetDecision = evaluation.DecisionCode()
			}
		}
		if economyBehaviorActive {
			planDecision := strings.Join(requestPlan.DecisionCodes, ",")
			if budgetDecision == "" {
				budgetDecision = planDecision
			} else {
				budgetDecision += ";" + planDecision
			}
		}
		outputBudgetOverride := ""
		if strings.HasPrefix(requestBudgetDecision, "recover.output") {
			outputBudgetOverride = requestBudgetDecision
		}
		observation := mainUsageObservation{
			model: e.settings.Provider.ActiveModelID, usage: response.Usage, changeReasons: changeReasons,
			messageCount: shape.MessageCount, durationMS: elapsedMS(requestStart), rewriteVersion: shape.RewriteVersion,
			manifestHash: manifest.LogicalHash(), finishReason: response.FinishReason,
			transport: e.settings.Provider.Type, budgetDecision: budgetDecision,
			phase: phasePointer(phaseController), retryOf: retryOf,
			reasoningTier: request.Reasoning, outputCap: request.MaxTokens,
			truncationEscalated: retryOf != nil, outputBudgetOverride: outputBudgetOverride,
			taskEpochID: e.activeTaskEpochPointer(),
		}
		if err := e.recordUsageAndEmit(func() error { return e.recordMainUsage(ctx, observation) }); err != nil {
			return "", stats, fmt.Errorf("persist request usage: %w", err)
		}
		e.emitColdStartNoticeIfPending(ctx) // C6: honest resume cold-start notice, off the usage lock
		e.emitUpstreamNoticeIfPending(ctx)  // a routing-layer flip is the other honest cause of a cold turn
		e.lastShape = &shape
		// Record the POST-normalization wire message count, not len(messages). Next
		// turn feeds this as settledCount to NewWirePrefixShape, which slices the
		// settled window out of the NORMALIZED history it hashes. Storing the
		// pre-normalization count mis-sizes that window whenever the provider's
		// StableRequestMessages adds or drops an entry (e.g. repairing a dangling
		// tool call on a resumed session), producing a false "history changed" abort
		// on a purely append-only turn. With no normalizer shape.MessageCount ==

		// len(messages), so this is a strict no-op there.
		e.lastSentMessageCount = shape.MessageCount
		if err := e.persistPrefixShapeOnce(ctx, shape); err != nil {
			return "", stats, err
		}
		// Emit the task-cumulative usage (all streams), not this response's
		// single-request usage: the live tokens/cache tag must describe the
		// whole task so far, matching the end-of-task summary's semantics.
		e.emitContext(response.Usage.PromptTokens)
		// B-3: finish_reason "length" means the model's reply was cut at the output
		// cap â€” surface it instead of silently treating a truncated answer as
		// complete (MiniMax/GLM hit this at 16k).
		if response.FinishReason == "length" {
			e.recordHarnessEvent(ctx, contract.HarnessProvider, "output-truncated", "finish_reason=length")
			e.callbacks.EmitNotice("The model's reply reached the output limit and was cut off â€” it may be incomplete.")
		}
		// T038: calibrate the token estimator from this real provider usage so the
		// pressure estimate (used when provider tokens are unavailable, e.g. the
		// bootstrap turn) tracks the actual tokenizer rather than a fixed 0.25
		// chars/token guess. promptText is the system prompt just sent; its chars
		// count toward promptTokens but are not stored in the message log.
		if response.Usage.PromptTokensAvailable {
			e.history.CalibrateRequest(response.Usage.PromptTokens, messages, len(encodedDefinitions))
		}
		truncationDecision := TruncationDecision{Code: "output.complete"}
		if economyBehaviorActive {
			truncationDecision = truncationController.Record(TruncationObservation{
				LogicalStepID: logicalStepID, RequestSeq: requestSeq, CurrentCap: request.MaxTokens,
				MaximumCap:   e.phaseOutputBudget(e.settings.Provider.ActiveModelID, requestPhase, int(^uint(0)>>1), toolTokens),
				FinishReason: response.FinishReason,
			})
		}
		if truncationDecision.Retry {
			pendingTruncation = &PendingTruncationRetry{
				LogicalStepID: logicalStepID, NextCap: truncationDecision.NextCap,
				RetryOf: truncationDecision.RetryOf, Code: truncationDecision.Code,
			}
			e.setTaskControl(truncationDecision.Code, "The previous output was cut off. Retry this logical step once with the bounded larger output cap; do not execute partial tool JSON.")
		}
		stopForTruncation := truncationDecision.Stop
		if doneCriteria == "" {
			doneCriteria = extractDoneCriteria(response.Content)
		}
		calls, assistantText := response.ToolCalls, response.Content
		if !isFinal && len(calls) == 0 && e.rescue != nil {
			calls, assistantText = e.rescue(response.Content, toolNames(definitions))
		}
		if isFinal {
			productivity.Record(requestPhase, RequestProductivityOutcome{FinalAnswer: strings.TrimSpace(assistantText) != ""})
			if e.hasSteering() {
				if strings.TrimSpace(assistantText) != "" {
					_ = e.persistAssistant(ctx, assistantText)
				}
				continue
			}
			_ = recordFinalPhase(phaseController, uint64(e.nextRequestSeq()))
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if len(calls) == 0 && (truncationDecision.Retry || stopForTruncation) {
			if strings.TrimSpace(assistantText) != "" {
				_ = e.persistMessage(ctx, "assistant", "truncated", assistantText, "", map[string]any{"role": "assistant", "content": assistantText, "truncated": true, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)})
			}
			e.history.Append(contract.Message{Role: contract.RoleAssistant, Content: "[assistant output truncated at the provider cap; partial content omitted from replay]"})
			if stopForTruncation {
				return e.finalize(ctx, "The provider truncated the same logical step twice. Partial tool calls were not executed; split the requested change into a smaller step and continue."), stats, nil
			}
			continue
		}
		if len(calls) == 0 {
			trimmed := strings.TrimSpace(assistantText)
			productiveDecision := productivity.Record(requestPhase, RequestProductivityOutcome{FinalAnswer: trimmed != "" && !trailingIntent(trimmed)})
			if productiveDecision.Converge {
				e.setTaskControl("productivity.converge", "Stop restating intent. Use existing evidence for one required action, or finish with the factual result or blocker.")
			}
			if e.hasSteering() {
				if trimmed != "" {
					_ = e.persistAssistant(ctx, trimmed)
				}
				continue
			}
			if trimmed == "" && sawToolCall && emptyFinalRetries < 2 {
				emptyFinalRetries++
				e.setTaskControl("finish.empty", "Tool results are available. Give the final answer now; call another tool only if essential.")
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
				e.setTaskControl("finish.trailing_intent", "You announced an action but made no tool call. Make that call now, or give the final result.")
				continue
			}
			// DG-7 (feature 008): AutoReview â€” at max effort, when substantial
			// file-changing work is about to finalize, nudge ONCE to review it
			// before answering. A dynamic tail rider (never prefix), fired at most
			// once per task, skipped when nothing meaningful changed.
			e.mergeChangedFiles(filesChanged)
			if profile.AutoReview && !autoReviewNudged && len(filesChanged) >= 2 {
				autoReviewNudged = true
				// Feature 011 T019: the nudge consults the review gate first â€” a
				// trivial two-file change (docs, renames, tiny low-risk edits) no
				// longer triggers a review just because the effort is max. The gate
				// may only suppress or shape this trigger, never widen it (contract
				// Â§6).
				decision := Decide(e.reviewProfileForTask(currentClass, filesChanged, taskLinesAdded, taskLinesRemoved))
				e.setTaskReviewDecision(decision)
				if decision.Tier != ReviewTierSkip {
					if trimmed != "" {
						_ = e.persistAssistant(ctx, trimmed)
					}
					// The session reviews its own work: it already holds the diff in
					// context, so this is a re-read of what it just wrote, not a
					// dispatch. Naming a tool here that no longer exists is how a

					// nudge turns into a wasted turn and an invented tool call.
					e.history.Append(contract.Message{Role: contract.RoleUser, Content: fmt.Sprintf("[review] Before finishing: re-read the files you changed and check them at a %s level â€” correctness first, then anything you left half-done. Fix what you find, then give the final answer.", decision.Tier)})
					continue
				}
			}
			if phaseErr := recordFinalPhase(phaseController, uint64(e.nextRequestSeq())); phaseErr != nil && authoritativePhase {
				e.setTaskControl("phase.verify.required", "Run the smallest proving verification, then finish factually.")
				continue
			}
			return e.finalize(ctx, fallbackAnswer(trimmed, filesChanged)), stats, nil
		}

		sawToolCall = true
		e.countTerminalReadCalls(calls) // feature 011 SC-006 violation counter
		if strings.TrimSpace(assistantText) != "" {
			if err := e.persistMessage(ctx, "assistant", "message", assistantText, "", map[string]any{"role": "assistant", "content": assistantText, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
		}
		// TB03: a call cut off at the output cap is never dispatched â€” its JSON is
		// incomplete. It is answered with the chunked-write protocol, and history
		// stores a marker instead of the half-written payload so the dead bytes are
		// billed once rather than on every later request.
		truncated := truncatedSet(response)
		executable, cut := splitTruncatedCalls(calls, truncated)
		e.history.Append(assistantReplayMessage(response, assistantText, sanitizeTruncatedCalls(calls, truncated)))
		outcomes := e.truncatedOutcomes(e.mainScope(definitions), cut)
		outcomes = append(outcomes, e.executeBatch(ctx, executable, definitions, live)...)
		for _, outcome := range outcomes {
			toolCalls++
			if outcome.Failed {
				consecutiveFailures++
				// H5: record the failed call's turn in the sliding window for the
				// distinct-failure terminator. The windowed count (not the
				// reset-on-success consecutive counter) is what stops a model
				// alternating between distinct failing actions from looping to
				// the 120-turn ceiling.
				//
				// A pre-dispatch gate rejection (role gate, arg validation, verbatim
				// repeat, repeat limiter, read-only-shell, continuation-review mask)
				// is recoverable guidance, not a tool that ran and failed, so it is
				// excluded from this terminator â€” otherwise a single turn-1 batch of
				// role-gated edits force-finalizes the task with a false "genuine
				// blocker." The gate's own escalation, the B7 all-failed guard, the
				// nudge below, and the hard ceiling remain the liveness backstops.
				if !outcome.GateRejected {
					e.recordTaskFailure(turns)
				}
			} else {
				consecutiveFailures = 0
			}
			if isCheckCall(outcome.Call) && !outcome.Failed {
				checksRun++
			}
			trackChanged(outcome, filesChanged)
			// UD-6 (feature 008): accumulate linesÂ± from applied (successful)
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
			// derived from its input args â€” the same derivation the live TUI does â€”
			// so a reopened transcript row names WHAT the call acted on. Redacted
			// like every persisted field.
			toolTarget := e.redact(contract.ToolTarget(outcome.Call.ToolName(), []byte(outcome.Call.ArgumentsJSON())))
			if err := e.persistMessage(ctx, "tool", outcome.Call.ToolName(), outcome.Output, toolTarget, map[string]any{"role": "tool", "name": outcome.Call.ToolName(), "input": outcome.Call.ArgumentsJSON(), "output": outcome.Output, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return "", stats, err
			}
			e.history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: outcome.Call.ID, Content: outcome.Output})
		}
		phaseErr := recordToolPhases(phaseController, outcomes, uint64(e.nextRequestSeq()))
		if authoritativePhase && phaseErr != nil {
			e.setTaskControl("phase.recovery.exhausted", "Recovery is exhausted; stop calling tools and report the factual result or blocker.")
			forceFinal = true
		} else if authoritativePhase && phaseController.Current() == contract.ExecutionPhaseRecover {
			e.setTaskControl("phase.recover", "Change the approach once; if that cannot progress, report the blocker.")
		}
		if stopForTruncation {
			return e.finalize(ctx, "The provider truncated the same logical step twice. Incomplete tool calls were not executed; split the remaining change into a smaller step and continue."), stats, nil
		}
		if truncationDecision.Retry {
			continue
		}
		if authoritativePhase && phaseErr == nil {
			if final, complete := deterministicOutcomeFinal(outcomes, assistantText, filesChanged); complete {
				_ = recordFinalPhase(phaseController, uint64(e.nextRequestSeq()))
				return e.finalize(ctx, final), stats, nil
			}
		}
		productiveDecision := productivity.Record(requestPhase, toolProductivityOutcome(outcomes))
		if productiveDecision.Converge {
			e.setTaskControl("productivity.converge", "Two requests produced no new useful outcome. Reuse existing evidence and converge on the smallest required action or finish.")
		}
		if authoritativePhase && productiveDecision.RecoverOrFinish {
			if phaseController.Current() == contract.ExecutionPhaseRecover || phaseController.Record(PhaseOutcomeFailed, uint64(e.nextRequestSeq())) != nil {
				e.setTaskControl("productivity.finish", "This phase produced no new useful outcome after bounded recovery. Stop exploring and report the factual result or blocker.")
				forceFinal = true
			} else {
				e.setTaskControl("productivity.recover", "This phase produced no new useful outcome three times. Change approach once, then finish if it still cannot progress.")
			}
		}
		// B7: all-failed-turns detector. A turn in which EVERY tool call failed is
		// a strong loop signal the per-call storm breaker can miss (e.g. the model
		// alternates between distinct failing tools, so no single (tool,error)
		// class reaches 3). Two consecutive all-failed turns inject one loop-guard
		// notice; any success resets the streak.
		if allFailed(outcomes) {
			allFailedTurnStreak++
			if allFailedTurnStreak == 2 {
				e.setTaskControl("failure.all_tools", "[loop guard] Every tool call has failed for two turns running. Change approach, or give the factual status and genuine blocker.")
			}
		} else {
			allFailedTurnStreak = 0
		}
		if consecutiveFailures >= 3 {

			consecutiveFailures = 0
			e.setTaskControl("failure.multiple", "Several tools failed. Use their recorded errors, change approach, and report a genuine blocker if progress is impossible.")
		}
		// H5: distinct-failure terminator. N failed tool calls (any signatures)
		// within the last taskFailureWindow turns force-finalize instead of
		// nudging forever. The nudge-and-reset above could loop to the 120-turn
		// ceiling when a model alternated between distinct failing actions; this
		// windowed count is not reset by interspersed successes or the nudge.
		if failures := e.taskFailureWindowCount(turns); failures >= maxTaskFailures {
			terminateReason = fmt.Sprintf("repeated tool failures â€” %d failures in the last %d turns; stopping to report", failures, taskFailureWindow)
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:tool-failures", terminateReason)
			e.setTaskControl("failure.exhausted", terminateReason+". Stop retrying; give the factual status and genuine blocker.")
			// Consume the breaker instruction in one bounded synthesis turn. The old
			// immediate return finalized a partial tool-call response before the model
			// could report the structured failures and genuine blocker.
			currentClass = ClassEpic
			forceFinal = true
			convergeNoted, finalNoted = true, false
			continue
		}
		if toolCalls > budget.ToolCalls && !overBudgetNoted {
			overBudgetNoted = true
			e.taskMu.Lock()
			e.taskOverBudget = toolCalls - budget.ToolCalls
			e.taskMu.Unlock()
			e.setTaskControl("budget.tools.soft", "The tool budget is passed. Avoid new exploration, finish required work, and report.")
		}
	}
}
