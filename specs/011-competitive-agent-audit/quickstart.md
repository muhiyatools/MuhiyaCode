# Quickstart: Validating the Competitive Agent Transformation

**Feature**: `011-competitive-agent-audit` · Maps directly to spec SC-001..SC-009.
**References**: [contracts/review-gating.md](contracts/review-gating.md) · [contracts/subagent-handoff.md](contracts/subagent-handoff.md) · [contracts/benchmark-run.md](contracts/benchmark-run.md) · [data-model.md](data-model.md)

## Prerequisites

- Built agent (`make build` → `bin/muhiyacode`), signed in against the live gateway, with a mixed pairing configurable (main: MiniMax-family; subagent: DeepSeek-family) via `/model`.
- `go test ./... -count=1` green before starting.
- Benchmark fixtures checked out (committed with this feature per contracts/benchmark-run.md §2.6).

## Step 0 — Baseline capture (MUST precede any transformation merge)

```bash
# Run the suite twice on the unchanged build, both model configs:
scripts/bench_011.(ps1|sh) -config single  ; scripts/bench_011.(ps1|sh) -config single
scripts/bench_011.(ps1|sh) -config mixed   ; scripts/bench_011.(ps1|sh) -config mixed
```

**Expected**: four run records under `specs/011-competitive-agent-audit/benchmarks/`; variance band computed and recorded; baseline aggregates show the current pain (trivial-task auto-review ≈ every full-depth task; no review ceilings; no per-pairing hit rates in records — `reported:false` acceptable pre-change).

## Step 1 — Review gating scenarios (SC-001, SC-002, SC-009)

| Scenario | Do | Expect |
|---|---|---|
| Trivial docs | Ask for a README wording fix | task completes, **no review subagent**, completion shows `review: skip — docs-only…` |
| Small logic | 1-file, ~10-line logic fix, tests pass | `review: skip` (default mode) / `focused` (conservative) |
| Risky one-liner | Remove/alter an auth check (fixture) | **review runs** (`focused` min) despite 1-line diff — H4 risk rule |
| Multi-file logic | 5-file change | `review: focused — …`, scope = changed files + direct dependents |
| Explicit request | `/review` after a trivial change | review always runs (H1) |
| Gating off | set `review_gating off`, risky change | no auto review; explicit still works |

## Step 2 — Ceiling & partial coverage (SC-004)

Run the large-codebase fixture task (deliberately oversized review surface). **Expect**: review stops at its tier ceiling, wrap-up turn produces a `CoverageReport` with explicit covered/skipped lists, run record shows `ceiling_hit:true` and spend ≤ cap; nothing silently truncated. 100% ceiling compliance across large-fixture runs.

## Step 3 — Mixed-model & cache pins (SC-005)

1. Configure mixed pairing; run a session with ≥ 2 sequential different-kind subagent tasks (explore → review).
2. **Expect**: `/context` (or TaskStats) shows per-pairing rows — `(main-model, :main)`, `(sub-model, :sub:explore)`, `(sub-model, :sub:review)` — each with its own steady-state hit rate or `not reported`.
3. Compare against single-model baseline records: each model's steady-state hit rate within 5 points (SC-005). Same-kind repeat dispatches share a pin (warm); different kinds no longer evict each other.

## Step 4 — Token efficiency & tool discipline (SC-003, SC-006)

1. Rerun the full suite post-transformation, same configs as Step 0.
2. **Expect vs baseline**: median small-task tokens ↓ ≥ 30% at equal-or-better completion rate; `terminal_read_when_tool_exists` + `duplicate_reads` violations < 2% of file-access actions; review overhead ≤ 20% median (small/medium tasks).
3. Prompt budget: `go test ./internal/instructions/... ./internal/orchestrator/ -run 'Budget|Stability|PrefixShape' -count=1` — pinned budget not exceeded (D5 net ≤ 0), prefix byte-stability tests green.

## Step 5 — Audit deliverable & traceability (SC-007)

Verify research.md §2 register: every FR-001 subsystem has findings (F1–F15) with evidence/severity/remediation; every roadmap item (tasks.md, Phase 2) traces to ≥ 1 finding and ≥ 1 SC. Exemplar-prompt comparison section present (populated, or explicitly marked pending with the D10 protocol).

## Step 6 — Regression gate (always-on)

`make verify` + the conformance tests from all three contracts + the double-run variance check on any config used in a claim. A quality or completion-rate regression blocks the change regardless of efficiency gains (Constitution I/II/X).
