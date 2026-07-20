package instructions

// Gateway per-family prompt addenda (internal/gateway/model.go
// ModelProfile.PromptAddendum). Prefix-class: each is appended into the
// main-loop system prompt's ENVIRONMENT section via PromptContext.
// ModelAddendum, and is fixed per session (the active model's family never
// changes mid-session). gateway is a service-layer package like workspace —
// both may import this foundation-layer package (internal/arch/layering_test.go),
// so these live here rather than staying gateway-local pointers.
const (
	GatewayDeepSeekAddendumBody = "DeepSeek: use native structured tool calls; do not emit DSML. After tool results, continue to a concrete final answer. Report only work actually performed; if steps remain, say so."
	GatewayMiniMaxAddendumBody  = "MiniMax: preserve interleaved reasoning_details with every assistant tool-call turn, keep tool arguments exact, and finish tool-driven work with a concise result."
	GatewayGLMAddendumBody      = "GLM: use one valid JSON object per native tool call and never narrate a call instead of executing it."
	GatewayGenericAddendumBody  = "Use native structured tool calls and exact schema field names."
)

var (
	_ = Register(Text{ID: "gateway.addendum.deepseek", Audience: MainStatic, Cache: Prefix, Body: GatewayDeepSeekAddendumBody})
	_ = Register(Text{ID: "gateway.addendum.minimax", Audience: MainStatic, Cache: Prefix, Body: GatewayMiniMaxAddendumBody})
	_ = Register(Text{ID: "gateway.addendum.glm", Audience: MainStatic, Cache: Prefix, Body: GatewayGLMAddendumBody})
	_ = Register(Text{ID: "gateway.addendum.generic", Audience: MainStatic, Cache: Prefix, Body: GatewayGenericAddendumBody})
)
