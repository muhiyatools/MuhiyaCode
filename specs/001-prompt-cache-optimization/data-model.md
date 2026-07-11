# Data Model: Prompt Cache Optimization

**Feature**: `001-prompt-cache-optimization` | **Date**: 2026-07-11
**Sources**: [spec.md](spec.md) Key Entities, [research.md](research.md) decisions D1–D12.

Conventions: "nullable" means the value distinguishes *absent/unavailable* from *zero*
(Go: pointer or explicit `Valid` flag — chosen at implementation). All persisted artifacts are
**additive** to the `~/.muhiya` layout (compatibility boundary): v1 readers ignore unknown
files/fields.

## 1. UsageRecord (per request)

One record per provider request, written append-only to `sessions/<id>/usage.jsonl`.

| Field | Type | Source / rule |
|---|---|---|
| `seq` | int | Monotonic request counter within the session |
| `at` | RFC3339 timestamp | Local clock at response completion (never enters any prompt) |
| `model` | string | The `model` field actually sent |
| `prompt_tokens` | int, nullable | Provider `usage.prompt_tokens` verbatim |
| `completion_tokens` | int, nullable | Provider `usage.completion_tokens` verbatim |
| `cache_read_tokens` | int, nullable | DeepSeek `prompt_cache_hit_tokens`, else nested `prompt_tokens_details.cached_tokens`; null when neither present |
| `cache_miss_tokens` | int, nullable | DeepSeek `prompt_cache_miss_tokens`; else derived `prompt_tokens − cache_read_tokens` when both terms exist |
| `miss_derived` | bool | true when `cache_miss_tokens` was derived, not reported |
| `hit_rate` | float, nullable | `cache_read / (cache_read + cache_miss)`; null unless both terms non-null |
| `prefix_changed` | bool | From PrefixShape comparison (entity 3) |
| `change_reasons` | []string | Empty when `prefix_changed=false`; else invalidation causes (entity 4 taxonomy) |
| `attribution` | enum | `agent` (shape changed), `provider` (shape unchanged, miss > new-tail estimate), `cold-start` (seq==1 or first after resume), `n/a` (metrics unavailable) |

**Validation**: provider-reported fields are stored verbatim — no clamping, no backfill.
`hit_rate` is never computed from estimates (Constitution VI). A record is written even when
usage is entirely absent (all nullables null, `attribution=n/a`) so request counts stay honest.

## 2. SessionUsageAggregate

Derived, never independently stored — rebuilt by folding `usage.jsonl` on load (resume-safe by
construction).

| Field | Rule |
|---|---|
| `requests` | count of records |
| `sum_prompt`, `sum_completion` | sums over non-null fields |
| `sum_cache_read`, `sum_cache_miss` | sums over non-null fields |
| `session_hit_rate` | `Σread / (Σread + Σmiss)` over records where both non-null |
| `steady_state_hit_rate` | same sum excluding records with `attribution ∈ {cold-start}` — the SC-001 figure |
| `unavailable_requests` | count where cache fields null — display honesty guard |

## 3. PrefixShape

In-memory per session (persisted only inside UsageRecord annotations). Computed immediately
before each request send.

| Field | Type | Rule |
|---|---|---|
| `system_hash` | 64-bit+ hash | Over the exact system-message bytes as serialized |
| `tools_hash` | hash | Over the canonical serialized tools array (post-sort, post-canonicalize) |
| `rewrite_version` | int | `History` counter, bumped by every fold/trim/compact/window-drop |
| `model_id` | string | Request `model` field |

**Comparison** (`CompareShape(prev, cur) → []reason`): each differing field maps to a reason:
`system` → prompt-rebuild family, `tools` → toolset-change family, `rewrite_version` →
history-rewrite family, `model_id` → model-switch. Empty result = stable prefix.

**State transitions**:

```text
UNINITIALIZED --first request--> STABLE
STABLE --CompareShape non-empty--> INVALIDATED(reasons)   [record InvalidationEvent(s)]
INVALIDATED --next request, CompareShape empty--> STABLE  [re-stabilization, FR-006]
```

A session that oscillates STABLE→INVALIDATED on consecutive requests without an explicit cause
is a defect (test assertion in the cache-guard suite).

## 4. InvalidationEvent

Append-only ledger per session (persisted alongside usage records; exact carrier —
`usage.jsonl` annotation vs. sibling `invalidations.jsonl` — decided at implementation, both
additive).

| Field | Type | Notes |
|---|---|---|
| `at` | timestamp | |
| `cause` | enum | `fold`, `trim`, `compact`, `window-drop`, `toolset-change`, `model-switch`, `prompt-rebuild`, `user-compact`, `probe-change` |
| `trigger` | enum | `pressure`, `user-action`, `config-change`, `boundary` |
| `scope` | string | Human-readable: what changed (e.g., "folded 3 completed tasks", "mcp__supabase tools added") |
| `pressure` | float, nullable | Usable-context ratio at trigger time (provider-token based when available) |
| `request_seq` | int | First request that transmitted the changed bytes |

**Rules** (FR-005): every rewrite of previously transmitted bytes MUST create exactly one
event before the next request is sent; no event ⇒ no rewrite is permitted. `pressure`-triggered
events require `pressure ≥ 0.60` (D4 floor).

## 5. ToolSurfaceSnapshot (MCP schema cache)

Persisted store under the state dir (file layout finalized at implementation; keyed store,
additive). One entry per MCP server spec.

| Field | Type | Rule |
|---|---|---|
| `fingerprint` | string | Hash of server spec: transport, command/URL, args, env **keys sorted** (map-order-proof) |
| `server` | string | Server name as namespaced into `mcp__<server>__<tool>` |
| `tools` | []ToolSchema | Canonicalized-once schemas, **sorted by namespaced tool name** |
| `captured_at` | timestamp | Last successful live handshake |
| `spec_version` | int | Bumped on incompatible cache-format changes |

**Lifecycle**: session build loads entries for all enabled servers and registers the pinned
surface before the first request; live handshake results overwrite the disk entry only
(visible next session); explicit user `add/remove/enable/disable/authorize` invalidates the
entry and applies at a task boundary with a `toolset-change` event. A server with no cache
entry (first ever use) contributes nothing to the pinned surface until its first handshake
completes, which applies at a task boundary with a `toolset-change` event (one-time,
attributable).

## 6. ProbeSnapshot (gateway web-search availability)

Persisted under the state dir, keyed by `hash(baseURL + apiKey)`.

| Field | Type | Rule |
|---|---|---|
| `fingerprint` | string | Endpoint identity key |
| `web_search` | enum | `supported`, `unsupported` — definitive results only |
| `checked_at` | timestamp | Last definitive probe |

**Merge rule** (D3): a definitive probe result overwrites; a transient failure (timeout,
network error) preserves the previous snapshot. First-ever run with no snapshot and a failed
probe defaults to `unsupported` (conservative; upgrade applies next session as `probe-change`).

## 7. BenchmarkScenario / BenchmarkRunResult

Inputs and outputs of the `cachebench` harness (D8); results stored under
`specs/001-prompt-cache-optimization/benchmarks/`.

**BenchmarkScenario**: `name`, ordered `turns` (scripted user inputs incl. file reads, edits,
searches, follow-ups), `min_turns ≥ 20`, fixed `model`, fixed `effort`, workspace fixture.

**BenchmarkRunResult**: `scenario`, `build` (`baseline` | `improved`), `run_index` (1..N, N≥3),
per-request UsageRecords, `session_hit_rate`, `steady_state_hit_rate`, `wall_clock_ms`,
`derived_cost` (from configured price table, labeled with its source), `unattributed_misses`
(must be 0 for SC-007).

**Comparison rule** (SC-005): baseline vs improved compared per scenario with identical
`model/effort/workspace`; improvement claim requires improved `steady_state_hit_rate ≥ 0.99`
on eligible requests, baseline below it, variance across runs ≤ 1 percentage point.

## Relationships

```text
Session 1──* UsageRecord ──0..1 PrefixShape comparison ──* InvalidationEvent
Session 1──1 SessionUsageAggregate (derived from UsageRecords)
Session 1──1 pinned tool surface ──* ToolSurfaceSnapshot (per enabled MCP server)
Application 1──* ProbeSnapshot (per endpoint fingerprint)
BenchmarkScenario 1──* BenchmarkRunResult ──* UsageRecord (captured copies)
```
