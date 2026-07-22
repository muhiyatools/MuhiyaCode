package orchestrator

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// BudgetLimit is one soft/hard dimension. A zero limit is explicit; callers
// use Available=false when the provider cannot measure that dimension.
type BudgetLimit struct {
	Soft      int64
	Hard      int64
	Available bool
}

func (limit BudgetLimit) Validate(name string) error {
	if !limit.Available {
		return nil
	}
	if limit.Soft < 0 || limit.Hard < 0 || limit.Soft > limit.Hard {
		return fmt.Errorf("%s budget must satisfy 0 <= soft <= hard", name)
	}
	return nil
}

// Remaining uses saturating subtraction so corrupted/oversized ledgers cannot
// wrap a budget back into a large positive allowance.
func (limit BudgetLimit) Remaining(consumed int64) (int64, bool) {
	if !limit.Available {
		return 0, false
	}
	if consumed <= 0 {
		return limit.Hard, true
	}
	if consumed >= limit.Hard {
		return 0, true
	}
	return limit.Hard - consumed, true
}

type ExecutionBudget struct {
	Mode              string
	Class             TaskClass
	Risky             bool
	MainRequests      BudgetLimit
	AuxRequests       BudgetLimit
	PromptTokens      BudgetLimit
	CacheMissTokens   BudgetLimit
	OutputTokens      BudgetLimit
	InlineObservation BudgetLimit
	ToolCalls         BudgetLimit
	WallTimeMS        BudgetLimit
	OverrideCount     int
}

func (budget ExecutionBudget) Validate() error {
	if budget.Class == "" {
		return errors.New("task class is required")
	}
	for name, limit := range map[string]BudgetLimit{
		"main requests": budget.MainRequests, "aux requests": budget.AuxRequests,
		"prompt tokens": budget.PromptTokens, "cache miss tokens": budget.CacheMissTokens,
		"output tokens": budget.OutputTokens, "inline observation": budget.InlineObservation,
		"tool calls": budget.ToolCalls, "wall time": budget.WallTimeMS,
	} {
		if err := limit.Validate(name); err != nil {
			return err
		}
	}
	if budget.OverrideCount < 0 {
		return errors.New("override count cannot be negative")
	}
	return nil
}

type ExpectedResult string

const (
	ExpectedAnswer       ExpectedResult = "answer"
	ExpectedToolCalls    ExpectedResult = "tool_calls"
	ExpectedVerification ExpectedResult = "verification"
	ExpectedFinal        ExpectedResult = "final"
)

type RecoveryPolicy struct {
	Kind                string
	MaximumAttempts     int
	MaximumOutputTokens int
}

type SegmentBudget struct {
	StablePrefix   int
	ProjectContext int
	PriorCapsules  int
	CurrentTask    int
	ActiveEvidence int
	Tail           int
}

func (budget SegmentBudget) Total() int64 {
	values := []int{budget.StablePrefix, budget.ProjectContext, budget.PriorCapsules, budget.CurrentTask, budget.ActiveEvidence, budget.Tail}
	var total int64
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if int64(value) > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += int64(value)
	}
	return total
}

type StopCondition struct {
	Code  string
	Value int64
}

type RequestPlan struct {
	EpochID         string
	RequestSeq      uint64
	Phase           contract.ExecutionPhase
	Reasoning       contract.ReasoningTier
	MaxOutputTokens int
	ToolPolicy      contract.ToolPolicy
	ExpectedResult  ExpectedResult
	Recovery        RecoveryPolicy
	ContextBudget   SegmentBudget
	StopConditions  []StopCondition
	DecisionCodes   []string
}

type RequestPlanInput struct {
	EpochID         string
	RequestSeq      uint64
	Class           TaskClass
	Risky           bool
	Phase           contract.ExecutionPhase
	UserEffort      contract.EffortLevel
	FailureRecovery bool
	MaxOutputTokens int
}

func BuildRequestPlan(input RequestPlanInput) RequestPlan {
	reasoning, reasoningCodes := selectRequestReasoning(input)
	toolPolicy, expected := phaseRequestShape(input.Phase, input.Class)
	maxOutput := max(1, input.MaxOutputTokens)
	plan := RequestPlan{
		EpochID: input.EpochID, RequestSeq: input.RequestSeq, Phase: input.Phase,
		Reasoning: reasoning, MaxOutputTokens: maxOutput, ToolPolicy: toolPolicy, ExpectedResult: expected,
		Recovery:      RecoveryPolicy{Kind: "bounded-output-retry", MaximumAttempts: 1, MaximumOutputTokens: saturatingDoubleInt(maxOutput)},
		DecisionCodes: []string{"phase." + string(input.Phase)},
	}
	plan.DecisionCodes = append(plan.DecisionCodes, reasoningCodes...)
	plan.DecisionCodes = append(plan.DecisionCodes, fmt.Sprintf("output.%s.%d", input.Phase, maxOutput))
	return plan
}

func selectRequestReasoning(input RequestPlanInput) (contract.ReasoningTier, []string) {
	phaseDefault := contract.ReasoningLow
	if (input.Class == ClassStandard && (input.Phase == contract.ExecutionPhaseChange || input.Phase == contract.ExecutionPhaseRecover)) ||
		((input.Class == ClassLarge || input.Class == ClassEpic) && input.Phase != contract.ExecutionPhaseOrient && input.Phase != contract.ExecutionPhaseFinish) {
		phaseDefault = contract.ReasoningMedium
	}
	if input.Class == ClassEpic && input.Phase == contract.ExecutionPhaseRecover {
		phaseDefault = contract.ReasoningHigh
	}
	selected := lowerReasoning(phaseDefault, ReasoningForEffort(input.UserEffort))
	codes := []string{"reasoning." + string(selected) + ".phase_default"}
	if input.Risky && (input.Phase == contract.ExecutionPhaseChange || input.Phase == contract.ExecutionPhaseVerify || input.Phase == contract.ExecutionPhaseRecover) {
		selected = higherReasoning(selected, contract.ReasoningMedium)
		codes = append(codes, "reasoning.escalated.risk")
	}
	if input.FailureRecovery {
		selected = higherReasoning(selected, contract.ReasoningHigh)
		codes = append(codes, "reasoning.escalated.failure")
	}
	codes[0] = "reasoning." + string(selected) + ".selected"
	return selected, codes
}

func phaseRequestShape(phase contract.ExecutionPhase, class TaskClass) (contract.ToolPolicy, ExpectedResult) {
	switch phase {
	case contract.ExecutionPhaseOrient:
		if class == ClassChat {
			return contract.ToolPolicyNone, ExpectedAnswer
		}
		return contract.ToolPolicyCore, ExpectedToolCalls
	case contract.ExecutionPhaseInspect, contract.ExecutionPhaseChange, contract.ExecutionPhaseRecover:
		return contract.ToolPolicyCore, ExpectedToolCalls
	case contract.ExecutionPhaseVerify:
		return contract.ToolPolicyCore, ExpectedVerification
	case contract.ExecutionPhaseFinish:
		return contract.ToolPolicyNone, ExpectedFinal
	default:
		return contract.ToolPolicyCore, ExpectedToolCalls
	}
}

func reasoningRank(tier contract.ReasoningTier) int {
	switch tier {
	case contract.ReasoningMedium:
		return 1
	case contract.ReasoningHigh:
		return 2
	case contract.ReasoningMax:
		return 3
	default:
		return 0
	}
}

func lowerReasoning(left, right contract.ReasoningTier) contract.ReasoningTier {
	if reasoningRank(left) <= reasoningRank(right) {
		return left
	}
	return right
}

func higherReasoning(left, right contract.ReasoningTier) contract.ReasoningTier {
	if reasoningRank(left) >= reasoningRank(right) {
		return left
	}
	return right
}

func saturatingDoubleInt(value int) int {
	if value <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if value > maxInt/2 {
		return maxInt
	}
	return value * 2
}

func (plan RequestPlan) Validate() error {
	if plan.Phase == "" {
		return errors.New("phase is required")
	}
	if plan.Reasoning == "" {
		return errors.New("reasoning is required")
	}
	if plan.MaxOutputTokens <= 0 {
		return errors.New("max output tokens must be positive")
	}
	if plan.ToolPolicy == "" || plan.ExpectedResult == "" {
		return errors.New("tool policy and expected result are required")
	}
	if plan.Recovery.MaximumAttempts < 0 || plan.Recovery.MaximumOutputTokens < 0 {
		return errors.New("recovery values cannot be negative")
	}
	for _, condition := range plan.StopConditions {
		if condition.Code == "" || condition.Value < 0 {
			return errors.New("invalid stop condition")
		}
	}
	return nil
}

type PhaseTransitionReason string

const (
	TransitionEvidenceComplete PhaseTransitionReason = "evidence_complete"
	TransitionUserChange       PhaseTransitionReason = "user_change"
	TransitionFailureRecovery  PhaseTransitionReason = "failure_recovery"
	TransitionScopeEscalation  PhaseTransitionReason = "scope_escalation"
	TransitionFinalize         PhaseTransitionReason = "finalize"
)

type ExecutionPhaseState struct {
	Phase             contract.ExecutionPhase
	PreviousPhase     contract.ExecutionPhase
	EnteredAtRequest  uint64
	Objective         string
	RequiredEvidence  []string
	SatisfiedEvidence []string
	Attempts          int
	LastFailureClass  string
	TransitionReason  PhaseTransitionReason
	EnteredAt         time.Time
}

func ValidPhaseTransition(from, to contract.ExecutionPhase) bool {
	if to == contract.ExecutionPhaseRecover && from != contract.ExecutionPhaseFinish && from != "" {
		return true
	}
	switch from {
	case "":
		return to == contract.ExecutionPhaseOrient
	case contract.ExecutionPhaseOrient:
		return to == contract.ExecutionPhaseInspect || to == contract.ExecutionPhaseFinish
	case contract.ExecutionPhaseInspect:
		return to == contract.ExecutionPhaseInspect || to == contract.ExecutionPhaseChange || to == contract.ExecutionPhaseVerify || to == contract.ExecutionPhaseFinish
	case contract.ExecutionPhaseChange:
		return to == contract.ExecutionPhaseVerify
	case contract.ExecutionPhaseVerify:
		return to == contract.ExecutionPhaseChange || to == contract.ExecutionPhaseFinish
	case contract.ExecutionPhaseRecover:
		return to == contract.ExecutionPhaseInspect || to == contract.ExecutionPhaseChange || to == contract.ExecutionPhaseVerify || to == contract.ExecutionPhaseFinish
	default:
		return false
	}
}

func (state ExecutionPhaseState) Transition(to contract.ExecutionPhase, requestSeq uint64, reason PhaseTransitionReason) (ExecutionPhaseState, error) {
	if !ValidPhaseTransition(state.Phase, to) {
		return state, fmt.Errorf("invalid execution phase transition %q -> %q", state.Phase, to)
	}
	if requestSeq < state.EnteredAtRequest {
		return state, errors.New("phase request sequence cannot move backwards")
	}
	if reason == "" {
		return state, errors.New("transition reason is required")
	}
	next := state
	next.PreviousPhase = state.Phase
	next.Phase = to
	next.EnteredAtRequest = requestSeq
	next.TransitionReason = reason
	next.EnteredAt = time.Time{}
	next.Attempts = 0
	next.RequiredEvidence = nil
	next.SatisfiedEvidence = nil
	return next, nil
}
