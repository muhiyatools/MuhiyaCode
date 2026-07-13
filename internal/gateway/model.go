package gateway

import (
	"regexp"
	"strings"
)

type ModelProfile struct {
	Family               string
	Temperature          float64
	TopP                 float64
	MaxOutputTokens      int
	DefaultContextWindow int
	NeedsToolCallRescue  bool
	PromptAddendum       string
}

func ResolveModelProfile(name string) ModelProfile {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "deepseek"):
		return ModelProfile{Family: "deepseek", Temperature: .1, TopP: .95, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, PromptAddendum: "DeepSeek: use native structured tool calls; do not emit DSML. After tool results, continue to a concrete final answer. Report only work actually performed; if steps remain, say so."}
	case strings.Contains(lower, "minimax"):
		return ModelProfile{Family: "minimax", Temperature: .15, TopP: .95, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, PromptAddendum: "MiniMax: keep tool arguments exact and finish tool-driven work with a concise result."}
	case strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu"):
		return ModelProfile{Family: "glm", Temperature: .1, TopP: .9, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, PromptAddendum: "GLM: use one valid JSON object per native tool call and never narrate a call instead of executing it."}
	default:
		return ModelProfile{Family: "generic", Temperature: .1, TopP: .95, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, PromptAddendum: "Use native structured tool calls and exact schema field names."}
	}
}

var thinkBlock = regexp.MustCompile(`(?is)<think>(.*?)</think>`)

func SplitThinkBlocks(content string) (visible, reasoning string) {
	var thoughts []string
	visible = thinkBlock.ReplaceAllStringFunc(content, func(block string) string {
		match := thinkBlock.FindStringSubmatch(block)
		if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
			thoughts = append(thoughts, strings.TrimSpace(match[1]))
		}
		return ""
	})
	return strings.TrimSpace(visible), strings.Join(thoughts, "\n")
}
