package orchestrator

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestValidPhaseTransitionTable(t *testing.T) {
	valid := [][2]contract.ExecutionPhase{
		{"", contract.ExecutionPhaseOrient},
		{contract.ExecutionPhaseOrient, contract.ExecutionPhaseInspect},
		{contract.ExecutionPhaseOrient, contract.ExecutionPhaseFinish},
		{contract.ExecutionPhaseInspect, contract.ExecutionPhaseInspect},
		{contract.ExecutionPhaseInspect, contract.ExecutionPhaseChange},
		{contract.ExecutionPhaseInspect, contract.ExecutionPhaseVerify},
		{contract.ExecutionPhaseInspect, contract.ExecutionPhaseFinish},
		{contract.ExecutionPhaseChange, contract.ExecutionPhaseVerify},
		{contract.ExecutionPhaseVerify, contract.ExecutionPhaseChange},
		{contract.ExecutionPhaseVerify, contract.ExecutionPhaseFinish},
		{contract.ExecutionPhaseInspect, contract.ExecutionPhaseRecover},
		{contract.ExecutionPhaseRecover, contract.ExecutionPhaseInspect},
		{contract.ExecutionPhaseRecover, contract.ExecutionPhaseFinish},
	}
	for _, edge := range valid {
		if !ValidPhaseTransition(edge[0], edge[1]) {
			t.Errorf("valid transition rejected: %q -> %q", edge[0], edge[1])
		}
	}
	invalid := [][2]contract.ExecutionPhase{
		{"", contract.ExecutionPhaseChange},
		{contract.ExecutionPhaseOrient, contract.ExecutionPhaseChange},
		{contract.ExecutionPhaseChange, contract.ExecutionPhaseFinish},
		{contract.ExecutionPhaseFinish, contract.ExecutionPhaseRecover},
		{contract.ExecutionPhaseFinish, contract.ExecutionPhaseInspect},
		{contract.ExecutionPhaseRecover, contract.ExecutionPhaseOrient},
	}
	for _, edge := range invalid {
		if ValidPhaseTransition(edge[0], edge[1]) {
			t.Errorf("invalid transition accepted: %q -> %q", edge[0], edge[1])
		}
	}
}

func TestRequestPlanReasoningScalesByClassRiskPhaseAndUserEnvelope(t *testing.T) {
	tests := []struct {
		name string
		in   RequestPlanInput
		want contract.ReasoningTier
	}{
		{name: "chat orient", in: RequestPlanInput{Class: ClassChat, Phase: contract.ExecutionPhaseOrient, UserEffort: contract.EffortMax, MaxOutputTokens: 512}, want: contract.ReasoningLow},
		{name: "tiny change", in: RequestPlanInput{Class: ClassTiny, Phase: contract.ExecutionPhaseChange, UserEffort: contract.EffortMax, MaxOutputTokens: 2000}, want: contract.ReasoningLow},
		{name: "standard change", in: RequestPlanInput{Class: ClassStandard, Phase: contract.ExecutionPhaseChange, UserEffort: contract.EffortHigh, MaxOutputTokens: 4000}, want: contract.ReasoningMedium},
		{name: "large capped by user", in: RequestPlanInput{Class: ClassLarge, Phase: contract.ExecutionPhaseInspect, UserEffort: contract.EffortLow, MaxOutputTokens: 2000}, want: contract.ReasoningLow},
		{name: "risk escalation", in: RequestPlanInput{Class: ClassTiny, Phase: contract.ExecutionPhaseVerify, UserEffort: contract.EffortLow, Risky: true, MaxOutputTokens: 1200}, want: contract.ReasoningMedium},
		{name: "failure escalation", in: RequestPlanInput{Class: ClassSmall, Phase: contract.ExecutionPhaseRecover, UserEffort: contract.EffortMedium, FailureRecovery: true, MaxOutputTokens: 2400}, want: contract.ReasoningHigh},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := BuildRequestPlan(test.in)
			if plan.Reasoning != test.want {
				t.Fatalf("reasoning=%s want=%s plan=%+v", plan.Reasoning, test.want, plan)
			}
			if err := plan.Validate(); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(plan.DecisionCodes, " ")
			if !strings.Contains(joined, "phase."+string(test.in.Phase)) || !strings.Contains(joined, "reasoning.") || !strings.Contains(joined, "output.") {
				t.Fatalf("missing decision attribution: %v", plan.DecisionCodes)
			}
		})
	}
}

func TestRequestPlanDeescalatesAfterRiskyPhaseCompletes(t *testing.T) {
	risky := BuildRequestPlan(RequestPlanInput{Class: ClassTiny, Phase: contract.ExecutionPhaseVerify, UserEffort: contract.EffortLow, Risky: true, MaxOutputTokens: 1200})
	finish := BuildRequestPlan(RequestPlanInput{Class: ClassTiny, Phase: contract.ExecutionPhaseFinish, UserEffort: contract.EffortLow, MaxOutputTokens: 800})
	if risky.Reasoning != contract.ReasoningMedium || finish.Reasoning != contract.ReasoningLow {
		t.Fatalf("reasoning did not de-escalate: risky=%s finish=%s", risky.Reasoning, finish.Reasoning)
	}
	if strings.Contains(strings.Join(finish.DecisionCodes, ","), "escalated") {
		t.Fatalf("stale escalation leaked into finish plan: %v", finish.DecisionCodes)
	}
}

func TestExecutionPhaseTransitionIsImmutableAndMonotonic(t *testing.T) {
	original := ExecutionPhaseState{Phase: contract.ExecutionPhaseInspect, EnteredAtRequest: 2, Attempts: 3, RequiredEvidence: []string{"path"}, SatisfiedEvidence: []string{"path"}}
	next, err := original.Transition(contract.ExecutionPhaseChange, 4, TransitionEvidenceComplete)
	if err != nil {
		t.Fatal(err)
	}
	if original.Phase != contract.ExecutionPhaseInspect || original.Attempts != 3 {
		t.Fatalf("transition mutated original: %+v", original)
	}
	if next.Phase != contract.ExecutionPhaseChange || next.PreviousPhase != contract.ExecutionPhaseInspect || next.EnteredAtRequest != 4 || next.Attempts != 0 || len(next.RequiredEvidence) != 0 {
		t.Fatalf("unexpected next state: %+v", next)
	}
	if _, err := next.Transition(contract.ExecutionPhaseVerify, 3, TransitionEvidenceComplete); err == nil {
		t.Fatal("request sequence moved backwards")
	}
}

func TestRequestPlanValidationAndSerializationDeterministic(t *testing.T) {
	plan := RequestPlan{
		EpochID: "epoch-1", RequestSeq: 2, Phase: contract.ExecutionPhaseInspect,
		Reasoning: contract.ReasoningLow, MaxOutputTokens: 1200,
		ToolPolicy: contract.ToolPolicyCore, ExpectedResult: ExpectedToolCalls,
		Recovery:       RecoveryPolicy{Kind: "one-retry", MaximumAttempts: 1, MaximumOutputTokens: 2400},
		ContextBudget:  SegmentBudget{StablePrefix: 2500, CurrentTask: 500, ActiveEvidence: 1200},
		StopConditions: []StopCondition{{Code: "budget.main.hard", Value: 4}},
		DecisionCodes:  []string{"phase.inspect", "reasoning.low.small", "output.inspect.1200"},
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("request plan serialization changed:\n%s\n%s", first, second)
	}
	invalid := plan
	invalid.MaxOutputTokens = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero output cap accepted")
	}
}

func TestBudgetArithmeticSaturatesAndNeverGrowsRemaining(t *testing.T) {
	limit := BudgetLimit{Soft: 5, Hard: 10, Available: true}
	previous := int64(math.MaxInt64)
	for consumed := int64(-1); consumed <= 12; consumed++ {
		remaining, ok := limit.Remaining(consumed)
		if !ok || remaining < 0 || remaining > 10 {
			t.Fatalf("consumed=%d remaining=%d available=%v", consumed, remaining, ok)
		}
		if consumed >= 0 && remaining > previous {
			t.Fatalf("remaining increased at consumed=%d: %d > %d", consumed, remaining, previous)
		}
		previous = remaining
	}
	maxInt := int(^uint(0) >> 1)
	segments := SegmentBudget{StablePrefix: maxInt, ProjectContext: maxInt, Tail: maxInt}
	if got := segments.Total(); got != math.MaxInt64 {
		t.Fatalf("overflow did not saturate: %d", got)
	}
	if _, ok := (BudgetLimit{}).Remaining(1); ok {
		t.Fatal("unavailable budget returned a numeric remaining value")
	}
}

func TestExecutionBudgetRejectsInvertedOrNegativeLimits(t *testing.T) {
	budget := ExecutionBudget{Mode: "observe", Class: ClassSmall, MainRequests: BudgetLimit{Soft: 4, Hard: 7, Available: true}}
	if err := budget.Validate(); err != nil {
		t.Fatal(err)
	}
	budget.MainRequests = BudgetLimit{Soft: 8, Hard: 7, Available: true}
	if err := budget.Validate(); err == nil {
		t.Fatal("inverted budget accepted")
	}
}
