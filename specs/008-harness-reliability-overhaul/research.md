# Phase 0 Research — Harness Reliability & Clarity Overhaul (feature 008)

**Date**: 2026-07-14. Inputs: [spec.md](spec.md), constitution v1.0.0, three parallel
code-inventory passes over the client repo (delegation orchestration; usage/display
plumbing; model-switch flow) plus the Reasonix reference repo (Principle VII). All
findings carry file:line evidence from the current tree. Every plan unknown (R1–R8)
is resolved; no NEEDS CLARIFICATION remains.

---

## R1. Why the model never delegates — root-cause inventory (verified)

The delegation path is **enforcement-only**; nothing affirmatively teaches the model
to delegate:

| # | Finding | Evidence |
|---|---------|----------|
| F1 | The ONLY system-prompt delegation text is one double-hedged sentence ("delegate only … and only when …"), framed as a cost-saver, placed dead-LAST in the ENVIRONMENT section | prompt.go:80-83, :114-119 |
| F2 | The `run_subagent` tool description is six words — "Delegate one independent task to a bounded specialist." — with no when-to-use, no examples; the four agent kinds are bare enum strings (their descriptions never reach the caller) | engine.go:2460-2467; subagent.go:34-53 |
| F3 | Class caps zero the budget for chat/tiny/small (`classAgents = {chat:0,tiny:0,small:0,standard:2,large:5,epic:8}`); effective cap = min(effort, class). The user saying "subagent/delegate" merely bumps class to standard | classify.go:117, :132, :108-111 |
| F4 | The tail brief advertises the allowance ONLY as a ceiling: `agents<=N` / `agents=0 (no run_subagent)` — a cap, never an invitation | classify.go:157-161 |
| F5 | **Dead code**: `EffortProfile.Directives` (the only pro-delegation copy in the effort layer — e.g. "Use parallel agents for independent work and a review pass." at max), `AutoReview`, and `PlanBeforeEdit` are populated but consumed NOWHERE. `ParallelAgents` is read once, only to parallelize already-issued calls | effort.go:11,14,15,31-56; engine.go:1392 |
| F6 | CACHE DISCIPLINE actively competes: "every past read … is already available as current truth. Never re-read…" undercuts the "delegate to save context reads" rationale; OPERATING CONTRACT #2 steers to direct batched reads | prompt.go:99-100, :89 |
| F7 | The model learns about delegation mostly through **post-hoc denials** ("no subagent budget…", "run_subagent is closed for the rest of this task…") | subagent.go:83-104 |
| F8 | The only affirmative advertisement lives inside plan mode and is immediately hedged | plan.go:197-204 |

**Decision D1 — rewrite the inducement surfaces, Reasonix-style, in the static
prefix (one combined cache epoch):**
- A dedicated DELEGATION paragraph in the system prompt (positive decision criteria:
  delegate independent exploration of unfamiliar scope, parallelizable sub-parts,
  and post-edit verification at high allowances; do NOT delegate single-file linear
  work), explicitly reconciled with CACHE DISCIPLINE ("context already read is
  cheap — NEW broad exploration is what you delegate").
- A rich `run_subagent` description with when-to-use + a worked example + per-kind
  guidance (the Reasonix `task` tool pattern, R6), since tool descriptions are the
  reference architecture's strongest inducement surface.
- Static explanation of the dynamic ceiling: the prompt teaches that "agents<=N in
  the task brief is your allowance for THIS task — plan its use for independent
  sub-parts"; the brief itself stays the terse `agents<=N` (≤~50-token rider budget,
  feature 002 SC-004, unchanged).
**Rationale**: fixes F1–F4/F6 at their surfaces; everything lands in byte-stable
prefix content as ONE recorded epoch (Principle III/II). **Alternatives**: per-turn
delegation nudges on the tail — rejected as the primary mechanism (burns rider
budget every turn; the static prompt is cached and free); a conductor command à la
Reasonix `/plan-exec` — rejected (new user surface, spec Out of Scope).

**Decision D2 — consume or delete the dead effort fields:** implement `AutoReview`
as a harness-side tail nudge (at max effort, after file-changing work completes with
agent budget remaining, one dynamic rider suggests a `review` subagent — dynamic
content on the tail, Principle IV); DELETE `Directives` and `PlanBeforeEdit`
(their intent moves into the static DELEGATION text; dead fields violate
clean-code discipline). **Alternative**: wiring `Directives` per-effort into the
prompt — rejected: per-effort prompt text = prefix variance across sessions with
different effort (Principle III).

**Decision D3 — class gating stays, with one adjustment:** `standard` keeps 2;
`large/epic` keep 5/8; chat/tiny/small keep 0 (gratuitous delegation guard,
FR-003/SC-003). No numeric changes (spec Out of Scope) — reliability comes from
inducement, not bigger caps.

## R2. Model-switch warning hook (verified flow)

Flow today: `/model` → role modal → `openModelList(role)` → `chooseModel(role, id)`
→ `SetModel` action → `application.setModel` → `Engine.SwitchModel` (mutates shared
settings pointer, emits `InvalidationEvent{Cause: model-switch}`, saves settings,
`provider.UpdateConfig`). Engine already refuses mid-task (`e.cancel != nil` guard).
Refresh is a separate branch that never calls SwitchModel (actions.go:272-353,
application.go:327-358, engine.go:368-410).

**Decision**: insert the warning in the TUI at the top of `chooseModel`, BEFORE
dispatching `SetModel`:
- Trigger: selected ID ≠ current ID for that role AND
  `m.runtime.Engine.UsageAggregate().Requests ≥ 1` (any completed provider request
  ⇒ warmed cache exists somewhere in the session).
- Modal: existing `openChoice` two-choice pattern; **Cancel is the Recommended
  (default-selected) choice**; Esc = cancel (already clean, no callback). Proceed
  dispatches the unchanged `SetModel` path (invalidation event still emitted by
  the engine — single source of truth).
- Message states: per-model provider cache restarts cold (whole context re-read at
  the uncached rate) and pricing may differ; names the role (main/subagent) and
  both model IDs.
- Non-triggers: same-model reselect; `Requests == 0`; the refresh branch.
**Rationale**: pure pre-dispatch gate; zero engine changes; cancel path provably
side-effect-free (modal machinery already guarantees it). **Alternative**: engine-
side confirmation — rejected (engine has no interaction surface; TUI owns modals).

## R3. Honest headline formula (verified fields)

`m.usage` already receives the per-task delta (`emitTaskUsage` →
`subtractUsage(sessionUsage, taskUsageStart)`); `contract.Usage` carries
`CacheReadTokens/CacheMissTokens *int` and availability flags. Today's headline
renders `TotalTokens` (= prompt+completion, so the fully-resent prompt re-counts
every turn) — the misleading figure the spec targets (view.go:299, :1149-1169).

**Decision**: headline figure = **CacheMissTokens + CompletionTokens** (per-task
delta) when cache metrics are available for the task's records; else fallback to
`TotalTokens` with the cache tag showing unavailable. The live tag simplifies to
`cache N%` (percentage only — read/new detail moves out of the headline into
/context, FR-012). The task-summary line uses the identical formula from
`stats.Usage` (FR-017: one formula, two call sites). Estimated-usage records keep
the existing `~`/estimated markers. **Alternative**: showing both numbers —
rejected (restores the confusion the spec removes).

## R4. Usage-panel data sources — what exists vs what must be added (verified)

| Need (FR-014) | Exists? | Evidence / gap |
|---|---|---|
| Per-model attribution | **YES** — `UsageRecord.Model` populated on every stream; persisted in usage.jsonl; survives resume | cache.go:27-54; engine.go:959,636,1872; subagent.go:229 |
| Per-model aggregation | NO — `AggregateUsage` groups by Stream only | cache.go:153-217 |
| Request duration (API time) | **NO — captured nowhere** (record has only `At`) | cache.go:29; repo-wide grep |
| Task duration | Per-task only (`TaskStats.DurationMS`); no cumulative sum | engine.go:603,667 |
| Lines added/removed | Render-time only (`diffCounts` per tool row); never accumulated | view.go:894-910 |
| Cost | Per-record `CostUSD` + member-set session sum (`SumCreditsUSD`) | cache.go:75-119; engine.go:557 |
| Cache hit % | Session/steady rates in aggregate | cache.go:139-141 |

**Decision**:
- Add `DurationMS *int64` to `UsageRecord` (nullable — old persisted records simply
  lack it; JSON-compatible, no migration). Captured around the provider call and
  threaded through the existing observation structs. API time = Σ non-nil durations.
- Active time = Σ `TaskStats.DurationMS` accumulated in a session counter at task
  end (in-memory; after resume it restarts at 0 and is labeled "this session" —
  honest scope note in the panel).
- Lines ±: count at the tool-outcome path (where `filesChanged` is already
  populated) using the same diff-marker parse as the TUI (extracted to one shared
  helper so the two can't drift); per-task on `TaskStats` + session accumulator.
- New pure `contract.AggregateUsageByModel(records)` → rows {model, uncached (miss),
  output, cacheRead, cost (member-set per model), requests, estimated count}.
**Alternatives**: persisting task durations/lines into a new sidecar — rejected
(state-layout compat boundary; in-memory session scope satisfies the spec).

## R5. Context-category estimation (verified inputs)

Available today: calibrated `tokPerChar` estimator (history.go:606-627), history
total, compact-summary length, context limit, free = limit − history. NOT measured
anywhere: system-prompt size (transient `len(promptText)`), tool-definitions size,
project-memory block size, skills-section size.

**Decision**: at request-assembly time the engine records byte sizes of the
components it already holds in hand (base prompt, skills section, project-context
block, marshaled tool definitions, compact summary) and converts via the calibrated
`tokPerChar`; messages = history estimate − summary; free = limit − Σ. Exposed as
`ContextReport.Categories` (name, tokens, percent), all labeled estimated.
Categories: system prompt · tool definitions · project memory & skills ·
conversation · summary · free. **Alternative**: exact tokenization — rejected (no
tokenizer dependency; the estimator is already calibrated against provider counts,
Principle IX no-new-deps).

## R6. Reasonix reference pass (constitution Principle VII gate)

- Reasonix's base system prompt contains **no delegation text at all**; its
  inducement is concentrated in the **`task` tool description** — concrete, with a
  worked example: "Use this to (a) keep long exploration sequences out of the
  parent's context budget, or (b) delegate self-contained work like 'find every
  place that calls X and summarise the patterns'." (task.go:268-269, :323-324) plus
  schema-field guidance ("Be specific about the deliverable — the sub-agent does
  not see this conversation", task.go:276) and a plan-exec conductor
  (controller.go:1110,1119).
- Feature 002's inventory treated subagents as isolation plumbing only (rows 12,
  39-40) — inducement copy was never in scope there; this feature adds it.
- **Adopted**: tool-description-centric inducement (D1), deliverable-focused
  schema-field guidance, child-side self-containment phrasing. **Adapted**: our
  criteria live in BOTH the tool description and a prompt DELEGATION paragraph
  (DeepSeek benefits from redundant imperative instruction — the spec's weakness
  premise). **Rejected**: the conductor command (new surface, Out of Scope).

**DeepSeek failure-mode evidence in this repo (grounds FR-004/005):**
- **Near-miss edits are classified as SUCCESS**: `edit_file`/`multi_edit` no-match
  returns `err=nil` with Summary "No changes needed." + note "oldString not found;
  closest region: …" — `IsToolFailure` sees success, so the failure terminator,
  consecutive-failure nudges, and failed-call cache ALL miss DeepSeek's most common
  edit-fumble loop (files.go:360-393, :261-263; registry.go:219-231;
  engine.go:1570-1583). `apply_patch` mismatches DO error and count.
- Existing compensations: DSML tool-call rescue (rescue.go), DeepSeek prompt
  addendum, empty-`reasoning_content` replay (feature 002 F7).

**Decision D4 — edit-mismatch reclassification**: "oldString not found" and
"appears N times" outcomes become **failures** (Failed=true) — keeping their
helpful note text — so the existing bounded loop guards finally cover the
edit-fumble loop; genuinely idempotent outcomes ("newString already present",
"identical; skipped") stay successes. **Rationale**: FR-004(a)/FR-005 with zero new
mechanisms — the guards exist, they just never fire. **Alternative**: a new
edit-specific retry counter — rejected (duplicate mechanism; Principle VIII).

## R7. Delegation benchmark protocol (Principle X)

**Decision**: two scripted live workloads checked into
`specs/008-…/benchmarks/`, run before/after on the same build pair, model,
gateway, and effort:
1. **Delegation workload** (max effort): one prompt over a fixture tree with 3–4
   genuinely independent sub-scopes (audit + small change each) — mirrors the real
   32-request/0-subagent session's shape. Metrics from usage records + TaskStats:
   `AgentRuns` (SC-001 ≥2), final main-stream `PromptTokens` (SC-001 ≥25% smaller),
   billed = Σmiss+Σcompletion (SC-002 ≤+10%), steady-state hit rate (SC-002 no
   regression), duplicate-read count (FR-006), correctness checklist (expected
   files modified/verified).
2. **Control workload** (low effort): single-file fix — expects `AgentRuns == 0`
   (SC-003).
Runner reuses the cachebench harness pattern (scripted turns against the live
gateway, results as JSON in benchmarks/before|after). **Alternative**: offline
scripted-provider simulation — kept for unit-level behavior tests but rejected as
the improvement evidence (constitution X requires a real endpoint).

## R8. Panel layout under modal constraints (verified constraints)

The `/context` modal body is a single wrapped string: width capped at ~72 usable
columns regardless of terminal, tall content row-clipped, and every line passes
through the RTL display pass (which would break wide multi-column art but leaves
left-aligned label/value lines intact) (view.go:493-603, :607-613).

**Decision**: extend the existing `/context` info modal (no new command) with the
session panel FIRST, then the existing sections, then categories — all in the
label/value style already used (2-space indents, padded numeric columns ≤72 chars,
per-model rows as `  <model>  in X · out Y · read Z · cost C`), honoring
unavailable-state rendering per metric. Live line + summary keep their one-line
formats. **Alternative**: a separate full-screen usage view — rejected for scope
(modal machinery suffices; Principle VIII smallest change).

---

## Appendix — evidence index

- **Delegation inventory** (F1–F8, Reasonix quotes, edit-failure flow):
  prompt.go:75-134; engine.go:2460-2475, :1392, :1499-1583, :1146-1150, :38-42;
  classify.go:61, :66-175; subagent.go:34-53, :80-134, :151-168; effort.go:11-56;
  plan.go:197-204; files.go:224-276, :360-393; patch.go:181,187;
  registry.go:189-231; rescue.go:15-91; Reasonix task.go:22-39, :268-276,
  :323-324; controller.go:1110,1119; specs/002 research.md rows 12/39-40.
- **Usage plumbing**: types.go:159-185, :399-443; cache.go:27-54, :75-150,
  :153-217; engine.go:463-473, :531-575, :603-677, :959, :2193-2318, :2362-2366,
  :2577-2598; state/session.go:79-84; provider.go:34-38, :259-290;
  history.go:121-131, :567-627; view.go:275-344, :493-613, :894-910, :1149-1185;
  actions.go:31-36, :457-540, :696-851; model.go:143, :163, :169-182, :633-634,
  :1176-1177; bridge.go:24,89; credits.go:9-13.
- **Model-switch flow**: actions.go:70-71, :272-353; application.go:102, :297-310,
  :327-358, :718-719, :803; engine.go:368-410, :2001-2004; config.go:231-280;
  cache.go:243, :253, :258-267; invalidation.go:40-48; view.go:556-587;
  mouse.go:380-388.
