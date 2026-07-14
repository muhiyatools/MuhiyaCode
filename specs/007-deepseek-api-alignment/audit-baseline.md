# Audit Baseline — DeepSeek API Alignment (feature 007)

**Captured**: 2026-07-14. Facts verified against source on this date; DeepSeek doc facts
fetched from api-docs.deepseek.com on this date. This file is the raw evidence base for
the spec and the `/speckit-plan` research phase. It records WHAT IS, not what should be.

## A. DeepSeek documented behavior (fetched 2026-07-14)

### KV / context cache (`guides/kv_cache`)
- Automatic for all accounts; prefix-match from position zero; "a subsequent request can
  only hit the cache if it fully matches a cache prefix unit".
- Cache units built at request boundaries and fixed token intervals; construction takes
  seconds; best-effort.
- Usage fields: `prompt_cache_hit_tokens`, `prompt_cache_miss_tokens`.
- Cache scoped per account.

### Chat completion API (`api/create-chat-completion`)
- Models documented: `deepseek-v4-flash`, `deepseek-v4-pro`.
- `user_id` (string, nullable): **max 512 chars, pattern `[a-zA-Z0-9\-_]`** — end-user
  identifier.
- `thinking` object: `{type: enabled|disabled}` (default enabled) + `reasoning_effort:
  high|max`.
- `temperature` 0–2 default 1; `top_p` 0–1 default 1; `stop` ≤16 sequences;
  `tools` ≤128 functions; `tool_choice` none|auto|required|specific;
  `response_format` text|json_object; `stream_options.include_usage`;
  `logprobs`/`top_logprobs` (0–20).
- **Deprecated (non-functional): `frequency_penalty`, `presence_penalty`.**
- Usage object: `completion_tokens`, `prompt_tokens`, `prompt_cache_hit_tokens`,
  `prompt_cache_miss_tokens`, `total_tokens`, `completion_tokens_details.reasoning_tokens`.
- `reasoning_content` present in messages and stream deltas for thinking mode.

### Rate limits (`quick_start/rate_limit`)
- Hard per-account concurrency: **v4-pro 500, v4-flash 2500** concurrent requests; HTTP
  429 on breach. Expanded-quota accounts get **per-`user_id` limits** (429 per user).
- Under load: non-streaming returns continuous **empty lines**; streaming returns SSE
  keep-alive comments **`: keep-alive`** — "do not affect parsing of the JSON body".
- **Server closes the connection if inference has not started after 10 minutes.**
- No documented 429 remediation beyond capacity requests.

### Multi-round chat (`guides/multi_round_chat`)
- Stateless: full history must be concatenated and resent every round, chronological
  append.

### Tool calls (`guides/tool_calls`)
- `tools[].type=function` with name/description/parameters; response `message.tool_calls[]`
  each with `id`; results returned as `{role: "tool", tool_call_id, content}`.
- Beta `strict: true` mode (schema-enforced arguments) via `base_url=.../beta`.

### JSON mode (`guides/json_mode`)
- `response_format={type: json_object}`; prompt MUST contain the word "json" plus an
  example; set `max_tokens` to avoid truncation; known issue: occasional empty content.

### Chat prefix completion (`guides/chat_prefix_completion`) — beta
- `base_url=https://api.deepseek.com/beta`; last message `role=assistant` with
  `prefix: true`; `stop` to bound output.

### FIM completion (`guides/fim_completion`) — beta
- `/completions` on the beta base URL; `prompt` + optional `suffix`; `max_tokens` ≤ 4k.

## B. MuhiyaCode (client) — current wire behavior

Repo: `F:\MuhiyaCode Agent Go` (module `github.com/muhiya/muhiyacode`).

- **Request build**: `internal/gateway/provider.go` `chatOnce` (~:106–166). Sends:
  `model`, `messages` (replayed), `temperature` (DeepSeek profile 0.1), `top_p` (0.95),
  `max_tokens` (input or profile 16000), `stream: true`,
  `stream_options.include_usage: true`, `tools` + `tool_choice: "auto"` when tools
  present, and **top-level `reasoning_effort` = raw effort string** when reasoning set.
- **Not sent**: `user`/`user_id`, `stop`, `response_format`, `seed`, `n`,
  `presence_penalty`, `frequency_penalty`, `logprobs`.
- **Identity**: only HTTP headers — `X-Muhiya-Session` = `sessionID:main|:sub|:aux`
  (provider.go:150–156; engine.go:787,925; subagent.go:228), `X-Client-App: MuhiyaCode`,
  `X-Muhiya-Effort`. Session ID explicitly never serialized into the body
  (contract/types.go:239–243).
- **Endpoint/key**: `Settings.Provider.BaseURL`, default `https://api.muhiya.com/v1`
  (state/config.go:19); Bearer key from `secrets.json`. No env overrides.
- **SSE**: `internal/gateway/sse.go` `ConsumeLine` (:60–129): parses `data:` lines,
  skips `[DONE]`, tolerates ≤20 malformed lines; parses `delta.content`,
  `delta.reasoning_content`, `delta.tool_calls` (by index), `finish_reason`, `usage`.
  Custom `muhiya_log` chunk parsed in provider.go:246–276 → cost/log_id/estimated.
  `<think>` blocks split post-stream (model.go:32–44).
- **Cache usage parsing** (sse.go:196–287): DeepSeek `prompt_cache_hit_tokens` /
  `prompt_cache_miss_tokens` take precedence; OpenAI `prompt_tokens_details.cached_tokens`
  fallback with derived miss; contradiction detection (hit+miss ≠ prompt).
- **History**: `internal/orchestrator/history.go` — append-only with persisted snapshot;
  per-turn reassembly = system + optional compact summary + recent units fitting
  `contextLimit − reserve`; fold/trim/compact only under pressure, originals archived to
  `pruned.jsonl`; `rewriteVersion` tracks cache-busting rewrites; token estimator
  calibrated from real usage.
- **Replay transform** (provider.go:278–291): strips `reasoning_content` on replay;
  DeepSeek-specific: assistant tool-call turns keep the key as empty string `""`.
- **Tool loop**: every announced tool_call gets exactly one `{role: tool, tool_call_id}`
  result (engine.go:1084–1119); DSML/XML/plain-JSON rescue for DeepSeek/MiniMax/GLM
  (`rescue.go:25–56`, model.go:22–26 `NeedsToolCallRescue`).
- **Model profile** (model.go:21–22): deepseek family — temp 0.1, top_p 0.95,
  **16k max output, 128k context**, rescue on, prompt addendum.
- **Retry** (provider.go:81–104): ≤3 retries on 408/429/≥500 or pre-stream network error;
  **no retry once streaming began**; honors `Retry-After` (cap 30s) else exponential+jitter;
  **10-min request lifetime; 90s idle timeout** cancels a stalled stream.
- **Effort**: client sends raw effort 1:1 (effort.go:84–99); relies on gateway mapping.
- **Absent client-side**: FIM, prefix completion (`prefix: true`), JSON mode, `user_id`.
- **README.md**: 229 lines, dense marketing-style; heavy internal design detail.
- **In-app slash commands** (tui/model.go:385–394, actions.go:23–131): `/reasoning`
  (alias `/effort`), `/goal`, `/plan`, `/resume`, `/new`, `/context`, `/compact`,
  `/model`, `/login`, `/logout`, `/usage`, `/permissions` (alias `/mode`), `/skills`,
  `/mcp`, `/paste`, `/diff`, `/rewind`.

## C. Muhiya Gateway (proxy) — current wire behavior

Repo: `F:\MuhiyaWorkspace\MuhiyaWorkspace` (Go module `gateway`). Packages: `main`
(bootstrap/routes), `proxy` (core: handler/router/translator/thinking/cache/
stickysession/limiter/agent/tools/outbox), `db` (Postgres models/migrations), `admin`
(REST + dashboard). Existing `GATEWAY_AUDIT.md` is partially stale — verify against code.

- **Route**: `/v1/chat/completions` (+aliases incl. `/v1/completions`) → `serveOpenAIClient`
  (handler.go:519) → `proxyOpenAIToOpenAI` (handler.go:931–953) for DeepSeek.
- **Upstream URL**: `provider.BaseURL` (`https://api.deepseek.com`) + `/chat/completions`
  — **`/v1/completions` is NOT FIM**: same chat path; no `/beta` route anywhere; no
  `suffix`/`prefix` support.
- **Body handling**: original client bytes decoded to a generic map (lossless,
  handler.go:699–707). Gateway **rewrites** `model`→TargetModel; **strips** `web_search`;
  **adds** `stream_options.include_usage` when streaming. Thinking
  (`ApplyThinkingOpenAI` thinking.go:211): when effort resolved (header > body), deletes
  client `reasoning_effort`+`thinking`, then reasoner targets get
  `thinking:{type:enabled}` + `reasoning_effort: high|max` (low/med→high, high/max→max);
  non-reasoner gets nothing. **When no effort is resolved, client `reasoning_effort`/
  `thinking` pass through untouched** (raw `low`/`medium` could reach DeepSeek).
  Everything else passes verbatim — including any client-sent `user` field,
  `frequency_penalty`, `presence_penalty` (deprecated upstream).
- **Headers upstream**: only `Content-Type` + `Authorization: Bearer <provider key>`
  (Anthropic path: `x-api-key` + `anthropic-version`). Client headers not propagated.
- **Identity**: virtual key sha256 lookup → `UserID` → `User` (db.go:714, 250–253).
  **No per-user identifier forwarded upstream.** `X-Muhiya-Session` + key id hashed into
  an **in-memory** sticky map pinning model choice for 24h (stickysession.go:18–20,50–87)
  — lost on restart; not shared across instances (Redis is only used for rate limits).
- **Streaming**: verbatim line relay + flush; **120s idle watchdog**
  (handler.go:44,50–56); `finish()` before `[DONE]` computes cost, persists billing row,
  emits **`muhiya_log` meta chunk** `{cost, log_id, usage_estimated}` gated to
  MuhiyaChat/MuhiyaCode clients (handler.go:1950–1983). Early-disconnect usage estimated
  by word heuristic (handler.go:1723–1764, `UsageEstimated=true`).
- **Cache accounting**: `OpenAIUsage` declares `prompt_cache_hit_tokens` /
  `prompt_cache_miss_tokens` / `cached_tokens` (translator.go:78–89);
  `CacheReadTokens()` = max(cached_tokens, hit_tokens); **miss tokens captured but never
  logged or used**; cache-read logged to `request_logs.cache_read_tokens`; DeepSeek
  cache-write billed 0. `InjectAnthropicCacheControl` is claude-only, no-op for DeepSeek.
- **Router**: complexity scoring on FIRST user turn only (router.go:33–49);
  `muhiya-ai-router` failover on ≥400 via `BufferedResponseWriter`; explicit-model
  requests get a **single upstream attempt, no retry**.
- **Timeouts/pool** (handler.go:21–37): client Timeout 15m; connect 10s;
  **ResponseHeaderTimeout 90s**; MaxIdleConnsPerHost 50; IdleConnTimeout 90s. Server:
  ReadHeaderTimeout 10s, IdleTimeout 120s, no WriteTimeout (SSE); 60s drain.
- **Rate limiting** (limiter.go): per-virtual-key sliding 60s RPM/TPM; Redis Lua when
  `REDIS_URL` set else in-memory; fail-open on Redis errors; budget windows + credits
  checked per request; 5s user/plan cache.
- **DeepSeek config** (db.go:408,424–426; seeds): provider `deepseek`
  BaseURL `https://api.deepseek.com`, Anthropic base `/anthropic`;
  models: `deepseek-chat` (0.14/0.28, cacheRead 0.07; **context 64000, max_output
  8192**), `deepseek-reasoner` (0.55/2.19, cacheRead 0.14; 64000/8192),
  `deepseek-v4-flash` alias → target `deepseek-chat`. All `routing_tier='none'`.
  Reasoner detection: name contains `reasoner`/`r1` (thinking.go:340–342).
  **No off-peak pricing logic.** No context-length enforcement.
- **Other endpoints**: `/v1/models` (+`/{id}`), `/v1/usage`, `/v1/capabilities`,
  `/v1/tools/web_search`, `/v1/messages`, `/v1/audio/transcriptions`, admin `/api/…`.

## D. Divergence candidates (doc ↔ code deltas to adjudicate in the audit)

| # | Area | Observed | Documented | Severity guess |
|---|------|----------|------------|----------------|
| D1 | User identity | No `user_id` sent by client or gateway | `user_id` param exists; per-`user_id` concurrency on expanded quotas | High (isolation + future quota) |
| D2 | Model limits | Client profile 128k ctx / 16k out; gateway rows 64k / 8192 | Provider limits per pricing docs | High (over-limit requests) |
| D3 | Reasoning params | Client sends top-level raw `reasoning_effort` (`low`…); gateway rewrites only when effort resolved; raw passthrough otherwise | `thinking{type}` + `reasoning_effort: high|max` only | High (invalid values upstream) |
| D4 | Keep-alive vs idle timeouts | Client 90s idle cancel; gateway 120s watchdog; gateway ResponseHeaderTimeout 90s | Keep-alives for up to 10 min before inference starts | High (aborted long-queued requests) — verify whether keep-alive lines reset idle timers |
| D5 | Cache-miss accounting | Gateway drops `prompt_cache_miss_tokens` from logs | Field documented for cache observability | Medium |
| D6 | Sticky pins durability | In-memory, 24h, lost on restart / not multi-instance | Cache is account-scoped, prefix-exact; model swap wipes prefix | Medium |
| D7 | Deprecated params | Gateway passes `frequency_penalty`/`presence_penalty` verbatim if a client sends them | Deprecated, non-functional | Low |
| D8 | Beta features | No FIM, no prefix completion, no strict tools, no JSON mode anywhere | Documented beta features | Low (opportunity, not defect) |
| D9 | Model naming | Docs describe `deepseek-v4-flash`/`deepseek-v4-pro`; gateway targets `deepseek-chat`/`deepseek-reasoner` (+v4-flash alias) | Current names | Medium (verify targets still canonical) |
| D10 | 429 semantics | Client retries ≤3 w/ Retry-After; gateway single attempt (explicit model) | Concurrency-based 429; keep-alive guidance | Verify (likely conformant) |
| D11 | JSON mode rules | Unused today | Requires "json" in prompt + example + max_tokens headroom | Info (capability map) |

## E. Known context from prior features

- Feature 001 (prompt-cache-optimization): live steady-state hit rate plateaued ~96.5%;
  "PrefixShape blindness" identified as residual limitation.
- Gateway wire compat boundary (Principle VIII): `muhiya_log` cost chunk,
  `X-Muhiya-Session`, `X-Muhiya-Effort`, virtual-key auth chain must stay compatible.
- Gateway `GET /v1/usage` self-service endpoint shipped with feature 003.
