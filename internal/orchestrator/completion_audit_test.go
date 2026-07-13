package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// 004 US1 (T014/FR-017): the completion audit reconciles the final answer with
// recorded plan state, disclosing open steps instead of implying completion.

func TestCompletionDisclosesIncompleteSteps(t *testing.T) {
	engine, _ := lifecycleEngine(t, "ca-incomplete")
	engine.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "Wire the handler", Status: contract.PlanCompleted},
		{Title: "Add the migration", Status: contract.PlanInProgress},
		{Title: "Write the tests", Status: contract.PlanPending},
	}, UpdatedAt: time.Now().UTC()}
	engine.SetPlanPhase(contract.PlanPhaseExecuting)
	out := engine.finalize(context.Background(), "All set — the feature is complete.")
	if !strings.Contains(out, "2 of 3 plan steps incomplete") {
		t.Fatalf("disclosure missing: %q", out)
	}
	if !strings.Contains(out, "Add the migration") || !strings.Contains(out, "Write the tests") {
		t.Fatalf("disclosure did not name the open steps: %q", out)
	}
}

func TestCompletionNoDiscloseWhenComplete(t *testing.T) {
	engine, _ := lifecycleEngine(t, "ca-complete")
	engine.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "Only step", Status: contract.PlanCompleted},
	}, UpdatedAt: time.Now().UTC()}
	engine.SetPlanPhase(contract.PlanPhaseExecuting)
	out := engine.finalize(context.Background(), "Done and verified.")
	if strings.Contains(out, "incomplete") {
		t.Fatalf("a completed plan must not disclose incompleteness: %q", out)
	}
}

func TestCompletionNoDiscloseForNonPlanTask(t *testing.T) {
	engine, _ := lifecycleEngine(t, "ca-nonplan")
	// No plan phase (a plain task that happened to use update_plan-style steps
	// but is not an executing plan lifecycle) → no disclosure.
	engine.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "x", Status: contract.PlanInProgress},
	}, UpdatedAt: time.Now().UTC()}
	out := engine.finalize(context.Background(), "Answered the question.")
	if strings.Contains(out, "incomplete") {
		t.Fatalf("a non-plan task must not get a plan disclosure: %q", out)
	}
}
