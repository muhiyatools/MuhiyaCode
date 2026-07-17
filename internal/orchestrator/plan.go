package orchestrator

import (
	"context"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
)

// Plan mode, adapted from Reasonix. While the lifecycle is in a read-only state
// (research/planning/awaiting-approval) the agent researches read-only and
// produces a concrete plan; the engine blocks every mutating tool at execution
// time, and a plan-mode marker rides on the user message (never the system
// prompt) so toggling it does not disturb the prefix cache. The plan-mode
// toggle setters live in goal.go (they share modeMu with goal toggling so the
// plan⇄goal exclusion in G3 stays race-free).

// ResetGoalTaskCounter (G5) zeroes the AutoTurns counter at the boundary of a
// new task so the cap is per-task, not per-goal-lifetime.
func (e *Engine) ResetGoalTaskCounter() {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	if e.goal != nil {
		e.goal.AutoTurns = 0
		e.goal.IdleTurns = 0
		e.goal.LastMarker = ""
	}
}

// PendingPlan (P2) reports whether a saved plan is awaiting a bare "proceed" to
// execute it — the single pending lifecycle state.
func (e *Engine) PendingPlan() bool {
	return e.LifecycleState() == contract.LifecyclePending
}

// SetPendingPlan (P2) saves the plan for later (or clears that saved state) and
// persists the sidecar so it survives a restart. Used by the TUI modal choices
// and by tests. Setting it on moves the lifecycle to pending; clearing it (only
// meaningful from pending) returns to direct — the executing/discarded/etc.
// transitions are owned by their dedicated methods.
func (e *Engine) SetPendingPlan(on bool) {
	e.modeMu.Lock()
	if on {
		e.lifecycle.Set(contract.LifecyclePending)
	} else if e.lifecycle.State == contract.LifecyclePending {
		e.lifecycle.Set(contract.LifecycleDirect)
	}
	e.modeMu.Unlock()
	e.persistPlanState()
}

// RestoredPlanNotice (P2) returns the one-shot notice surfaced when a lifecycle
// state was restored from the plan_state.json sidecar on session resume, then
// clears it so it is shown only once.
func (e *Engine) RestoredPlanNotice() string {
	e.modeMu.Lock()
	notice := e.restoredPlanNotice
	e.restoredPlanNotice = ""
	e.modeMu.Unlock()
	return notice
}

// SetLifecycleState is the terminal-sticky-guarded direct setter for the
// plan-lifecycle affordance changes (drafting/ready/pending/executing/
// interrupted/finished). It replaces the old SetPlanPhase: a terminal state is
// never revived into an executable one; only a fresh planning cycle (or going
// idle to direct) may leave it. Persists the sidecar.
func (e *Engine) SetLifecycleState(state contract.LifecycleState) {
	e.modeMu.Lock()
	e.lifecycle.Set(state)
	e.modeMu.Unlock()
	e.persistPlanState()
}

// DiscardPlan (004 US2, T12) marks the current plan discarded — a terminal
// state that withdraws every executable affordance. Backs the /plan clear
// action. It force-sets discarded (even over another terminal) and releases the
// orchestration depth so a discarded plan's approval gate and mutation block
// never survive the plan they guarded (009 audit fix: /plan clear must not leave
// the pipeline stranded at approve with no escape).
func (e *Engine) DiscardPlan() {
	e.modeMu.Lock()
	e.lifecycle.State = contract.LifecycleDiscarded
	e.lifecycle.Depth = ""
	e.modeMu.Unlock()
	e.persistPlanState()
}

// stampPlanCompletionPhase (004 US2, T10/T11) is the end-of-task hook that
// resolves an executing plan. Only the implementing/validating states are
// stamped, so non-plan tasks and pre-execution states are untouched. Orchestrated
// work that is still in implement/validate at task exit never reached validated
// completion (the pipeline would have transitioned to finished), so it is
// interrupted; a manual plan execution is finished when every step is complete,
// interrupted otherwise. Called from the Run defer so it runs on every exit —
// normal, user-stop, error, and breaker alike.
func (e *Engine) stampPlanCompletionPhase() {
	l := e.Lifecycle()
	if l.State != contract.LifecycleImplementing && l.State != contract.LifecycleValidating {
		return
	}
	if l.Orchestrated() || e.hasIncompletePlan() {
		e.SetLifecycleState(contract.LifecycleInterrupted)
		return
	}
	e.SetLifecycleState(contract.LifecycleFinished)
}

// planStateSnapshot (P2/010) reads the unified lifecycle under modeMu and
// returns its persisted shape: the canonical `state` field plus `depth`. Legacy
// flag/phase fields are never written — a resumed sidecar migrates through the
// single loader in internal/state.
func (e *Engine) planStateSnapshot() contract.PlanStateSnapshot {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return contract.PlanStateSnapshot{State: e.lifecycle.State, PipelineDepth: e.lifecycle.Depth}
}

// persistPlanState (P2/004/010) writes the plan-state sidecar off the hot path.
// It clears the sidecar only when the lifecycle is fully idle (direct); a
// terminal state (finished/superseded/discarded) is written, not deleted, so
// resume can distinguish "a finished plan exists" from "no plan ever" and never
// resurrects an executable hint. No-op when no persistence hooks are wired.
func (e *Engine) persistPlanState() {
	snapshot := e.planStateSnapshot()
	if snapshot.State == contract.LifecycleDirect {
		e.clearPlanStateSidecar()
		return
	}
	e.writePlanStateSidecar(snapshot)
}

// writePlanStateSidecar serializes the sidecar write under writeMu and swallows
// the error: a failed persist must never break a lifecycle transition.
func (e *Engine) writePlanStateSidecar(snapshot contract.PlanStateSnapshot) {
	if e.persistence.WritePlanState == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.persistence.WritePlanState(context.Background(), snapshot)
}

// clearPlanStateSidecar removes the sidecar under writeMu. Idempotent and
// fault-tolerant: a missing or failing ClearPlanState hook never blocks mode
// transitions.
func (e *Engine) clearPlanStateSidecar() {
	if e.persistence.ClearPlanState == nil {
		return
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.persistence.ClearPlanState(context.Background())
}

// maybeSignalPlanReady (P2 belt-and-suspenders) marks a plan-mode task as
// plan-ready when the model finished WITHOUT calling exit_plan_mode (e.g. a
// free-text "may I proceed?"). Only fires in a read-only state with a plan that
// has at least one incomplete step, so a normal non-plan task end is untouched
// and re-entering a read-only state over an already-completed plan cannot re-arm
// the hint. In interactive runs (TaskComplete wired) the read-only state stays
// for the TUI modal; in one-shot runs it resolves to pending so a later
// "proceed" executes the saved plan.
func (e *Engine) maybeSignalPlanReady(planReady *bool) {
	if !e.PlanMode() || len(e.CurrentPlan().Steps) == 0 {
		return
	}
	// 004 US2 (T2): a plan-ready signal requires at least one incomplete step.
	// This closes the compounding stale-hint bug where re-entering plan mode
	// over an already-completed plan re-armed the pending flag.
	if !e.hasIncompletePlan() {
		return
	}
	*planReady = true
	e.SetLifecycleState(contract.LifecycleApproval) // T2: awaiting the decision
	if e.callbacks.TaskComplete == nil {
		e.SetLifecycleState(contract.LifecyclePending) // T6: one-shot resolves to proceed-later
	}
}

// planBlock is the per-turn instruction appended to the task brief in a
// read-only lifecycle state. P1: the block is multi-line and concrete —
// explicit tools, explicit deliverable, explicit exit. Keeping it on the
// user-message tail preserves the prime invariant (cache byte-stability of
// system+tools).
func (e *Engine) planBlock() string {
	if !e.PlanMode() {
		return ""
	}
	return instructions.PlanBlockBody
}

// planStepProgress (004 US2) counts completed steps and the total. Pure helper
// shared by the interrupted-resume notice and the TUI progress line.
func planStepProgress(plan contract.Plan) (done, total int) {
	total = len(plan.Steps)
	for _, step := range plan.Steps {
		if step.Status == contract.PlanCompleted {
			done++
		}
	}
	return done, total
}

// planHasIncompleteSteps (004 US2) reports whether any step is not completed.
// Pure counterpart to the engine's hasIncompletePlan, usable before the engine
// mutex exists (restore-time correction) and in tests.
func planHasIncompleteSteps(plan contract.Plan) bool {
	for _, step := range plan.Steps {
		if step.Status != contract.PlanCompleted {
			return true
		}
	}
	return false
}

// planHasProgressingStep (004 US2, T8) reports whether any step has started or
// finished — the signal that a saved plan has begun executing regardless of how
// the user phrased their go-ahead.
func planHasProgressingStep(steps []contract.PlanStep) bool {
	for _, step := range steps {
		if step.Status == contract.PlanInProgress || step.Status == contract.PlanCompleted {
			return true
		}
	}
	return false
}
