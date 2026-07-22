package orchestrator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const EconomyProfileVersion = "014-policy-v1"

type EconomyProfileKey struct {
	Class          TaskClass
	RiskLevel      string
	ProviderFamily string
	Phase          contract.ExecutionPhase
}

type EconomyBudgetProfile struct {
	Key                   EconomyProfileKey
	MainRequestSoft       int64
	MainRequestHard       int64
	AuxRequestSoft        int64
	AuxRequestHard        int64
	CumulativePromptSoft  int64
	CumulativePromptHard  int64
	CumulativeOutputSoft  int64
	CumulativeOutputHard  int64
	InlineObservationSoft int64
	InlineObservationHard int64
	DefaultReasoning      contract.ReasoningTier
	DefaultOutputCap      int
	TruncationOutputCap   int
}

type EconomyProfileSet struct {
	Version  string
	Profiles map[EconomyProfileKey]EconomyBudgetProfile
}

func DefaultEconomyProfiles() EconomyProfileSet {
	profiles := make(map[EconomyProfileKey]EconomyBudgetProfile)
	for class, requestLimit := range defaultRequestLimits() {
		for _, phase := range economyPhases() {
			key := EconomyProfileKey{Class: class, RiskLevel: "normal", ProviderFamily: "generic", Phase: phase}
			profiles[key] = defaultEconomyProfile(key, requestLimit)
			riskyKey := key
			riskyKey.RiskLevel = "high"
			riskyLimit := requestLimit
			riskyLimit.Soft++
			riskyLimit.Hard += 2
			profiles[riskyKey] = defaultEconomyProfile(riskyKey, riskyLimit)
		}
	}
	return EconomyProfileSet{Version: EconomyProfileVersion, Profiles: profiles}
}

func (set EconomyProfileSet) Lookup(key EconomyProfileKey) (EconomyBudgetProfile, error) {
	if set.Version == "" || len(set.Profiles) == 0 {
		return EconomyBudgetProfile{}, errors.New("economy profile set is empty")
	}
	key.ProviderFamily = normalizedProviderFamily(key.ProviderFamily)
	key.RiskLevel = normalizedRiskLevel(key.RiskLevel)
	if profile, ok := set.Profiles[key]; ok {
		return profile, nil
	}
	key.ProviderFamily = "generic"
	if profile, ok := set.Profiles[key]; ok {
		return profile, nil
	}
	return EconomyBudgetProfile{}, fmt.Errorf("no economy profile for class=%s risk=%s phase=%s", key.Class, key.RiskLevel, key.Phase)
}

type requestLimitPair struct {
	Soft int64
	Hard int64
}

func defaultRequestLimits() map[TaskClass]requestLimitPair {
	return map[TaskClass]requestLimitPair{
		ClassChat: {Soft: 1, Hard: 2}, ClassTiny: {Soft: 2, Hard: 4}, ClassSmall: {Soft: 4, Hard: 7},
		ClassStandard: {Soft: 8, Hard: 14}, ClassLarge: {Soft: 14, Hard: 24}, ClassEpic: {Soft: 20, Hard: 36},
	}
}

func economyPhases() []contract.ExecutionPhase {
	return []contract.ExecutionPhase{
		contract.ExecutionPhaseOrient, contract.ExecutionPhaseInspect, contract.ExecutionPhaseChange,
		contract.ExecutionPhaseVerify, contract.ExecutionPhaseFinish, contract.ExecutionPhaseRecover,
	}
}

func defaultEconomyProfile(key EconomyProfileKey, requests requestLimitPair) EconomyBudgetProfile {
	promptHard := requests.Hard * 24_000
	outputHard := requests.Hard * 2_000
	return EconomyBudgetProfile{
		Key: key, MainRequestSoft: requests.Soft, MainRequestHard: requests.Hard,
		AuxRequestSoft: 0, AuxRequestHard: 1, CumulativePromptSoft: promptHard * 3 / 4, CumulativePromptHard: promptHard,
		CumulativeOutputSoft: outputHard * 3 / 4, CumulativeOutputHard: outputHard,
		InlineObservationSoft: 6_000, InlineObservationHard: 12_000,
		DefaultReasoning: contract.ReasoningLow, DefaultOutputCap: 2_000, TruncationOutputCap: 8_000,
	}
}

func normalizedRiskLevel(level string) string {
	if strings.EqualFold(strings.TrimSpace(level), "high") || strings.EqualFold(strings.TrimSpace(level), "critical") {
		return "high"
	}
	return "normal"
}

func normalizedProviderFamily(family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	if family == "" || family == "openai-compatible" {
		return "generic"
	}
	return family
}

type EconomyConsumption struct {
	MainRequests            int64
	AuxRequests             int64
	PromptTokens            int64
	PromptTokensAvailable   bool
	CacheMissTokens         int64
	CacheMissAvailable      bool
	OutputTokens            int64
	OutputTokensAvailable   bool
	InlineObservationTokens int64
	ToolCalls               int64
	WallTimeMS              int64
}

type EconomyBudgetRequest struct {
	Mode                    string
	Key                     EconomyProfileKey
	UsageRecords            []contract.UsageRecord
	InlineObservationTokens int64
	ToolCalls               int64
	Elapsed                 time.Duration
}

type EconomyBudgetEvaluation struct {
	Profile      EconomyBudgetProfile
	Budget       ExecutionBudget
	Consumption  EconomyConsumption
	SoftExceeded []string
	HardExceeded []string
}

type BudgetOverrideRequest struct {
	Reason         contract.BudgetOverrideReason
	Phase          contract.ExecutionPhase
	HardExceeded   []string
	EvidenceCodes  []string
	AlreadyGranted []contract.BudgetOverrideReason
}

type BudgetOverrideDecision struct {
	Granted            bool
	Code               string
	AdditionalRequests int
	Reason             contract.BudgetOverrideReason
}

type VerificationBoundaryDecision struct {
	AllowRequest bool
	Stop         bool
	Code         string
	Override     *contract.BudgetOverrideReason
}

type RequestProductivityOutcome struct {
	EvidenceCodes        []string
	Mutation             bool
	Verification         bool
	RequiredUserDecision bool
	FinalAnswer          bool
}

type ProductivityDecision struct {
	Productive       bool
	Converge         bool
	RecoverOrFinish  bool
	ConsecutiveEmpty int
	PhaseEmpty       int
	Code             string
}

type ProductivityTracker struct {
	consecutiveEmpty int
	phaseEmpty       map[contract.ExecutionPhase]int
	seenEvidence     map[string]struct{}
}

func NewProductivityTracker() *ProductivityTracker {
	return &ProductivityTracker{
		phaseEmpty:   make(map[contract.ExecutionPhase]int),
		seenEvidence: make(map[string]struct{}),
	}
}

func (tracker *ProductivityTracker) Record(phase contract.ExecutionPhase, outcome RequestProductivityOutcome) ProductivityDecision {
	productive := outcome.Mutation || outcome.Verification || outcome.RequiredUserDecision || outcome.FinalAnswer
	for _, code := range outcome.EvidenceCodes {
		if _, exists := tracker.seenEvidence[code]; exists {
			continue
		}
		tracker.seenEvidence[code] = struct{}{}
		productive = true
	}
	if productive {
		tracker.consecutiveEmpty = 0
		return ProductivityDecision{Productive: true, Code: "productivity.productive"}
	}
	tracker.consecutiveEmpty++
	tracker.phaseEmpty[phase]++
	decision := ProductivityDecision{
		ConsecutiveEmpty: tracker.consecutiveEmpty,
		PhaseEmpty:       tracker.phaseEmpty[phase],
		Code:             "productivity.unproductive",
	}
	if decision.ConsecutiveEmpty >= 2 {
		decision.Converge = true
		decision.Code = "productivity.converge"
	}
	if decision.PhaseEmpty >= 3 {
		decision.RecoverOrFinish = true
		decision.Code = "productivity.recover_or_finish"
	}
	return decision
}

type AuxiliaryKind string

const (
	AuxiliaryModelAdvisor AuxiliaryKind = "model_advisor"
	AuxiliaryOnboarding   AuxiliaryKind = "onboarding"
	AuxiliaryCompaction   AuxiliaryKind = "compaction"
)

type AuxiliaryAdmissionInput struct {
	Kind                   AuxiliaryKind
	DeterministicAvailable bool
	MaterialDecision       bool
	ExpectedTokenCost      int64
	ExpectedMainSavings    int64
	RemainingRequests      int64
	Isolated               bool
}

type AuxiliaryAdmissionDecision struct {
	Allowed bool
	Code    string
}

func EvaluateAuxiliaryAdmission(input AuxiliaryAdmissionInput) AuxiliaryAdmissionDecision {
	if input.DeterministicAvailable {
		return AuxiliaryAdmissionDecision{Code: "aux.denied.local_available"}
	}
	if !input.MaterialDecision {
		return AuxiliaryAdmissionDecision{Code: "aux.denied.low_value"}
	}
	if input.ExpectedTokenCost <= 0 || input.ExpectedMainSavings <= input.ExpectedTokenCost {
		return AuxiliaryAdmissionDecision{Code: "aux.denied.no_break_even"}
	}
	if input.RemainingRequests <= 0 {
		return AuxiliaryAdmissionDecision{Code: "aux.denied.budget"}
	}
	if !input.Isolated {
		return AuxiliaryAdmissionDecision{Code: "aux.denied.not_isolated"}
	}
	return AuxiliaryAdmissionDecision{Allowed: true, Code: "aux.admitted." + string(input.Kind)}
}

func EvaluateBudgetOverride(request BudgetOverrideRequest) BudgetOverrideDecision {
	decision := BudgetOverrideDecision{Reason: request.Reason}
	if !request.Reason.Valid() {
		decision.Code = "override.invalid_reason"
		return decision
	}
	if len(request.HardExceeded) == 0 {
		decision.Code = "override.not_required"
		return decision
	}
	if containsOverrideReason(request.AlreadyGranted, request.Reason) {
		decision.Code = "override.already_used." + string(request.Reason)
		return decision
	}
	requiredEvidence := overrideEvidenceCode(request.Reason)
	if !containsString(request.EvidenceCodes, requiredEvidence) {
		decision.Code = "override.missing_evidence." + string(request.Reason)
		return decision
	}
	if request.Reason == contract.BudgetOverrideRequiredVerification && request.Phase != contract.ExecutionPhaseVerify && request.Phase != contract.ExecutionPhaseChange {
		decision.Code = "override.invalid_phase.required_verification"
		return decision
	}
	decision.Granted = true
	decision.AdditionalRequests = 1
	decision.Code = "override." + string(request.Reason)
	return decision
}

func EvaluateVerificationBoundary(evaluation EconomyBudgetEvaluation, phase contract.ExecutionPhase, needsVerification bool, granted []contract.BudgetOverrideReason) VerificationBoundaryDecision {
	exceeded := append(append([]string(nil), evaluation.HardExceeded...), evaluation.SoftExceeded...)
	if len(exceeded) == 0 {
		return VerificationBoundaryDecision{AllowRequest: true, Code: evaluation.DecisionCode()}
	}
	if !needsVerification {
		if len(evaluation.HardExceeded) > 0 {
			return VerificationBoundaryDecision{Stop: true, Code: evaluation.DecisionCode()}
		}
		return VerificationBoundaryDecision{AllowRequest: true, Code: evaluation.DecisionCode()}
	}
	reason := contract.BudgetOverrideRequiredVerification
	override := EvaluateBudgetOverride(BudgetOverrideRequest{
		Reason: reason, Phase: phase, HardExceeded: exceeded,
		EvidenceCodes: []string{"required_verification"}, AlreadyGranted: granted,
	})
	if !override.Granted {
		return VerificationBoundaryDecision{Stop: true, Code: override.Code}
	}
	return VerificationBoundaryDecision{AllowRequest: true, Code: override.Code, Override: &reason}
}

func overrideEvidenceCode(reason contract.BudgetOverrideReason) string {
	return map[contract.BudgetOverrideReason]string{
		contract.BudgetOverrideCorrectness:            "correctness_required",
		contract.BudgetOverrideSafety:                 "safety_required",
		contract.BudgetOverrideExplicitScope:          "explicit_scope",
		contract.BudgetOverrideRequiredVerification:   "required_verification",
		contract.BudgetOverrideProviderRecovery:       "provider_recovery_eligible",
		contract.BudgetOverrideUserSteering:           "user_steering",
		contract.BudgetOverrideMigrationCompatibility: "migration_compatibility",
	}[reason]
}

func containsOverrideReason(reasons []contract.BudgetOverrideReason, target contract.BudgetOverrideReason) bool {
	for _, reason := range reasons {
		if reason == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type currentTaskBudgetInput struct {
	UsageRecordStart int
	Assessment       Assessment
	Phase            contract.ExecutionPhase
	CurrentUsage     contract.Usage
	ToolCalls        int
	Elapsed          time.Duration
}

type taskBudgetBoundaryInput struct {
	UsageRecordStart int
	Assessment       Assessment
	Phase            contract.ExecutionPhase
	ToolCalls        int
	Elapsed          time.Duration
}

func (e *Engine) currentTaskBudget(input currentTaskBudgetInput) (EconomyBudgetEvaluation, error) {
	records := e.taskUsageRecords(input.UsageRecordStart)
	current := usageRecord(usageRecordInput{model: e.settings.Provider.ActiveModelID, stream: contract.UsageStreamMain, usage: input.CurrentUsage})
	records = append(records, current)
	return e.evaluateTaskBudget(records, input.Assessment, input.Phase, input.ToolCalls, input.Elapsed)
}

func (e *Engine) taskBudgetAtBoundary(input taskBudgetBoundaryInput) (EconomyBudgetEvaluation, error) {
	return e.evaluateTaskBudget(e.taskUsageRecords(input.UsageRecordStart), input.Assessment, input.Phase, input.ToolCalls, input.Elapsed)
}

func (e *Engine) taskUsageRecords(start int) []contract.UsageRecord {
	records := e.UsageRecords()
	if start > len(records) {
		return nil
	}
	return records[start:]
}

func (e *Engine) evaluateTaskBudget(records []contract.UsageRecord, assessment Assessment, phase contract.ExecutionPhase, toolCalls int, elapsed time.Duration) (EconomyBudgetEvaluation, error) {
	risk := "normal"
	if assessment.Risky {
		risk = "high"
	}
	return EvaluateEconomyBudget(DefaultEconomyProfiles(), EconomyBudgetRequest{
		Mode:         e.settings.TokenEconomyMode,
		Key:          EconomyProfileKey{Class: assessment.Class, RiskLevel: risk, ProviderFamily: e.settings.Provider.Type, Phase: phase},
		UsageRecords: records, ToolCalls: int64(toolCalls), Elapsed: elapsed,
	})
}

func (e *Engine) remainingAuxiliaryRequests(assessment Assessment, phase contract.ExecutionPhase) int64 {
	risk := "normal"
	if assessment.Risky {
		risk = "high"
	}
	profile, err := DefaultEconomyProfiles().Lookup(EconomyProfileKey{
		Class: assessment.Class, RiskLevel: risk, ProviderFamily: e.settings.Provider.Type, Phase: phase,
	})
	if err != nil {
		return 0
	}
	aggregate := e.UsageAggregate()
	consumed := int64(aggregate.AuxRequests + aggregate.SubagentRequests)
	return max(int64(0), profile.AuxRequestHard-consumed)
}

func EvaluateEconomyBudget(profileSet EconomyProfileSet, request EconomyBudgetRequest) (EconomyBudgetEvaluation, error) {
	profile, err := profileSet.Lookup(request.Key)
	if err != nil {
		return EconomyBudgetEvaluation{}, err
	}
	consumption := consumptionFromRecords(request.UsageRecords)
	consumption.InlineObservationTokens = max(int64(0), request.InlineObservationTokens)
	consumption.ToolCalls = max(int64(0), request.ToolCalls)
	consumption.WallTimeMS = max(int64(0), request.Elapsed.Milliseconds())
	budget := executionBudgetFromProfile(request.Mode, profile)
	evaluation := EconomyBudgetEvaluation{Profile: profile, Budget: budget, Consumption: consumption}
	evaluation.SoftExceeded, evaluation.HardExceeded = exceededBudgetDimensions(budget, consumption)
	return evaluation, budget.Validate()
}

func consumptionFromRecords(records []contract.UsageRecord) EconomyConsumption {
	consumption := EconomyConsumption{PromptTokensAvailable: true, CacheMissAvailable: true, OutputTokensAvailable: true}
	providerRows := 0
	for _, record := range records {
		if record.RetryOf == nil {
			if record.Stream == contract.UsageStreamAux || record.Stream == contract.UsageStreamSubagent {
				consumption.AuxRequests++
			} else {
				consumption.MainRequests++
			}
		}
		providerRows++
		accumulateProviderTokens(&consumption, record)
	}
	if providerRows == 0 {
		consumption.PromptTokensAvailable = false
		consumption.CacheMissAvailable = false
		consumption.OutputTokensAvailable = false
	}
	return consumption
}

func accumulateProviderTokens(consumption *EconomyConsumption, record contract.UsageRecord) {
	if record.PromptTokens == nil {
		consumption.PromptTokensAvailable = false
	} else {
		consumption.PromptTokens += int64(*record.PromptTokens)
	}
	if record.CacheMissTokens == nil {
		consumption.CacheMissAvailable = false
	} else {
		consumption.CacheMissTokens += int64(*record.CacheMissTokens)
	}
	if record.CompletionTokens == nil {
		consumption.OutputTokensAvailable = false
	} else {
		consumption.OutputTokens += int64(*record.CompletionTokens)
	}
}

func executionBudgetFromProfile(mode string, profile EconomyBudgetProfile) ExecutionBudget {
	return ExecutionBudget{
		Mode: mode, Class: profile.Key.Class, Risky: profile.Key.RiskLevel == "high",
		MainRequests:      BudgetLimit{Soft: profile.MainRequestSoft, Hard: profile.MainRequestHard, Available: true},
		AuxRequests:       BudgetLimit{Soft: profile.AuxRequestSoft, Hard: profile.AuxRequestHard, Available: true},
		PromptTokens:      BudgetLimit{Soft: profile.CumulativePromptSoft, Hard: profile.CumulativePromptHard, Available: true},
		CacheMissTokens:   BudgetLimit{Soft: profile.CumulativePromptSoft, Hard: profile.CumulativePromptHard, Available: true},
		OutputTokens:      BudgetLimit{Soft: profile.CumulativeOutputSoft, Hard: profile.CumulativeOutputHard, Available: true},
		InlineObservation: BudgetLimit{Soft: profile.InlineObservationSoft, Hard: profile.InlineObservationHard, Available: true},
		ToolCalls:         BudgetLimit{Soft: profile.MainRequestSoft * 3, Hard: profile.MainRequestHard * 3, Available: true},
		WallTimeMS:        BudgetLimit{Soft: profile.MainRequestSoft * 120_000, Hard: profile.MainRequestHard * 120_000, Available: true},
	}
}

func exceededBudgetDimensions(budget ExecutionBudget, consumption EconomyConsumption) ([]string, []string) {
	checks := []struct {
		name      string
		limit     BudgetLimit
		consumed  int64
		available bool
	}{
		{"main_requests", budget.MainRequests, consumption.MainRequests, true},
		{"aux_requests", budget.AuxRequests, consumption.AuxRequests, true},
		{"prompt_tokens", budget.PromptTokens, consumption.PromptTokens, consumption.PromptTokensAvailable},
		{"cache_miss_tokens", budget.CacheMissTokens, consumption.CacheMissTokens, consumption.CacheMissAvailable},
		{"output_tokens", budget.OutputTokens, consumption.OutputTokens, consumption.OutputTokensAvailable},
		{"inline_observation", budget.InlineObservation, consumption.InlineObservationTokens, true},
		{"tool_calls", budget.ToolCalls, consumption.ToolCalls, true},
		{"wall_time_ms", budget.WallTimeMS, consumption.WallTimeMS, true},
	}
	var soft, hard []string
	for _, check := range checks {
		if !check.available || !check.limit.Available {
			continue
		}
		if check.consumed > 0 && check.consumed >= check.limit.Hard {
			hard = append(hard, check.name)
		} else if check.consumed > 0 && check.consumed >= check.limit.Soft {
			soft = append(soft, check.name)
		}
	}
	sort.Strings(soft)
	sort.Strings(hard)
	return soft, hard
}

func (evaluation EconomyBudgetEvaluation) DecisionCode() string {
	if len(evaluation.HardExceeded) > 0 {
		return "budget.hard." + strings.Join(evaluation.HardExceeded, "+")
	}
	if len(evaluation.SoftExceeded) > 0 {
		return "budget.soft." + strings.Join(evaluation.SoftExceeded, "+")
	}
	return "budget.within"
}
