# Contract: Completion Integrity & Terminal States

**Feature**: `004-deepseek-agent-polish` | Decisions: [research D2, D6](../research.md) | Data: [data-model §3](../data-model.md)

Requirements FR-013, FR-016, FR-017; SC-004, SC-006, SC-008. Motivated by research B3 (DeepSeek answers-anyway 94–96%, premature completion claims): terminal truth comes from recorded state, never model self-report.

## 1. Terminal-state guarantees

**G1 — Engine event pairing.** Every `ToolStart` callback is paired with exactly one `ToolEnd` on *all* exits: success, tool error, gate block, user cancellation, context cancellation, panic recovery. Every subagent start event is paired with a terminal `done` event carrying a final status (`done`, `failed`, `cancelled`, `turn-limit`). Implementation site: dispatch wrapper (defer-based), subagent runner.

**G2 — TUI task-end sweep (safety net).** When a task's completion/stats message reaches the TUI, any transcript `toolView` still `state=="running"` transitions to `"cancelled"`, and any `agentView` still `"running"` transitions to `"cancelled"` — including nested subagent tool items. Rendered with a neutral terminal glyph; spinners stop. The sweep is idempotent and must not touch items that already reached `ok`/`fail`.

**G3 — Plan-step honesty.** Steps are never auto-completed or auto-failed by the harness; an interrupted run records truth at the plan level (`interrupted`, [plan-lifecycle T11](plan-lifecycle.md)) while step statuses remain exactly what the model last reported.

**G4 — Headless parity.** One-shot runs (`root.go`) get G1 (engine-side) and the plan-phase stamping; there is no transcript to sweep, and the final printed answer carries the §2 disclosures.

Acceptance: SC-004 — 100% of scripted and benchmark runs end with zero items rendered as in-progress and every started work item in a terminal state.

## 2. Completion audit (inside `finalize`, classes ≥ standard)

Inputs (already recorded, no new collection): plan phase + step statuses; `TaskStats{FilesChanged, ChecksRun, StopCause, DoneCriteria}`; final answer text; task class.

| Rule | Condition | Action (append to answer, harness-attributed) |
|---|---|---|
| C1 phase stamp | executing ∧ all steps completed | plan → `finished` (no text needed — summary/plan surfaces reflect it) |
| C2 incomplete disclosure | executing ∧ incomplete steps | plan → `interrupted`; append `— N of M plan steps incomplete: <up to 3 titles>` |
| C3 check-claim reconciliation | answer matches a check-claim pattern ∧ `ChecksRun == 0` | append `(note: no checks were run this task)` |
| C4 empty answer | answer empty after retries | existing `fallbackAnswer` synthesis (files changed summary) + C2/C3 as applicable — never bare `"Done."` |

**Check-claim patterns (C3, fixed, reviewed set)**: case-insensitive presence of `tests pass`, `tests are passing`, `all tests`, `build succeeds`, `build passes`, `vet/lint passes`, `verified by running`. The set is deliberately small and literal — false negatives are acceptable; false positives are not (a wrong "note" would itself be dishonest, VI).

**Hard properties**: the audit is deterministic, adds zero model calls and zero turns, never blocks or reorders finalize, never rewrites or deletes model text (append-only, visually attributed to the harness), and is inert for `chat`/`tiny` classes.

## 3. Recovery & retry posture (FR-016 restated as invariants)

- Transient wire failures: existing provider policy is the contract — retries ≤3 on 408/429/≥5xx or pre-stream network error; never after streaming began; `Retry-After` honored (≤30 s) else exponential+jitter. Task progress is never lost by a retry (request-scoped).
- Zero-token empty completions: the dedicated ladder in [deepseek-wire §5](deepseek-wire.md) (shared ≤2 bound with empty-final retries).
- Permanent failure: surfaced honestly via the existing breaker/finalize path with the task's plan phase stamped per [plan-lifecycle T11](plan-lifecycle.md) and StopCause recorded — the state left behind is resumable (T7/T8).

## 4. Test obligations

1. G1 unit tests: forced error / cancellation / gate-block paths each emit the paired `ToolEnd`; subagent cancellation emits terminal `done`.
2. G2 sweep tests: transcript with stuck `running` tool + agent items → task-end message → all terminal, idempotent on re-delivery.
3. C1–C4 table tests incl. the pattern set (positive + guarded-negative cases) and class-gating.
4. Headless one-shot: interrupted plan → printed answer carries C2 line; exit state resumable.
5. SC-008 measurement hook: benchmark runs report manual-intervention count (target ≥95% intervention-free).
