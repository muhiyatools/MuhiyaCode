# Implementation Plan: Enforced Orchestration Pipeline & MiniMax Provider Integration

**Branch**: `009-orchestration-minimax-overhaul` | **Date**: 2026-07-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-orchestration-minimax-overhaul/spec.md`

## Summary

Make MuhiyaCode orchestrate like Claude Code — the main agent as
**planner/orchestrator/reviewer/QA**, sub-agents doing most research and
implementation — and make that flow **harness-enforced, not prompt-hoped**. For any
task that needs a plan: enter planning mode, fan out research sub-agents, synthesize
a detailed Markdown plan, **always pause for user approval (even in auto-accept)**,
then on approval divide the plan into dependency-aware implementation tasks executed
by sub-agents, collect and review their reports, run/delegate final validation, and
only then finalize. Depth scales by task class (standard = light: research + plan +
approval; large/epic = full four phases); per-phase sub-agent **counts scale by
effort only**; no user override surface. A no-plan request is answered directly with
no agent flow. Separately, add **production-ready MiniMax provider support** to the
gateway (M3/M2.x models, function calling, passive prompt caching to the DeepSeek
standard, tiered pricing, usage/billing) and make DeepSeek+MiniMax mixed sessions
work; fresh installs default to **M3 main + DeepSeek V4 Pro sub** with graceful
DeepSeek fallback. MiniMax is wired and testable but the user will not fund the API
now — so all MiniMax verification runs against a **recorded/simulated upstream**, not
live calls, and DeepSeek remains the live-benchmark provider.

## Technical Context

**Language/Version**: Go 1.25+ — client `github.com/muhiya/muhiyacode`; gateway
module `gateway` at `F:\MuhiyaWorkspace\MuhiyaWorkspace`.

**Primary Dependencies**: Existing only (Bubble Tea v2 TUI, stdlib net/http, uniseg).
No new dependencies anticipated (Principle IX). Pipeline is built ON the existing
plan-mode, plan-file, knowledge-store, subagent, and approval (Ask/Confirm)
machinery — extend, don't replace (Principle VIII).

**Storage**: `~/.muhiya` session state (compat boundary); the plan artifact extends
the existing workspace plan-file mechanism; pipeline phase state persists in the
existing session sidecar shape. Gateway: Postgres model/provider rows + request_logs
gain MiniMax coverage (nullable/additive; no destructive migration).

**Testing**: `go test ./... -count=1` both repos; scripted-provider orchestrator
tests for the pipeline state machine (phase gates, approval pause, degradation,
dependency ordering, completeness/anti-premature-finish); the feature-008
`delegationbench` runner for the live before/after DeepSeek pipeline benchmark;
MiniMax conformance via a **recorded/simulated upstream fixture** (no live MiniMax
spend — user directive).

**Target Platform**: Windows/macOS/Linux terminals (TUI incl. narrow-width);
gateway on Linux.

**Project Type**: Two existing codebases, incremental. Client = orchestration
overhaul + MiniMax client capability/replay. Gateway = MiniMax provider.

**Performance Goals**: SC-001 pipeline phases observed end-to-end on the benchmark;
SC-003 plan-only execution passes 8/8 with main conversation ≤50% of the no-pipeline
baseline; SC-005 MiniMax conformance ≥50% warm cached share (simulated fixture);
SC-006 mixed-session cache within 5 pts of single-provider baselines.

**Constraints**: Byte-stable prefix discipline (pipeline instructions are ONE static
epoch; all phase/handoff state rides dynamic surfaces — tail, sidecars, artifacts);
honest measurement (all displayed/benchmarked figures provider-reported or labeled);
**no token ceiling reintroduced**; effort→allowance numbers unchanged; `~/.muhiya`
layout preserved; **the always-pause approval overrides auto-accept** (a flow gate,
distinct from permission mode); DeepSeek-only deployments byte-identical (FR-018).

**Scale/Scope**: ~10 client files (classify, plan, engine pipeline state machine,
subagent handoff, prompt, model.go capability, config defaults, tui approval/phase
rendering), ~7 gateway files (provider row, models/pricing, handler routing +
MiniMax body shaping + cached-token accounting, thinking dialect, translator,
migration), 4 contracts, 1 benchmark extension, MiniMax simulated fixture.

**Phase 0 unknowns (resolved in research.md)**:
- R1: The pipeline state machine — states, gates, and how each maps onto EXISTING
  plan-mode/plan-file/knowledge/subagent primitives (no parallel system).
- R2: The enforcement mechanism — how the harness *gates* implementation on
  research-done + plan-exists + approved, without a token ceiling and without
  breaking the simple-task fast path.
- R3: The always-pause approval that overrides auto-accept — which existing surface
  (Ask vs Confirm vs plan-ready), and headless (`-p`) behavior when no approver.
- R4: "Needs a plan" decision from the existing classifier — thresholds, visibility.
- R5: Structured handoff contract — role/scope/deliverable/format in, bounded
  structured summary out; built on the knowledge store + subagent report format;
  dependency-aware task division; anti-duplication and completeness/anti-premature
  -finish guards.
- R6: MiniMax provider integration — gateway routing/body-shaping/cached-token
  accounting/tiered pricing; client capability profile + the per-family reasoning
  replay policy (the Mini-Agent `reasoning_details` requirement vs DeepSeek strip).
- R7: Fresh-install defaults (M3 main + V4Pro sub) with DeepSeek fallback; no change
  to existing configs.
- R8: MiniMax verification WITHOUT live spend — the recorded/simulated upstream
  fixture design; DeepSeek stays the live pipeline benchmark.
- R9: Reasonix + Claude Code reference pass (Principle VII) for the orchestration
  shape; Mini-Agent reference for MiniMax wire fidelity (baseline done).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate for this feature | Pre-Phase-0 | Post-Phase-1 |
|---|-----------|----------------------|-------------|--------------|
| I | Correctness before optimization | Pipeline must raise correctness (completeness/anti-premature-finish), never trade it for speed | PASS — SC-003 completeness + review phase are acceptance bars | PASS — pipeline contract makes verified completion the finish gate |
| II | Cache efficiency without quality loss | Pipeline prompt = one epoch; MiniMax caching to DeepSeek standard; mixed-session per-provider prefixes stable | PASS — FR-015/019, SC-006 | PASS — contracts pin static-vs-dynamic placement + per-provider affinity |
| III | Deterministic stable prefix | Pipeline instructions byte-stable; phase/handoff state never in prefix; MiniMax replay variant analyzed for prefix stability | PASS — planned as one epoch + dynamic riders | PASS — replay-policy contract asserts per-provider prefix stability |
| IV | Dynamic vs cached separation | Phase state, plan refs, handoff summaries ride tail/sidecars/artifacts | PASS | PASS — pipeline-state contract fixes placement |
| V | No redundant retransmission | Handoffs pass bounded summaries, not transcripts; no re-explore of covered scope | PASS — FR-011, SC-004 | PASS — handoff contract + duplicate-read audit |
| VI | Honest measurement | Pipeline/MiniMax metrics provider-reported or labeled; MiniMax figures from the simulated fixture are labeled simulated, never presented as live | PASS — FR-015/021, R8 | PASS — quickstart marks simulated vs live explicitly |
| VII | Reference architecture (Reasonix) | Study before cache-affecting prompt design; Claude Code as behavioral target; Mini-Agent for MiniMax wire | PASS — R9 planned; baseline done | PASS — research.md §R9 records findings |
| VIII | Improve, don't rewrite | Pipeline built on existing plan-mode/knowledge/subagent; MiniMax mirrors DeepSeek provider path | PASS — no new subsystem | PASS — Complexity Tracking justifies the one new state machine |
| IX | Clean, secure, provider-compatible | fmt/vet/test; MiniMax key handling like existing providers; graceful degradation (no MiniMax ⇒ DeepSeek-only unchanged) | PASS — FR-018 | PASS — contracts specify fallback + secret handling |
| X | Verified improvements | DeepSeek pipeline before/after live benchmark; MiniMax via labeled simulation; cold vs steady separated | PASS — SC-001/003/008, R8 | PASS — quickstart defines runs |

**Gate result**: PASS (both). One item enters Complexity Tracking: the pipeline
**state machine** is genuinely new structure (justified below); everything else is
extension of existing primitives.

## Project Structure

### Documentation (this feature)

```text
specs/009-orchestration-minimax-overhaul/
├── spec.md                # done (with Clarifications)
├── minimax-baseline.md    # done — MiniMax docs + Mini-Agent + pricing + M-risks
├── plan.md                # this file
├── research.md            # Phase 0: R1–R9 decisions
├── data-model.md          # Phase 1: pipeline phases, plan artifact, handoff, routes, MiniMax profile
├── quickstart.md          # Phase 1: validation (live DeepSeek pipeline + simulated MiniMax)
├── contracts/
│   ├── pipeline-state.md         # phase state machine, gates, approval pause, degradation
│   ├── handoff-contract.md       # subagent role/scope/deliverable in; bounded summary out; dependency + completeness guards
│   ├── minimax-provider.md       # gateway routing/body/caching/pricing + client replay policy
│   └── default-models.md         # fresh-install M3+V4Pro with fallback; no-touch existing config
└── tasks.md               # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
# Client — F:\MuhiyaCode Agent Go
internal/orchestrator/
├── classify.go        # "needs a plan" decision surfaced; class→pipeline-depth mapping
├── plan.go            # plan-mode lifecycle reused; plan artifact write; approval-pause hook
├── pipeline.go        # NEW: the phase state machine (research/plan/approve/implement/validate)
├── engine.go          # phase gates in the run loop; approval pause (overrides auto-accept); completeness/anti-premature-finish
├── subagent.go        # handoff contract (role/scope/deliverable/format); structured report; dependency ordering
├── knowledge.go       # handoff-summary substrate (bounded, structured)
├── prompt.go          # ONE static pipeline-orchestration epoch (main-as-orchestrator)
├── model.go(gateway pkg) # MiniMax capability profile + per-family reasoning replay policy
internal/gateway/
├── provider.go / sse.go  # per-family replay (MiniMax reasoning_details preserved) + cached-token read
internal/state/config.go   # fresh-install M3+V4Pro defaults with DeepSeek fallback
internal/tui/
├── actions.go / view.go   # approval-pause modal; phase/role/model visibility; plan render

# Gateway — F:\MuhiyaWorkspace\MuhiyaWorkspace
proxy/handler.go       # MiniMax route + OpenAI-dialect body shaping + cached-token accounting + tiered cost
proxy/thinking.go      # MiniMax reasoning_split normalization (documented form only)
proxy/translator.go    # MiniMax usage fields (prompt_tokens_details.cached_tokens)
db/db.go + migrations/ # MiniMax provider + model rows (tiered pricing) + cache column reuse
```

**Structure Decision**: Two repos, incremental. The single genuinely new client
unit is `pipeline.go` (the phase state machine); it composes existing primitives
(plan mode, plan file, knowledge store, subagents, Ask/Confirm) rather than
replacing any. Gateway MiniMax mirrors the DeepSeek provider path.

## Complexity Tracking

| Violation | Why needed | Simpler alternative rejected because |
|-----------|-----------|--------------------------------------|
| New `pipeline.go` phase state machine (VIII) | The enforced research→plan→approve→implement→validate flow with dependency ordering, completeness verification, and anti-premature-finish is real new behavior that no single existing primitive expresses; scattering it across engine.go's already-large run loop would be less maintainable and untestable in isolation | Doing it purely via prompt text (rejected — that is exactly today's unreliable, model-discretion approach the spec exists to replace); overloading plan-mode alone (rejected — plan mode is one phase of five and has no research/implement/validate/dependency semantics) |
