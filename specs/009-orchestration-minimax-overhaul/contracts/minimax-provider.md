# Contract: MiniMax Provider Integration

Feature 009 (US4, US5; FR-013..021). Gateway + client MiniMax support, mirroring the
DeepSeek path. Evidence: [minimax-baseline.md](../minimax-baseline.md),
[research.md](../research.md) R6–R8. **Verified via a recorded/simulated upstream —
no live MiniMax spend (user directive); MiniMax figures are labeled simulated.**

## 1. Gateway routing & body shaping

| ID | Requirement |
|---|---|
| MX-1 | The gateway MUST support a MiniMax provider (OpenAI-compatible base `https://api.minimax.io/v1`, Bearer auth) with model rows for M3 / M2.7 / M2.7-highspeed / M2.5 / M2.1 / M2 and their documented context limits (M3 1M, others 204.8k) |
| MX-2 | Requests route to MiniMax by the resolved model/provider for the stream (main vs sub-agent), stable within a session (FR-019); the DeepSeek path is untouched |
| MX-3 | Body shaping uses standard OpenAI `messages` / `tools[{type:function}]` / `tool_calls` / `{role:tool, tool_call_id}` — no legacy MiniMax fields (confirmed absent in the reference). Streaming relayed like the DeepSeek path |
| MX-4 | Reasoning MUST be emitted in MiniMax's documented control form (`reasoning_split` on the OpenAI path) — never a raw client value passed through, consistent with the per-provider normalization policy |

## 2. Function calling & multi-turn continuity

| ID | Requirement |
|---|---|
| MX-5 | Tool-call round-trips MUST preserve call IDs, JSON-string arguments, and `{role:tool}` results exactly (FR-014) |
| MX-6 | **Per-family reasoning replay (client, the M1 risk)**: on replay to MiniMax, the assistant's reasoning content MUST be PRESERVED in history (MiniMax interleaved-thinking continuity requirement — Mini-Agent `reasoning_details`), whereas DeepSeek's strip-on-replay is unchanged. `replayMessages` becomes per-family; the MiniMax branch keeps reasoning as settled append-only bytes |
| MX-7 | The MiniMax replay variant MUST keep the stable prefix byte-stable across turns (settled reasoning is append-only, so preserving it does not vary the prefix) — verified by a prefix-stability check for the MiniMax family |

## 3. Prompt caching (DeepSeek standard)

| ID | Requirement |
|---|---|
| MX-8 | Passive caching (≥512 input tokens) MUST be exploited via stable prefixes end-to-end (same discipline as DeepSeek); no control surface to add (automatic) |
| MX-9 | The gateway MUST read `prompt_tokens_details.cached_tokens`, derive miss = prompt − cached (labeled derived), and record both in `request_logs` exactly like DeepSeek; below the 512-token threshold cache metrics render honest zero/unavailable (never fabricated) |
| MX-10 | Cost math MUST branch on prompt size for M3 (≤512k vs >512k tiers: 0.30/1.20/0.06 vs 0.60/2.40/0.12 per M in/out/cached) and use flat rates for M2.x; cached vs uncached input distinguished in billing |
| MX-11 | MiniMax usage, cache metrics, and cost MUST land in the SAME per-request records, session panels, and billing rows as DeepSeek's (FR-015/021) |

## 4. Robustness, security, degradation

| ID | Requirement |
|---|---|
| MX-12 | Provider errors and rate limits relay cleanly with the same bounded-retry and error-surface behavior as DeepSeek; keys handled like existing providers (FR-016) |
| MX-13 | Client capability profile for the MiniMax family (008-style): documented limits (replacing the stale 128k/16k), tool-call rescue on, reasoning parsing — so requests never use capabilities MiniMax lacks (FR-020) |
| MX-14 | A deployment WITHOUT MiniMax configured MUST behave byte-identically to today (zero DeepSeek-only regression, FR-018) |

## 5. Mixed-provider operation (US5)

| ID | Requirement |
|---|---|
| MX-15 | Main and sub-agent streams route independently to their configured provider/model; each provider's prefix stays byte-stable within its own streams (cache affinity per provider) |
| MX-16 | Session accounting attributes usage/cache/cost per model across both providers accurately (per-model rows, honest unavailable states, FR-021) |

## 6. Acceptance (maps to spec)

- SC-005 (simulated fixture): a MiniMax conformance session (≥15 requests, multi-turn
  tool calls) — 0 request-shape rejections; 100% usage records carry token counts and
  (above 512-token prefix) cached-token counts; a repeated-prefix probe shows warm
  cached share ≥50% on the second identical request. **Labeled simulated.**
- SC-006 (mixed): main+subs on different providers complete 8/8; per-model rows for
  both providers; each provider's steady-state cache within 5 points of its
  single-provider baseline.
- SC-007: DeepSeek-only conformance capture byte-identical to pre-feature.
- Unit tests: tiered M3 cost math (both tiers + cached), per-family replay (MiniMax
  preserves reasoning / DeepSeek strips), cached-token derivation, fallback when
  MiniMax absent. Live MiniMax deferred (fixture swaps to real endpoint on funding).
