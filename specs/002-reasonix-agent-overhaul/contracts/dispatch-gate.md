# Contract: Shared Tool-Dispatch Gate

**Feature**: `002-reasonix-agent-overhaul` · **Consumers**: main agent loop, subagent runner · **Owner**: `internal/orchestrator` (engine.go `gatedExecute`, per data-model §6)

Every tool execution in the process — main loop and every subagent — passes through ONE gate with identical semantics. This contract defines the gate's order, each stage's exact model-facing outputs, and its scope rules. It consolidates REV items H1/H2/H6/B2/B6/B7/B8 and P3/P4 into a single testable interface.

## 1. Gate order (fixed)

```
(0) pairing guarantee wrapper   — every announced call WILL receive exactly one result
(1) argument validation         — schema of the definitions actually sent this request
(2) mode gate                   — plan-mode mutation block (incl. read-only-shell passthrough)
(3) identical-repeat limiter    — callCounts
(4) duplicate-read guard        — inspection ledger (main scope only)
(5) failed-call short-circuit   — failedCalls signature → cached error
(6) dispatch                    — registry / synthetic tool execution
(7) outcome recording           — failure caches, storm/violation counters, ledgers
```

## 2. Stage contracts

### (0) Pairing guarantee
- The assistant message carrying tool_calls and the corresponding tool results are appended **atomically with respect to any early exit**: cancellation, `exit_plan_mode`, forced finalize, or errors NEVER leave an announced call without a result.
- Synthetic results (exact strings): cancelled → `[cancelled by user before execution]`; skipped after an exit signal → `skipped: plan mode exited`.
- **Test hook**: batch `[read_file, exit_plan_mode, write_file]` in plan mode → history contains 3 tool results; provider accepts the next request.

### (1) Argument validation (pre-dispatch, uniform)
- Validates against the `ToolDefinition.Parameters` of the definitions **sent in the current request** (never a fresher registry view).
- Checks, in order: JSON parses to an object → every `required` key present → declared primitive types match (`integer`/`number` both accept JSON numbers; unknown extra fields are **ignored**) → enum membership (numbers compare as numbers, strings as strings, bools as bools — REV B8).
- Nested objects/arrays: shallow container-type check only (documented limit).
- Error strings (exact shapes, `Failed:true`):
  - `<tool>: arguments were not valid JSON (<reason>). Re-emit the call with complete, well-formed JSON arguments.`
  - `<tool>: missing required field "<field>" (<type>). Supply every required field and retry.`
  - `<tool>: field "<field>" must be a <type>, got <actual>.`
  - `<tool>: field "<field>" must be one of <enum-values>.`
- Missing definition for a known-registered tool → validation passes through (registry decides), never a panic.

### (2) Mode gate
- Plan mode ON + mutating tool → block (`Failed:true`) with the P4 text; **exception**: `run_shell` whose parsed command satisfies `IsReadOnlyShell` passes; malformed `run_shell` args fail CLOSED (blocked).
- `mcp__*` in plan mode → `MCP tools are unavailable in plan mode.`
- Violation counter: increments on every plan-mode block regardless of tool/args; at 3, escalate once per task with the STOP-planning notice appended to the owning scope's transcript.
- `exit_plan_mode` while plan mode OFF → **harmless no-op result** (`not in plan mode — continue with the task`, `Failed:false`); it must NOT end the task nor set pendingPlan (REV B2a).
- Subagents: `general` kind is refused at RUN start while plan mode is on; per-call mutation gate still applies inside every subagent as defense in depth (REV P3).

### (3) Identical-repeat limiter
- Signature = tool name + compacted argument bytes. Threshold: successes blocked at > 3; calls whose previous outcome FAILED are blocked at > 1 (the failed-cache at stage 5 normally fires first).
- Block text: `Blocked: identical call repeated…` (existing).

### (4) Duplicate-read guard
- Main scope only (inspection ledger is main-task state); subagent scopes pass `nil` ledger. Never claims a duplicate against a trimmed/folded/degraded prior result (existing intact-check preserved).

### (5) Failed-call short-circuit
- `failedCalls[signature]` hit → immediate `Failed:true`: `This exact call already failed: <last error>. Do not repeat it verbatim — change your approach (e.g. re-read the file first).`

### (7) Outcome recording
- On failure: record signature→error; increment `failedClassCounts[(tool, normalizedError)]` — normalizedError = first line, paths/numbers stripped; at 3 → one loop-guard escalation notice to the owning scope.
- All-failed-turn streak (owning scope): every turn where ALL calls failed increments; any success resets; at 2 → loop-guard notice (REV B7).
- Token/failure breakers (main scope): subagent usage feeds INTO the parent `taskTokens` (REV B9); breaker/terminator force-finalize via the standard finalize path (stats still emitted).

## 3. Scope rules

- One `callCounters` per main task, reset at `Run` start; one per subagent RUN, independent (REV B6).
- Escalation/loop-guard notices append ONLY to the owning scope: main → main history tail; subagent → its own transcript/report. A subagent goroutine never writes parent history mid-turn.
- Concurrency: gate state is owned by its scope's goroutine; any cross-scope aggregation (taskTokens) goes through the existing task mutex. `-race` clean is an acceptance gate.

## 4. Conformance tests (minimum set)

1. Per-failure-class validation strings (all five shapes above; numeric AND string enums; extra field ignored; valid call untouched).
2. Verbatim failed repeat short-circuits on attempt 2 (main AND subagent scope).
3. Cosmetic-variation storm: 3 distinct-args failures of one tool class → exactly one escalation notice.
4. Two all-failed turns → loop-guard notice on turn 2.
5. Plan-mode: read-only shell passes; malformed shell blocked; 3 violations escalate once; `exit_plan_mode` off-mode no-op.
6. Pairing: cancellation mid-batch and exit-mid-batch both yield full result sets.
7. Subagent with malformed args receives the stage-1 actionable error, not a raw Go error.
8. Parallel read-only subagents failing concurrently: `-race` clean, notices land in their own reports.
