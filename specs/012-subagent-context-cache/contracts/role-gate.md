# Contract: Phase-Scoped Role Gate

**Feature 012** · The main-model implementation-read gate. Implements FR-006 (Clarification Q4); decision R-D8. Joins the codified gate-policy inventory (gatepolicy.go) as a new row satisfying all five clauses.

## RG-1 Activation predicate (all must hold)

```
Lifecycle.Orchestrated() && Depth == full && State == implementing
  && !phaseDegraded && !postFailureDiagnosisActive
  && Settings.ContextLinking != "off"   // feature kill switch also disables the gate
```

Light-depth pipelines, validating state, degraded/recovery paths, and manual plan mode are **never** gated (their advance-notice texts promise main-model reads — clause (a) consistency, R-F18 precedent).

## RG-2 Gated surface

- `read_file` (full-content reads), and `run_shell` commands matching the existing terminal-read classifier (`cat/type/head/tail/less/more/Get-Content/gc`) — same regex, now denying under RG-1 instead of counting only.
- **Not gated**: `grep`, `search_text`, `glob`, `list_files`, `git_status`, `git_diff`, `read_plan`, subagent reports, memory tools. Discovery is not implementation reading; the denylist philosophy follows the shellclassify precedent (allowlist gates documented as a failure class).
- Subagent scopes are never gated (dispatchScope discrimination — same chokepoint flag pattern as dedupe/trackStats).

## RG-3 Denial & bounds (gate-policy clauses)

- Denial produces a well-formed toolOutcome (RoleTool result), Failed:true, guidance text registered in the instructions registry (Sidecar class): *"Implementation phase: file reading belongs to the implementation subagents. Dispatch the work (run_subagent) or wait for the phase report. [read allowance N of 2]"*.
- **Bounded ≤2 denials per task**; the third attempt is accepted with a recorded degradation `read-gate-waived` and the gate stays open for the task's remainder (clause (c); fault-injection invariant holds).
- Exempt reads and waivers are telemetered (`phase-read-block`, `phase-read-waived`, `phase-read-exempt:<reason>`) via recordHarnessEvent; counts land in `TaskStats.ReadGate`.

## RG-4 Post-failure diagnosis exemption

- The first non-succeeded implementation dispatch in the phase sets `postFailureDiagnosisActive` for the phase's remainder; main-model reads flow freely, each recorded `phase-read-exempt:post-failure` (Clarification Q4; the orchestrator is never blind after a failure — Constitution I).
- `canOverwrite` interaction: impossible deadlock — the gate never applies where the main model implements (light depth, degraded, post-waiver), and read-before-edit paths in full-depth belong to subagents.

## RG-5 Instruction alignment

- The main-model pipeline prelude for full-depth implementing states the rule in advance (clause (a)): reads are dispatched, not performed; the gate text names the one-step fix (dispatch or await). PromptDelegation section updated in the same reviewed prompt epoch as PH-1 (budget test re-baselined once).

## RG-6 Tests pinned by this contract

- Predicate table: every RG-1 conjunct flips the gate (unit).
- Shell bypass: `run_shell "cat file"` denied under RG-1; `grep` passes.
- Bound: denials 1–2 deny, 3rd waives with recorded degradation; fault-injection recovery invariant passes.
- Exemptions: failed dispatch opens the gate; light-depth and degraded phases never gate (regression against R-F18's advance-notice texts).
- Telemetry: every outcome lands in TaskStats.ReadGate + harness events.
