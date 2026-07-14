# Contract: Gateway → DeepSeek Upstream Request

Feature 007. Governs every request the gateway forwards to the DeepSeek provider on
behalf of a MuhiyaCode (or any OpenAI-dialect) client. Verified baseline behavior in
[audit-baseline.md](../audit-baseline.md) §C; decisions in [research.md](../research.md).

## 1. Endpoint & headers

| Item | Contract |
|---|---|
| URL | `provider.BaseURL` + `/chat/completions` (unchanged) |
| Headers | Exactly: `Content-Type: application/json`, `Authorization: Bearer <provider key>`. No client headers propagate upstream. (unchanged) |
| Transport | Connect 10s; **ResponseHeaderTimeout 630s** (was 90s — R1a); overall client timeout 15min; pooling unchanged |

## 2. Body transformation pipeline (order fixed, deterministic)

Client bytes are decoded to a lossless generic map (unchanged), then, in order:

| # | Step | Rule |
|---|---|---|
| 1 | Model rewrite | `model` ← `Model.TargetModel` (unchanged; target values migrate to documented names per R3c as a logged cache-epoch event) |
| 2 | Gateway-tool strip | `web_search` removed (unchanged) |
| 3 | **Param strip-list** | Delete if present: `frequency_penalty`, `presence_penalty`, `user`, `user_id` (R5). Constant list; exact-set test required |
| 4 | **Identity injection** | `user_id` ← StableUserIdentifier (data-model §1). Injected on EVERY DeepSeek request when the identity secret is configured; feature-off ⇒ step skipped entirely (never a partial value) |
| 5 | **Thinking normalization (unconditional)** | Delete client `reasoning_effort` + `thinking`; re-emit `thinking:{type:"enabled"}, reasoning_effort:"high"|"max"` when resolved effort ⇒ reasoning-on, else `thinking:{type:"disabled"}` (R4). Applies on every path, header or not |
| 6 | Stream options | `stream_options:{include_usage:true}` when `stream=true` (unchanged) |
| 7 | Everything else | Passes through byte-preserved (messages, tools, tool_choice, temperature, top_p, max_tokens, stop, response_format, seed, logprobs…) |

**Determinism**: steps 1–6 touch only top-level keys; the `messages` and `tools`
regions are never reordered, re-serialized differently, or mutated — the tokenized
prompt prefix is byte-identical to the client's intent (Principle III). Go's
map-marshal key ordering keeps the emitted JSON deterministic.

**Cache neutrality**: every field this contract adds/changes (`user_id`, `thinking`,
`reasoning_effort`, `stream_options`, `model`) is a top-level parameter outside the
tokenized prompt; none can invalidate the prefix cache. (`user_id` selects the cache
*namespace* per the provider's documented KVCache isolation — R2 disclosure.)

## 3. Streaming relay

| Item | Contract |
|---|---|
| Keep-alive | Upstream `: keep-alive` comments and empty lines reset the 120s watchdog (verified G1) and are forwarded verbatim on the OpenAI→OpenAI path (verified G2) — both behaviors are now contractual (regression-guarded) |
| Meta chunk | `muhiya_log {cost, log_id, usage_estimated}` emitted before `[DONE]` to MuhiyaChat/MuhiyaCode clients (unchanged, compat boundary) |
| Idle fire | Watchdog fire ⇒ upstream body closed, error frame sent, row logged (unchanged) |

## 4. Error relay

| Item | Contract |
|---|---|
| Status | Upstream status relayed verbatim (unchanged) |
| **Retry-After** | When upstream sends `Retry-After` on ≥400, the relayed response MUST carry it; gateway-origin 429s SHOULD set one (R7 — new) |
| Body | `translateErrorBytes` dialect translation (unchanged) |
| Billing | Failed-upstream rows: cost 0, spend-excluded; one billed row max per logical request (verified G4 — regression-guarded by audit) |

## 5. Conformance assertions (audit/test hooks)

1. Capture N≥50 upstream bodies from a live multi-turn session: every top-level key ∈
   documented parameter set; `user_id` matches `^mu-[0-9a-f]{40}$`; no
   `frequency_penalty`/`presence_penalty`; `thinking` present on 100% of requests.
2. Same session, two users: identifiers constant per user, distinct across users.
3. Keep-alive simulation (quickstart §3): headers delayed 8 min ⇒ no 502; comment-only
   stream for 9 min ⇒ no watchdog fire on either hop.
4. Byte-diff the `messages`/`tools` regions of consecutive-turn captures: prefix
   byte-identical (existing PrefixShape/cachebench discipline).
