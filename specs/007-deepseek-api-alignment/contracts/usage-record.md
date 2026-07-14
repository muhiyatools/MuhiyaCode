# Contract: Usage Record & Cache Accounting

Feature 007. Governs how provider-reported cache activity is persisted and surfaced.
Schema in [data-model.md](../data-model.md) §3; verified current behavior G4/G6.

## 1. Persistence

| ID | Requirement |
|---|---|
| UR-1 | New nullable `cache_miss_tokens` column on `request_logs` (migration NNN). Backfill: none (historical rows stay NULL — honest absence, not zero) |
| UR-2 | On stream completion with provider usage: persist `prompt_cache_hit_tokens` → `cache_read_tokens` (existing) AND `prompt_cache_miss_tokens` → `cache_miss_tokens` (new; the value is already parsed in `OpenAIUsage`, currently dropped) |
| UR-3 | `usage_estimated = true` rows (mid-stream disconnect heuristic) MUST leave `cache_miss_tokens` NULL — an estimate never fabricates a provider-reported metric (FR-017, Principle VI) |
| UR-4 | Cost formula unchanged: provider-reported usage preferred; miss tokens do NOT change billing math in this feature (input−cacheRead pricing already correct) |

## 2. Surfacing

| ID | Requirement |
|---|---|
| UR-5 | `/v1/usage` aggregates gain provider-reported hit-rate: `Σ cache_read / (Σ cache_read + Σ cache_miss)` over rows where miss is non-NULL; response labels the derived-fallback rate distinctly when used |
| UR-6 | Dashboard cache-hit-rate switches to the same provider-reported formula with the same labeled fallback |
| UR-7 | The client-visible `muhiya_log` chunk is UNCHANGED (compat boundary); the client already receives hit/miss in the standard usage frame |

## 3. Regression guards (verified-conform behavior frozen by this contract)

| ID | Guarded behavior (evidence) |
|---|---|
| UR-8 | No duplicate-billed rows for one logical request: idempotent `finish()`, PK-guarded outbox retry, failover bills winning candidate only (G4) |
| UR-9 | Failed-upstream rows: cost 0, spend-excluded (`status 2xx AND cost>0` gating) (G4) |
| UR-10 | Contradiction tolerance: hit+miss ≠ prompt_tokens is recorded as reported, flagged downstream by the client's existing detection — the gateway never "corrects" provider numbers (Principle VI) |

## 4. Conformance assertions

1. Integration: streamed request with provider usage frame ⇒ row has both hit and miss
   populated exactly as reported.
2. Integration: killed stream (client disconnect) ⇒ `usage_estimated=true`,
   `cache_miss_tokens` NULL.
3. Query: `/v1/usage` on a mixed dataset (NULL + non-NULL miss) returns
   provider-reported rate over the non-NULL subset with the fallback labeled.
4. Replay G4 traces (429 pre-stream, router failover, mid-stream disconnect) ⇒ row
   counts and billed-row counts unchanged from baseline.
