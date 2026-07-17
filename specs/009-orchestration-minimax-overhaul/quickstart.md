# Quickstart — Validating Feature 009 End-to-End

Runnable validation scenarios. Contracts: [pipeline-state](contracts/pipeline-state.md),
[handoff-contract](contracts/handoff-contract.md),
[minimax-provider](contracts/minimax-provider.md),
[default-models](contracts/default-models.md).

**Provider split (user directive):** DeepSeek is the LIVE benchmark provider;
MiniMax is verified against a **recorded/simulated upstream fixture** — no live
MiniMax spend — and every MiniMax figure is labeled simulated.

## Prerequisites

- Client repo `F:\MuhiyaCode Agent Go`; gateway `F:\MuhiyaWorkspace\MuhiyaWorkspace`;
  `go build ./... && go vet ./... && go test ./... -count=1` green in both
- Live DeepSeek gateway + key in `~/.muhiya` (respect the account budget window)
- The feature-008 `delegationbench` workload/runner (reused + extended for phases)
- The MiniMax simulated upstream fixture (`proxy/minimax_fixture_test.go`)

## 1. Enforced pipeline — live before/after (SC-001, SC-003 → US1/US2)

```text
1. BEFORE leg exists (feature 008 before/): 4-module max-effort task, 0 subagents.
2. AFTER leg (feature build): run the SAME delegation workload at max effort.
3. Assert the pipeline is OBSERVED end-to-end from the run's records/transcript:
   - ≥2 research subagent runs complete BEFORE any file-changing tool call
   - a plan.md artifact exists on disk BEFORE the first implementation edit
   - ≥2 implementation subagent runs execute plan steps
   - ≥1 validation/review run completes before the final answer
   - final answer is correct (8/8 checklist)
4. Plan quality: a fresh run where implementation subagents execute ONLY the plan's
   steps passes 8/8; final main conversation ≤50% of the no-pipeline baseline size.
```

## 2. Simple-task fast path unchanged (SC-002 → US1)

```text
Run the low-effort single-file control workload → 0 subagents, 0 plan file,
correct completion, turns/time within +10% of the current control baseline.
```

## 3. Approval pause overrides auto-accept (PL-9..12 → US1)

```text
1. Auto-accept mode ON, a pipeline task: after the plan is written the approval
   modal appears (approve / keep planning / cancel) — it does NOT auto-proceed.
2. Cancel → plan persists Pending, task ends; later "proceed" resumes implementation.
3. Headless (muhiyacode -p "<complex task>") → writes plan, saves Pending, prints
   the proceed instruction; never implements.
4. Resume: restart mid-implement → phase + plan recovered, execution continues.
```

## 4. Handoffs & no-premature-finish (SC-004, PL-7 → US3)

```text
1. Audit the AFTER-run transcripts: 100% of subagent prompts contain role +
   deliverable + output format + scoped context; 0 wholesale re-reads of
   predecessor-covered scopes (duplicate-read counter ≤ baseline).
2. Scripted-provider test: a run that leaves a plan step incomplete stamps
   Interrupted (resumable), NEVER a false Finished; no token-ceiling message.
```

## 5. MiniMax conformance — SIMULATED (SC-005 → US4)

```text
Against the recorded MiniMax upstream fixture (NO live spend):
1. ≥15-request multi-turn tool-calling session → 0 request-shape rejections; call
   IDs/args/results preserved; reasoning_details preserved on replay (MX-6).
2. 100% usage records carry token counts; above a 512-token prefix, cached_tokens
   present; a repeated-prefix probe → warm cached share ≥50% on the 2nd request.
3. Tiered M3 cost unit test: ≤512k vs >512k input rates + cached discount correct.
All MiniMax outputs LABELED "simulated". Live run deferred until the user funds the
MiniMax API (swap the fixture base for the real endpoint — one flag).
```

## 6. Mixed-provider & defaults (SC-006, FR-022 → US5)

```text
1. Config main=DeepSeek, sub=MiniMax(simulated) [and the reverse]: run the
   delegation workload → completes 8/8; per-model usage rows show BOTH providers;
   each provider's steady-state cache within 5 points of its single-provider
   baseline. (DeepSeek live; MiniMax simulated — mixed run uses the fixture for the
   MiniMax stream.)
2. Fresh-install default: discovery reporting both M3 and V4Pro → M3 main + V4Pro
   sub; DeepSeek-only discovery → today's defaults unchanged; pre-pinned → untouched.
```

## 7. Regression gates (SC-007, all stories)

```text
- Both repos: go fmt (no diff) · go vet ./... · go test ./... -count=1 — green.
- 008 guards byte-stable: denial texts, brief format, prefix-stability/marshal-
  determinism (now covering the pipeline prompt epoch + the MiniMax replay variant),
  loop guards, no token ceiling.
- DeepSeek-only conformance capture identical to pre-feature (FR-018).
- Existing plan-mode, goals, and memory-tool behavior unchanged for non-pipeline use.
```
