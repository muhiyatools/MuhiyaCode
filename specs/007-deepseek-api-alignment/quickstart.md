# Quickstart — Validating Feature 007 End-to-End

Runnable validation scenarios proving the feature works. Each maps to spec Success
Criteria. Contracts: [upstream-request](contracts/upstream-request.md),
[capability-profile](contracts/capability-profile.md),
[usage-record](contracts/usage-record.md).

## Prerequisites

- Client repo `F:\MuhiyaCode Agent Go`, gateway repo `F:\MuhiyaWorkspace\MuhiyaWorkspace`
- Gateway running against Postgres with the new migration applied; `identity_secret`
  configured; a live DeepSeek provider key (for live scenarios) or the fixture
  upstream (for simulation scenarios)
- Two test users with distinct virtual keys (`user-a`, `user-b`)
- `go` toolchain; both repos: `go build ./... && go vet ./... && go test ./... -count=1` green

## 1. Wire-shape validity (SC-001, SC-002 → US1)

```text
1. Enable the gateway's upstream capture (test flag / debug tap) OR point the gateway
   at the recording fixture upstream.
2. From MuhiyaCode, run the feature-001 canonical multi-turn session (tools, edits,
   reasoning on and off) — ≥15 requests.
3. Assert over every captured body (contract §5.1):
   - top-level keys ⊆ documented parameter set; no frequency_penalty/presence_penalty
   - user_id present, matches ^mu-[0-9a-f]{40}$
   - thinking present on 100% of requests (enabled+effort or disabled)
   - zero upstream 4xx attributable to request shape
```

**Expected**: all assertions pass; audit report rows for the nine areas complete with
verdicts + citations; every high-severity row `fixed`.

## 2. Stable user identity (SC-003 → US2)

```text
1. user-a runs two sessions (different days or clock-shifted); user-b runs two sessions.
2. Extract user_id from all captures.
3. Assert: one constant value per user; values differ across users; format conforms.
4. Rotate user-a's virtual key; run one more session → identifier unchanged.
5. Send a client request with a spoofed "user_id":"attacker" → captured body carries
   the gateway-derived value instead.
```

## 3. Keep-alive survival (SC-004 → US1)

Uses the simulation harness (spec assumption; a fixture upstream that emits documented
keep-alive signals on demand).

```text
1. Fixture: accept request, delay response HEADERS 8 minutes, then stream normally.
   → Expect: no gateway 502 (header timeout now 630s); client completes.
2. Fixture: send headers immediately, then only ": keep-alive" comment lines for
   9 minutes, then data frames.
   → Expect: neither the gateway 120s watchdog nor the client 90s idle timer fires
     (both reset on comment lines — verified C1/G1, now regression-locked);
     completion succeeds.
3. Fixture: total silence (no bytes) for 3 minutes.
   → Expect: timers DO fire (genuine stall detection preserved).
4. Record before-behavior: rerun (1) against the pre-change gateway → 502 at ~90s
   (the SC-004 before-measurement).
```

## 4. Cache accounting & no-regression benchmark (SC-005, SC-006 → US4)

```text
1. Apply migration; run one live streamed request → request_logs row has
   cache_read_tokens AND cache_miss_tokens populated (usage-record §4.1).
2. Kill a stream mid-flight → row has usage_estimated=true, cache_miss_tokens NULL.
3. GET /v1/usage → provider-reported hit rate present; fallback labeled.
4. Before/after benchmark (constitution Principle X):
   benchmarks/cachebench with the feature-001 canonical workload, single user,
   model/gateway/effort pinned — run on pre-change and post-change builds.
   → Expect: steady-state hit rate ≥ baseline (~96.5%); cold-start reported
     separately; total tokens + cost reported alongside (whole picture).
```

## 5. README relaunch (SC-007, SC-008 → US5)

```text
1. wc -l README.md → ≤ 125.
2. Diff the Commands table against internal/tui/model.go's command registry
   (17 commands + /effort and /mode aliases) → exact match, zero invented commands,
   all framed as in-app commands.
3. Fresh-machine walkthrough: follow README only → npm install -g muhiyacode →
   muhiyacode launches → /login flow reachable. Sections present per FR-019.
```

## 6. Regression gates (all stories)

```text
- Both repos: go fmt ./... (no diff), go vet ./..., go test ./... -count=1 → green.
- Client: existing prefix-stability / marshal-determinism / steady-state-diff tests
  untouched and green (no prompt-region change in this feature).
- Gateway: strip-list exact-set test; Retry-After propagation test; billing-path
  replay (usage-record §4.4).
```
