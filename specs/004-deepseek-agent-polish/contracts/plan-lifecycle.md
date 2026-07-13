# Contract: Plan Lifecycle

**Feature**: `004-deepseek-agent-polish` | Decision: [research D1](../research.md) | Data: [data-model §1](../data-model.md)

Governs the plan phase machine, its persistence, and every user-visible plan affordance. Requirements FR-001..005; SC-001.

## 1. Phase transitions (authoritative table)

Legend: choke points are the *only* places a transition may be wired.

| # | From | Trigger (choke point) | To | Side effects |
|---|---|---|---|---|
| T1 | any non-terminal | `SetPlanMode(true)` (`/plan` on) | `drafting` | Goal cleared (existing G3); sidecar written |
| T2 | `drafting` | Plan-ready: `exit_plan_mode` sentinel or `maybeSignalPlanReady` — **now requires ≥1 incomplete step** | `ready` | If a previous plan sits `pending`/`interrupted`, it transitions `superseded` (T9) before the new plan becomes `ready` |
| T3 | `ready` | Modal "Proceed now" | `executing` | `pendingPlan=false`, `planMode=false`; proceed prompt submitted |
| T4 | `ready` | Modal "Proceed later" | `pending` | `pendingPlan=true`, `planMode=false`; save notice (once) |
| T5 | `ready` | Modal "Keep planning" | `drafting` | flags untouched (plan mode stays on) |
| T6 | `drafting` | One-shot plan-ready (no interactive client) | `pending` | existing `SetPendingPlan(true)` + `SetPlanMode(false)` path |
| T7 | `pending`, `interrupted` | Run start with continuation match (widened word-boundary phrase list, [mode-capability §5](mode-capability.md)) — plan markdown injected | `executing` | `pendingPlan=false`; `[executing saved plan]` brief rider (existing) |
| T8 | `pending`, `interrupted`, `ready` | First `update_plan` call that marks any step `in_progress`/`completed` while plan mode is off (step-progress-driven detection) | `executing` | `pendingPlan=false`; covers natural phrasings that miss T7 |
| T9 | `pending`, `interrupted` | A newer plan reaches `ready` (T2) | `superseded` | terminal; hint withdrawn; event recorded |
| T10 | `executing` | `finalize` with `allStepsCompleted` | `finished` | terminal; sidecar resolved (§3); progress line retires (§4) |
| T11 | `executing` | Run ends with `hasIncompleteSteps` (finalize, user stop, error, breaker, turn-cap) | `interrupted` | wording keyed on `TaskStats.StopCause` |
| T12 | any non-terminal | `/plan clear` (new action) | `discarded` | terminal; sidecar resolved; plan.md left on disk as a record |

**Invariants**: exactly one plan lifecycle at a time; terminal phases (`finished`, `superseded`, `discarded`) never transition out — a new `/plan` starts a fresh lifecycle at T1. `executing` ⇒ plan mode off. Phase changes and flag changes happen under the existing `modeMu`. Steps are never auto-completed by the harness.

## 2. Execution-start detection (the A2 fix)

A saved plan counts as *executing* when **any** of: T3 (explicit modal), T7 (continuation phrase — accelerator only), or T8 (step progress — the backstop that is phrasing-independent). T8 is the load-bearing rule: however the user phrased consent, the run that starts advancing plan steps flips `pending → executing`, which is what guarantees T10/T11 can later resolve the lifecycle and the stale-hint class (research A2) is structurally closed.

## 3. Persistence (`plan_state.json`)

- Written on every phase change: `{ "planMode": bool, "pendingPlan": bool, "phase": "<value>" }` — booleans stay consistent with phase (data-model §1.3 validation).
- Terminal resolution: on `finished`/`superseded`/`discarded` the sidecar is written with the terminal phase and both booleans false. (Writing, not deleting, lets resume distinguish "finished plan exists" from "no plan ever" for truthful display; `ClearPlanState`'s delete remains for `none`.)
- Legacy snapshots (no `phase`): derive `pending` / `drafting` / `none` (data-model §1.3). A legacy `pending` derivation for a plan whose steps are all completed is corrected to `finished` at load time (belt-and-suspenders for sessions predating this feature — this specifically kills pre-existing stale hints on first resume).
- `plan.md`/`tasks.md` remain append/overwrite-only records; content is never destroyed by lifecycle transitions.

## 4. Affordance matrix (every surface, by phase)

Surfaces from research A3. "—" = must not render.

| Surface | drafting | ready | pending | executing | interrupted | finished / superseded / discarded |
|---|---|---|---|---|---|---|
| Startup/resume notice (`engine` restored notice → `root.go` / `actions.go`) | "Plan mode restored…" (existing) | — (modal re-offered instead) | "Saved plan pending — say 'proceed' (or 'go ahead') to execute it." | — | "Plan partially executed — N/M steps done; say 'proceed' to resume." | — |
| Plan-ready modal | — | ✓ (owns the decision) | — | — | — | — |
| Save notice (one-time, on T4) | — | ✓ at transition | — | — | — | — |
| One-shot finalize answer | — | — | "Plan ready — saved. Say 'proceed' (or 'go ahead') to execute it." (T6 moment only) | — | — | — |
| Progress line (`view.go` `plan N/M`) | ✓ | ✓ | ✓ | ✓ | ✓ ("N/M · interrupted") | — |
| Footer "· plan mode" | ✓ | ✓ (mode still on) | — | — | — | — |

**Hard rule (FR-002/FR-003)**: text implying the plan can be executed appears **only** in `pending` (and `interrupted`, in resume-partial wording). The moment T7/T8/T10/T11/T9/T12 fire, the affordance is withdrawn on the very next render/launch — no surface caches it.

## 5. Resume & abnormal termination (FR-004)

- Resume reads phase from the sidecar; notices follow §4 exactly. A `finished` plan resumes silent.
- Crash mid-`executing`: next load sees phase `executing` with no live run → corrected to `interrupted` at load (StopCause `disconnect` wording).
- `maybeSignalPlanReady` re-fire over a completed plan (research A2 compounding factor) is impossible: T2 requires ≥1 incomplete step.

## 6. Test obligations

1. Transition unit tests: every row T1–T12, including the T8 phrasing-independent path ("proceed with the plan", "yes, build it") and the T2 supersede chain.
2. Save/resume integration: `finished` → relaunch → no notice, no progress line, sidecar terminal (SC-001's resume clause).
3. Legacy-snapshot derivation incl. the completed-steps correction (§3).
4. Interrupted paths: user stop, error, turn-cap → phase + wording per §4; resume → T7/T8 back to executing.
5. Headless (`root.go`) parity for the notice matrix.
