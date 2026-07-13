# Harness Audit: Coding Agent Quality Polish & DeepSeek V4 Optimization

**Feature**: `004-deepseek-agent-polish` | **Created**: 2026-07-12 | Seeded from [research.md](research.md) Part A.

Every confirmed finding is either **fixed** (with the implementing task and verifying test) or **deferred** (with a recorded rationale), per FR-012 / SC-009. The seven FR-012 categories are covered below; each cites at least one finding or an explicit no-defect sweep note.

## Category: unfinished task / plan states

| ID | Symptom | Root cause (file:line) | Disposition |
|---|---|---|---|
| A2 | A completed plan keeps advertising "say 'proceed' to execute it" forever | `pendingPlan` had no terminal transition tied to execution; only a bare-token regex or the modal cleared it ([classify.go:45](../../internal/orchestrator/classify.go), [engine.go](../../internal/orchestrator/engine.go) pending-injection) | **fixed** — T022–T029 (PlanPhase machine, step-progress execution detection T8, finalize stamping T10/T11). Verified: `plan_lifecycle_test.go` (12 tests incl. `TestPlanLifecycleProceedThenFinished`, `TestPlanLifecycleStepProgressDetectsExecution`) |
| A3 | Multiple UI surfaces re-emit the executable hint on every launch/resume | Hint surfaces keyed on the raw `pendingPlan` flag, not lifecycle truth | **fixed** — T029 affordance matrix (NewEngine notice, `view.go` progress line phase-gated). Verified: `plan_lifecycle_test.go` resume tests + `TestPlanLifecycleFinishedResumesSilent` |
| A6 | TUI tool/subagent items stick at "running" after cancel/error | No task-end reconciliation of work-item state ([model.go](../../internal/tui/model.go)) | **fixed** (TUI safety net) — T013 `sweepRunningWork` on `statsMsg`. Verified: `sweep_test.go`. **Engine-side ToolEnd pairing (T012)** — *deferred*: the existing gate already emits `end(output)` on the known cancel/block paths; a full defer-based guarantee across every dispatch exit is a larger change to the dispatch wrapper and is not required for the TUI-visible guarantee (SC-004) that the sweep already delivers. Rationale recorded for a follow-up. |

## Category: incorrect / dishonest completion

| ID | Symptom | Root cause | Disposition |
|---|---|---|---|
| A7 | The final answer can imply work the model did not do | `finalize` never reconciled the answer against recorded plan/step state ([engine.go](../../internal/orchestrator/engine.go) finalize) | **fixed** (C2 disclosure) — T014 `appendCompletionDisclosure` discloses open plan steps. Verified: `completion_audit_test.go`. **C3 check-claim reconciliation** (answer claims checks ran while `ChecksRun==0`) — *deferred*: `checksRun` is a Run-loop local not available in `finalize` without threading a new task-scoped counter; the higher-value plan-step disclosure (C2) is shipped and covers the primary over-claim mode (research B3). Follow-up: thread `taskChecksRun` for C3. |

## Category: invalid tool calls

| ID | Symptom | Root cause | Disposition |
|---|---|---|---|
| A5.2 | Unknown/hallucinated tool names get a bare "unknown tool" with no correction path | `registry.Execute` had no suggestion ([registry.go:150](../../internal/orchestrator/registry.go)) | **fixed** — T034 nearest-name suggestion (Levenshtein ≤3, deterministic). Verified: `mode_scenario_test.go` `TestUnknownToolSuggestsNearest` |
| — | Malformed-argument feedback | Already actionable (`validateCallArgs` + "Re-emit the call with well-formed arguments.") | **no defect** — existing behavior meets FR-015; retained |

## Category: incorrect mode transitions / out-of-mode attempts

| ID | Symptom | Root cause | Disposition |
|---|---|---|---|
| A5.1 | Read-only MCP tools are blocked in plan mode | `isMutation` treats every `mcp__` tool as mutating ([subagent.go:294](../../internal/orchestrator/subagent.go)); MCP `readOnlyHint` annotations are never captured | **partially fixed / deferred** — the plan-mode MCP block now names the allowed alternative (T033 wording, verified by existing plan-mode tests). Full read-only *admission* (T032: capture `annotations.readOnlyHint` in `mcpclient`, admit annotated tools) is **deferred**: it requires MCP-manager plumbing whose end-to-end behavior can only be verified against a live MCP server advertising the annotation, which this offline pass cannot exercise. Fail-closed default (absent → mutating) means deferring is safe. |
| A5.4 | `propose_changes` tells the model "approved" with no human review | Non-interactive path returned a plain approval verdict ([engine.go](../../internal/orchestrator/engine.go) proposeChanges) | **fixed** — T036 honest `auto_approved` label. Verified: `mode_scenario_test.go` `TestProposeChangesNonInteractiveIsHonest` |
| — | Delegated subagents were not told their exact toolset/boundary | Mission brief lacked a capability statement | **fixed** — T038 `capabilityStatement` (sorted toolset + boundary + report contract). Verified: `mode_scenario_test.go` `TestSubagentCapabilityStatement` |

## Category: delegation problems

| ID | Symptom | Root cause | Disposition |
|---|---|---|---|
| A5.3 | With `agents=0`, the hard "stop calling run_subagent" message only appears on the *second* denial | Denial ladder gated the hard message on `denied>1` regardless of whether any budget existed ([subagent.go](../../internal/orchestrator/subagent.go)) | **fixed** — T035 first-denial hard-close when `agents=0`; mid-task exhaustion still escalates soft→hard. Verified: `subagent_budget_test.go` `TestSubagentDenialEscalates` (rewritten to the new semantics) |
| — | Delegation briefs tuned to worker strengths | Research B7/D10 | **fixed** — T038 report-contract line ("flag ambiguity/architectural choices back to the caller") |

## Category: repeated / unnecessary work

| ID | Symptom | Root cause | Disposition |
|---|---|---|---|
| A5.5 | The duplicate-read block returns `Failed:false`, so it feeds none of the loop breakers | Design choice ([engine.go](../../internal/orchestrator/engine.go) duplicate-read guard) | **deferred (no change)** — the separate repeat limiter (`repeats>3`) already bounds a re-read loop, and making the duplicate-read block feed the failure breakers risks tripping the storm/failure terminators on a non-failure signal. Retained as-is; recorded rationale. Existing behavior verified by the current inspection tests. |

## Category: DeepSeek wire robustness (research B4/B6 → D5)

| ID | Symptom | Disposition |
|---|---|---|
| D5-replay | Thinking-mode `reasoning_content` replay may 400 on multi-turn tool chains | **deferred (verification-gated)** — T005 live probe against the launch gateway is required before any change (research B6 conflicts with the working live system). The current empty-string replay is retained as the safe default; T017 contingency builds only on a `replay-required` verdict. Cannot be run offline. |
| D5-salvage | Tool calls intermittently emitted as text in `content` | **deferred** — the `NeedsToolCallRescue` path exists; hardening it against the documented shapes (JSON-in-content, DSML, language-prefixed) needs live DeepSeek traffic to validate the detector against real failures. Recorded for the live phase. |
| D5-empty | Zero-token empty completions after tool results | **deferred** — detection + bounded retry is designed ([contracts/deepseek-wire.md](contracts/deepseek-wire.md) §5); validating it needs the live failure mode. Recorded for the live phase. |
| tool_choice | Thinking-mode `tool_choice:"required"` returns 400 | **no defect** — `tool_choice` is already pinned `"auto"` on every main-loop request and covered by `cachehit_guard_test.go` / `request_assembly_test.go`; the invariant is retained. |
| legacy IDs | `deepseek-chat`/`deepseek-reasoner` retire 2026-07-24 | **open (T006)** — config/docs sweep for legacy IDs; carried into the docs-sync task. |

## Prompt optimization (FR-018, US4)

| Item | Disposition |
|---|---|
| Priority rule + re-read dedup + completion-honesty addendum | **fixed** — T041/T042(a,b)/T043; net size 3625 ≤ 3642 baseline, determinism preserved. Verified: `prompt_budget_test.go`, `prompt_stability_test.go`, `restart_determinism_test.go` |
| Broad tool-description rewrite (T042c) | **deferred** — the highest-risk-per-value prefix change; it most needs the live before/after cache benchmark (T049/T050) to confirm no hit-rate regression, which this offline pass cannot run. Recorded for the live phase. |

## Verification note (constitution X)

The live measurement tasks — baseline benchmarks (T003/T004), the `reasoning_content` replay probe (T005), the after-benchmarks (T049/T050), and the manual TUI walkthrough (T051) — require the MuhiyaLLM gateway, DeepSeek credits, and a live endpoint. They are **owned by the operator** and are not runnable in this offline implementation pass. Every code change shipped here is verified by `go test ./... -count=1` (all packages green) and `go vet ./...` (clean). The one prefix-affecting change (US4) is verified deterministic offline; its cache-hit validation is the operator's live-run gate.
