package state

import "github.com/muhiya/muhiyacode/internal/contract"

// MigrateLifecycle is the SINGLE migration loader (feature 010 UL-7): the only
// code that reads the legacy plan_state.json fields. It resolves a persisted
// sidecar of any era into the unified (state, depth) pair:
//
//  1. the canonical `state` field wins outright (a feature-010 sidecar);
//  2. else a feature-009 `pipeline_phase` (+ `pipeline_depth`) maps through the
//     restore table, defaulting a depth-less legacy pipeline to full;
//  3. else the pre-009 planMode/pendingPlan/phase flags derive it, PRESERVING
//     the two load-time truthfulness corrections verbatim — a pending plan whose
//     steps are all complete was actually executed under an older build (→
//     finished), and a plan caught mid-execution by a crash resumes interrupted
//     (or finished if every step completed).
//
// A malformed sidecar is treated as absent upstream (ReadPlanState), so the zero
// snapshot here migrates to LifecycleDirect. Depth is empty for a manual
// (non-orchestrated) plan-mode lifecycle and light/full for an orchestrated one.
func MigrateLifecycle(snapshot contract.PlanStateSnapshot, plan contract.Plan) (contract.LifecycleState, string) {
	// 1. New sidecar: the unified state is authoritative; ignore the legacy fields.
	if snapshot.State != "" {
		return snapshot.State, snapshot.PipelineDepth
	}
	// 2. Feature-009 pipeline sidecar: map the orchestration phase.
	if snapshot.PipelinePhase != "" && snapshot.PipelinePhase != contract.PipelinePhaseDirect {
		depth := snapshot.PipelineDepth
		if depth == "" {
			// A legacy sidecar written before depth was persisted restores as full —
			// the pre-existing behavior; a light pipeline persists its real depth.
			depth = "full"
		}
		return pipelinePhaseToLifecycle(snapshot.PipelinePhase), depth
	}
	// 3. Pre-009 flags: derive the plan phase, then apply the two corrections.
	phase := snapshot.Phase
	if phase == contract.PlanPhaseNone {
		switch {
		case snapshot.PendingPlan:
			phase = contract.PlanPhasePending
		case snapshot.PlanMode:
			phase = contract.PlanPhaseDrafting
		}
	}
	// Correction 1: a "pending" plan whose steps are all complete was executed
	// under an older build — resume it finished so no stale hint resurrects.
	if phase == contract.PlanPhasePending && len(plan.Steps) > 0 && !planIncomplete(plan) {
		phase = contract.PlanPhaseFinished
	}
	// Correction 2: a plan caught mid-execution (no pipeline) resumes interrupted
	// when steps remain open, finished when they are all complete.
	if phase == contract.PlanPhaseExecuting {
		if planIncomplete(plan) {
			phase = contract.PlanPhaseInterrupted
		} else {
			phase = contract.PlanPhaseFinished
		}
	}
	return planPhaseToLifecycle(phase), ""
}

func pipelinePhaseToLifecycle(phase contract.PipelinePhase) contract.LifecycleState {
	switch phase {
	case contract.PipelinePhaseResearch:
		return contract.LifecycleResearch
	case contract.PipelinePhasePlan:
		return contract.LifecyclePlanning
	case contract.PipelinePhaseApprove:
		return contract.LifecycleApproval
	case contract.PipelinePhaseImplement:
		return contract.LifecycleImplementing
	case contract.PipelinePhaseValidate:
		return contract.LifecycleValidating
	case contract.PipelinePhaseDone:
		return contract.LifecycleFinished
	default:
		return contract.LifecycleDirect
	}
}

func planPhaseToLifecycle(phase contract.PlanPhase) contract.LifecycleState {
	switch phase {
	case contract.PlanPhaseDrafting:
		return contract.LifecyclePlanning
	case contract.PlanPhaseReady:
		return contract.LifecycleApproval
	case contract.PlanPhasePending:
		return contract.LifecyclePending
	case contract.PlanPhaseExecuting:
		return contract.LifecycleImplementing
	case contract.PlanPhaseInterrupted:
		return contract.LifecycleInterrupted
	case contract.PlanPhaseFinished:
		return contract.LifecycleFinished
	case contract.PlanPhaseSuperseded:
		return contract.LifecycleSuperseded
	case contract.PlanPhaseDiscarded:
		return contract.LifecycleDiscarded
	default:
		return contract.LifecycleDirect
	}
}

// planIncomplete reports whether any plan step is not completed.
func planIncomplete(plan contract.Plan) bool {
	for _, step := range plan.Steps {
		if step.Status != contract.PlanCompleted {
			return true
		}
	}
	return false
}
