# Contract: Gateway Usage Wire Surface

**Feature**: `003-tui-ux-overhaul` | Serves FR-012a, FR-018, FR-020 | Gateway repo: `F:\MuhiyaWorkspace\MuhiyaWorkspace`

Two surfaces: the per-request cost meta chunk (existing mechanism, extended) and the new self-service account-usage endpoint. Both are backward-compatible additions; the gateway ships before the client consumes them.

## 1. Per-request cost — `muhiya_log` meta chunk (extended)

### Trigger

Emitted by `sendMuhiyaMetaChunk` (`proxy/handler.go:1883-1910`) as the last data frame before `[DONE]` (OpenAI dialect) / before `message_stop` (Anthropic dialect) on **streaming** responses, when the resolved client app (`getClientAppName`, `handler.go:144-187`) is in the allowlist.

**Change 1 — allowlist**: `{"MuhiyaChat"}` → `{"MuhiyaChat", "MuhiyaCode"}`. Exact string match on the resolved client-app name (from `X-Client-App` header, User-Agent fallback). No prefix matching.

**Change 2 — honesty field**: add `usage_estimated` (bool) mirroring the `request_logs.usage_estimated` value for this request (true when upstream disconnected and tokens/cost came from `estimateTokens`, `handler.go:1659-1700`).

### Frame shape (OpenAI dialect)

```json
data: {"id":"<logID>","object":"chat.completion.chunk","model":"<model>",
       "usage":{"prompt_tokens":N,"completion_tokens":N,"total_tokens":N},
       "muhiya_log":{"cost":0.02417,"log_id":"<logID>","usage_estimated":false}}
```

| Field | Type | Semantics |
|---|---|---|
| `muhiya_log.cost` | float | Request cost in **USD** (`calculateCost`, `handler.go:1702-1717`) — same value persisted to `request_logs.cost` |
| `muhiya_log.log_id` | string | `request_logs.id` for cross-checking (used by D14 credits verification) |
| `muhiya_log.usage_estimated` | bool | true ⇒ client must mark (`~`) or omit, never present as measured (Constitution VI) |

### Client obligations (MuhiyaCode, `internal/gateway/provider.go`)

- Parse the chunk when present; map `cost` → `contract.Usage.CostUSD`, `usage_estimated` → `CostEstimated`. Ignore unknown fields.
- **Absence is normal** (generic OpenAI gateway, older MuhiyaLLM, non-stream path): `CostUSD` stays nil; every credits display is omitted (FR-012). No retries, no warnings in the transcript.
- The chunk must not be forwarded into model-visible content or history (it is transport metadata; keeping it out of the prompt preserves Constitution III/IV).
- Unit conversion happens at display only: **credits = USD × 100**, formatted 2 decimals.

## 2. Account usage — `GET /v1/usage` (new)

### Request

```
GET /v1/usage            (also mounted at /usage, matching existing dual-mount convention)
Authorization: Bearer sk-virt-…        (or x-api-key: sk-virt-…)
```

Authenticated by `authenticateVirtualKey` (`proxy/handler.go:371-399`) — the caller's own virtual key; resolves `key.UserID`. **No admin credentials involved.**

### Response 200

```json
{
  "user": {"id": "u-…", "name": "…"},
  "plan": {
    "name": "Pro",
    "windows": [
      {"name": "5 Hours",  "duration_seconds": 18000,
       "budget_usd": 5.00,  "current_spent_usd": 1.23, "reset_time": "2026-07-12T18:00:00Z"},
      {"name": "Monthly",  "duration_seconds": 2678400,
       "budget_usd": 100.00, "current_spent_usd": 41.87, "reset_time": "2026-08-01T00:00:00Z"}
    ]
  },
  "credits": {"extra_total": 500.0, "extra_remaining": 349.53},
  "spend": {"today_usd": 3.42}
}
```

| Field | Source (existing unless noted) | Notes |
|---|---|---|
| `plan.windows[]` | `db.GetUserBudgetUsage` (`db/db.go:1244-1285`) | USD; sliding windows anchored at `plan_assigned_at`; `reset_time` verbatim |
| `credits.extra_total` / `extra_remaining` | `db.GetUser` populated fields (`db/db.go:471-476`) | **credits** (1 credit = $0.01), numbers (float64) — `DOUBLE PRECISION` sums; FIFO overage deductions (`DeductExtraCreditsIfExceeded`, `db/db.go:1170`) make `extra_remaining` generally fractional |
| `spend.today_usd` | **new** `db.GetUserSpendingToday(userID)` — `SELECT COALESCE(SUM(cost),0) FROM request_logs WHERE user_id=$1 AND status_code >= 200 AND status_code < 300 AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')` | UTC calendar day per clarification; 2xx only, matching `GetUserSpendingInWindow`/`GetUserBudgetUsage` semantics (`db/db.go:1116, 1269`) so it sums the identical row set as the windows' `current_spent_usd` |

Billing-period spend = the `Monthly` (longest-duration) window's `current_spent_usd`; the client selects it by max `duration_seconds` — the server does not duplicate it.

### Errors

Standard proxy error shape (`writeError`, `handler.go:1607-1633`) — **dialect follows the request's auth style**: Bearer-authenticated callers (MuhiyaCode always — `provider.go:145,314`) get the OpenAI envelope `{"error":{message,type}}`; requests carrying `x-api-key`/`anthropic-version` headers get the Anthropic envelope `{"type":"error","error":{type,message}}` with the same `type` values (existing `isAnthropicRequest` branching, which auth-stage 401s emitted before the handler runs also follow):

| Status | `error.type` | When |
|---|---|---|
| 401 | `invalid_request_error` | Missing/invalid/expired/revoked key |
| 405 | `invalid_request_error` | Non-GET |
| 500 | `api_error` | DB failure |

No rate-limit headers (consistent with the gateway today). Endpoint is exempt from RPM/TPM counting (it is not model traffic).

### Client obligations (MuhiyaCode `/usage` command)

- 5 s timeout; failures render the friendly modal states of FR-020 (offline / invalid key / malformed), UI stays responsive; never cached across sessions. FR-020 parsing targets the OpenAI envelope (MuhiyaCode always sends Bearer); an unparseable body falls into the FR-020 "malformed" friendly state.
- Display: per-window `remaining = budget_usd − current_spent_usd` (floor 0) with reset time; credits shown both as credits and USD-equivalent, rounded at display only (2 decimals — the gateway sends raw floats); session spend comes from client-side Σ `CostUSD` (never from this endpoint — different freshness, see [data-model.md §4](../data-model.md)).
- Command hidden while signed out (FR-019); direct invocation while signed out explains sign-in is required — no request is sent.

## 3. Compatibility & sequencing

1. Gateway change ships first; verified by gateway-repo tests: allowlist gate (MuhiyaCode receives chunk, unknown apps don't), `usage_estimated` passthrough, `/v1/usage` happy path + window math + 401 under **both** auth headers (Bearer → OpenAI envelope, x-api-key → Anthropic envelope).
2. MuhiyaCode tolerates old gateways (no chunk, 404 on `/v1/usage` → `/usage` shows "usage data requires an updated gateway" info state — treated as the FR-020 friendly-failure family).
3. **Every MuhiyaCode request streams today** (`OpenAICompatible.Chat` sets `"stream": true`, `provider.go:119`, with `X-Client-App` at `:146`) — main turns *and* aux (onboarding/compaction) and subagent calls all receive the chunk from an updated gateway. The real uncosted-record sources are: failed aux calls that record empty usage (e.g. onboarding error/timeout, `onboarding.go:39-41` → `engine.go:526`; compaction retry exhaustion, `engine.go:1655`) and old/non-allowlisted gateways. Under the [data-model §1.1](../data-model.md) member-set rules, empty-usage records are excluded and a usage-bearing record without cost makes **only its containing task's** credits unavailable (plus the session sum's honesty count) — never later tasks'.
