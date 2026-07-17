# Tasks: Harness Reliability & Clarity Overhaul

**Input**: Design documents from `/specs/008-harness-reliability-overhaul/`

**Prerequisites**: plan.md, spec.md (US1–US5, FR-001..021, SC-001..009), research.md
(R1–R8, D1–D4, F1–F8), data-model.md, contracts/ (delegation-guidance,
model-switch-warning, usage-display, memory-tool), quickstart.md

**Tests**: Included where a contract names a conformance assertion or the
constitution mandates them (Principles VI/X: before/after benchmark evidence and
prefix-stability checks are NOT optional). Accuracy emphasis: every behavior change
carries a regression-guard test; nothing ships unverified.

**Organization**: Single repo `F:\MuhiyaCode Agent Go` (client only). Tasks grouped
by user story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 from spec.md

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Benchmark workloads + runner every story's validation uses.

- [X] T001 Create the delegation benchmark workload fixture (a task prompt over a fixture tree with 3–4 genuinely independent sub-scopes: audit + small change each, mirroring the real 32-request/0-subagent session shape) plus expected-outcome checklist, in `specs/008-harness-reliability-overhaul/benchmarks/delegation-workload.md` (+ fixture files under `benchmarks/fixture/`)
- [X] T002 [P] Create the low-effort control workload (single-file fix, single concern) with expected outcome in `specs/008-harness-reliability-overhaul/benchmarks/control-workload.md`
- [X] T003 [P] Extend the benchmark runner to execute a workload live at a pinned effort and emit per-run JSON (AgentRuns, final main-stream PromptTokens, Σmiss/Σcompletion, steady-state hit rate, duplicate-read count, tool-call audit incl. write_file-on-existing-file count, correctness checklist results) in `benchmarks/delegationbench/main.go` (reuse the cachebench harness patterns)

---

## Phase 2: Foundational (Before-Measurements — BLOCKING)

**Purpose**: Constitution Principle X — the "before" leg MUST run on the unmodified
build; it blocks every behavior-changing story.

**⚠️ CRITICAL**: Complete before any task in Phases 3–7 that changes behavior.

- [X] T004 Run the BEFORE leg: delegation workload at max effort on the pre-feature build (pinned model/gateway/effort); save run JSON + raw usage records to `specs/008-harness-reliability-overhaul/benchmarks/before/` (baseline for SC-001/002/008: expect ~0 subagent runs, record final main PromptTokens, billed tokens, steady-state rate, edit-audit counts)

**Checkpoint**: Before-evidence captured — behavior changes may now land.

---

## Phase 3: User Story 1 — The agent delegates like a senior engineer (P1) 🎯 MVP

**Goal**: The model actually delegates independent sub-parts within its allowance,
prefers surgical edits over whole-file rewrites, and near-miss edit loops are
bounded — all guidance in the byte-stable prefix as one recorded epoch.

**Independent Test**: quickstart §1 (before/after benchmark), §2 (control), §3
(edit-fumble bounded recovery).

### Static-prefix guidance (one epoch)

- [X] T005 [US1] Rewrite the delegation guidance in `internal/orchestrator/prompt.go`: replace the hedged trailing ENVIRONMENT clause (prompt.go:80-83) with a dedicated DELEGATION section implementing DG-1..DG-3 (positive criteria: independent exploration of scope not in context / parallelizable sub-parts / post-edit review at high allowance; negative: single-file linear work; brief semantics: `agents<=N` is this task's allowance to plan with) and reconcile CACHE DISCIPLINE (prompt.go:99-100) with one clause ("already-read context is cheap — NEW broad exploration is what you delegate") (research F1/F6, contract DG-1..3)
- [X] T006 [US1] Add the edit-discipline guidance to the same static section in `internal/orchestrator/prompt.go`: prefer `edit_file`/`multi_edit` for existing files; `write_file` only for new files or genuine full regeneration (FR-018; ships in the same epoch as T005)
- [X] T007 [US1] Rewrite the `run_subagent` tool description in `internal/orchestrator/engine.go` (~:2460-2467) per DG-4: when-to-use + one worked example + per-kind guidance (explore/plan/review/general) + deliverable-focused `task` field description ("the subagent does not see this conversation — be specific about the deliverable"); keep schema fields/enum unchanged (research F2/R6 Reasonix pattern)

### Effort-layer cleanup & AutoReview

- [X] T008 [US1] Delete the dead `EffortProfile.Directives` and `PlanBeforeEdit` fields and their initializers in `internal/orchestrator/effort.go` (research F5, D2); keep `AutoReview` and `ParallelAgents`; grep-verify zero remaining references
- [X] T009 [US1] Consume `AutoReview`: in `internal/orchestrator/engine.go`, at max effort after file-changing work with agent budget remaining, append a one-time tail rider suggesting a `review` subagent (DG-7: ≤ the ~50-token rider budget, at most once per task, dynamic tail only — never prefix)

### Bounded recovery (edit-fumble loop)

- [X] T010 [US1] Reclassify near-miss edit outcomes as failures in `internal/workspace/registry.go` (execEdit/execMultiEdit): return an error carrying the existing note text when any edit reports `oldString not found` or `oldString appears N times`; keep idempotent outcomes (`newString already present`, `identical; skipped`) as successes (DG-9/10, data-model §2; research D4 — files.go note text itself stays verbatim)

### Tests (contract conformance)

- [X] T011 [P] [US1] Extend prefix-stability/marshal-determinism coverage over the new prompt text + tool description (DG-5): steady-state byte-identity across consecutive turns; single recorded upgrade epoch, in `internal/orchestrator/prompt_test.go` (or the existing stability test file)
- [X] T012 [P] [US1] Scripted-provider test: repeated near-miss `edit_file` calls now trip the loop-guard nudge and the 8-in-6-turns failure terminator with the `closest region` note intact and NO token-ceiling message (DG-9/11, quickstart §3) in `internal/orchestrator/h5_circuit_breaker_test.go`
- [X] T013 [P] [US1] Regression tests: denial/budget texts byte-identical (subagent.go:83-134), `agents<=N` brief format unchanged, classAgents caps unchanged, AutoReview rider fires once at max effort only and fits the rider budget, in `internal/orchestrator/subagent_test.go` / `classify_test.go`
- [X] T014 [P] [US1] Idempotent-edit regression test: `newString already present` and `identical; skipped` still classify as success (no failure recorded) in `internal/workspace/registry_test.go`

### Validation (after US1 lands)

- [ ] T015 [US1] Run the AFTER leg: identical delegation-workload run on the feature build → `specs/008-harness-reliability-overhaul/benchmarks/after/`; assert SC-001 (AgentRuns ≥ 2; final main PromptTokens ≤ 75% of before), SC-002 (billed ≤ before+10%; steady-state rate ≥ before), SC-008 (zero write_file-on-existing where an edit sufficed), duplicate-reads ≤ before (DG-14); write the comparison summary to `benchmarks/comparison.md`
- [ ] T016 [US1] Run the control workload at low effort on the feature build → assert AgentRuns == 0 and correct completion (SC-003), recorded in `benchmarks/comparison.md`

**Checkpoint**: US1 independently complete — delegation + edit discipline proven on live benchmark.

---

## Phase 4: User Story 2 — No surprise bills from a mid-session model switch (P2)

**Goal**: Warning with proceed/cancel before any mid-session model change applies.

**Independent Test**: quickstart §4 matrix.

- [X] T017 [US2] Insert the warning gate at the top of `chooseModel` in `internal/tui/actions.go` (~:326): trigger = selected ID ≠ current for the role AND `m.runtime.Engine.UsageAggregate().Requests ≥ 1`; two-choice modal via `openChoice` with **Cancel as the Recommended default**, message naming role + old→new IDs + cold-cache + pricing-may-differ (MS-1..3, MS-7); proceed dispatches the unchanged SetModel path; same-model/fresh-session/refresh paths untouched (MS trigger matrix)
- [X] T018 [P] [US2] TUI tests in `internal/tui/model_switch_test.go`: warning appears (main + subagent roles, names both IDs); Cancel and Esc leave settings/model/invalidation ledger untouched (byte-identical settings snapshot); proceed applies exactly one model-switch invalidation event; same-model reselect and zero-request session show no warning (SC-004, quickstart §4)

**Checkpoint**: US1+US2 independently verifiable.

---

## Phase 5: User Story 3 — The token number means what it costs (P3)

**Goal**: Headline = provider-reported uncached input + output; `cache N%` tag;
detail stays in /context.

**Independent Test**: quickstart §5.

- [X] T019 [US3] Add one shared headline helper (per-task `contract.Usage` → figure + tag state implementing UD-1..4: miss+completion when cache metrics present; TotalTokens fallback; estimated markers preserved) in `internal/tui/view.go` (or `internal/contract` if the summary path needs it), and use it in BOTH `renderActivity` (~:299-301) and `taskSummaryLine` (~:1149-1169); simplify `cacheTag` to percentage-only (UD-2)
- [X] T020 [P] [US3] Table-driven tests: cache-metrics case equals miss+completion exactly; no-metrics fallback equals TotalTokens; estimated marker rendering; live-line and summary-line agree with each other and with /context's uncached total for the same task (UD-5/SC-005) in `internal/tui/usage_display_test.go`

**Checkpoint**: headline honest and consistent across all views.

---

## Phase 6: User Story 4 — A session report worth reading (P4)

**Goal**: Session panel (cost, API vs active time, lines ±, per-model table, hit
rates) + context categories, inside the existing /context modal, ≤72 cols.

**Independent Test**: quickstart §6.

- [X] T021 [US4] Add `DurationMS *int64` to `contract.UsageRecord` (nullable; usage.jsonl-compatible) in `internal/contract/cache.go`, and capture request wall-time around all three provider-call sites (main, aux, subagent) threading through the existing observation structs into `usageRecord()` in `internal/orchestrator/engine.go` + `internal/orchestrator/subagent.go` (data-model §4)
- [X] T022 [P] [US4] Add pure `AggregateUsageByModel(records)` (rows: model, requests, uncached-in, output, cache-read, member-set cost, estimated count; + Total) in `internal/contract/cache.go` with unit tests in `internal/contract/cache_test.go` (UD-7)
- [X] T023 [P] [US4] Extract ONE shared diff-count helper (the view.go:894-910 logic) into `internal/contract/diffcount.go`; switch `internal/tui/view.go` to it; add `LinesAdded/LinesRemoved int` to `contract.TaskStats` and accumulate at the tool-outcome path (where `filesChanged` is populated) in `internal/orchestrator/engine.go` (data-model §4; applied successful calls only)
- [X] T024 [US4] Session accumulators + report plumbing in `internal/orchestrator/engine.go`: `sessionActiveMS` (Σ TaskStats.DurationMS at task end), `sessionLinesAdded/Removed`; extend `ContextReport` with APITimeMS (Σ non-nil record durations), ActiveMS, LinesAdded/Removed, `ByModel` rows, and `Categories` (depends on T021/T022/T023)
- [X] T025 [US4] Context categories: capture component byte sizes at request assembly (base prompt + addendum, marshaled tool definitions at tool-boundary changes, project-context block + skills section, compact summary) in `internal/orchestrator/engine.go`; convert via calibrated `tokPerChar`; messages = history − summary; free = limit − Σ (floor 0); populate `ContextReport.Categories` labeled estimated (UD-10..12, data-model §6)
- [X] T026 [US4] Rewrite `formatContextReport` in `internal/tui/actions.go`: session panel FIRST (cost credits with priced-fallback, API time, active time "this session", lines +A −R, session/steady hit rates), then Context window, then per-model table rows (`  <model>  in X · out Y · read Z · cost C` + Total), then Categories table, then existing cache-health/invalidations; every absent metric renders explicit unavailable; all lines ≤72 cols in the existing label/value style (UD-6..9, UD-13..16)
- [X] T027 [P] [US4] Tests: by-model member-set cost (nil ⇒ unavailable row), categories sum to limit ±1pt, every rendered panel line ≤72 cols, resume rebuilds API time + per-model from records while active time/lines reset with "this session" label (UD-9/11/13; SC-006) in `internal/tui/context_panel_test.go` + `internal/orchestrator/engine_test.go`

**Checkpoint**: /context reconciles exactly with usage.jsonl; narrow terminals clean.

---

## Phase 7: User Story 5 — Memory that feels like a first-class ability (P5)

**Goal**: `save_memory` tool with distinct Memory rendering; MEMORY.md format
preserved; approval-gate parity.

**Independent Test**: spec US5 scenarios 1–5; contract memory-tool §4.

- [X] T028 [US5] Memory append helper in `internal/orchestrator/memory.go` (or `internal/state` if file IO lives there): append a concise entry to workspace `MEMORY.md` in the established format — create-if-absent, whitespace-normalized exact-dedupe ("already known"), ~500-char content bound with reject-and-summarize error, reserved-tag escaping reuse from `project_context.go` (MT-4..7)
- [X] T029 [US5] Register the `save_memory` tool in `internal/orchestrator/engine.go` (+ executor wiring): schema `{content required, title optional, additionalProperties:false}`, description per MT-2 (what belongs in memory; rare and concise); route through the SAME mutation approval gate as file edits (MT-9); apply the existing secret-redaction screening to content (MT-10); on success emit the existing one-shot memory-update block so the in-session view refreshes without boot-block resend (MT-8)
- [X] T030 [US5] Rewrite the memory instruction in `internal/orchestrator/prompt.go` (memoryInstruction, ~:12-13) to route saves through `save_memory` (hand-editing remains possible for restructuring) — NOTE: if shipped in the same release as T005/T006, fold into the SAME commit/epoch; otherwise record a second epoch explicitly (MT-3, DG-5 note)
- [X] T031 [US5] Distinct TUI rendering in `internal/tui/view.go`: `toolLabel` gains `save_memory → "Memory"`; collapsed row shows the saved title/first words; outcome measure renders "saved" / "already known" / denied (never a diff count); expanded view shows the full entry (MT-11..13)
- [X] T032 [P] [US5] Tests: helper unit tests (dedupe, size bound, tag escaping, create-if-absent, manual-edit preservation across 10 mixed writes — SC-009), tool tests (schema validation, approval-gate parity in normal vs auto-accept, redaction, update-block emission), TUI test (Memory label + outcome states) in `internal/orchestrator/memory_test.go` + `internal/tui/memory_row_test.go`

**Checkpoint**: all five stories independently complete.

---

## Phase 8: Polish & Cross-Cutting

- [X] T033 Update `docs/agent-design.md` where orchestration behavior descriptions changed (delegation guidance, edit-failure classification, memory tool) — constitution workflow gate
- [X] T034 [P] Epoch audit: assert exactly ONE recorded prompt-upgrade epoch for the release (delegation + edit-discipline + memory instruction text landed together) via the invalidation ledger / stability test, documented in `specs/008-harness-reliability-overhaul/benchmarks/comparison.md`
- [X] T035 Full gates both dimensions: `go fmt` (no diff), `go vet ./...`, `go test ./... -count=1` all packages green; rider-budget test still bounds the tail with the AutoReview nudge included (quickstart §7)
- [ ] T036 Run the full quickstart matrix §§1–7 end-to-end and record results per SC in `specs/008-harness-reliability-overhaul/benchmarks/comparison.md` (SC-001..009 sign-off table)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: none — T001/T002/T003 parallel-friendly (T003 consumes T001/T002 formats; start T003 after fixture shapes settle)
- **Phase 2 (Before-leg)**: needs T001+T003; **blocks Phases 3–7 behavior changes** (Principle X)
- **Phase 3 (US1)**: T005→T006 (same file, same section), T007/T008 parallel to T005-T006 (different files); T009 after T008 (consumes AutoReview); T010 independent; tests T011–T014 after their implementations; T015/T016 last (T015 needs T005–T014 complete)
- **Phase 4 (US2)**: independent of US1; T017→T018
- **Phase 5 (US3)**: independent; T019→T020
- **Phase 6 (US4)**: T021→T024→T025→T026; T022/T023 parallel after Phase 2; T027 last. T023 touches view.go — coordinate with T019 (same file, sequence within the file)
- **Phase 7 (US5)**: T028→T029→T031; T030 coordinates with T005/T006 (same file + epoch note); T032 last
- **Phase 8**: after all desired stories; T034 requires the release grouping decision from T030's note

### Within-file coordination (no parallel edits to one file)

`prompt.go`: T005→T006→T030 · `engine.go`: T007→T009→T021→T024→T025→T029 ·
`view.go`: T019→T023→T031 · `actions.go`: T017→T026

## Parallel Example: after Phase 2 completes

```text
Lane A (orchestrator): T005→T006 ∥ T007 ∥ T008→T009 ∥ T010 → T011–T014 → T015/T016
Lane B (TUI, independent): T017→T018 ∥ T019→T020
Lane C (contract): T022 ∥ T023 (then feed Lane A's engine tasks T021→T024→T025)
Then: T026→T027 (US4 render), T028→T029→T030→T031→T032 (US5), T033–T036 (polish)
```

## Implementation Strategy

**MVP = Phase 1 + Phase 2 + Phase 3 (US1)**: after T016 the agent demonstrably
delegates, edits surgically, and cannot burn tokens on near-miss edit loops —
the core reliability value, proven on a live before/after benchmark.
Then increments in priority order: US2 (switch warning) → US3 (honest headline)
→ US4 (session report) → US5 (memory tool) — each independently testable per its
quickstart/contract section; stop and validate at every checkpoint. Accuracy
discipline throughout: no task is marked done without its paired test/benchmark
assertion green, and Phase 2's before-evidence is immutable once captured.
