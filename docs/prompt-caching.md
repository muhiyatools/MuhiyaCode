# Prompt caching

MuhiyaCode keeps the system prompt, tool schemas, and settled conversation history byte-stable
across a session. Per-turn details such as the date, task class, and budgets are appended to the
latest user message. MCP schemas are pinned at the session boundary; a changed MCP configuration
is applied at the next deliberate boundary and recorded as an invalidation event.

Use a concrete model such as `deepseek-v4-flash` instead of a router that may choose a different
backing model per request. Model changes intentionally start a new provider cache scope.

## Reading the metrics

`/context` reports prompt and output totals, cache-read and uncached input, session and
steady-state hit rates, unavailable usage records, recent invalidations, and context pressure.
The activity line shows a compact tag such as `cache 99% (12.3k read / 128 new)`. `unavailable`
means the endpoint did not provide trustworthy cache figures; it is never treated as zero.

The first request is excluded from the steady-state rate. A prefix change is accompanied by a
cause such as tools, model, compaction, or external workspace change. When the bytes are stable
but the provider reports a miss, attribution is `provider` rather than an invented client cause.

## Provider limitations and observed evidence

The live 24-turn benchmark used three baseline and three improved runs on
`deepseek-v4-flash` at the configured `low` effort. Baseline steady-state mean was 96.5699%;
improved mean was 96.5602%, with a 0.452 percentage-point improved range and zero unattributed
misses. The 99% acceptance target was therefore not met. Monetary cost is unavailable because
the gateway catalog exposed no authoritative prices; no rate was fabricated. Raw provider usage
is retained under `specs/001-prompt-cache-optimization/benchmarks/`.

- Cold starts cannot read a session cache and are excluded from steady state.
- Cache TTL, eviction, and capacity are provider-controlled. Stable bytes may still miss.
- DeepSeek's 64-token cache blocks leave a small uncached trailing block.
- Caches are scoped by model and account; routers can fragment them.
- OpenAI-compatible endpoints generally provide no explicit cache-retention control.
- Reporting varies: fields may be absent, nested, zero, or malformed. Missing data stays unavailable.
- Thinking-mode tool-call replay requires an empty `reasoning_content` key, while reasoning text is never replayed.
- The measured shortfall was provider-attributed: all six arms recorded zero agent-attributed or unattributed invalidations. The client can guarantee prefix stability, not provider retention.
