# Tasks: Ultimate Consolidation — One Coherent, Provably Stable Agent

**Input**: Design documents from `specs/010-ultimate-consolidation/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md)

**Tests**: Included — the contracts demand them (the fault suite, audits, and
enforcement checks ARE the feature), and the constitution (VI/X) makes
measurement + prefix-stability tasks mandatory for this cache-affecting change.

**Organization**: Grouped by user story (US1–US5). Single repo; all paths
relative to `F:\MuhiyaCode Agent Go`.

## Executor Notes (read before starting)

1. **Never-broken rule**: `go build ./... ; go vet ./... ; go test ./... -count=1`
   green after EVERY task or logical group — this rework lands as ordered green
   steps, not a big bang. Commit per group.
2. **Behavior preservation is the contract**: pinned denial/block/notice texts,
   the approval always-pause, resume notices, and DeepSeek wire captures stay
   byte-for-byte (UL-3/UL-11, IS-11, FR-017/FR-019). Renaming tests is allowed;
   weakening what they assert is not.
3. **One prompt epoch**: every static model-facing text change in this feature
   lands together; the Prefix-bytes golden's single update IS the epoch (IS-9,
   FR-018). Re-baseline `internal/orchestrator/prompt_budget_test.go` once,
   with a justification comment.
4. **Ratchet pattern**: the `internal/arch` size/layering tests land EARLY with
   today's violations explicitly allowlisted, and tasks later EMPTY the
   allowlists — enforcement is never disabled, only satisfied.
5. **Limits shape work, never destroy it** (standing directive): no hard
   token/turn failure ceilings; every gate stays bounded-then-degrade.
6. **DeepSeek is the only live provider** (budget window respected; record
   "budget-blocked" rather than fabricate); MiniMax stays simulated-only.
7. Contract IDs: UL-x = [unified-lifecycle](contracts/unified-lifecycle.md),
   MS-x = [module-structure](contracts/module-structure.md),
   IS-x = [instruction-system](contracts/instruction-system.md),
   FI-x = [fault-injection](contracts/fault-injection.md),
   WI-x = [wiring-inventory](contracts/wiring-inventory.md).
8. Line numbers cited from research.md's inventories — re-grep before editing;
   the R1–R5 agent reports hold the full evidence maps.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US5 (user-story phases only)

---

## Phase 1: Setup (BEFORE captures — Principle X)

**Purpose**: Pin the pre-rework baselines every later comparison needs.

- [X] T001 Verify the green baseline (`go build ./... ; go vet ./... ; go test ./... -count=1`; `gofmt -l internal cmd benchmarks` empty) and create `specs/010-ultimate-consolidation/results.md` with the pinned run-conditions table (model IDs, gateway build, effort, dates; live vs SIMULATED columns) per SC-008
- [X] T002 [P] Capture BEFORE analysis baselines into `specs/010-ultimate-consolidation/before/`: `deadcode ./...` and `deadcode -test ./...` outputs, the non-test file-size list (>400 lines), and the current internal import graph (`go list -f '{{.ImportPath}} {{.Imports}}' ./internal/...`)
- [X] T003 [P] Record the BEFORE behavior baselines in `specs/010-ultimate-consolidation/results.md`: the steady-state DeepSeek cache-hit figure (from the latest live session/benchmark records), the prompt-budget baseline value, and confirmation the 009 DeepSeek conformance goldens verify against the current build (`DEEPSEEK_GOLDEN_DIR` run) — these are the SC-006/FR-019 comparison anchors

**Checkpoint**: Baselines pinned — the rework may begin.

---

## Phase 2: Foundational (shared types + the safety net + the ratchets)

**Purpose**: The type layer every story builds on, plus the guards that protect
the rework itself. **Blocks all user stories.**

- [X] T004 Add the `contract.LifecycleState` type with the 11 state constants (`direct, research, planning, awaiting-approval, pending, implementing, validating, interrupted, finished, superseded, discarded`) and the additive `State LifecycleState` (json `state,omitempty`) field on `PlanStateSnapshot` in `internal/contract/types.go` (data-model §1–2; UL-6)
- [X] T005 [P] Move the MiniMax-M3 name classifier to `internal/contract` (new `contract.IsMiniMaxM3Name`, moved from `internal/gateway/model.go:118-129`), update `internal/state/config.go:201` and gateway callers — erasing the `state → gateway` lateral edge with byte-identical behavior (MS-6; existing defaults tests green)
- [X] T006 [P] Create the `internal/arch` leaf package with `size_test.go`: walk non-test `.go` files under `internal/`, `cmd/`, `benchmarks/`; fail any file over 800 lines EXCEPT an explicit allowlist map seeded with today's offenders (engine.go, tui model/view/actions, application.go, delegationbench/main.go, pipeline.go, manager.go) each with a "shrinks in US4" note (MS-1; ratchet)
- [X] T007 [P] Add `internal/arch/layering_test.go`: parse every internal package's imports (stdlib `go/parser`, ImportsOnly); assert each package's internal imports ⊆ the allow-map (contract:{} · gateway/workspace/state:{contract} · mcpclient:{contract,state} · orchestrator:{contract,gateway} · tui:{contract,gateway,orchestrator} · command:{*}) plus a transitive cycle check (MS-5/MS-7; the state→gateway entry is NOT in the map because T005 removed the edge)
- [X] T008 Build the recovery-invariant safety net in `internal/orchestrator/faultinjection_test.go`: the `faultCase` table type and `assertRecoveryInvariant(t, engine, stats, runErr, wantOutcome)` helper — exactly-one of {guided-success, recorded-degradation, user-decision}, turns below the hard ceiling, no identical denial text >3× in history, runErr nil-or-canceled — plus a `hasAnyDegradation` accessor on the current pipeline state (FI-1..4; R5 design; protects every later phase)
- [X] T009 Seed the fault table with the seven pinned 008/009 live incidents as rows (content-bar deadlock, exit-plan desync, /plan-clear deadlock, stale-approve leak, terminal revival, depth loss, premature finish) so the invariant guards the unification while it happens (FI-13; reuse the existing pipeline_polish/pipeline_gates scenarios' scripted setups)

**Checkpoint**: Types in place, enforcement ratchets armed, safety net live.

---

## Phase 3: User Story 1 — One lifecycle, one truth (Priority: P1) 🎯 MVP

**Goal**: One 11-state `Lifecycle` replaces planMode/pendingPlan/planPhase/
pipeline; desyncs impossible by construction; old sidecars migrate through one
bounded loader.

**Independent Test**: quickstart §1 — every lifecycle/resume/gate suite green
post-unification; the UL-8 grep proves legacy flags exist only in the
compat layer; fixture sidecars from every era restore correctly.

### Implementation for User Story 1

- [X] T010 [US1] Create `internal/orchestrator/lifecycle.go`: the `Lifecycle` type (State, Depth, Why, gate facts, Degradations, planBarStrikes) with the predicate API (`IsReadOnly`≡`BlocksMutation`, `InvitesProceed`, `IsApprovalPause`, `IsPipelineResumable`, `IsTerminal`, `IsActive`, `HasAnyDegradation`) and the single legal-edge transition table incl. the one terminal-sticky rule (data-model §1; UL-1/UL-2/UL-4; merge the semantics of `PipelineState.Transition`, `SetPlanPhase`, `SetPlanMode`, `DiscardPlan`)
- [X] T011 [US1] Add table-driven unit tests in `internal/orchestrator/lifecycle_test.go`: every legal and illegal edge, every predicate's exact state set, terminal stickiness, the plan⇄goal exclusion guard (UL-4/UL-13; port assertion intent from pipeline_test.go + plan_lifecycle_test.go)
- [X] T012 [US1] Write the single sidecar migration loader in `internal/state/session.go` (or a new `internal/state/lifecycle_migrate.go`): `state` field wins; else `pipeline_phase`+`pipeline_depth` map via the restore table; else legacy planMode/pendingPlan/phase derivation — preserving the two truthfulness corrections verbatim (pending-but-complete→finished; executing-no-pipeline→interrupted/finished) and the malformed-is-absent rule (UL-6/UL-7; data-model §2)
- [X] T013 [US1] Add migration-loader tests in `internal/state/lifecycle_migrate_test.go`: fixture sidecars from each era (P2 flags-only, 004 phase, 009 pipeline_phase+depth, new state-field) each restore to the correct unified state; corrections and malformed handling pinned (UL-7/UL-9)
- [X] T014 [US1] Rewire the ENGINE onto `Lifecycle`: replace the four fields at `internal/orchestrator/engine.go:145-150`; collapse the two mutation gates (~1814-1841) into ONE keyed on `BlocksMutation()` while preserving BOTH escalation counters and every pinned block text byte-for-byte; rewire `exit_plan_mode`, the task-entry routing (~742-760) into one state switch, and the run-loop inline writes (UL-1/UL-3; R1 consumer inventory is the checklist)
- [X] T015 [US1] Rewire plan/goal/pipeline method surfaces onto `Lifecycle` in `internal/orchestrator/{plan.go,goal.go,pipeline.go}`: setters become thin transition calls; `stampPlanCompletionPhase` becomes the simple implementing/validating→finished|interrupted stamp; delete `DrivenPlanPhase`, `persistedPipelinePhase`, `restoredPipelineState`, and merge the dual restore-notice builders into one over the unified states (UL-1; MS-10 notice merge)
- [X] T016 [US1] Isolate the compat boundary: move ALL legacy-shape handling (NewEngine restore derivation → the T012 loader call) into `internal/orchestrator/restore.go` + the state loader, so legacy identifiers appear nowhere else (UL-8; MS-3)
- [X] T017 [US1] Rewire the TUI onto predicates in `internal/tui/{actions.go,view.go,model.go}`: the plan-ready modal's dual pipeline/legacy branch (~903-947) becomes ONE path; `/plan` handlers, mode line, progress line, and restore notices read predicates only (UL-2/UL-5; TUI modal tests keep their assertions)
- [X] T018 [US1] Add the UL-8 grep-proof test in `internal/arch/compat_boundary_test.go`: outside `restore.go` + the migration loader (+ their tests), zero production references to `planMode`, `pendingPlan`, legacy `PlanPhase` flag fields, or `PipelinePhase` (SC-001, structurally checkable)
- [X] T019 [US1] Green the full pinned surface: run and mechanically update (renames only, assertion intent unchanged) plan_lifecycle, pipeline_test, pipeline_gates, pipeline_polish, pipeline_fastpath (prefix byte-stability across all 11 states), plan_mode_phase3, restart_determinism, completion_audit, cachehit_guard, and the TUI suites (UL-9/UL-11/UL-12)

**Checkpoint**: One truth. The desync bug class is structurally dead.

---

## Phase 4: User Story 2 — Stability is a testable property (Priority: P2)

**Goal**: The chaos catalog runs the whole agent through every fault class and
asserts the three-outcome invariant on all of them.

**Independent Test**: quickstart §2 — 100% catalog pass; the mutation-guard
check fails the suite when a bounded-recovery constant is flipped.

### Implementation for User Story 2

- [X] T020 [P] [US2] Add malformed-argument rows to `internal/orchestrator/faultinjection_test.go`: truncated JSON, wrong primitive type, missing required field, bad enum — through the main loop AND the subagent scope, covering the untested `validateCallArgs` branches → guided-success (FI-5)
- [X] T021 [P] [US2] Add budget-exhaustion rows: per-phase subagent allowance exhausted in research and validate → recorded-degradation; turn-governor final-step and failure-terminator paths → recorded-degradation/user-decision (FI-6)
- [X] T022 [P] [US2] Add the remaining seed rows: interrupt/restart at every lifecycle state reaching one of the three outcomes (FI-7); oversized plan 13+ steps → guided-success covering the untested >12 rejection (FI-8); invalid regex through a full Run → guided-success (FI-9); blocked capabilities incl. unknown tool and dead MCP (FI-10); empty/failed subagent + wrap-up preservation (FI-11); trailing-intent bounded retry (FI-12)
- [X] T023 [US2] Add the mutation-guard self-test: a test-only hook flips a bounded-recovery constant (e.g. the plan-bar strike limit) and asserts the suite FAILS — proving the invariant bites (FI-14); document the add-a-row template so new incidents inherit the assertions (FI-15)

**Checkpoint**: Stability is now a red-bar property, not a hope.

---

## Phase 5: User Story 3 — One coherent instruction voice (Priority: P3)

**Goal**: Every model-facing text in one audited registry; rules taught before
enforcement; zero contradictions; the Prefix golden is the epoch.

**Independent Test**: quickstart §3 — audit suite green; golden dump reviewed;
exactly one Prefix-golden update in the release diff.

### Implementation for User Story 3

- [X] T024 [US3] Create `internal/instructions` (imports only `contract`): the `Text` registry type (ID, Audience, Cache, Body, StatesRule/EnforcesRule, Example, MentionsTools, AllowlistCtx) and the worked-`examples.go` constants (data-model §3; IS-1/IS-2)
- [X] T025 [US3] Move the STATIC prefix texts into the package as pure composers — system-prompt sections from `internal/orchestrator/prompt.go`, all tool + property descriptions from `internal/orchestrator/{engine.go,memory.go}`, `internal/workspace/registry.go:25-37`, `internal/gateway/web.go`, and the gateway per-family addenda references — byte-preserving (the R4 §1A/1B inventory is the checklist; IS-1/IS-11)
- [X] T026 [US3] Move the DYNAMIC texts: subagent system/handoff/capability templates (`internal/orchestrator/subagent.go`), phase preludes + notices (`internal/orchestrator/pipeline.go` §1D), mode blocks (`plan.go`/`goal.go`/`classify.go`), ALL gate/block/denial/guard strings (engine.go §1F), and model-facing workspace errors (`internal/workspace/files.go`) — each registered with rule/example/tool metadata (IS-1/IS-2)
- [X] T027 [US3] Fix the five ranked incoherences as registry-backed changes: reconcile the `general` subagent schema with its executor (trim or route the synthetic tools — IS-6); state the read-only shell allowlist IN planBlock and the read-only subagent instructions before any violation (IS-4); single canonical plan-step example + report-format field list referenced by ID from all sites (IS-5); align the `write_file` permission sentence between tool description and system prompt (IS-5); add the validator-passing plan-note worked example with `Verification:`/`Risks:` (IS-7)
- [X] T028 [US3] Build the audit suite in `internal/instructions/audit_test.go`: stated-in-advance (IS-4), contradiction/canonical-copy (IS-5), capability-reference wired from real allowlists (IS-6), worked-example-passes-its-validator (IS-7), gate-message quality template fields — all/shape/budget/alternative — applied to the terse gates (IS-8), and the no-dynamic-sentinels-in-Prefix assertion (IS-10)
- [X] T029 [US3] Build the goldens in `internal/instructions/dump_test.go`: the full instruction-system dump (fixed inputs, grouped by Audience/Cache) and the SEPARATE Prefix-bytes golden; land ALL static-text deltas from this feature as that golden's single update; re-baseline `internal/orchestrator/prompt_budget_test.go` once with justification (IS-9; FR-018; prefix-stability + marshal-determinism suites green)

**Checkpoint**: The agent is instructed by one voice, provably consistent.

---

## Phase 6: User Story 4 — Clean structure, nothing dead, nothing duplicated (Priority: P4)

**Goal**: The splits land, the ratchet allowlists empty, dead code and
duplicate mechanisms are gone with ledger proof.

**Independent Test**: quickstart §4 — arch tests green with EMPTY allowlists;
both deadcode invocations report nothing; ledger complete.

### Implementation for User Story 4

- [X] T030 [US4] Split `internal/orchestrator/engine.go` into responsibility files per the R2 map (engine/turnloop/dispatch/gates/validate/toolhandlers/definitions/usage/maintenance/contextreport/planrender + the T016 restore.go) — pure intra-package moves, `git log --follow`-clean, no body rewrites (MS-2/MS-4)
- [X] T031 [P] [US4] Split the TUI trio `internal/tui/{model.go,view.go,actions.go}` into update/keys/ingest/render_header/render_transcript/render_tool/render_modal/hittest/slash/modals/format per the R2 map (MS-2)
- [X] T032 [P] [US4] Split `internal/command/application.go` (→ runtime_build/mcp_actions/actions/skills) and `internal/mcpclient/manager.go` (→ surface/tool/schema); split the lifecycle-adjacent remains of `internal/orchestrator/pipeline.go` (phase runners / plan bar) under budget; move the read-only-shell classifier out of `internal/orchestrator/inspection.go` into its own file (MS-2/MS-4)
- [X] T033 [US4] Empty the `internal/arch` size-test allowlist (delete every seeded entry; the test now enforces ≤800 with zero exceptions) and split `benchmarks/delegationbench/main.go` under budget (MS-1 ratchet closed)
- [X] T034 [P] [US4] Execute the safe deletions with `RL-###` ledger rows in `specs/010-ultimate-consolidation/removal-ledger.md`: the 6 truly-dead functions (`Application.Paths`, `contract.CreditsToUSD`, `Manager.uniqueName`, `assembleRequest`, `state.CheckpointPath`, `Workspace.Root`) — each with its zero-reference proof (MS-8/MS-11)
- [X] T035 [US4] Resolve the test-only survivors WITH their tests (ledger Class=test-only): delete the orphaned `internal/tui/transcript.go` `TranscriptWindow` type + its test; migrate the 3 gateway tests off `ParseOpenAIStream`/`NewStreamAccumulator` onto the streaming `ConsumeLine` path then delete the island; resolve the remaining R3 §2B entries (keep deliberate `*ForTest` shims) (MS-8)
- [X] T036 [P] [US4] Merge the duplicate-mission clusters (ledger Class=duplicate-merged): one foundation token formatter replacing `humanTokens`/`formatTokens`; one `truncateEllipsis` primitive + `digest` wrapper absorbing `truncate`/`oneLineGoal`/byte-unsafe `truncateLine`; ONE uniseg width family (rewrite `truncateMiddle`/`truncateLeft`, unify `fitLine`/`oneLine`/`oneLineEllipsis`, drop the go-runewidth import — restoring width.go's stated invariant); resolve the two dead alignment helpers together (MS-10)
- [X] T037 [US4] Resolve write-only/injected state (ledger rows): remove `AgentEvent.CallID/Turns/ToolCalls` (no consumer; keep `Handoff` with its benchmark consumer noted); wire `buildinfo.Commit`/`Date` into `--version` output OR remove them with their ldflags lines in `.goreleaser.yaml` + `Makefile` (MS-9)
- [X] T038 [US4] Final dead-code proof: both `deadcode` invocations empty; `go vet` + `gofmt -l` clean; ledger complete with proof-or-migration-note on every row (MS-8/MS-11; SC-004) — NOTE: `deadcode -test ./...` is 0 (nothing genuinely dead anywhere, incl. tests); `deadcode ./...` (no `-test`) went 34→4, the 4 remainder being cross-package test-fixture exports (`instructions.All`, `workspace.NewMemoryTrustStore`/`IsTrusted`/`Trust`) that CANNOT move into a `_test.go` file without breaking another package's test build — see removal-ledger.md's "Structurally irreducible" section for the full reasoning (RL-012, RL-025)

**Checkpoint**: Structure enforced by tests; nothing dead; nothing duplicated.

---

## Phase 7: User Story 5 — Everything advertised is wired (Priority: P5)

**Goal**: The full advertised surface is enumerated, verified, and guarded
bidirectionally; ghosts are wired or removed.

**Independent Test**: quickstart §5 — guard tests green; zero unresolved
inventory entries.

### Implementation for User Story 5

- [X] T039 [US5] Author the wiring inventory at `specs/010-ultimate-consolidation/wiring-inventory-data.md`: every registry tool, synthetic tool, slash command + alias, keybinding, Settings field, AgentEvent kind, and Callbacks member with Advertised behavior + Verified-by + Status∈{wired,removed}; conditional surfaces name their gate and verifying interface (WI-1/WI-2/WI-4)
- [X] T040 [US5] Build the guard tests (`internal/tui/wiring_inventory_test.go` + the `Engine.SessionToolNames` orchestrator surface feeder): enumerate the LIVE surface at runtime and fail on any divergence from the inventory in either direction; no third status parses (WI-3)
- [X] T041 [US5] Resolve the ghosts: `Settings.UI.Density` removed — field, default, validator clause, and `uiDensity` `SetConfig` case all deleted (no clean rendering effect existed to wire; RL-026) (WI-5); added the exercising test for the grep invalid-pattern guidance path (`TestGrepInvalidPatternGuidance`) and closed the UNVERIFIED per-command/per-keybinding/per-AgentEvent-kind/per-Callbacks-member cells with new exercising tests (WI-7)
- [X] T042 [US5] Tool-description accuracy pass: verified every registered description against real behavior and limits (read-before-overwrite requirements, exit_plan_mode's outside-plan-mode no-op, schema limits); no drift found — added missing behavior tests for list_files/glob/write_file/git_status/git_diff where only schema-golden coverage existed (WI-4/FR-016)

**Checkpoint**: Zero ghost features; drift now fails CI the moment it appears.

---

## Phase 8: Verification & Measurement (constitutional — MANDATORY)

- [X] T043 Re-verify wire preservation: DeepSeek conformance goldens byte-identical (`DEEPSEEK_GOLDEN_DIR` run); prefix-stability/marshal-determinism suites green with exactly the ONE sanctioned epoch delta vs the T003 baseline (FR-019; IS-11)
- [ ] T044 Live measurement legs (budget-window permitting; else record "budget-blocked"): delegation benchmark plan-only execution 8/8; steady-state cache hit within 1 point of the T003 BEFORE figure; cold vs steady reported separately in `specs/010-ultimate-consolidation/results.md` (SC-006/SC-008)
- [ ] T045 The live acceptance session (SC-007): rebuild the binary, run a fresh real end-to-end session (research → plan → approve → implement → validate) on a non-trivial workspace; PASS = zero harness-caused error messages; any snag becomes a new FI catalog row BEFORE release (quickstart §7)

---

## Phase 9: Polish & Cross-Cutting

- [X] T046 [P] Update `docs/agent-design.md` (unified lifecycle, instruction system, fault suite, arch enforcement) and add the `CHANGELOG.md` entry; note the removal ledger location
- [X] T047 [P] Full final sweep: `gofmt -l` empty, `go vet ./...`, `go test ./... -count=1`, `go build -o muhiyacode ./cmd/muhiyacode` with `--version` verified; execute the full [quickstart.md](quickstart.md) §1–7 checklist and record deviations in results.md

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** → everything.
- **Foundational (Phase 2)** blocks all stories; T005 must precede T007's
  allow-map (the state→gateway entry is absent by design); T008/T009 (safety
  net) SHOULD land before US1 begins.
- **US1 (Phase 3)**: T010/T011 → T012/T013 → T014 → T015 → T016 → T017 →
  T018/T019. The MVP.
- **US2 (Phase 4)**: after US1 (rows reference the unified lifecycle); T020–T022
  parallel, T023 last.
- **US3 (Phase 5)**: after US1 (texts extracted from post-unification code);
  T024 → T025/T026 → T027 → T028 → T029 (the single epoch lands here).
- **US4 (Phase 6)**: after US1 + US3 (splits move the post-extraction code);
  T030 before T033; T034/T036 parallel to splits; T038 last.
- **US5 (Phase 7)**: after US4 (the surface is final); T039 → T040 → T041/T042.
- **Verification (Phase 8)**: after all stories; T045 gates the release.
- **Polish (Phase 9)**: last.

### Critical path

T001 → T004 → T010 → T012 → T014 → T015 → T016 → T017 → T019 → (US3: T024 →
T025/T026 → T029) → (US4: T030 → T033 → T038) → T039/T040 → T043 → T044 →
T045 → T047

### Parallel opportunities

```text
# Setup:        T002 ∥ T003 (after T001)
# Foundational: T005 ∥ T006 ∥ T008 (T007 after T005; T009 after T008)
# US2 rows:     T020 ∥ T021 ∥ T022
# US4:          T031 ∥ T032 alongside T030's review; T034 ∥ T036 ∥ T037
# Polish:       T046 ∥ T047
```

## Implementation Strategy

1. **MVP = Phases 1+2+3 (T001–T019)**: the unified lifecycle with the safety
   net armed and every pinned behavior green — the bug class that caused every
   recent live failure dies here.
2. **Incremental**: US2 (the invariant becomes total), US3 (the voice), US4
   (the structure), US5 (the surface) — each independently verifiable, each
   landing green.
3. **The release gate is T045**: a real session with zero harness-caused
   errors. Nothing ships on green tests alone.

## Notes

- [P] = different files, no dependency on an incomplete task.
- Out of scope (do NOT touch): gateway repo behavior, wire protocol,
  effort/classAgents numbers, the four subagent kinds, MiniMax live calls,
  any hard token/turn failure ceiling.
