# Tasks: Terminal-Bench Readiness

**Input**: Design documents from `/specs/014-terminal-bench-readiness/` and the evidence-backed roadmap at [`docs/MUHIYACODE_TERMINAL_BENCH_READINESS_PLAN.md`](../../docs/MUHIYACODE_TERMINAL_BENCH_READINESS_PLAN.md).

**Prerequisites**: `spec.md`, `plan.md`, the roadmap's findings B-1 through B-38, and the MuhiyaCode Constitution.

**Tests**: Required for every behavior change. Changes to prompts, tool schemas, history, or compaction must retain byte-stable prefix coverage. Cost and quality improvement claims require repeatable before/after evidence under identical configuration.

**Resumption rule**: Start with the first unchecked task whose dependencies are complete. Never reset, stash, discard, or overwrite unrelated working-tree changes. Mark `[X]` only after the implementation and the test/gate named in that task have passed.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup and Baseline

**Purpose**: Preserve the existing checkout and establish a measurable, resumable implementation baseline.

- [X] T001 Record `git status --short`, current HEAD, toolchain, and baseline test results in `audits/terminalbench/baseline/README.md` without modifying unrelated files.
- [X] T002 [P] Create the benchmark result contract in `specs/014-terminal-bench-readiness/contracts/benchmark-result.md` with status, exit-code, config, usage, trajectory, verification, and redaction rules.
- [X] T003 [P] Create generic adapter fixture repositories and expected-output documentation in `specs/014-terminal-bench-readiness/fixtures/` and `specs/014-terminal-bench-readiness/quickstart.md`.
- [X] T004 Add an anti-leakage review checklist to `specs/014-terminal-bench-readiness/checklists/requirements.md` covering prompts, project-marker-only verification, task isolation, and hidden-test exclusion.
- [X] T005 Run and record the unchanged focused package suite in `audits/terminalbench/baseline/README.md`: `go test ./internal/command ./internal/orchestrator ./internal/workspace ./internal/gateway -count=1`.

**Checkpoint**: The continuation record, result contract, fixtures, and unchanged baseline are durable.

## Phase 2: Foundational Benchmark Execution

**Purpose**: Deliver a non-interactive adapter, run record, and isolated runner. This phase blocks all benchmark acceptance claims.

- [X] T006 Add benchmark run configuration and validated result/status types in `internal/command/bench.go` and shared types in `internal/contract/`.
- [X] T007 Add `muhiyacode bench` registration and task-file/stdin/output flag parsing in `internal/command/root.go`.
- [X] T008 Add one-result emission, status-to-exit-code mapping, and stdout/file write behavior in `internal/command/bench.go`.
- [X] T009 Extend `internal/command/benchjson.go` to include status, cause, reason, duration, tool/check counts, changed files, session/trajectory identity, configuration, usage/cost, models, errors, and verification fields while retaining backward-compatible `completed`.
- [X] T010 Add adapter unit and subprocess tests in `internal/command/bench_test.go` for valid records, stdin/file tasks, output files, and exit codes.
- [X] T011 Add per-tool start/end timing to transcript emission in `internal/orchestrator/turnloop.go` and `internal/state/session.go`, with redaction-preserving trajectory tests.
- [X] T012 Add a per-task isolated-state and result-capture harvester in `scripts/bench_terminalbench.ps1` and `scripts/bench_terminalbench.sh`.
- [X] T013 Add harvester tests or fixture smoke commands in `scripts/bench_terminalbench.ps1`, `scripts/bench_terminalbench.sh`, and `specs/014-terminal-bench-readiness/quickstart.md` for a single task and a timeout task.

**Checkpoint**: A fake-provider task can drive the real adapter path and emit an inspectable record with a linked trajectory.

## Phase 3: User Story 1 — Isolated, Configurable Headless Runs (Priority: P1)

**Goal**: A fresh non-TTY container can perform contained mutations and testing with inline configuration, explicit bounds, and truthful lifecycle output.

**Independent test**: An empty temporary `MUHIYA_HOME`, fake provider, and non-TTY subprocess create a file in the workspace, emit a result/trajectory, and do not create settings.

- [X] T014 [US1] Add auto-accept-only workspace auto-trust in `internal/workspace/permissions.go`; preserve normal confirmation and sensitive-root rejection tests in `internal/workspace/permissions_test.go`.
- [X] T015 [US1] Add an explicit canonical-workspace trust override surface in `internal/command/root.go` and `internal/command/runtime_build.go`, with tests proving it cannot trust out-of-root or sensitive paths.
- [X] T016 [US1] Define CLI/environment override parsing for model, base URL, API key, effort, context limit, task timeout, token budget, cost budget, advisor, and seed in `internal/command/root.go`.
- [X] T017 [US1] Apply override values in memory only after loading state in `internal/command/runtime_build.go`; test that benchmark runs do not persist `settings.json` or secrets.
- [X] T018 [US1] Permit inline configuration to satisfy the one-shot setup gate in `internal/command/root.go`, while retaining the existing setup error for genuinely incomplete configuration.
- [X] T019 [US1] Disable benchmark-mode onboarding, startup discovery, web/MCP probes, and settings write-back in `internal/command/runtime_build.go`, with no-network/startup-mutation tests.
- [X] T020 [US1] Add timeout and budget fields plus `StopCauseTimeout`/budget termination semantics in `internal/contract/types.go` and `internal/orchestrator/turnloop.go`.
- [X] T021 [US1] Wrap benchmark execution with timeout cause and deferred result emission in `internal/command/bench.go`, including cancellation-safe status derivation tests.
- [X] T022 [US1] Enforce cumulative token and known-cost budgets after usage recording in `internal/orchestrator/turnloop.go` and `internal/orchestrator/usage.go`, with nil/unknown-cost coverage.
- [X] T023 [US1] Add a non-resetting post-header stream lifetime cap in `internal/gateway/provider.go` and keep-alive-drip coverage in `internal/gateway/provider_timeout_test.go`.
- [X] T024 [US1] Add fresh-state headless end-to-end tests in `internal/command/bench_test.go` for mutation success, timeout exit, budget exit, and missing-config `blocked` status.

**Checkpoint**: The critical path B-1/B-3/B-4/B-5/B-6/B-8/B-11/B-29/B-33 is implemented and testable without a pre-baked home directory.

## Phase 4: User Story 2 — Safe, Reliable Tool Execution (Priority: P2)

**Goal**: Tool operations work predictably in a benchmark workspace without weakening path and shell protections.

**Independent test**: Workspace/orchestrator unit suites cover each recovered failure and each contained-vs-sensitive denial boundary.

- [X] T025 [US2] Add bounded `fullLines`/raw long-line support to `read_file` in `internal/workspace/files.go` and schema/registry wiring in `internal/workspace/registry.go`.
- [X] T026 [US2] Add long-line first-attempt edit coverage in `internal/workspace/files_test.go` for `read_file` followed by `edit_file`.
- [X] T027 [US2] Make `inspect_code` advertise and report its Go-only limitation clearly and add continuation/pagination behavior in `internal/workspace/codeindex.go` and `internal/workspace/registry.go`.
- [X] T028 [US2] Add Go and non-Go inspection behavior tests in `internal/workspace/codeindex_test.go`.
- [X] T029 [US2] Make `multi_edit` atomic for non-idempotent misses in `internal/workspace/files.go`, preserving useful mismatch diagnostics.
- [X] T030 [US2] Add atomic multi-edit tests in `internal/workspace/files_test.go` for a mixed match/miss batch and already-applied idempotent cases.
- [X] T031 [US2] Permit canonical in-workspace, non-sensitive cleanup operands in auto-accept mode in `internal/workspace/risk.go`; retain blocking for root, parent traversal, and sensitive paths.
- [X] T032 [US2] Scope environment enumeration and safe in-workspace chmod behavior to benchmark auto-accept in `internal/workspace/risk.go`, with positive and negative tests.
- [X] T033 [US2] Refuse `git add`, `git commit`, and `git push` in benchmark mode in `internal/workspace/risk.go` and record the policy in `internal/instructions/tools.go`.
- [X] T034 [US2] Tighten unknown-property and empty-required-string rejection in `internal/orchestrator/validate.go` and add nested schema tests in `internal/orchestrator/validate_test.go`.
- [X] T035 [US2] Extend prose-to-tool rescue for arrays and `tool_calls` envelopes in `internal/gateway/rescue.go` with coverage in `internal/gateway/rescue_test.go`.
- [X] T036 [US2] Run `go test ./internal/workspace ./internal/orchestrator ./internal/gateway -count=1` and record the Phase 4 gate in `audits/terminalbench/phase-2.md`.

**Checkpoint**: Tool errors no longer arise from known, recoverable file/shell/argument defects, while sandbox boundaries remain explicit and tested.

## Phase 5: User Story 3 — Context and Honest Cost Efficiency (Priority: P3)

**Goal**: Preserve task-critical context through compaction, reduce redundant inspection, and distinguish reported from estimated cost.

**Independent test**: Compaction recovery, deterministic summaries, scoped invalidation, equivalent search deduplication, and prefix invariants pass.

- [X] T037 [US3] Add bounded, secret-screened read-content recovery storage keyed by canonical path and mtime in `internal/orchestrator/maintenance.go` and `internal/orchestrator/knowledge.go`.
- [X] T038 [US3] Make compaction deterministic with stable ordering and digest fallback in `internal/orchestrator/maintenance.go`; add repeat-run tests in `internal/orchestrator/maintenance_test.go`.
- [X] T039 [US3] Annotate or exclude compacted file knowledge in `internal/orchestrator/knowledge.go` and test briefing integrity in `internal/orchestrator/knowledge_test.go`.
- [X] T040 [US3] Store search roots and invalidate only overlapping searches in `internal/orchestrator/inspection.go`; retain conservative invalidation for workspace-wide and shell operations.
- [X] T041 [US3] Normalize search signatures through canonical path keys in `internal/orchestrator/inspection.go` and add equivalent-path deduplication tests.
- [X] T042 [US3] Add a local, explicitly estimated fallback price table in `internal/orchestrator/usage.go` or `internal/contract/cache.go`, with no-provider-cost tests.
- [X] T043 [US3] Run prefix, request-assembly, inspection, knowledge, and compaction tests in `internal/orchestrator/` and record any sanctioned cache invalidation in `audits/terminalbench/phase-3.md`.

**Checkpoint**: Context/token efficiency fixes are deterministic, bounded, redaction-aware, and do not obscure measurement provenance.

## Phase 6: User Story 4 — Verification, Termination, and Recovery (Priority: P4)

**Goal**: Runs stop for the right reason and code-changing runs perform a bounded, generic verification stage before a clean outcome.

**Independent test**: Scripted providers and fixture repositories prove final checks, no-op recovery, final-turn execution, stalled termination, bounded exploration, and provider failure handling.

- [X] T044 [US4] Add generic project-marker test-runner discovery and verification result types in `internal/orchestrator/verification.go`.
- [X] T045 [US4] Inject one non-reentrant, bounded verification stage before finalization in `internal/orchestrator/turnloop.go` for changed code-task runs with no checks.
- [X] T046 [US4] Record verification command, output truncation, result, and failure effect in `internal/contract/types.go`, `internal/command/benchjson.go`, and adapter result tests.
- [X] T047 [US4] Add Go, Node, Rust, Python, no-runner, and failed-check fixture tests in `internal/orchestrator/verification_test.go`.
- [X] T048 [US4] Execute final-turn tool calls before finalization in `internal/orchestrator/turnloop.go` and add cap-turn dispatch coverage.
- [X] T049 [US4] Add a bounded no-op-first-response re-prompt in `internal/orchestrator/turnloop.go` with empty-response fault-injection coverage.
- [X] T050 [US4] Add a stalled-progress terminator and `blocked` status mapping in `internal/orchestrator/turnloop.go` and `internal/command/bench.go`.
- [X] T051 [US4] Add bounded no-edit exploration runway escalation in `internal/orchestrator/turnloop.go` and scripted read-then-edit tests.
- [X] T052 [US4] Add successful-loop window limits per tool in `internal/orchestrator/gates.go` or `internal/orchestrator/turnloop.go` with distinct-argument loop coverage.
- [X] T053 [US4] Add sliding provider-failure breaker and bounded `Retry-After` recovery in `internal/gateway/provider.go` and/or `internal/orchestrator/turnloop.go`.
- [X] T054 [US4] Extend `internal/orchestrator/faultinjection_test.go` with timeout, budget, verification, final-turn call, empty turn, stalled progress, 503, and 429 scenarios.
- [X] T055 [US4] Run `go test ./internal/orchestrator ./internal/gateway ./internal/command -count=1` and record the Phase 6 gate in `audits/terminalbench/phase-4.md`.

**Checkpoint**: A model cannot silently report work complete after doing nothing, skipping required generic verification, dropping its final action, or spinning without progress.

## Phase 7: User Story 5 — Model Integrity, Observability, and Reproducibility (Priority: P5)

**Goal**: Fixed-model and routed benchmark runs are distinct, reproducible, and completely attributed.

**Independent test**: Catalog fixtures demonstrate no fixed-model substitution, explicit routed switches, appropriately gated provider fields, and repeatable records.

- [X] T056 [US5] Add benchmark advisor modes (`off`, `pinned`, `routed`) and hard-block missing configured models in `internal/command/runtime_build.go` and `internal/orchestrator/advisor.go`.
- [X] T057 [US5] Add fixed-model and routed-mode tests in `internal/orchestrator/advisor_test.go` and `internal/command/bench_test.go`.
- [X] T058 [US5] Capture model switch events, per-request reasoning, and invalidations in `internal/orchestrator/usage.go`, `internal/contract/types.go`, and `internal/command/benchjson.go`.
- [X] T059 [US5] Gate `reasoning_effort` request-body emission by provider capability in `internal/gateway/provider.go` and test strict generic profiles.
- [X] T060 [US5] Thread optional benchmark seed through `internal/command`, `internal/gateway`, and profile capability checks with deterministic fixture coverage where supported.
- [X] T061 [US5] Isolate one process/state root per task and document no-cross-task-state behavior in `internal/command/bench.go`, `scripts/bench_terminalbench.ps1`, and `scripts/bench_terminalbench.sh`.
- [X] T062 [US5] Extend harvester aggregation in `scripts/bench_terminalbench.ps1` and `scripts/bench_terminalbench.sh` for repeats, P50/P95, variance, usage/cache, cost/success, timeout, test execution, and loop metrics.
- [X] T063 [US5] Add result redaction and broader secret-export checks in `internal/state/session.go`, `internal/command/bench.go`, and their focused tests.

**Checkpoint**: A published result identifies exactly how the task ran, which model(s) participated, what was spent, why it ended, and where the redacted trajectory is stored.

## Phase 8: Validation, Documentation, and Readiness Gate

**Purpose**: Demonstrate the complete acceptance path with no benchmark-specific behavior or unverified quality claims.

- [X] T064 Add Linux container smoke assets in `benchmarks/terminalbench/` or a dedicated `Dockerfile.bench` and document invocation in `specs/014-terminal-bench-readiness/quickstart.md`.
- [X] T065 Run container smoke scenarios for Go and Python fixtures with a fresh state root; store sanitized results in `audits/terminalbench/container-smoke/`.
- [X] T066 Run the repeated fixed-model and routed benchmark matrix through `scripts/bench_terminalbench.ps1` or `scripts/bench_terminalbench.sh`, storing raw records and aggregate output in `audits/terminalbench/final/`.
- [X] T067 Perform anti-leakage inspection of `internal/instructions/`, `internal/workspace/`, `internal/orchestrator/`, scripts, and fixtures; record results in `audits/terminalbench/final/anti-leakage.md`.
- [X] T068 Update `README.md`, `docs/agent-design.md`, `docs/architecture.md`, and `docs/security.md` for the benchmark command, isolation, result semantics, verification policy, and scoped auto-trust behavior.
- [X] T069 Run `go test ./... -count=1`, `go vet ./...`, `gofmt -l cmd internal benchmarks`, and the production build; record outcomes in `audits/terminalbench/final/validation.md`.
- [X] T070 Run race tests for changed concurrency-sensitive packages where a C compiler is available; otherwise record the exact environment limitation in `audits/terminalbench/final/validation.md`.
- [X] T071 Update the B-1–B-38 resolution table and Definition-of-Done evidence in `docs/MUHIYACODE_TERMINAL_BENCH_READINESS_PLAN.md`.
- [X] T072 Mark the final readiness checklist in `specs/014-terminal-bench-readiness/checklists/requirements.md` only after all required implementation and validation evidence is present.

## Dependencies and Execution Order

- Phase 1 preserves and measures the baseline before behavioral changes.
- Phase 2 creates the adapter contract; Phase 3 makes that adapter usable in a fresh container.
- Phase 4 and Phase 5 may proceed after Phase 3 but must merge sequentially if they overlap shared workspace/orchestrator files.
- Phase 6 depends on Phase 2 result types and Phase 3 lifecycle semantics.
- Phase 7 depends on the result record and adapter configuration from Phases 2–3.
- Phase 8 depends on all prior implementation tasks and is the only phase permitted to claim readiness.

## Parallel Opportunities

- T002, T003, and T004 are documentation-only and independent after T001.
- T011 and T012 affect distinct telemetry/script surfaces after T006–T010 define the record.
- T025/T027/T029/T031/T034/T035 can be developed independently but must be integrated carefully around shared workspace tests.
- T037/T040/T042 are independent within context work, subject to shared test updates.
- T044 and T053 can begin after the status/result model is stable; T048–T052 are sequential turn-loop edits.
- T056, T059, and T060 are separate model/provider surfaces once Phase 3 configuration exists.

## Implementation Strategy

1. Deliver the runnable adapter and headless critical path first, including its focused tests.
2. Integrate tool/context fixes in small tested groups, never weakening security to gain convenience.
3. Make verification and termination truthful before running comparative evaluations.
4. Finish only after repeated container-based validation, anti-leakage review, and repository-wide gates.
