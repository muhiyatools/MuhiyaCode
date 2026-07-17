# Data Model — Harness Reliability & Clarity Overhaul (feature 008)

**Date**: 2026-07-14. Derived from [spec.md](spec.md) Key Entities +
[research.md](research.md) decisions. Only entities this feature creates or changes
are modeled; verified current shapes cited from the research evidence index.

## 1. DelegationGuidance (changed, static prompt content)

The rewritten inducement surfaces. All parts are compile-time constants — one
combined **cache epoch** on upgrade (recorded invalidation), byte-stable thereafter.

| Part | Placement | Content requirement |
|---|---|---|
| DELEGATION paragraph | System prompt, own section (no longer a trailing ENVIRONMENT clause) | Positive criteria: delegate independent exploration of unfamiliar scope, parallelizable sub-parts, verification after substantial edits; do NOT delegate single-file linear work; reconciles CACHE DISCIPLINE ("already-read context is cheap — NEW broad exploration is what you delegate"); explains the brief's `agents<=N` as this task's allowance to plan with |
| `run_subagent` description | Tool definition (part of tools serialization → prefix) | When-to-use + worked example + per-kind guidance (explore/plan/review/general), deliverable-focused task-field guidance ("the subagent does not see this conversation") — Reasonix `task`-tool pattern |
| AutoReview nudge | **Dynamic** user-message tail rider (max effort only, post-edit, budget remaining) | One sentence suggesting a `review` subagent; fits the existing ≤~50-token rider budget (feature 002 SC-004) |
| Denial/budget texts | Unchanged (subagent.go) | Regression-guarded |

**Removed**: `EffortProfile.Directives`, `EffortProfile.PlanBeforeEdit` (dead
fields — intent folded into the static paragraph). `AutoReview` becomes consumed
(the nudge above). `ParallelAgents` unchanged.

**Validation rules**: prefix-stability tests must cover the new prompt text and
tool description; the rider is the only dynamic part and rides the tail.

## 2. EditOutcomeClassification (changed, orchestrator/workspace boundary)

Re-classification of edit results so loop guards cover the DeepSeek edit-fumble
loop (research D4):

| Outcome (existing note text kept) | Today | New |
|---|---|---|
| `oldString not found; closest region: …` | success (err=nil) | **failure** (counts toward terminator/nudges/failed-call cache) |
| `oldString appears N times; add surrounding context…` | success | **failure** |
| `newString is already present; skipped stale edit.` | success | success (idempotent) |
| `oldString and newString are identical; skipped.` | success | success |
| `apply_patch` context/deletion mismatch | failure | failure (unchanged) |

**State transitions**: unchanged failure machinery — `recordTaskFailure` →
window count → `maxTaskFailures(8)/taskFailureWindow(6)` terminator; nudges at 2
all-failed turns / 3 consecutive failures. No new counters (FR-005: no token
ceiling; bounded by existing guards).

## 3. ModelSwitchWarning (new, TUI-only)

| Field | Definition |
|---|---|
| Trigger | `selectedID != currentID(role)` AND `Engine.UsageAggregate().Requests ≥ 1` |
| Non-triggers | same-model reselect; zero completed requests; refresh branch; initial setup |
| Content | role (main/subagent), old ID → new ID, cache-cold consequence, pricing-may-differ note |
| Choices | `[Switch model, Cancel]` — **Cancel carries Recommended** (default selection); Esc ≡ Cancel |
| Proceed path | unchanged `SetModel` dispatch → engine `SwitchModel` (existing invalidation event remains the single record) |
| Cancel path | modal closes, zero state changes (existing `closeModal(-1)` semantics) |

## 4. SessionUsageLedger extensions (changed, contract + engine + state)

| Change | Shape | Persistence |
|---|---|---|
| `UsageRecord.DurationMS *int64` (new) | wall-clock of the provider request; nil when unknown (old records, failures before send) | usage.jsonl (nullable JSON field — backward/forward compatible; no migration) |
| `TaskStats.LinesAdded/LinesRemoved int` (new) | Σ diff adds/removes of applied (successful) file-changing calls in the task, via ONE shared diff-count helper (TUI + engine use the same function) | in TaskStats (transcript summary), not persisted separately |
| Session accumulators (new, engine, in-memory) | `sessionActiveMS` (Σ TaskStats.DurationMS), `sessionLinesAdded/Removed` | session-scoped; reset on resume; panel labels them "this session" |
| `AggregateUsageByModel(records)` (new pure func, contract) | rows: {Model, Requests, UncachedIn (Σmiss), Output (Σcompletion), CacheRead (Σread), Cost (member-set per model: nil if any priced-eligible record lacks cost), EstimatedCount} + Total row | computed on demand from existing persisted records (model attribution already present) |

## 5. HeadlineFigure (changed, display formula)

| Case | Headline | Cache tag |
|---|---|---|
| Task records have cache metrics | `CacheMissTokens + CompletionTokens` (per-task delta) | `cache N%` (percentage only) |
| No cache metrics | `TotalTokens` (delta) | `cache unavailable` |
| Estimated usage present | same formulas + existing estimate marker | unchanged |

One formula, two call sites (live activity line; task-summary line), both reading
the same per-task delta source (FR-017). Full read/uncached/per-stream detail
remains in `/context` (unchanged fields).

## 6. ContextCategories (new, ContextReport extension)

`ContextReport.Categories []ContextCategory{Name, Tokens, Percent, Estimated}` —
computed at request-assembly from component byte sizes already in hand × calibrated
`tokPerChar`:

| Category | Source |
|---|---|
| System prompt | base prompt + model addendum text length |
| Tool definitions | marshaled session definitions length (measured at tool-boundary changes) |
| Project memory & skills | project-context block + skills section lengths |
| Conversation | history estimate − summary estimate |
| Summary | compact-summary length |
| Free | contextLimit − Σ above (floor 0) |

**Invariant**: Σ(categories incl. Free) = ContextLimit ± rounding (SC-006); every
row labeled estimated.

## 7. Relationships

```text
EffortProfile ──(MaxAgentRuns; ParallelAgents)──▶ Budget (min with classAgents)   [unchanged]
Budget.Brief  ──(tail rider `agents<=N`)──▶ model                                  [unchanged]
DelegationGuidance(static) + Brief(dynamic) ──▶ delegation decisions               [new inducement]
toolOutcome ──▶ EditOutcomeClassification ──▶ taskFailures window ──▶ terminator   [reclassified]
UsageRecord(+DurationMS) ──▶ AggregateUsage (by stream)  ──▶ headline/summary
                        └──▶ AggregateUsageByModel        ──▶ usage panel
TaskStats(+Lines±, DurationMS) ──▶ session accumulators   ──▶ usage panel
Engine assembly sizes ──▶ ContextReport.Categories        ──▶ /context table
```

## 8. Explicitly unchanged (compat boundaries)

`~/.muhiya` layout (usage.jsonl gains only a nullable field); effort→allowance
numbers; classAgents caps; subagent kinds/tool surfaces; denial texts; the
invalidation-event machinery; wire protocol (client↔gateway); the removed token
ceiling stays removed.
