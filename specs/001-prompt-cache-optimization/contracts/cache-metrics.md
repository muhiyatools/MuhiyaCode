# Contract: Cache Metrics & Usage Accounting

**Feature**: `001-prompt-cache-optimization` | Consumers: `internal/gateway` (parsing),
`internal/contract` (types), `internal/state` (persistence), `internal/orchestrator`
(pressure inputs, aggregates), `internal/tui` (display), `benchmarks/cachebench`.

## Parsing (provider → UsageRecord)

Accepted provider usage shapes, in precedence order per field:

1. **DeepSeek top-level**: `usage.prompt_cache_hit_tokens` → `cache_read_tokens`;
   `usage.prompt_cache_miss_tokens` → `cache_miss_tokens` (`miss_derived=false`).
2. **OpenAI nested**: `usage.prompt_tokens_details.cached_tokens` → `cache_read_tokens`;
   `cache_miss_tokens` derived as `prompt_tokens − cache_read_tokens` only when both terms are
   present (`miss_derived=true`).
3. **Neither present**: both fields null.

Rules:

- Values are stored verbatim; no clamping, no smoothing, no backfill from estimates
  (Constitution VI). Contradictory payloads (e.g., hit+miss ≠ prompt) are stored as reported
  and flagged in the record; they are never "corrected".
- Zero is a value; null is absence. A provider reporting `0` cached tokens yields
  `cache_read_tokens=0`, not null.
- `prompt_tokens`/`completion_tokens` parse independently of cache fields.
- Parsing MUST NOT fail the request: malformed usage yields nulls plus a diagnostic.

## Derived figures

- Per-request `hit_rate = cache_read / (cache_read + cache_miss)`; null if either term null;
  denominators of 0 yield null (not NaN, not 100%).
- Session `session_hit_rate = Σ cache_read / (Σ cache_read + Σ cache_miss)` over main-stream
  records with both terms non-null and `attribution != n/a`.
- `steady_state_hit_rate`: same, excluding `attribution=cold-start`. This raw rate remains the
  workload/cost KPI.
- `prefix_stability_rate = Σcache_read / Σ(prompt_tokens - new_tail_tokens)` over main-stream
  requests after the cold start, where `new_tail_tokens = max(0, prompt_n - prompt_n-1)` until
  a byte-accurate provider tail count is available. **This is the SC-001 acceptance figure.**
- Cost derivations always name their price-table source and are labeled *derived*; they never
  masquerade as provider-reported amounts.

## Persistence

- One UsageRecord per request appended to `sessions/<id>/usage.jsonl`
  (schema: [data-model.md](../data-model.md) §1). Append-only; a session's file is never
  rewritten.
- Records are written even when usage is absent (`attribution=n/a`) so request counts and
  unavailable-rates are honest.
- Session aggregates are always recomputed from the file (resume-safe); no separately stored
  running totals that can drift.
- The `~/.muhiya` layout remains v1-compatible: `usage.jsonl` is additive; no existing file or
  SQLite schema changes.

## Display

- `/context` MUST show: session totals (prompt, output), cache split (read / uncached), session,
  raw steady-state, and prefix-stability rates, per-stream request counts, unavailable-request
  count when > 0, and the last
  prefix-change annotation with its cause.
- The activity line MUST show a compact per-request cache tag when available (e.g.,
  `cache 99% (12.3k read / 128 new)`); it shows nothing fabricated when metrics are
  unavailable.
- Task summaries MUST show billed vs cached figures per task from provider-reported sums.
- "Unavailable" is rendered as such — never as `0`, never omitted silently when the provider
  simply doesn't report caching (FR-013).

## Internal consumption

- Context-pressure decisions (fold/trim/compact triggers) MUST consume the latest
  provider-reported `prompt_tokens` when available; the `len/4` estimator is bootstrap-only
  (before the first response) and its use is marked in the pressure input so tests can assert
  the switchover.
- The miss-attribution pipeline consumes `prefix_changed` + `change_reasons` from the
  PrefixShape comparison and the InvalidationEvent ledger
  ([invalidation-events.md](invalidation-events.md)); it MUST label every miss `agent`,
  `agent-suspect`, `provider`, `cold-start`, or `n/a`. `provider` is permitted only inside the
  expected-new-tail tolerance (SC-007: zero unexplained misses in benchmarks).

## Benchmark output (cachebench)

Machine-readable JSON per run: scenario, build (`baseline`|`improved`), run index, per-request
records (verbatim UsageRecord fields), aggregates (raw and prefix-stability rates), wall-clock, derived cost +
price-table source, `unattributed_misses`. Human summary printed after each run. Results are
committed under `specs/001-prompt-cache-optimization/benchmarks/` (SC-005 evidence).
