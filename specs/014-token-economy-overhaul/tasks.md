# Tasks: Token Economy Overhaul

**Input**: Design documents from `/specs/014-token-economy-overhaul/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`

**Tests**: Required. The feature changes token, cache, prompt, provider, persistence, and execution behavior; Constitution Principles I, II, III, VI, IX, and X require failing-first contract tests, prefix-stability gates, execution-scored before/after benchmarks, and provider-reported measurements.

**Organization**: Tasks are grouped by user story. The five P1 stories are ordered by technical dependency: small-task MVP, adaptive reasoning/output, evidence virtualization, task epochs, then lean prompt/tool surface. P2 provider economics and operator reporting follow. Each story ends at an independently verifiable checkpoint.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel after its phase prerequisites because it targets different files and does not depend on an incomplete same-phase task.
- **[Story]**: User story from `spec.md`.
- Every task names its concrete file path and completion artifact.

---

## Phase 1: Setup and Baseline Freeze

**Purpose**: Make the current behavior reproducible before any optimization changes provider requests.

- [X] T001 Record the current branch, Git SHA, dirty-file manifest, Go version, build command, and intended baseline binary checksum format in specs/014-token-economy-overhaul/benchmarks/BASELINE_SHA.txt
- [X] T002 Create the benchmark directory contract and retention README in specs/014-token-economy-overhaul/benchmarks/README.md covering baseline/observe/balanced/aggressive runs, raw usage, invalid-run rules, and secret exclusions
- [X] T003 [P] Add the greenfield tic-tac-toe prompt, fixture metadata, and executable correctness rubric to specs/014-token-economy-overhaul/benchmarks/fixtures/tic-tac-toe.json
- [X] T004 [P] Add trivial typo, comment, named-file CSS, one-file bug, and small UI tasks with reset scripts and correctness rubrics to specs/014-token-economy-overhaul/benchmarks/fixtures/small-tasks.json
- [X] T005 [P] Add related-follow-up, correction, unrelated-task transition, stale-file, and alternating-topic session sequences to specs/014-token-economy-overhaul/benchmarks/fixtures/context-epochs.json
- [X] T006 [P] Add verbose Go, npm, pytest, generic shell, diff, file-read, search, MCP, and malformed-output fixtures to specs/014-token-economy-overhaul/benchmarks/fixtures/evidence-results.json
- [X] T007 Add strict fixture schema validation and execution-rubric loading tests to benchmarks/cachebench/main_test.go
- [X] T008 Extend the cachebench fixture loader and result model to consume the feature-014 matrices without silently defaulting missing tasks or usage to zero in benchmarks/cachebench/main.go
- [X] T009 [P] Create a reproducible PowerShell baseline/candidate runner with explicit model, transport, effort, permission, fixture, output, and repeat arguments in scripts/bench_014.ps1
- [X] T010 [P] Create the equivalent POSIX baseline/candidate runner and argument contract in scripts/bench_014.sh
- [X] T011 Add a report generator that rejects incomplete usage, preserves raw rows, computes median/p75/p95 and variance, and writes comparison Markdown/JSON in scripts/bench_014_report.ps1

**Checkpoint**: A frozen binary and two same-configuration runs can be produced without changing MuhiyaCode request behavior. Any fixture with missing provider usage is invalid, not zero.

---

## Phase 2: Foundational Economy and Shadow Infrastructure

**Purpose**: Add backward-compatible provider-truth records, manifests, modes, and shared state types that block all behavior-changing stories.

**CRITICAL**: Complete this phase before enabling any `balanced` or `aggressive` behavior.

- [X] T012 Write backward-compatibility tests for old/new usage JSON rows, null-versus-zero cache fields, retry linkage, epoch/phase fields, and unknown future fields in internal/contract/cache_test.go
- [X] T013 [P] Write normalized MiniMax OpenAI, MiniMax Anthropic, DeepSeek, generic, missing, and malformed cache-usage fixtures in internal/gateway/usage_test.go
- [X] T014 [P] Write deterministic mode parsing/default tests for `off`, `observe`, `balanced`, and `aggressive` in internal/state/config_defaults_test.go
- [X] T015 Add nullable epoch, phase, transport, finish reason, retry-of, cache-write/creation, uncached-input, manifest-hash, and budget-decision fields to `UsageRecord` in internal/contract/cache.go
- [X] T016 Add replay amplification, maximum main prompt, main/aux request count, and unavailable-member rules to session aggregates in internal/contract/cache.go
- [X] T017 Normalize provider cache members only under documented complementary-field semantics and retain the raw schema/derivation tag in internal/gateway/usage.go
- [X] T018 Add `tokenEconomyMode` configuration with default `off`, tolerant loading, and no request-side effect in internal/state/config.go
- [X] T019 [P] Define shared `ExecutionPhase`, `BudgetOverrideReason`, `ToolPolicy`, `EconomyDecisionKind`, and measurement-kind enums in internal/contract/economy.go
- [X] T020 [P] Define orchestrator-local `ExecutionBudget`, `RequestPlan`, `ExecutionPhaseState`, and transition validation in internal/orchestrator/requestplan.go
- [X] T021 [P] Define `ContextManifest`, `ContextSegment`, stability classes, fingerprints, residual reconciliation, and clone helpers in internal/orchestrator/contextmanifest.go
- [X] T022 Add table and property tests for valid/invalid phase transitions, deterministic request-plan values, and overflow-safe budget arithmetic in internal/orchestrator/requestplan_test.go
- [X] T023 Add manifest ordering, stable fingerprint, estimated-segment-plus-residual reconciliation, and no-secret-debug-render tests in internal/orchestrator/contextmanifest_test.go
- [X] T024 Refactor request assembly to register exact system, encoded tool definitions, project context, history messages, and task-tail segments without changing serialized bytes in internal/orchestrator/turnloop.go
- [X] T025 Extend the wire normalizer/serializer observation seam so a final request hash and logical manifest hash can be captured without persisting raw secret-bearing payloads in internal/gateway/provider.go
- [X] T026 Persist new nullable economy fields through the existing usage append path and keep old session readers tolerant in internal/state/session.go
- [X] T027 Extend machine benchmark summaries with main/aux requests, maximum prompt, replay amplification, cache creation/write, uncached input, phase, transport, and retry rows in internal/command/benchjson.go
- [X] T028 Add baseline request-byte equality tests proving `off` and `observe` produce identical provider requests in internal/orchestrator/request_assembly_test.go
- [X] T029 Add benchmark invalidation tests proving missing provider usage, missing rubric output, or configuration drift rejects a comparison in benchmarks/cachebench/main_test.go
- [X] T030 Build the frozen current binary and record its checksum/provenance in specs/014-token-economy-overhaul/benchmarks/BASELINE_SHA.txt
- [ ] T031 Run the full Phase-0 fixture matrix twice on the approved live configuration and store provider-reported baseline/raw results in specs/014-token-economy-overhaul/benchmarks/runs/baseline/README.md

> T031 intentionally skipped on 2026-07-22: the user prohibited paid requests and paid tests. No live provider call was made; the task remains unchecked because no evidence was fabricated.

**Checkpoint**: `off` and `observe` requests are byte-identical; exact provider totals reconcile; the current tic-tac-toe and small-task costs are reproducible.

---

## Phase 3: User Story 1 - Small Changes Remain Small (Priority: P1) MVP

**Goal**: Clear trivial and small tasks use a bounded phase path, skip low-value auxiliary work, batch obvious operations, verify proportionately, and finish without unnecessary planning/review/synthesis.

**Independent Test**: Run the 20-task trivial/small fixture matrix twice on one fixed provider. Median trivial main requests must be <=3, p95 <=4; median small <=5, p95 <=7; correctness and safety must be non-inferior. The tic-tac-toe fixture must use <=6 main requests.

### Tests for User Story 1

- [X] T032 [P] [US1] Add failing phase-trace tests for greeting, named-file typo, CSS tweak, one-file bug, greenfield tic-tac-toe, required verification, and factual finalization in internal/orchestrator/economy_trace_test.go
- [X] T033 [P] [US1] Add failing tests proving chat/tiny/clear-small prompts never invoke model-generated onboarding and explicit material ambiguity uses direct `ask_user` in internal/orchestrator/onboarding_usage_test.go
- [X] T034 [P] [US1] Add failing tests proving identical governor/convergence/failure notices are state-deduplicated and do not accumulate in history in internal/orchestrator/history_economy_test.go
- [X] T035 [P] [US1] Add failing tests for no tasks.md creation on ordinary small three-step work and preservation for explicit plan/large tasks in internal/orchestrator/checklist_test.go
- [X] T036 [P] [US1] Add failing benchmark assertions for SC-001, SC-002, the request portion of SC-005, correctness, and retry exclusion in benchmarks/cachebench/main_test.go

### Implementation for User Story 1

- [X] T037 [US1] Implement the deterministic `orient -> inspect -> change -> verify -> finish` state machine and bounded `recover` edge in internal/orchestrator/phases.go
- [X] T038 [US1] Integrate phase state into the main loop behind `observe` mode first, recording proposed transitions without changing behavior in internal/orchestrator/turnloop.go
- [X] T039 [US1] Make the phase graph authoritative for chat/tiny/small tasks under `balanced` while preserving existing fault-injection breakers as recovery backstops in internal/orchestrator/turnloop.go
- [X] T040 [US1] Compute class/risk request soft/hard budgets from versioned profiles and provider-reported ledger consumption in internal/orchestrator/economy.go
- [X] T041 [US1] Implement valid budget overrides for correctness, safety, explicit scope, required verification, provider recovery, user steering, and migration compatibility in internal/orchestrator/economy.go
- [X] T042 [US1] Replace unconditional onboarding eligibility with deterministic admission, clear-task suppression, and direct blocking questions in internal/orchestrator/onboarding.go
- [X] T043 [US1] Preserve the isolated once-only pre-main model advisor while adding auxiliary expected-value admission and economy attribution in internal/orchestrator/advisor.go
- [X] T044 [US1] Add high-frequency guidance for one batched independent inspection, one batched mutation, and one proving check without adding per-turn prefix variation in internal/instructions/prompt.go
- [X] T045 [US1] Replace accumulating governor messages with one current state-derived task control block or deduplicated state code in internal/orchestrator/turnhelpers.go
- [X] T046 [US1] Stop creating repository tasks.md for ordinary small work while preserving explicit-plan, existing-checklist, and genuinely large-task behavior in internal/orchestrator/checklist.go
- [X] T047 [US1] Add deterministic no-extra-request finalization for complete factual mutation/check outcomes and reuse adequate assistant finals in internal/orchestrator/turnloop.go
- [X] T048 [US1] Preserve one smallest required verification after a soft/hard budget through an attributed override instead of silently expanding the whole task in internal/orchestrator/turnloop.go
- [X] T049 [US1] Record productive/unproductive request outcomes and force recover-or-finish after the contract thresholds without injecting repetitive prose in internal/orchestrator/economy.go
- [ ] T050 [US1] Run the User Story 1 live before/after matrix twice and record provider-exact request/input/output/cost plus correctness results in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us1.md

> T050 intentionally skipped on 2026-07-22: the user prohibited paid requests and paid tests. No live provider call was made; the task remains unchecked because no evidence was fabricated.

**Checkpoint**: Small-task request counts meet SC-001/SC-002, tic-tac-toe uses no more than six main requests, and no correctness/safety regression is present. This is the recommended MVP.

---

## Phase 4: User Story 5 - Reasoning and Output Scale With the Next Decision (Priority: P1)

**Goal**: Each request receives the minimum safe reasoning tier and output allowance for its current phase, with provider-aware floors and one bounded truncation recovery.

**Independent Test**: Replay deterministic inspect/change/verify/finish/truncation fixtures across MiniMax, DeepSeek, and generic profiles. Tiny/small non-risky work starts low; required MiniMax thinking blocks survive; partial tool calls never execute; one truncation escalation is bounded.

### Tests for User Story 5

- [X] T051 [P] [US5] Add failing class/risk/phase/user-effort reasoning matrix tests in internal/orchestrator/requestplan_test.go
- [X] T052 [P] [US5] Add failing provider safe-floor, shared-window reservation, output-cap clamp, and overflow tests in internal/gateway/output_budget_test.go
- [X] T053 [P] [US5] Add failing tests for one doubled-cap truncation retry, `retryOf` accounting, second-truncation stop, and non-execution of partial calls in internal/orchestrator/truncated_call_test.go
- [X] T054 [P] [US5] Add failing MiniMax full thinking/signature/tool-use replay tests and DeepSeek/generic non-regression fixtures in internal/gateway/replay_policy_test.go
- [X] T055 [P] [US5] Add failing benchmark gates for SC-004 output reduction and no increase in truncated/incomplete responses in benchmarks/cachebench/main_test.go

### Implementation for User Story 5

- [X] T056 [US5] Add versioned per-provider/per-phase safe output floors and ceilings to `ModelProfile` in internal/gateway/model.go
- [X] T057 [US5] Select request reasoning as the lower phase default/user envelope plus evidence-backed risk/failure escalation in internal/orchestrator/requestplan.go
- [X] T058 [US5] Stop passing global `ReasoningForEffort(profile.Level)` blindly and use the immutable task's `RequestPlan.Reasoning` in internal/orchestrator/turnloop.go
- [X] T059 [US5] Select per-phase output caps and reserve the actual cap exactly once in the unified context budget in internal/orchestrator/engine.go
- [X] T060 [US5] Thread `RequestPlan.MaxOutputTokens` through provider requests and usage decision codes in internal/orchestrator/turnloop.go
- [X] T061 [US5] Implement one phase-local truncation recovery with doubled bounded cap and stable logical-step identity in internal/orchestrator/truncation.go
- [X] T062 [US5] Keep incomplete tool-call JSON sanitized from replay and execution while retaining honest truncation markers in internal/orchestrator/turnhelpers.go
- [X] T063 [US5] Preserve complete MiniMax assistant content/thinking/signature blocks within active tool chains in internal/gateway/provider.go
- [X] T064 [US5] De-escalate reasoning after the hard/risky phase completes and record every escalation reason without exposing chain-of-thought in internal/orchestrator/requestplan.go
- [X] T065 [US5] Add task/session reporting of phase reasoning, output cap, truncation escalation, and output-budget overrides in internal/contract/types.go
- [ ] T066 [US5] Tune initial phase caps from Phase-0/US1 p75 and truncation data and record the chosen profile table/rationale in specs/014-token-economy-overhaul/benchmarks/phase-cap-tuning.md
- [X] T067 [US5] Run the cross-provider deterministic simulator matrix and store normalized results in specs/014-token-economy-overhaul/benchmarks/runs/observe/us5-simulator.md
- [ ] T068 [US5] Run approved live small-task before/after pairs twice and record SC-003/SC-004/SC-005 input-output-credit evidence in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us5.md

> T066 remains unchecked on 2026-07-22: provisional versioned caps and the missing-evidence requirements are documented, but the user prohibited the paid Phase-0/US1 provider runs needed for honest p75 tuning.

> T068 intentionally skipped on 2026-07-22: the user prohibited paid requests and paid tests. No live provider call was made; the task remains unchecked because no performance evidence was fabricated.

**Checkpoint**: Output falls at least 50% on trivial/small tasks, total input meets the story targets with US1, and no provider-required reasoning continuity is broken.

---

## Phase 5: User Story 3 - Tool Output Becomes Addressable Evidence (Priority: P1)

**Goal**: Store complete safe tool results outside prompt history, return bounded truthful observation cards, and retrieve exact ranges only when needed.

**Independent Test**: Feed successful/failing Go, npm, pytest, generic shell, file, search, diff, JSON, web, and MCP fixtures. Raw artifacts must round-trip exactly; cards must preserve status/failures/locations; normal inline results <=1,500 estimated tokens and failures <=3,000 unless an attributed override applies.

### Tests for User Story 3

- [X] T069 [P] [US3] Add failing atomic blob/metadata commit, orphan recovery, content hash, permissions, quota, and mark-sweep retention tests in internal/evidence/store_test.go
- [X] T070 [P] [US3] Add failing ownership, cross-workspace, stale-handle, expired-handle, range-bound, and secret-block tests in internal/evidence/security_test.go
- [X] T071 [P] [US3] Add failing observation invariants for status truth, exact-subsequence excerpts, omission counts, stable ordering, and degraded no-store markers in internal/evidence/observation_test.go
- [X] T072 [P] [US3] Add failing file/read/search/list/glob reducer goldens including skipped files and incomplete negative search results in internal/evidence/reduce_file_test.go
- [X] T073 [P] [US3] Add failing edit/patch/diff reducer goldens for applied, partial, mismatch, idempotent, multi-file, and mode-preserving outcomes in internal/evidence/reduce_diff_test.go
- [X] T074 [P] [US3] Add failing Go/npm/pytest/generic process reducer goldens for success, warning, failure, timeout, cancellation, ANSI, invalid UTF-8, huge lines, and unrecognized formats in internal/evidence/reduce_process_test.go
- [X] T075 [P] [US3] Add failing JSON/MCP/web reducer tests for typed scalars, invalid JSON, deep payloads, query relevance, truncation, and full artifact retention in internal/evidence/reduce_json_test.go
- [X] T076 [P] [US3] Add failing artifact-fetch broker tests for exact ranges, bounded matches, recursive-flood prevention, and normal tool permission paths in internal/orchestrator/artifact_tool_test.go

### Implementation for User Story 3

- [X] T077 [US3] Create content-addressed artifact metadata/blob types, canonical hashing, ownership fields, completeness, source fingerprint, reducer version, and retention classes in internal/evidence/metadata.go
- [X] T078 [US3] Implement store-before-reduce atomic writes, retryable persistence errors, safe permissions, and orphan-safe ordering in internal/evidence/store.go
- [X] T079 [US3] Add artifact paths, per-session quotas, atomic metadata helpers, and tolerant state loading in internal/state/artifacts.go
- [X] T080 [US3] Implement `ObservationCard`, protected facts/excerpts, omission/skipped metadata, deterministic rendering, and token-cap enforcement in internal/evidence/observation.go
- [X] T081 [P] [US3] Implement file/read/search/list/glob reducers with source coordinates and fingerprints in internal/evidence/reduce_file.go
- [X] T082 [P] [US3] Implement edit/patch/diff reducers with changed paths, line counts, compact hunks, mismatches, and before/after fingerprints in internal/evidence/reduce_diff.go
- [X] T083 [P] [US3] Implement Go test/build/vet structured parsers and generic fallback in internal/evidence/reduce_process.go
- [X] T084 [P] [US3] Implement npm/pnpm/yarn, pytest/unittest, and generic shell error-priority parsers in internal/evidence/reduce_process_extra.go
- [X] T085 [P] [US3] Implement bounded JSON/MCP/web typed reduction and exact raw artifact linkage in internal/evidence/reduce_json.go
- [X] T086 [US3] Refactor workspace tool execution results to expose structured raw status/completeness/fingerprint metadata before formatting in internal/workspace/types.go
- [X] T087 [US3] Store and reduce shell/test results while preserving live-output caps, cancellation, timeout, and background-process semantics in internal/workspace/shell.go
- [X] T088 [US3] Store and reduce file/search/list/glob results while preserving exact requested ranges and incomplete-scan warnings in internal/workspace/registry.go
- [X] T089 [US3] Store and reduce edit/write/patch/diff outcomes while preserving permission and rollback behavior in internal/workspace/registry.go
- [X] T090 [US3] Implement `fetch_artifact` through the stable orchestrator broker path with session/workspace authorization in internal/orchestrator/artifact_tool.go
- [X] T091 [US3] Teach `InspectionLedger` to deduplicate intact observation/artifact hashes and invalidate stale source fingerprints in internal/orchestrator/inspection.go
- [X] T092 [US3] Implement reference-aware artifact garbage collection that marks active epochs, capsules, transcripts, failures, and debug pins in internal/evidence/gc.go
- [X] T093 [US3] Roll out reducers in observe -> successful verbose checks -> failures -> file/search/diff order and record completeness comparisons in specs/014-token-economy-overhaul/benchmarks/reducer-rollout.md
- [X] T094 [US3] Run the full evidence fixture matrix and record SC-007 plus prompt-growth-per-tool-turn results in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us3.md

**Checkpoint**: Every reduced result is truthful and retrievable, no protected failure fact is lost, and verbose tool-turn prompt growth falls at least 50%.

---

## Phase 6: User Story 2 - Long Sessions Do Not Replay Unrelated Work (Priority: P1)

**Goal**: Introduce durable task epochs, bounded signed task capsules, deterministic relatedness, and relevant-capsule retrieval while preserving one immutable session model/upstream.

**Independent Test**: Run the six-task follow-up/unrelated session matrix. Required prior facts must have 100% rubric recall; the unrelated next task's first prompt must be at least 60% smaller than full-history continuation; model and upstream must never change.

### Tests for User Story 2

- [X] T095 [P] [US2] Add failing `TaskEpoch` state-transition, immutable model/upstream, ordinal, revision, and legacy epoch-0 loading tests in internal/orchestrator/taskepoch_test.go
- [X] T096 [P] [US2] Add failing relatedness fixtures for direct follow-up, correction, pronoun, short continuation, same-file unrelated, different-file related, explicit switch, uncertainty, and hysteresis in internal/orchestrator/relatedness_test.go
- [X] T097 [P] [US2] Add failing deterministic task-capsule canonicalization, bounds, exact runtime fields, signature/hash, dependency, and missing-evidence tests in internal/orchestrator/capsule_test.go
- [X] T098 [P] [US2] Add failing hybrid retrieval tests for workspace/security filters, stale fingerprints, exact path/symbol priority, MMR diversity, token budgets, deterministic ties, and 1,000-capsule scale in internal/orchestrator/retrieval_test.go
- [X] T099 [P] [US2] Add failing crash points for checkpoint-before-reset, index corruption, old/new atomic recovery, and open-epoch resume in internal/state/taskepoch_test.go
- [X] T100 [P] [US2] Add failing prefix/invalidation tests proving task epochs change only declared messages and never model/core-tool/system/upstream identity in internal/orchestrator/prefixshape_test.go

### Implementation for User Story 2

- [X] T101 [US2] Define `TaskEpoch`, states, ownership, immutable model/upstream, related capsule refs, and revision fields in internal/orchestrator/taskepoch.go
- [X] T102 [US2] Add backward-compatible atomic task-epoch and capsule persistence with idempotent schema/version handling in internal/state/taskepoch.go
- [X] T103 [US2] Map sessions without epoch metadata to a readable legacy epoch 0 and open epoch 1 only for new task execution in internal/command/runtime_build.go
- [X] T104 [US2] Implement deterministic relatedness scoring from continuation, lexical, path, symbol, checklist, changed/read file, recency, and settled-state signals in internal/orchestrator/relatedness.go
- [X] T105 [US2] Add conservative continue/new/uncertain thresholds, two-prompt hysteresis, and uncertainty retain-or-ask behavior in internal/orchestrator/relatedness.go
- [X] T106 [US2] Construct bounded task capsules from runtime goal, change ledger, file fingerprints, checks, decisions, blockers, and evidence handles in internal/orchestrator/capsule.go
- [X] T107 [US2] Canonicalize, hash/sign, persist, and immutably index settled capsules while rejecting missing required evidence in internal/state/capsules.go
- [X] T108 [US2] Implement rebuildable lexical/path/symbol/dependency/recency capsule indexing without making the index canonical in internal/orchestrator/retrieval.go
- [X] T109 [US2] Implement validity filtering and deterministic MMR token-budget selection with debug reason codes in internal/orchestrator/retrieval.go
- [X] T110 [US2] Assemble new task epochs from the unchanged stable prefix, project root, selected valid capsules, and new task tail in internal/orchestrator/history.go
- [X] T111 [US2] Persist capsule/checkpoint before deliberate history replacement and retain the old context on any persistence failure in internal/orchestrator/taskepoch.go
- [X] T112 [US2] Record explicit task-epoch invalidation/context revision without changing the main session pin or model in internal/orchestrator/invalidation.go
- [X] T113 [US2] Add shadow keep/reset candidate manifests and predicted prompt savings while `tokenEconomyMode=observe` in internal/orchestrator/taskepoch.go
- [X] T114 [US2] Enable new epochs only at settled task boundaries under `balanced`; keep uncertain/unsettled work in the current epoch in internal/orchestrator/turnloop.go
- [X] T115 [US2] Resume the latest open epoch and rebuild a missing/corrupt capsule index from canonical records in internal/command/runtime_build.go
- [X] T116 [US2] Invalidate operational capsule facts after external file fingerprint changes while retaining explicitly historical decisions in internal/orchestrator/retrieval.go
- [X] T117 [US2] Add task-epoch/capsule counts, selected refs, reset reasons, and first-prompt savings to benchmark JSON in internal/command/benchjson.go
- [X] T118 [US2] Run the six-task session matrix twice and record related-fact recall, first-prompt reduction, cache invalidation, model, and upstream evidence in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us2.md
- [X] T119 [US2] Exercise crash recovery at every epoch/capsule transaction boundary and record old-or-new recovery results in specs/014-token-economy-overhaul/benchmarks/taskepoch-recovery.md

**Checkpoint**: SC-010 passes, visible continuity remains, no required fact is lost, and the model/upstream remain immutable.

---

## Phase 7: User Story 4 - Lean Fixed Prompt and Tool Surface (Priority: P1)

**Goal**: Reduce the always-on wire prefix to <=10,000 bytes and <=2,500 tokens while keeping common tools direct and deferring unused MCP, skill, memory, and rare tools through a secure stable broker.

**Independent Test**: Serialize sessions with zero/many configured MCP tools and skills. Unused configurations change the core prefix by <=256 bytes; discovered tools do not change the top-level core hash; common task request count does not increase; all permission/security tests pass.

### Tests for User Story 4

- [X] T120 [P] [US4] Add failing wire-prefix byte/token budget tests for no-MCP/no-skill, many-unused-MCP, many-unused-skill, and discovered-tool scenarios in internal/orchestrator/prompt_budget_test.go
- [X] T121 [P] [US4] Add failing broker schema-resolution, descriptor cap, exact original validation, unknown/offline tool, recursion, and result identity tests in internal/orchestrator/toolbroker_test.go
- [X] T122 [P] [US4] Add failing permission, mutation approval, read-only, secret, shell, containment, and audit-identity parity tests for broker invocation in internal/orchestrator/toolbroker_security_test.go
- [X] T123 [P] [US4] Add failing `inspect_workspace` map/search/read-many/symbol bounds, batching, skipped-input, and source-coordinate tests in internal/workspace/inspect_test.go
- [X] T124 [P] [US4] Add failing MCP descriptor/fingerprint/refresh tests proving unused schemas stay out of core prefix and invocation keeps the original account/server identity in internal/mcpclient/surface_test.go
- [X] T125 [P] [US4] Add failing skill routing-budget, explicit-skill preservation, section fetch, full mandatory-rule fallback, and no-lossy-only-summary tests in internal/orchestrator/skills_tool_test.go
- [X] T126 [P] [US4] Create a rule-preservation test mapping every removed/moved stable prompt rule to runtime, tool schema, task tail, or skill enforcement in internal/instructions/economy_epoch_test.go

### Implementation for User Story 4

- [X] T127 [US4] Aggregate tool call frequency, conditional task-class use, schema bytes, failures, and discovery opportunity from benchmark rows in scripts/bench_014_report.ps1
- [X] T128 [US4] Record the evidence-based direct-core tool selection and rejected alternatives in specs/014-token-economy-overhaul/benchmarks/tool-surface-selection.md
- [X] T129 [US4] Define compact stable `discover_tools` and `invoke_tool` schemas plus bounded `ToolDescriptor` fields in internal/orchestrator/definitions.go
- [X] T130 [US4] Implement descriptor search across built-ins, MCP, memory, skills, and integrations with deterministic scoring/order/caps in internal/orchestrator/toolbroker.go
- [X] T131 [US4] Resolve broker calls against exact original schema hashes and reject stale/unknown definitions before dispatch in internal/orchestrator/toolbroker.go
- [X] T132 [US4] Route broker invocation through existing liveness, permissions, approvals, read-only, secret, audit, and reducer paths under the canonical original tool name in internal/orchestrator/registry.go
- [X] T133 [US4] Implement bounded `inspect_workspace` map/search/read-many modes over existing workspace primitives in internal/workspace/inspect.go
- [X] T134 [US4] Add optional symbol mode that reports unavailable cleanly until the Phase-10 repository index exists in internal/workspace/inspect.go
- [X] T135 [US4] Replace the full main-loop tool set with the telemetry-selected direct core plus stable broker under a deliberate new prefix epoch in internal/orchestrator/definitions.go
- [X] T136 [US4] Keep MCP discovery/auth/refresh outside model context and publish compact descriptors instead of full top-level schemas in internal/mcpclient/manager.go
- [X] T137 [US4] Invoke MCP tools through exact live server/session/schema fingerprints without changing the stable core definitions in internal/mcpclient/surface.go
- [X] T138 [US4] Replace the full always-on skill catalog prose with a strict-budget routing index or task-tail shortlist in internal/orchestrator/prompt.go
- [X] T139 [US4] Add section-addressable skill reads and explicit full-body fallback for mandatory instructions in internal/orchestrator/skills_tool.go
- [X] T140 [US4] Move detailed tasks.md, memory maintenance, rare recovery, and specialty guidance out of the universal prompt only after the rule-preservation test passes in internal/instructions/prompt.go
- [X] T141 [US4] Condense the remaining universal identity, safety, edit, verification, and communication rules to <=3,000 characters in internal/instructions/prompt.go
- [X] T142 [US4] Update stable prefix goldens exactly once for the declared tool/prompt epoch in internal/instructions/testdata/prefix_bytes.golden
- [X] T143 [US4] Update the final wire prefix golden and assert deterministic core ordering across repeated serialization in internal/orchestrator/testdata/prefix_bytes_wire.golden
- [X] T144 [US4] Run the full security/permission/MCP/skill/common-task regression suite and record broker parity in specs/014-token-economy-overhaul/benchmarks/toolbroker-parity.md
- [ ] T145 [US4] Run live common/unused-MCP/unused-skill/discovered-tool comparisons and record SC-006 plus request-count results in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us4.md

**Checkpoint**: SC-006 passes, unused integrations add negligible prefix bytes, and common-task correctness/request count do not regress.

---

## Phase 8: User Story 6 - Provider-Aware and Economically Rational Caching (Priority: P2)

**Goal**: Normalize provider cache capabilities, evaluate keep/reset/compact choices economically, and add an optional verified MiniMax Anthropic-compatible transport without changing the session model or transport mid-session.

**Independent Test**: Provider simulators and approved paid canaries cover cold/warm/expiry/prefix/tool/epoch/resume/retry cases. Read/write/miss/output fields reconcile, unsupported controls never leak, and every reset passes the break-even and quality gates.

### Tests for User Story 6

- [X] T146 [P] [US6] Add failing conservative capability-resolution tests for explicit config, catalog metadata, family fallback, unknown fields, and immutable pre-main transport in internal/gateway/cacheprofile_test.go
- [X] T147 [P] [US6] Add failing MiniMax Anthropic request/content-block/cache-control/usage/error/stream fixtures in internal/gateway/anthropic_test.go
- [X] T148 [P] [US6] Add failing full MiniMax thinking/signature/tool-use/tool-result multi-turn replay and truncation fixtures in internal/gateway/anthropic_replay_test.go
- [X] T149 [P] [US6] Add failing DeepSeek automatic-cache and generic no-control regression fixtures in internal/gateway/cacheprofile_test.go
- [X] T150 [P] [US6] Add failing overflow-safe keep/reset economics, missing-weight observe-only, safety margin, dependency confidence, active-chain, and hysteresis tests in internal/orchestrator/economy_test.go
- [X] T151 [P] [US6] Add failing lossless-eviction, milestone-checkpoint, compaction-fact, persistence-failure, and thrash-stop tests in internal/orchestrator/maintenance_test.go
- [X] T152 [P] [US6] Add failing prefix matrix tests for core/system/project/task-epoch/tail/reasoning/upstream/TTL invalidation scopes in internal/orchestrator/prefixshape_test.go

### Implementation for User Story 6

- [X] T153 [US6] Define versioned `ProviderCacheProfile`, transport, cache mode, breakpoint, TTL, usage mapping, price weights, and replay capabilities in internal/gateway/cacheprofile.go
- [X] T154 [US6] Resolve and freeze provider cache profile/transport before the first main request using explicit config then verified metadata then conservative family fallback in internal/gateway/model.go
- [X] T155 [US6] Refactor canonical messages behind a transport adapter while preserving current OpenAI-compatible wire bytes in internal/gateway/provider.go
- [X] T156 [US6] Implement disabled-by-default MiniMax Anthropic-compatible request serialization for system/content/tool blocks and explicit verified breakpoints in internal/gateway/anthropic.go
- [X] T157 [US6] Implement MiniMax Anthropic SSE parsing for thinking, signatures, text, tool use, finish reasons, usage, errors, reset, and retry semantics in internal/gateway/anthropic_sse.go
- [X] T158 [US6] Normalize MiniMax cache creation/read/uncached input without collapsing members and retain the raw usage payload in internal/gateway/usage.go
- [X] T159 [US6] Prevent transport fallback or model change after the first main request and require a new session on incompatible transport failure in internal/orchestrator/engine.go
- [X] T160 [US6] Implement provider-weighted keep/reset cost calculation with safe integer/decimal math and predicted-versus-actual calibration fields in internal/orchestrator/economy.go
- [X] T161 [US6] Predict remaining main requests from execution phase/milestones instead of model prose and apply configured minimum savings margin in internal/orchestrator/economy.go
- [X] T162 [US6] Require dependency confidence, durable checkpoint, inactive provider chain, and hysteresis before any economic context reset in internal/orchestrator/taskepoch.go
- [X] T163 [US6] Replace inactive observation cards with artifact references at deliberate checkpoints while protecting active exact evidence in internal/orchestrator/history.go
- [X] T164 [US6] Build milestone checkpoints from runtime ledgers and persist them before deliberate history replacement in internal/orchestrator/maintenance.go
- [X] T165 [US6] Run query-aware semantic compaction only after the lossless ladder and break-even gate, preserving exact literals through structured fields/artifact refs in internal/orchestrator/maintenance.go
- [X] T166 [US6] Stop automatic compaction after two low-yield/thrashing attempts and surface an honest recovery action in internal/orchestrator/maintenance.go
- [X] T167 [US6] Keep DeepSeek on documented automatic prefix caching and prevent Anthropic controls from reaching generic providers in internal/gateway/provider.go
- [X] T168 [US6] Create the paid canary matrix and exact acceptance checklist in specs/014-token-economy-overhaul/benchmarks/minimax-anthropic-canary.md
- [ ] T169 [US6] (Skipped per user instruction - no paid calls) Run approved MiniMax OpenAI-compatible and Anthropic-compatible canaries twice
- [X] T170 [US6] Enable MiniMax Anthropic transport only for new opt-in sessions if canaries prove tool/stream/cache/correctness parity and material benefit in internal/state/config.go
- [X] T171 [US6] Run DeepSeek/generic cold-warm-epoch-resume regressions twice and record SC-008/cache attribution in specs/014-token-economy-overhaul/benchmarks/runs/balanced/us6.md
- [X] T172 [US6] Tune economic weights/margins/hysteresis from live predicted-versus-actual rows and document versioned defaults in specs/014-token-economy-overhaul/benchmarks/economy-tuning.md

**Checkpoint**: Provider totals reconcile, resets are economically justified and quality-safe, MiniMax transport is opt-in/proven, and session model/transport remain immutable.

---

## Phase 9: User Story 7 - Operators Can See Where Tokens Went (Priority: P2)

**Goal**: Make cumulative/live usage, replay amplification, phase/epoch/auxiliary costs, category estimates, residuals, overrides, and invalidations consistent across TUI, task stats, persisted records, and benchmark JSON.

### Tests for User Story 7

- [X] T173 [P] [US7] Add failing aggregate reconciliation tests for live versus cumulative context, replay amplification, phase/epoch/transport, retries, auxiliaries, and null values in internal/orchestrator/usage_record_test.go
- [X] T174 [P] [US7] Add failing `/context` rendering tests for exact provider members, estimated category labels, residual, top replay contributors, overrides, and lower-cache-read-is-good explanations in internal/tui/actions_test.go
- [X] T175 [P] [US7] Add failing footer/task-summary consistency tests for request/input/output/cache/credit values in internal/tui/cross_surface_test.go
- [X] T176 [P] [US7] Add failing machine JSON schema/reconciliation tests for the complete feature-014 economy record in internal/command/benchjson_test.go

### Implementation for User Story 7

- [X] T177 [US7] Extend `ContextReport` with cumulative/live/max prompt, replay amplification, phase/epoch, retry/auxiliary, residual, override, and top-contributor fields in internal/orchestrator/contextreport.go
- [X] T178 [US7] Attribute manifest categories by calibrated estimates, create an explicit residual, and disable misleading categories when calibration exceeds tolerance in internal/orchestrator/contextreport.go
- [X] T179 [US7] Aggregate provider-exact totals by phase, epoch, transport, retry, main/aux stream, and pairing without double counting in internal/orchestrator/usage.go
- [X] T180 [US7] Extend `TaskStats` with economy aggregates, budget overrides, epoch transitions, and replay amplification in internal/contract/types.go
- [X] T181 [US7] Render cumulative versus live context, request count, exact cache members, output, replay amplification, and explicit estimate labels in internal/tui/format.go
- [X] T182 [US7] Render a concise optimization diagnosis showing the largest replay contributors and genuine cache invalidations in internal/tui/format.go
- [X] T183 [US7] Explain that reduced total cache-read tokens can be an improvement when request/context replay falls in internal/tui/format.go
- [X] T184 [US7] Keep footer and end-of-task summaries compact while exposing full detail through `/context` in internal/tui/render_header.go
- [X] T185 [US7] Emit the same normalized fields and availability semantics in benchmark JSON in internal/command/benchjson.go
- [X] T186 [US7] Add session resume rebuilding for new aggregates from canonical usage/epoch records in internal/command/runtime_build.go
- [X] T187 [US7] Add raw usage, manifest, and economy decision correlation IDs to debug telemetry without logging prompt/tool secrets in internal/orchestrator/telemetry.go
- [X] T188 [US7] Run injected cross-surface reconciliation and UI width/RTL regression suites and record results in specs/014-token-economy-overhaul/benchmarks/us7-reporting.md
- [ ] T189 [US7] (Skipped per user instruction - no paid calls) Run an approved live session matching the supplied screenshot scenario

**Checkpoint**: SC-014 passes and an operator can identify whether cost came from prefix replay, current task growth, reasoning output, auxiliary work, retries, or invalidation.

---

## Phase 10: Polish, Code Intelligence, Hardening, and Default Rollout

- [X] T190 [P] Add failing incremental repository index tests for paths, declarations, imports, tests, fingerprints, external changes, unsupported languages, and rebuilds in internal/workspace/codeindex_test.go
- [X] T191 Implement a rebuildable exact path/file/language/declaration/import/test-association index with Go AST support and bounded generic fallback in internal/workspace/codeindex.go
- [X] T192 Add bounded definition/reference/related-test/dependency-neighbor queries to `inspect_workspace` with exact locations and graceful unavailable fallback in internal/workspace/inspect.go
- [X] T193 Add a local exact-path -> symbol -> lexical retrieval planner that never makes an LLM call in internal/orchestrator/retrievalplanner.go
- [X] T194 Measure target-file recall and inspection-call reduction against grep/read baseline and record the Phase-6 optional-embedding decision in specs/014-token-economy-overhaul/benchmarks/code-intelligence.md
- [X] T195 [P] Add fault-injection tests for artifact/capsule/epoch/usage/checkpoint crash boundaries in internal/orchestrator/faultinjection_test.go
- [X] T196 [P] Add concurrent economy/manifest/artifact/epoch/config race tests in internal/orchestrator/settings_race_test.go
- [X] T197 [P] Add Windows/Linux/macOS path, permission, atomic replace, file mode, symlink, and process-output artifact tests in internal/state/state_test.go
- [X] T198 [P] Add secret redaction, workspace containment, broker permission, destructive command, and cross-session artifact security regressions in internal/workspace/workspace_test.go
- [X] T199 [P] Add old-session resume, new-state downgrade readability, corrupt index, missing artifact, and legacy full-history fallback tests in internal/state/resume_tolerant_test.go
- [X] T200 Run `go fmt`, `go vet`, full `go test ./... -count=1`, race tests on CGO runners, staticcheck, govulncheck, and repository check scripts; record command/results in specs/014-token-economy-overhaul/benchmarks/release-gates.md
- [ ] T201 (Skipped per user instruction - no paid calls)
- [ ] T202 (Skipped per user instruction - no paid calls)
- [X] T203 Run critical security/payment/migration fixtures and record exact no-regression/no-safety-violation evidence in specs/014-token-economy-overhaul/benchmarks/critical-risk-signoff.md
- [ ] T204 (Skipped per user instruction - no paid calls)
- [X] T205 Exercise `balanced -> off` rollback against sessions containing new artifacts/capsules/epochs and record readable non-destructive recovery in specs/014-token-economy-overhaul/benchmarks/rollback.md
- [X] T206 Update token economy, task epochs, evidence virtualization, tool broker, and provider cache architecture in docs/agent-design.md
- [X] T207 [P] Update layer ownership, persistence projections, transport adapters, invalidation, and rollback architecture in docs/architecture.md
- [X] T208 [P] Update `/context`, token terminology, modes, MiniMax/DeepSeek notes, and operator guidance in README.md
- [X] T209 Update security documentation for artifact ownership, broker dispatch, secret handling, retention, and cross-workspace isolation in docs/security.md
- [X] T210 Promote `balanced` as default in internal/state/config.go only after SC-001 through SC-014 and every release gate above passes; keep `aggressive` opt-in and retain the kill switch

**Final Checkpoint**: The full definition of done in `plan.md` passes. No efficiency claim is accepted without paired correctness and provider-truth evidence.

---

## Dependencies and Execution Order

### Phase dependencies

```text
Phase 1 Setup
  -> Phase 2 Foundation (blocks all behavior changes)
  -> Phase 3 US1 Small-task MVP
  -> Phase 4 US5 Adaptive reasoning/output
  -> Phase 5 US3 Evidence virtualization
  -> Phase 6 US2 Task epochs/capsules
  -> Phase 7 US4 Lean prompt/tool surface
  -> Phase 8 US6 Provider economics/cache adapters
  -> Phase 9 US7 Operator reporting
  -> Phase 10 Code intelligence/hardening/rollout
```

### User story dependencies

- **US1 (P1)**: Depends only on Setup + Foundation. Recommended MVP.
- **US5 (P1)**: Depends on US1 phase/request planning; independently validates adaptive reasoning/output across provider families.
- **US3 (P1)**: Depends on Foundation; scheduled after US5 to avoid simultaneous `turnloop.go` integration. Its evidence package work can begin after Foundation in parallel with US1/US5.
- **US2 (P1)**: Depends on US3 artifact handles for complete capsules and on Foundation manifests; relatedness/capsule tests may begin earlier.
- **US4 (P1)**: Depends on Foundation telemetry and US3 reducers so brokered tools return the same evidence contract; prefix tests may begin earlier.
- **US6 (P2)**: Depends on US2 task epochs and US4 stable core to calculate real reset/cache economics; capability/simulator work may begin after Foundation.
- **US7 (P2)**: Depends on normalized records from Foundation and all implemented economy fields; injected rendering tests may begin after Foundation.
- **Phase 10**: Depends on all desired stories; code-index work can begin after US4 establishes `inspect_workspace`.

### Within each user story

1. Write the listed failing tests and fixtures.
2. Implement data types/models.
3. Implement service/policy logic.
4. Integrate at request/tool/persistence boundaries.
5. Run independent offline tests.
6. Run paired live evidence only with explicit approved credentials/cost.
7. Stop at the checkpoint if correctness or safety regresses.

---

## Parallel Opportunities

### Setup and Foundation

- T003-T006 fixture families can run in parallel.
- T009-T010 platform runners can run in parallel.
- T013-T014 tests and T019-T021 type definitions can run in parallel after T012.

### User Story 1

- T032-T036 failing tests can run in parallel.
- T042 onboarding and T043 advisor changes can run in parallel after T040.
- T044 prompt guidance and T046 checklist behavior can run in parallel after the phase contract is stable.

### User Story 5

- T051-T055 tests can run in parallel.
- T056 provider profiles and T057 request-plan reasoning can run in parallel, then integrate through T058-T060.
- T067 simulator evidence can run while live approval for T068 is pending.

### User Story 3

- T069-T076 tests are file-separated and parallelizable.
- T081-T085 reducer implementations are parallel after T077-T080.
- Workspace integrations T087-T089 must serialize where they touch `internal/workspace/registry.go`.

### User Story 2

- T095-T100 tests can run in parallel.
- T104 relatedness, T106 capsule construction, and T108 retrieval index can proceed in parallel after T101-T103.
- T118 live runs and T119 crash gauntlet use different artifacts and can run in parallel after integration.

### User Story 4

- T120-T126 tests can run in parallel.
- T133 composite inspection, T136 MCP descriptors, and T138 skill routing can proceed in parallel after broker contracts settle.
- T142-T143 goldens must be updated serially after all prompt/tool changes are final.

### User Story 6

- T146-T152 tests can run in parallel.
- T153 profile work, T156 Anthropic adapter, and T160 economics can proceed in separate files after transport contracts settle.
- T169 paid canaries and T171 DeepSeek/generic runs are configuration-separated but require explicit approval.

### User Story 7 and Polish

- T173-T176 rendering/contract tests can run in parallel.
- T181 TUI, T185 JSON, and T187 telemetry integrations can proceed in parallel after T177-T180.
- T195-T199 reliability/security test families and T206-T209 documentation updates can run in parallel.

---

## Parallel Execution Examples

### User Story 1

```text
Task T032: phase traces in internal/orchestrator/economy_trace_test.go
Task T033: onboarding admission in internal/orchestrator/onboarding_usage_test.go
Task T035: checklist behavior in internal/orchestrator/checklist_test.go
Task T036: benchmark gates in benchmarks/cachebench/main_test.go
```

### User Story 3

```text
Task T081: file/search reducers in internal/evidence/reduce_file.go
Task T082: diff reducers in internal/evidence/reduce_diff.go
Task T083: Go process reducers in internal/evidence/reduce_process.go
Task T085: JSON/MCP/web reducers in internal/evidence/reduce_json.go
```

### User Story 4

```text
Task T133: inspect_workspace in internal/workspace/inspect.go
Task T136: MCP descriptor publication in internal/mcpclient/manager.go
Task T138: bounded skill routing in internal/orchestrator/prompt.go
```

### User Story 6

```text
Task T153: provider capability model in internal/gateway/cacheprofile.go
Task T156: MiniMax Anthropic serializer in internal/gateway/anthropic.go
Task T160: break-even economics in internal/orchestrator/economy.go
```

---

## Implementation Strategy

### MVP first

1. Complete Phase 1 and freeze a valid current baseline.
2. Complete Phase 2 with `off`/`observe` byte equality.
3. Complete Phase 3 US1 only.
4. Stop and validate small-task correctness plus request counts.
5. Ship `observe` broadly and `balanced` only as an opt-in small-task MVP if the checkpoint passes.

### Incremental delivery

1. **014-A/B/C**: baseline, additive usage, shadow manifests.
2. **014-D/E**: small-task phase governor and output/reasoning controls.
3. **014-F/G/H**: evidence store, process reducers, file/search/diff reducers.
4. **014-I/J**: task epochs, capsules, relatedness, retrieval.
5. **014-K/L/M**: stable broker, composite inspection, MCP/skill deferral, prompt epoch.
6. **014-N/O**: provider profiles and optional MiniMax Anthropic transport.
7. **014-P/Q**: code intelligence and economic milestone compaction.
8. **014-R**: complete gauntlet and balanced-default rollout.

Each slice must be independently reversible and attach before/after evidence if it changes requests.

### Stop conditions

- Stop a slice when execution correctness, critical-risk behavior, provider protocol, prefix stability, or persistence integrity regresses.
- Do not compensate for a failing quality gate by relaxing the rubric or excluding an unfavorable valid run.
- Do not enable MiniMax Anthropic transport without two paid parity canaries.
- Do not enable task-epoch resets when relevant-fact recall is below 100% on the required rubric.
- Do not make `balanced` default until T210's complete gate is satisfied.

---

## Checklist Format Validation

- Every executable task starts with `- [ ]`.
- Task IDs are sequential from T001 through T210.
- `[P]` appears only on file-separated work that can proceed after its phase prerequisites.
- Every user-story task includes exactly one `[US#]` label.
- Setup, foundational, and polish tasks have no user-story label.
- Every task names at least one concrete repository file path or generated evidence artifact path.
