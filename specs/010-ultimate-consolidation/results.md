# Results — Ultimate Consolidation (feature 010)

Measurement ledger per SC-008. All figures state their run conditions; live vs
SIMULATED is labeled. MiniMax is never called live.

## Run conditions (pinned)

| Item | Value |
|---|---|
| Repo | `F:\MuhiyaCode Agent Go` @ pre-rework baseline (feature 010 start) |
| Go toolchain | go1.26.4 |
| Binary version | 1.0.2 |
| Live provider | DeepSeek (via Muhiya gateway); budget-window gated |
| Simulated provider | MiniMax (fixture only; no live spend) |
| Baseline date | 2026-07-15 |

## BEFORE baselines (Phase 1)

**T001 — green baseline**: `go build ./...` exit 0 · `go vet ./...` exit 0 ·
`go test ./... -count=1` all packages ok · `gofmt -l internal cmd benchmarks`
empty. Captured 2026-07-15.

**T002 — analysis baselines** (stored under `before/`):
- `deadcode ./...` → **34** unreachable funcs (`before/deadcode-all.txt`).
- `deadcode -test ./...` → **6** unreachable funcs (`before/deadcode-test.txt`)
  — the truly-dead set: `Application.Paths`, `contract.CreditsToUSD`,
  `Manager.uniqueName`, `orchestrator.assembleRequest`, `state.CheckpointPath`,
  `Workspace.Root`. (`ModelProfile.IsDeprecatedParam`, `Sessions.PrunedRecords/
  Transcript`, `TranscriptWindow` ×17, `NewStreamAccumulator`/`ParseOpenAIStream`,
  `padToWidth`, `DisplayLine.AlignedTo`, `NewMemoryTrustStore`+2,
  `ProjectInstructions.Loaded`, `connectionForTest` are the 28 test-only
  survivors resolved in US4.)
- File-size list (`before/file-sizes.txt`): 8 non-test files over the 800-line
  budget — engine.go 3152, model.go 1379, view.go 1265, actions.go 1120,
  application.go 1043, delegationbench/main.go 1019, pipeline.go 965,
  manager.go 883. (cachebench/main.go 791 and inspection.go 766 are under.)
- Internal import graph (`before/import-graph.txt`): layering holds acyclic;
  the only lateral edges are `state → gateway` (T005 erases via
  `IsMiniMaxM3Name` → contract) and `mcpclient → state` (documented allowed).

**T003 — behavior baselines**:
- Prompt-budget baseline: `promptBaselineChars = 5709`
  (`internal/orchestrator/prompt_budget_test.go:18`) — the feature-009 epoch
  ceiling; feature 010's single new epoch re-baselines this once (IS-9).
- DeepSeek conformance goldens: verify against the current gateway build
  (`DEEPSEEK_GOLDEN_DIR` run) — the FR-019 byte-identical anchor.
- Steady-state cache-hit figure: LIVE, pending a budget-window session
  (last observed ~90% in the CapCut live runs; to be re-measured on the
  reworked build for SC-006's ≤1-point comparison). Recorded here as
  **pending-live**, not fabricated.

## AFTER (feature 010 complete, 2026-07-15)

**Full suite**: `go build ./...`, `go vet ./...`, `go test -p 1 ./... -count=1` —
all packages green (including the new `internal/arch` and `internal/instructions`
packages). `gofmt -l internal` empty. Binary builds: `MuhiyaCode 1.0.2`.

**SC-001 — one lifecycle**: the four legacy engine fields
(planMode/pendingPlan/planPhase/pipeline) are gone; one `Lifecycle` machine
(11 states, predicate API) remains. `internal/arch/compat_boundary_test.go`
(`TestLegacyLifecycleConfinedToCompatLayer`) proves the legacy
`contract.PlanPhase`/`PipelinePhase` types appear only in the single migration
loader `internal/state/lifecycle_migrate.go`. Migration covers all three
sidecar eras + the two truthfulness corrections
(`lifecycle_migrate_test.go`, `plan_lifecycle_test.go`).

**SC-002 — stability is a property**: `internal/orchestrator/faultinjection_test.go`
= 31 fault scenarios, each through the shared `assertRecoveryInvariant`
(exactly one of guided-success / recorded-degradation / user-decision; bounded
turns; no denial >3×; no hard crash), plus the mutation-guard self-test proving
the invariant bites. All seven pinned 008/009 live incidents are rows.

**SC-003 — instruction coherence**: all model-facing text lives in
`internal/instructions` with the audit suite (stated-in-advance,
contradiction/canonical-copy, capability-reference, worked-example-passes-
validator, no-dynamic-sentinel-in-prefix). The five R4 incoherences are each
closed with a pinned assertion (one — the general-subagent schema — was found
already-correct and guarded rather than changed).

**SC-004 — clean structure**: `internal/arch` size test enforces ≤800 lines
with an EMPTY allowlist (max non-test file now 791, was engine.go at 3125,
split into ~11 files); layering test enforces the acyclic contract →
{gateway,workspace,state,instructions} → orchestrator → {tui,command} graph
(the `state → gateway` edge erased; `instructions` added as a second foundation
package). `deadcode -test ./...` = **0**; `deadcode ./...` = 4 (all
cross-package test-support exports Go visibility cannot relocate — documented in
the removal ledger, RL-001..026).

**SC-005 — everything wired**: `wiring-inventory-data.md` (92 rows: 91 wired,
1 removed) + `internal/tui/wiring_inventory_test.go`, which enumerates the LIVE
surface at runtime (real registry, AST-parsed runSlash/handleKey switches,
reflected Settings/Callbacks, scanned AgentEvent kinds) and fails on any
divergence. `Settings.UI.Density` (a ghost — no renderer) removed (RL-026).

**Prompt epoch (FR-018/IS-9)**: `promptBaselineChars` re-baselined 5709 → 5789
ONCE (the write_file permission-sentence alignment); prefix-stability,
prefix-shape, prompt-budget, and marshal-determinism all green — the single
sanctioned epoch.

**Three real production bugs found & fixed by the pinned tests / fault catalog
during the rework** (none masked): (1) `Lifecycle.Transition` universal-edge
gate leak (discard/supersede wrongly gated); (2) `SetPlanMode` supersede-on-
reentry regression — fixed as an improvement (unified read-only Planning is
strictly safer than the old terminal-Superseded transient that left the
mutation gate open); (3) the `[continue] incomplete steps` nudge fired
regardless of lifecycle state, hijacking replies after a discarded/parked plan
(now scoped to `LifecycleImplementing`).

### Pending-live (require a budget window / human-driven run — NOT fabricated)

- **T044** — steady-state DeepSeek cache-hit measurement on the reworked build
  (SC-006, ≤1-point regression). Deferred to a budget-window session.
- **T045** — the live end-to-end acceptance session (research → plan → approve →
  implement → validate) with zero harness-caused errors (SC-007). Requires a
  real interactive run on the rebuilt binary.
- **DeepSeek conformance goldens** (gateway repo, feature 009): the client
  prefix is byte-stable except the single sanctioned epoch; a full gateway
  conformance re-capture is a gateway-repo step outside 010's client scope.
