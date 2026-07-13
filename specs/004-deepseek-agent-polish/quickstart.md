# Quickstart: Validating 004 — Coding Agent Quality Polish & DeepSeek V4 Optimization

**Feature**: `004-deepseek-agent-polish` | **Plan**: [plan.md](plan.md) | **Contracts**: [contracts/](contracts/)

This is the end-to-end validation guide. Implementation detail lives in tasks.md; formulas and rules live in the contracts. Order matters: **§4.1 baselines are captured before any behavior change lands** (research D9).

## 1. Prerequisites

- Go 1.25.0 toolchain; repository root `F:\MuhiyaCode Agent Go`.
- A MuhiyaLLM gateway key with access to the launch models (`deepseek-v4-pro`, `deepseek-v4-flash`) — live runs bill real credits.
- Windows Terminal or equivalent for TUI scenarios (80×24 baseline).

## 2. Static gates (every change wave)

```powershell
go fmt ./... ; go vet ./... ; go test ./... -count=1
```

Constitution IX gate. The prefix-affecting commit (prompt bump, [prompt-architecture §4](contracts/prompt-architecture.md)) must additionally keep `prompt_stability_test.go`, `restart_determinism_test.go`, `cachehit_guard_test.go`, and the new size-budget test green **in the same commit**.

## 3. Scripted suites (one per story)

Run individually while implementing; all must be green before the after-benchmark. Expected pre-implementation state: the lifecycle/terminal/mode suites are **red** where they codify the reported defects — commit that red-baseline evidence per D9.

| Suite | Command | Proves (spec ref) |
|---|---|---|
| Plan lifecycle | `go test ./internal/orchestrator -run TestPlanLifecycle -count=1` | T1–T12 transitions; natural-phrasing execution (T8); supersede; legacy-snapshot correction; resume matrix ([plan-lifecycle §6](contracts/plan-lifecycle.md)) — US2/SC-001 |
| Mode scenarios | `go test ./internal/orchestrator -run TestModeScenario -count=1` | blocked-with-feedback strings; read-only MCP admission; `agents=0` first-denial close; mid-session flip; recurrence ≤2 ([mode-capability §7](contracts/mode-capability.md)) — US3/SC-002 |
| Terminal states | `go test ./internal/orchestrator ./internal/tui -run 'TestTerminalState|TestTaskEndSweep' -count=1` | ToolEnd pairing on all exits; TUI sweep idempotence ([completion-terminal §4](contracts/completion-terminal.md)) — US1/SC-004 |
| Completion audit | `go test ./internal/orchestrator -run TestCompletionAudit -count=1` | C1–C4 rules incl. pattern guard-negatives — FR-017 |
| Wire robustness | `go test ./internal/gateway -run 'TestSalvage|TestEmptyCompletion|TestToolChoice' -count=1` | salvage shapes + one-per-turn bound; empty-completion ladder; `tool_choice` invariant ([deepseek-wire §9](contracts/deepseek-wire.md)) |
| Tool rows | `go test ./internal/tui -run 'TestToolRow|TestTruncateMiddle' -count=1` | ordering; `apply_patch` derivation; filename-preserving truncation ([tool-row-target §5](contracts/tool-row-target.md)) — US5/SC-007 |

## 4. Live verification (constitution X)

### 4.1 Baseline capture (FIRST — before any behavior change)

```powershell
cd benchmarks/cachebench
go run . -scenario coding-session -runs 3 -out ..\..\specs\004-deepseek-agent-polish\benchmarks\baseline\
go run . -scenario fat-context   -runs 3 -out ..\..\specs\004-deepseek-agent-polish\benchmarks\baseline\
```

Supply the price flags so cost is derived; record the wall-clock window (peak/off-peak 2× pricing — token counts are the primary comparator, [deepseek-wire §7](contracts/deepseek-wire.md)). Capture per-run: steady-state hit rate, prefix-stability rate, tokens, cost, invalid-call count, redundant-call count, manual interventions.

### 4.2 Replay probe (blocking gate for the D5 group)

Execute the multi-turn tool-call chain probe from [deepseek-wire §3](contracts/deepseek-wire.md) against both launch IDs; write verdict + evidence to `benchmarks/replay-probe.md`. The contingency is built only on a `replay-required` verdict.

### 4.3 After runs + comparison (identical config, matched time window)

```powershell
go run . -scenario coding-session -runs 3 -out ..\..\specs\004-deepseek-agent-polish\benchmarks\improved\
go run . -scenario fat-context   -runs 3 -out ..\..\specs\004-deepseek-agent-polish\benchmarks\improved\
go run . -compare ..\..\specs\004-deepseek-agent-polish\benchmarks\baseline\ ..\..\specs\004-deepseek-agent-polish\benchmarks\improved\
```

**Merge gates** (research D9): prefix stability ≥99% on every run; steady-state hit rate ≥ baseline (variance ≤1.0 pp); median tokens and cost per completed task ≤ baseline; invalid + redundant calls per session −50% vs baseline (SC-003); ≥95% intervention-free (SC-008); every scripted suite green (SC-001/002/004/007).

## 5. Manual TUI walkthrough (10 minutes, pre-signoff)

1. `/plan` on → draft a 3-step plan → plan-ready modal → **Proceed later** → quit → relaunch: pending notice shows. Say **"proceed with the plan"** (natural phrasing) → plan executes → completion: progress line gone, no executable hint. Relaunch: silent (SC-001's resume clause).
2. Re-enter plan mode over the finished plan → new plan → approve: old plan reads superseded, exactly one lifecycle active.
3. Mid-execution press Esc → status reads partially executed (N/M); say "proceed" → resumes; finish → finished.
4. In plan mode ask for a file edit → observe the block + corrective feedback; watch the model adjust without repeating >2×.
5. Trigger a Write/Edit/apply_patch in a narrow (≤100-col) window → rows read `label → path (filename visible) → +A −R`; Ctrl+O keeps the same header.
6. Kill the terminal mid-task → relaunch → no spinner/"running" artifacts; plan phase truthful.

## 6. Audit & docs closure

- [audit.md](audit.md): every FR-012 category has findings with dispositions (`fixed:<task>` / `deferred:<rationale>`) or a no-defect sweep note; SC-009's traceability check — each model-specific adaptation cites a Part B finding.
- Docs updated in-change (D11): `README.md`, `docs/agent-design.md`, `docs/architecture.md`, `docs/prompt-caching.md` (incl. replay verdict + legacy-ID retirement note).
- Signoff: record gate outcomes in `benchmarks/signoff.md` (whole-picture reporting — hit rate **and** totals, constitution VI).
