package orchestrator

import (
	"errors"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 010 US1: the unified lifecycle state machine. These port the
// assertion intent of the former pipeline_test.go PipelineState tests
// (legal/illegal edges, gates, degradation) and the plan_lifecycle predicate
// checks onto the one Lifecycle type.

// TestLifecycleLegalTransitionsAndGates walks the full orchestrated pipeline
// path and verifies each edge's gate fact (ported from
// TestPipelineLegalTransitionsAndDrivenPlanPhase + TestPipelineGatesAndResearchDegradation).
func TestLifecycleLegalTransitionsAndGates(t *testing.T) {
	l := Lifecycle{State: contract.LifecycleResearch, Depth: PipelineDepthFull}

	// research → planning is gated on research readiness.
	if err := l.Transition(contract.LifecyclePlanning); !errors.Is(err, errLifecycleGate) {
		t.Fatalf("research gate error = %v", err)
	}
	if err := l.RecordDegradation(contract.LifecycleResearch, "allowance exhausted"); err != nil {
		t.Fatal(err)
	}
	if err := l.Transition(contract.LifecyclePlanning); err != nil {
		t.Fatalf("degraded research did not advance: %v", err)
	}
	if len(l.Degradations) != 1 || l.Degradations[0].Reason != "allowance exhausted" {
		t.Fatalf("degradation not recorded: %+v", l.Degradations)
	}

	// planning → approval is gated on a written plan.
	if err := l.Transition(contract.LifecycleApproval); !errors.Is(err, errLifecycleGate) {
		t.Fatalf("plan gate error = %v", err)
	}
	l.PlanWritten = true
	if err := l.Transition(contract.LifecycleApproval); err != nil {
		t.Fatal(err)
	}

	// approval → implementing is gated on explicit approval.
	if err := l.Transition(contract.LifecycleImplementing); !errors.Is(err, errLifecycleGate) {
		t.Fatalf("approval gate error = %v", err)
	}
	l.Approved = true
	if err := l.Transition(contract.LifecycleImplementing); err != nil {
		t.Fatal(err)
	}

	// implementing → validating is gated on completed steps.
	if err := l.Transition(contract.LifecycleValidating); !errors.Is(err, errLifecycleGate) {
		t.Fatalf("steps gate error = %v", err)
	}
	l.StepsComplete = true
	if err := l.Transition(contract.LifecycleValidating); err != nil {
		t.Fatal(err)
	}

	// validating → finished is gated on confirmed validation.
	if err := l.Transition(contract.LifecycleFinished); !errors.Is(err, errLifecycleGate) {
		t.Fatalf("validation gate error = %v", err)
	}
	l.Validated = true
	if err := l.Transition(contract.LifecycleFinished); err != nil {
		t.Fatal(err)
	}
	if l.State != contract.LifecycleFinished {
		t.Fatalf("final state = %s, want finished", l.State)
	}
}

// TestLifecycleSteerEdges: an awaiting-approval plan may be steered back to
// research or planning (ported from TestPipelineSteerEdges).
func TestLifecycleSteerEdges(t *testing.T) {
	for _, next := range []contract.LifecycleState{contract.LifecycleResearch, contract.LifecyclePlanning} {
		l := Lifecycle{State: contract.LifecycleApproval, PlanWritten: true}
		if err := l.Transition(next); err != nil {
			t.Fatalf("approval -> %s: %v", next, err)
		}
	}
}

// TestLifecycleIllegalTransitions: edges outside the table are rejected as
// illegal (not gate) errors, including every exit from a terminal state
// (ported from TestPipelineIllegalTransitions).
func TestLifecycleIllegalTransitions(t *testing.T) {
	for _, tc := range []struct {
		from contract.LifecycleState
		to   contract.LifecycleState
	}{
		{contract.LifecycleDirect, contract.LifecycleResearch},
		{contract.LifecycleResearch, contract.LifecycleImplementing},
		{contract.LifecyclePlanning, contract.LifecycleImplementing},
		{contract.LifecycleApproval, contract.LifecycleFinished},
		{contract.LifecycleImplementing, contract.LifecycleFinished},
		{contract.LifecycleValidating, contract.LifecyclePlanning},
		{contract.LifecycleFinished, contract.LifecycleResearch},
		{contract.LifecycleDiscarded, contract.LifecyclePlanning},
	} {
		l := Lifecycle{State: tc.from, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true, Validated: true}
		if err := l.Transition(tc.to); !errors.Is(err, errLifecycleTransition) {
			t.Fatalf("%s -> %s error = %v", tc.from, tc.to, err)
		}
	}
}

// TestLifecycleUniversalEdges: any non-terminal state may be discarded (/plan
// clear) or superseded (a fresh plan over the old one).
func TestLifecycleUniversalEdges(t *testing.T) {
	for _, from := range []contract.LifecycleState{
		contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleApproval,
		contract.LifecyclePending, contract.LifecycleImplementing, contract.LifecycleValidating,
		contract.LifecycleInterrupted,
	} {
		for _, to := range []contract.LifecycleState{contract.LifecycleDiscarded, contract.LifecycleSuperseded} {
			l := Lifecycle{State: from}
			if err := l.Transition(to); err != nil {
				t.Fatalf("%s -> %s must be legal: %v", from, to, err)
			}
		}
	}
}

// TestLifecycleTerminalSticky: Set never revives a terminal plan into an
// executable state; only a fresh planning cycle (or going idle to direct) may
// leave a terminal state (ported from TestPlanLifecycleTerminalIsSticky).
func TestLifecycleTerminalSticky(t *testing.T) {
	l := Lifecycle{State: contract.LifecycleFinished}
	l.Set(contract.LifecycleImplementing) // must be a no-op
	if l.State != contract.LifecycleFinished {
		t.Fatalf("finished plan was revived to %s", l.State)
	}
	l.Set(contract.LifecyclePending) // also a no-op
	if l.State != contract.LifecycleFinished {
		t.Fatalf("finished plan was revived to %s", l.State)
	}
	// A brand-new planning cycle is allowed to start.
	l.Set(contract.LifecyclePlanning)
	if l.State != contract.LifecyclePlanning {
		t.Fatalf("planning after terminal failed: %s", l.State)
	}
}

// TestLifecyclePredicateSets pins each predicate's exact state set.
func TestLifecyclePredicateSets(t *testing.T) {
	all := []contract.LifecycleState{
		contract.LifecycleDirect, contract.LifecycleResearch, contract.LifecyclePlanning,
		contract.LifecycleApproval, contract.LifecyclePending, contract.LifecycleImplementing,
		contract.LifecycleValidating, contract.LifecycleInterrupted, contract.LifecycleFinished,
		contract.LifecycleSuperseded, contract.LifecycleDiscarded,
	}
	in := func(set ...contract.LifecycleState) map[contract.LifecycleState]bool {
		m := map[contract.LifecycleState]bool{}
		for _, s := range set {
			m[s] = true
		}
		return m
	}
	checks := []struct {
		name string
		pred func(contract.LifecycleState) bool
		want map[contract.LifecycleState]bool
	}{
		{"IsReadOnly", contract.LifecycleState.IsReadOnly, in(contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleApproval)},
		{"BlocksMutation", contract.LifecycleState.BlocksMutation, in(contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleApproval)},
		{"InvitesProceed", contract.LifecycleState.InvitesProceed, in(contract.LifecyclePending, contract.LifecycleInterrupted)},
		{"IsApprovalPause", contract.LifecycleState.IsApprovalPause, in(contract.LifecycleApproval)},
		{"IsPipelineResumable", contract.LifecycleState.IsPipelineResumable, in(contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleImplementing, contract.LifecycleValidating)},
		{"IsTerminal", contract.LifecycleState.IsTerminal, in(contract.LifecycleFinished, contract.LifecycleSuperseded, contract.LifecycleDiscarded)},
		{"IsActive", contract.LifecycleState.IsActive, in(contract.LifecycleResearch, contract.LifecyclePlanning, contract.LifecycleApproval, contract.LifecyclePending, contract.LifecycleImplementing, contract.LifecycleValidating, contract.LifecycleInterrupted, contract.LifecycleFinished, contract.LifecycleSuperseded, contract.LifecycleDiscarded)},
	}
	for _, c := range checks {
		for _, s := range all {
			if got := c.pred(s); got != c.want[s] {
				t.Errorf("%s(%q) = %v, want %v", c.name, s, got, c.want[s])
			}
		}
	}
}

// TestLifecycleOrchestratedFromDepth: the orchestration signal is the depth,
// so no second field can drift from the state.
func TestLifecycleOrchestratedFromDepth(t *testing.T) {
	if (Lifecycle{State: contract.LifecyclePlanning}).Orchestrated() {
		t.Fatal("a depth-less planning lifecycle must be manual (not orchestrated)")
	}
	if !(Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthLight}).Orchestrated() {
		t.Fatal("a depth-carrying planning lifecycle must be orchestrated")
	}
}

// TestLifecyclePlanGoalExclusion: activating a goal from a read-only planning
// lifecycle takes it over into direct work (UL-13, G3). Ultimate Polish P1 removed
// the reverse direction (there is no manual plan toggle to clear a goal); a goal is
// instead refused while a plan executes (DG2, goal_hardening_test.go).
func TestLifecyclePlanGoalExclusion(t *testing.T) {
	engine, _ := lifecycleEngine(t, "lc-exclusion")
	engine.SetLifecycleState(contract.LifecyclePlanning)
	if !engine.PlanMode() {
		t.Fatal("planning did not engage")
	}
	engine.SetGoal("keep the tests green")
	if engine.PlanMode() {
		t.Fatal("setting a goal must take over the read-only planning lifecycle")
	}
	if _, active := engine.GoalSnapshot(); !active {
		t.Fatal("goal should be active after SetGoal")
	}
}
