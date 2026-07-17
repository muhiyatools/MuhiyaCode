# Tasks: Enforced Orchestration Pipeline & MiniMax Provider Integration

**Input**: Design documents from `specs/009-orchestration-minimax-overhaul/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md),
[minimax-baseline.md](minimax-baseline.md)

**Tests**: Included — the contracts explicitly demand unit/scripted-provider tests, and the
constitution (Principles VI/X) makes measurement + prefix-stability tasks MANDATORY for
this cache-affecting feature.

**Organization**: Grouped by user story (US1–US5 from spec.md), foundational work first.
Two repos: client paths are relative to `F:\MuhiyaCode Agent Go`; gateway tasks use
absolute `F:\MuhiyaWorkspace\MuhiyaWorkspace\...` paths.

## Executor Notes (GPT 5.6 Sol — read before starting)

1. **NO LIVE MINIMAX CALLS — ever.** The user will not fund the MiniMax API. All MiniMax
   verification runs against the recorded/simulated fixture (T041). Label every MiniMax
   figure **simulated** in code comments, test names, and results.md (R8; Principle VI).
2. **DeepSeek is the only live provider.** Live benchmark legs (T055–T058) respect the
   account's $-budget burst window; if a leg 429s on budget, record "budget-blocked" in
   results.md and stop — never fabricate numbers.
3. **One prompt epoch.** All static prompt/tool-description changes in this feature
   (T017 + T029) land as ONE recorded feature-009 epoch; re-baseline
   `internal/orchestrator/prompt_budget_test.go` once, with a justification comment.
4. **Do not change**: effort→allowance numbers, `classAgents` caps
   (standard 2 / large 5 / epic 8), denial texts, the four subagent kinds, the
   client↔gateway wire protocol, `~/.muhiya` layout (one additive sidecar field only),
   DeepSeek's strip-on-replay. The token ceiling stays removed — do not reintroduce any.
5. **Improve, don't rewrite** (Principle VIII): every task extends the named existing
   function/file; the only NEW client file is `internal/orchestrator/pipeline.go` (plus
   test files). Evidence line numbers below come from research.md's appendix — re-grep
   if a line has drifted.
6. Contract IDs referenced per task: PL-x = [contracts/pipeline-state.md](contracts/pipeline-state.md),
   HO-x = [contracts/handoff-contract.md](contracts/handoff-contract.md),
   MX-x = [contracts/minimax-provider.md](contracts/minimax-provider.md),
   DM-x = [contracts/default-models.md](contracts/default-models.md).
7. After each task or logical group: `go build ./... ; go vet ./... ; go test ./... -count=1`
   in the touched repo, then commit.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 (user-story phases only)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Pin the green starting state and capture the BEFORE baselines that
Principle X and FR-018 require. **T002 MUST complete before any gateway code change.**

- [X] T001 Verify green baseline in both repos (`go build ./... ; go vet ./... ; go test ./... -count=1` in `F:\MuhiyaCode Agent Go` and `F:\MuhiyaWorkspace\MuhiyaWorkspace`) and create the results scaffold `specs/009-orchestration-minimax-overhaul/results.md` containing the pinned run-conditions table (model IDs, gateway build, effort, dates; live vs SIMULATED columns) per SC-008
- [X] T002 [P] Capture the DeepSeek-only BEFORE conformance snapshot (the existing wire-shape capture procedure) from the CURRENT gateway build into `specs/009-orchestration-minimax-overhaul/before/deepseek-conformance/` — the FR-018/SC-007 byte-identical comparison baseline; must precede T035+
- [X] T003 Record the delegation-workload BEFORE baseline in `specs/009-orchestration-minimax-overhaul/results.md`: reuse feature 008's `benchmarks/delegationbench` before-leg (0 subagents observed) as the pinned baseline; if the account budget window permits, re-run on the current build and store under `specs/009-orchestration-minimax-overhaul/before/` (Principle X; depends on T001)

**Checkpoint**: Baselines pinned — implementation may begin.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared type/state layer every client story builds on.

**⚠️ CRITICAL**: Blocks US1/US2/US3 and US5's client work. US4's gateway tasks
(T035–T043) do NOT depend on this phase — they may run in parallel once Setup is done.

- [X] T004 Add `PipelinePhase` string constants (`direct`, `research`, `plan`, `approve`, `implement`, `validate`, `done`) and an additive `PipelinePhase string` (json `pipeline_phase,omitempty`) field on `PlanStateSnapshot` in `internal/contract/types.go` (~line 328) — absent value = legacy/direct, backward-compatible sidecar (data-model §1 Persistence)
- [X] T005 [P] Add `PlanNeedVerdict{NeedsPlan bool; Depth string; Reason string}` and the pure predicate `NeedsPlan(assessment)` in `internal/orchestrator/classify.go`: true for Class ∈ {standard, large, epic} that is not a pure no-workspace question; Depth = `light` (standard) / `full` (large|epic); Reason reuses `Assessment.Reason`; `classAgents` untouched (R4; PL-1/PL-3/PL-4; data-model §2)
- [X] T006 Create `internal/orchestrator/pipeline.go` — the phase state machine: legal transitions exactly per data-model §1 table, gate predicates (research done-or-degraded, plan written, approved, steps complete, validated), degradation recording with reason strings, and the `contract.PlanPhase` transition each pipeline phase drives; no engine wiring yet (R1; depends on T004, T005)
- [X] T007 Add table-driven unit tests in `internal/orchestrator/pipeline_test.go`: every legal and illegal transition, each degradation path records its reason, `PipelinePhase` persistence round-trip through `PlanStateSnapshot` (depends on T006)

**Checkpoint**: State machine unit-green — client user stories can begin.

---

## Phase 3: User Story 1 — Complex work runs a real pipeline, not a monologue (Priority: P1) 🎯 MVP

**Goal**: Harness-enforced research → plan → approve (always pauses, even in
auto-accept) → implement → validate flow for every task that needs a plan; simple
tasks stay byte-identical direct.

**Independent Test**: Scripted-provider tests observe every gate; live check =
delegation benchmark shows research runs before any file-changing call, a plan on
disk before the first edit, and the control task produces zero orchestration (SC-001/002,
verified live in Phase 8).

### Implementation for User Story 1

- [X] T008 [US1] Wire the entry gate in the engine run loop in `internal/orchestrator/engine.go`: when `NeedsPlan` is true, enter pipeline `research` (auto plan-mode read-only via the existing `SetPlanMode` path, goal.go:172) with `WithAgentFloor` ≥1 (classify.go:168, applied like engine.go:727); record the verdict + Reason and surface them in the session brief/notice (FR-001; PL-1; R2 entry gate; depends on T006)
- [X] T009 [US1] Add the implementation gate inside `gatedExecute` in `internal/orchestrator/engine.go` (~1637): for a pipeline task, block main-loop mutating tool calls until pipeline phase == `implement`, reusing the plan-mode mutation-block text machinery (~1666-1683) and bounded escalation (~1523) with pipeline-specific guidance ("finish research/plan; await approval"); NO token ceiling (PL-6; R2; depends on T008)
- [X] T010 [US1] Implement the research phase in `internal/orchestrator/engine.go` + `pipeline.go`: fan out `explore`/`plan`/`review` subagents per independent scope (counts from unchanged `classAgents`), bank reports via `AddReport` (knowledge.go:47); advance research→plan only when ≥1 research subagent completed OR degradation recorded (PL-5; PL-13; depends on T009)
- [X] T011 [US1] Wire plan-phase completion in `internal/orchestrator/engine.go`: `update_plan` (~1817) + `WritePlan` → `plan.md` via `planMarkdown` (~2673); `exit_plan_mode` (~1793) moves pipeline plan→approve through the existing planExited flow (~1217-1276) (PL-14; depends on T010)
- [X] T012 [US1] Bind the approve phase to the plan-ready flow gate in `internal/orchestrator/engine.go` + `internal/tui/actions.go` (openPlanReadyModal, ~888): approval → `implement` (proceed flow engine.go:830-849), steer → back to research/plan, cancel → plan persists `Pending` + clean task end; flow gates already ignore PermissionMode so auto-accept still pauses — assert, don't branch (PL-9/PL-10/PL-12; R3; depends on T011)
- [X] T013 [US1] Verify/adjust headless `-p` behavior in `internal/orchestrator/engine.go` + `internal/command/root.go` (console callbacks ~565-618): a pipeline task writes the plan, saves it `Pending`, prints the proceed instruction, and NEVER auto-implements (the no-`TaskComplete` one-shot signal) (PL-11; R3; depends on T012)
- [X] T014 [US1] Implement the implement phase in `internal/orchestrator/pipeline.go` + `engine.go`: on approval drive `PlanPhaseExecuting`; `full` depth (large/epic) launches implementation subagents (`general` kind) for plan parts, `light` depth (standard) allows main-loop implementation; per-phase counts from unchanged `classAgents` (PL-15; FR-003; depends on T012)
- [X] T015 [US1] Implement the validate phase + anti-premature-finish gate in `internal/orchestrator/plan.go` (stampPlanCompletionPhase, ~107) + `pipeline.go` + `engine.go`: a review pass over changed files + the plan's checks must complete before `done`; `done` unreachable while `hasIncompletePlan()` (engine.go:1935) is true or validation unconfirmed; premature finalize stamps `Interrupted` (resumable), never `Finished` (PL-7; PL-16; R5; depends on T014)
- [X] T016 [US1] Implement degradation paths in `internal/orchestrator/pipeline.go` + `engine.go`: empty/failed research, exhausted allowance, or a failed phase falls back to bounded direct work with the reason recorded in the session — the pipeline can never deadlock on an empty phase (PL-8; FR-005; depends on T015)
- [X] T017 [US1] Add the ONE static pipeline-orchestration section (main-as-orchestrator: per-phase role, delegation duty, plan bar, validation duty) to `internal/orchestrator/prompt.go` and re-baseline `internal/orchestrator/prompt_budget_test.go` with an epoch-justification comment; NO phase/handoff state in the prefix — dynamic state rides the task tail/sidecars (Principle III/IV; coordinate with T029 — same single 009 epoch; depends on T008)
- [X] T018 [US1] Add a scripted-provider routing assertion (extend `internal/orchestrator/delegation_regression_test.go` or a new `pipeline_routing_test.go`): every pipeline subagent request carries the configured `SubagentModelID`, verifiable in usage records (PL-17; FR-004; depends on T014)
- [X] T019 [US1] Render pipeline visibility in `internal/tui/view.go` (+ notices threaded from `internal/orchestrator/engine.go`): the NeedsPlan verdict + reason at task start and each phase transition line (research → plan → awaiting approval → implementing → validating) (FR-001 visibility; depends on T016)
- [X] T020 [P] [US1] Add scripted-provider gate tests in `internal/orchestrator/pipeline_gates_test.go`: research-before-plan, no-mutation-before-approve, approval-pause-under-auto-accept, anti-premature-finish → `Interrupted`, each degradation fallback, headless proceed-later, resume from every phase (pipeline-state Acceptance; depends on T016)
- [X] T021 [P] [US1] Add fast-path regression tests in `internal/orchestrator/pipeline_fastpath_test.go`: chat/tiny/small tasks and pure questions never enter the pipeline — no plan file, no forced subagents, no gating, prompts byte-identical to today (PL-2; FR-006; SC-002 precondition; depends on T016)
- [X] T022 [US1] MANDATORY prefix-stability check: extend the existing prefix-stability/marshal-determinism tests (`internal/gateway/marshal_determinism_test.go` + the orchestrator prompt stability test) to assert the 009 epoch prompt is byte-stable across turns and pipeline phases, and that no phase state ever rides the stable prefix (Principle III; depends on T017)

**Checkpoint**: US1 fully functional via scripted providers — pipeline enforced,
approval always pauses, fast path untouched.

---

## Phase 4: User Story 2 — A plan a cheaper model can execute like an expensive one (Priority: P2)

**Goal**: The plan artifact is execution-grade: findings with exact references,
steps with scope + cited finding + acceptance check, verification commands, risks;
versioned and resumable.

**Independent Test**: Inspect a generated plan.md — every step grounded and checkable;
scripted resume/supersede tests pass; live proof is SC-003 in Phase 8.

### Implementation for User Story 2

- [X] T023 [US2] Extend `planMarkdown` in `internal/orchestrator/engine.go` (~2673-2686) to compose the execution-grade artifact: Findings section from banked research facts (cited by scope), ordered Steps each naming target file/function scope + citing a finding + an observable acceptance check, a Verification section (commands for the validate phase), and Risks (HO-8; FR-007; data-model §3; depends on T011)
- [X] T024 [US2] Enforce the plan-phase content bar in `internal/orchestrator/pipeline.go` + `engine.go`: on a pipeline task, `exit_plan_mode` is accepted only when the plan has non-empty steps and the artifact carries findings/verification; otherwise return bounded guidance to complete the plan (same loop-guard shape as existing plan blocks) (HO-9 content bar; depends on T023)
- [X] T025 [US2] Implement supersede semantics in `internal/orchestrator/plan.go` + `engine.go`: a new pipeline run over an existing plan stamps the old one `Superseded` (existing SetPlanPhase, plan.go:78-87) and writes a fresh artifact — never silently overwrites unrelated content (HO-10; FR-009; depends on T023)
- [X] T026 [US2] Wire resume-mid-pipeline in `internal/orchestrator/engine.go` + `pipeline.go` restore path: restart in `approve` reloads the pending plan and re-presents the approval gate; restart in `implement`/`validate` resumes execution with phase + artifact recovered from the `plan_state.json` sidecar (HO-11; FR-007; depends on T012, T004)
- [X] T027 [P] [US2] Add unit tests in `internal/orchestrator/plan_artifact_test.go`: artifact contains all four elements (findings/steps-with-checks/verification/risks), content bar blocks empty plans, supersede-not-overwrite, resume-from-approve and resume-from-implement (HO Acceptance; depends on T024, T025, T026)

**Checkpoint**: Plan artifacts are execution-grade, versioned, resumable.

---

## Phase 5: User Story 3 — Handoffs are contracts, not vibes (Priority: P3)

**Goal**: Every subagent launch carries role/scope/context/deliverable/output-format;
results bank as bounded structured summaries; division is dependency-aware; the TUI
shows phase/role/model per subagent.

**Independent Test**: Unit tests on handoff composition + banking; transcript audit
(SC-004) runs live in Phase 8.

### Implementation for User Story 3

- [X] T028 [US3] Compose the HandoffContract at subagent launch in `internal/orchestrator/subagent.go` (~155): append a fixed template — Role (research-scope / implement-step / review), Scope (specific files/step), Context (only the scope-relevant `Knowledge.Briefing` extract, knowledge.go:102 — never transcript), Deliverable, OutputFormat — with per-role formats (implement reports: changes made / validation performed / problems / remaining concerns) (HO-1/HO-2/HO-6; FR-010; depends on T010)
- [X] T029 [US3] Extend the `run_subagent` tool `task`-field guidance in `internal/orchestrator/engine.go` with the per-phase required output format (extends 008's DG-4 text); this is static tool text — land it in the SAME single 009 prompt epoch as T017 and update the prompt-budget baseline once (HO-3; depends on T017)
- [X] T030 [US3] Tag banked knowledge with phase/role in `internal/orchestrator/knowledge.go` (AddReport, ~47) and scope the `Briefing` extract (~102) so each phase's consumers receive only scope-relevant facts (digest ≤500 / full ≤4000 caps unchanged); consumers never re-read a predecessor's full report inline (HO-4/HO-5; FR-011; depends on T028)
- [X] T031 [US3] Implement bounded recovery in `internal/orchestrator/pipeline.go` + `subagent.go`: a failed/unusable subagent result is re-scoped ONCE or absorbed into direct main-loop work, with the recovery recorded — never a silent loss of a plan part (HO-7; FR-005; depends on T028)
- [X] T032 [US3] Implement dependency-aware division + collection in `internal/orchestrator/pipeline.go` + `engine.go`: the implement phase divides work by the plan's steps; steps the plan marks independent run as parallel subagents within the effort allowance, dependent steps run in order; the main agent collects all reports, verifies every plan step completed, and resolves missing/conflicting work before the validate phase (HO-12/HO-13; R9 Reasonix conductor; depends on T014, T028)
- [X] T033 [US3] Render per-subagent visibility in `internal/tui/view.go`: phase, role, and model for each running subagent; the final answer attributes what each phase contributed (FR-012; depends on T019, T028)
- [X] T034 [P] [US3] Add unit tests in `internal/orchestrator/handoff_test.go`: every launch contains all five in-fields per role, phase-tagged banking + scoped Briefing consumption, failed-subagent bounded recovery (re-scope once), division ordering (independent parallel / dependent serial within allowance) (HO Acceptance; depends on T030, T031, T032)

**Checkpoint**: All client orchestration stories complete — full pipeline behavior
testable end-to-end with scripted providers.

---

## Phase 6: User Story 4 — MiniMax as a first-class provider through the gateway (Priority: P4)

**Goal**: Production-ready MiniMax support — routing, OpenAI-dialect body shaping,
reasoning normalization, cached-token accounting, tiered M3 pricing — verified ONLY
against the simulated fixture (no live spend), plus the client capability profile and
the per-family reasoning-replay policy.

**Independent Test**: The fixture conformance session (≥15 requests, multi-turn tools)
passes with zero shape rejections and honest simulated cache metrics (SC-005,
labeled simulated); DeepSeek-only capture stays byte-identical (FR-018).

**⚠️ Gateway tasks (T035–T043) require only Setup (T002) — run them in parallel with
Phases 2–5 if desired. Client tasks (T044–T048) are independent of the pipeline too.**

### Gateway implementation (F:\MuhiyaWorkspace\MuhiyaWorkspace)

- [X] T035 [P] [US4] Add the MiniMax provider + model rows via an additive migration in `F:\MuhiyaWorkspace\MuhiyaWorkspace\db\migrations\` (+ any row plumbing in `F:\MuhiyaWorkspace\MuhiyaWorkspace\db\db.go`): M3 (1M ctx) and M2.7 / M2.7-highspeed / M2.5 / M2.1 / M2 (204.8k ctx) with tiered M3 pricing (≤512k: $0.30/$1.20/$0.06; >512k: $0.60/$2.40/$0.12 per M in/out/cached) and flat M2.x rates; nullable/additive only — no destructive change (MX-1; FR-013; after T002)
- [X] T036 [US4] Add MiniMax routing + body shaping in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\handler.go`: route resolved MiniMax models to base `https://api.minimax.io/v1` with Bearer auth; standard OpenAI `messages`/`tools[{type:function}]`/`tool_calls`/`{role:tool, tool_call_id}` shapes; streaming relayed exactly like the DeepSeek path; DeepSeek code path untouched (MX-2/MX-3; FR-013; depends on T035)
- [X] T037 [P] [US4] Normalize MiniMax reasoning control in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\thinking.go`: emit the documented `reasoning_split` form on the OpenAI path — never raw client passthrough, consistent with the per-provider normalization policy (MX-4; FR-017; depends on T035)
- [X] T038 [US4] Add MiniMax cached-token accounting in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\translator.go` + `handler.go`: parse `prompt_tokens_details.cached_tokens`, derive miss = prompt − cached (labeled derived), record both in `request_logs` exactly like DeepSeek; below the 512-token threshold render honest zero/unavailable — never fabricated (MX-8/MX-9; FR-015; depends on T036)
- [X] T039 [US4] Implement tiered M3 cost math at the gateway cost-computation site in `F:\MuhiyaCode Agent Go`-facing billing path (`F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\handler.go` or the existing cost helper): branch on prompt size ≤512k vs >512k for M3 input/output/cached rates, flat rates for M2.x, cached vs uncached input distinguished in billing rows (MX-10/MX-11; depends on T038)
- [X] T040 [US4] Verify/extend error + rate-limit relay parity for MiniMax in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\handler.go`: bounded retries, Retry-After passthrough, clean status/error surfaces, keys handled like existing providers (MX-12; FR-016; depends on T036)
- [X] T041 [P] [US4] Build the SIMULATED MiniMax upstream fixture in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\minimax_fixture_test.go`: an httptest server replaying authored MiniMax SSE — text, tool calls, reasoning blocks, usage with `prompt_tokens_details.cached_tokens`, and warm-prefix behavior (second identical request reports ≥50% cached) — swappable to the real endpoint by one base-URL flag when the user funds MiniMax (R8; after T002)
- [X] T042 [US4] Add the gateway conformance test against the fixture in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\minimax_conformance_test.go`: ≥15-request multi-turn tool-calling session → 0 request-shape rejections, call IDs/arguments/results preserved exactly, 100% usage records carry token counts, repeated-prefix probe shows warm cached share ≥50%; ALL outputs labeled simulated (SC-005; MX Acceptance; depends on T036, T037, T038, T041)
- [X] T043 [P] [US4] Add tiered-cost + cache-derivation unit tests in `F:\MuhiyaWorkspace\MuhiyaWorkspace\proxy\` (cost test file next to the cost site): both M3 tiers, cached discount, boundary at 512k, below-512-token honest zero, M2.x flat (MX-10; depends on T039)

### Client implementation (F:\MuhiyaCode Agent Go)

- [X] T044 [P] [US4] Add the MiniMax family capability profile in `internal/gateway/model.go`: documented context/output limits (204.8k; M3 1M — replacing the stale 128k/16k defaults), tool-call rescue ON, reasoning parsing, family detection for M3/M2.x names (MX-13; FR-020; after T001)
- [X] T045 [US4] Implement the per-family reasoning replay policy in `internal/gateway/provider.go` (`replayMessages`, ~298): add a `ReasoningReplayPolicy` to the model profile — MiniMax = PRESERVE assistant reasoning (`reasoning_details`) in replayed history (interleaved-thinking continuity, Mini-Agent requirement M1), DeepSeek = strip, byte-for-byte unchanged; settled reasoning is append-only so the MiniMax prefix stays stable (MX-6; FR-014; depends on T044)
- [X] T046 [US4] Parse MiniMax usage shape client-side in `internal/gateway/sse.go` (+ `internal/contract/cache.go` if a field is missing): read `prompt_tokens_details.cached_tokens` for MiniMax streams so session cache metrics render honestly (zero/unavailable below threshold) through the existing per-model usage rows (FR-015 client surface; depends on T044)
- [X] T047 [US4] MANDATORY prefix-stability check for the replay variant: extend `internal/gateway/marshal_determinism_test.go` to assert the MiniMax-family replay keeps the stable prefix byte-stable across turns while preserving reasoning, and that the DeepSeek replay output is byte-identical to pre-feature (MX-7; Principle III; depends on T045)
- [X] T048 [P] [US4] Add per-family replay unit tests in `internal/gateway/replay_policy_test.go`: MiniMax preserves reasoning on replay including tool-call turns; DeepSeek strips exactly as before; unknown families default to today's behavior (MX Acceptance; depends on T045)
- [X] T049 [US4] Prove FR-018 zero-regression: re-run the DeepSeek-only conformance capture on the finished gateway build and diff against the T002 BEFORE snapshot — byte-identical required; record the diff result in `specs/009-orchestration-minimax-overhaul/results.md` (SC-007; depends on T036–T043)

**Checkpoint**: MiniMax fully wired and simulation-verified; DeepSeek-only behavior
proven unchanged.

---

## Phase 7: User Story 5 — DeepSeek and MiniMax, perfect together (Priority: P5)

**Goal**: Mixed-provider sessions route each stream to its configured provider with
per-provider cache affinity and accurate per-model accounting; fresh installs default
to M3 main + V4Pro sub.

**Independent Test**: Defaults unit matrix passes; scripted mixed-routing test passes;
live-mixed benchmark (DeepSeek live + MiniMax fixture) runs in Phase 8 (SC-006).

### Implementation for User Story 5

- [X] T050 [US5] Add the explicit fresh-install default pair rule as a pre-step in the discovery/assign path — `internal/command/application.go` (`addDiscoveredModels`, ~1032) + `internal/state/config.go` (before `AutoAssignModels`, ~155): when NO ActiveModelID/SubagentModelID is configured AND discovery reports both a MiniMax M3 model and a DeepSeek V4 Pro model, set ActiveModelID = M3 and SubagentModelID = V4Pro explicitly; MiniMax absent → today's `AutoAssignModels` result byte-unchanged; already-configured settings NEVER altered (DM-1..DM-5; FR-022; do NOT rely on score heuristics — verified they mis-pair; depends on T044)
- [X] T051 [P] [US5] Extend `internal/state/config_defaults_test.go` with the four DM acceptance cases: both-providers fresh → M3 main + V4Pro sub; DeepSeek-only fresh → unchanged AutoAssign result; pre-pinned roles → untouched; MiniMax-only (no V4Pro) → falls back to score-based assignment (DM Acceptance; depends on T050)
- [X] T052 [US5] Add a scripted mixed-provider routing test in `internal/orchestrator/mixed_provider_test.go` (two scripted providers): main and subagent streams route independently to their configured provider/model, stay stable within the session, and each provider's request prefix stays byte-stable across its own turns (per-provider cache affinity) (FR-019; MX-15; depends on T045, T018)
- [X] T053 [US5] Verify mixed-session accounting in `internal/contract/cache.go` (`AggregateUsageByModel`) + extend `internal/tui/usage_display_test.go`: per-model rows attribute tokens/cache/cost across BOTH providers with honest unavailable states below cache thresholds (FR-021; MX-16; depends on T046)

**Checkpoint**: All five stories implemented — full feature behavior in place.

---

## Phase 8: Verification & Measurement (constitutional — MANDATORY, cross-cutting)

**Purpose**: The before/after proof Principles VI/X require. DeepSeek legs are LIVE
(budget-window permitting); every MiniMax figure is SIMULATED and labeled.

- [X] T054 Extend `benchmarks/delegationbench/main.go` to observe the pipeline: record phase-transition events, per-phase subagent runs, plan-file mtime vs first-edit time, validation-run presence, and the duplicate-read counter needed by SC-001/SC-003/SC-004 (depends on Phases 3–5)
- [ ] T055 Run the LIVE DeepSeek AFTER leg (delegation workload, max effort, pinned conditions): assert SC-001 (≥2 research runs before any file-changing call; plan on disk before first implementation edit; ≥2 implementation runs; ≥1 validation run before the final answer; checklist 8/8) and SC-003 (plan-only execution 8/8; final main conversation ≤50% of the T003 baseline); store transcripts under `specs/009-orchestration-minimax-overhaul/after/` with cold and steady-state figures separated; if the budget window blocks, record "budget-blocked" and stop (SC-001/SC-003/SC-008; depends on T054)
- [ ] T056 Run the LIVE control leg (simple single-file task): 0 subagents, 0 plan file, correct completion, turns/wall-time within +10% of the current control baseline; record in `specs/009-orchestration-minimax-overhaul/results.md` (SC-002; depends on T054)
- [X] T057 Run the handoff audit over the T055 transcripts: 100% of subagent prompts contain role + deliverable + output format + scoped context; duplicate-read counter not above the T003 baseline; record results (SC-004; depends on T055)
- [ ] T058 Run the mixed-provider benchmark leg: main = DeepSeek (live) + sub = MiniMax (fixture) and note the reverse config as simulated-only; checklist 8/8, per-model usage rows show both providers, each provider's steady-state cache within 5 points of its single-provider baseline; MiniMax figures labeled SIMULATED (SC-006; depends on T052, T053, T055)
- [X] T059 Write the final measurement report into `specs/009-orchestration-minimax-overhaul/results.md`: before/after tables (cold vs steady separated), pinned run conditions, live vs SIMULATED labels on every figure, budget-blocked entries where applicable (SC-008; Principle VI/X; depends on T055–T058)

---

## Phase 9: Polish & Cross-Cutting Concerns

- [ ] T060 [P] Run the full regression sweep in BOTH repos: `gofmt -l` (no diff), `go vet ./...`, `go test ./... -count=1`; confirm the 008 guards byte-stable — denial texts, brief format, prefix-stability/marshal-determinism (now covering the 009 epoch + MiniMax replay variant), loop guards, no token ceiling (SC-007; depends on Phases 3–7)
- [X] T061 [P] Update docs: pipeline section in `docs/agent-design.md`, client `CHANGELOG.md` entry, and the gateway changelog/readme note for MiniMax (marked "verified via simulated fixture; live pending funding") (depends on Phases 3–7)
- [ ] T062 Execute the full [quickstart.md](quickstart.md) validation pass (sections 1–7), checking each scenario off and recording any deviation in `specs/009-orchestration-minimax-overhaul/results.md` (depends on T059, T060)

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)**: starts immediately. T002 MUST precede any gateway change (T035+).
- **Foundational (Phase 2)**: after Setup. Blocks US1/US2/US3 and US5-client work.
- **US1 (Phase 3)**: after Phase 2. The MVP.
- **US2 (Phase 4)**: after US1's T011/T012 (plan write + approval wiring).
- **US3 (Phase 5)**: after US1's T010/T014 (research + implement phases); T029 after T017.
- **US4 (Phase 6)**: gateway track (T035–T043) needs ONLY Setup — fully parallel with
  Phases 2–5 (different repo). Client track (T044–T048) needs only T001. T049 last.
- **US5 (Phase 7)**: T050/T051 after T044; T052 after T045+T018; T053 after T046.
- **Verification (Phase 8)**: T054 after Phases 3–5; T055–T059 sequential-ish on live
  budget; T058 additionally after US5.
- **Polish (Phase 9)**: last; T060/T061 parallel, T062 final.

### Critical path

T001 → T004/T005 → T006 → T008 → T009 → T010 → T011 → T012 → T014 → T015 → T016 →
(T023–T026, T028–T032) → T054 → T055 → T059 → T062

### Cross-repo parallel tracks (biggest win)

- **Track A (client)**: Phases 2 → 3 → 4 → 5
- **Track B (gateway)**: T035 → T036/T037 → T038 → T039/T040 → T041 → T042/T043
  (starts right after T002, independent of Track A)
- Tracks merge at Phase 7 (US5) and Phase 8.

## Parallel Examples

```text
# Setup:            T002 ∥ (T001 → T003)
# Foundational:     T005 ∥ T004, then T006 → T007
# During Phase 3:   gateway T035–T043 run concurrently (different repo);
#                   client T044 ∥ Phase 3 (different package)
# US1 tests:        T020 ∥ T021 (separate new test files)
# US4 gateway:      T037 ∥ T036;  T041 ∥ T036–T040;  T043 ∥ T042
# US4 client:       T048 ∥ T047 after T045
# Polish:           T060 ∥ T061
```

## Implementation Strategy

1. **MVP = Phase 1 + Phase 2 + Phase 3 (US1)**: the enforced pipeline with the
   always-pause approval and the untouched fast path, fully proven by scripted-provider
   tests (T020/T021/T022). This alone answers the user's core frustration.
2. **Incremental delivery**: US2 (plan quality) → US3 (handoffs/visibility) each add
   value and are independently testable; gateway US4 can land in parallel at any point;
   US5 defaults last.
3. **Verification is not optional**: Phase 8's live DeepSeek legs + labeled-simulated
   MiniMax legs gate the "done" claim (SC-008). If the budget window blocks a live leg,
   record it and leave the task open — never fabricate.

## Notes

- [P] = different files, no dependency on an incomplete task.
- Commit after each task or logical group; keep gateway and client commits separate.
- Evidence line numbers (engine.go:1637 etc.) are from research.md's appendix — re-grep
  before editing if the file has drifted.
- Out of scope (do NOT build): MiniMax Anthropic-dialect endpoint, explicit/manual-TTL
  caching, multimodal, cross-provider mid-stream failover, new subagent kinds, any
  token ceiling.
