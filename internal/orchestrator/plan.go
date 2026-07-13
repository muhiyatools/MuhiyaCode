package orchestrator

import (
	"context"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Plan mode, adapted from Reasonix. While it is on, the agent researches
// read-only and produces a concrete plan; the engine blocks every mutating
// tool at execution time, and a plan-mode marker rides on the user message
// (never the system prompt) so toggling it does not disturb the prefix cache.
// The setters live in goal.go (they share modeMu with goal toggling so the
// mutual exclusion in G3 stays race-free).

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

// PendingPlan (P2) reports whether a saved plan is awaiting a bare "proceed"
// to execute it. Set by the TUI's "Proceed later" modal choice; cleared (and
// the plan injected into the brief) when the user later sends a continuation
// prompt that matches continuationRE.
func (e *Engine) PendingPlan() bool {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return e.pendingPlan
}

// SetPendingPlan (P2) sets or clears the pending-plan flag and persists the
// plan-state sidecar so it survives a restart. Used by the TUI's "Proceed
// later" (true) and "Proceed now" / "Keep planning" (false) modal choices.
func (e *Engine) SetPendingPlan(on bool) {
	e.modeMu.Lock()
	e.pendingPlan = on
	// 004 US2: keep the lifecycle invariant pendingPlan ⟺ phase==pending. Setting
	// the flag on moves the phase to pending; clearing it leaves the phase for the
	// caller to set (the plan is now executing, discarded, superseded, etc.).
	if on {
		e.planPhase = contract.PlanPhasePending
	}
	e.modeMu.Unlock()
	e.persistPlanState()
}

// RestoredPlanNotice (P2) returns the one-shot notice surfaced when plan-mode
// or a pending plan was restored from the plan_state.json sidecar on session
// resume, then clears it so it is shown only once.
func (e *Engine) RestoredPlanNotice() string {
	e.modeMu.Lock()
	notice := e.restoredPlanNotice
	e.restoredPlanNotice = ""
	e.modeMu.Unlock()
	return notice
}

// PlanPhase (004 US2) reports the whole-plan lifecycle state under modeMu.
func (e *Engine) PlanPhase() contract.PlanPhase {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return e.planPhase
}

// SetPlanPhase (004 US2) transitions the plan lifecycle and persists the
// sidecar. A terminal phase (finished/superseded/discarded) never transitions
// out except back into drafting, which starts a fresh lifecycle for a new plan;
// this guard makes a stray transition a no-op so a finished plan can never be
// revived into an executable state.
func (e *Engine) SetPlanPhase(phase contract.PlanPhase) {
	e.modeMu.Lock()
	if e.planPhase.IsTerminal() && phase != contract.PlanPhaseDrafting && phase != contract.PlanPhaseNone {
		e.modeMu.Unlock()
		return
	}
	e.planPhase = phase
	e.modeMu.Unlock()
	e.persistPlanState()
}

// DiscardPlan (004 US2, T12) marks the current plan discarded — a terminal
// phase that withdraws every executable affordance. Backs the /plan clear
// action. Also clears the pending flag so no legacy consumer re-arms the hint.
func (e *Engine) DiscardPlan() {
	e.modeMu.Lock()
	e.pendingPlan = false
	e.planPhase = contract.PlanPhaseDiscarded
	e.modeMu.Unlock()
	e.persistPlanState()
}

// stampPlanCompletionPhase (004 US2, T10/T11) is the end-of-task hook that
// resolves an executing plan: all steps completed → finished; any step still
// open → interrupted (resumable). Only an executing plan transitions, so
// non-plan tasks and pre-execution phases (drafting/ready/pending) are
// untouched, and the plan-ready finalize paths (phase ready/pending) never
// misfire. Called from the Run defer so it runs on every exit — normal,
// user-stop, error, and breaker alike.
func (e *Engine) stampPlanCompletionPhase() {
	if e.PlanPhase() != contract.PlanPhaseExecuting {
		return
	}
	if e.hasIncompletePlan() {
		e.SetPlanPhase(contract.PlanPhaseInterrupted)
	} else {
		e.SetPlanPhase(contract.PlanPhaseFinished)
	}
}

// planStateSnapshot (P2) reads the plan-mode + pending-plan flags and the
// lifecycle phase under modeMu and returns their persisted shape.
func (e *Engine) planStateSnapshot() contract.PlanStateSnapshot {
	e.modeMu.Lock()
	defer e.modeMu.Unlock()
	return contract.PlanStateSnapshot{PlanMode: e.planMode, PendingPlan: e.pendingPlan, Phase: e.planPhase}
}

// persistPlanState (P2/004) writes the plan-state sidecar off the hot path. It
// clears the sidecar only when there is genuinely no plan (phase none, both
// flags false); a terminal phase (finished/superseded/discarded) is written,
// not deleted, so resume can distinguish "a finished plan exists" from "no plan
// ever" and never resurrects an executable hint. No-op when no persistence
// hooks are wired (unit-test engines).
func (e *Engine) persistPlanState() {
	snapshot := e.planStateSnapshot()
	if snapshot.Phase == contract.PlanPhaseNone && !snapshot.PlanMode && !snapshot.PendingPlan {
		e.clearPlanStateSidecar()
		return
	}
	e.writePlanStateSidecar(snapshot)
}

// writePlanStateSidecar serializes the sidecar write under writeMu and swallows
// the error: a failed persist must never break a plan-mode/pending-plan toggle.
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
// free-text "may I proceed?"). Only fires when plan mode is on and the plan has
// at least one step, so a normal non-plan task end is untouched. In interactive
// runs (TaskComplete wired) plan mode stays on for the TUI modal; in one-shot
// runs it resolves to proceed-later so a later "proceed" executes the plan.
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
	e.SetPlanPhase(contract.PlanPhaseReady) // T2
	if e.callbacks.TaskComplete == nil {
		e.SetPendingPlan(true)
		e.SetPlanMode(false)
		e.SetPlanPhase(contract.PlanPhasePending) // T6: one-shot resolves to proceed-later
	}
}

// planBlock is the per-turn instruction appended to the task brief in plan mode.
// P1: the block is now multi-line and concrete — explicit tools, explicit
// deliverable, explicit exit. Keeping it on the user-message tail preserves
// the prime invariant (cache byte-stability of system+tools).
func (e *Engine) planBlock() string {
	if !e.PlanMode() {
		return ""
	}
	return `[plan-mode]
PLAN MODE — read-only investigation. Every mutating tool call will be BLOCKED and wasted. Do not attempt edits, writes, patches, or state-changing shell commands.

Allowed read-only tools: read_file, list_files, grep, glob, search_text, git_status, git_diff, update_plan, ask_user, and run_subagent with kind=explore|plan|review within the agents budget in the task brief. When that budget reads 0 or a run_subagent call reports the budget exhausted, do not call run_subagent again — investigate directly with the read-only tools and finish the plan yourself.

Deliverable: Produce a concrete ordered plan with exact files, exact changes, and verification steps. Keep update_plan current as the plan evolves.

Exit: When the plan is complete, end by calling the exit_plan_mode tool with a one-paragraph summary. Do not begin implementation.`
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
// mutex exists (NewEngine load-time correction) and in tests.
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

// planRestoreNotice (004 US2, T029) builds the one-shot resume notice from the
// restored lifecycle phase, per the affordance matrix. An executable hint is
// offered only for a genuinely pending plan; an interrupted plan gets resumable
// partial wording; terminal and executing phases surface nothing.
func planRestoreNotice(phase contract.PlanPhase, plan contract.Plan) string {
	switch phase {
	case contract.PlanPhaseDrafting:
		return "Plan mode restored — the agent will keep researching without editing."
	case contract.PlanPhasePending:
		return "Saved plan pending — say 'proceed' (or 'go ahead') to execute it."
	case contract.PlanPhaseInterrupted:
		done, total := planStepProgress(plan)
		return fmt.Sprintf("Plan partially executed — %d/%d steps done; say 'proceed' to resume.", done, total)
	default:
		return ""
	}
}
