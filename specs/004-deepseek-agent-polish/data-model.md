# Data Model: Coding Agent Quality Polish & DeepSeek V4 Optimization

**Feature**: `004-deepseek-agent-polish` | **Date**: 2026-07-12 | **Plan**: [plan.md](plan.md)

Entities are grouped by strand. Fields marked **(new)** are introduced by this feature; everything else is existing structure being given defined semantics. State-transition detail lives in the contracts; this file is the authoritative field/validation inventory.

## 1. Plan lifecycle

### 1.1 `PlanPhase` (new type, `internal/contract`)

String enum. Values and meaning:

| Value | Meaning | Executable hint allowed? |
|---|---|---|
| `""` / `none` | No plan exists for the session | No |
| `drafting` | Plan mode on; plan being written/refined | No |
| `ready` | Plan-ready signaled; awaiting the user's proceed/save/keep-planning decision | No (modal owns the moment) |
| `pending` | Saved for later execution; genuinely awaiting "proceed" | **Yes — the only phase** |
| `executing` | A run is actively working the plan's steps | No |
| `finished` | All steps completed; terminal | No — never again |
| `interrupted` | Execution ended with incomplete steps (stop/error/breaker) | Resumable-partial wording only |
| `superseded` | Replaced by a newer approved plan; terminal | No |
| `discarded` | Explicitly cleared by the user; terminal | No |

**Validation rules**: exactly one phase at a time; terminal phases (`finished`, `superseded`, `discarded`) never transition out (a new plan starts a fresh lifecycle at `drafting`); `executing` requires plan mode off; `drafting` requires plan mode on. Unknown/absent phase values (forward/backward compat) derive per §1.3.

### 1.2 `contract.Plan` (existing) + runtime phase

Existing fields unchanged: `Steps []PlanStep`, `Note`, `UpdatedAt`. The engine holds the phase alongside its existing `planMode`/`pendingPlan` fields (single `modeMu` ownership preserved). `PlanStep.Status` stays `pending|in_progress|completed` with the existing at-most-one-`in_progress` invariant — steps remain model-owned; the harness never fabricates step completion (research D2).

**Derived predicates** (used by transitions and display):
- `allStepsCompleted` := len(Steps) > 0 ∧ every status == `completed`
- `hasIncompleteSteps` := any status ≠ `completed` (existing `hasIncompletePlan`)

### 1.3 `PlanStateSnapshot` (existing, extended) — `plan_state.json`

```json
{ "planMode": false, "pendingPlan": false, "phase": "finished" }
```

- `phase` **(new, additive)**: current `PlanPhase`. Written on every phase change; the booleans remain authoritative for their existing consumers and MUST stay consistent with the phase (validation: `pendingPlan == (phase=="pending")`, `planMode == (phase=="drafting")` after this feature's transitions land).
- **Legacy derivation** (snapshot without `phase`): `pendingPlan:true → pending`; `planMode:true → drafting`; both false → `none`. This preserves old sessions without migration (constitution VIII).
- **Sidecar retention**: file persists while phase ∈ {drafting, ready, pending, executing, interrupted}; cleared (or written terminal, see [contracts/plan-lifecycle.md](contracts/plan-lifecycle.md) §3) on terminal phases so resume never resurrects an affordance.

### 1.4 Transition triggers (summary — full table in the contract)

| Trigger (existing choke point) | Phase effect |
|---|---|
| `SetPlanMode(true)` | `* → drafting` (non-terminal `*`) |
| Plan-ready (exit_plan_mode / maybeSignalPlanReady with ≥1 incomplete step) | `drafting → ready`; an existing `pending/interrupted` plan → `superseded` first |
| Modal: Proceed now / Proceed later / Keep planning | `ready → executing / pending / drafting` |
| One-shot plan-ready (no interactive client) | `drafting → pending` |
| Pending-plan brief injection (continuation match) | `pending|interrupted → executing` |
| First `update_plan` marking a step `in_progress`/`completed` while phase ∈ {pending, interrupted, ready} and plan mode off | `→ executing` (step-progress-driven detection) |
| `finalize` with phase `executing` ∧ `allStepsCompleted` | `→ finished` |
| `finalize`/task-abort with phase `executing` ∧ `hasIncompleteSteps` | `→ interrupted` (wording keyed on `TaskStats.StopCause`) |
| `/plan clear` (new user action) | non-terminal → `discarded` |

## 2. Mode & capability

### 2.1 `CapabilityStatement` (new, composed — not stored)

The per-mission text block a delegated subagent receives inside its system message ([contracts/mode-capability.md](contracts/mode-capability.md) §2). Composition inputs, all existing:

| Field | Source | Determinism rule |
|---|---|---|
| Agent kind + role sentence | `subagentSpecs()[kind].System` | static per kind |
| Toolset line | `spec.Allowed` names | sorted lexicographically, comma-joined |
| Boundary line | fixed template: "Anything not listed is unavailable to you." + kind-specific edit prohibition | static per kind |
| Report contract line | fixed template (D10): findings/diffs only; ambiguity and architectural choices flagged back, not decided | static |
| Workspace + shared memory | existing brief fields | unchanged |

Validation: statement is per-mission message content (never prefix); byte-deterministic for identical (kind, Allowed, workspace, memory) inputs.

### 2.2 MCP tool annotation (new field on the MCP tool surface)

`readOnly bool` **(new)** — captured from MCP `tools/list` `annotations.readOnlyHint`; absent → `false` (fail-closed: treated as mutating). Consumed by `isMutation` (both sites) so plan mode admits read-only-annotated MCP tools. No persistence; re-captured per connection.

### 2.3 Per-turn rider inventory (existing, contract-governed)

Task-brief (class/tools/turns/agents/reasoning/verify + scope/DONE clauses), plan block, goal block, governor/breaker/loop-guard riders — enumerated with exact wording in [contracts/mode-capability.md](contracts/mode-capability.md) §1/§4. This feature aligns wording to one taxonomy; it adds no new rider classes to plain turns (002 SC-004 tail budget preserved).

## 3. Terminal states & completion

### 3.1 Work-item state machines

**`toolView.state`** (TUI): `running → ok | fail | cancelled` **(new value: `cancelled`)**. Transition sources: `toolEndMsg` (existing) or the task-end sweep (new — any item still `running` when the task's stats message lands → `cancelled`). Terminal states never re-animate.

**`agentView.status`** (TUI): `running → <final event status> | cancelled` **(new value)** — same sweep rule.

**Engine guarantee**: every emitted `ToolStart` is paired with a `ToolEnd` on all paths (success, failure, cancellation, panic-recovery); every subagent start with a terminal `done` event. The TUI sweep is the safety net, not the primary mechanism.

### 3.2 `CompletionAudit` (new, computed in `finalize` — not stored)

Inputs (all existing recorded state): plan phase + step statuses, `TaskStats{FilesChanged, ChecksRun, StopCause, DoneCriteria}`, final answer text, task class.

Outputs:
- Plan phase stamp (§1.4 finalize rows).
- Disclosure append: when `hasIncompleteSteps` ∧ phase transitioned to `interrupted`: `"— N of M plan steps incomplete: <first incomplete titles ≤3>"`.
- Claim reconciliation append: answer matches check-claim patterns ∧ `ChecksRun == 0` → `"(note: no checks were run this task)"`.
- Empty-answer path: existing `fallbackAnswer` synthesis + disclosures (never bare `"Done."`).

Rules: applies to classes ≥ standard; appends are harness-attributed; never blocks or delays finalize; adds no model calls ([contracts/completion-terminal.md](contracts/completion-terminal.md)).

## 4. DeepSeek wire records

### 4.1 Wire event records (new, observability — session event log)

| Event | Fields | When |
|---|---|---|
| `tool_call_salvaged` | tool name, source shape (json-in-content / dsml / prefixed-text) | D5 salvage repaired a text-emitted call (≤1 per turn) |
| `empty_completion_retry` | attempt number (1..2), outcome | Zero-token 200 after a tool-result turn triggered the retry ladder |

Both surface in provider-usage-adjacent telemetry; neither alters message bytes retroactively (append-only).

### 4.2 Replay verification outcome (feature artifact, not runtime state)

Recorded in `benchmarks/` evidence + [contracts/deepseek-wire.md](contracts/deepseek-wire.md) §3: verdict ∈ {`empty-string-accepted` (keep current), `replay-required` (build contingency)}. Contingency runtime shape (only if required): assistant tool-call turns in the active task window retain `ReasoningContent` for verbatim replay; folded at settled boundaries exactly like completed-task tool payloads (append-only, prefix-safe).

## 5. Feature governance artifacts (document schemas)

**`AuditFinding`** (rows of [audit.md](audit.md)): `id`, `category` (one of FR-012's seven), `symptom`, `root cause (file:line)`, `disposition` (`fixed:<task-id>` | `deferred:<rationale>`), `verification` (test/benchmark ref). Completeness rule: every category cites ≥1 finding or an explicit no-defect sweep note (SC-009).

**`ResearchFinding`** (rows of [research.md](research.md) Part B): `id`, `finding`, `source + tier (T1/T2/T3)`, `implication`. Traceability rule: every model-specific adaptation in the contracts cites a `B#` (FR-019); D-decisions carry the citations.

## 6. Explicitly unchanged

Tool schema set and serialization (III); history append-only/fold/compaction machinery (002); effort profiles and reasoning mapping; permission/trust/containment security model; `~/.muhiya` layout beyond the additive `phase` field; goal machine (G3 exclusivity preserved — plan-phase wiring touches only the plan side of the existing toggle).
