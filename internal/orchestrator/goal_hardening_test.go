package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestGoalBlockedWhenTaskEndsUnresolved pins DG1: a goal still active at task end
// (the H5 breaker / turn ceiling / an unmarked final answer) is blocked and dropped,
// so it cannot resurrect as active on the next task or on resume.
func TestGoalBlockedWhenTaskEndsUnresolved(t *testing.T) {
	engine := resilienceEngine(t)
	engine.SetGoal("ship the feature")
	if _, ok := engine.GoalSnapshot(); !ok {
		t.Fatal("goal should be active right after SetGoal")
	}
	engine.reconcileGoalOnTaskEnd()
	if _, ok := engine.GoalSnapshot(); ok {
		t.Fatal("an unresolved goal must be blocked (inactive) at task end")
	}
	last, ok := engine.LastGoalResult()
	if !ok || last.Status != GoalBlocked {
		t.Fatalf("expected a blocked tombstone, got %+v ok=%v", last, ok)
	}
	// Idempotent: a second reconcile (no active goal) is a clean no-op.
	engine.reconcileGoalOnTaskEnd()
}

// TestSetGoalRefusedDuringPipeline pins DG2: a goal cannot be set while the lifecycle
// is pipeline-active (implementing/pending/validating/interrupted); a read-only
// planning phase is instead taken over (G3), not refused.
func TestSetGoalRefusedDuringPipeline(t *testing.T) {
	engine := resilienceEngine(t)
	engine.SetLifecycleState(contract.LifecycleImplementing)
	notice := engine.SetGoal("do something")
	if !strings.Contains(notice, "Finish, proceed, or discard") {
		t.Fatalf("expected a refusal notice during an active pipeline, got %q", notice)
	}
	if _, ok := engine.GoalSnapshot(); ok {
		t.Fatal("a goal must not be set while a plan is executing")
	}

	// Read-only planning → G3 takeover, goal becomes active.
	planning := resilienceEngine(t)
	planning.SetLifecycleState(contract.LifecyclePlanning)
	planning.SetGoal("take over planning")
	if _, ok := planning.GoalSnapshot(); !ok {
		t.Fatal("a goal should take over a read-only planning phase (G3)")
	}

	// Direct lifecycle → goal sets normally.
	direct := resilienceEngine(t)
	direct.SetGoal("normal goal")
	if _, ok := direct.GoalSnapshot(); !ok {
		t.Fatal("a goal should set normally from a direct lifecycle")
	}
}
