# Implementation Plan: Harness Reliability & Clarity Overhaul

**Branch**: `008-harness-reliability-overhaul` | **Date**: 2026-07-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/008-harness-reliability-overhaul/spec.md`

## Summary

Make the agent harness measurably more reliable and its numbers honest, in four
slices: (1) DeepSeek-aligned delegation — rewrite the delegation guidance so the
model actually uses its subagent allowance when the task shape calls for it (the
real before-case: 32 main-loop requests, 0 subagents at max effort), with explicit
compensations for DeepSeek's execution drift, instruction-dropping, and edit
fumbling, all inside the byte-stable prefix as a one-time cache epoch; (2) a
mid-session model-switch warning (per-model provider cache cold-starts + pricing
differences) with proceed/cancel; (3) an honest headline token figure (provider-
reported uncached input + output, plus cache-hit %) in the live status and task
summary, detail staying in `/context`; (4) a redesigned session usage panel and
context view (cost, model-wait vs task time, lines ±, per-model breakdown,
usage-by-category context table) in MuhiyaCode's own visual system. Client repo
only. Verified by a before/after delegation benchmark under the constitution's
measurement standards.

## Technical Context

**Language/Version**: Go 1.25+ (module `github.com/muhiya/muhiyacode`, toolchain
go1.26.4). Client repo only — no gateway changes (spec Out of Scope).

**Primary Dependencies**: Existing only — Bubble Tea v2 / Lipgloss (TUI), uniseg
(width), stdlib. No new dependencies anticipated (Principle IX).

**Storage**: `~/.muhiya` session state (compat boundary, unchanged layout); any new
session-scoped aggregates (per-model usage rows, cumulative line counts, request
durations) live in existing in-memory session structures and existing persistence
shapes — no format break without a migration note.

**Testing**: `go test ./... -count=1` (all packages); scripted-provider orchestrator
tests for delegation/denial/recovery flows; existing prefix-stability +
marshal-determinism checks must stay green; live before/after delegation benchmark
(constitution Principle X) with a new scripted delegation-appropriate workload
checked into `specs/008-…/benchmarks/`.

**Target Platform**: Windows/macOS/Linux terminals (TUI), incl. narrow-width and
RTL-safe rendering rules for the new tables.

**Project Type**: Single existing CLI/TUI codebase; incremental edits only
(Principle VIII).

**Performance Goals**: SC-001 — benchmark task completes with ≥2 subagent runs and a
≥25% smaller final main conversation; SC-002 — billed (non-cached) tokens ≤ +10% vs
before, steady-state cache-hit rate no regression; SC-003 — zero gratuitous
delegation on the low-effort workload.

**Constraints**: Byte-stable stable-prefix discipline (delegation guidance is a
single recorded cache-epoch upgrade; per-task nudges ride the user-message tail
within the established ≤~50-token rider budget from feature 002 SC-004); honest
measurement (all displayed figures provider-reported or labeled estimated); the
removed token ceiling MUST NOT return (spec FR-005); `~/.muhiya` compat preserved;
existing effort→allowance numbers unchanged.

**Scale/Scope**: ~8 client files expected (orchestrator: prompt/classify/subagent/
engine/effort consumption; TUI: actions/view/model), 3 contracts, 1 new benchmark
workload, 0 new dependencies.

**Phase 0 unknowns (all resolved in research.md)**:
- R1: Why the model skips subagents today — verbatim inventory of current
  delegation guidance (system prompt, tool description, classify riders, budget
  note timing).
- R2: The exact mid-session model-switch flow and the correct warning hook point
  (picker → apply → invalidation event), incl. subagent-model switches.
- R3: Exact provider-usage fields backing the honest headline (miss + completion;
  fallback totals; estimated markers).
- R4: What per-request data exists for the usage panel (per-model attribution,
  request durations, cost rows) and what must be added.
- R5: Context-category estimation inputs (system prompt size, tool-definition
  size, project memory, messages, summary, free) from the calibrated estimator.
- R6: Reasonix reference pass (Principle VII) — how the reference architecture
  induces delegation; DeepSeek failure-mode evidence in this repo.
- R7: Delegation benchmark design (scripted workload; before/after protocol).
- R8: Layout adaptation of the Claude-Code-inspired panels to MuhiyaCode's visual
  system.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate for this feature | Pre-Phase-0 | Post-Phase-1 |
|---|-----------|----------------------|-------------|--------------|
| I | Correctness before optimization | Delegation must not degrade answer quality; edits verified; bounded recovery — quality outcomes gate the benchmark | PASS — SC-001 requires "completes correctly"; FR-004/005 encode verification + bounded stops | PASS — delegation contract makes correct completion the acceptance bar |
| II | Cache efficiency without quality loss | Prompt-text change disclosed as one-time epoch; no per-request prefix variance; steady-state hit rate guarded | PASS — FR-007 + SC-002 | PASS — delegation-guidance contract pins placement + epoch; benchmark guards regression |
| III | Deterministic stable prefix | New guidance byte-identical across turns; no timestamps/randomness | PASS — FR-001/FR-007 | PASS — contract asserts prefix-stability checks cover the new text |
| IV | Dynamic vs cached separation | Per-task delegation nudges ride the user tail within the ≤~50-token rider budget | PASS — planned as classify-rider extension, never prefix | PASS — contract fixes rider placement/budget |
| V | No redundant retransmission | Subagent reports consumed, no wholesale re-reads (FR-006); no new retransmission | PASS | PASS — measured on benchmark (duplicate-read counters exist) |
| VI | Honest measurement | Headline/panels from provider-reported usage; estimates labeled; no fabricated splits | PASS — FR-011..013, FR-017 | PASS — usage-display contract defines exact formulas + fallbacks |
| VII | Reference architecture: Reasonix | Study before cache-affecting prompt design; findings in research.md | PASS — R6 planned (002 inventory carry-forward + fresh delegation-focused pass) | PASS — research.md §R6 records findings |
| VIII | Improve, don't rewrite | Smallest changes to prompt/classify/TUI; no orchestration rewrite | PASS | PASS — Complexity Tracking empty |
| IX | Clean, maintainable, secure, provider-compatible | fmt/vet/test gates; displays degrade when provider lacks cache fields (no DeepSeek hard-dependence) | PASS — spec edge case covers generic gateways | PASS — contracts specify unavailable-state rendering |
| X | Verified improvements | Before/after delegation benchmark, same workload/model/effort; cold vs steady reported | PASS — SC-001/002/003 + R7 protocol | PASS — quickstart defines the runs |

**Gate result**: PASS (both evaluations). No violations → Complexity Tracking empty.

## Project Structure

### Documentation (this feature)

```text
specs/008-harness-reliability-overhaul/
├── spec.md              # Feature specification (done)
├── plan.md              # This file
├── research.md          # Phase 0: R1–R8 (current-state inventory + decisions)
├── data-model.md        # Phase 1: entities (guidance, ledger, switch event, categories)
├── quickstart.md        # Phase 1: validation scenarios per SC
├── contracts/
│   ├── delegation-guidance.md   # Fixed-prefix delegation block + rider rules
│   ├── model-switch-warning.md  # Warning modal trigger/content/semantics
│   └── usage-display.md         # Headline formula, panel fields, category table
├── benchmarks/          # Before/after delegation benchmark artifacts (Phase 2+)
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
internal/orchestrator/
├── prompt.go        # delegation-guidance block (one-time epoch, byte-stable)
├── classify.go      # task-shape signal + tail rider (≤ rider budget)
├── subagent.go      # run_subagent description rewrite; report-consumption note
├── engine.go        # allowance disclosure timing; edit-failure corrective text;
│                    # session usage ledger (per-model rows, durations, lines ±)
├── effort.go        # (read-only reference — numbers unchanged)
internal/tui/
├── actions.go       # /model warning modal hook; /context & usage panel rendering
├── view.go          # honest headline in renderActivity + taskSummaryLine
├── model.go         # session-scoped display state (usage panel data)
internal/contract/
└── types.go         # only if ledger/report fields must cross the boundary
benchmarks/          # delegation workload runner (reuses cachebench patterns)
docs/agent-design.md # updated where orchestration behavior descriptions change
```

**Structure Decision**: Single repo, incremental edits in the two subsystems the
spec touches (orchestrator guidance/ledger; TUI display). No new packages unless
the ledger outgrows engine.go (decided in Phase 1, not by default).

## Complexity Tracking

> No Constitution Check violations — table intentionally empty.
