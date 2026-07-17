# delegationbench

Live delegation benchmark for feature 008: runs one scripted workload through
the real agent engine against the live gateway and records whether — and how
efficiently — the run delegated to subagents. Reuses the cachebench harness
patterns (isolated benchmark home, throwaway workspace copied from the
fixture, pinned model, auto-accepted permissions, JSON + raw usage output).

## Usage

```sh
go run ./benchmarks/delegationbench \
  -workload delegation \            # delegation | control
  -build-label before \             # before | after
  -out specs/008-harness-reliability-overhaul/benchmarks/before \
  -model <pinned-concrete-model-id> # defaults to the configured active model
```

Optional flags: `-effort` (defaults to `max` for the delegation workload and
`low` for the control workload), `-workload-dir` (default
`specs/008-harness-reliability-overhaul/benchmarks`), `-fixture` (default
`<workload-dir>/fixture`), `-timeout` (default 45m for the single prompt).

Workload files (`delegation-workload.md`, `control-workload.md`) carry exactly
one numbered prompt plus `expect:`/`check:` verification lines; only the
prompt is sent to the model, and the workload file is never copied into the
run workspace, so the agent cannot read the checklist.

Output: `<out>/<label>-<workload>.json` (metrics below) and
`<out>/<label>-<workload>-raw-usage.jsonl` (raw provider usage stream).

## Metrics

| Field | Source |
|---|---|
| `agentRuns`, `agentRunsReused`, `turns` | `contract.TaskStats` returned by `Engine.Run` |
| `mainToolCalls`, `subagentToolCalls`, `toolCalls` | audited tool-start events (both scopes; see below) |
| `finalMainPromptTokens` | `PromptTokens` of the last main-stream `contract.UsageRecord` (empty `Stream` counts as main, same rule as `contract.AggregateUsage`) |
| `billedTokens` | Σ `CacheMissTokens` + Σ `CompletionTokens` over all usage records; a record without `CacheMissTokens` falls back to `PromptTokens` and sets `billedTokensEstimated` + `billedTokensFallbackRecords` |
| `steadyStateHitRate` | `Engine.UsageAggregate().SteadyStateHitRate` (nil when not derivable) |
| `filesChanged` / `filesDeleted` | ground truth: sha256 snapshot of the run workspace before vs after the run — covers main-loop AND subagent writes |
| `engineFilesChanged` | `TaskStats.FilesChanged` for comparison; the engine only tracks main-loop mutating calls (`trackChanged` is main-scope), so subagent writes are missing from it by design |
| `checklistResults` / `checklistFailures` | post-run case-insensitive greps of the workspace for every `check:` line in the workload file |
| `writeFileAudit`, `duplicateReads`, `toolEvents` | the tool-call audit (below) |
| `taskStats`, `usage_records`, `aggregate` | full copies for offline analysis (T004/T015 comparisons) |

## Tool-call audit mechanism

The engine does not centrally log individual tool names into its history or
usage records, so the runner audits tool calls through the engine's existing
**read-only observation callbacks** — the least invasive mechanism available,
requiring zero engine changes:

- **Main loop:** `contract.Callbacks.ToolStart(name, argsJSON)` is invoked by
  the shared dispatch gate (`Engine.gatedExecute` via `mainScope.onStart`)
  synchronously BEFORE validation and execution of every attempted call.
- **Subagents:** the same gate's subagent scope emits
  `contract.Callbacks.Agent(AgentEvent{Kind: "tool_start", Tool, Arguments})`
  for every attempted subagent call (subagents were the alternative blind
  spot; this covers them symmetrically).

Because both callbacks fire before the tool runs, the auditor can stat a
`write_file` target inside the run workspace *at call time* to decide whether
the call would overwrite a pre-existing file:

- `writeFileAudit.totalCalls` — every attempted `write_file` in both scopes.
- `writeFileAudit.overwritesOfExistingFiles` — attempted `write_file` calls
  whose target file already existed when the call was made (the SC-008
  "whole-file write where an edit sufficed" metric).
- `duplicateReads` — attempted read-only calls (`read_file`, `list_files`,
  `grep`, `search_text`, `glob`, `git_status`, `git_diff`) whose exact
  tool+arguments signature was already seen earlier in the run (DG-14),
  broken down by scope. Signatures use the raw arguments JSON, matching the
  engine's own H2 failed-call signature approach.
- `toolEvents` — the per-call event list for manual inspection.

Known caveats (documented rather than papered over):

- Counts are **attempted** calls: the gate fires the callback before H1
  argument validation, the H2 failed-call cache, and plan-mode checks, so a
  blocked call is still counted. For SC-008 this errs on the strict side (an
  attempted rewrite is still a discipline miss). Cross-check against
  `filesChanged` (snapshot diff) when a landed-writes-only view is needed.
- Two concurrent subagents racing on the same path could mis-stat existence
  for one event; the snapshot diff is immune to this.
- MCP tools are excluded from the duplicate-read audit (unknown argument
  semantics).

Rejected alternatives: wrapping the tool `Registry` (needs engine wiring
changes in `internal/`, off-limits while other 008 work is in flight) and
parsing persisted transcript events from the benchmark home's session store
(indirect, schema-coupled, and no richer than the callbacks for this purpose).
