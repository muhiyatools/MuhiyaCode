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

	"github.com/google/uuid"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
)

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
	e.taskExecutionID = e.session.ID + ":" + started.UTC().Format("20060102T150405.000000000Z")
	e.taskMu.Unlock()
	eventStart := len(e.InvalidationEvents())
	profile := Profile(e.effort())
	assessment := Classify(userPrompt, e.previous)
	e.previous = assessment.Class
	budget := BudgetFor(assessment, profile)
	// The user-selected session model is authoritative. Catalog reconciliation
	// validates it but never substitutes or routes to another model.
	if err := e.reconcileCatalog(); err != nil {
		e.callbacks.EmitNotice(err.Error())
		return "", contract.TaskStats{Status: contract.TaskStatusFailed, TerminatedReason: "Missing configured model"}, err
	}
	modelPrompt := userPrompt
	e.resetTaskState(budget)
	// 013 FR-008: whatever the manual /skills flow already wrapped into this
	// prompt counts as delivered, so read_skill will not send it a second time.
	e.seedProvidedSkills(modelPrompt)
	if err := e.applyBoundaryToolChange(ctx); err != nil {
		return "", stats, err
	}
	filesChanged := make(map[string]bool)
	toolCalls, checksRun, turns, folded := 0, 0, 0, 0
	taskLinesAdded, taskLinesRemoved := 0, 0 // UD-6: Σ diff adds/removes of applied file-changing calls this task
	doneCriteria := ""
	taskIndeterminate := false
	taskStartedPersisted := false

	terminateReason := "" // H5: reason a task was force-finalized (token breaker / failure terminator), surfaced to the TUI as a warn
	defer func() {
		// Computed before the taskMu lock below — harnessEventsSince takes taskMu,
		// and sync.Mutex is not reentrant.
		_, harnessVisible := e.harnessEventsSince(harnessStart)
		e.taskMu.Lock()
		if terminateReason == "" && e.taskEvidenceError != "" {
			terminateReason = "task-graph evidence persistence failed: " + e.taskEvidenceError
		}
		stats = contract.TaskStats{DurationMS: time.Since(started).Milliseconds(), Effort: profile.Level, TaskClass: string(assessment.Class), Usage: subtractUsage(e.sessionUsage, usageStart), PeakContextPercent: e.taskPeakContext, ToolCalls: toolCalls, Turns: turns, ChecksRun: checksRun, FoldedTokens: folded, DisciplineScore: max(0, 100-min(24, e.taskDuplicates*8)-min(16, e.taskOverBudget*2)), DoneCriteria: doneCriteria, TerminatedReason: terminateReason, LinesAdded: taskLinesAdded, LinesRemoved: taskLinesRemoved, HarnessEvents: harnessVisible}
		// UD-6/UD-9 (feature 008): session accumulators for the usage panel —
		// active time and lines± are session-scoped (reset on resume, labeled
		// "this session"); API time derives from persisted record durations.
		e.sessionActiveMS += stats.DurationMS
		e.sessionLinesAdded += taskLinesAdded
		e.sessionLinesRemoved += taskLinesRemoved
		// T043: attach the cumulative session hit-rate so the persistent footer can
		// show session hit-rate, not just this task's cache tag. Computed under
		// taskMu with usageRecords.
		//
		// The footer is a main-session health signal. Explicit auxiliary actions
		// have their own rows and must not distort the main prefix's cache rate.
		stats.SessionHitRate = contract.AggregateUsage(e.usageRecords).SessionHitRate
		// Feature 011 D8: per-(model, pin) cache health for mixed-model sessions.
		stats.PerPairing = contract.PerPairingRates(e.usageRecords)
		// The credits figure is the FULL SESSION cost — every session turn and
		// auxiliary request the gateway priced (user directive: the end-of-task
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
		case taskIndeterminate:
			stats.Status = contract.TaskStatusIndeterminate
			if ctx.Err() != nil || (runErr != nil && errors.Is(runErr, context.Canceled)) {
				stats.StopCause = contract.StopCauseUserStop
			}
		case ctx.Err() != nil || (runErr != nil && errors.Is(runErr, context.Canceled)):
			stats.StopCause = contract.StopCauseUserStop
			stats.Status = contract.TaskStatusCancelled
		case runErr != nil:
			stats.StopCause = contract.StopCauseError
			stats.Status = contract.TaskStatusFailed
		case terminateReason != "":
			stats.Status = contract.TaskStatusIncomplete
		default:
			stats.Status = contract.TaskStatusSucceeded
		}
		// 004 US2 (T10/T11): stamp the plan lifecycle at every task exit. An
		// executing plan resolves to finished (all steps done) or interrupted (any
		// step still open, resumable). Pre-execution and terminal phases are
		// untouched, so the plan-ready finalize paths and non-plan tasks never
		// misfire. Placed here so user-stop, error, and breaker exits stamp too.
		allEvents := e.InvalidationEvents()
		if eventStart < len(allEvents) {
			stats.Invalidations = append([]contract.InvalidationEvent(nil), allEvents[eventStart:]...)
		}
		// Fold in the shared gate's write tally so every mutation this task made
		// counts, wherever in the loop it happened.
		e.mergeChangedFiles(filesChanged)
		for file := range filesChanged {
			stats.FilesChanged = append(stats.FilesChanged, file)
		}
		sort.Strings(stats.FilesChanged)
		if taskStartedPersisted {
			e.finalizeTaskJournal(parent, answer, &stats, &runErr)
		}
		if e.callbacks.TaskComplete != nil {
			e.callbacks.TaskComplete(stats)
		}
	}()

	if err := e.persistTaskStarted(ctx, userPrompt); err != nil {
		return "", stats, err
	}
	taskStartedPersisted = true
	if err := e.persistConversation(ctx, conversationPersistence{
		Message:    contract.Message{Role: contract.RoleUser, Content: userPrompt},
		Kind:       "message",
		Transcript: map[string]any{"role": "user", "content": userPrompt, "createdAt": time.Now().UTC().Format(time.RFC3339Nano)},
	}); err != nil {
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
	sessionPinMain := e.cacheSessionID("main")

	// Optional MCP schemas are task-boundary hydrated or explicitly activated.
	definitions := e.taskDefinitions()
	promptContext := e.prompt
	promptContext.Workspace = e.session.WorkspacePath
	promptContext.OS = runtime.GOOS
	promptContext.HasWeb = hasDefinition(definitions, "web_search")
	promptText := SystemPrompt(promptContext)
	// UD-10/T025 (feature 008): record the component byte sizes behind the
	// /context usage-by-category estimate. Measured once per task from values
	// already in hand — read-side (contextCategories) never recomputes them.
	e.recordAssemblySizes(promptText, promptContext, definitions)
	reserve := e.requestReserve(definitions)

	turnCap := budget.MaxTurns
	convergeNoted, finalNoted := false, false
	sawToolCall, emptyFinalRetries, consecutiveFailures := false, 0, 0
	outputTruncationRetries := 0
	truncatedToolRecoveryRetries := 0
	intentFinalRetries := 0            // FR-004b: bounded retries when a turn narrates an action without calling a tool
	allFailedTurnStreak := 0           // B7: consecutive turns where EVERY tool call failed (reset at Run start via this local)
	providerFailures := make([]int, 0) // G2.2: provider-error sliding window
	stalledProgressTurns := 0
	controller := newPhaseController()
	lastEvidenceRevision := 0
	successfulToolCalls := make(map[string][]int)
	for {
		definitions, reserve = e.refreshTaskDefinitions(definitions, reserve, promptText, promptContext)
		requestReserve := reserve.Total
		if e.taskTokenLimitReached() {
			terminateReason = fmt.Sprintf("hard task token limit reached (%d)", budget.MaxTaskTokens)
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:task-tokens", terminateReason)
			return e.finalize(ctx, fallbackAnswer("", filesChanged)), stats, nil
		}
		if e.BudgetExceeded() {
			terminateReason = "budget exceeded"
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:budget", terminateReason)
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Stop executing; give the final factual status."})
			return e.finalize(ctx, fallbackAnswer("", filesChanged)), stats, nil
		}
		turns++
		if err := ctx.Err(); err != nil {
			return "", stats, fmt.Errorf("task stopped: %w", err)
		}
		live := Profile(e.effort())
		ceiling := e.hardTurnCeiling()
		if e.drainSteering(ctx) {
			turnCap = min(ceiling, max(turnCap, turns+2))
			convergeNoted, finalNoted = false, false
		}
		e.mergeChangedFiles(filesChanged)
		isFinal := turns >= turnCap
		if isFinal && !finalNoted {
			controller.transition(phaseFinalize)
			finalNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Final step: do not call tools. Give the complete factual final answer now: outcome, verification, and genuine remaining work."})
		} else if !isFinal && turns >= turnCap-2 && !convergeNoted {
			convergeNoted = true
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[governor] Two steps remain. Dispatch only what is essential, accept the report, and finish."})
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
		// Routed-window risk: behind a load balancer the model's advertised window
		// is the LARGEST peer's, not every peer's. Past the floor a fallback can
		// drop this conversation on a machine too small to hold it, which fails the
		// request outright rather than degrading. One notice per compaction cycle,
		// sharing the soft-advisory latch so the two cannot both fire and nag.
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
				usable := max(1, e.contextLimit()-requestReserve)
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
		built, effectiveReserve, preflightErr := e.buildMainRequest(promptText, definitions, sessionPinMain, reserve)
		if preflightErr != nil {
			return "", stats, preflightErr
		}
		if err := requestBuildError(built, e.contextLimit(), effectiveReserve); err != nil {
			return "", stats, err
		}
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
		// TB03 (tool_calls truncations) needs bounded reasoning + bounded visible content +
		// a deterministic budget. DeepSeek R1 handles reasoning implicitly, but generic
		// endpoints with effort mappings scale output probabilistically.
		request := e.mainChatRequest(messages, definitions, sessionPinMain)
		// P0-W5: per-turn correlation ID. The turn-level retry (turns--; continue)
		// regenerates this for its next attempt; the transport-level retry inside
		// the provider (MaxRetries=1) reuses it. The gateway uses it as the
		// request_log row ID so one ID ties client turn → gateway attempt → settlement.
		request.RequestID = uuid.NewString()
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
		if serialized, serializeErr := json.Marshal(shapeRequest.Messages); serializeErr == nil {
			built.SerializedMessageBytes = len(serialized)
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
			// One turn-level recovery before the task dies. Aborting throws away
			// every completed step of a long task over a single failed request —
			// and the request is cheap to repeat: identical bytes against a prefix
			// the provider just cached. Bounded at ONE per task so a genuinely
			// down upstream still surfaces quickly, and skipped entirely when the
			// user cancelled or the error is one repeating will not fix.
			// A 429 needs its cause named before anything else: "slow down" and
			// "out of budget" arrive as the same status, and only one of them is
			// worth waiting through.
			if gateway.IsRateLimited(err) {
				explanation := e.explainRateLimit(ctx, err)
				if explanation == "" {
					explanation = "Rate limit reached (429). The provider is temporarily throttling requests."
				}
				terminateReason = explanation
				e.callbacks.EmitNotice(explanation)
				e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:rate-limit", explanation)
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + explanation + ". Stop retrying; give the final factual status."})
				return e.finalize(ctx, fallbackAnswer("", filesChanged)), stats, nil
			}
			if ctx.Err() == nil && recoverableChatError(err) {
				providerFailures = append(providerFailures, turns)
				cutoff := turns - 15
				windowFailures := 0
				for _, t := range providerFailures {
					if t > cutoff {
						windowFailures++
					}
				}
				if windowFailures > 3 {
					terminateReason = fmt.Sprintf("repeated provider failures — %d failures in the last 15 turns; stopping to report", windowFailures)
					e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:provider-failures", terminateReason)
					e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Stop retrying; give the final factual status."})
					return e.finalize(ctx, fallbackAnswer("", filesChanged)), stats, nil
				}
				e.callbacks.EmitStatus("Provider hiccup — retrying this step...")
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
		observation := mainUsageObservation{model: e.settings.Provider.ActiveModelID, usage: response.Usage, changeReasons: changeReasons, messageCount: shape.MessageCount, durationMS: elapsedMS(requestStart), requestBuild: built, rewriteVersion: shape.RewriteVersion, prefixHash: shape.PrefixHash}
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
		e.persistPrefixShapeOnce(ctx, shape) // C3: baseline for the next resume's drift check
		// Emit the task-cumulative usage (all streams), not this response's
		// single-request usage: the live tokens/cache tag must describe the
		// whole task so far, matching the end-of-task summary's semantics.
		e.emitContext(response.Usage.PromptTokens)
		// B-3: finish_reason "length" means the model's reply was cut at the output
		// cap — surface it instead of silently treating a truncated answer as
		// complete (MiniMax/GLM hit this at 16k).
		if response.FinishReason == "length" {
			e.recordHarnessEvent(ctx, contract.HarnessProvider, "output-truncated", "finish_reason=length")
			e.callbacks.EmitNotice("The model's reply reached the output limit and was cut off — it may be incomplete.")
		}
		// Calibrate from the normalized messages and tool surface actually sent.
		// Stored-but-windowed history must not dilute the observed ratio.
		if response.Usage.PromptTokensAvailable {
			e.history.Calibrate(response.Usage.PromptTokens, requestCalibrationChars(shapeRequest.Messages, definitions))
		}
		if doneCriteria == "" {
			doneCriteria = extractDoneCriteria(response.Content)
		}
		calls, assistantText := response.ToolCalls, response.Content
		if retry, terminate := e.handleTruncatedTextResponse(ctx, response, &outputTruncationRetries); retry {
			continue
		} else if terminate {
			terminateReason = "model output reached the limit repeatedly; the final response may be incomplete"
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if !isFinal && len(calls) == 0 && e.rescue != nil && e.needsToolCallRescue() {
			calls, assistantText = e.rescue(response.Content, toolNames(definitions))
		}
		if isFinal && len(calls) == 0 {
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
			if trimmed == "" && !sawToolCall && turns == 1 && emptyFinalRetries < 1 {
				emptyFinalRetries++
				e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[continue] You replied with an empty response. Please begin work on the task."})
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
			return e.finalize(ctx, fallbackAnswer(trimmed, filesChanged)), stats, nil
		}

		sawToolCall = true
		// TB03: a call cut off at the output cap is never dispatched — its JSON is
		// incomplete. It is answered with the chunked-write protocol, and history
		// stores a marker instead of the half-written payload so the dead bytes are
		// billed once rather than on every later request.
		truncated := truncatedSet(response)
		executable, cut := splitTruncatedCalls(calls, truncated)
		if len(cut) > 0 {
			truncatedToolRecoveryRetries++
			if truncatedToolRecoveryRetries > 2 {
				terminateReason = "the model repeatedly exceeded its output limit while constructing a tool call"
				e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:truncated-tool-call", terminateReason)
				return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
			}
			// A cap-hit is a transport-format recovery, not task progress. Always
			// leave room for one compact chunked write plus one factual final turn,
			// even when the original oversized call landed on the nominal last turn.
			turnCap = min(e.hardTurnCeiling(), max(turnCap, turns+2))
			finalNoted = false
			isFinal = false
			e.callbacks.EmitStatus("Recovering an oversized file write...")
		}
		assistantMessage := assistantReplayMessage(response, assistantText, sanitizeTruncatedCalls(calls, truncated))
		if err := e.persistAssistantReplay(ctx, assistantMessage, assistantText); err != nil {
			return "", stats, err
		}
		e.history.Append(assistantMessage)
		outcomes := e.truncatedOutcomes(e.mainScope(definitions), cut)
		outcomes = append(outcomes, e.executeBatch(ctx, executable, definitions, live)...)
		controller.Observe(outcomes)
		for _, outcome := range outcomes {
			// A truncated call never reached dispatch. Counting it as a tool
			// attempt could exhaust the task governor on the exact turn where the
			// model needs one smaller recovery write, turning valid guidance into
			// an immediate false finalization.
			formatRecovery := truncated[outcome.Call.ID]
			if !formatRecovery {
				toolCalls++
			}
			if outcome.IsIndeterminate() {
				taskIndeterminate = true
				if terminateReason == "" {
					terminateReason = "a mutating tool was interrupted after dispatch; its side effect could not be confirmed"
				}
			}
			if outcome.IsFailure() && !formatRecovery {
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
				// excluded from this terminator — otherwise a single turn-1 batch of
				// role-gated edits force-finalizes the task with a false "genuine
				// blocker." The gate's own escalation, the B7 all-failed guard, the
				// nudge below, and the hard ceiling remain the liveness backstops.
				if !outcome.WasRejected() {
					e.recordTaskFailure(turns)
				}
			} else if !formatRecovery {
				consecutiveFailures = 0
			}
			if isCheckCall(outcome.Call) {
				if outcome.Succeeded() {
					checksRun++
				} else if outcome.Status == contract.ToolOutcomeFailed {
					e.grantFailedCheckRetry()
				}
			}
			trackChanged(outcome, filesChanged)
			// UD-6 (feature 008): accumulate lines± from applied (successful)
			// file-changing calls via the SAME shared diff parser the TUI rows use,
			// so the session panel and the per-row counts can never drift.
			if outcome.Succeeded() {
				successfulToolCalls[outcome.Call.ToolName()] = append(successfulToolCalls[outcome.Call.ToolName()], turns)
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
			if err := e.persistToolConversation(ctx, outcome, toolTarget); err != nil {
				return "", stats, err
			}
			e.history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: outcome.Call.ID, Content: outcome.Output})
		}
		if shouldFinalizeAfterEvidence(budget, toolCalls, checksRun, len(filesChanged)) && e.beginTaskFinalization() {
			turnCap = min(turnCap, turns+1)
			convergeNoted = true
			e.history.Append(contract.Message{
				Role:    contract.RoleUser,
				Content: "[governor] The requested change exists and its verification budget is complete. Stop testing, do not create a harness or another artifact, and give the final factual answer.",
			})
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
		// H6: distinct-argument loop coverage per tool.
		if reason, triggered := evaluateToolLoopBreakers(successfulToolCalls, turns); triggered {
			terminateReason = reason
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:tool-loop", terminateReason)
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Stop repeating this action; give the final factual status."})
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if controller.EvidenceRevision() == lastEvidenceRevision && sawToolCall {
			stalledProgressTurns++
		} else {
			stalledProgressTurns = 0
			lastEvidenceRevision = controller.EvidenceRevision()
		}
		if stalledProgressTurns >= 10 {
			terminateReason = "stalled progress: no new tool evidence for 10 turns"
			e.recordHarnessEvent(ctx, contract.HarnessRecovery, "breaker:stalled-progress", terminateReason)
			e.history.Append(contract.Message{Role: contract.RoleUser, Content: "[breaker] " + terminateReason + ". Give the final factual status."})
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
		if isFinal {
			if terminateReason == "" {
				terminateReason = "turn limit reached after tool execution; no final completion response was produced"
			}
			return e.finalize(ctx, fallbackAnswer(assistantText, filesChanged)), stats, nil
		}
	}
}
