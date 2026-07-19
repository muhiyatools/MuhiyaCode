# Phase 0 Research: Full-System Forensic Analysis & Decisions

**Feature**: `011-competitive-agent-audit` · **Date**: 2026-07-19
**Inputs**: two dedicated forensic traces of the current working tree (engine core + instruction set; subagent/review/policy system), the constitution (v1.0.0), the Reasonix reference study (`specs/002-reasonix-agent-overhaul/research.md`, Principle VII), and this session's gateway-side verification work (`STABILITY_AND_DISCOVERABILITY_PLAN.md`, `GATEWAY_AUDIT_DELTA.md` in the gateway repo).
**Evidence convention**: all `file:line` references are into this repo at the reviewed working tree; gateway references name the gateway repo explicitly.

---

## 1. Verified system anatomy (the baseline map)

### 1.1 Turn lifecycle (`internal/orchestrator/turnloop.go`, 783 lines)

`Engine.Run` (`turnloop.go:21`) is the single task entrypoint: guard/setup (`:26-46`) → classify & route (`:59-101`, `Classify` at `classify.go:135`, `NeedsPlan` at `classify.go:57`) → budget (`:102-110`, `BudgetFor` `classify.go:211`) → optional onboarding questions on the subagent model (`:112-130`) → persist + maintenance boundary (`:207-222`) → **task-brief assembly on the user-message tail** (`:223-311`, final append at `:310`) → stable prefix assembly (`:321-331`: `sessionDefinitions()` `definitions.go:15`, `SystemPrompt` `prompt.go:77`) → tool-call loop (`:341-770`) with per-turn `PrefixShape` compare (`:476-491`) around every `provider.Chat` (`:496`) → `finalize` (`turnhelpers.go:62`).

Turn-loop cost controls that already exist: turn governor + converge/final nudges (`:343-373`), compaction ladder soft 0.50 / reclaim 0.60 / force 0.90 (`:379-436`), all-failed-turns and H5 distinct-failure breakers (`:735-769`).

### 1.2 Instruction set (`internal/instructions`, 9 files — ALL model-facing text lives here)

`orchestrator/prompt.go` is only the composer; every literal is a registered `Text` (`instructions/types.go:65-128`) with audience/cache-position metadata, self-audited by `audit_test.go` (contradiction, stated-in-advance, capability-reference checks) and pinned by two goldens (`dump_test.go`) plus a **byte budget: 5,789 chars** (`prompt_budget_test.go:22`).

Static prefix sections (same bytes every request in a session): Identity (~230c, `instructions/prompt.go:38`), Operating contract (~640c, `:42-49`), Context & edit discipline (~560c, `:58-61`), Cache discipline (~720c, `:68-70`), Tools & recovery (~380c, `:77-80`), Delegation (~950c session-fixed template, `:136-141`), **Orchestration pipeline (~1,050c, `:148-155`)**, Communication (~290c), Safety (~230c), Environment (session-fixed, `:106-110`), Skills (session-fixed), Project memory (~1,030c, `:167-168`), per-family model addendum (`instructions/gateway.go:11-14`).

Dynamic content correctly rides the user-message tail (task class, budgets, plan/goal/pipeline blocks, governor nudges — `turnloop.go:223-311`); reasoning effort is a request parameter, never a message (`:460`). This discipline is enforced (see §1.4) and MUST be preserved.

**Tool catalog**: 21 non-MCP tools assembled in `sessionDefinitions` (`definitions.go:15-25`). Description sizes range from ~25c (`git_status`) to ~700c (`run_subagent`), with `update_plan` (~530c) and `save_memory` (~600c) also deliberately rich. **`run_shell`'s description is a bare one-liner with zero steering** (`instructions/tools.go:33`): "Run a shell command in the workspace with streaming output and cancellation."

### 1.3 Context/history management (`history.go`, 657 lines)

Append-only history with windowing (`BuildRequestWithMetadata` `history.go:177`), fold/trim reclamation (`Maintain` `:393`, error results pinned `:309`), accumulate-only compaction digests (`CompactTo` `:462`) on an isolated cold aux stream (`maintenance.go:76`). Duplicate-read protection **exists and works**: `InspectionLedger` (`inspection.go`) matches read/search signatures + line-range coverage (`coveringSegments:429`) with mtime/size staleness fingerprints (`:328`), enforced centrally in `gatedExecute` (`gates.go:139-150`); mutations supersede stale reads via `InvalidateFor` (`inspection.go:196`) → `history.MarkSuperseded` (`history.go:198`).

### 1.4 Caching / stable-prefix enforcement

`PrefixShape` (`prefixshape.go:19-28`) hashes system/tools/history/settled wire regions; `CompareShape` (`:87`) catches in-place rewrites; the turn loop **hard-fails the task** on an unexplained stable-prefix change (`turnloop.go:490`), with a loud degraded mode when the provider lacks a wire normalizer (`maintenance.go:228`). Cross-resume drift detection: `cacheresilience.go:83,174`. Byte-stability is covered by a dense test lattice (`prefixshape_test.go`, `prompt_stability_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go`, `restart_determinism_test.go`, `rtl_cache_test.go`).

### 1.5 Subagent system (`subagent.go`, `phaserunners.go`, `dynamicfanout.go`, `knowledge.go`)

Four kinds with fixed tool allowlists and turn caps (`subagent.go:95-100`): `explore` (read-only, 12), `plan` (read-only, 14), `review` (read-only, 14), `general` (full registry, 24). Dispatch: model-invoked `run_subagent` (`subagent.go:103-213`) or harness pipeline launches (`phaserunners.go:152-176`); parallel only when all calls are non-`general` subagents (`dispatch.go:82-87`).

**The handoff seam is a strength**: a subagent receives exactly two messages (`subagent.go:242-243`) — a small per-kind system prompt (role text + capability statement + `HandoffContract.Render()`: Role/Scope≤1,200c/Context≤1,500c scoped knowledge digest/Deliverable/OutputFormat) and the task string. **Never the main transcript.** Results bank into `Knowledge` (digest 500/full 4,000, `knowledge.go:57-72`) with epoch-keyed reuse (`Reusable`, `knowledge.go:74-85` — identical repeat = zero tokens); pipeline phases re-enter findings only as next-phase briefings (`phaserunners.go:29-33`), explicitly to avoid re-transmitting ~4,000 chars across billed turns (`phaserunners.go:104-106`).

Dynamic fan-out (prior feature work, confirmed **done**): greenfield-skip below 3 source files (`dynamicfanout.go:85-91`), lens-scaling 1/2/3 by repo size (`:64-77`), implementation-phase grouping with dependent-collapse at ≤6 steps (`phaserunners.go:325-327`) and disjoint-parallel detection (`:278-298`).

### 1.6 Review triggering — the root cause of the user's pain (verified)

There is **no risk/size gate at any review dispatch site**. Two independent triggers:

- **Trigger A (primary)**: `preparePipelinePhase` (`phaserunners.go:41-45`) — when a full-depth pipeline reaches `Validating`, `runPipelineValidation` (`:394-431`) dispatches a `review` subagent **unconditionally** (`:403-408`), plus a possible second re-scoped review on failure (`:409-419`). The only "gate" is upstream classification: `NeedsPlan` assigns `full` depth to `ClassLarge`/`ClassEpic` (`classify.go:79-82`), and `Classify` escalates to `Large` on **any one of**: text > 1,200 chars, one `breadthRE` word (`entire|all|every|rewrite|redesign|overhaul|migration|migrate|audit|from scratch|end-to-end`, `classify.go:117`), ≥8 bullets, or ≥4 file paths (`classify.go:178`). One word ("migrate", "audit", "all") → full pipeline → forced review.
- **Trigger B**: the AutoReview tail nudge (`turnloop.go:630-642`) — max effort only (`effort.go:59`), fires when ≥2 files changed, regardless of what changed (two one-line edits qualify).

Review subagent prompt: `SubagentReviewSystem` (`instructions/subagents.go:76-77`) + `PipelineValidateTaskTmpl` (`instructions/pipeline.go:100`); verdict contract `phaserunners.go:186-192`.

### 1.7 Budgets — the confirmed gap

Subagent budgets are **run-count only**: `taskAgentCap = min(effort.MaxAgentRuns, classAgents[class])` (`turnloop.go:136`, `classify.go:207,222`; chat/tiny/small 0, standard 2, large 5, epic 8; effort low/med/high/max → 1/2/4/8). Per-subagent **turn** caps exist (`subagent.go:291-299`, scaled by `AgentTurnScale` `effort.go`), but **no token or cost ceiling exists anywhere for a subagent** — a review/general subagent may consume up to ~14–24×scale full-context turns unmetered. "Complexity-scaled budget" from prior planning is confirmed **not implemented**.

### 1.8 Mixed-model handling (agent + gateway)

- Main model `ActiveModelID` (`turnloop.go:460`); subagents `SubagentModelID` with fallback (`subagent.go:217-219`); all four kinds share the one subagent model; onboarding and compaction run on subagent/active model respectively as isolated cold streams.
- Reasoning tiers are deliberately split: main `ReasoningForEffort` vs subagent `AgentReasoning` one tier lower (`effort.go:40-61`).
- Session cache pins separate main from subagents: `:main` / `:sub` / `:aux` (`turnloop.go:316`, `subagent.go:321`, `maintenance.go:76`), tested by `mixed_provider_test.go:71-77`. Gateway side (verified this session): `X-Muhiya-Session` → sticky model pinning + per-provider prefix-cache separation; per-model dialect mapping (`thinking.go`), dual cache-usage dialect parsing, deterministic byte-stable transforms pinned by tests.
- **Gap**: all subagent kinds share the single `:sub` pin while carrying different per-kind system prefixes (`subagent.go:321`) — sequential explore→plan→review churns one provider cache identity and cannot reuse prefixes. **Gap**: per-model/per-provider cache hit rates are aggregated (`usage.go:16,47,143`; `ContextReport` has per-model rows `contextreport.go:51`) but not surfaced as the per-pairing metric SC-005 needs.

### 1.9 Reference architecture (Constitution Principle VII)

The Reasonix study is already on file (`specs/002-reasonix-agent-overhaul/research.md`) and its conclusions are **implemented** in the current tree (append-only ladder, compose-once prefix, tail-riding volatility, isolated subagent sessions, one-result returns). Key takeaways this feature reuses: (a) subagent isolation with single-result return is the correct seam — preserve it; (b) DeepSeek-wire caching is purely byte-prefix stability — no cache_control exists there; (c) volatility rides the newest turn as strippable blocks. Deviation kept: MuhiyaCode's `X-Muhiya-Session` pin (multi-model gateway requirement, absent in Reasonix). New deviation this feature introduces: **per-kind subagent pins** (D4) — justified because Reasonix has no per-kind subagent prefix divergence to protect.

### 1.10 Competitor-class patterns (design reference, not imitation)

Patterns common to Claude-Code-class agents that this plan adopts where evidence supports them: review is **on-demand or risk-triggered**, never a fixed pipeline stage for routine work; explicit cheapest-tool guidance lives in the main prompt (dedicated read/search tools over shell); sub-task handoffs are structured briefs with structured returns; effort level scales both reasoning and verification depth; every background action is disclosed with a one-line rationale. The owner's exemplar system prompt (pending input, spec FR-003) will refine this section when provided; the instructions registry (`types.go`) makes a section-by-section diff mechanical.

---

## 2. Findings register (seed of the audit deliverable and roadmap)

Severity: **H**igh / **M**edium / **L**ow. Type: Cost / Cache / Quality / Hygiene. Each maps to spec FRs.

| ID | Sev | Type | Finding (evidence) | Remediation (decision) |
|---|---|---|---|---|
| F1 | H | Cost | Review subagent dispatched unconditionally at full-depth validation (`phaserunners.go:41-45,403-408`), plus possible second review (`:409-419`); no diff/risk check at dispatch | Gate at dispatch site via TaskProfile → ReviewDecision (D1) |
| F2 | H | Cost | Classifier escalates to Large on one breadth word / ≥4 paths / >1,200 chars (`classify.go:117,178`) → full pipeline → forced review; root cause of "review after almost every task" | Corroboration-based escalation (D2); gate remains authoritative backstop |
| F3 | M | Cost | AutoReview nudge gated only by max-effort + ≥2 files (`turnloop.go:630-642`, `effort.go:59`), not by risk/type | Same TaskProfile gate before nudging (D1) |
| F4 | H | Cost | No token/cost ceiling for any subagent — run-count and turn caps only (`turnloop.go:136`, `classify.go:207`, `subagent.go:291-299`); the planned complexity-scaled budget was never built | Per-kind token ceilings, complexity-scaled (D3) |
| F5 | M | Cache | All subagent kinds share one `:sub` session pin with divergent per-kind prefixes (`subagent.go:321`) → provider prefix-cache churn between kinds | Per-kind pins `:sub:<kind>` (D4) |
| F6 | M | Cost | No cheapest-tool steering in the main static prompt; `run_shell` description is an undirected one-liner (`instructions/tools.go:33`); steering exists only in read-only/gate texts (`subagents.go:14`, `gates.go:99`) | One budgeted static rule + shell-description sentence (D5) |
| F7 | M | Cost | Model-invoked subagent reports re-enter main history verbatim (≤2,200c, `subagent.go:208-212`); pipeline path already uses the better fixed-body + knowledge-bank pattern (`phaserunners.go:430`, `instructions/pipeline.go:70`) | Align model-invoked returns with digest + banking (D7) |
| F8 | M | Cost | Research lenses can re-read overlapping trees on large repos (`dynamicfanout.go:42-50`; an acknowledged ~915k-token incident in comments); disjointness only guaranteed for explicit per-directory scopes | Scope-partitioned lens briefs; measure in benchmark (D9) — bounded-scope follow-up, not a rewrite |
| F9 | L | Hygiene | Stale header comment: "19 tools / 6 synthetic" (`instructions/tools.go:4-6`) vs actual 21 / 8 (`definitions.go:29`) | Fix comment (roadmap hygiene batch) |
| F10 | L | Cache | Orchestration-pipeline static section (~1,050c) rides the prefix on every task even when no pipeline runs, partly duplicating the dynamic pipeline block (`instructions/prompt.go:148-155` vs `pipeline.go:330`) | Trim/condense to fund D5's addition; net prefix growth ≤ 0 (D5) |
| F11 | L | Quality | Plan⇄goal contradiction resolved by runtime backstop drop (`turnloop.go:267-277`) rather than by construction | Keep backstop; add setter-level invariant (hygiene batch) |
| F12 | L | Quality | Pre-dispatch arg validation covers only top-level primitives/enums (`validate.go:73,117`); nested schemas pass and fail at execution | Extend validation depth (hygiene batch) |
| F13 | M | Measurement | Per-model cache attribution exists internally (`usage.go:16-143`, `contextreport.go:51`) but per-pairing (model+provider+pin) hit rates are not surfaced — SC-005 unmeasurable today | Per-pin usage attribution surfaced in ContextReport/TaskStats (D8) |
| F14 | — | Strengths | Preserve: handoff seam (no transcript dumps), `InspectionLedger` dedupe, `PrefixShape` enforcement, knowledge `Reusable` zero-token repeats, greenfield-skip, lens-scaling, impl-phase grouping, prompt byte-budget test, `:main`/`:sub` pin separation | Constitution VIII: all changes are additive around these seams |
| F15 | L | Measurement | Onboarding aux usage lands in the task delta only via a capture-ordering subtlety (`turnloop.go:56,119-123`) | Document + assert with a test (hygiene batch) |

---

## 3. Decisions

### D1 — Review gating at the dispatch site (TaskProfile → ReviewDecision)

**Decision**: introduce a deterministic gating module (new `internal/orchestrator/reviewgate.go`) evaluated at BOTH dispatch sites (pipeline validation `phaserunners.go:41-45` and AutoReview nudge `turnloop.go:630`). Inputs form a `TaskProfile`: git-derived diff stats (files changed, lines added/deleted — already available via the workspace git tools), touched-area risk categories (path/pattern matching: auth, billing/payment, concurrency primitives, security config, migrations), task type (docs/comment/format/rename vs logic — from diff content classes), test outcome (if run this task), repo size bucket (reuse `dynamicfanout` probe), and explicit-request flag. Output is a `ReviewDecision`: tier `skip | focused | deep`, one-line user-visible rationale, and spend ceilings. Explicit user request always runs (spec FR-009); config surface `off | conservative | default`.
**Rationale**: the audit proves gating cannot live in the classifier alone (F1/F2) — the dispatch site is the only place all signals exist (the diff exists only after implementation). Deterministic rules keep the decision reproducible, testable, and token-free (an LLM-judge gate would itself cost tokens and violate determinism expectations).
**Alternatives considered**: (a) fix only the classifier — rejected: dispatch remains unconditional, trigger B untouched; (b) LLM-based gating — rejected as primary for cost/nondeterminism; (c) remove pipeline validation phase — rejected: it is correct for genuinely large work (Constitution I).

### D2 — Classifier corroboration

**Decision**: `Large` escalation requires **two** independent signals (e.g., breadth word + ≥4 paths, or length + bullets), not one; single-signal prompts stay `Standard` (light depth). The `breadthRE` word list loses bare "audit"/"all" as sole escalators.
**Rationale**: F2 evidence shows one word forces the full pipeline. With D1 as the authoritative gate, the classifier only needs to stop over-routing, not carry the whole gating burden.
**Alternatives**: leave classifier untouched and rely on D1 alone — rejected: full-depth pipelines carry other costs (research/planning phases) beyond the review.

### D3 — Complexity-scaled subagent token ceilings

**Decision**: add a per-dispatch token ceiling to `subagentInput` (enforced in the subagent loop from provider-reported usage per turn): ceiling = base-by-kind × tier scale (review focused/deep get distinct bases) × complexity scale (repo-size bucket). On ceiling hit: graceful wrap-up turn → report-what-was-covered (mirrors the review partial-coverage contract), never silent truncation.
**Rationale**: F4 — the single biggest unbounded cost. Provider-reported usage is already recorded per subagent turn (`usage.go`), so enforcement is measurement-based (Constitution VI), not estimated.
**Alternatives**: char-based output caps only — rejected: caps output, not consumption; global per-task token budget — rejected for now: coarser, penalizes legitimate large tasks (revisit after baselines).

### D4 — Per-kind subagent session pins

**Decision**: session pins become `e.session.ID + ":sub:" + spec.Name` (`subagent.go:321`); onboarding keeps `:sub:onboarding`-style separation.
**Rationale**: F5 — each kind has a distinct stable prefix; per-kind pins let the gateway's sticky sessions and the provider's prefix cache keep each kind warm independently. Zero wire-protocol change (the gateway hashes any opaque string; verified in the gateway's `stickysession.go`).
**Alternatives**: one shared pin (status quo) — rejected by evidence; per-dispatch unique pins — rejected: destroys within-kind reuse entirely.

### D5 — Cheapest-tool steering, prefix-budget-neutral

**Decision**: add one compact static rule to TOOLS AND RECOVERY ("for reading/searching, dedicated tools — read_file/grep/glob — are cheaper and cache-tracked; the shell is for executing, not reading") and one steering sentence to `run_shell`'s description; **fund it** by condensing the redundant static orchestration-pipeline section (F10) so the pinned prompt budget (`prompt_budget_test.go`) does not grow. All changes are static/byte-stable (Constitution III), with the budget test updated in the same change and the invalidation event recorded (one deliberate cache-reset per session on upgrade — the accepted, loud pattern already used by `SwitchModel`).
**Rationale**: F6 — the guidance simply does not exist where the main loop can see it; SC-006 requires it.
**Alternatives**: per-turn dynamic reminder — rejected: violates Constitution IV for static guidance; longer prose — rejected: budget discipline (F10 funds only a compact rule).

### D6 — Review scope + partial-coverage contract

**Decision**: the focused-tier review brief carries the diff file list + summary and instructs coverage of changed files and their direct dependents only; deep tier adds acceptance-command verification (current `PipelineValidateTaskTmpl` behavior). Every review reports the surface covered; ceiling-hit reviews report covered/skipped explicitly.
**Rationale**: spec FR-008; large-codebase cost bound requires scope, not just ceilings.
**Alternatives**: whole-repo review with ceilings alone — rejected: ceilings without scope produce arbitrary truncation.

### D7 — Structured return alignment for model-invoked subagents

**Decision**: model-invoked `run_subagent` returns adopt the pipeline pattern: full report banks into `Knowledge` (already implemented for pipeline launches), and the tool result carries a bounded digest + status + follow-ups; reports smaller than the digest bound pass through unchanged.
**Rationale**: F7 — Constitution V (no redundant retransmission); the superior pattern already exists in-tree (`phaserunners.go:104-106,430`) — this is convergence, not invention.
**Alternatives**: keep verbatim with a smaller cap — rejected: still retransmits what knowledge banking already stores.

### D8 — Per-pairing cache metrics (SC-005 enabler)

**Decision**: extend usage attribution (`usage.go`) to aggregate provider-reported cache reads/misses per (modelID, session pin), surface per-pairing steady-state hit rates in `ContextReport`/`TaskStats`, and include them in benchmark records. Agent-side only — provider-reported fields already flow through the gateway untouched.
**Rationale**: F13; Constitution VI (honest measurement) — SC-005 cannot be judged without this.
**Alternatives**: gateway-side aggregation — rejected: the agent already holds per-request usage with pin context; spec assumption keeps gateway work minimal.

### D9 — Benchmark suite & baseline-first ordering

**Decision**: build the FR-018 suite on the existing `benchmarks/` + `LIVE_GAUNTLET.md` conventions: a fixed task matrix (trivial docs/comment/rename/format → standard logic → risky auth/billing → large multi-file; × single-model and mixed MiniMax-main/DeepSeek-sub pairing), a runner that records the benchmark-run contract (provider-reported usage per task, review decisions with rationale, per-pairing cache rates), and a two-run variance check. **The first implementation task of this feature is baseline capture on the unchanged build** — every SC delta is measured against it (Constitution X).
**Rationale**: spec SC-001..SC-009 all reference baselines; the constitution forbids estimated or after-the-fact baselines.
**Alternatives**: third-party benchmark harnesses — rejected: not representative of this gateway/model catalog; spec operationalizes competitiveness internally.

### D10 — Exemplar-prompt comparison protocol

**Decision**: when the owner supplies the exemplar system prompt, diff it section-by-section against the instructions registry dump (`dump_test.go` goldens give the exact current bytes); classify each deviation keep/adopt/adapt with a one-line justification; adopted changes enter through the same budgeted, byte-stable path as D5. Until then, §1.10 patterns + the Reasonix reference stand in.
**Rationale**: spec FR-003 (input dependency, not blocker); the registry makes the comparison mechanical and auditable.
**Alternatives**: wholesale prompt replacement — rejected: Constitution VIII and the existing test lattice make incremental adoption strictly safer.

---

## 4. Resolved unknowns

All Technical Context unknowns are resolved by the traces above; no NEEDS CLARIFICATION remains. The single external input dependency (exemplar prompt) is handled by D10 without blocking any phase.
