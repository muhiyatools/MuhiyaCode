package contract

// ExecutionPhase is the deterministic next-decision state used by the local
// economy governor. It is runtime policy, never model-authored prose.
type ExecutionPhase string

const (
	ExecutionPhaseOrient  ExecutionPhase = "orient"
	ExecutionPhaseInspect ExecutionPhase = "inspect"
	ExecutionPhaseChange  ExecutionPhase = "change"
	ExecutionPhaseVerify  ExecutionPhase = "verify"
	ExecutionPhaseFinish  ExecutionPhase = "finish"
	ExecutionPhaseRecover ExecutionPhase = "recover"
)

// BudgetOverrideReason is deliberately closed: convenience or a model's
// preference cannot silently enlarge a task budget.
type BudgetOverrideReason string

const (
	BudgetOverrideCorrectness            BudgetOverrideReason = "correctness"
	BudgetOverrideSafety                 BudgetOverrideReason = "safety"
	BudgetOverrideExplicitScope          BudgetOverrideReason = "explicit_scope"
	BudgetOverrideRequiredVerification   BudgetOverrideReason = "required_verification"
	BudgetOverrideProviderRecovery       BudgetOverrideReason = "provider_recovery"
	BudgetOverrideUserSteering           BudgetOverrideReason = "user_steering"
	BudgetOverrideMigrationCompatibility BudgetOverrideReason = "migration_compatibility"
)

func (reason BudgetOverrideReason) Valid() bool {
	switch reason {
	case BudgetOverrideCorrectness, BudgetOverrideSafety, BudgetOverrideExplicitScope,
		BudgetOverrideRequiredVerification, BudgetOverrideProviderRecovery,
		BudgetOverrideUserSteering, BudgetOverrideMigrationCompatibility:
		return true
	default:
		return false
	}
}

type ToolPolicy string

const (
	ToolPolicyNone      ToolPolicy = "none"
	ToolPolicyCore      ToolPolicy = "core"
	ToolPolicyBroker    ToolPolicy = "broker"
	ToolPolicyAllLoaded ToolPolicy = "all_loaded"
)

type EconomyDecisionKind string

const (
	EconomyKeepContext       EconomyDecisionKind = "keep_context"
	EconomyNewEpoch          EconomyDecisionKind = "new_epoch"
	EconomyEvictObservation  EconomyDecisionKind = "evict_observation"
	EconomyCompact           EconomyDecisionKind = "compact"
	EconomyLoadTool          EconomyDecisionKind = "load_tool"
	EconomyEscalateReasoning EconomyDecisionKind = "escalate_reasoning"
	EconomyStop              EconomyDecisionKind = "stop"
)

// MeasurementKind prevents local estimates from being rendered as provider
// truth. Unknown is the zero value for backward-compatible decoding.
type MeasurementKind string

const (
	MeasurementUnknown            MeasurementKind = "unknown"
	MeasurementProviderExact      MeasurementKind = "exact-provider"
	MeasurementTokenizerExact     MeasurementKind = "exact-tokenizer"
	MeasurementCalibratedEstimate MeasurementKind = "calibrated-estimate"
)
