package command

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Benchmark summary emission (feature 011 T004a, contracts/benchmark-run.md).
// When MUHIYA_BENCH_JSON=1, one-shot mode prints — as its LAST stdout line —
// one machine-readable JSON object the benchmark runner consumes. This is the
// authoritative measurement source: everything in it is provider-reported (or
// explicitly flagged estimated); the runner never scrapes rendered output.

type benchUsage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	CacheReadTokens  int  `json:"cache_read_tokens"`
	CacheMissTokens  int  `json:"cache_miss_tokens"`
	CacheWriteTokens int  `json:"cache_write_tokens"`
	Reported         bool `json:"reported"`
}

type benchReview struct {
	Tier        string `json:"tier"`
	Rationale   string `json:"rationale"`
	SpendTokens int    `json:"spend_tokens"`
	CeilingHit  bool   `json:"ceiling_hit"`
}

type benchViolations struct {
	TerminalReadWhenToolExists int `json:"terminal_read_when_tool_exists"`
	DuplicateReads             int `json:"duplicate_reads"`
}

type benchPairing struct {
	Model              string   `json:"model"`
	Pin                string   `json:"pin"`
	SteadyStateHitRate *float64 `json:"steady_state_hit_rate,omitempty"`
	Reported           bool     `json:"reported"`
	PromptTokens       int      `json:"prompt_tokens,omitempty"`
	CompletionTokens   int      `json:"completion_tokens,omitempty"`
}

type benchSummary struct {
	TaskClass     string          `json:"task_class"`
	Completed     bool            `json:"completed"`
	Turns         int             `json:"turns"`
	Usage         benchUsage      `json:"usage"`
	CostUSD       float64         `json:"cost_usd"`
	CostEstimated bool            `json:"cost_estimated"`
	Review        benchReview     `json:"review"`
	Violations    benchViolations `json:"violations"`
	PerPairing    []benchPairing  `json:"per_pairing,omitempty"`
	// ReadGate counts the dispatch gate's outcomes for this task.
	ReadGate struct {
		Denied int `json:"denied,omitempty"`
		Waived int `json:"waived,omitempty"`
		Exempt int `json:"exempt,omitempty"`
	} `json:"read_gate,omitzero"`
}

// emitBenchSummary renders the summary from the task's completion stats. A task
// with no review decision reports tier "none" (distinct from a gated "skip").
func emitBenchSummary(w io.Writer, stats contract.TaskStats, runErr error) {
	usage := benchUsage{
		PromptTokens:     stats.Usage.PromptTokens,
		CompletionTokens: stats.Usage.CompletionTokens,
		Reported:         stats.Usage.PromptTokensAvailable || stats.Usage.CacheReadTokens != nil,
	}
	if stats.Usage.CacheReadTokens != nil {
		usage.CacheReadTokens = *stats.Usage.CacheReadTokens
	}
	if stats.Usage.CacheMissTokens != nil {
		usage.CacheMissTokens = *stats.Usage.CacheMissTokens
	}
	review := benchReview{Tier: "none"}
	if stats.ReviewTier != "" {
		review.Tier = stats.ReviewTier
		review.Rationale = stats.ReviewRationale
		review.CeilingHit = stats.ReviewCeilingHit
		if review.Tier != "skip" {
			// Feature 012 R-D13: exact per-pin attribution — the :sub:review
			// pairing's provider-reported spend (retires the feature-011
			// whole-task approximation). Falls back to the old upper bound only
			// when no review pairing reported.
			for _, pairing := range stats.PerPairing {
				if strings.HasSuffix(pairing.Pin, ":sub:review") || pairing.Pin == ":sub:review" {
					review.SpendTokens += pairing.PromptTokens + pairing.CompletionTokens
				}
			}
			if review.SpendTokens == 0 {
				review.SpendTokens = stats.AgentUsage.TotalTokens
				if review.SpendTokens == 0 {
					review.SpendTokens = stats.AgentUsage.PromptTokens + stats.AgentUsage.CompletionTokens
				}
			}
		}
	}
	summary := benchSummary{
		TaskClass: stats.TaskClass,
		Completed: runErr == nil && stats.StopCause == "" && stats.TerminatedReason == "",
		Turns:     stats.Turns,
		Usage:     usage,
		Review:    review,
		Violations: benchViolations{
			TerminalReadWhenToolExists: stats.TerminalReadViolations,
			DuplicateReads:             stats.DuplicateReadViolations,
		},
	}
	if stats.CreditsUSD != nil {
		summary.CostUSD = *stats.CreditsUSD
		summary.CostEstimated = stats.CreditsEstimated
	}
	for _, pairing := range stats.PerPairing {
		summary.PerPairing = append(summary.PerPairing, benchPairing{
			Model: pairing.Model, Pin: pairing.Pin,
			SteadyStateHitRate: pairing.SteadyStateHitRate, Reported: pairing.Reported,
			PromptTokens: pairing.PromptTokens, CompletionTokens: pairing.CompletionTokens,
		})
	}
	payload, err := json.Marshal(map[string]benchSummary{"muhiya_bench": summary})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(payload))
}
