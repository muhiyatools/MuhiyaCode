package gateway

import (
	"regexp"
	"strings"
)

// BetaFeature records a provider beta capability and whether MuhiyaCode adopts it
// (feature 007, contracts/capability-profile.md CP-6). Status is one of
// "adopted", "not_adopted", or "not_adopted_reevaluate"; Rationale explains the
// decision so a future change has the context recorded in one place.
type BetaFeature struct {
	Name      string
	Status    string
	Rationale string
}

// ModelProfile is the client's boot-frozen capability profile for a model family
// (feature 007, contracts/capability-profile.md). It is the single source of truth
// for what the provider supports, so the request builder never emits an unsupported
// or deprecated parameter and never exceeds a documented limit. Two token pairs are
// deliberately distinct: MaxOutputTokens/DefaultContextWindow are the OPERATIONAL
// values actually sent/budgeted (a cost & latency bound), while OutputTokenLimit/
// ContextWindowLimit are the provider's DOCUMENTED ceilings used only to validate
// that the operational values stay in range. The profile is constant per family and
// never mutates mid-session, so the emitted wire shape is session-stable.
type ModelProfile struct {
	Family               string
	Temperature          float64
	TopP                 float64
	MaxOutputTokens      int // default max_tokens emitted when the caller does not specify one
	OutputTokenLimit     int // documented hard ceiling; a requested max_tokens above this is clamped (0 = unknown, no clamp)
	DefaultContextWindow int // operational context budget (deliberate cost/latency bound)
	ContextWindowLimit   int // documented provider context window; the operational budget must stay within it (0 = unknown)
	NeedsToolCallRescue  bool
	PromptAddendum       string
	// Capability metadata (feature 007). Documented parameter surface for the family;
	// empty slices mean "unknown / permissive" so non-DeepSeek providers degrade
	// gracefully (CP-7) rather than being over-constrained.
	SupportedParams  []string
	DeprecatedParams []string
	JSONModeRules    string
	BetaFeatures     []BetaFeature
	KeepAliveNote    string
}

// deepSeekBetaFeatures is the recorded adoption status of DeepSeek beta features
// (feature 007 research R9). None are adopted: MuhiyaCode edits via tool calls and
// emits free-form answers, and the gateway does not expose the `/beta` base URL.
var deepSeekBetaFeatures = []BetaFeature{
	{Name: "chat_prefix_completion", Status: "not_adopted", Rationale: "agent output is free-form; no format-forcing need; gateway does not expose the /beta base URL"},
	{Name: "fim_completion", Status: "not_adopted", Rationale: "code changes go through tool-based edits, not raw insertion; FIM is deepseek-v4-pro only"},
	{Name: "strict_tool_schemas", Status: "not_adopted_reevaluate", Rationale: "would harden tool arguments but needs the /beta base chain-wide; the DSML tool-call rescue layer already handles malformed calls"},
}

// deepSeekSupportedParams / deepSeekDeprecatedParams mirror the DeepSeek chat API
// as documented on 2026-07-14 (audit-baseline.md §A). Deprecated params are never
// emitted (contracts/capability-profile.md CP-2).
var (
	deepSeekSupportedParams  = []string{"model", "messages", "temperature", "top_p", "max_tokens", "stream", "stream_options", "stop", "tools", "tool_choice", "response_format", "thinking", "reasoning_effort", "logprobs", "top_logprobs", "user_id"}
	deepSeekDeprecatedParams = []string{"frequency_penalty", "presence_penalty"}
)

func ResolveModelProfile(name string) ModelProfile {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "deepseek"):
		return ModelProfile{
			Family: "deepseek", Temperature: .1, TopP: .95,
			MaxOutputTokens: 16_000, OutputTokenLimit: 384_000,
			DefaultContextWindow: 128_000, ContextWindowLimit: 1_000_000,
			NeedsToolCallRescue: true,
			PromptAddendum:      "DeepSeek: use native structured tool calls; do not emit DSML. After tool results, continue to a concrete final answer. Report only work actually performed; if steps remain, say so.",
			SupportedParams:     deepSeekSupportedParams,
			DeprecatedParams:    deepSeekDeprecatedParams,
			JSONModeRules:       "response_format=json_object requires the word \"json\" plus an example of the shape in the prompt, and max_tokens sized to avoid truncation (known: occasional empty content).",
			BetaFeatures:        deepSeekBetaFeatures,
			KeepAliveNote:       "streaming keep-alive arrives as \": keep-alive\" comment lines; non-streaming as empty lines — both are liveness, not data.",
		}
	case strings.Contains(lower, "minimax"):
		return ModelProfile{Family: "minimax", Temperature: .15, TopP: .95, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, PromptAddendum: "MiniMax: keep tool arguments exact and finish tool-driven work with a concise result."}
	case strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu"):
		return ModelProfile{Family: "glm", Temperature: .1, TopP: .9, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, PromptAddendum: "GLM: use one valid JSON object per native tool call and never narrate a call instead of executing it."}
	default:
		return ModelProfile{Family: "generic", Temperature: .1, TopP: .95, MaxOutputTokens: 16_000, DefaultContextWindow: 128_000, PromptAddendum: "Use native structured tool calls and exact schema field names."}
	}
}

// ClampOutputTokens returns req bounded to the documented output-token ceiling. It
// reports clamped=true when it had to reduce the value, so the caller can log the
// event (contracts/capability-profile.md CP-3). An unknown limit (0) is a no-op,
// keeping non-DeepSeek families permissive (CP-7).
func (p ModelProfile) ClampOutputTokens(req int) (value int, clamped bool) {
	if p.OutputTokenLimit > 0 && req > p.OutputTokenLimit {
		return p.OutputTokenLimit, true
	}
	return req, false
}

// IsDeprecatedParam reports whether name is a parameter the provider has deprecated
// and MuhiyaCode must never emit (contracts/capability-profile.md CP-2).
func (p ModelProfile) IsDeprecatedParam(name string) bool {
	for _, d := range p.DeprecatedParams {
		if d == name {
			return true
		}
	}
	return false
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
