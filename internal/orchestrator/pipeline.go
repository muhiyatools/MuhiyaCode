package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

var (
	// Accepts [F1] and multi-citation forms models naturally write —
	// [F1,F5], [F1, f2], [F2/F3] (a live plan used [F1,F5] and was rejected).
	findingCitationRE = regexp.MustCompile(`(?i)\[f\d+(\s*[,&/+]\s*f?\d+)*\]`)
	acceptanceLabelRE = regexp.MustCompile(`(?i)\b(accept|acceptance|verify|check):`)
)

// Lifecycle returns a race-safe copy of the unified lifecycle for gates, UI,
// and tests. The Degradations slice is deep-copied so callers never alias the
// engine's ledger.
func (e *Engine) Lifecycle() Lifecycle {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	l := e.lifecycle
	l.Degradations = append([]LifecycleDegradation(nil), l.Degradations...)
	return l
}

// LifecycleState returns the single authoritative state (replaces the old
// PipelinePhase + PlanPhase readers).
func (e *Engine) LifecycleState() contract.LifecycleState {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return e.lifecycle.State
}

// pipelineActive reports whether the enforced pipeline drives the current
// lifecycle (a depth was assigned). Retained for the gates and tests that read
// the old `pipeline.Phase != direct` signal.
func (e *Engine) pipelineActive() bool {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return e.lifecycle.Orchestrated()
}

func (e *Engine) beginPipeline(ctx context.Context, verdict PlanNeedVerdict) {
	e.mu.Lock()
	hadPlan := len(e.plan.Steps) > 0
	if hadPlan {
		e.supersededPlan = planMarkdown(e.plan)
	}
	e.mu.Unlock()
	e.modeMu.Lock()
	hadPlan = hadPlan && e.lifecycle.State != contract.LifecycleDirect
	// Feature 011 T043 (audit F11): setter-level plan⇄goal exclusivity, the
	// mirror of SetGoal's DG2 guard. A pipeline starting over an active goal
	// clears the goal HERE, at the transition, instead of leaving two live modes
	// for the per-turn brief-assembly backstop to reconcile. The backstop stays
	// as defense in depth.
	goalCleared := false
	if e.goal != nil {
		e.clearGoalLocked("an orchestrated plan is taking over this session")
		e.goal = nil
		goalCleared = true
	}
	// A fresh pipeline run starts a brand-new lifecycle at research with the
	// task's depth (which is also the orchestration signal). Gate facts and the
	// strike counter reset.
	e.lifecycle = Lifecycle{State: contract.LifecycleResearch, Depth: verdict.Depth, Why: verdict.Reason}
	e.modeMu.Unlock()
	if goalCleared {
		e.clearGoalSidecar()
		e.callbacks.EmitNotice("Active goal cleared — an orchestrated plan is taking over.")
	}
	e.persistPlanState()
	if hadPlan {
		e.recordPipelineEvent(ctx, "plan_superseded", map[string]any{"reason": "new pipeline run"})
	}
	e.recordPipelineEvent(ctx, "verdict", map[string]any{"needsPlan": true, "depth": verdict.Depth, "reason": verdict.Reason, "phase": pipelineLabel(contract.LifecycleResearch)})
	e.emitPipelineNotice(contract.LifecycleResearch, verdict.Reason)
}

// parkPipelineForDirectTask clears an orchestrated lifecycle stranded at the
// approval pause so an unrelated direct task can run unobstructed. The saved
// plan itself is preserved as pending (dropping the depth so the resume runs
// direct, the legacy pending-plan path), so a later "proceed" still executes it
// in the main conversation; only the harness gating that belonged to the
// ABANDONED task is released.
func (e *Engine) parkPipelineForDirectTask(ctx context.Context, verdict PlanNeedVerdict) {
	e.modeMu.Lock()
	if !e.lifecycle.State.IsApprovalPause() {
		e.modeMu.Unlock()
		return
	}
	next := Lifecycle{State: contract.LifecycleDirect, Why: verdict.Reason}
	if !e.lifecycle.State.IsTerminal() {
		next.State = contract.LifecyclePending
	}
	e.lifecycle = next
	e.modeMu.Unlock()
	e.persistPlanState()
	e.recordPipelineEvent(ctx, "parked", map[string]any{"reason": "unrelated direct task at approval pause; plan kept pending"})
	e.callbacks.EmitNotice("Pipeline parked — the saved plan stays pending; say 'proceed' anytime to run it. Continuing with the new task directly.")
}

func (e *Engine) recordDirectVerdict(ctx context.Context, verdict PlanNeedVerdict) {
	// A completed pipeline belongs to the prior task. Reset the lifecycle before a
	// fresh direct task so stale done-phase instructions and agent attribution
	// cannot leak across task boundaries. The completed plan and its durable
	// artifacts remain available for audit/resume history.
	e.modeMu.Lock()
	reset := false
	if e.lifecycle.State == contract.LifecycleFinished && e.lifecycle.Orchestrated() {
		e.lifecycle = Lifecycle{State: contract.LifecycleDirect, Why: verdict.Reason}
		reset = true
	}
	e.modeMu.Unlock()
	if reset {
		e.persistPlanState()
	}
	e.recordPipelineEvent(ctx, "verdict", map[string]any{"needsPlan": false, "reason": verdict.Reason, "phase": pipelineLabel(contract.LifecycleDirect)})
	e.callbacks.EmitNotice("Direct path — no plan needed: " + verdict.Reason)
}

// transitionLifecycle applies one gated pipeline edge and emits its notice. It
// merges the old pipelineTransition: legal edges are enforced by
// Lifecycle.Transition, and the terminal-sticky guard lives there too.
func (e *Engine) transitionLifecycle(ctx context.Context, next contract.LifecycleState) error {
	e.modeMu.Lock()
	err := e.lifecycle.Transition(next)
	reason := e.lifecycle.Why
	e.modeMu.Unlock()
	if err != nil {
		return err
	}
	e.persistPlanState()
	e.recordPipelineEvent(ctx, "transition", map[string]any{"phase": pipelineLabel(next)})
	e.emitPipelineNotice(next, reason)
	return nil
}

func (e *Engine) emitPipelineNotice(state contract.LifecycleState, reason string) {
	label := map[contract.LifecycleState]string{
		contract.LifecycleResearch:     "research",
		contract.LifecyclePlanning:     "planning",
		contract.LifecycleApproval:     "awaiting approval",
		contract.LifecycleImplementing: "implementing",
		contract.LifecycleValidating:   "validating",
		contract.LifecycleFinished:     "done",
	}[state]
	if label == "" {
		label = pipelineLabel(state)
	}
	message := "Pipeline phase → " + label
	if state == contract.LifecycleResearch && strings.TrimSpace(reason) != "" {
		message += ": " + reason
	}
	e.callbacks.EmitNotice(message)
}

func (e *Engine) recordPipelineEvent(ctx context.Context, kind string, fields map[string]any) {
	if e.persistence.AddEvent == nil {
		return
	}
	fields["kind"] = kind
	fields["at"] = "dynamic"
	raw, err := json.Marshal(fields)
	if err == nil {
		_ = e.persistence.AddEvent(ctx, "pipeline", kind, e.redact(string(raw)), "")
	}
}

func (e *Engine) MarkPipelineResearchCompleted(ctx context.Context) error {
	e.modeMu.Lock()
	e.lifecycle.ResearchCompleted = true
	e.modeMu.Unlock()
	e.recordPipelineEvent(ctx, "research_complete", map[string]any{})
	return e.transitionLifecycle(ctx, contract.LifecyclePlanning)
}

func (e *Engine) MarkPipelinePlanWritten(ctx context.Context) {
	e.modeMu.Lock()
	e.lifecycle.PlanWritten = true
	e.modeMu.Unlock()
	e.persistPlanState()
	e.recordPipelineEvent(ctx, "plan_written", map[string]any{})
}

// ApprovePipeline is the flow-gate action; it deliberately has no permission-
// mode branch, so auto-accept cannot bypass the explicit plan approval. A
// terminal plan (discarded/superseded/finished) can never be approved into
// execution — the terminal-sticky guard in Transition closes the revival path.
func (e *Engine) ApprovePipeline(ctx context.Context) error {
	e.modeMu.Lock()
	if !e.lifecycle.State.IsApprovalPause() {
		e.modeMu.Unlock()
		return nil
	}
	e.lifecycle.Approved = true
	e.modeMu.Unlock()
	return e.transitionLifecycle(ctx, contract.LifecycleImplementing)
}

// DeferPlan saves the plan for later ("Proceed later"): it moves the lifecycle
// to pending, releasing the read-only gating while keeping the plan resumable
// with a bare "proceed". Depth is preserved, so an orchestrated plan resumes
// through the pipeline and a manual one resumes direct.
func (e *Engine) DeferPlan(ctx context.Context) {
	e.modeMu.Lock()
	if e.lifecycle.State.IsReadOnly() || e.lifecycle.State == contract.LifecyclePending {
		e.lifecycle.Set(contract.LifecyclePending)
	}
	e.modeMu.Unlock()
	e.persistPlanState()
	e.recordPipelineEvent(ctx, "approval_deferred", map[string]any{})
}

// KeepPlanning returns an approved-but-not-executed plan to planning. For an
// orchestrated lifecycle it steers approve→plan and re-opens the gate facts and
// guidance rounds; for a manual one it simply re-enters the planning state.
func (e *Engine) KeepPlanning(ctx context.Context) error {
	e.modeMu.Lock()
	orchestrated := e.lifecycle.Orchestrated()
	atApproval := e.lifecycle.State.IsApprovalPause()
	if orchestrated && atApproval {
		e.lifecycle.Approved = false
		e.lifecycle.PlanWritten = false
		e.modeMu.Unlock()
		return e.transitionLifecycle(ctx, contract.LifecyclePlanning)
	}
	e.lifecycle.Set(contract.LifecyclePlanning)
	e.modeMu.Unlock()
	e.persistPlanState()
	return nil
}

// ProceedWithPlan approves and starts executing the plan ("Proceed now"),
// returning whether execution actually began (false when the plan was terminal
// or not awaiting a decision — the dead-click guard the modal needs).
func (e *Engine) ProceedWithPlan(ctx context.Context) bool {
	e.modeMu.Lock()
	orchestrated := e.lifecycle.Orchestrated()
	atApproval := e.lifecycle.State.IsApprovalPause()
	e.modeMu.Unlock()
	if orchestrated && atApproval {
		if err := e.ApprovePipeline(ctx); err != nil {
			return false
		}
		return e.LifecycleState() == contract.LifecycleImplementing
	}
	return e.beginExecution(ctx)
}

// beginExecution transitions a saved/approved plan into implementation on an
// explicit user "proceed" (the task-entry resume and the manual modal path). A
// terminal plan is never executed. It carries no permission-mode branch, so the
// approval pause is never bypassed by auto-accept.
func (e *Engine) beginExecution(ctx context.Context) bool {
	e.modeMu.Lock()
	if e.lifecycle.State.IsTerminal() {
		e.modeMu.Unlock()
		return false
	}
	e.lifecycle.Approved = true
	e.lifecycle.Set(contract.LifecycleImplementing)
	orchestrated := e.lifecycle.Orchestrated()
	reason := e.lifecycle.Why
	e.modeMu.Unlock()
	e.persistPlanState()
	if orchestrated {
		e.recordPipelineEvent(ctx, "transition", map[string]any{"phase": pipelineLabel(contract.LifecycleImplementing)})
		e.emitPipelineNotice(contract.LifecycleImplementing, reason)
	}
	return true
}

func (e *Engine) SteerPipeline(ctx context.Context) error {
	e.modeMu.Lock()
	if !e.lifecycle.State.IsApprovalPause() {
		e.modeMu.Unlock()
		return nil
	}
	e.lifecycle.Approved = false
	e.lifecycle.PlanWritten = false
	e.modeMu.Unlock()
	return e.transitionLifecycle(ctx, contract.LifecyclePlanning)
}

func (e *Engine) MarkPipelineStepsComplete(ctx context.Context, complete bool) error {
	e.modeMu.Lock()
	e.lifecycle.StepsComplete = complete
	state := e.lifecycle.State
	e.modeMu.Unlock()
	e.persistPlanState()
	if complete && state == contract.LifecycleImplementing {
		if err := e.transitionLifecycle(ctx, contract.LifecycleValidating); err != nil {
			return err
		}
		if e.Lifecycle().DoneReady() {
			return e.transitionLifecycle(ctx, contract.LifecycleFinished)
		}
	}
	return nil
}

func (e *Engine) ConfirmPipelineValidation(ctx context.Context, source string) error {
	e.modeMu.Lock()
	e.lifecycle.Validated = true
	state := e.lifecycle.State
	ready := e.lifecycle.DoneReady()
	e.modeMu.Unlock()
	e.recordPipelineEvent(ctx, "validation_confirmed", map[string]any{"source": source})
	if state == contract.LifecycleValidating && ready {
		return e.transitionLifecycle(ctx, contract.LifecycleFinished)
	}
	e.persistPlanState()
	return nil
}

func (e *Engine) RecordPipelineDegradation(ctx context.Context, state contract.LifecycleState, reason string) error {
	e.modeMu.Lock()
	err := e.lifecycle.RecordDegradation(state, reason)
	e.modeMu.Unlock()
	if err != nil {
		return err
	}
	e.persistPlanState()
	e.recordPipelineEvent(ctx, "degraded", map[string]any{"phase": pipelineLabel(state), "reason": reason})
	e.recordHarnessEvent(ctx, contract.HarnessRecovery, "degradation:"+pipelineLabel(state), reason)
	e.callbacks.EmitNotice(fmt.Sprintf("Pipeline %s degraded — %s", pipelineLabel(state), reason))
	return nil
}

// pipelineBlock is the per-turn orchestration prelude, shown only while an
// orchestrated pipeline is in an active (non-terminal, non-pending) phase.
func (e *Engine) pipelineBlock() string {
	e.modeMu.Lock()
	l := e.lifecycle
	e.modeMu.Unlock()
	if !l.Orchestrated() || !isActivePipelinePhase(l.State) {
		return ""
	}
	return fmt.Sprintf("[orchestration-pipeline]\nverdict=plan required; depth=%s; phase=%s; reason=%s\nFollow the current phase gate. Phase state is harness-enforced; do not attempt to skip ahead.", l.Depth, pipelineLabel(l.State), l.Why)
}

// isActivePipelinePhase reports the orchestrated states that run harness phase
// logic and carry the orchestration prelude (research→validate), excluding
// pending and the terminals.
func isActivePipelinePhase(state contract.LifecycleState) bool {
	switch state {
	case contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleApproval,
		contract.LifecycleImplementing, contract.LifecycleValidating:
		return true
	default:
		return false
	}
}
