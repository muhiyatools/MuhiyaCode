# Quickstart: Validating Subagent Context Reuse & Cache-First Orchestration

**Feature 012** · Runnable validation scenarios. Contracts: [context-linking](contracts/context-linking.md), [phase-handoff](contracts/phase-handoff.md), [role-gate](contracts/role-gate.md). Data shapes: [data-model.md](data-model.md).

## Prerequisites

- Go 1.25+, repo green: `go build ./... && go vet ./... && go test ./... -count=1`
- A configured gateway key (`muhiyacode login`) for live scenarios; **live runs cost real money — owner go-ahead required first** (standing constraint; also research.md R-D12).
- Feature 011 benchmark harness in place (`scripts/bench_011.ps1`, fixtures under `specs/011-competitive-agent-audit/benchmarks/`).

## 0. Measure-first probes (BEFORE any behavior change merges — R-D12)

| Probe | Command sketch | Settles |
|---|---|---|
| P1 DeepSeek continuation | tiny live subagent task, then its continuation; compare continuation request's `prompt_cache_hit_tokens` vs predecessor's last `prompt_tokens` (bench JSON per-pairing rows) | end-of-output cache-unit boundary; expected SC-001 magnitude |
| P2 MiniMax routing truth | owner queries production `providers`/`models` tables (direct api.minimax.io vs OpenRouter), then P1-equivalent on the real path | whether SC-005's 70% MiniMax target stands or re-scopes |
| P3 effort-flip | identical resend with only `reasoning_effort` flipped | whether continuations must pin effort to the predecessor's |

Record raw results in `specs/012-subagent-context-cache/benchmarks/probes/`.

## 1. Baselines on the unchanged build (Constitution X — blocks all behavior changes)

```powershell
# frozen current build, legacy-free run of the 011 fixture suite (multi-phase subset)
$env:MUHIYA_BENCH_JSON = "1"
scripts/bench_011.ps1 -Suite multiphase -OutDir specs/012-subagent-context-cache/benchmarks/runs/baseline
```

Captures per-task: cost, per-pairing steady-state hit rates, file-read counts, handoff sizes. These are the SC-001/002/004/005/006 denominators.

## 2. Scenario validations (post-implementation)

### S1 — Continuation link happens and is verified (US1, SC-001)
Run a fixture task producing two sequential implement phases. Expect: task summary shows `link: continued (same-kind)` with a provider-reported cache share ≥ the P1-predicted floor; bench JSON `links[]` row present; **zero** re-reads of unchanged predecessor files (ledger `rereadDirectives` empty).

### S2 — Self-edit chain is not stale (R-D5 regression)
Fixture where phase-1 implementer edits 10 files it read. Phase-2 link must be `continued` with changed-fraction 0 (end-of-run fingerprints). A deliberate external edit between phases (test harness touches 6 of 10) must flip to `digest-seeded / stale:60%`.

### S3 — Review continues the implementer (US1/Q3)
Full-depth task reaching validation with a `clean-done` implementer. Expect `link: continued (review-after-implement)`; review runs on the implementer's stream; a mutation attempt by the review continuation is denied by the capability mask (CL-2); verdict parsing unchanged.

### S4 — Role gate holds and yields honestly (US2, role-gate contract)
Full-depth implementing: main-model `read_file` denied twice with guidance, third waived with `read-gate-waived` degradation; `run_shell "cat ..."` denied the same way; `grep` passes. Inject one failed dispatch → gate opens (`phase-read-exempt:post-failure`). Light-depth fixture: gate never fires.

### S5 — Lean handoffs (US4, SC-006)
Assert per-dispatch outbound handoff ≤ documented bound, contains `phaseRef` + Verification digest, **no file bodies** (prohibited-content test); communication overhead ≤10% of task tokens in bench JSON.

### S6 — Visibility & honesty (US5, SC-008)
Every dispatch in every fixture task has a `links[]` row with a reason; `digest-seeded` never renders as "linked"; zero-cache injection renders `linked but cold`; unavailable provider fields render "unavailable".

### S7 — Legacy degradation (FR-017)
`ContextLinking=off` and a pre-012 session dir (no `agents/` sidecars): wire bytes and behavior identical to baseline build (golden-compared); one-shot mode unaffected.

## 3. After/Before comparison (Constitution X)

```powershell
scripts/bench_011.ps1 -Suite multiphase -OutDir specs/012-subagent-context-cache/benchmarks/runs/after
scripts/bench_011_report.ps1 -Baseline runs/baseline -After runs/after
```

Accept when: SC-001 ≥60% (or P1-adjusted target), SC-002 ≥30% read reduction with zero still-current re-reads, SC-004 ≥25% cost reduction, SC-005 targets (P2-adjusted), SC-006 ≤10%, SC-007 completion parity, SC-008 100% decision coverage — each from provider-reported fields, cold/steady reported separately.

## 4. Unit/integration gates (no live cost)

```powershell
go test ./internal/orchestrator/ -run 'ContextLink|ReadGate|Handoff|Continuation' -count=1
go test ./internal/gateway/ -run 'Replay|Determinism|Marshal' -count=1   # extended goldens
go test ./... -count=1                                                    # full suite + arch guards
```

The one-time cache epoch (PH-1 handoff relocation + `read_plan` tool) regenerates instruction/wiring goldens via the existing `-update` / `-update-prefix-golden` flags in a single reviewed commit.
