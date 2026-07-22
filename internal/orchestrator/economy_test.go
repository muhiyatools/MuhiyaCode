package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestEconomyProfilesAreVersionedAndRiskAware(t *testing.T) {
	profiles := DefaultEconomyProfiles()
	if profiles.Version != EconomyProfileVersion {
		t.Fatalf("profile version=%q", profiles.Version)
	}
	normal, err := profiles.Lookup(EconomyProfileKey{Class: ClassSmall, RiskLevel: "normal", ProviderFamily: "openai-compatible", Phase: contract.ExecutionPhaseInspect})
	if err != nil {
		t.Fatal(err)
	}
	risky, err := profiles.Lookup(EconomyProfileKey{Class: ClassSmall, RiskLevel: "high", ProviderFamily: "unknown-provider", Phase: contract.ExecutionPhaseInspect})
	if err != nil {
		t.Fatal(err)
	}
	if risky.MainRequestHard <= normal.MainRequestHard || normal.MainRequestSoft > normal.MainRequestHard {
		t.Fatalf("normal=%+v risky=%+v", normal, risky)
	}
}

func TestEconomyBudgetUsesProviderLedgerAndPreservesUnavailable(t *testing.T) {
	prompt, output, miss := 100, 10, 20
	records := []contract.UsageRecord{
		{Stream: contract.UsageStreamMain, PromptTokens: &prompt, CompletionTokens: &output, CacheMissTokens: &miss},
		{Stream: contract.UsageStreamMain, PromptTokens: nil, CompletionTokens: &output, CacheMissTokens: nil},
	}
	evaluation, err := EvaluateEconomyBudget(DefaultEconomyProfiles(), EconomyBudgetRequest{
		Mode: "observe", Key: EconomyProfileKey{Class: ClassTiny, Phase: contract.ExecutionPhaseInspect},
		UsageRecords: records, ToolCalls: 2, Elapsed: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Consumption.MainRequests != 2 || evaluation.Consumption.PromptTokensAvailable || evaluation.Consumption.CacheMissAvailable {
		t.Fatalf("consumption=%+v", evaluation.Consumption)
	}
	if !evaluation.Consumption.OutputTokensAvailable || evaluation.Consumption.OutputTokens != 20 {
		t.Fatalf("output consumption=%+v", evaluation.Consumption)
	}
}

func TestEconomyBudgetReportsSoftAndHardDimensions(t *testing.T) {
	profileSet := DefaultEconomyProfiles()
	profile, err := profileSet.Lookup(EconomyProfileKey{Class: ClassChat, Phase: contract.ExecutionPhaseOrient})
	if err != nil {
		t.Fatal(err)
	}
	prompt, output, miss := int(profile.CumulativePromptHard), 1, 1
	evaluation, err := EvaluateEconomyBudget(profileSet, EconomyBudgetRequest{
		Mode: "balanced", Key: profile.Key,
		UsageRecords: []contract.UsageRecord{{PromptTokens: &prompt, CompletionTokens: &output, CacheMissTokens: &miss}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(evaluation.DecisionCode(), "budget.hard.prompt_tokens") {
		t.Fatalf("decision=%q hard=%v", evaluation.DecisionCode(), evaluation.HardExceeded)
	}
}

func TestBudgetOverridesRequireClosedReasonEvidenceAndRemainOneShot(t *testing.T) {
	reasons := []contract.BudgetOverrideReason{
		contract.BudgetOverrideCorrectness, contract.BudgetOverrideSafety, contract.BudgetOverrideExplicitScope,
		contract.BudgetOverrideRequiredVerification, contract.BudgetOverrideProviderRecovery,
		contract.BudgetOverrideUserSteering, contract.BudgetOverrideMigrationCompatibility,
	}
	for _, reason := range reasons {
		phase := contract.ExecutionPhaseInspect
		if reason == contract.BudgetOverrideRequiredVerification {
			phase = contract.ExecutionPhaseVerify
		}
		request := BudgetOverrideRequest{
			Reason: reason, Phase: phase, HardExceeded: []string{"main_requests"},
			EvidenceCodes: []string{overrideEvidenceCode(reason)},
		}
		decision := EvaluateBudgetOverride(request)
		if !decision.Granted || decision.AdditionalRequests != 1 {
			t.Fatalf("reason=%s decision=%+v", reason, decision)
		}
		request.AlreadyGranted = []contract.BudgetOverrideReason{reason}
		if repeated := EvaluateBudgetOverride(request); repeated.Granted {
			t.Fatalf("reason=%s granted twice: %+v", reason, repeated)
		}
	}
}

func TestBudgetOverridesRejectMissingEvidenceAndUnknownReasons(t *testing.T) {
	missing := EvaluateBudgetOverride(BudgetOverrideRequest{
		Reason: contract.BudgetOverrideSafety, Phase: contract.ExecutionPhaseInspect, HardExceeded: []string{"main_requests"},
	})
	if missing.Granted || !strings.Contains(missing.Code, "missing_evidence") {
		t.Fatalf("missing evidence decision=%+v", missing)
	}
	unknown := EvaluateBudgetOverride(BudgetOverrideRequest{
		Reason: "convenience", Phase: contract.ExecutionPhaseInspect, HardExceeded: []string{"main_requests"}, EvidenceCodes: []string{"convenience"},
	})
	if unknown.Granted || unknown.Code != "override.invalid_reason" {
		t.Fatalf("unknown reason decision=%+v", unknown)
	}
}

func TestVerificationBoundaryAllowsExactlyOneAttributedCheck(t *testing.T) {
	evaluation := EconomyBudgetEvaluation{SoftExceeded: []string{"main_requests"}}
	decision := EvaluateVerificationBoundary(evaluation, contract.ExecutionPhaseChange, true, nil)
	if !decision.AllowRequest || decision.Stop || decision.Override == nil || decision.Code != "override.required_verification" {
		t.Fatalf("first verification decision=%+v", decision)
	}
	repeated := EvaluateVerificationBoundary(evaluation, contract.ExecutionPhaseChange, true, []contract.BudgetOverrideReason{*decision.Override})
	if !repeated.Stop || repeated.AllowRequest || repeated.Code != "override.already_used.required_verification" {
		t.Fatalf("repeated verification decision=%+v", repeated)
	}
}

func TestVerificationBoundaryNarrowsSoftWorkAndStopsHardExploration(t *testing.T) {
	soft := EvaluateVerificationBoundary(EconomyBudgetEvaluation{SoftExceeded: []string{"main_requests"}}, contract.ExecutionPhaseInspect, false, nil)
	if !soft.AllowRequest || soft.Stop || soft.Code != "budget.soft.main_requests" {
		t.Fatalf("soft decision=%+v", soft)
	}
	hard := EvaluateVerificationBoundary(EconomyBudgetEvaluation{HardExceeded: []string{"main_requests"}}, contract.ExecutionPhaseInspect, false, nil)
	if hard.AllowRequest || !hard.Stop || hard.Code != "budget.hard.main_requests" {
		t.Fatalf("hard decision=%+v", hard)
	}
}

func TestProductivityTrackerConvergesAndThenRequiresRecovery(t *testing.T) {
	tracker := NewProductivityTracker()
	phase := contract.ExecutionPhaseInspect
	first := tracker.Record(phase, RequestProductivityOutcome{EvidenceCodes: []string{"file:a:1"}})
	if !first.Productive {
		t.Fatalf("new evidence was not productive: %+v", first)
	}
	duplicate := tracker.Record(phase, RequestProductivityOutcome{EvidenceCodes: []string{"file:a:1"}})
	if duplicate.Productive || duplicate.Converge {
		t.Fatalf("first duplicate decision=%+v", duplicate)
	}
	second := tracker.Record(phase, RequestProductivityOutcome{})
	if !second.Converge || second.RecoverOrFinish || second.ConsecutiveEmpty != 2 {
		t.Fatalf("second empty decision=%+v", second)
	}
	third := tracker.Record(phase, RequestProductivityOutcome{})
	if !third.RecoverOrFinish || third.PhaseEmpty != 3 || third.Code != "productivity.recover_or_finish" {
		t.Fatalf("third empty decision=%+v", third)
	}
}

func TestProductivityTrackerResetsConsecutiveCountOnUsefulAction(t *testing.T) {
	tracker := NewProductivityTracker()
	phase := contract.ExecutionPhaseChange
	tracker.Record(phase, RequestProductivityOutcome{})
	productive := tracker.Record(phase, RequestProductivityOutcome{Mutation: true})
	if !productive.Productive {
		t.Fatalf("mutation decision=%+v", productive)
	}
	after := tracker.Record(phase, RequestProductivityOutcome{})
	if after.ConsecutiveEmpty != 1 || after.Converge {
		t.Fatalf("post-mutation empty decision=%+v", after)
	}
}

func TestToolProductivityDistinguishesChecksFromShellMutations(t *testing.T) {
	check := toolOutcome{Call: contract.NewToolCall("check", "run_shell", `{"command":"go test ./..."}`), Output: "ok"}
	read := toolOutcome{Call: contract.NewToolCall("read", "run_shell", `{"command":"git status --short"}`), Output: " M a.go"}
	mutation := toolOutcome{Call: contract.NewToolCall("write", "edit_file", `{"path":"a.go"}`), Output: "Applied edit"}
	got := toolProductivityOutcome([]toolOutcome{check, read, mutation})
	if !got.Verification || !got.Mutation || len(got.EvidenceCodes) != 1 {
		t.Fatalf("tool productivity=%+v", got)
	}
}

func TestAuxiliaryAdmissionRequiresExpectedValueBudgetAndIsolation(t *testing.T) {
	base := AuxiliaryAdmissionInput{
		Kind: AuxiliaryModelAdvisor, MaterialDecision: true, ExpectedTokenCost: 200,
		ExpectedMainSavings: 500, RemainingRequests: 1, Isolated: true,
	}
	if decision := EvaluateAuxiliaryAdmission(base); !decision.Allowed || decision.Code != "aux.admitted.model_advisor" {
		t.Fatalf("valid admission=%+v", decision)
	}
	tests := []struct {
		name   string
		mutate func(*AuxiliaryAdmissionInput)
	}{
		{name: "local answer exists", mutate: func(input *AuxiliaryAdmissionInput) { input.DeterministicAvailable = true }},
		{name: "low value", mutate: func(input *AuxiliaryAdmissionInput) { input.MaterialDecision = false }},
		{name: "no break even", mutate: func(input *AuxiliaryAdmissionInput) { input.ExpectedMainSavings = input.ExpectedTokenCost }},
		{name: "budget exhausted", mutate: func(input *AuxiliaryAdmissionInput) { input.RemainingRequests = 0 }},
		{name: "not isolated", mutate: func(input *AuxiliaryAdmissionInput) { input.Isolated = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if decision := EvaluateAuxiliaryAdmission(input); decision.Allowed {
				t.Fatalf("inadmissible auxiliary call allowed: %+v", decision)
			}
		})
	}
}
