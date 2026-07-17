# Contract: Unified Task Lifecycle

Feature 010 (US1; FR-001..004). Evidence: [research.md](../research.md) R1;
shapes in [data-model.md](../data-model.md) §1–2.

## 1. One truth

| ID | Requirement |
|---|---|
| UL-1 | Exactly ONE lifecycle state machine exists: the 11-state `Lifecycle` (data-model §1). The fields `planMode`, `pendingPlan`, `planPhase`, and `pipeline` are DELETED from the engine; `DrivenPlanPhase`, `persistedPipelinePhase`, `restoredPipelineState`, and the two-truth reconciliation in `stampPlanCompletionPhase` are deleted with them |
| UL-2 | Consumers read state ONLY through the predicate API (IsReadOnly/BlocksMutation, InvitesProceed, IsApprovalPause, IsPipelineResumable, IsTerminal, IsActive) — no raw-state comparison outside the type and its tests |
| UL-3 | The two mutation gates collapse into ONE gate keyed on `BlocksMutation()`, preserving both escalation counters' bounded behavior and every pinned block/denial text byte-for-byte |
| UL-4 | All transitions go through the single legal-edge table; the terminal-sticky rule (no exit from finished/superseded/discarded except into a fresh planning/research) is enforced in exactly one place |
| UL-5 | Ending/discarding/superseding/parking a plan releases every gate and affordance tied to it atomically in the same transition (FR-004) — verified by the existing 009-polish regression tests, which must pass unchanged |

## 2. Persistence & compatibility

| ID | Requirement |
|---|---|
| UL-6 | The sidecar gains ONE additive `state` field (omitempty); new sidecars write `state`+`depth` only. Malformed-is-absent, terminal-written-not-deleted, and idle-clear semantics are unchanged |
| UL-7 | A single migration loader in `internal/state` is the ONLY code reading the legacy fields; it preserves the two load-time truthfulness corrections verbatim (pending-but-complete → finished; executing-no-pipeline → interrupted/finished by step state) |
| UL-8 | SC-001 grep proof: outside the migration loader (and its tests), zero references to `planMode`, `pendingPlan`, `PlanPhase*` legacy flags, or `PipelinePhase*` remain in production code |
| UL-9 | Restart at EVERY lifecycle state restores that state with plan content, depth, progress, and its exact one-shot restore notice (FR-003); the existing resume-from-every-phase tests pass with only mechanical renames |

## 3. Behavior preservation

| ID | Requirement |
|---|---|
| UL-10 | The approval always-pause survives: the approve transition has no permission-mode branch; auto-accept can never bypass it |
| UL-11 | Every pinned lifecycle test (plan_lifecycle, pipeline gates/polish/fastpath, plan_mode_phase3, restart determinism, completion audit, TUI modal tests) passes after the unification — renames allowed, assertion intent unchanged |
| UL-12 | Lifecycle state stays OFF the byte-stable prefix: the fast-path and prefix-stability tests (prompt byte-identical across all states; direct tasks carry no mode blocks) pass unchanged |
| UL-13 | The plan⇄goal exclusion invariant is preserved as a transition guard |

## Acceptance

- Scripted: drive a task through every state incl. interrupt/restart at each;
  one authoritative value visible at every consumer (US1 scenarios 1–4).
- Static: UL-8 grep proof; deadcode reports the deleted projection helpers gone.
- Full suite green; fault-injection rows for desync-class incidents
  (exit-plan desync, /plan-clear deadlock, stale-approve leak, terminal
  revival) remain green by construction.
