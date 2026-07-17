package orchestrator

import (
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// restore.go is the orchestrator-side compat boundary (feature 010 UL-8/MS-3).
// The persistence-layer migration (state.MigrateLifecycle) has already resolved
// any legacy sidecar into the unified (state, depth) pair on the snapshot; this
// file reads ONLY those two fields, rebuilds the gate facts from the durable
// plan, and builds the one-shot resume notice. No legacy plan-mode flag or phase
// enum is referenced here or anywhere else in production.

// restoreLifecycle reconstructs the engine lifecycle from a migrated sidecar
// snapshot plus the durable plan, returning it together with the one-shot resume
// notice. Gate facts are rebuilt only for an orchestrated lifecycle (a non-empty
// depth); a manual plan-mode lifecycle carries none.
func restoreLifecycle(snapshot contract.PlanStateSnapshot, plan contract.Plan) (Lifecycle, string) {
	l := Lifecycle{State: snapshot.State, Depth: snapshot.PipelineDepth, Why: "restored persisted pipeline"}
	if !l.Orchestrated() {
		l.Why = ""
		return l, lifecycleRestoreNotice(l, plan)
	}
	hasSteps := len(plan.Steps) > 0
	switch l.State {
	case contract.LifecyclePlanning:
		l.ResearchCompleted = true
	case contract.LifecycleApproval:
		l.ResearchCompleted, l.PlanWritten = true, hasSteps
	case contract.LifecyclePending, contract.LifecycleInterrupted:
		l.ResearchCompleted, l.PlanWritten, l.Approved = true, hasSteps, true
	case contract.LifecycleImplementing:
		l.ResearchCompleted, l.PlanWritten, l.Approved = true, hasSteps, true
		l.StepsComplete = hasSteps && !planHasIncompleteSteps(plan)
	case contract.LifecycleValidating:
		l.ResearchCompleted, l.PlanWritten, l.Approved, l.StepsComplete = true, hasSteps, true, true
	case contract.LifecycleFinished:
		l.ResearchCompleted, l.PlanWritten, l.Approved, l.StepsComplete, l.Validated = true, hasSteps, true, true, true
	}
	return l, lifecycleRestoreNotice(l, plan)
}

// lifecycleRestoreNotice is the ONE resume-notice builder (feature 010 MS-10),
// merging the former planRestoreNotice and pipelineRestoreNotice over the
// unified states. Orchestrated planning/approval read the pipeline wording;
// manual planning reads the plan-mode wording. Terminal states resume silently.
func lifecycleRestoreNotice(l Lifecycle, plan contract.Plan) string {
	switch l.State {
	case contract.LifecycleResearch:
		return "Orchestration pipeline restored at research — investigation will resume read-only."
	case contract.LifecyclePlanning:
		if l.Orchestrated() {
			return "Orchestration pipeline restored at planning — the execution-grade plan will be completed before approval."
		}
		return "Plan mode restored — the agent will keep researching without editing."
	case contract.LifecycleApproval:
		if l.Orchestrated() {
			return "Orchestration pipeline restored awaiting approval — no implementation has run; say 'proceed' to approve the saved plan."
		}
		return ""
	case contract.LifecyclePending:
		return "Saved plan pending — say 'proceed' (or 'go ahead') to execute it."
	case contract.LifecycleImplementing:
		if l.Orchestrated() {
			done, total := planStepProgress(plan)
			return fmt.Sprintf("Orchestration pipeline restored at implementation — %d/%d to-dos done.", done, total)
		}
		return ""
	case contract.LifecycleValidating:
		return "Orchestration pipeline restored at validation — completion remains blocked until review and checks pass."
	case contract.LifecycleInterrupted:
		done, total := planStepProgress(plan)
		return fmt.Sprintf("Plan partially executed — %d/%d to-dos done; say 'proceed' to resume.", done, total)
	default:
		return ""
	}
}
