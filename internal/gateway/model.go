package gateway

import (
	"regexp"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/instructions"
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

type ReasoningReplayPolicy string

const (
	ReasoningReplayStrip    ReasoningReplayPolicy = "strip"
	ReasoningReplayPreserve ReasoningReplayPolicy = "preserve"
)

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
	ParsesReasoning      bool
	ReasoningReplay      ReasoningReplayPolicy
	CacheMinPromptTokens int
	PromptAddendum       string
	// Capability metadata (feature 007). Documented parameter surface for the family;
	// empty slices mean "unknown / permissive" so non-DeepSeek providers degrade
	// gracefully (CP-7) rather than being over-constrained.
	SupportedParams  []string
	DeprecatedParams []string
	JSONModeRules    string
	BetaFeatures     []BetaFeature
	KeepAliveNote    string
	// ContinuationLinking (feature 012 FR-010, research R-D12) states whether the
	// family's caching rewards byte-identical transcript replay, making subagent
	// stream continuation worthwhile: DeepSeek matches identical prefixes from
	// token 0 in 64-token blocks (R-F19); MiniMax caches passively over
	// tool-list→system→messages order with a 512-token floor (R-F20). Unknown
	// families degrade to the digest-seeded fallback (CP-7 / Constitution IX).
	ContinuationLinking ContinuationSupport
	// EffortPinned makes continuations inherit the predecessor's reasoning tier:
	// an effort flip changes top-level body params (thinking/reasoning_effort)
	// whose cache effect is undocumented for DeepSeek — conservative default is
	// pinned until the P3 probe proves otherwise (research R-D12).
	EffortPinned bool
}

// ContinuationSupport is the per-family continuation-linking capability.
type ContinuationSupport string

const (
	// ContinuationSupported: byte-identical replay is billed as cache reads.
	ContinuationSupported ContinuationSupport = "supported"
	// ContinuationDigestOnly: no verified replay caching — digest fallback only.
	ContinuationDigestOnly ContinuationSupport = "digest-only"
)

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
			// TB02: 32k, not the old 16k. A single-file implementation write plus
			// reasoning did not fit in 16k, so the tool call was cut mid-JSON and
			// the whole (unusable) payload was re-billed on every later turn. The
			// documented ceiling is 384k, and DeepSeek bills output separately
			// from the context window, so headroom here costs nothing per request.
			MaxOutputTokens: 32_000, OutputTokenLimit: 384_000,
			DefaultContextWindow: 128_000, ContextWindowLimit: 1_000_000,
			NeedsToolCallRescue: true,
			ParsesReasoning:     true,
			ReasoningReplay:     ReasoningReplayStrip,
			PromptAddendum:      instructions.GatewayDeepSeekAddendumBody,
			ContinuationLinking: ContinuationSupported,
			EffortPinned:        true,
			SupportedParams:     deepSeekSupportedParams,
			DeprecatedParams:    deepSeekDeprecatedParams,
			JSONModeRules:       "response_format=json_object requires the word \"json\" plus an example of the shape in the prompt, and max_tokens sized to avoid truncation (known: occasional empty content).",
			BetaFeatures:        deepSeekBetaFeatures,
			KeepAliveNote:       "streaming keep-alive arrives as \": keep-alive\" comment lines; non-streaming as empty lines — both are liveness, not data.",
		}
	case isMiniMaxModelName(lower):
		limit := 204_800
		if isMiniMaxM3Name(lower) {
			limit = 1_000_000
		}
		return ModelProfile{
			Family: "minimax", Temperature: .15, TopP: .95,
			// MiniMax counts max_tokens against the shared context budget
			// (prompt + max_tokens must fit the window), so the operational
			// default must stay far below the documented ceiling or every
			// request 400s with "maximum context length exceeded".
			MaxOutputTokens: 16_000, OutputTokenLimit: limit,
			DefaultContextWindow: limit, ContextWindowLimit: limit,
			NeedsToolCallRescue: true, ParsesReasoning: true,
			ReasoningReplay: ReasoningReplayPreserve, CacheMinPromptTokens: 512,
			ContinuationLinking: ContinuationSupported,
			PromptAddendum:      instructions.GatewayMiniMaxAddendumBody,
			SupportedParams:     []string{"model", "messages", "temperature", "top_p", "max_tokens", "stream", "stream_options", "tools", "tool_choice", "reasoning_split"},
		}
	case strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu"):
		return ModelProfile{Family: "glm", Temperature: .1, TopP: .9, MaxOutputTokens: 32_000, DefaultContextWindow: 128_000, NeedsToolCallRescue: true, ContinuationLinking: ContinuationDigestOnly, PromptAddendum: instructions.GatewayGLMAddendumBody}
	default:
		return ModelProfile{Family: "generic", Temperature: .1, TopP: .95, MaxOutputTokens: 32_000, DefaultContextWindow: 128_000, ContinuationLinking: ContinuationDigestOnly, PromptAddendum: instructions.GatewayGenericAddendumBody}
	}
}

// OutputBudget resolves the output-token cap for a request (TB02). The catalog's
// per-model MaxOutput wins when the provider reported one (it was parsed and then
// ignored before this); otherwise the family default applies. The result is always
// bounded by the documented ceiling.
//
// MiniMax is deliberately excluded from the raise: it counts max_tokens against
// the SHARED context window (see the profile comment), so extra output headroom
// would shrink the usable input window. For MiniMax the chunked-write protocol,
// not a bigger cap, is the answer to an oversized write.
func (p ModelProfile) OutputBudget(catalogMaxOutput int) int {
	budget := p.MaxOutputTokens
	if catalogMaxOutput > 0 {
		budget = catalogMaxOutput
	}
	value, _ := p.ClampOutputTokens(budget)
	return value
}

func isMiniMaxModelName(lower string) bool {
	lower = strings.TrimSpace(lower)
	return strings.Contains(lower, "minimax") || lower == "m3" || strings.HasPrefix(lower, "m3 ") || lower == "m2" || strings.HasPrefix(lower, "m2.") || strings.HasPrefix(lower, "m2 ")
}

// isMiniMaxM3Name delegates to the single foundation-package detector
// (contract.IsMiniMaxM3Name) so gateway and state share one truth. The input
// is already lowercased by the caller; contract lowercases idempotently.
func isMiniMaxM3Name(lower string) bool {
	return contract.IsMiniMaxM3Name(lower)
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
