// Package orchestrator owns MuhiyaCode's model-facing control loop and the
// token-economy policies that keep it bounded.
package orchestrator

import "github.com/muhiya/muhiyacode/internal/contract"

type EffortProfile struct {
	Level                  contract.EffortLevel
	Rank                   int
	Summary                string
	Directives             []string
	MaxAgentRuns           int
	ParallelAgents         bool
	AutoReview             bool
	PlanBeforeEdit         bool
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
		Directives:   []string{"Act directly; read only files required for the change.", "Delegate at most one subagent run, and only when it clearly saves context.", "Run at most one targeted check when risk justifies it.", "Do not expand scope."},
		MaxAgentRuns: 1, MaxTurns: 16, AgentTurnScale: .75,
		KeepFullToolOutputs: 4, TrimmedToolOutputChars: 500, ToolOutputCap: 8_000, CompactThreshold: .80,
		Reasoning: contract.ReasoningLow, AgentReasoning: contract.ReasoningLow,
	},
	contract.EffortMedium: {
		Level: contract.EffortMedium, Rank: 1,
		Summary:      "Balanced speed, cost, and care for everyday engineering.",
		Directives:   []string{"Work directly unless delegation clearly saves context.", "Run the smallest meaningful check and fix failures.", "Keep changes focused on the request."},
		MaxAgentRuns: 2, PlanBeforeEdit: true, MaxTurns: 24, AgentTurnScale: 1,
		KeepFullToolOutputs: 5, TrimmedToolOutputChars: 600, ToolOutputCap: 10_000, CompactThreshold: .85,
		Onboarding: true, Reasoning: contract.ReasoningMedium, AgentReasoning: contract.ReasoningLow,
	},
	contract.EffortHigh: {
		Level: contract.EffortHigh, Rank: 2,
		Summary:      "Deep work with parallel exploration and thorough checks.",
		Directives:   []string{"Think through edge cases before editing.", "Delegate independent exploration or isolated subtasks when it saves main context.", "Run the full relevant checks."},
		MaxAgentRuns: 4, ParallelAgents: true, PlanBeforeEdit: true, MaxTurns: 36, AgentTurnScale: 1.1,
		KeepFullToolOutputs: 6, TrimmedToolOutputChars: 700, ToolOutputCap: 16_000, CompactThreshold: .87,
		Onboarding: true, Reasoning: contract.ReasoningHigh, AgentReasoning: contract.ReasoningMedium,
	},
	contract.EffortMax: {
		Level: contract.EffortMax, Rank: 3,
		Summary:      "Production-critical migrations, audits, and architecture work.",
		Directives:   []string{"Design before implementation and maintain a real plan.", "Use parallel agents for independent work and a review pass.", "Validate exhaustively and review the complete diff."},
		MaxAgentRuns: 8, ParallelAgents: true, AutoReview: true, PlanBeforeEdit: true, MaxTurns: 48, AgentTurnScale: 1.4,
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
