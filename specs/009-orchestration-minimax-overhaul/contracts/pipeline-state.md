# Contract: Orchestration Pipeline State Machine

Feature 009 (US1, FR-001..006). The enforced research→plan→approve→implement→
validate flow. Built on existing primitives ([research.md](../research.md) R1–R5);
states in [data-model.md](../data-model.md) §1.

## 1. Entry & the needs-a-plan decision

| ID | Requirement |
|---|---|
| PL-1 | Every task is evaluated by `NeedsPlan(assessment)`: true for Class ∈ {standard, large, epic} that is not a pure no-workspace question. The verdict and its reason MUST be recorded and visible in the session (FR-001) |
| PL-2 | `NeedsPlan == false` ⇒ `direct` phase: the task runs exactly as today with NO plan, NO subagents forced, NO gating — the simple-task fast path is byte-unchanged (FR-006) |
| PL-3 | `NeedsPlan == true` ⇒ the pipeline; depth = light (standard) or full (large/epic) per data-model §2. There is NO user override surface, per-task or global (clarified 2026-07-14) |
| PL-4 | Per-phase subagent COUNTS scale by effort only (existing classAgents map, unchanged); task size selects which phases are mandatory |

## 2. Phase gates (harness-enforced, not prompt-suggested)

| ID | Requirement |
|---|---|
| PL-5 | **Research gate**: on entry a pipeline task is in the read-only research state (existing plan-mode block) with an agent floor ≥1; it MUST complete ≥1 research subagent (or record research degradation) before the plan phase |
| PL-6 | **Implementation gate**: a pipeline task's main-loop mutating tool calls MUST be blocked until phase == `implement` (after research-done + plan-written + approved), reusing the existing plan-mode mutation block + bounded escalation (no new loop mechanism; no token ceiling) |
| PL-7 | **Anti-premature-finish gate**: the pipeline MUST NOT reach `done` while `hasIncompletePlan()` is true or the validate phase has not confirmed the plan's acceptance checks; a premature finalize stamps `Interrupted` (resumable), never a false `Finished` (FR-005, US1-3) |
| PL-8 | Every gate degrades gracefully: empty research, exhausted allowance, or a failed phase falls back to bounded direct work with the reason recorded — the pipeline can NEVER deadlock on an empty phase (FR-005, US1-6) |

## 3. Approval pause (overrides auto-accept)

| ID | Requirement |
|---|---|
| PL-9 | After the plan is written, the pipeline MUST pause and present the approval flow gate — approve / steer (keep planning) / cancel — for EVERY pipeline task, INCLUDING under auto-accept permission mode (it is a flow gate, independent of PermissionMode) |
| PL-10 | Cancel/no-response persists the plan as `Pending` and ends the task cleanly; a later proceed resumes implementation from the approved plan (no work lost; nothing implements without approval) — FR-002, edge cases |
| PL-11 | Headless (`-p`) tasks write the plan, save it Pending, and print the proceed instruction; they NEVER auto-implement (the existing no-`TaskComplete` one-shot signal) |
| PL-12 | Steer re-enters the pipeline at the appropriate phase (research/plan) rather than abandoning the plan; an active goal at the pause still pauses (approval is universal) and resumes after go-ahead |

## 4. Phase execution

| ID | Requirement |
|---|---|
| PL-13 | Research: parallel `explore`/`plan`/`review` subagents, one per independent scope, banking structured reports (handoff-contract) |
| PL-14 | Plan: the main agent synthesizes banked findings into the execution-grade plan artifact (contract handoff-contract §plan; FR-007/008) and calls `exit_plan_mode` |
| PL-15 | Implement (after approval): the main agent divides the plan's independent steps and launches implementation subagents (full depth) or executes main-loop (light depth); each subagent reports changes, validation, problems, remaining concerns |
| PL-16 | Validate: the main agent runs or delegates a review pass over the changed files + the plan's verification checks; findings are addressed or explicitly reported before `done` |
| PL-17 | Every sub-agent in every phase runs on the configured sub-agent model, verifiable per request in usage records (FR-004) |

## 5. Acceptance (maps to spec)

- SC-001: on the delegation benchmark (max effort) — ≥2 research runs before any
  file-changing call, plan artifact on disk before the first edit, ≥2 implementation
  runs, ≥1 validation run before final answer.
- SC-002: the simple control task — 0 subagents, 0 plan file, correct completion,
  turns/time within +10% of the current control baseline (fast path intact).
- SC-003: executing only the plan's steps passes 8/8; final main conversation ≤50%
  of the no-pipeline baseline on the same workload.
- Scripted-provider unit tests: NeedsPlan predicate; each gate (research-before-plan,
  no-mutation-before-approve, approval-pause-under-auto-accept, anti-premature-finish
  → Interrupted); degradation paths; headless proceed-later; resume from each phase.
