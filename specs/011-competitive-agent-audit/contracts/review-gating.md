# Contract: Review Gating (TaskProfile → ReviewDecision)

**Consumers**: pipeline validation dispatch (`phaserunners.go:41-45`) and the AutoReview nudge (`turnloop.go:630-642`). Both MUST consult this contract before dispatching/nudging a review; neither may dispatch unconditionally after this feature lands.

## 1. Decision function

```
Decide(profile TaskProfile) → ReviewDecision
```

Deterministic, side-effect-free, zero token cost. Same profile ⇒ same decision (testable as a pure table).

## 2. Hard rules (evaluated first, in order)

| # | Condition | Outcome |
|---|---|---|
| H1 | `ExplicitReviewRequest` | **bypass** — explicit requests dispatch directly via the ungated path (§6a) and never enter gating; defense in depth: if a flagged profile reaches `Decide()` anyway, tier ≥ focused (FR-009) |
| H2 | `GatingMode == off` | `skip` (unless H1) |
| H3 | `FilesChanged == 0` | `skip` — nothing to review |
| H4 | `RiskAreas ≠ none` | tier ≥ focused — risk outranks size (spec US2-AS2: a one-line auth change reviews) |
| H5 | `TestOutcome == failed` | tier ≥ focused |

## 3. Default tier selection (after hard rules)

| TaskType | Size signal | default mode | conservative mode |
|---|---|---|---|
| docs / comment / format / rename (tests passing) | any | **skip** | **skip** |
| config | ≤ 2 files | skip | focused |
| logic / mixed | ≤ 2 files AND ≤ 40 changed lines | skip | focused |
| logic / mixed | > 2 files OR > 40 lines | **focused** | focused |
| logic / mixed | Large/Epic class OR > 8 files | **deep** | deep |

**Modifiers (applied after the table, before ceilings):**

- **Greenfield cap**: if `RepoSizeBucket == tiny` (initial scaffolding — the same <3-source-file signal `dynamicfanout.go` uses for greenfield-skip) AND `RiskAreas == none`, cap the selected tier at `focused`. This satisfies the spec edge case "heavyweight review depths must not fire on initial scaffolding" — a 12-file scaffold is size-`deep` by the table but greenfield-capped to `focused`. Risk areas override the cap (a scaffold that writes an auth module still gets full depth).

Thresholds (40 lines, 8 files) and the greenfield rule are initial defaults; they MUST be tuned against baseline data (D9) before defaults ship, and the tuned values recorded in this section.

## 4. Ceilings (per tier)

| Tier | Proportional cap | Absolute cap | Scope |
|---|---|---|---|
| skip | — | 0 | — |
| focused | ≤ 20% of the task's own provider-reported spend | per-tier token ceiling (see "Initial ceiling values" below) | changed files + direct dependents ONLY |
| deep | ≤ 35% | higher ceiling, same scaling | focused scope + acceptance/verification commands (current `PipelineValidateTaskTmpl` behavior) |

**Effective ceiling** at dispatch = `min(proportional-cap-resolved-to-tokens, absolute-cap)`. The absolute cap is copied into the dispatched subagent's `TokenCeiling` (data-model.md §3); the proportional cap is resolved to a token count from this task's running provider-reported spend at dispatch time.

**Initial ceiling values** (T021 sets these): base absolute cap per tier = the **p75 of baseline review-subagent total-token spend per `task_class`**, read from the per-class review-spend table that T007 writes into `benchmarks/BASELINES.md` (computed from the raw `runs/baseline/` records' `review.spend_tokens` + `task_class` fields; focused ← `standard`, deep ← `large`/`epic`), then complexity-scaled by `RepoSizeBucket` (tiny ×0.5, small ×1, medium ×1.5, large ×2). The concrete integers chosen MUST be written back into this table when T021 runs. Until baselines exist the ceiling is 0 (uncapped) so no arbitrary number ships unmeasured (Constitution VI).

**Direct dependents** (focused scope) = the changed files PLUS: (a) other files in the same Go package(s) as any changed file, and (b) one-hop reverse dependents — files whose package imports a changed file's package — discovered via `go list`/import graph, **capped at 15 files total**; overflow is dropped and named in `CoverageReport.skipped`. No transitive closure (bounded cost on large repos, spec FR-008).

Ceiling enforcement uses provider-reported usage per subagent turn (Constitution VI). Ceiling hit ⇒ one wrap-up turn ⇒ `CoverageReport` with explicit covered/skipped lists — never silent truncation, never overspend.

## 5. User-visible rationale (SC-009)

Every decision — including `skip` — emits exactly one line:

```
review: <tier> — <reason>
```

Examples: `review: skip — docs-only change, tests unaffected` · `review: focused — auth-adjacent edit in 1 file` · `review: deep — 12-file logic change (Large)`. The line rides task completion output / the turn tail, NEVER the stable prefix (Constitution IV).

## 6. Configuration surface

`review_gating`: `off | conservative | default` (settings + `/config`-style command). `conservative` biases toward reviewing (see table); `off` disables automatic review only — explicit requests always work. Trigger B additionally remains bound by its existing conditions (max effort, agent budget) — this contract can only *suppress or shape* it, never widen it.

**§6a Explicit-request path (FR-009 / SC-002).** An explicit user review request runs through the model-invoked `run_subagent("review")` tool path — the route natural-language requests already take today. That path is NEVER wired through `Decide()` and works in every gating mode **including `off`**. The gate wires ONLY the two automatic triggers (pipeline validation dispatch, AutoReview nudge). This resolves any apparent tension between H1 and the suppress-only rule above: explicit requests never reach Trigger B's site, so nothing ever widens it.

## 7. Conformance tests (minimum)

1. Table-driven `Decide` test covering every row above + all hard rules.
2. Trivial-category suite tasks produce `skip` for **more than 90%** (strictly matching SC-001's "under 10%" auto-review) and risky tasks produce ≥ focused ≥ 95% (SC-002) on the benchmark matrix.
3. Ceiling-compliance test: synthetic oversized review hits the cap, wraps up, and reports partial coverage (SC-004).
4. Determinism: identical profiles across 100 shuffled runs yield identical decisions.
5. Explicit-request test (§6a): a model-invoked `run_subagent("review")` dispatches in every gating mode — including `off` — untouched by `Decide()`.
