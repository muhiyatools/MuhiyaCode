# MiniMax & Reference Baseline — feature 009

**Captured 2026-07-14.** Evidence base for the spec and `/speckit-plan` research
phase: official MiniMax doc captures + full analysis of MiniMax's official
Mini-Agent reference CLI + the current partial MiniMax awareness in our two
codebases. Records WHAT IS, not what should be.

## A. MiniMax official docs (fetched 2026-07-14)

### Text generation (`docs/guides/text-generation`)
- Base URLs: **OpenAI-compatible `https://api.minimax.io/v1`** (our integration
  surface); Anthropic-compatible `https://api.minimax.io/anthropic` (out of scope).
- Auth: `Authorization: Bearer <key>`.
- Models: **MiniMax-M3 (1,000,000 context — "frontier multimodal coding model")**,
  M2.7 (204,800; ~60 tok/s), M2.7-highspeed (~100 tok/s), M2.5, M2.1
  (programming-focused), M2 (agentic), M2-her (64,000; dialogue).
- Reasoning: thinking blocks on the Anthropic path; OpenAI path via
  `reasoning_split` (see Mini-Agent §C4).

### Prompt caching (`docs/api-reference/text-prompt-caching`)
- **Automatic/passive** — no request fields or headers; applies to calls with
  **≥512 input tokens**; TTL auto-adjusted by system load (explicit 5-min variant
  exists but is the non-passive mode).
- Supported: M3, M2.7, M2.5, M2.1 series.
- Usage reporting: OpenAI SDK → **`prompt_tokens_details.cached_tokens`**;
  Anthropic SDK → `cache_read_input_tokens`.
- Pricing: cached tokens ~**80% discount** (example $0.12/M cached vs $0.60/M new).
- Best practice (verbatim intent): put static/repeated content (tool list, system
  prompt) at the beginning, dynamic content at the end — prefix matching, same
  discipline as DeepSeek.

### Pay-as-you-go pricing (`docs/guides/pricing-paygo`, fetched 2026-07-14)
Per 1M tokens, USD (input / output / cached-read):
- **MiniMax-M3 ≤512k ctx**: 0.30 / 1.20 / **0.06** (cached = 80% off input)
- **MiniMax-M3 >512k ctx**: 0.60 / 2.40 / 0.12
- **M2.7**: 0.30 / 1.20 / 0.06 · **M2.7-highspeed**: 0.60 / 2.40 / 0.06
- **M2.5 / M2.1 / M2 (legacy)**: 0.30 / 1.20 / 0.03 · highspeed 0.60 / 2.40 / 0.03
Note: MiniMax M3 has a **tiered input rate by context length** (≤512k vs >512k) —
the gateway's per-model cost math must branch on prompt size, unlike DeepSeek's
flat rate. Cached-read is the only discount tier (no separate cache-write line).

### M3 function calling (`docs/guides/text-m3-function-call`)
- OpenAI form: standard `tools[{type:function,function:{name,description,
  parameters}}]`; responses in `tool_calls[]` with JSON-STRING arguments; results
  as `{role:"tool", tool_call_id, content}`.
- **Continuity requirement**: the COMPLETE assistant message (all content/reasoning
  blocks) must be appended to history; thinking interleaves BETWEEN tool calls.
- OpenAI reasoning modes: `reasoning_split=true` → separated reasoning; `false` →
  `<think>` tags embedded in content (MuhiyaCode already parses `<think>` via
  SplitThinkBlocks and rescues DSML — model.go).

## B. Mini-Agent reference CLI (read-only analysis, full report)

Python single-agent CLI (`anthropic`/`openai` SDKs, pydantic, mcp). Default model
**MiniMax-M2.5**; suffix routing `api.minimax.io` + `/v1` (OpenAI) or `/anthropic`.

**The finding that matters most for us (wire continuity, OpenAI path):**
- Enables reasoning via `extra_body={"reasoning_split": True}`
  (openai_client.py:69); parses `message.reasoning_details[].text`;
- **re-sends it on replay**: `assistant_msg["reasoning_details"] =
  [{"text": msg.thinking}]` with the verbatim comment: *"IMPORTANT … This is
  CRITICAL for Interleaved Thinking to work properly! The complete
  response_message (including reasoning_details) must be preserved in Message
  History and passed back to the model in the next turn."*
  (openai_client.py:160-166)
- ⚠️ This is the OPPOSITE of our DeepSeek replay policy (reasoning stripped on
  replay; empty-string key on tool-call turns). Per-family replay policy is
  therefore REQUIRED (spec FR-014/FR-020); plan phase resolves the mechanism.

Other verified facts:
- No caching control surface — client only READS cache fields (Anthropic path sums
  `cache_read_input_tokens`+`cache_creation_input_tokens` into prompt_tokens;
  its OpenAI path doesn't even read `cached_tokens`).
- Tool declaration/calls/results: standard OpenAI shapes on the /v1 path; no
  legacy `abab`/ChatCompletionPro fields anywhere (reply_constraints, sender_type,
  bot_setting, mask_sensitive: zero hits).
- Agent loop: single-agent, non-streaming, max_steps 50–100; retries exponential
  (3, 1s→60s); context summarization at 80k-token trigger keeping system + user
  messages, replacing execution spans with LLM-written "[Assistant Execution
  Summary]" user messages.
- **NO subagents, NO planner/executor split, NO plan files** — orchestration hits
  in its tree are only bundled third-party skill docs. Mini-Agent informs the
  MiniMax WIRE integration, not the pipeline design.
- Skills: progressive disclosure (metadata in prompt, body on demand) — same
  philosophy as MuhiyaCode's SKILLS section.
- Usage: `total_tokens` accumulated for stats + summarization trigger only.

## C. Current MiniMax awareness in OUR codebases (starting points, not done)

- **Gateway** (`F:\MuhiyaWorkspace\MuhiyaWorkspace`): `famMiniMax` classification +
  thinking dialect in `proxy/thinking.go` (M2+ "always-on; only reasoning_split is
  useful" mapping); no provider row, no model rows/pricing, no cached-token
  accounting for MiniMax, no conformance coverage.
- **Client** (`internal/gateway/model.go`): `minimax` ModelProfile family exists
  (temp .15, top_p .95, 16k out / 128k ctx — STALE vs documented 204.8k–1M;
  NeedsToolCallRescue: true; short addendum). No capability metadata (008 added it
  for DeepSeek only). SSE parser already handles `reasoning_content`, `<think>`
  splitting, and OpenAI `prompt_tokens_details.cached_tokens` (fallback path) —
  the cached-token read for MiniMax may Just Work client-side; gateway
  accounting/billing does not.
- **Orchestration**: feature 008 shipped delegation inducement + AutoReview nudge +
  bounded recovery; live benchmark still showed 0 subagent runs on the before-leg
  and the pipeline (research→plan→implement→validate) does not exist as an
  enforced flow — that is this feature's US1–US3.

## D. Spec-relevant divergence/risk candidates for the plan phase

| # | Area | Fact | Consequence |
|---|---|---|---|
| M1 | Reasoning replay | MiniMax needs `reasoning_details` preserved; DeepSeek needs reasoning stripped | Per-family replay policy in the client (and/or gateway shim); prefix-stability analysis required for the MiniMax variant |
| M2 | Client model profile | minimax family says 128k ctx/16k out | Stale vs 204.8k–1M documented; capability metadata needed like DeepSeek's |
| M3 | Gateway | Only thinking mapping exists | Provider row, models+pricing (cached/uncached), routing, usage/billing, conformance tests all missing |
| M4 | Cache accounting | MiniMax reports `prompt_tokens_details.cached_tokens` (no miss field) | Gateway/billing must derive miss = prompt − cached (labeled derived); client already tolerates this shape |
| M5 | reasoning_split | Gateway currently strips/normalizes thinking fields per family | Must emit MiniMax's documented control (`reasoning_split` extra field or thinking form) — never raw client passthrough |
| M6 | Cacheable floor | ≥512 tokens | Honest zero/unavailable below threshold (spec edge case) |
| M7 | Orchestration reference | Mini-Agent has none | Pipeline design leans on Reasonix (constitution VII) + Claude Code behavioral target; Mini-Agent scoped to wire fidelity only |
