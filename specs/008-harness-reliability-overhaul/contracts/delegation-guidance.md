# Contract: Delegation Guidance & Bounded Recovery

Feature 008. Governs the surfaces that make the model delegate reliably and the
failure classification that keeps recovery bounded. Evidence:
[research.md](../research.md) R1/R6 (F1–F8, D1–D4).

## 1. Inducement surfaces (static prefix — one cache epoch)

| ID | Requirement |
|---|---|
| DG-1 | The system prompt MUST contain a dedicated DELEGATION section (not a trailing ENVIRONMENT clause) stating positive criteria: delegate (a) independent exploration of scope not yet in context, (b) genuinely parallelizable sub-parts, (c) verification/review after substantial multi-file edits when allowance remains; and negative criteria: never delegate single-file linear work or work whose inputs are already fully in context |
| DG-2 | The DELEGATION section MUST reconcile with CACHE DISCIPLINE explicitly: already-read context is cheap and reusable; NEW broad exploration is the thing to delegate — the two sections must not contradict |
| DG-3 | The section MUST teach the brief semantics: `agents<=N` is this task's allowance to PLAN with (an invitation up to N), `agents=0` is a prohibition |
| DG-4 | The `run_subagent` tool description MUST contain: when-to-use guidance, at least one concrete worked example, per-kind selection guidance (explore/plan/review/general), and deliverable-focused task-field guidance ("the subagent does not see this conversation — be specific about the deliverable") |
| DG-5 | All DG-1..4 text is compile-time constant, byte-stable across turns and sessions given identical configuration; shipping it is ONE recorded upgrade epoch; prefix-stability and marshal-determinism tests MUST cover the new text |
| DG-6 | Per-effort wording variance in the prefix is FORBIDDEN (low and max effort see identical static text; the dynamic `agents<=N` brief carries the difference) |

## 2. Dynamic riders (user-message tail only)

| ID | Requirement |
|---|---|
| DG-7 | The AutoReview nudge (max effort, file-changing task, agent budget remaining at completion-approach) rides the user-message tail, ≤ the established ~50-token rider budget, and appears at most once per task |
| DG-8 | No other new per-turn delegation text may be injected anywhere (denials and budget notes remain exactly as today) |

## 3. Bounded recovery (edit-fumble loop)

| ID | Requirement |
|---|---|
| DG-9 | `oldString not found` and `oldString appears N times` edit outcomes MUST classify as failures (feed `recordTaskFailure`, consecutive-failure nudges, and the failed-call cache) while keeping their existing helpful note text verbatim |
| DG-10 | Idempotent outcomes (`newString already present`, `identical; skipped`) MUST remain successes |
| DG-11 | The existing bounded guards are the ONLY loop stops: failure terminator (8-in-6-turns), loop-guard nudges, per-effort turn budget. Reintroducing any cumulative token ceiling is a contract violation (spec FR-005) |
| DG-12 | A failed subagent run MUST NOT be re-delegated verbatim (the existing failed-call cache covers `run_subagent` like any tool); the main loop may finish that sub-part directly |

## 4. Report consumption

| ID | Requirement |
|---|---|
| DG-13 | The DELEGATION section MUST instruct: after a subagent report, use its findings; re-read only the specific ranges you must edit — never wholesale re-explore a delegated scope |
| DG-14 | Benchmark measurement (duplicate-read counters) is the enforcement for DG-13 in this feature; parent-ledger integration of subagent reads is a recorded fallback option, not implemented now |

## 5. Acceptance (maps to spec)

- Delegation benchmark (quickstart §1): ≥2 subagent runs, ≥25% smaller final main
  conversation, ≤+10% billed tokens, no steady-state cache regression, correct
  completion (SC-001/002).
- Control workload: 0 subagent runs at low effort (SC-003).
- Scripted-provider unit tests: near-miss edit loop now trips the loop guards
  within the existing bounds; denial texts unchanged (DG-9/11).
- Prefix checks green with the new text (DG-5).
