# Feature 008 — Benchmark Comparison & Sign-off

**Date**: 2026-07-14. Protocol: research.md R7 / quickstart.md §1–§2. Both legs run
the identical workload, model (`deepseek-v4-flash`, pinned), gateway, and effort;
only the build varies (before = clean HEAD `72c7d51` in a worktree; after = the
feature working tree). Runner: `benchmarks/delegationbench`.

## T034 — Upgrade-epoch audit (contract DG-5, MT-3 note)

All static-prefix text changes land in ONE release, therefore ONE cache epoch:

| Prefix surface | Change | Task |
|---|---|---|
| System prompt | DELEGATION section (replaces the hedged ENVIRONMENT clause) | T005 |
| System prompt | Edit-discipline rule (surgical edits; no whole-file rewrites) | T006 |
| System prompt | PROJECT MEMORY routes saves through `save_memory` | T030 |
| Tools serialization | `run_subagent` rich description + field descriptions | T007 |
| Tools serialization | `save_memory` definition added | T029 |

Byte-stability across constructions and turns is asserted by
`TestSystemPromptByteStableAcrossConstructions`,
`TestRunSubagentDefinitionRichAndDeterministic` (marshal-determinism), and the
pre-existing prefix-stability suite — all green post-change. The prompt-size
regression guard was re-baselined once (3642 → 4522 chars) with the epoch
justification recorded in `prompt_budget_test.go`. Within a session the prefix is
byte-identical every turn; the epoch costs one cold prefix read per session on
first upgrade, after which caching resumes (~97% warm reads re-measured live on
2026-07-14 post-007-deploy).

## T035 — Gates

`go build ./...` exit 0 · `go vet ./...` exit 0 · `go test ./... -count=1`: all 9
test packages ok (command, contract, gateway, mcpclient, orchestrator, state, tui,
workspace, terminalbench). Rider-budget check: the AutoReview rider measures ≤50
estimated tokens (asserted in `TestAutoReviewNudgeFiresOnceAtMax`).

## SC sign-off table

| SC | Criterion | Result |
|---|---|---|
| SC-001 | Benchmark: ≥2 subagent runs; final main conversation ≤75% of before | pending live legs (below) |
| SC-002 | Billed ≤ before+10%; steady-state hit rate ≥ before | pending live legs |
| SC-003 | Control workload at low effort: 0 subagent runs | pending live legs |
| SC-004 | Switch-warning matrix | ✅ `model_switch_test.go` (6 tests): trigger, Cancel-default, Esc purity, proceed dispatch, same-model, fresh-session |
| SC-005 | Headline = provider miss+output exactly; fallback = total; /context keeps splits | ✅ `usage_display_test.go` (exact-figure assertions incl. anti-leak guard) |
| SC-006 | Panel reconciles; categories sum ±1pt; ≤72 cols | ✅ `context_panel_test.go` (7 tests incl. width sweep with pathological model IDs) |
| SC-007 | Full suite + prefix checks green; single epoch | ✅ (gates above + T034 audit) |
| SC-008 | Zero whole-file rewrites of existing files on the benchmark | pending live legs (runner's writeFileAudit) |
| SC-009 | Memory renders as Memory row; restart persistence; format survives mixed writes | ✅ `memory_test.go` (10 tests incl. update-block emission) + `memory_row_test.go` (4 tests) |

## Live legs

### BEFORE (delegation workload, max effort, HEAD build `72c7d51`)

Run 2026-07-14 18:38 live against the deployed gateway, `deepseek-v4-flash` pinned:

| Metric | Value |
|---|---|
| **agentRuns** | **0** — reproduces the real-session 0-subagent behavior the feature targets |
| turns / toolCalls | 5 / 16 |
| finalMainPromptTokens | 9,040 |
| billedTokens (Σmiss+Σcompletion) | 10,810 |
| steadyStateHitRate | 86.8% |
| writeFileAudit.overwritesOfExistingFiles | 0 |
| duplicateReads | 0 |
| checklist | 8/8 correct (all four planted issues fixed) |

### AFTER (delegation workload, max effort, feature build)

**Attempt 2026-07-14 18:41 — BLOCKED by the account's own budget guard**, not by any
defect: the gateway returned `429 rate_limit_error: budget limit of $0.10 exceeded
for window 'Burst (5 Hours)' (current spending: $0.1002)` (the day's live testing
consumed the burst window). The client's 007-aligned retry behavior worked as
designed (bounded retries with backoff, then a clean rate-limit error). No usage
was recorded; 0-values in `after-delegation.json` are the aborted run, NOT a
measurement. Re-run once the burst window slides or the plan budget is raised:

```
go run ./benchmarks/delegationbench -workload delegation -build-label after \
  -out specs/008-harness-reliability-overhaul/benchmarks/after -model deepseek-v4-flash -timeout 20m
```

### CONTROL (low effort, feature build)
_blocked on the same budget window; run after the AFTER leg:_

```
go run ./benchmarks/delegationbench -workload control -build-label after \
  -out specs/008-harness-reliability-overhaul/benchmarks/after -model deepseek-v4-flash -timeout 10m
```
