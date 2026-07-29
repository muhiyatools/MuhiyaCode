// Package orchestrator owns MuhiyaCode's model-facing control loop and the
// token-economy policies that keep it bounded.
package orchestrator

import "github.com/muhiya/muhiyacode/internal/contract"

type EffortProfile struct {
	Level                  contract.EffortLevel
	Rank                   int
	Summary                string
	MaxTurns               int
	KeepFullToolOutputs    int
	TrimmedToolOutputChars int
	ToolOutputCap          int
	CompactThreshold       float64
	Reasoning              contract.ReasoningTier
}

var effortProfiles = map[contract.EffortLevel]EffortProfile{
	contract.EffortLow: {
		Level: contract.EffortLow, Rank: 0,
		Summary:             "Fast, direct work with the lightest useful checks.",
		MaxTurns:            16,
		KeepFullToolOutputs: 4, TrimmedToolOutputChars: 500, ToolOutputCap: 8_000, CompactThreshold: .80,
		Reasoning: contract.ReasoningLow,
	},
	contract.EffortMedium: {
		Level: contract.EffortMedium, Rank: 1,
		Summary:             "Balanced speed, cost, and care for everyday engineering.",
		MaxTurns:            24,
		KeepFullToolOutputs: 5, TrimmedToolOutputChars: 600, ToolOutputCap: 10_000, CompactThreshold: .85,
		Reasoning: contract.ReasoningMedium,
	},
	contract.EffortHigh: {
		Level: contract.EffortHigh, Rank: 2,
		Summary:             "Deep work with broad exploration and thorough checks.",
		MaxTurns:            36,
		KeepFullToolOutputs: 6, TrimmedToolOutputChars: 700, ToolOutputCap: 16_000, CompactThreshold: .87,
		Reasoning: contract.ReasoningHigh,
	},
	contract.EffortMax: {
		Level: contract.EffortMax, Rank: 3,
		Summary:             "Production-critical migrations, audits, and architecture work.",
		MaxTurns:            48,
		KeepFullToolOutputs: 8, TrimmedToolOutputChars: 900, ToolOutputCap: 24_000, CompactThreshold: .90,
		Reasoning: contract.ReasoningMax,
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

// ReasoningForTask prevents a high session setting from making trivial work
// reason like an architecture migration. Effort remains authoritative within
// the ceiling appropriate to the classified task.
func ReasoningForTask(class TaskClass, level contract.EffortLevel) contract.ReasoningTier {
	requested := ReasoningForEffort(level)
	ceiling := contract.ReasoningMax
	switch class {
	case ClassChat, ClassTiny:
		ceiling = contract.ReasoningLow
	case ClassSmall:
		ceiling = contract.ReasoningMedium
	case ClassStandard:
		ceiling = contract.ReasoningHigh
	}
	if reasoningRank(requested) > reasoningRank(ceiling) {
		return ceiling
	}
	return requested
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
