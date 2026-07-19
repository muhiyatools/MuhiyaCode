# Data Model: Competitive Agent Audit & Transformation

**Feature**: `011-competitive-agent-audit` · **Date**: 2026-07-19
**Sources**: spec.md Key Entities · research.md decisions D1–D10 · existing types this model extends (`HandoffContract` `subagent.go:43-54`, `Budget` `classify.go:211-265`, `AggregateUsage`/`TaskStats` `usage.go`, `EffortProfile` `effort.go`)

## 1. TaskProfile (new — D1)

The signal set describing a just-completed (or about-to-complete) task, assembled at review-dispatch time.

| Field | Type | Source | Validation |
|---|---|---|---|
| `TaskClass` | enum chat/tiny/small/standard/large/epic | existing `Classify` output | must equal the class used for budgets this task |
| `FilesChanged` | int ≥ 0 | git diff stat (workspace tools) | 0 ⇒ tier can only be `skip` |
| `LinesAdded` / `LinesDeleted` | int ≥ 0 | git diff stat | — |
| `TaskType` | enum docs / comment / format / rename / config / logic / mixed | diff content classes | `mixed` when >1 class detected |
| `RiskAreas` | []enum auth / billing / concurrency / security-config / migration / none | path+pattern match on changed files | non-empty ⇒ never `skip` (spec US2-AS2) |
| `TestOutcome` | enum passed / failed / not-run | this task's tool results | `failed` ⇒ never `skip` |
| `RepoSizeBucket` | enum tiny / small / medium / large | `dynamicfanout` probe reuse | same buckets as lens-scaling |
| `ExplicitReviewRequest` | bool | user prompt / command | `true` ⇒ gating bypassed entirely (FR-009) |
| `GatingMode` | enum off / conservative / default | settings | `off` ⇒ decision is always `skip` unless explicit |

**Validation rules**: profile assembly MUST be deterministic and side-effect-free (no model calls, no token spend); all fields derivable from state already held at the dispatch site.

## 2. ReviewDecision (new — D1/D6)

| Field | Type | Rules |
|---|---|---|
| `Tier` | enum skip / focused / deep | selection table in contracts/review-gating.md |
| `Rationale` | string ≤ 120 chars | ALWAYS user-visible (SC-009); format `review: <tier> — <reason>` |
| `ProportionalCapPct` | int | ceiling as % of this task's own provider-reported spend |
| `AbsoluteCapTokens` | int | per-tier hard ceiling |
| `Scope.Files` | []path | changed files (focused) |
| `Scope.Dependents` | []path | direct dependents only (focused); deep adds acceptance commands |
| `CoverageReport` | covered []path, skipped []path, ceilingHit bool | mandatory in review output; `ceilingHit` ⇒ skipped MUST be non-empty or explained |

**State transitions**: `proposed → dispatched → completed(full)` or `→ completed(partial, ceilingHit)` or `→ skipped(rationale)`. A `skipped` decision emits the rationale but dispatches nothing. Trigger B (AutoReview nudge) consumes the same decision: tier `skip` suppresses the nudge; otherwise the nudge carries the tier.

## 3. SubagentBudget (extends run-count budget — D3)

| Field | Type | Rules |
|---|---|---|
| `Kind` | explore / plan / review / general | existing spec kinds (`subagent.go:95-100`) |
| `MaxTurns` | int | existing per-kind caps × `AgentTurnScale` (unchanged) |
| `TokenCeiling` | int | NEW: base-by-kind × tier scale (review) × complexity scale (RepoSizeBucket) |
| `ConsumedTokens` | int | running sum of provider-reported total tokens per subagent turn |

**Rules**: ceiling checked after each subagent turn from provider-reported usage (never estimates); on breach → one wrap-up turn instructing report-what-was-covered → return marked `partial`. Ceiling values are tuned from baseline data (D9) before enforcement defaults ship.

## 4. HandoffBrief / ReturnPackage (existing seam, formalized — D7)

**HandoffBrief** = existing `HandoffContract` (Role, Scope ≤ 1,200c, Context ≤ 1,500c scoped knowledge digest, Deliverable, OutputFormat) — **unchanged**; documented here as a preserved contract (F14).

**ReturnPackage** (formalizes the model-invoked return, aligning with the pipeline pattern):

| Field | Type | Rules |
|---|---|---|
| `Status` | ok / partial / failed | `partial` when ceiling or turn cap hit |
| `Digest` | string ≤ existing digest bound | what the main context receives |
| `BankedReportID` | knowledge key | full report (≤ 4,000c) banked via existing `AddPhaseReport` path |
| `ArtifactsChanged` | []path | general-kind only |
| `FollowUps` | []string | optional |
| `TruncationNote` | string | present iff anything was bounded |

**Rule**: reports at or under the digest bound pass through verbatim (no double-storage); the main history never receives content above the digest bound (Constitution V).

## 5. ModelPairing & UsageAttribution (extends usage.go — D4/D8)

| Field | Type | Rules |
|---|---|---|
| `MainModelID` / `SubModelID` | string | existing settings; sub falls back to main |
| `SessionPins` | map role→pin | `:main`, `:sub:<kind>` (NEW per-kind), `:aux` |
| `PerPairing[model,pin]` | {promptTokens, cacheRead, cacheMiss, cacheWrite, requests} | aggregated from provider-reported usage only; absent fields ⇒ `reported:false`, never fabricated |
| `SteadyStateHitRate[model,pin]` | float or "not reported" | excludes each pin's first (cold) request; surfaced in ContextReport rows + TaskStats |

**Validation**: attribution is additive bookkeeping on the existing `recordMainUsage`/`appendUsageLocked` path; it MUST NOT alter any request. SC-005 verdicts read exclusively from this structure.

## 6. BenchmarkRun (new — D9; full schema in contracts/benchmark-run.md)

| Field | Notes |
|---|---|
| `RunID`, `SuiteVersion`, `Timestamp` | reproducibility keys |
| `ConfigSnapshot` | models (main/sub), effort, gating mode, gateway build — held constant within a comparison (Constitution X) |
| `TaskRecords[]` | per task: category, outcome, turns, provider-reported usage, ReviewDecision (tier + rationale), per-pairing cache rates |
| `Aggregates` | completion rate, cost/completed task, trivial-review rate, high-risk review retention, violation counts |
| `VarianceBand` | from the mandatory double-run on unchanged builds |

## 7. AuditFinding / RoadmapItem (research.md register, carried into the audit deliverable)

**AuditFinding**: `ID (F#)`, `Severity H/M/L`, `Type cost/cache/quality/hygiene/measurement`, `Evidence (file:line or measurement)`, `Remediation (D# link)`. Already instantiated: F1–F15 in research.md §2.
**RoadmapItem**: `Decision (D#)`, `Findings addressed (F#…)`, `Success criteria moved (SC-…)`, `Phase` — the traceability spine required by FR-004/SC-007; tasks.md (Phase 2) derives directly from it.

## 8. TokenBudgetManifest (extends prompt_budget_test — D5)

| Field | Rules |
|---|---|
| `Sections[name] → bytes` | one row per static prompt section (from the instructions registry) |
| `PinnedTotal` | the budget test's asserted value; D5 changes must keep ΔTotal ≤ 0 |
| `Justification` | required for any future addition exceeding the pinned total (FR-012) |

**Producing artifact**: this manifest is not new code — the `Sections→bytes` table is emitted into the audit's instruction inventory (task T010) by dumping the existing instructions registry, and `PinnedTotal` is the value asserted by the existing `prompt_budget_test.go` (updated in T028). No standalone builder is created; §8 is a documentation view over data the registry already holds.
