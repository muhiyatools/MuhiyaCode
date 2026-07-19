# Implementation Plan: Competitive Agent Audit & Transformation

**Branch**: `011-competitive-agent-audit` | **Date**: 2026-07-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-competitive-agent-audit/spec.md`

## Summary

Transform MuhiyaCode into a demonstrably competitive coding agent through an audit-driven, measurement-first program. Phase 0 ([research.md](research.md)) delivered the full forensic map: the review-spam root cause is proven (an eager classifier funnels tasks into full-depth pipelines whose validation phase dispatches a review subagent **unconditionally** — `classify.go:178` + `phaserunners.go:403-408` — plus an ungated max-effort AutoReview nudge at `turnloop.go:630-642`), subagents have **no token ceilings** (run-count caps only), the main prompt carries **no cheapest-tool steering** (`run_shell` description is an undirected one-liner), all subagent kinds churn **one shared cache pin**, and per-pairing cache metrics needed for mixed-model verification are not surfaced. The transformation is ten decisions (D1–D10): a deterministic review gate at the dispatch sites, classifier corroboration, complexity-scaled subagent token ceilings, per-kind cache pins, budget-neutral prompt steering, review scope/partial-coverage contracts, structured subagent returns, per-pairing cache metrics, a baseline-first benchmark suite, and an exemplar-prompt comparison protocol. Every change is additive around the proven-strong seams (handoff isolation, `PrefixShape` enforcement, `InspectionLedger` dedupe, knowledge banking) per Constitution VIII.

## Technical Context

**Language/Version**: Go (module `github.com/muhiya/muhiyacode`, go 1.22 in go.mod; local toolchain go1.26.4 verified this session)

**Primary Dependencies**: Bubble Tea v2 / Bubbles v2 / Lipgloss v2 (TUI, untouched by this feature); no new dependencies anticipated

**Storage**: `~/.muhiya` (settings.json, secrets.json, per-session sidecars, SQLite transcript store); benchmark artifacts under `specs/011-competitive-agent-audit/benchmarks/` and `benchmarks/`

**Testing**: `go test ./... -count=1` (+ `make verify` = fmt/vet/test/build; race on CGO runners); existing lattice this feature extends: `prompt_budget_test.go` (5,789-char pinned budget), `prompt_stability_test.go`, `prefixshape_test.go`, `request_assembly_test.go`, `cachehit_guard_test.go`, `mixed_provider_test.go`, instructions `audit_test.go`/`dump_test.go` goldens

**Target Platform**: Windows + Linux terminals (agent CLI/TUI); MuhiyaLLM gateway (separate repo) reached via OpenAI-compatible wire — **no gateway changes required** (per-pairing metrics are computable agent-side from provider-reported usage)

**Project Type**: single Go CLI/TUI agent; this feature touches `internal/orchestrator` (37 files; key: `turnloop.go` 783, `classify.go` 286, `phaserunners.go` 473, `subagent.go` 460, `effort.go` 104, `usage.go` 304), `internal/instructions` (9 files), benchmarks/scripts

**Performance Goals**: spec SC-001..SC-009 — trivial-task auto-review <10% (from ~always), ≥95% high-risk review retention, ≥30% median small-task token reduction, review overhead ≤20% median with 100% ceiling compliance, mixed-provider per-model hit rates within 5 points of single-model baselines, cheapest-tool violations <2%, reproducible benchmark variance band

**Constraints**: Constitution v1.0.0 gates I–X — notably byte-identical stable prefix (III), dynamic-content tail placement (IV), no redundant retransmission (V), provider-reported measurement only (VI), smallest-change transformation (VIII), before/after verification on real sessions (X)

**Scale/Scope**: 21 non-MCP tools + MCP; 4 subagent kinds; 5,789-char pinned system prompt; sessions against DeepSeek/MiniMax/OpenRouter-hosted models through the gateway; benchmark matrix ≈ 20–30 tasks × 2 model configurations

## Constitution Check

*GATE: evaluated before Phase 0 research; re-evaluated after Phase 1 design — both PASS, no violations to track.*

| Principle | Gate verdict | How this plan complies |
|---|---|---|
| I Correctness before optimization | PASS | Review gating never blocks explicit requests (FR-009); high-risk retention is a hard SC (SC-002) checked before any cost win is accepted; benchmark suite guards completion rate alongside spend |
| II Cache efficiency without quality loss | PASS | D5 trims redundant static text to fund new steering (net prefix ≤ 0); no content the model needs is removed — the trimmed section is duplicated dynamically (F10 evidence); validated per X |
| III Deterministic stable prefix | PASS | All prompt edits are static/byte-stable; `prompt_budget_test.go` + `dump_test.go` goldens updated in the same change; one deliberate, recorded invalidation on upgrade (existing `SwitchModel` pattern); `PrefixShape` enforcement untouched |
| IV Dynamic/cached separation | PASS | TaskProfile/ReviewDecision are runtime state; the only model-visible artifacts (review rationale line, partial-coverage note) ride tool results / the turn tail, never the prefix |
| V No redundant retransmission | PASS | D7 converges model-invoked subagent returns onto the knowledge-banking pattern; briefs remain scoped digests; no new verbatim re-entry paths |
| VI Honest measurement | PASS | All ceilings and metrics use provider-reported usage (`usage.go`); estimates stay labeled; benchmark records carry raw usage fields (contracts/benchmark-run.md) |
| VII Reasonix reference | PASS | `specs/002-reasonix-agent-overhaul/research.md` cited and re-derived in research.md §1.9; the one new deviation (per-kind pins, D4) is justified against Reasonix's single-kind design |
| VIII Improve, don't rewrite | PASS | No subsystem rewrite: D1 is a new small module wired into two existing call sites; D2–D8 are targeted edits around preserved seams (F14); compatibility surfaces (`~/.muhiya`, wire protocol) untouched |
| IX Clean, maintainable, secure, compatible | PASS | Standard gates (`make verify`); no security-model changes; degrades gracefully when a provider omits cache fields (per-pairing metrics show "not reported") |
| X Verified improvements | PASS | Baseline capture on the unchanged build is the first implementation task (D9); every SC delta measured before/after with model/gateway/effort/workload held constant; artifacts stored in this feature directory |

**Complexity Tracking**: no entries — no constitutional violations require justification.

## Project Structure

### Documentation (this feature)

```text
specs/011-competitive-agent-audit/
├── spec.md              # Feature specification (/speckit-specify)
├── plan.md              # This file
├── research.md          # Phase 0: forensic analysis, findings register F1–F15, decisions D1–D10
├── data-model.md        # Phase 1: entities (TaskProfile, ReviewDecision, budgets, metrics, benchmark records)
├── quickstart.md        # Phase 1: validation guide (baselines, gating scenarios, mixed-model checks)
├── contracts/
│   ├── review-gating.md     # TaskProfile → ReviewDecision contract, tiers, ceilings, config surface
│   ├── subagent-handoff.md  # Brief/return-package contract, per-kind pins, token-ceiling wrap-up
│   └── benchmark-run.md     # Benchmark record schema, variance band, storage rules
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks — not created by this command)
```

### Source Code (repository root)

```text
internal/orchestrator/
├── reviewgate.go        # NEW — D1/D6: TaskProfile assembly + ReviewDecision (tier, rationale, ceilings, scope)
├── classify.go          # D2: corroboration-based Large escalation (breadthRE handling at :117,:178)
├── phaserunners.go      # D1/D6: gate call before runPipelineValidation dispatch (:41-45,:394-431)
├── turnloop.go          # D1: gate call before AutoReview nudge (:630-642)
├── subagent.go          # D3: token ceiling in subagentInput + loop enforcement; D4: per-kind pins (:321); D7: digest returns (:208-212)
├── dynamicfanout.go     # D3: complexity scale reuse (repo-size buckets); F8 scope-partitioned lens briefs
├── effort.go            # D3: per-tier ceiling bases; AutoReview interplay (:59)
├── usage.go             # D8: per-(model, pin) cache attribution aggregation
├── contextreport.go     # D8: per-pairing hit-rate rows
└── *_test.go            # New: reviewgate_test, ceilings test, pin-separation test, return-digest test

internal/instructions/
├── prompt.go            # D5: TOOLS AND RECOVERY steering rule; F10 condensed pipeline section
├── tools.go             # D5: run_shell steering sentence; F9 count-comment fix
└── (audit/dump/budget tests updated in the same changes)

benchmarks/ + scripts/   # D9: task matrix, runner, baseline + variance procedure (per contracts/benchmark-run.md)
```

**Structure Decision**: single-project layout (existing repo structure); one new file (`reviewgate.go`), all other work lands as targeted edits in the files enumerated above, matching the smallest-change mandate. The gateway repo is intentionally out of scope (Technical Context note).

## Phase 0: Research (complete)

See [research.md](research.md): verified anatomy (§1), findings register F1–F15 with file:line evidence and severities (§2), decisions D1–D10 with rationale and rejected alternatives (§3). All unknowns resolved; the exemplar system prompt remains a non-blocking input dependency handled by D10.

## Phase 1: Design & Contracts (complete)

- [data-model.md](data-model.md) — entities, validation rules, and state transitions for TaskProfile, ReviewDecision, SubagentBudget, HandoffBrief/ReturnPackage, ModelPairing/UsageAttribution, BenchmarkRun, AuditFinding/RoadmapItem, TokenBudgetManifest.
- [contracts/review-gating.md](contracts/review-gating.md) — the gating decision contract both dispatch sites consume, tier-selection defaults, ceilings, config surface, rationale-line format, partial-coverage reporting.
- [contracts/subagent-handoff.md](contracts/subagent-handoff.md) — the preserved brief seam, per-kind session-pin scheme, structured return package, ceiling wrap-up behavior.
- [contracts/benchmark-run.md](contracts/benchmark-run.md) — honest-measurement record schema and reproducibility rules the suite and all SC verdicts use.
- [quickstart.md](quickstart.md) — runnable validation scenarios mapping directly to SC-001..SC-009.

**Post-design Constitution re-check**: PASS (table above reflects the completed design; no new violations introduced by Phase 1 artifacts).
