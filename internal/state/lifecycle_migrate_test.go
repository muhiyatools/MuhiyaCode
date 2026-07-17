package state

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func completePlan() contract.Plan {
	return contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanCompleted}}}
}

func incompletePlan() contract.Plan {
	return contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanCompleted}, {Title: "b", Status: contract.PlanPending}}}
}

// TestMigrateLifecycleAcrossEras (feature 010 T013/UL-7) verifies the single
// migration loader resolves a plan_state.json sidecar of every era into the
// unified (state, depth) pair, preserving the two truthfulness corrections.
func TestMigrateLifecycleAcrossEras(t *testing.T) {
	cases := []struct {
		name      string
		snapshot  contract.PlanStateSnapshot
		plan      contract.Plan
		wantState contract.LifecycleState
		wantDepth string
	}{
		// Era 3 (feature 010): the canonical state field wins outright.
		{"new-sidecar-wins", contract.PlanStateSnapshot{State: contract.LifecycleImplementing, PipelineDepth: "light", PlanMode: true, Phase: contract.PlanPhaseDrafting}, incompletePlan(), contract.LifecycleImplementing, "light"},
		{"new-sidecar-direct", contract.PlanStateSnapshot{State: contract.LifecycleDirect}, contract.Plan{}, contract.LifecycleDirect, ""},

		// Era 2 (feature 009): pipeline_phase maps; depth defaults to full, light preserved.
		{"pipeline-approve-default-full", contract.PlanStateSnapshot{PipelinePhase: contract.PipelinePhaseApprove}, incompletePlan(), contract.LifecycleApproval, "full"},
		{"pipeline-implement-light", contract.PlanStateSnapshot{PipelinePhase: contract.PipelinePhaseImplement, PipelineDepth: "light"}, incompletePlan(), contract.LifecycleImplementing, "light"},
		{"pipeline-research", contract.PlanStateSnapshot{PipelinePhase: contract.PipelinePhaseResearch}, contract.Plan{}, contract.LifecycleResearch, "full"},
		{"pipeline-direct-falls-through", contract.PlanStateSnapshot{PipelinePhase: contract.PipelinePhaseDirect}, contract.Plan{}, contract.LifecycleDirect, ""},

		// Era 1 (pre-009): the two booleans + explicit phase derive the state.
		{"legacy-pending", contract.PlanStateSnapshot{PendingPlan: true}, incompletePlan(), contract.LifecyclePending, ""},
		{"legacy-planmode", contract.PlanStateSnapshot{PlanMode: true}, contract.Plan{}, contract.LifecyclePlanning, ""},
		{"legacy-phase-ready", contract.PlanStateSnapshot{Phase: contract.PlanPhaseReady}, incompletePlan(), contract.LifecycleApproval, ""},

		// Correction 1: a "pending" plan whose steps are all complete was executed
		// under an older build → resume finished, no stale executable hint.
		{"correction-pending-complete", contract.PlanStateSnapshot{PendingPlan: true, Phase: contract.PlanPhasePending}, completePlan(), contract.LifecycleFinished, ""},
		{"correction-pending-incomplete-stays", contract.PlanStateSnapshot{PendingPlan: true}, incompletePlan(), contract.LifecyclePending, ""},

		// Correction 2: an executing plan (no pipeline) resumes interrupted with
		// open steps, finished when all complete.
		{"correction-executing-incomplete", contract.PlanStateSnapshot{Phase: contract.PlanPhaseExecuting}, incompletePlan(), contract.LifecycleInterrupted, ""},
		{"correction-executing-complete", contract.PlanStateSnapshot{Phase: contract.PlanPhaseExecuting}, completePlan(), contract.LifecycleFinished, ""},

		// Malformed/empty sidecar (treated as absent upstream) → direct.
		{"empty-sidecar", contract.PlanStateSnapshot{}, contract.Plan{}, contract.LifecycleDirect, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, depth := MigrateLifecycle(tc.snapshot, tc.plan)
			if state != tc.wantState || depth != tc.wantDepth {
				t.Fatalf("MigrateLifecycle = (%q, %q), want (%q, %q)", state, depth, tc.wantState, tc.wantDepth)
			}
		})
	}
}
