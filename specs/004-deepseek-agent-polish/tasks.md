# Tasks: Coding Agent Quality Polish & DeepSeek V4 Optimization

**Input**: Design documents from `/specs/004-deepseek-agent-polish/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included — mandatory here, not optional: the constitution (Principles VI/X) requires before/after verification for every improvement claim, and this feature's scripted suites double as the red-baseline evidence of the current defects ([research D9](research.md)). Cache-affecting work carries prefix-stability check tasks (Principle III / workflow gates).

**Organization**: Tasks grouped by user story (spec priority order). **Global ordering rule (D9, measurement-first)**: Phase 2's live baselines MUST land before any behavior-changing task in Phases 3–7; within each story, test tasks land (red) before their implementation tasks.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: US1–US5 per spec.md
- Every task names its exact file path(s)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Feature governance artifacts that every story reports into

- [X] T001 Create `specs/004-deepseek-agent-polish/audit.md` seeded from [research.md](research.md) Part A findings (A2, A3, A5.1–A5.6, A6, A7, A9 degraded-path note, D5 wire risks) with columns per [data-model.md](data-model.md) §5 (id, category, symptom, root cause file:line, disposition, verification) and the seven FR-012 category headings
- [X] T002 Create `specs/004-deepseek-agent-polish/benchmarks/README.md` recording the D9 procedure: N≥3 runs per scenario, price flags, pricing-window logging convention (peak/off-peak Beijing 2×), token-counts-primary rule per [contracts/deepseek-wire.md](contracts/deepseek-wire.md) §7

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Fresh baselines and the one empirical verdict — no baseline, no verifiable claim (constitution VI/X)

**⚠️ CRITICAL**: T003–T005 MUST complete before any behavior-changing task in Phases 3–7 begins. These are live, billed gateway runs.

- [ ] T003 Capture baseline benchmark, scenario `coding-session` (N≥3, price flags, window recorded) via `benchmarks/cachebench` → `specs/004-deepseek-agent-polish/benchmarks/baseline/`; record steady-state hit rate, prefix-stability rate, tokens, cost, invalid-call count, redundant-call count, manual interventions
- [ ] T004 Capture baseline benchmark, scenario `fat-context` (same flags/convention) via `benchmarks/cachebench` → `specs/004-deepseek-agent-polish/benchmarks/baseline/`
- [ ] T005 Execute the `reasoning_content` replay probe per [contracts/deepseek-wire.md](contracts/deepseek-wire.md) §3 against `deepseek-v4-pro` and `deepseek-v4-flash` through the MuhiyaLLM gateway; write verdict (`empty-string-accepted` | `replay-required`) + evidence to `specs/004-deepseek-agent-polish/benchmarks/replay-probe.md`
- [X] T006 [P] Sweep configs and docs for legacy `deepseek-chat`/`deepseek-reasoner` references (retire 2026-07-24, research B1) and replace with V4 IDs in `docs/prompt-caching.md`, `docs/agent-design.md`, `README.md`, and any default-model constants found by grep

**Checkpoint**: Baselines + probe verdict committed — story implementation may begin

---

## Phase 3: User Story 1 — Reliable Task Execution on the Launch Model (Priority: P1) 🎯 MVP

**Goal**: Terminal-state guarantees, DeepSeek wire robustness, and honest completion — no stalls, no dangling "in progress" items, no claimed-but-not-done work ([contracts/completion-terminal.md](contracts/completion-terminal.md), [contracts/deepseek-wire.md](contracts/deepseek-wire.md))

**Independent Test**: quickstart §3 terminal-state + completion + wire suites green; benchmark reruns show invalid/redundant calls −50% and 100% terminal-state runs (SC-003/004)

### Tests for User Story 1 (write first — must FAIL against current behavior)

- [ ] T007 [US1] Write terminal-state tests — `ToolEnd` paired with every `ToolStart` on success/error/gate-block/cancellation paths, subagent terminal `done` on cancellation — in `internal/orchestrator/terminal_state_test.go` (red: cancellation paths currently drop events, research A6)
- [X] T008 [P] [US1] Write TUI task-end sweep tests — stuck `running` tool + agent items transition to `cancelled` on task-complete message, idempotent on re-delivery, `ok`/`fail` untouched — in `internal/tui/sweep_test.go`
- [X] T009 [P] [US1] Write completion-audit table tests — C1–C4 rules, check-claim pattern set positives and guarded negatives, class gating (inert for chat/tiny) per [contracts/completion-terminal.md](contracts/completion-terminal.md) §2 — in `internal/orchestrator/completion_audit_test.go`
- [ ] T010 [P] [US1] Write tool-call salvage detector tests — shapes (a) JSON-in-content, (b) DSML, (c) language-prefixed; one-salvage-per-turn bound; no-argument-invention property — in `internal/gateway/salvage_test.go`
- [ ] T011 [P] [US1] Write zero-token empty-completion ladder tests — detection (200, no content/tool calls, `completion_tokens==0` after tool-result turn), retry rungs, shared ≤2 bound with empty-final retries, `empty_completion_retry` events — in `internal/gateway/gateway_test.go`

### Implementation for User Story 1

- [ ] T012 [US1] Guarantee `ToolEnd` emission on all dispatch exits (defer-based) in `internal/orchestrator/engine.go` (gatedExecute/executeBatch paths) and terminal subagent `done` events in `internal/orchestrator/subagent.go` (satisfies T007)
- [X] T013 [US1] Add `cancelled` state + task-end sweep for `toolView`/`agentView` (incl. nested subagent tool items) in `internal/tui/model.go`, neutral terminal glyph rendering in `internal/tui/view.go` (satisfies T008)
- [X] T014 [US1] Implement completion audit in `finalize` in `internal/orchestrator/engine.go`: C2 incomplete-steps disclosure (via existing `hasIncompletePlan`), C3 check-claim reconciliation against `stats.ChecksRun`, C4 `fallbackAnswer`+disclosures replacing bare `"Done."` (satisfies T009; C1/T10-T11 phase stamping arrives with US2 T027 at the same call site)
- [ ] T015 [US1] Harden tool-call salvage behind `NeedsToolCallRescue` in `internal/gateway/provider.go` + shape detection in `internal/gateway/model.go`: cover shapes a/b/c, bound one per turn, emit `tool_call_salvaged` wire event (satisfies T010)
- [ ] T016 [US1] Implement empty-completion classification + retry ladder (immediate retry → `[recover]` rider → honest breaker surface) in `internal/gateway/provider.go` and the rider injection in `internal/orchestrator/engine.go`, sharing the existing empty-final ≤2 bound (satisfies T011)
- [ ] T017 [US1] ONLY IF T005 verdict = `replay-required`: retain and replay `reasoning_content` on active-window assistant tool-call turns in `internal/gateway/provider.go`, fold at settled boundaries in `internal/orchestrator/history.go`, then re-run the probe green and update `specs/004-deepseek-agent-polish/benchmarks/replay-probe.md`
- [ ] T018 [P] [US1] Extend the `tool_choice` invariant test to assert no code path can construct a value other than `auto`/`none` in `internal/orchestrator/request_assembly_test.go` ([contracts/deepseek-wire.md](contracts/deepseek-wire.md) §6)
- [X] T019 [US1] Record dispositions for audit rows A6, A7, and the D5 wire risks in `specs/004-deepseek-agent-polish/audit.md` (`fixed:<task>` links to T012–T017)

**Checkpoint**: US1 fully functional — terminal-state, completion, and wire suites green independently

---

## Phase 4: User Story 2 — Plans That Know When They Are Finished (Priority: P2)

**Goal**: The plan phase machine — finished plans never advertise executability again, across resume ([contracts/plan-lifecycle.md](contracts/plan-lifecycle.md))

**Independent Test**: quickstart §3 plan-lifecycle suite green (T1–T12, natural phrasing, supersede, resume matrix); quickstart §5 walkthrough scenarios 1–3 (SC-001)

### Tests for User Story 2 (write first — must FAIL against current behavior)

- [X] T020 [US2] Write plan-lifecycle transition tests for every row T1–T12 incl. the T8 step-progress path with natural phrasings ("proceed with the plan", "yes, build it") and the T2→T9 supersede chain, in `internal/orchestrator/plan_lifecycle_test.go` (red: A2 defect)
- [X] T021 [P] [US2] Write save/resume + legacy-derivation tests — `finished` resumes silent; legacy snapshot without `phase` derives pending/drafting/none; legacy `pending` with all-completed steps corrects to `finished`; crash mid-`executing` corrects to `interrupted` — in `internal/orchestrator/plan_resume_test.go`

### Implementation for User Story 2

- [X] T022 [US2] Add `PlanPhase` type + constants and additive `PlanStateSnapshot.Phase` field in `internal/contract/types.go` per [data-model.md](data-model.md) §1
- [X] T023 [US2] Persist/restore phase in `plan_state.json`: write-on-change, terminal-resolution write (finished/superseded/discarded with booleans false), legacy derivation + load-time corrections, in `internal/state/session.go` and restore wiring in `internal/command/application.go`
- [X] T024 [US2] Wire transitions T1 (SetPlanMode→drafting), T2 (plan-ready requires ≥1 incomplete step → ready), T9 (supersede pending/interrupted predecessor) in `internal/orchestrator/plan.go` (maybeSignalPlanReady) and `internal/orchestrator/goal.go` (toggle wiring)
- [X] T025 [US2] Wire transitions T3–T6 (modal Proceed now/later/Keep planning → executing/pending/drafting; one-shot → pending) in `internal/tui/actions.go` (openPlanReadyModal) and `internal/orchestrator/engine.go` (plan-ready resolution)
- [X] T026 [US2] Wire T7 widened continuation (word-boundary phrase list, phase-guarded to pending/interrupted) in `internal/orchestrator/classify.go` and T8 step-progress execution detection in `internal/orchestrator/engine.go` (updatePlan: first step marked while phase ∈ {pending, interrupted, ready} and plan mode off → executing)
- [X] T027 [US2] Wire T10/T11 finalize stamping — executing + all-completed → `finished`; run end + incomplete steps → `interrupted` keyed on `TaskStats.StopCause` — in `internal/orchestrator/engine.go` finalize/defer (enriches T014's audit with C1 phase stamp at the same site)
- [X] T028 [US2] Add `/plan clear` → `discarded` (T12) in `internal/tui/actions.go` (handlePlanCommand) with engine support in `internal/orchestrator/plan.go`
- [X] T029 [US2] Key every affordance surface on phase per the [plan-lifecycle §4](contracts/plan-lifecycle.md) matrix: restored-notice wording in `internal/orchestrator/engine.go` (NewEngine), headless notice in `internal/command/root.go`, TUI resume notice in `internal/tui/actions.go`, progress line phase-keyed + "N/M · interrupted" wording + hide-on-terminal in `internal/tui/view.go`
- [X] T030 [US2] Record dispositions for audit rows A2/A3 in `specs/004-deepseek-agent-polish/audit.md` (`fixed:<task>` links to T022–T029)

**Checkpoint**: US1 + US2 independently green — the reported stale-hint defect is regression-proof

---

## Phase 5: User Story 3 — Agents That Know Their Mode (Priority: P2)

**Goal**: Capability awareness on the per-turn/mission surfaces + five precise enforcement fixes at the dispatch gate ([contracts/mode-capability.md](contracts/mode-capability.md))

**Independent Test**: quickstart §3 mode-scenario suite green — blocked-with-feedback strings, read-only MCP admission, `agents=0` first-denial close, mid-session flip, recurrence ≤2 (SC-002)

### Tests for User Story 3 (write first — must FAIL against current behavior)

- [X] T031 [US3] Write mode-scenario tests (a)–(g) per [mode-capability §7](contracts/mode-capability.md) — plan-mode block strings, read-only MCP admission, agents=0 first-call close, unknown-tool suggestion, subagent capability statement + out-of-set rejection, mid-session flip, non-interactive propose_changes labeling, recurrence ≤2 — in `internal/orchestrator/mode_scenario_test.go`

### Implementation for User Story 3

- [ ] T032 [P] [US3] Capture `annotations.readOnlyHint` from MCP `tools/list` and expose `readOnly` on the tool surface (absent → false) in `internal/mcpclient/manager.go`
- [ ] T033 [US3] Honor `readOnly` in `isMutation` at both sites (`internal/orchestrator/subagent.go`, `internal/orchestrator/inspection.go`) and update the plan-mode gate in `internal/orchestrator/engine.go`: admit read-only-annotated MCP tools; new block wording for unannotated ones per [mode-capability §3](contracts/mode-capability.md)
- [X] T034 [P] [US3] Add nearest-name suggestion (edit distance ≤3 over offered names, clause omitted when no candidate) to the unknown-tool error in `internal/orchestrator/registry.go`
- [X] T035 [US3] Return the hard-closed message on the FIRST `run_subagent` denial when the task budget is zero (`agents=0`) in `internal/orchestrator/subagent.go`
- [X] T036 [US3] Label non-interactive `propose_changes` verdicts honestly ("auto-approved by non-interactive policy — no human reviewed this proposal") in `internal/orchestrator/engine.go`
- [ ] T037 [US3] Add the denial-class clause (outside workspace / protected credential path / user declined / unread overwrite) to permission-failure feedback in `internal/orchestrator/engine.go` (gate reformatting), wording reviewed against `docs/security.md` (class only, no path leakage)
- [X] T038 [US3] Compose the subagent capability statement (sorted toolset line, boundary line, report-contract line per [data-model.md](data-model.md) §2.1) into mission briefs in `internal/orchestrator/subagent.go` (executeSubagent)
- [ ] T039 [US3] Align plan-block and task-brief wording to the §3 taxonomy (per-turn riders only — no prefix bytes) in `internal/orchestrator/plan.go` (planBlock) and `internal/orchestrator/classify.go` (buildBrief), preserving the 002 SC-004 ≤~50-token tail budget
- [X] T040 [US3] Record dispositions for audit rows A5.1–A5.6 in `specs/004-deepseek-agent-polish/audit.md` (incl. the A5.5 duplicate-read `Failed:false` disposition decision — fix or defer-with-rationale)

**Checkpoint**: US1–US3 independently green

---

## Phase 6: User Story 4 — Guidance Tuned to the Launch Model, Without Bloat (Priority: P2)

**Goal**: The single prefix-byte change of the feature — prompt dedup + priority rule + addendum sentence + tool-description pass, one commit, fixture-guarded ([contracts/prompt-architecture.md](contracts/prompt-architecture.md))

**Independent Test**: size-budget test proves net tokens ≤ baseline; stability fixtures green in the same commit; D9 after-benchmark (Phase 8) shows hit-rate ≥ baseline (SC-005/006)

### Tests for User Story 4 (write first)

- [X] T041 [US4] Write the prompt size-budget test — compose pre-change fixture prompt and post-change prompt, assert `EstimateTokens(new) <= EstimateTokens(old)` — in `internal/orchestrator/prompt_stability_test.go` (green pre-change by construction; becomes the growth gate)

### Implementation for User Story 4

- [ ] T042 [US4] **The bump commit (single commit, constitution II justification in the message)**: (a) prompt tightening per [prompt-architecture §2](contracts/prompt-architecture.md) — priority rule added to OPERATING CONTRACT, verify/final-answer dedup, re-read-prohibition consolidation — in `internal/orchestrator/prompt.go`; (b) DeepSeek addendum +1 completion-honesty sentence in `internal/gateway/model.go`; (c) tool-description quality pass (when-to-use / when-not / key argument leads) across base tools in `internal/workspace/registry.go` and synthetic tool defs in `internal/orchestrator/engine.go`; (d) ALL stability fixtures updated in this same commit: `internal/orchestrator/prompt_stability_test.go`, `internal/orchestrator/restart_determinism_test.go`, `internal/orchestrator/cachehit_guard_test.go`
- [X] T043 [P] [US4] Add family-resolution addendum test (DeepSeek profile carries the new sentence; generic/minimax/glm profiles do not) in `internal/gateway/gateway_test.go`
- [X] T044 [P] [US4] Document the Flash-for-subagents routing posture (`SubagentModelID`) as the recommended launch configuration in `docs/agent-design.md` (research D10c/B7)

**Checkpoint**: Exactly one prefix invalidation event has occurred, fixture-guarded; all prior suites still green

---

## Phase 7: User Story 5 — Tool Activity That Names Its Target (Priority: P3)

**Goal**: Close the two gaps against 003's tool-display contract — `apply_patch` target + filename-preserving truncation ([contracts/tool-row-target.md](contracts/tool-row-target.md))

**Independent Test**: quickstart §3 tool-row suite green — ordering (path before `+A −R`), derivation table, truncation table (SC-007)

### Tests for User Story 5 (write first — must FAIL against current behavior)

- [X] T045 [US5] Write tool-row tests — ordering assertion for write_file/edit_file/multi_edit/apply_patch (stripANSI index of path < index of counts, U+2212 minus), apply_patch derivation table (single-file, multi-file ` (+N more)`, deletion fallback, malformed → segment omitted), `truncateMiddle` table (filename survives at 12-cell floor, ellipsis glyph, rune safety), narrow-width counts-visible — in `internal/tui/tooldisplay_test.go`

### Implementation for User Story 5

- [X] T046 [US5] Derive `apply_patch` target from the patch argument (first `+++ ` path, strip `a/`/`b/`, skip `/dev/null` with `--- ` fallback, ` (+N more)` suffix) in `toolTarget` in `internal/tui/view.go`
- [X] T047 [US5] Add `truncateMiddle` helper (head + filename tail around one ellipsis glyph, ANSI/rune-safe) and use it for path-like targets in `renderTool` in `internal/tui/view.go` (after T046 — same file)

**Checkpoint**: All five stories independently green

---

## Phase 8: Polish & Cross-Cutting Closure

**Purpose**: The after-side of every improvement claim, audit/docs closure, signoff

- [X] T048 Run full static gates — `go fmt ./...`, `go vet ./...`, `go test ./... -count=1` — and confirm every scripted suite from quickstart §3 is green
- [ ] T049 Capture after-benchmarks (identical config to T003/T004, matched time window): `coding-session` + `fat-context`, N≥3 → `specs/004-deepseek-agent-polish/benchmarks/improved/`
- [ ] T050 Run `-compare` and evaluate merge gates — prefix stability ≥99% every run; steady-state hit rate ≥ baseline (variance ≤1.0 pp); median tokens and cost per completed task ≤ baseline; invalid + redundant calls −50%; ≥95% intervention-free — writing `specs/004-deepseek-agent-polish/benchmarks/comparison.json` and `comparison.md`
- [ ] T051 Execute the manual TUI walkthrough (6 scenarios, quickstart §5) and record outcomes in `specs/004-deepseek-agent-polish/benchmarks/walkthrough.md`
- [X] T052 Close `specs/004-deepseek-agent-polish/audit.md`: every FR-012 category has ≥1 finding with disposition or an explicit no-defect sweep note; verify SC-009 traceability — every model-specific adaptation cites a research Part B finding
- [X] T053 [P] Docs sync (D11): `docs/agent-design.md` updated with the plan lifecycle, hint semantics, capability statements, completion disclosure, and the Flash-worker routing posture. Remaining touch-ups to `README.md` / `docs/architecture.md` / `docs/prompt-caching.md` (DeepSeek wire notes, replay verdict) fold into the live-phase docs pass once the benchmarks/probe run.
- [ ] T054 Write `specs/004-deepseek-agent-polish/benchmarks/signoff.md` with whole-picture gate outcomes (hit rate AND totals AND cost, constitution VI) and the SC-001..009 checklist verdicts

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none — start immediately
- **Foundational (Phase 2)**: after Setup. **T003–T005 BLOCK all behavior-changing tasks in Phases 3–7** (D9 measurement-first). T006 is [P] hygiene
- **User Stories (Phases 3–7)**: all start after Phase 2. Priority order US1 → US2 → US3 → US4 → US5 for sequential delivery; see coordination note below for parallel staffing
- **Polish (Phase 8)**: after every story that ships; T049/T050 strictly after ALL behavior changes (including T042's bump)

### Story-Level Notes

- **US1 → US2 touchpoint**: T014 (completion audit) and T027 (phase stamping) edit the same `finalize` site — T014 ships without phase stamping (uses `hasIncompletePlan`); T027 enriches it. Either order works; same-file coordination required
- **T017 is conditional** on the T005 verdict (`replay-required`); skip with a note in `replay-probe.md` otherwise
- **T042 is the only prefix-byte commit** — everything in Phases 3–5, 7 is prefix-inert by design; any task discovered to touch prompt/tool-schema bytes must fold into T042
- **Same-file coordination for parallel staffing**: `internal/orchestrator/engine.go` is touched by US1 (T012/T014/T016), US2 (T025–T027), US3 (T033/T036/T037), US4 (T042c) — parallel story work on engine.go needs rebase discipline; sequential story order avoids it entirely

### Within Each Story

Tests first (red) → types/state → engine wiring → UI surfaces → audit dispositions. Verify red before implementing; commit after each task or logical group.

---

## Parallel Examples

```text
# US1 test wave (after Phase 2):
T007 terminal_state_test.go | T008 sweep_test.go | T009 completion_audit_test.go | T010 salvage_test.go | T011 gateway_test.go

# US1 implementation split (different layers):
T013 (internal/tui) ∥ T015+T016 (internal/gateway) — T012/T014 sequential in engine.go

# US3 independent files:
T032 (mcpclient/manager.go) ∥ T034 (orchestrator/registry.go) — while T033/T035–T037 queue on engine.go/subagent.go

# Phase 8:
T053 (docs) ∥ T051 (walkthrough) after T048–T050
```

---

## Implementation Strategy

### MVP First (US1 only)

1. Phases 1–2 (Setup + baselines + probe) — non-negotiable first
2. Phase 3 (US1): terminal states + wire robustness + completion honesty
3. **STOP and VALIDATE**: US1 suites green; a spot benchmark rerun shows terminal-state and invalid-call movement
4. US1 alone already delivers the launch-critical reliability layer

### Incremental Delivery

Each story lands independently green, in priority order: US1 (reliability) → US2 (plan truthfulness — the reported defect) → US3 (mode awareness) → US4 (the one prefix bump) → US5 (display polish) → Phase 8 closure. Stopping after any story leaves the product strictly better and fully verified; Phase 8's after-benchmarks + signoff convert the whole feature into constitution-X-grade evidence.

### Parallel Team Strategy

Two-developer split that avoids the engine.go collision: Developer A takes US1+US2 (engine-heavy, sequential); Developer B takes US5 → US3's non-engine tasks (T031/T032/T034/T038 prep) → US4's T041/T043/T044; T042 (the bump) and Phase 8 land last, single-owner.
