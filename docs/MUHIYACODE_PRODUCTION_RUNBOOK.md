# MuhiyaCode Production Runbook

This runbook covers the MuhiyaCode client and the Go gateway deployed at
`api.muhiya`. It deliberately contains no credentials.

## Release gate

A public release is blocked unless all of these are true:

- Client and gateway `go test ./...` and `go build ./...` pass.
- Gateway CI runs with real PostgreSQL and Redis services.
- The deployed `/health` reports the expected build and migration count.
- `REQUIRE_REDIS=1`, `DATABASE_URL`, `REDIS_URL`,
  `PROVIDER_KEY_ENCRYPTION_KEY`, `ADMIN_PASSWORD`, and `IDENTITY_SECRET` are
  configured in production.
- The catalog v2 endpoint returns only complete tagged records and the client
  accepts their resolution receipts.
- A concurrency drill proves a hard budget cannot be over-reserved.
- MiniMax M3 and both DeepSeek variants pass the pinned wire/tool corpus.
- A canary window has no wrong-model execution, hidden model call, budget
  overshoot, lost settlement, duplicate mutation, or unexplained prefix reset.

## Deployment order

1. Back up PostgreSQL and record the current gateway build, catalog ETag, and
   applied migration count.
2. Deploy the gateway first. Its migrations are transactional and serialized by
   a PostgreSQL advisory lock. Do not deploy two different migration sets at the
   same time.
3. Verify `/health`, `/v1/muhiyacode/models`, one low-cost completion, its
   request log, reservation, ledger row, and model receipt.
4. Deploy the client only after the gateway contract is verified.
5. Start with a small virtual-key cohort. Expand only after the canary gates
   below remain green.

Rollback the binary before rolling back data. The schema additions are
forward-compatible; never delete exact-money, reservation, catalog, or event
data during an emergency rollback.

## Canary stop conditions

Automatically stop cohort expansion and return traffic to the prior build on
any of these:

- any customer charge greater than its admitted reservation;
- committed spend plus active reservations above a budget window;
- a missing or mismatched model-resolution receipt;
- a model request not equal to the session-selected immutable record;
- a mutation repeated after an indeterminate prior outcome;
- settlement backlog or `billing_loss` increasing;
- Redis-required traffic falling back to process-local limits;
- catalog validation failures that remove the last-known-good catalog;
- repeated isolated cache reads with no recorded prefix, model, route, or
  compatibility change.

Recommended warning thresholds:

- active reservation older than 10 minutes: investigate;
- reservation older than its lease: critical until reconciled;
- settlement failure rate above 0.1% over 15 minutes: stop rollout;
- model receipt mismatch: immediate critical stop;
- unexplained cache-cold rate above 1% of warm turns: stop rollout;
- gateway 5xx above 1% or P95 admission latency above 250 ms: investigate.

## PostgreSQL checks

Run read-only queries first.

```sql
SELECT name, applied_at FROM _migrations ORDER BY applied_at DESC;

SELECT status, count(*), max(now() - created_at) AS oldest
FROM budget_reservations
GROUP BY status;

SELECT r.id, r.request_id, r.user_id, r.amount_nano_usd,
       r.lease_expires_at, r.created_at
FROM budget_reservations r
WHERE r.status = 'reserved'
ORDER BY r.created_at;

SELECT reservation_id, count(*)
FROM request_logs
WHERE reservation_id IS NOT NULL
GROUP BY reservation_id
HAVING count(*) > 1;

SELECT br.user_id,
       sum(br.amount_nano_usd) FILTER
         (WHERE br.status = 'reserved' AND br.lease_expires_at > now())
         AS active_reserved
FROM budget_reservations br
GROUP BY br.user_id;
```

Use the gateway reconciler for expired reservations. Do not hand-edit settled
costs or ledger rows. If manual intervention is unavoidable, preserve the
reservation, request, and idempotency identifiers and record an incident.

## Redis loss

Production must set `REQUIRE_REDIS=1`.

- At startup, a missing, invalid, or unreachable Redis configuration leaves the
  limiter in an explicit fail-closed state; requests receive a retryable
  infrastructure error instead of silently using per-process limits.
- At runtime, Redis command failures also fail closed.
- Restore Redis, verify `PING`, then issue low-volume requests and confirm RPM,
  TPM, and route-affinity keys are created.
- Do not unset `REQUIRE_REDIS` on a multi-replica deployment.

## PostgreSQL loss

- Admission must fail before provider dispatch when PostgreSQL cannot establish
  financial authority.
- The watchdog marks the gateway unready after repeated failures and restores
  readiness when the pool heals.
- After recovery, inspect active/expired reservations, settlement backlog, and
  `billing_loss` before reopening the canary.

## Provider outage or partial stream

- A logical turn permits one bounded same-model retry at most.
- A pinned OpenRouter route may rebind once only before response bytes reach the
  client. Partial streams are never rerouted.
- Provider usage above the admitted ceiling is recorded diagnostically, but the
  customer charge remains capped at the reservation.
- Empty, malformed, or truncated output must terminate with a factual client
  status rather than a completion claim.

## Model mismatch and catalog rollback

- Treat a missing/mismatched model receipt as a security and correctness
  incident. The client must stop before tool execution.
- Compare selected record ID, target model, adapter version, compatibility
  epoch, and catalog ETag.
- A malformed catalog never replaces the client last-known-good cache.
- To roll back metadata, publish a new valid catalog version. Do not mutate an
  immutable record under the same compatibility epoch.

## Cache-affinity incident

For a cold warm-session request, compare:

1. selected model record and compatibility epoch;
2. client prefix hash and change reasons;
3. gateway request ID;
4. OpenRouter upstream provider;
5. Redis route-affinity key lifetime;
6. provider-reported cache read/miss usage.

If prefix and route are unchanged, classify the miss as provider-side
eviction/TTL. Do not alter stable prompt bytes to work around an upstream
eviction.

## Client session recovery

At startup, an incomplete tool dispatch or unresolved mutation produces a
recovery warning and blocks new mutations. Read-only inspection remains
available.

```text
muhiyacode doctor
muhiyacode doctor repair-session ...
```

Inspect the target before choosing committed, not-started, or indeterminate.
Restart the session after reconciliation. Never blindly replay an indeterminate
mutation.

## Evidence to retain for every release

- client and gateway test/build logs;
- migration list and health response;
- catalog document hash/ETag;
- pinned benchmark manifest and report;
- concurrency/fault drill output;
- canary cohort, duration, thresholds, and rollback decision;
- representative request IDs tying client, gateway, provider, reservation, and
  settlement;
- baseline-versus-release success, tokens, cache, latency, and cost.

