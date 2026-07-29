package tui

import (
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

const requestTraceLimit = 30

func formatRequestTrace(engine *orchestrator.Engine) string {
	if engine == nil {
		return "Request diagnostics are unavailable while the runtime is loading."
	}
	records := engine.UsageRecords()
	if len(records) == 0 {
		return "No model requests have completed in this session."
	}
	start := max(0, len(records)-requestTraceLimit)
	var lines []string
	for _, record := range records[start:] {
		cache := "cache n/a"
		if record.CacheReadTokens != nil && record.CacheMissTokens != nil {
			total := *record.CacheReadTokens + *record.CacheMissTokens
			rate := 0.0
			if total > 0 {
				rate = 100 * float64(*record.CacheReadTokens) / float64(total)
			}
			cache = fmt.Sprintf("cache %.1f%% (%d/%d)", rate, *record.CacheReadTokens, total)
		}
		tokens := ""
		if record.PromptTokens != nil || record.CompletionTokens != nil {
			tokens = fmt.Sprintf(" · tokens %d/%d", intValue(record.PromptTokens), intValue(record.CompletionTokens))
		}
		latency := ""
		if record.DurationMS != nil {
			latency = fmt.Sprintf(" · %dms", *record.DurationMS)
		}
		route := record.Upstream
		if route == "" {
			route = "direct/unknown"
		}
		lines = append(lines, fmt.Sprintf("#%d · %s · %s · %s%s%s · route %s",
			record.Seq, record.Model, record.Purpose, cache, tokens, latency, route))
		if record.Attribution != "" || len(record.ChangeReasons) > 0 {
			lines = append(lines, "  attribution: "+string(record.Attribution)+formatTraceReasons(record.ChangeReasons))
		}
		if record.PrefixHash != "" {
			lines = append(lines, "  prefix: "+shortTraceValue(record.PrefixHash))
		}
		if record.LogID != "" {
			lines = append(lines, "  request: "+record.LogID)
		}
		if record.Diagnostic != "" {
			lines = append(lines, "  diagnostic: "+record.Diagnostic)
		}
	}
	return strings.Join(lines, "\n")
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func formatTraceReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return " · " + strings.Join(reasons, ", ")
}

func shortTraceValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}
