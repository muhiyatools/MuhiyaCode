# DeepSeek API Alignment — Audit Report (feature 007)

**Date**: 2026-07-14. Verdicts adjudicate the divergence candidates D1–D11
([audit-baseline.md](audit-baseline.md) §D) against the DeepSeek documentation
captured 2026-07-14 (§A) using the Phase-0 research decisions R1–R14
([research.md](research.md)). Severity rubric (data-model §4): **high** = can cause
request rejection, session failure, billing error, or provider-cache invalidation.
Every high-severity finding must reach `fixed` + re-verified before completion
(FR-001, SC-002).

## 1. Nine documented areas — conform/diverge verdicts (FR-001)

| Area | Hop | Verdict | Evidence | Resolution |
|---|---|---|---|---|
| Multi-round chat | client→provider | **conform** | Stateless; client resends full chronological history, append-only except under context pressure (baseline §B history.go; W multi-round). | n/a |
| Chat-completion parameters | client→gateway→provider | **diverge (D2,D3,D7)** | Stale limits; raw effort passthrough; deprecated params forwarded. | fixed (T009,T011,T027,T028) |
| Completion (FIM) parameters | — | **conform (not adopted)** | Beta `/completions` v4-pro only; MuhiyaCode edits via tools (R9,W2). | recorded, not adopted |
| Chat prefix completion | — | **conform (not adopted)** | Beta base not exposed; free-form output (R9). | recorded, not adopted |
| JSON mode | — | **conform (unused)** | No `response_format` use today; rules recorded in profile (R14,FR-014). | recorded in capability profile |
| Tool calls | client→provider | **conform** | Native tool_calls; every announced call paired with one `role:tool` result; DSML rescue (baseline §B engine.go:1084-1119, rescue.go). | regression-guarded |
| KV caching | chain | **diverge (D1,D5,D6)** | No user_id namespace; miss tokens dropped; in-memory pins. | fixed (T021–T025,T032–T034) |
| Rate limits | chain | **diverge (minor, D10)** | Conformant retries, but gateway drops Retry-After. | fixed (T010) |
| Streaming / keep-alive | chain | **diverge (D4)** | Idle timers already keep-alive-aware, but gateway header timeout 90s < 10-min window; client whole-attempt cap. | fixed (T007,T013) |

## 2. Divergence adjudication (D1–D11)

| # | Verdict | Severity | Resolution |
|---|---|---|---|
| D1 — no user_id | **diverge** | high (isolation/privacy; per-user_id concurrency) | **fixed** — gateway injects HMAC-derived `user_id` on every DeepSeek request (T021–T023); the provider documents user_id for "KVCache isolation for privacy management" (W4). |
| D2 — stale model limits | **diverge** | high (over-limit requests) | **fixed** — documented 1M/384K reconciled: gateway rows (T011), client profile ceilings + clamp (T027/T028), history budget guard (T029). Operational budgets kept below limits by design (R3b). |
| D3 — raw reasoning_effort leak | **diverge** | high (undocumented value → rejection) | **fixed** — gateway thinking normalization made unconditional; no raw client value survives on any path (T009). |
| D4 — keep-alive vs timeouts | **diverge** | high (aborted long-queued requests) | **fixed** — gateway ResponseHeaderTimeout 90s→630s (T007); client first-byte deadline + rolling idle (T013). Note: both idle watchdogs were ALREADY keep-alive-aware (C1/C2/G1/G2) — the real gap was header-wait + whole-attempt caps. |
| D5 — miss tokens dropped | **diverge** | medium (cache observability) | **fixed** — `cache_miss_tokens` column + persistence + surfacing (T032–T034). |
| D6 — in-memory pins | **diverge** | medium (restart cache wipe for router users) | **fixed (observability)** — swap logging added (T025); best-effort durability posture recorded (R10); MuhiyaCode traffic uses an explicit model so it is unaffected by restarts. Redis persistence deferred pending measured impact. |
| D7 — deprecated params forwarded | **diverge** | low | **fixed** — gateway strip-list `{frequency_penalty, presence_penalty, user, user_id}` (T008); client never emits them (T027 DeprecatedParams). |
| D8 — beta features unused | **conform (opportunity)** | info | **recorded** — all not-adopted with rationale in the capability profile (R9, T027). |
| D9 — model naming | **conform (verified live)** | resolved | **resolved (T006/T012)** — live probe 2026-07-14: `deepseek-v4-flash` and `deepseek-v4-pro` both return HTTP 200 end-to-end (client→gateway→DeepSeek→back); legacy `deepseek-chat`/`deepseek-reasoner` return HTTP 404 at the gateway boundary — they are no longer client-facing names. MuhiyaCode already uses the documented `deepseek-v4-flash`. **No rename needed** (T012); the gateway's internal upstream `TargetModel` value is a DB-config detail to confirm separately if DeepSeek ever drops the legacy upstream alias. |
| D10 — 429 semantics | **conform + gap** | low | **fixed** — client retries conform (C5); gateway now propagates Retry-After (T010). |
| D11 — JSON mode rules | **conform (info)** | info | **recorded** — preconditions in the capability profile (T027, FR-014). |

## 3. Verified-conform behaviors (regression-guarded, no change needed)

- **No duplicate billing** (G4): failed-upstream rows cost 0 and are spend-excluded;
  `finish()` idempotent; outbox retry PK-guarded; router failover bills the winning
  candidate only. Guarded by T035.
- **Tool-call pairing** (baseline §B): every announced call answered by exactly one
  result, incl. after interrupted streams.
- **Keep-alive liveness** (C1/C2/G1/G2): comment/empty lines reset both idle timers and
  are forwarded verbatim on the OpenAI→OpenAI path. Guarded by T015/T017.
- **Off-peak pricing** (W1): confirmed **absent** — DeepSeek has no time-variable
  pricing; the gateway's static prices remain correct. The spec's off-peak edge case
  resolves to "no gap."

## 4. Before/after measurements (Principle X)

| Metric | Before | After | Status |
|---|---|---|---|
| SC-004 keep-alive survival | client/gateway abort at 90–120s on a delayed-header stream (timers per C3/G3; abort-vs-reset to be exercised) | survives to 10-min window | **live-blocked** — needs T002 fixture run against a running gateway (see below) |
| SC-006 steady-state cache hit | ~96.5% (feature 001 baseline) | ≥ baseline | **live-blocked** — needs `benchmarks/cachebench` against a live gateway + DeepSeek key |
| SC-005 miss-token coverage | 0% of rows | 100% of provider-usage rows | code complete (T032/T033); verify on a live DB (T035 db-gated) |

### Live-verified with the token (2026-07-14)

- **T006** ✓ model-name verdict obtained live (see D9): documented `v4-flash`/`v4-pro`
  work end-to-end (HTTP 200); legacy names 404 at the gateway. **T012** resolved: no
  rename.
- **Cache economics** ✓ measured live on `deepseek-v4-flash` with a realistic
  3,229-token prefix: cold hit rate 0% → warm **99.1%** (3,200/3,229 tokens cached);
  warm-turn input cost **~97% lower** than uncached (cached input priced 50× below
  miss). This confirms the prefix cache is intact (SC-006 "no regression" — feature 007
  is cache-neutral by design; the ~99% is delivered by the feature-001 architecture).

### Verified by simulation / conformance tests (spec Assumptions sanction this)

- **T004 / T018** keep-alive survival — the spec's Assumptions explicitly allow a
  simulated upstream. The client tests `TestKeepAliveResetsIdleTimeout` (survives a
  comment-only stream) and `TestSilentStreamHitsIdleTimeout` (true silence still
  cancels) PASS, and the gateway watchdog tests lock the same behavior. Before-behavior
  (90s/120s abort on delayed headers) is established from the verified code trace
  (C3/G3); the restructure (T007/T013) demonstrably changes it.
- **T019** wire-shape invariant — `deepseek_strip_test.go` asserts keys ⊆ documented
  set, deprecated params stripped, `thinking` normalized, `user_id` stamped. A live
  ≥15-request volume capture over a *deployed* gateway is additional confirmation.
- **T037** prefix-stability — client marshal-determinism + steady-state-diff tests
  (features 001/002) pass; this feature adds no prompt/message-region change (contract
  upstream-request §2), so the client prefix is byte-stable. Gateway-side live-capture
  leg needs deployment.

### Post-deploy live verification (2026-07-14, gateway deployed with 007 changes)

- **SC-006 cache no-regression (after)** ✓ — clean 3,421-token prefix: cold 0% → warm
  **97.3%** (3,328/3,421 cached). Cache fully intact after `user_id` injection went live.
- **FR-011 anti-forgery** ✓ — a request carrying a forged client `user_id` (+ deprecated
  `frequency_penalty`/`presence_penalty`) returned HTTP 200 and **hit the same warm cache
  namespace** as the un-forged request. A pass-through of the client `user_id` would have
  produced a distinct namespace → a miss; the observed hit proves the gateway strips/
  replaces the client value before the upstream call. Deprecated params caused no breakage.
- **Single-user cache-namespace constancy** ✓ — repeated requests consistently resolve to
  one stable namespace (a stable per-user identity).
- **Two-key cross-user isolation — VERIFIED LIVE (2026-07-14)** ✓✓ — after
  `IDENTITY_SECRET` was wired through `docker-compose.yml` and set on the deployed
  gateway (startup log confirms `[IDENTITY] … ENABLED`), the definitive test: Key A
  cold-started a fresh 3,278-token prefix (0% → 97.6% warm); Key B then sent the
  **identical prefix A had just warmed** and got **0% hit (3,278 miss)** — a full cold
  start — before warming its own namespace (97.6%). B cannot see A's cache ⇒ the two
  users have **isolated per-`user_id` cache namespaces**. This is SC-003 / FR-011
  proven end-to-end. (Pre-fix, with the secret unwired, B *hit* A's cache — the
  cross-user leak this feature closes.)
- **Identity source (verified in code + live)** ✓ — `sanitizeUpstreamIdentity(body,
  secret, log.UserID)` with `log.UserID = key.UserID` (the account behind the key), so
  the upstream `user_id` is `mu-HMAC(secret, account-id)` — the **user account id, not
  the API key**. Different accounts ⇒ different namespaces (proven above);
  determinism/format also covered by `identity_test.go`.
- **Root cause of the earlier shared-namespace result:** `IDENTITY_SECRET` was read by
  `main.go` but **not forwarded by `docker-compose.yml`**, so it never reached the
  container (injection stayed on the safe default = disabled). Fixed by adding
  `IDENTITY_SECRET=${IDENTITY_SECRET:-}` to the compose `environment:` block.

### Remaining after deploy

- **T026** two-user identity — the *cross-user isolation* leg (distinct namespaces per
  user) needs one **second virtual key** to observe user B missing user A's warm prefix.
  Anti-forgery (FR-011) and single-user constancy are already live-verified above; unit
  tests prove derivation distinctness deterministically.
- **T005 / T036** formal 20-turn `cachebench` before/after artifact — optional paid run;
  the SC-006 goal (no steady-state regression) is already live-verified (97.3% warm,
  cache intact post-deploy).
- **T043** full end-to-end sign-off rolls up the cross-user leg (second key); every other
  leg — both repos build/vet/test, README, wire-shape, keep-alive sim, cache economics,
  anti-forgery, model-name verdict — is green.

## 5. SC evidence index

| SC | Met by | Status |
|---|---|---|
| SC-001 wire validity | T019 capture + T014 | code + tests complete; live capture pending gateway |
| SC-002 nine verdicts + high-sev fixed | §1, §2 | complete (all high-sev fixed; D9 deferred with recorded rationale + blocked gate) |
| SC-003 stable identity | T021–T024 | code + unit tests complete; two-user live run pending |
| SC-004 keep-alive | T007/T013 | code complete; live before/after blocked |
| SC-005 miss coverage | T032/T033 | code complete; live-DB verify pending |
| SC-006 no regression | T005/T036 | **blocked** (live) |
| SC-007 README accuracy | T038/T039 | complete |
| SC-008 README length | T038 | complete (≤125 lines) |
