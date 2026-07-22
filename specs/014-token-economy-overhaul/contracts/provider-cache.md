# Contract: Provider Cache Adapter

## 1. Capability negotiation

Each model/endpoint resolves one versioned `ProviderCacheProfile`. Resolution precedence:

1. explicit user/gateway capability configuration
2. verified endpoint/model catalog metadata
3. built-in conservative family profile
4. generic automatic/unknown fallback

Unknown fields disable the feature. Compatibility labels alone do not prove support.

## 2. Normalized usage

```go
type CacheUsage struct {
    PromptTokens       *int
    OutputTokens       *int
    CacheReadTokens    *int
    CacheWriteTokens   *int
    UncachedInput      *int
    Derivation         UsageDerivation
    RawSchema          string
}
```

Rules:

- Preserve the existing raw usage payload.
- Map documented complementary fields directly.
- Derive uncached input only when profile semantics prove `prompt = read + uncached` or equivalent.
- Never treat missing cache fields as zero.
- Cache creation and uncached post-breakpoint input remain distinct on Anthropic-style APIs.

## 3. MiniMax Anthropic-compatible path

Feature gate: `provider.transport=minimax-anthropic` or verified auto-selection before the first main request. No mid-session transport change.

Required paid-canary parity before defaulting:

- streamed text and thinking blocks
- multiple tool calls
- tool-result continuation
- assistant thinking/signature replay
- truncated tool call behavior
- error/retry mapping
- explicit system/tools/message cache controls
- provider-reported cache creation/read/uncached fields
- upstream affinity behavior through the deployed gateway

Breakpoint experiment:

1. last core tool definition
2. end of universal system/project-root stable block
3. latest durable task checkpoint when block/lookback constraints require it

Use only breakpoints and TTLs documented and verified for MiniMax. Do not inherit Anthropic's one-hour TTL or deferred-tool semantics by assumption.

## 4. MiniMax OpenAI-compatible path

- Keep deterministic tools -> system -> message content as adapted by the endpoint.
- Use automatic caching.
- Parse `prompt_tokens_details.cached_tokens` when present.
- Keep `reasoning_details` replay as currently required by model profile.
- No explicit `cache_control` fields.

## 5. DeepSeek path

- Automatic prefix caching only.
- Normalize `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens`.
- Preserve full overlapping prefix and stable ordering.
- Treat cache as best effort; do not retry merely because hit rate was low.

## 6. Generic path

- Stable prefix and honest total usage still apply.
- Cache-specific fields unavailable unless explicitly mapped.
- Economic context resets default to conservative task-boundary policy; no price-weight enforcement from invented numbers.

## 7. Invalidation matrix

The adapter exposes effects, never hides them:

| Change | Expected scope |
|---|---|
| core tool schema/order | entire prefix cold |
| system/project root | system + messages cold |
| task epoch | message history replaced; core/system may still hit |
| tail append | prior prefix reusable |
| reasoning/tool parameters | profile-defined; message-cache effect recorded |
| upstream flip | cache availability uncertain/cold; explicit event |
| TTL expiry | cold/creation; explicit attribution when fields show it |

## 8. Cache economics

Every profile may include versioned read/write/miss/output weights. If absent, the runtime reports token members without a synthetic monetary optimum.

Context reset decision must include:

- current and retained prompt estimates
- predicted remaining requests
- cold write/miss cost
- warm replay cost
- output/auxiliary cost if summary call required
- safety margin
- dependency confidence and quality gate

## 9. Prefix tests

- Deterministic map/schema ordering across 1,000 serializations.
- Exact stable prefix across consecutive turns.
- Deferred tool discovery does not change core top-level definitions.
- Task epoch changes only declared message portion.
- Resume with identical profile retains hashes.
- Model/transport/upstream change cannot happen silently.
- Usage field fixtures for MiniMax OpenAI, MiniMax Anthropic, DeepSeek, generic, malformed, and missing values.

