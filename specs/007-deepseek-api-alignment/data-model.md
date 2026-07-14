# Data Model — DeepSeek API Alignment (feature 007)

**Date**: 2026-07-14. Derived from [spec.md](spec.md) Key Entities +
[research.md](research.md) decisions. Only entities this feature creates or changes
are modeled; untouched entities (Session, Plan, Provider) are referenced as-is.

## 1. StableUserIdentifier (new, gateway)

The opaque per-user value sent upstream as `user_id` (R2).

| Field / property | Definition |
|---|---|
| Derivation | `"mu-" + hex(HMAC-SHA256(identity_secret, User.ID))[:40]` |
| Length | 43 chars (`mu-` + 40 hex) — ≤ 512 documented limit |
| Alphabet | `[a-z0-9-]` ⊂ documented `[a-zA-Z0-9\-_]` |
| Source | `User.ID` (account UUID) — never the virtual key, never client input |
| Secret | `identity_secret`: gateway config/env, required non-empty to enable injection; absent secret ⇒ feature disabled + startup warning (never a weaker fallback) |
| Stability | Deterministic per (secret, user) across sessions, devices, key rotations, restarts, instances |
| Uniqueness | Collision probability negligible (160-bit space); two distinct User.IDs never collide in practice |
| Privacy | HMAC prevents confirming a known UUID from the identifier; no PII recoverable |
| Lifecycle | Lives only in the upstream request body; **not stored** in any table; account deletion retires it implicitly |

**Validation rules**: computed value MUST match `^mu-[0-9a-f]{40}$`; injection MUST
replace any client-sent `user_id`/`user`; non-DeepSeek dialect mapping per R2
(OpenAI-compat → `user`, Anthropic → `metadata.user_id`, unknown → omit).

## 2. CapabilityProfile (new, client — extends ModelProfile)

Per-model-family, frozen at boot (R8; Reasonix X5 pattern). For family `deepseek`:

| Field | Value (2026-07-14 docs) | Consumed by |
|---|---|---|
| `ContextWindowTokens` | 1,000,000 (documented) | history budget ceiling check (operational budget stays separate, R3b) |
| `MaxOutputTokens` | 384,000 (documented max) | validates configured `max_tokens` ≤ limit |
| `SupportedParams` | model, messages, temperature, top_p, max_tokens, stream, stream_options, stop, tools, tool_choice, response_format, thinking, logprobs, top_logprobs, user_id | request builder: never emit a param outside this set |
| `DeprecatedParams` | frequency_penalty, presence_penalty | request builder: never emit; audit assertion |
| `ThinkingControl` | `thinking{type}` + `reasoning_effort ∈ {high,max}`; default-enabled server-side | effort mapping awareness (gateway owns final form, R4) |
| `JSONModeRules` | prompt must contain "json" + example; size max_tokens; known empty-content issue | any future response_format use (FR-014) |
| `BetaFeatures` | prefix_completion: not_adopted; fim: not_adopted (v4-pro only); strict_tools: not_adopted_reevaluate | R9 record; no wire effect |
| `KeepAliveSignals` | streaming: `: keep-alive` comments; non-streaming: empty lines | SSE consumer docs/tests (already conformant, C1/C2) |

**Invariant**: profile values are compile-time constants for the family — no network
fetch, no mid-session mutation (session-stable wire shape).

## 3. UsageRecord (changed, gateway `request_logs`)

Existing 21 columns (G6) plus:

| Column | Type | Semantics |
|---|---|---|
| `cache_miss_tokens` | BIGINT NULL (new migration) | Provider-reported `prompt_cache_miss_tokens`, verbatim. NULL when: provider omitted the field, or `usage_estimated = true` (estimates never fabricate a miss count — FR-017/Principle VI) |

**Derived metric change**: dashboard/`/v1/usage` cache-hit rate uses provider-reported
`cache_read_tokens / (cache_read_tokens + cache_miss_tokens)` when miss is non-NULL;
falls back to the current `cache_read/input` approximation otherwise (labeled derived).

**State/writing rules** (verified G4, unchanged): failed-upstream rows keep cost 0 and
are spend-excluded; `finish()` idempotence, PK-guarded outbox retry, and
bill-winner-only failover are regression-guarded by the audit.

## 4. AuditFinding (new, artifact — `audit-report.md` rows)

| Field | Definition |
|---|---|
| `area` | one of the nine documented areas (FR-001) |
| `hop` | client→gateway \| gateway→provider \| chain |
| `observed` | current behavior, file:line evidence |
| `documented` | provider-documented behavior, URL + quote |
| `verdict` | conform \| diverge |
| `severity` | high \| medium \| low \| info — **high** = can cause request rejection, session failure, billing error, or provider-cache invalidation (FR-001 rubric) |
| `resolution` | fixed(commit/task ref) \| accepted(rationale) \| n/a(conform) |

**Invariant**: every high-severity row MUST reach `fixed` + re-verified before feature
completion (FR-001, SC-002). Seed rows: divergence candidates D1–D11
([audit-baseline.md](audit-baseline.md) §D) adjudicated by research R1–R14.

## 5. Relationships

```text
AuthenticatedUser 1──n VirtualKey          (existing, unchanged)
AuthenticatedUser 1──1 StableUserIdentifier (derived, not stored)
VirtualKey        1──n UsageRecord          (existing; gains cache_miss_tokens)
ModelFamily       1──1 CapabilityProfile    (client, boot-frozen)
AuditFinding      n──1 area/hop             (report artifact)
Session ──(affinity)── Model                (existing sticky pin; gains swap logging, R10)
```

## 6. Explicitly unchanged (compat boundaries)

`~/.muhiya` session state; client↔gateway headers (`X-Muhiya-Session`,
`X-Muhiya-Effort`, `X-Client-App`); `muhiya_log` cost meta-chunk shape; virtual-key
auth chain; history/prompt composition (no prefix-affecting change in this feature).
