# Contract: DeepSeek V4 Wire Behavior

**Feature**: `004-deepseek-agent-polish` | Decision: [research D5](../research.md) | Findings: [research Part B](../research.md)

Client-side contract for the launch configuration (`deepseek-v4-pro` / `deepseek-v4-flash` through the MuhiyaLLM gateway). Every rule degrades gracefully on non-DeepSeek OpenAI-compatible providers (constitution IX): the DeepSeek-conditional paths key off the existing `ModelProfile.Family == "deepseek"` resolution, and no rule introduces a hard dependency on V4-only fields.

## 1. Model identity (B1)

- Launch IDs: `deepseek-v4-pro`, `deepseek-v4-flash` (hybrid thinking models; 1M context; 384K max output). Docs/config recommend the concrete `-flash` pin for the default and `SubagentModelID` (already supported) for worker routing — documented posture, no routing code (D10c).
- **Legacy `deepseek-chat` / `deepseek-reasoner` retire 2026-07-24**: implementation includes a config/docs sweep; any legacy reference is replaced with a V4 ID.
- Model resolution stays substring-based (`Family:"deepseek"` matches V4 IDs unchanged).

## 2. Thinking & effort mapping (B1/B5)

- `reasoning_effort` continues to be sent explicitly (body + `X-Muhiya-Effort` header) — never omitted, so DeepSeek's server-side auto-escalation for agent-shaped traffic cannot silently change cost/latency.
- Effective V4 levels are `high`/`max` (`low/medium`→high, `xhigh`→max server-side); MuhiyaCode's four-tier effort mapping is unchanged (the existing comment in `effort.go` already documents the ride-up).
- Sampling knobs are not a control surface in thinking mode (ignored server-side); determinism obligations remain on prompt structure + validation. No temperature/top_p changes.

## 3. `reasoning_content` replay — verification gate (B6, blocking for this group)

**Current behavior**: captured reasoning never replayed; DeepSeek-family assistant tool-call turns replay an **empty-string** `reasoning_content` key.

**Probe (before any related change)**: a live multi-turn tool-call chain in thinking mode against both launch IDs through the MuhiyaLLM gateway, asserting acceptance of the empty-string replay. Procedure + evidence land in `benchmarks/replay-probe.md`.

| Verdict | Action |
|---|---|
| Empty-string accepted | Keep current behavior; record verdict + date + model build in this contract and `docs/prompt-caching.md` |
| HTTP 400 / degraded | Build the contingency: retain `ReasoningContent` on assistant tool-call turns within the active task window; replay verbatim; fold at settled boundaries exactly like completed-task tool payloads (append-only ⇒ prefix-safe); re-run the probe green |

Either way the verdict is a recorded artifact (FR-023): this is the one empirically-gated design point of the feature.

## 4. Tool-call salvage (B4-ii, B8-6)

- Trigger: an assistant message with empty `tool_calls` whose `content` matches a salvageable shape: (a) a JSON object/array that validates against an offered tool's schema (name + args), (b) DSML markup, (c) either of those after a short natural-language prefix (any language).
- Action: reconstruct the structured call, dispatch normally, record a `tool_call_salvaged` wire event (shape + tool). **Bound: one salvage per turn**; a second text-emitted call in the same turn falls through to the normal no-tool path.
- Non-goals: salvage never invents arguments, never repairs semantics (Reasonix scope: mechanics only), never rewrites history bytes (the original content stays as transmitted; the salvage is dispatch-side).
- Non-DeepSeek providers: same detector may run — it keys on message shape, not family; profile flag (`NeedsToolCallRescue`) continues to gate it.

## 5. Zero-token empty completion class (B4-iii)

- Detection: HTTP 200 stream ending with no content, no tool calls, and `completion_tokens == 0`, on a turn whose previous message is a tool result.
- Ladder (shares the existing ≤2 empty-final bound — no new loop): (1) one immediate same-request retry; (2) one turn-scoped user rider `[recover] The provider returned an empty completion. Continue from the last tool results.`; (3) honest surface: finalize via the existing breaker path with the failure named — never silent, never infinite.
- Each rung records an `empty_completion_retry` wire event (attempt, outcome). Session restart is explicitly **not** attempted (would burn the warm prefix, B2).

## 6. `tool_choice` invariant (B4-i)

`tool_choice` is pinned `"auto"` on every main-loop request (existing, tested); `"none"` is the only other value any code path may send. `required`/named-function forcing is **forbidden** (thinking-mode 400). This is a reviewed invariant: the existing landing-stability test plus a contract note in code.

## 7. Cache usage accounting (B2)

- DeepSeek shape `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens` and OpenAI shape `cached_tokens` (+ derived miss, contradiction flags) both already parse; unchanged.
- Cost: `muhiya_log` chunk remains the only cost source; absence tolerated (never fabricated). Peak/off-peak 2× (Beijing windows) means **token counts are the primary before/after comparator**; benchmark records include the pricing window for any dollar figures ([quickstart §4](../quickstart.md)).

## 8. Degradation matrix (constitution IX)

| Rule | DeepSeek launch | Generic OpenAI-compatible |
|---|---|---|
| Replay behavior (§3) | per verdict | untouched (family-gated) |
| Salvage (§4) | active | profile-gated; detector shape-based |
| Empty-completion ladder (§5) | active | active (shape-based, harmless) |
| `tool_choice` pin (§6) | required | already the norm |
| Cache fields (§7) | DeepSeek shape | OpenAI shape / absent → omitted |
| Effort header (§2) | consumed by gateway | ignored harmlessly |

## 9. Test obligations

Salvage detector table tests (shapes a–c, one-per-turn bound, no-invention property); empty-completion ladder unit tests (rungs, bound sharing, event records); `tool_choice` invariant test (exists — extend to assert no other value is constructible); replay probe procedure documented + executed live (evidence artifact); legacy-ID sweep assertion in config/docs tests.
