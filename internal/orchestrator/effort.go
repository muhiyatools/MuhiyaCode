// Package orchestrator owns MuhiyaCode's model-facing control loop and the
// token-economy policies that keep it bounded.
package orchestrator

import "github.com/muhiya/muhiyacode/internal/contract"

type EffortProfile struct {
	Level   contract.EffortLevel
	Rank    int
	Summary string
	// MaxAgentRuns is the subagent allowance ceiling (min'd with the task class
	// cap). Scope (feature 009 PL-4): for a DIRECT task it is per task; for an
	// orchestrated pipeline task it applies PER PHASE — task size selects which
	// phases run, effort selects how many subagents each phase may use.
	// (ParallelAgents was removed by feature 014: subagents run one at a
	// time, always — serial chains reuse each other's cached streams and
	// never interleave workspace edits.)
	// Formerly: lets independent run_subagent calls execute concurrently;
	// AutoReview makes the engine nudge one review-subagent pass at the end of
	// substantial file-changing work (feature 008 DG-7). The former
	// Directives/PlanBeforeEdit fields were dead (populated, never consumed) — their
	// intent now lives in the static DELEGATION prompt section (feature 008 R1/D2).
	MaxAgentRuns           int
	AutoReview             bool
	MaxTurns               int
	AgentTurnScale         float64
	KeepFullToolOutputs    int
	TrimmedToolOutputChars int
	ToolOutputCap          int
	CompactThreshold       float64
	Onboarding             bool
	Reasoning              contract.ReasoningTier
	AgentReasoning         contract.ReasoningTier
}

var effortProfiles = map[contract.EffortLevel]EffortProfile{
	contract.EffortLow: {
		Level: contract.EffortLow, Rank: 0,
		Summary:      "Fast, direct work with the lightest useful checks.",
		MaxAgentRuns: 1, MaxTurns: 16, AgentTurnScale: .75,
		KeepFullToolOutputs: 4, TrimmedToolOutputChars: 500, ToolOutputCap: 8_000, CompactThreshold: .80,
		Reasoning: contract.ReasoningLow, AgentReasoning: contract.ReasoningLow,
	},
	contract.EffortMedium: {
		Level: contract.EffortMedium, Rank: 1,
		Summary:      "Balanced speed, cost, and care for everyday engineering.",
		MaxAgentRuns: 2, MaxTurns: 24, AgentTurnScale: 1,
		KeepFullToolOutputs: 5, TrimmedToolOutputChars: 600, ToolOutputCap: 10_000, CompactThreshold: .85,
		Onboarding: true, Reasoning: contract.ReasoningMedium, AgentReasoning: contract.ReasoningLow,
	},
	contract.EffortHigh: {
		Level: contract.EffortHigh, Rank: 2,
		Summary:      "Deep work with broad exploration and thorough checks.",
		MaxAgentRuns: 4, MaxTurns: 36, AgentTurnScale: 1.1,
		KeepFullToolOutputs: 6, TrimmedToolOutputChars: 700, ToolOutputCap: 16_000, CompactThreshold: .87,
		Onboarding: true, Reasoning: contract.ReasoningHigh, AgentReasoning: contract.ReasoningMedium,
	},
	contract.EffortMax: {
		Level: contract.EffortMax, Rank: 3,
		Summary:      "Production-critical migrations, audits, and architecture work.",
		MaxAgentRuns: 8, AutoReview: true, MaxTurns: 48, AgentTurnScale: 1.4,
		KeepFullToolOutputs: 8, TrimmedToolOutputChars: 900, ToolOutputCap: 24_000, CompactThreshold: .90,
		Onboarding: true, Reasoning: contract.ReasoningMax, AgentReasoning: contract.ReasoningHigh,
	},
}

func NormalizeEffort(value string) (contract.EffortLevel, bool) {
	switch value {
	case "low", "min", "minimal":
		return contract.EffortLow, true
	case "medium", "":
		return contract.EffortMedium, true
	case "high":
		return contract.EffortHigh, true
	case "max", "ultra", "xhigh":
		return contract.EffortMax, true
	default:
		return "", false
	}
}

func Profile(level contract.EffortLevel) EffortProfile {
	if profile, ok := effortProfiles[level]; ok {
		return profile
	}
	return effortProfiles[contract.EffortLow]
}

// ReasoningForEffort maps the user-facing reasoning-effort level directly onto
// the reasoning tier sent to the gateway. The mapping is 1:1: the gateway is
// responsible for adapting the level to each provider's supported ladder (for
// DeepSeek, low/medium ride "high" and high/max ride "max").
func ReasoningForEffort(level contract.EffortLevel) contract.ReasoningTier {
	switch level {
	case contract.EffortLow:
		return contract.ReasoningLow
	case contract.EffortMedium:
		return contract.ReasoningMedium
	case contract.EffortHigh:
		return contract.ReasoningHigh
	case contract.EffortMax:
		return contract.ReasoningMax
	default:
		return contract.ReasoningLow
	}
}
