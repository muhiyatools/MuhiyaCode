# Quickstart — Validating Feature 010 End-to-End

Runnable validation scenarios. Contracts: [unified-lifecycle](contracts/unified-lifecycle.md),
[module-structure](contracts/module-structure.md),
[instruction-system](contracts/instruction-system.md),
[fault-injection](contracts/fault-injection.md),
[wiring-inventory](contracts/wiring-inventory.md).

**Provider split (standing directives)**: DeepSeek is the only LIVE provider
(respect the account budget window; record "budget-blocked" rather than
fabricate); MiniMax remains simulated-fixture-only.

## Prerequisites

- `F:\MuhiyaCode Agent Go`: `go build ./... && go vet ./... && go test ./... -count=1` green
- BEFORE captures pinned: current DeepSeek conformance goldens, prompt-budget
  baseline, steady-state cache-hit figure, `deadcode` both-invocation output,
  file-size list, `git tag` or commit marking the pre-rework tree

## 1. One lifecycle, one truth (SC-001 → US1)

```text
1. go test ./internal/orchestrator -run 'Lifecycle|Plan|Pipeline' — all
   lifecycle/resume/gate suites green after unification (renames only).
2. Grep proof: legacy flags (planMode, pendingPlan, PlanPhase*, PipelinePhase*)
   appear ONLY in the migration loader + its tests → SC-001.
3. Sidecar migration: fixture sidecars from each legacy era (P2 flags-only,
   004 phase, 009 pipeline_phase+depth) restore to the correct unified state
   with the correct one-shot notice; new sidecars carry `state` only.
4. Interrupt/restart at every state resumes that state (existing
   resume-from-every-phase test, extended to all 11).
```

## 2. Stability is a property (SC-002 → US2)

```text
go test ./internal/orchestrator -run FaultInjection -count=1
- 100% of catalog rows pass assertRecoveryInvariant (three outcomes, liveness).
- The mutation-guard check fails the suite when a bounded-recovery constant is
  flipped (proves the invariant bites) — FI-14.
```

## 3. Instruction coherence (SC-003 → US3)

```text
go test ./internal/instructions -count=1
- Audit: zero contradictions; zero unavailable-capability references; every
  formatted deliverable has a validator-passing worked example.
- Golden dump reviewed; the Prefix-bytes golden shows exactly ONE update in
  the release diff (= the recorded epoch, FR-018).
```

## 4. Structure & dead code (SC-004 → US4)

```text
1. go test ./internal/arch -count=1 — size budget (≤800, empty allowlist) and
   layering (allow-map + cycle check) green.
2. GOFLAGS=-mod=mod go run golang.org/x/tools/cmd/deadcode@latest ./...   → empty
   (and with -test → empty).
3. Removal Ledger complete: every RL-### row has proof or migration note.
```

## 5. Wiring (SC-005 → US5)

```text
go test ./... -run WiringInventory — live surface == inventory, both directions;
no entry without Verified-by; Density/CallID/Turns/ToolCalls resolved
(wired or removed) per WI-5..7.
```

## 6. Preservation & measurement (SC-006/SC-008)

```text
1. Full suite green: go test ./... -count=1 (also at EVERY merge point during
   the rework — never-broken rule).
2. DeepSeek conformance capture byte-identical to the BEFORE goldens
   (DEEPSEEK_GOLDEN_DIR run); prompt-budget/prefix-stability tests green with
   the single sanctioned epoch delta.
3. Live delegation benchmark (budget permitting): plan-only execution 8/8;
   steady-state cache hit within 1 point of the BEFORE figure. Cold vs steady
   reported separately; simulated figures labeled.
```

## 7. The live acceptance test (SC-007)

```text
Rebuild the binary; run a fresh real session end-to-end
(research → plan → approve → implement → validate) on a non-trivial workspace.
PASS = zero harness-caused error messages in the transcript. Any snag becomes
a new fault-catalog row before the release ships.
```
