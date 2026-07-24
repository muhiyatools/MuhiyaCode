package command

import (
	"context"
	"encoding/json"
	"errors"
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
	ReasoningTokens  int  `json:"reasoning_tokens,omitempty"`
	TotalTokens      int  `json:"total_tokens"`
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
	ReasoningTokens    int      `json:"reasoning_tokens,omitempty"`
}

type benchSummary struct {
	Status           contract.BenchmarkStatus    `json:"status"`
	StopCause        string                      `json:"stop_cause,omitempty"`
	TerminatedReason string                      `json:"terminated_reason,omitempty"`
	TaskClass        string                      `json:"task_class"`
	Completed        bool                        `json:"completed"`
	SessionID        string                      `json:"session_id,omitempty"`
	TrajectoryPath   string                      `json:"trajectory_path,omitempty"`
	DurationMS       int64                       `json:"duration_ms"`
	Turns            int                         `json:"turns"`
	ToolCalls        int                         `json:"tool_calls"`
	ChecksRun        int                         `json:"checks_run"`
	FilesChanged     []string                    `json:"files_changed,omitempty"`
	Usage            benchUsage                  `json:"usage"`
	CostUSD          float64                     `json:"cost_usd"`
	CostEstimated    bool                        `json:"cost_estimated"`
	Review           benchReview                 `json:"review"`
	Verification     contract.VerificationResult `json:"verification"`
	Errors           []string                    `json:"errors,omitempty"`
	Violations       benchViolations             `json:"violations"`
	PerPairing       []benchPairing              `json:"per_pairing,omitempty"`
	Invalidations    []contract.InvalidationEvent `json:"invalidations,omitempty"`
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
		TotalTokens:      stats.Usage.TotalTokens,
		Reported:         stats.Usage.PromptTokensAvailable || stats.Usage.CacheReadTokens != nil,
	}
	if stats.Usage.CacheReadTokens != nil {
		usage.CacheReadTokens = *stats.Usage.CacheReadTokens
	}
	if stats.Usage.CacheMissTokens != nil {
		usage.CacheMissTokens = *stats.Usage.CacheMissTokens
	}
	if stats.Usage.ReasoningTokens != nil {
		usage.ReasoningTokens = *stats.Usage.ReasoningTokens
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
		Status:           benchmarkStatus(stats, runErr),
		StopCause:        stats.StopCause,
		TerminatedReason: stats.TerminatedReason,
		TaskClass:        stats.TaskClass,
		Completed:        runErr == nil && stats.StopCause == "" && stats.TerminatedReason == "",
		DurationMS:       stats.DurationMS,
		Turns:            stats.Turns,
		ToolCalls:        stats.ToolCalls,
		ChecksRun:        stats.ChecksRun,
		FilesChanged:     append([]string(nil), stats.FilesChanged...),
		Usage:            usage,
		Review:           review,
		Verification:     stats.Verification,
		Violations: benchViolations{
			TerminalReadWhenToolExists: stats.TerminalReadViolations,
			DuplicateReads:             stats.DuplicateReadViolations,
		},
		Invalidations: stats.Invalidations,
	}
	if runErr != nil {
		summary.Errors = []string{runErr.Error()}
	}
	if stats.CreditsUSD != nil {
		summary.CostUSD = *stats.CreditsUSD
		summary.CostEstimated = stats.CreditsEstimated
	}
	for _, pairing := range stats.PerPairing {
		summary.PerPairing = append(summary.PerPairing, benchPairing{
			Model: pairing.Model, Pin: pairing.Pin,
			SteadyStateHitRate: pairing.SteadyStateHitRate, Reported: pairing.Reported,
			PromptTokens: pairing.PromptTokens, CompletionTokens: pairing.CompletionTokens, ReasoningTokens: pairing.ReasoningTokens,
		})
	}
	payload, err := json.Marshal(map[string]benchSummary{"muhiya_bench": summary})
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(payload))
}

func benchmarkStatus(stats contract.TaskStats, runErr error) contract.BenchmarkStatus {
	switch {
	case errors.Is(runErr, context.DeadlineExceeded), stats.StopCause == contract.StopCauseTimeout:
		return contract.BenchmarkTimeout
	case errors.Is(runErr, context.Canceled):
		return contract.BenchmarkBlocked
	case runErr != nil:
		return contract.BenchmarkError
	case stats.StopCause == contract.StopCauseUserStop:
		return contract.BenchmarkBlocked
	case stats.StopCause != "":
		return contract.BenchmarkError
	case strings.HasPrefix(stats.TerminatedReason, "stalled progress"):
		return contract.BenchmarkBlocked
	case stats.TerminatedReason != "":
		return contract.BenchmarkFail
	default:
		return contract.BenchmarkPass
	}
}
