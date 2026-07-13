# Data Model: Reasonix-Aligned Agent Flow Overhaul

**Feature**: `002-reasonix-agent-overhaul` · **Date**: 2026-07-12 · **Source**: [spec.md](spec.md) Key Entities + [IMPLEMENTATION_REVIEW.md](../../IMPLEMENTATION_REVIEW.md)

Entities are conceptual; concrete Go types live in the packages noted. Field lists are contracts for behavior, not struct prescriptions — existing types are extended in place (Constitution VIII).

## 1. StablePrefix

The byte-identical portion of every request: standing instructions (system prompt), tool definitions, settled conversation history.

| Field / aspect | Rules |
|---|---|
| System prompt | Composed once per session from config + persisted snapshots only. Zero dynamic values. Includes the static cache-discipline section (one-time addition this feature). Owner: `internal/orchestrator/prompt.go`. |
| Tool definitions | Fixed composition order (base → synthetic → subagent → MCP-pinned), each group internally sorted/stable; identical bytes across the whole session regardless of mode. Owner: `engine.go sessionDefinitions` + `registry.go`. |
| Settled history | Append-only between rewrites. A message becomes settled when its turn completes; settled bytes change ONLY via a Rewrite (below). |

**Invariant (validation rule)**: for consecutive requests N, N+1 on the same stream: `prefix(N+1) == prefix(N) + appended-turn-messages`, unless exactly one InvalidationRecord explains the difference. Enforced by the prefix-shape guard on every request-producing path; degraded paths must be loud (FR-017).

**State transitions**: `composed → serving (per request, byte-identical) → rewritten (only via Rewrite w/ InvalidationRecord) → serving …`

## 2. DynamicTail

Per-turn content permitted to vary; rides only the newest user turn.

| Field | Rules |
|---|---|
| Task brief | ≤ ~38 tokens, once per task's first message. |
| Goal block | Only while a goal is Active; fixed template around goal text. |
| Plan block / pending-plan injection | Only in plan mode / on the pending-plan continuation turn. |
| Governor/steering notices | Tail-appended user-role messages; only near caps/failures/steer events. |
| Allowed tag set | CLOSED enumeration (contract: request-assembly.md §4). Adding a new tag requires a contract update + tail-budget guard update. |

**Validation rule (SC-004)**: plain follow-up turn (no goal/plan/pending/steer) adds ≤ ~50 system tokens beyond user text; guarded by an automated test.

## 3. InvalidationRecord

The explanation paired with every legitimate StablePrefix rewrite.

| Field | Notes |
|---|---|
| Trigger | enum: maintenance-fold, trim, compaction, window-drop, toolset-boundary-change, model-switch, probe-change, session-upgrade |
| Scope + timestamp + shape delta | Existing `internal/orchestrator/invalidation.go` event shape; extended triggers only if reclamation tiers need distinction. |

**Invariants**: (a) every Rewrite emits exactly one record BEFORE the next request is assembled; (b) no code path may mutate settled bytes then suppress the record (REV A1 defect class); (c) the shape guard treats "changed prefix + no record" as a hard error on the main stream, loud-degraded elsewhere.

## 4. Rewrite (Reclamation / Compaction operations)

| Operation | Preconditions | Effect | Bookkeeping |
|---|---|---|---|
| ReclaimTier1 (snip) | pressure ∈ [snip-floor, compact-threshold); estimated yield ≥ 5% of window; per-message: stale, > min-size floor | Stale tool results reduced to head+tail per ReclamationGeometry | Original archived to `pruned.jsonl` BEFORE mutation; one InvalidationRecord per pass; latch slot consumed |
| ReclaimTier2 (prune) | Tier1 insufficient, still below compact threshold | Eligible results elided to placeholder | Same archival + record rules |
| Compact | pressure ≥ compact threshold | Middle folded to digest; verbatim tail preserved; pinned items untouched | Digest ACCUMULATES (prior digests verbatim); consecutive-compact latch; record emitted |
| SoftNotice | pressure ≥ soft threshold, < compact | Advisory only | ZERO history mutation (guarded by test) |

**Pinned (never rewritten)**: system prompt (not in history), first user turn if < pin-size, prior digests, unsettled current-task messages, verbatim tail window.

**ReclamationGeometry** (per tool-result kind; exact constants confirmed in research.md from Reasonix): read-only results → large head / small tail; side-effecting results → balanced head/tail; below min-size floor → untouched.

**ArchiveRecord (`pruned.jsonl`, one JSON object per pruned message)**: `{task_index, turn, tool_name, original_content, reduced_to, reason(tier), pressure}` — session-local, append-only, recoverable.

## 5. MechanismInventory (Phase 0 artifact, lives in research.md)

| Field | Rules |
|---|---|
| Mechanism | Named Reasonix mechanism w/ source file:line |
| Area | one of the 8 spec areas (prompt, orchestration, context mgmt, caching, tool exec, session, memory/steering, token efficiency) |
| Decision | adopt / adapt (deviation + reason) / reject (reason) / already-present (MuhiyaCode file:line evidence) / not-applicable (provider difference) |
| Landing site | MuhiyaCode package/file for adopt/adapt rows |

**Validation (SC-008)**: 100% of the 8 areas covered; no mechanism row without a decision.

## 6. DispatchGate state (per scope)

`callCounters` — one instance per task (main loop) and one per subagent RUN:

| Field | Purpose |
|---|---|
| callCounts (signature → n) | identical-repeat limiter |
| failedCalls (signature → last error) | verbatim-failed short-circuit |
| failedClassCounts ((tool, normalized-error) → n) | storm breaker, escalate at 3 |
| planViolations (n) | plan-mode escalation at 3 |
| allFailedTurnStreak (n) | 2 consecutive all-failed turns → loop-guard notice |
| taskTokens / taskFailures window | token breaker + distinct-failure terminator (main scope; subagent tokens flow INTO parent taskTokens) |

**Invariants**: reset at scope start; never shared across scopes; escalation notices append to the OWNING scope's transcript only (subagent notices never touch parent history mid-turn).

## 7. GoalState / PlanState (mode state machine)

Single mutex (`modeMu`) owns ALL of: `goal`, `lastGoal` (tombstone), `planMode`, `pendingPlan`.

Goal transitions: `nil → Active (SetGoal; clears planMode) → {Complete | Blocked} (marker scan on EVERY turn shape incl. final; intercept-once for incomplete plan) → tombstone (removed from active state, sidecar cleared)`. Idle stop: 2 CONSECUTIVE no-tool auto-turns; missing-marker: nudge once, stop at 2 (streak fields on Goal). Markers stripped from display, retained in history.

Plan transitions: `off → on (SetPlanMode(true); clears goal) → planReady (exit_plan_mode WHILE planMode on; else harmless no-op) → {execute-now | pendingPlan | keep planning}`; `pendingPlan + continuation prompt → plan injected on tail, flag cleared`. All flags persisted in sidecars; restored with notices.

**Cross-invariant**: goal Active ⇒ planMode off, and vice versa (setter-enforced + defensive brief-assembly backstop: plan wins, drop goal block, log).

## 8. EfficiencyReadout

| Field | Source | Persistence |
|---|---|---|
| Session hit rate (Σcache-read / Σprompt, main stream) | existing `contract/cache.go` aggregation | live during task + persistent footer after |
| Task prefix-stability rate | existing usage records | same |
| Tokens billed / cached / new, cost | TaskStats (existing) | footer: cleared ONLY by new prompt, session switch, exit — never by timer |

## 9. MatchedWorkload (validation entity)

`{script: ordered prompt list, agent: muhiyacode|reasonix, provider+model fixed, request log: per-request prompt/cache-read/cache-miss/output tokens}` → derived: steady-state rate, prefix-stability, cost/task. **Rule (FR-018/SC-002)**: parity conclusions only from same-script runs; evidence appended under `benchmarks/`, never edited (FR-019).
