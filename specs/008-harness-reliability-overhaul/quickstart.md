# Quickstart — Validating Feature 008 End-to-End

Runnable validation scenarios proving the feature works. Contracts:
[delegation-guidance](contracts/delegation-guidance.md),
[model-switch-warning](contracts/model-switch-warning.md),
[usage-display](contracts/usage-display.md).

## Prerequisites

- Client repo `F:\MuhiyaCode Agent Go`; `go build ./... && go vet ./... &&
  go test ./... -count=1` green
- Live Muhiya gateway + key configured in `~/.muhiya` (benchmark scenarios)
- The two scripted workloads under `specs/008-harness-reliability-overhaul/benchmarks/`
  (delegation workload + low-effort control)

## 1. Delegation benchmark — before/after (SC-001, SC-002 → US1)

```text
1. BEFORE leg (pre-feature build): run the delegation workload at max effort,
   pinned model/gateway; save usage records + TaskStats JSON to benchmarks/before/.
   (The real-world reference shape: 32 main-loop requests, 0 subagents.)
2. AFTER leg (feature build): identical run → benchmarks/after/.
3. Assert from records:
   - AgentRuns ≥ 2 (subagents actually used)
   - final main-stream PromptTokens ≤ 75% of the before-leg's
   - billed tokens (Σ miss + Σ completion) ≤ before + 10%
   - steady-state hit rate ≥ before (no cache regression)
   - correctness checklist: every expected sub-scope's change applied
   - duplicate-read count not higher than before (DG-13/14)
```

## 2. Gratuitous-delegation control (SC-003 → US1)

```text
Run the single-file control workload at LOW effort on the feature build
→ AgentRuns == 0; task completes correctly.
```

## 3. Edit-fumble bounded recovery (DG-9..11 → US1)

```text
Scripted-provider unit run: provider repeatedly emits near-miss edit_file calls
(oldString never matches).
→ Each outcome now counts as a failure; loop-guard nudge fires; the
  failure terminator stops the task within the existing 8-failures/6-turns bound;
  the "closest region" note text is unchanged; NO token-ceiling message exists.
```

## 4. Model-switch warning matrix (SC-004 → US2)

```text
Manual/TUI-driven checks on a session with ≥1 completed turn:
1. /model → pick a DIFFERENT main model → warning appears (role, old→new IDs,
   cold-cache + pricing text); Cancel is the default-selected choice.
2. Choose Cancel (and separately press Esc) → modal closes; settings file,
   active model, and invalidation ledger unchanged.
3. Re-open and Proceed → model switches; exactly ONE model-switch invalidation
   event recorded (visible in /context).
4. Same-model reselect → no warning, no event.
5. Fresh session (zero requests) → pick a different model → no warning, applies
   immediately.
6. Subagent role: repeat (1)–(3) via the subagent picker → warning names the
   subagent role.
```

## 5. Honest headline verification (SC-005 → US3)

```text
1. Run one task against the live gateway (cache metrics present).
   → Live line + summary show miss+output as the token figure and `cache N%`;
     the number reconciles exactly with /context's uncached total for the task.
2. Simulated provider without cache fields (scripted test).
   → Headline falls back to TotalTokens; tag shows unavailable.
3. Interrupted stream (estimated usage) → estimate markers render as today.
```

## 6. Usage panel & categories reconciliation (SC-006 → US4)

```text
1. Session with ≥2 tasks across TWO models (switch once, accepting the warning).
2. Open /context:
   - Session panel: cost credits (or "N of M priced"), API time, active time
     ("this session"), lines +A −R, hit rates.
   - Per-model table: one row per model + Total; every figure reconciles with
     usage.jsonl records (spot-check by summing the JSONL).
   - Categories: tokens+% for system prompt / tools / project memory & skills /
     conversation / summary / free; percentages sum to 100% ± 1pt of the window.
   - Unavailable metrics render as unavailable (never zero-as-unknown).
3. Resume the session → per-model table + API time rebuild from records; active
   time and lines± restart at zero with the "this session" label.
4. Narrow terminal (≤80 cols): every panel line ≤72 cols, no wrapped columns.
```

## 7. Regression gates (all stories)

```text
- go fmt (no diff) · go vet ./... · go test ./... -count=1 — all packages green.
- Prefix-stability + marshal-determinism tests green WITH the new delegation
  text (one recorded upgrade epoch only).
- Existing rider-budget test still bounds the tail (AutoReview nudge included).
- Denial texts and effort→allowance numbers byte-identical to today.
```
