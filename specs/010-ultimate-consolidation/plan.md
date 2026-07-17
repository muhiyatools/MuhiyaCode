# Implementation Plan: Ultimate Consolidation — One Coherent, Provably Stable Agent

**Branch**: `010-ultimate-consolidation` | **Date**: 2026-07-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/010-ultimate-consolidation/spec.md`

## Summary

Consolidate MuhiyaCode from nine organically layered features into ONE designed
system, in four moves: (1) **unify the dual lifecycle** — the legacy plan-mode
flags (`planMode`/`pendingPlan`/`planPhase`) and the feature-009 pipeline phases
merge into a single task-lifecycle state machine with one owner, one persisted
shape, and a bounded compatibility layer for old sidecars, making desyncs
impossible by construction; (2) **restructure modules** — the 3,000-line engine
and the oversized TUI files split into single-responsibility units under an
enforced size budget, with the package layering (contract → gateway/workspace/
state → orchestrator → tui/command) proven acyclic by a test; (3) **one
instruction voice** — every model-facing text (prompt sections, tool
descriptions, preludes, gates, denials) moves into a single audited instruction
system with automated contradiction/capability/worked-example checks; and (4)
**stability as a test** — a table-driven fault-injection suite drives the whole
agent through the chaos catalog and asserts the three-outcome recovery
invariant (guided success / recorded degradation / user decision) on every
scenario. Dead code and duplicate mechanisms are removed with tool-verified
zero-reference proof; every advertised capability is verified wired or removed.
Behavior is preserved where pinned: the full existing suite stays green
throughout, DeepSeek wire captures stay byte-identical, all static prompt
changes land as ONE epoch, and the release finishes with a live end-to-end
session showing zero harness-caused errors.

## Technical Context

**Language/Version**: Go 1.25+ — single repo `github.com/muhiya/muhiyacode`
(`F:\MuhiyaCode Agent Go`). The gateway repo is out of scope except conformance
re-verification against existing captures.

**Primary Dependencies**: Existing only (Bubble Tea v2, lipgloss v2, stdlib).
No new runtime dependencies. Dev-time analysis tooling (dead-code detection) is
a Phase-0 decision (R3) — preferred: `golang.org/x/tools/cmd/deadcode` run via
`go run` (no module dependency added).

**Storage**: `~/.muhiya` layout is a compat boundary. The plan-state sidecar
gains ONE new unified-lifecycle shape written going forward; a bounded
compatibility loader migrates every legacy shape (planMode/pendingPlan/phase/
pipeline_phase/pipeline_depth) on read. No destructive migration.

**Testing**: `go test ./... -count=1` green at every merge point (never-broken
rule). New permanent suites: fault-injection (chaos catalog × recovery
invariant), instruction-system audit, layering + file-size budget checks,
wiring inventory check. All pinned guards (denial texts, prompt budget, prefix
stability, marshal determinism, conformance goldens, delegation regression)
remain authoritative.

**Target Platform**: Windows/macOS/Linux terminals; no platform changes.

**Project Type**: Single existing codebase, structural rework. No new
user-facing features; removals itemized in a reviewable ledger.

**Performance Goals**: Steady-state DeepSeek cache hit rate within 1 point of
the pre-rework baseline (SC-006); delegation benchmark plan-only execution 8/8;
no TUI responsiveness regression (existing perf-guard tests keep passing).

**Constraints**: Byte-stable prefix discipline — ALL static model-facing text
changes land as ONE recorded epoch (Principle III); DeepSeek-only wire behavior
byte-identical (existing conformance goldens); approval always-pause preserved;
no token/turn hard-failure ceilings reintroduced (standing directive: limits
shape work, never destroy it); MiniMax verification stays simulated-only;
live legs respect the account budget window.

**Scale/Scope**: Whole-client rework: ~1 state-machine unification (touches
orchestrator + tui + state + contract), ~8-12 file splits across orchestrator
and tui, one instruction-system module with audit tests, one fault-injection
suite, dead-code/duplicate removals per the R3 ledger, wiring inventory.
Estimated net-negative LOC excluding new tests.

**Phase 0 unknowns (resolved in research.md)**:
- R1: The unified lifecycle — state set, transitions, owner, persisted shape,
  compat migration, and the mapping of every current consumer.
- R2: Module split map, per-file size budget (spec default ≈800 lines) and its
  enforcement test; package-layering verification and the one known back-edge
  question (state → gateway helper).
- R3: Dead-code tooling choice and the verified candidate + duplicate-mission
  ledgers, including known false-positive traps (benchmark-only consumers).
- R4: Instruction-system single-artifact design and the audit test suite;
  prefix-vs-tail placement constraints.
- R5: Fault catalog coverage matrix (existing vs required), the chaos-suite
  harness design with the three-outcome invariant helper, and the wiring
  inventory format + enforcement.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate for this feature | Pre-Phase-0 | Post-Phase-1 |
|---|-----------|----------------------|-------------|--------------|
| I | Correctness before optimization | The rework's first success criterion is the recovery invariant + green suite, not speed/size | PASS — SC-002/SC-006 are the bar | PASS — fault-injection contract fixes the invariant per scenario |
| II | Cache efficiency without quality loss | Restructure must not disturb steady-state cache (≤1 pt); instruction rework is placement-aware | PASS — SC-006, FR-019 | PASS — instruction contract pins prefix/tail placement per text |
| III | Deterministic stable prefix | One epoch for all static text; unified lifecycle stays out of the prefix | PASS — FR-018 | PASS — lifecycle contract keeps state on dynamic surfaces only |
| IV | Dynamic vs cached separation | State/gate messages remain tail/sidecar riders | PASS | PASS — instruction inventory tags every text prefix-vs-tail |
| V | No redundant retransmission | Consolidation reduces duplicated text/mechanisms; no new re-sends | PASS | PASS — duplicate-mission merges are net-deduplicating |
| VI | Honest measurement | Before/after captures pinned; simulated figures labeled | PASS — SC-006/SC-008 | PASS — quickstart defines live vs simulated legs |
| VII | Reference architecture | Claude Code remains the behavioral target for lifecycle/instruction coherence | PASS — R1/R4 study existing primitives first | PASS — research recorded |
| VIII | Improve, don't rewrite | Tension acknowledged: this IS a structural rework. Resolution: behavior-preserving refactor under the always-green rule — the state machine UNIFIES two existing machines (no third system), splits MOVE code without rewriting it, and every pinned behavior survives byte-for-byte | PASS with justification (Complexity Tracking) | PASS — contracts define behavior-preservation gates per move |
| IX | Clean, secure, provider-compatible | fmt/vet/test plus new layering/size/dead-code enforcement; wire compat re-verified | PASS | PASS — enforcement tests are deliverables |
| X | Verified improvements | Every claim (stability, structure, coherence) has an automated check or before/after capture | PASS — SC-001..008 | PASS — quickstart maps each SC to a runnable check |

**Gate result**: PASS (with the Principle VIII justification recorded in
Complexity Tracking).

## Project Structure

### Documentation (this feature)

```text
specs/010-ultimate-consolidation/
├── spec.md                # done
├── plan.md                # this file
├── research.md            # Phase 0: R1–R5 decisions + inventories
├── data-model.md          # Phase 1: unified lifecycle, instruction system, fault catalog, ledgers
├── quickstart.md          # Phase 1: validation runs (suites, audits, live leg)
├── contracts/
│   ├── unified-lifecycle.md      # states, transitions, owner, persistence, compat migration
│   ├── module-structure.md       # split map, size budget, layering rules + enforcement
│   ├── instruction-system.md     # single-artifact layout + audit checks
│   ├── fault-injection.md        # chaos catalog × three-outcome invariant
│   └── wiring-inventory.md       # advertised-surface enumeration + enforcement
└── tasks.md               # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
internal/contract/         # unified lifecycle type + persisted shape (additive)
internal/orchestrator/     # engine split (lifecycle/turnloop/dispatch/handlers/budgets/usage)
│                          # + instructions module + fault-injection suite
internal/tui/              # render/layout/hittest/update splits
internal/state/            # sidecar compat loader
internal/command/          # wiring only (no behavior change)
benchmarks/                # unchanged runners; delegation benchmark re-run for SC-006
```

**Structure Decision**: Single repo, behavior-preserving restructure executed as
ordered moves (unify lifecycle → split modules → consolidate instructions →
add enforcement suites → remove dead weight), each move landing with the full
suite green.

## Complexity Tracking

| Violation | Why needed | Simpler alternative rejected because |
|-----------|-----------|--------------------------------------|
| Structural rework under Principle VIII (improve, don't rewrite) | The dual lifecycle is the proven root cause of the worst live failures (multiple deadlocks/desyncs across features 008–009); no incremental patch can remove the seam because the seam IS the duplication. The rework unifies existing machines and moves existing code — it does not reinvent behavior, and every pinned behavior/test survives | Keep patching sync points between the two machines (rejected — each of the last four live incidents was a new sync point nobody predicted; the bug class outlives any finite patch set); freeze structure and only add tests (rejected — tests over a two-truth system detect desync after the fact; only one truth makes it impossible) |
