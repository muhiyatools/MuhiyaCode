# Competitive Agent Audit — MuhiyaCode Agent (Feature 011)

**Date**: 2026-07-19
**Tasks discharged here**: T008 (subsystem audit), T010 (instruction-set comparison), T011 (traceability roadmap), T012 (US1 acceptance checklist). T009 (baseline fill-in) is **PENDING** — see §3.
**Inputs**: `specs/011-competitive-agent-audit/research.md` (forensic traces, findings F1–F15, decisions D1–D10), `spec.md` (FR-001..FR-018, SC-001..SC-009), `plan.md`, `contracts/review-gating.md`, `contracts/subagent-handoff.md`, `contracts/benchmark-run.md`, the Reasonix reference study (`specs/002-reasonix-agent-overhaul/research.md`), constitution v1.0.0, and direct code verification of key evidence points in this working tree.
**Baseline SHA**: `42b455003db55b369f53fe2cd2fdc5ba46ec7423` (`benchmarks/BASELINE_SHA.txt`; frozen reference binary `bin/muhiyacode-baseline-011.exe` built from this SHA + the 13 uncommitted files listed in that file; `go vet` clean, `go test ./... -count=1` = 12 packages ok at freeze).
**Evidence convention**: `file:line` references are into this repo at the reviewed working tree; gateway references name the gateway repo explicitly. Severity: **H**igh / **M**edium / **L**ow.

---

## 1. Verdict summary

| # | Subsystem (FR-001) | Verdict | Findings homed here |
|---|---|---|---|
| S1 | Orchestration loop | Defects found | F2 (H), F11 (L) |
| S2 | Instruction set (system prompts + per-tool guidance) | Defects found | F10 (L) |
| S3 | Tool catalog & cost characteristics | Defects found | F9 (L), F12 (L) |
| S4 | Automatic review policy | Defects found — root cause of owner's pain | F1 (H), F3 (M) |
| S5 | Subagent dispatch, handoff, result integration | Defects found | F4 (H), F7 (M), F8 (M) |
| S6 | Terminal-usage policy | Defect found | F6 (M) |
| S7 | Per-provider caching behavior | Defect found | F5 (M) |
| S8 | Mixed-model session handling | Defect found (measurement gap) | F13 (M) — FR-014 itself **satisfied** |
| S9 | Context/history management | **No defect found** | — (FR-011 satisfied; see §2.9) |
| S10 | Token accounting | Defect found (minor) | F15 (L) |

F14 is the cross-cutting **preservation register** (strengths that every remediation must leave intact); it is restated in §2.0. Coverage: 10/10 FR-001 subsystems, 15/15 findings mapped, satisfying SC-007's coverage clause.

---

## 2. Subsystem audit

### 2.0 Preservation register (F14 — strengths; Constitution VIII: all changes are additive around these seams)

Verified working and explicitly out of bounds for rewrites: the two-message subagent handoff seam (never a transcript dump, `subagent.go:242-243`); `InspectionLedger` duplicate-read dedupe (`inspection.go`, enforced in `gates.go:139-150`); `PrefixShape` stable-prefix enforcement with task hard-fail (`prefixshape.go:19-28`, `turnloop.go:490`); knowledge `Reusable` zero-token repeats (`knowledge.go:74-85`); greenfield-skip (`dynamicfanout.go:85-91`), lens-scaling (`:64-77`), impl-phase grouping (`phaserunners.go:325-327`); the pinned prompt byte budget (`prompt_budget_test.go:22`, const `promptBaselineChars = 5789` — verified in-tree); `:main`/`:sub`/`:aux` pin separation (`turnloop.go:316`, `subagent.go:321`, `maintenance.go:76`).

### 2.1 S1 — Orchestration loop

**Behavior**: `Engine.Run` (`internal/orchestrator/turnloop.go:21`, 783 lines) is the single task entrypoint: guard/setup (`:26-46`) → classify & route (`:59-101`; `Classify` `classify.go:135`, `NeedsPlan` `classify.go:57`) → budget (`:102-110`, `BudgetFor` `classify.go:211`) → optional onboarding on the subagent model (`:112-130`) → persist + maintenance boundary (`:207-222`) → task-brief assembly on the user-message tail (`:223-311`) → stable prefix assembly (`:321-331`) → tool-call loop (`:341-770`) with per-turn `PrefixShape` compare (`:476-491`) around every `provider.Chat` (`:496`) → `finalize` (`turnhelpers.go:62`). Cost controls already present: turn governor + converge/final nudges (`:343-373`), compaction ladder soft 0.50 / reclaim 0.60 / force 0.90 (`:379-436`), all-failed-turns and H5 distinct-failure breakers (`:735-769`).

**F2 — HIGH / Cost — hair-trigger classifier escalation.** `Classify` escalates to `Large` on **any one** of: text > 1,200 chars, a single `breadthRE` word (`entire|all|every|rewrite|redesign|overhaul|migration|migrate|audit|from scratch|end-to-end`, `classify.go:117`), ≥8 bullets, or ≥4 file paths (`classify.go:178`); `NeedsPlan` assigns `full` depth to `ClassLarge`/`ClassEpic` (`classify.go:79-82`); full depth forcibly reaches the Validating phase (S4). One word ("migrate", "audit", "all") → full pipeline → forced review. This is the upstream half of the "review after almost every task" pain. **Remediation**: D2 — `Large` requires two independent corroborating signals; bare "audit"/"all" removed as sole escalators (T020). D1's dispatch-site gate remains the authoritative backstop.

**F11 — LOW / Quality — plan⇄goal contradiction resolved by backstop, not construction.** The runtime drop at `turnloop.go:267-277` papers over a state that setters should never produce. **Remediation**: keep the backstop, add a setter-level exclusivity invariant + test (T043, hygiene batch).

### 2.2 S2 — Instruction set (system prompts + per-tool guidance)

**Behavior**: all model-facing text lives in `internal/instructions` (9 files); `orchestrator/prompt.go` is only the composer. Every literal is a registered `Text` (`instructions/types.go:65-128`) with audience/cache-position metadata, self-audited by `audit_test.go` (contradiction, stated-in-advance, capability-reference checks), pinned by two goldens (`dump_test.go`) and a byte budget of 5,789 chars (`prompt_budget_test.go:22`). Static prefix sections and sizes: Identity ~230c (`instructions/prompt.go:38`), Operating contract ~640c (`:42-49`), Context & edit discipline ~560c (`:58-61`), Cache discipline ~720c (`:68-70`), Tools & recovery ~380c (`:77-80`), Delegation ~950c session-fixed (`:136-141`), Orchestration pipeline ~1,050c (`:148-155`), Communication ~290c, Safety ~230c, Environment session-fixed (`:106-110`), Skills session-fixed, Project memory ~1,030c (`:167-168`), per-family model addendum (`instructions/gateway.go:11-14`). Dynamic content correctly rides the user-message tail (`turnloop.go:223-311`); reasoning effort is a request parameter, never a message (`:460`).

**F10 — LOW / Cache — dead static weight.** The ~1,050c Orchestration-pipeline section rides the prefix on every task even when no pipeline runs, partly duplicating the dynamic pipeline block (`instructions/prompt.go:148-155` vs `pipeline.go:330`). **Remediation**: condense it to fund D5's cheapest-tool rule at net prefix growth ≤ 0 (T026–T028).

**FR-012 status note**: the budget mechanism FR-012 requires already exists and is test-pinned (`prompt_budget_test.go`); the "written justification for additions" clause is discharged procedurally by T028's budget-neutral requirement.

**FR-013 — satisfied by existing implementation.** Per-turn dynamic content (task classification, budgets, plan/goal/pipeline blocks, governor nudges) rides per-turn positions on the user-message tail (`turnloop.go:223-311`, final append `:310`) and is never interleaved into stable instruction content or settled history; effort is a request parameter (`turnloop.go:460`). **Guarding tests**: the PrefixShape byte-stability lattice — `prefixshape_test.go`, `prompt_stability_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go`, `restart_determinism_test.go`, `rtl_cache_test.go` — plus the hard task-failure on unexplained stable-prefix change (`turnloop.go:490`). The T011 table reads FR-013 as verification-covered, not a gap.

### 2.3 S3 — Tool catalog & cost characteristics

**Behavior**: 21 non-MCP tools assembled in `sessionDefinitions` (`definitions.go:15-25`). Description sizes range ~25c (`git_status`) to ~700c (`run_subagent`); `update_plan` (~530c) and `save_memory` (~600c) are deliberately rich. Read-path tools are cheap and cache-tracked; tool outputs are bounded with explicit truncation markers ("entries truncated; narrow the path" `workspace/registry.go:66-67`, "matches truncated; narrow the query" `:133-134` — verified in-tree).

**F9 — LOW / Hygiene — stale header comment.** `instructions/tools.go:3-8` still claims "19 tools … 6 synthetic" vs the actual 21 / 8 (`definitions.go:29`). (Note: research.md cites `:4-6`; the comment actually spans `:3-8` — minor line drift, same defect.) **Remediation**: T042.

**F12 — LOW / Quality — shallow pre-dispatch validation.** `validateCallArgs` covers only top-level primitives/enums (`validate.go:73,117`); nested schemas pass validation and fail at execution, costing a wasted round trip. **Remediation**: extend to nested object/array item schemas (T044).

(The `run_shell` description defect is homed under S6/F6.)

### 2.4 S4 — Automatic review policy (root cause of the owner's pain — verified)

**Behavior**: there is **no risk/size gate at any review dispatch site**. Two independent triggers exist. **Trigger A (primary)**: when a full-depth pipeline reaches `Validating`, `preparePipelinePhase` (`phaserunners.go:41-45`) → `runPipelineValidation` (`:394-431`) dispatches a `review` subagent **unconditionally** (`:403-408`), plus a possible second re-scoped review on failure (`:409-419`). The only "gate" is the upstream classifier (S1/F2). **Trigger B**: the AutoReview tail nudge (`turnloop.go:630-642`), max effort only (`effort.go:59`), fires when ≥2 files changed regardless of what changed — two one-line edits qualify. Review prompt: `SubagentReviewSystem` (`instructions/subagents.go:76-77`) + `PipelineValidateTaskTmpl` (`instructions/pipeline.go:100`); verdict contract `phaserunners.go:186-192`.

**F1 — HIGH / Cost — unconditional dispatch at Trigger A** (`phaserunners.go:41-45,403-408`, second review `:409-419`); no diff, risk, or task-type check exists at the dispatch site. **Remediation**: D1 — deterministic `TaskProfile → ReviewDecision` gate (`reviewgate.go`, contract `contracts/review-gating.md` §2–§3: hard rules H1–H5 then the tier table, skip/focused/deep, config `off|conservative|default`, always-on one-line rationale) wired at both triggers (T013–T019, T023). Explicit user requests bypass gating entirely via the model-invoked `run_subagent("review")` path (contract §6a, FR-009).

**F3 — MEDIUM / Cost — Trigger B gated only by max-effort + ≥2 files** (`turnloop.go:630-642`, `effort.go:59`), not by risk or type. **Remediation**: same D1 gate consulted before nudging; the gate may only narrow Trigger B, never widen it (contract §6; T019).

(The absence of any review spend ceiling is homed under S5/F4; scope discipline is D6/T022.)

### 2.5 S5 — Subagent dispatch, handoff, result integration

**Behavior**: four kinds with fixed tool allowlists and turn caps (`subagent.go:95-100`): `explore` (read-only, 12), `plan` (read-only, 14), `review` (read-only, 14), `general` (full registry, 24). Dispatch via model-invoked `run_subagent` (`subagent.go:103-213`) or harness pipeline launches (`phaserunners.go:152-176`); parallel only when all calls are non-`general` (`dispatch.go:82-87`). **The handoff seam is a strength (F14)**: a subagent receives exactly two messages (`subagent.go:242-243`) — small per-kind system prompt (role + capability + `HandoffContract.Render()`: Role / Scope ≤1,200c / Context ≤1,500c scoped knowledge digest / Deliverable / OutputFormat) and the task string. Never the main transcript. Results bank into `Knowledge` (digest 500 / full 4,000, `knowledge.go:57-72`) with epoch-keyed `Reusable` zero-token repeats (`knowledge.go:74-85`); pipeline phases re-enter findings only as next-phase briefings (`phaserunners.go:29-33,104-106`). Dynamic fan-out from prior feature work is confirmed done (greenfield-skip, lens-scaling, impl-phase grouping — §2.0).

**F4 — HIGH / Cost — no token/cost ceiling anywhere for a subagent.** Budgets are run-count only (`taskAgentCap = min(effort.MaxAgentRuns, classAgents[class])`, `turnloop.go:136`, `classify.go:207,222`) plus per-subagent turn caps (`subagent.go:291-299`); a review/general subagent may consume up to ~14–24×scale full-context turns unmetered. The previously planned complexity-scaled budget was never built. **Remediation**: D3 — per-dispatch `TokenCeiling` enforced from provider-reported per-turn usage, graceful wrap-up + partial-coverage report on breach (T021, T024); ceilings derived from baseline p75 per `task_class` (contract §4 — 0/uncapped until baselines exist, per Constitution VI).

**F7 — MEDIUM / Cost — model-invoked returns re-enter main history verbatim** (≤2,200c, `subagent.go:208-212`) while the pipeline path already uses the superior fixed-body + knowledge-bank pattern (`phaserunners.go:430`, `instructions/pipeline.go:70`). **Remediation**: D7 — align model-invoked returns: bank full report to Knowledge, return status + bounded digest + follow-ups; ≤-bound reports pass through (T034, T035). This is convergence on an in-tree pattern, not invention.

**F8 — MEDIUM / Cost — research lenses can re-read overlapping trees on large repos** (`dynamicfanout.go:42-50`; an acknowledged ~915k-token incident in comments); disjointness is guaranteed only for explicit per-directory scopes. **Remediation**: scope-partitioned lens briefs, with overlap measured in the benchmark (D9). ⚠️ **Traceability caveat**: tasks.md contains **no dedicated implementation task** for lens scope-partitioning — only D9's measurement path covers F8 today. Recorded in §5 as a follow-up gap.

**FR-016 status**: the handoff direction (structured bounded brief, no transcript) is **satisfied** by the existing seam above; the return direction is satisfied on the pipeline path and completed for model-invoked dispatches by D7/T034. FR-017 (explicit failure surfacing) is verified/extended by T036, with tests beside T035.

### 2.6 S6 — Terminal-usage policy

**Behavior**: cheapest-tool steering exists **only** in subagent read-only texts and gate messages (`subagents.go:14`, `gates.go:99`) — i.e., in the places the main loop never sees. The main static prompt contains no rule preferring dedicated read/search tools over the shell, and `run_shell`'s description is a bare undirected one-liner (`instructions/tools.go:33`, verified: "Run a shell command in the workspace with streaming output and cancellation.").

**F6 — MEDIUM / Cost.** No steering where the main model reads it; SC-006 (<2% cheapest-tool violations) is unreachable without it. **Remediation**: D5 — one compact static rule in TOOLS AND RECOVERY + one steering sentence on `run_shell`, funded by the F10 condensation so the pinned 5,789-char budget does not grow; budget test + goldens updated in the same change with the one deliberate session-upgrade invalidation event (T026–T029). Enforcement of duplicate-read prevention is already mechanical (S9); terminal-read-when-tool-exists violations become measurable via T005's transcript analysis.

### 2.7 S7 — Per-provider caching behavior

**Behavior**: `PrefixShape` (`prefixshape.go:19-28`) hashes system/tools/history/settled wire regions; `CompareShape` (`:87`) catches in-place rewrites; the turn loop hard-fails the task on an unexplained stable-prefix change (`turnloop.go:490`), with a loud degraded mode when the provider lacks a wire normalizer (`maintenance.go:228`). Cross-resume drift detection: `cacheresilience.go:83,174`. Byte-stability is covered by a dense test lattice (`prefixshape_test.go`, `prompt_stability_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go`, `restart_determinism_test.go`, `rtl_cache_test.go`). Session cache pins separate main from subagents: `:main` / `:sub` / `:aux` (`turnloop.go:316`, `subagent.go:321`, `maintenance.go:76`), tested by `mixed_provider_test.go:71-77`. Gateway side (verified this session): `X-Muhiya-Session` → sticky model pinning + per-provider prefix-cache separation, deterministic byte-stable transforms pinned by tests.

**F5 — MEDIUM / Cache — all subagent kinds share the single `:sub` pin** while carrying divergent per-kind system prefixes (`subagent.go:321`, verified: `SessionID: e.session.ID + ":sub"`). Sequential explore→plan→review churns one provider cache identity; within-kind prefix reuse is impossible across kinds. **Remediation**: D4 — per-kind pins `:sub:<kind>` (+ `:sub:onboarding`), zero wire-protocol change (the gateway hashes any opaque string, verified in its `stickysession.go`); T031 with pin tests in `mixed_provider_test.go`. This is the one deliberate deviation from the Reasonix reference introduced by this feature (Reasonix has no per-kind subagent prefix divergence to protect — research §1.9).

### 2.8 S8 — Mixed-model session handling

**Behavior**: main model `ActiveModelID` (`turnloop.go:460`); subagents `SubagentModelID` with fallback (`subagent.go:217-219`); all four kinds share the one subagent model; onboarding and compaction run as isolated cold streams on the subagent/active model. Reasoning tiers deliberately split: main `ReasoningForEffort` vs subagent `AgentReasoning` one tier lower (`effort.go:40-61`). Gateway: per-model dialect mapping (`thinking.go`, gateway repo), dual cache-usage dialect parsing, sticky per-provider separation.

**FR-014 — satisfied by existing implementation.** Each request is formed in the receiving model's dialect: per-family model addendum in the instruction set (`instructions/gateway.go:11-14`), reasoning-tier split (`effort.go:40-61`), gateway per-model dialect mapping (`thinking.go`) with deterministic byte-stable transforms. No leakage of one provider's dialect into another's requests was found. **Guarding tests**: `mixed_provider_test.go` (agent side, incl. pin separation `:71-77`) and the gateway's dialect/transform test suite. The T011 table reads FR-014 as verification-covered, not a gap.

**F13 — MEDIUM / Measurement — SC-005 unmeasurable today.** Per-model cache attribution exists internally (`usage.go:16,47,143`; `ContextReport` has per-model rows `contextreport.go:51`) but per-pairing (model + provider + pin) steady-state hit rates are not surfaced anywhere. **Remediation**: D8 — per-(model, pin) aggregation of provider-reported cache read/miss/write in `usage.go`, `reported:false` when the provider omits fields, surfaced in `ContextReport`/`TaskStats` and benchmark records (T032, T033). Agent-side only; additive bookkeeping, no request changes.

### 2.9 S9 — Context/history management — **NO DEFECT FOUND**

**Verdict**: no defect found. **Evidence examined**: append-only history with windowing (`BuildRequestWithMetadata` `history.go:177`, 657 lines total), fold/trim reclamation (`Maintain` `history.go:393`, error results pinned `:309`), accumulate-only compaction digests (`CompactTo` `:462`) on an isolated cold aux stream (`maintenance.go:76`); mutation-driven staleness supersedes old reads (`InvalidateFor` `inspection.go:196` → `history.MarkSuperseded` `history.go:198`); compaction ladder scheduling in the turn loop (`turnloop.go:379-436`). The Reasonix-derived ladder semantics from feature 002 are implemented and test-covered; nothing in the forensic traces surfaced a cost, cache, or quality defect in this subsystem.

**FR-011 — satisfied by existing implementation.** (a) Duplicate reads of unchanged content are blocked/redirected by `InspectionLedger` (`inspection.go`): read/search signature matching + line-range coverage (`coveringSegments` `inspection.go:429`) with mtime/size staleness fingerprints (`:328`), enforced centrally for every gated tool in `gatedExecute` (`gates.go:139-150`). (b) Oversized tool outputs are bounded with explicit truncation markers and narrow-the-query guidance (`workspace/registry.go:66-67,133-134` — verified in-tree). (c) The remaining clause — unchanged content not retransmitted when avoidable — is already satisfied on the pipeline subagent path (knowledge banking, `phaserunners.go:104-106`) and is completed for model-invoked subagent returns by D7/T034 (F7). **Guarding tests**: `inspection_test.go` (in-tree, verified) plus the history/maintenance suites covered by the §2.7 lattice. The T011 table therefore maps FR-011 to D7/T034 **only** for clause (c); clauses (a)/(b) are verification-covered, not gaps.

### 2.10 S10 — Token accounting

**Behavior**: provider-reported usage is recorded per request including per subagent turn (`usage.go`), aggregated per model (`usage.go:16,47,143`) and surfaced in `ContextReport` (`contextreport.go:51`) and `TaskStats`. All measurement in this feature is provider-reported by constitutional rule (VI) — never estimated.

**F15 — LOW / Measurement — fragile onboarding attribution.** Onboarding aux usage lands inside the task delta only via a capture-ordering subtlety (`turnloop.go:56,119-123`); nothing asserts it. **Remediation**: document + pin with a test (T045, hygiene batch).

(Cross-ref: the per-pairing attribution gap is F13, homed in S8. T004's machine-readable per-task summary — `{task_id, task_class, completed, turns, usage, cost_usd, review:{tier,rationale,spend_tokens,ceiling_hit}}` — is the authoritative accounting source for the benchmark suite; the runner never scrapes rendered TUI output.)

---

## 3. Baseline measurements — **PENDING T006/T007 live runs**

**Status: PENDING.** Constitution X forbids after-the-fact or estimated baselines; no number below may be filled from anything but the committed run records in `benchmarks/runs/baseline/` produced by T006 on the frozen SHA/binary (double-run × {single-model, mixed MiniMax-main/DeepSeek-sub} = 4 runs) and aggregated by T007 into `benchmarks/BASELINES.md`. T009 fills this section from that file.

| Metric (FR-002 / T007) | Config | Value |
|---|---|---|
| Trivial-task auto-review rate | single-model / mixed | pending |
| Per-task token spend by task_class (provider-reported) | both | pending |
| Median small-task total tokens (`task_class ∈ {tiny, small}`) | both | pending |
| Review overhead, median % of task spend — small | both | pending |
| Review overhead, median % of task spend — medium | both | pending |
| Per-provider cache hit rates (or `not reported`) | per pairing | pending |
| Cheapest-tool violation counts (terminal-read / duplicate-read) | both | pending |
| Review-subagent spend distribution per task_class, incl. **p75** (feeds contract §4 ceilings via T021) | both | pending |
| Double-run variance band (feeds all noise-vs-win verdicts) | both | pending |

Expected qualitative shape from the code audit (to be confirmed, never substituted): trivial auto-review rate near 100% on Trigger-A-routed tasks (F1/F2); review overhead materially above the 20% SC-004 target on small tasks; duplicate-read violations near zero (InspectionLedger); terminal-read violations nonzero (F6).

---

## 4. Instruction-set comparison (T010, FR-003)

Sources: current sections from the instructions registry (§2.2 sizes; exact bytes pinned by `dump_test.go` goldens), the Reasonix reference (`specs/002-reasonix-agent-overhaul/research.md` §§1–3, 5), and competitor-class patterns (research.md §1.10: review on-demand/risk-triggered, cheapest-tool guidance in the main prompt, structured briefs/returns, effort scaling reasoning + verification depth, one-line disclosure of background actions).

| Current section (size) | Reasonix reference | Competitor-class pattern | Class | Justification |
|---|---|---|---|---|
| Identity (~230c) | ~660B const base prompt incl. frugality | standard | **keep** | Compact, byte-stable, audited |
| Operating contract (~640c) | UserDecisionPolicy const | standard | **keep** | Session-stable policy, no defect |
| Context & edit discipline (~560c) | edit-before-read enforced host-side | same | **keep** | Backed by mechanical gates (`gates.go`), not prose alone |
| Cache discipline (~720c) | was Reasonix trick #adoption in 002 | rare (differentiator) | **keep** | Feature-002 adoption already landed; test-pinned |
| Tools & recovery (~380c) | frugality text in base prompt | **cheapest-tool rule in main prompt** | **adopt** | F6: rule absent here — D5 adds it, funded by F10 (T027) |
| Delegation (~950c, session-fixed) | subagents fully isolated, one-result return | structured briefs | **keep** | Seam already matches both references (F14) |
| Orchestration pipeline (~1,050c) | Reasonix carries no pipeline prose in prefix | dynamic per-phase direction | **adapt** | F10: duplicates dynamic block `pipeline.go:330` — condense (T026) |
| Communication (~290c) | output style const | one-line action disclosure | **keep** | Rationale line rides the tail via T023, not the prefix (Constitution IV) |
| Safety (~230c) | — | standard | **keep** | No defect |
| Environment (session-fixed) | persisted probe snapshot, 24h TTL, flap-merge | standard | **keep** | Equivalent mechanism already verified (002 §4) |
| Skills (session-fixed) | index ≤4,000c, bodies never in prefix | standard | **keep** | Same pattern |
| Project memory (~1,030c) | `# Memory` persisted files + staleness text | MUHIYA.md-class memory | **keep** | Feature-006 design; byte-stable |
| Model addendum (`gateway.go:11-14`) | N/A (single provider) | rare | **keep** (deviation) | Multi-model gateway requires per-family dialect hints; FR-014 evidence |
| `run_shell` description (one-liner, `tools.go:33`) | per-tool geometry travels with the tool | steering in tool descriptions | **adopt** | F6: zero steering — D5 adds one sentence (T027) |
| Per-task tail brief (~38 tokens) | zero-wrapper steady state | varies | **keep** (recorded deviation) | Drives effort/budget levers; bounded, tail-positioned (002 §5) |
| Review dispatch behavior (S4) | — | **review on-demand / risk-triggered, never a fixed stage** | **adopt** | F1/F2/F3 → D1/D2; the largest divergence from competitor class |

Net instruction-set verdict: the architecture (registry + audits + goldens + byte budget) is competitor-grade; the two material content gaps are cheapest-tool steering (adopt, D5) and the review-policy divergence (adopt, D1/D2). All adoptions enter through the budgeted, byte-stable path with net prefix growth ≤ 0.

**D10 exemplar-prompt slot: RESOLVED (2026-07-19): the owner confirmed NO exemplar system prompt exists. Per D10's fallback, the Reasonix reference + competitor-class pattern comparison in this section IS the final FR-003 comparison; the exemplar clause is closed as not-applicable.** Protocol on arrival (D10): diff section-by-section against the registry dump goldens (`dump_test.go` gives exact current bytes); classify each deviation keep/adopt/adapt with a one-line justification; adopted changes enter through the same budgeted path as D5. Until then this section's competitor-class + Reasonix comparison stands in, as FR-003 and the spec's input-dependency assumption allow.

---

## 5. Traceability roadmap (T011, FR-004, SC-007)

Every roadmap item traces back to finding(s) and forward to success criteria and tasks; task IDs cross-checked against tasks.md (T001–T047, all accounted for).

| Roadmap item | Findings addressed | FRs | SC moved | Tasks | Phase |
|---|---|---|---|---|---|
| Benchmark harness + frozen baselines (D9) | enables all; F8 (overlap measured) | FR-002, FR-018 | SC-008 (+ enables SC-001..006 verdicts) | T001–T007 | 1–2 |
| Audit deliverable + roadmap (this document) | F1–F15 recorded | FR-001, FR-004 | SC-007 | T008, T009*, T010, T011, T012 | 3 |
| Review gating at dispatch sites (D1) | F1, F3 (F2 corroborating) | FR-005, FR-006, FR-007, FR-009 | SC-001, SC-002, SC-009 | T013–T019, T023, T025 | 4 |
| Classifier corroboration (D2) | F2 | FR-005 | SC-001 | T020 | 4 |
| Complexity-scaled subagent token ceilings (D3) | F4 | FR-008 | SC-004 | T021, T024 | 4 |
| Review scope + partial-coverage contract (D6) | F1 (scope half), F4 (bound half) | FR-008 | SC-004 | T022 | 4 |
| Cheapest-tool steering, budget-neutral (D5) | F6, F10 | FR-010, FR-012 | SC-003, SC-006 | T026–T030 | 5 |
| Per-kind subagent session pins (D4) | F5 | FR-015 | SC-005 | T031, T037 | 6 |
| Per-pairing cache metrics (D8) | F13 | FR-015 | SC-005 | T032, T033, T037 | 6 |
| Structured returns for model-invoked subagents (D7) | F7 | **FR-011 (clause c) AND FR-016 (return half)** — both map here, deliberately not double-counted (analysis D1 note in tasks.md T011) | SC-003 | T034, T035 | 6 |
| Subagent failure surfacing — verify/extend (no new decision) | — | FR-017 | SC-007 coverage | T036 | 6 |
| Exemplar comparison protocol (D10) | — | FR-003 | SC-007 | T010 (pending slot) | 3 |
| Competitive-parity checklist + final verdicts (D9 close-out) | — | FR-018 | SC-008 | T038, T039, T040, T041 | 7 |
| Hygiene: stale tool-count comment | F9 | — | — | T042 | 8 |
| Hygiene: plan⇄goal setter invariant | F11 | — | — | T043 | 8 |
| Hygiene: nested arg validation | F12 | — | — | T044 | 8 |
| Hygiene: onboarding usage-ordering test | F15 | FR-002 accuracy | — | T045 | 8 |
| Docs sync + final gate | — | constitutional | all (verification) | T046, T047 | 8 |

\* T009 pending T007.

**Verification-covered rows (not gaps)**: FR-011 clauses (a)/(b) — InspectionLedger + registry output caps (§2.9); FR-013 — tail placement + PrefixShape lattice (§2.2); FR-014 — dialect mapping + `mixed_provider_test.go` + gateway transform tests (§2.8). Only FR-011's retransmission clause carries new work (D7/T034).

**Known traceability gap (carried forward)**: F8's remediation ("scope-partitioned lens briefs") has **no dedicated implementation task** in tasks.md — only its measurement path (D9 benchmark overlap counting) is tasked. If baseline runs confirm material lens-overlap cost, a task must be added before this feature closes; recorded here so SC-007's "every finding → remediation" clause is honest about the follow-up status.

---

## 6. US1 acceptance checklist (T012)

Spec US1 acceptance scenarios, verified against this document:

| # | Scenario (spec.md US1) | Verdict | Evidence |
|---|---|---|---|
| 1 | Baseline measurements exist for per-task token spend by category, review-overhead share, per-provider cache hit rates, trivial-task auto-review rate — all provider-reported, from real sessions | **PENDING** — awaits T006 live runs on the frozen baseline binary + T007 aggregation; §3 carries the committed table skeleton and the provider-reported-only rule. No estimates are presented as measurements (Constitution VI/X). | §3; `benchmarks/BASELINE_SHA.txt` |
| 2 | Every enumerated subsystem has ≥1 finding with evidence/severity/remediation, or an explicit "no defect found" verdict with evidence examined | **PASS** — 10/10 FR-001 subsystems in §2; nine carry findings with `file:line` evidence, severity, and a decision-linked remediation; S9 (context/history) carries the explicit no-defect verdict with the evidence examined enumerated. | §1, §2.1–§2.10 |
| 3 | Every roadmap item references the finding(s) it fixes and the success criterion it moves | **PASS** — §5 table: every behavior row lists F#, FR#, SC#, T#, phase; verification-covered requirements and the one known F8 tasking gap are explicitly called out rather than silently absorbed. | §5 |
| 4 | Instruction set compared section-by-section against the owner's exemplar prompt; each deviation classified keep/adopt/adapt | **PASS** — the owner confirmed (2026-07-19) that no exemplar system prompt exists, so per the spec's input-dependency assumption and D10's fallback, the section-by-section comparison against the two available references (Reasonix study + competitor-class patterns) — every row classified keep/adopt/adapt with justification — IS the final, complete FR-003 comparison. The exemplar clause is closed as not-applicable. | §4 |

**US1 standing**: shippable as the MVP deliverable per tasks.md Phase 3 checkpoint, with two clearly-labeled pending inputs — live baselines (T006/T007 → T009) and the owner's exemplar prompt (D10 → §4 slot).
