package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestNaturalLanguageDiscardsPendingPlan pins P2: "discard the plan" while a plan is
// pending drops it (the replacement for /plan clear) and stops, without executing.
func TestNaturalLanguageDiscardsPendingPlan(t *testing.T) {
	engine := resilienceEngine(t)
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "do the thing", Status: contract.PlanPending}}}
	engine.SetLifecycleState(contract.LifecyclePending)
	answer, _, err := engine.Run(context.Background(), "discard the plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "Discarded") {
		t.Fatalf("expected a discard acknowledgement, got %q", answer)
	}
	if !engine.Lifecycle().State.IsTerminal() {
		t.Fatalf("the plan should be discarded (terminal), lifecycle=%q", engine.Lifecycle().State)
	}
}

// TestClassifyPlanDocExecution pins P3(a): "execute/run/follow the plan in X.md"
// classifies as a plan-document execution and NEVER re-enters the planning pipeline.
func TestClassifyPlanDocExecution(t *testing.T) {
	for _, p := range []string{
		"execute the plan in PLAN.md",
		"follow docs/plan.md",
		`run the plan in F:/x/MYPLAN.md exactly`,
		"implement the plan in X.md across the codebase",
		"use the plan in ULTIMATE_POLISH_PLAN.md",
	} {
		a := Classify(p, "")
		if !a.PlanDoc || a.PlanDocPath == "" {
			t.Errorf("%q should be a plan-doc execution, got %+v", p, a)
		}
		if NeedsPlan(a).NeedsPlan {
			t.Errorf("%q must NOT enter the pipeline — the plan already exists: %+v", p, a)
		}
	}
}

// TestClassifyPlanCreation pins P3(b): "create/make a plan…" (the natural-language
// replacement for /plan) enters the planning pipeline regardless of size.
func TestClassifyPlanCreation(t *testing.T) {
	for _, p := range []string{
		"create a plan for the auth system",
		"make a plan to migrate the database",
		"plan out the refactor first",
		"I want a plan before you edit anything",
		"plan the auth flow",
		"draft a plan for the payment integration",
	} {
		a := Classify(p, "")
		if !a.PlanRequest || a.PlanDoc {
			t.Errorf("%q should be a plan-creation request, got %+v", p, a)
		}
		if !NeedsPlan(a).NeedsPlan {
			t.Errorf("%q must enter the planning pipeline: %+v", p, a)
		}
	}
}

// TestClassifyPlanIntentNegatives pins that ordinary prompts are not swept up.
func TestClassifyPlanIntentNegatives(t *testing.T) {
	// A bare proceed is still a continuation, not a plan intent.
	if a := Classify("proceed", ClassStandard); a.PlanRequest || a.PlanDoc {
		t.Errorf("'proceed' must not be a plan intent: %+v", a)
	}
	// A planning question stays conversational, never a request.
	if a := Classify("what is a good plan?", ""); a.PlanRequest {
		t.Errorf("a planning question must not be a plan request: %+v", a)
	}
	// A plain coding task without plan language is unaffected.
	if a := Classify("fix the null check in auth.go", ""); a.PlanRequest || a.PlanDoc {
		t.Errorf("an ordinary task must have no plan intent: %+v", a)
	}
}

// TestPlanDocBlockContent pins the inline execution instruction (P3a).
func TestPlanDocBlockContent(t *testing.T) {
	b := planDocBlock("PLAN.md")
	for _, want := range []string{"[plan-document]", "PLAN.md", "update_plan", "Verify", "Do not re-plan"} {
		if !strings.Contains(b, want) {
			t.Errorf("plan-doc block missing %q:\n%s", want, b)
		}
	}
	if got := planDocBlock(""); !strings.Contains(got, "the plan file you named") {
		t.Errorf("empty path should fall back gracefully: %s", got)
	}
}
