# Contract: Mode Capability — Delivery & Enforcement

**Feature**: `004-deepseek-agent-polish` | Decisions: [research D3, D10](../research.md) | Data: [data-model §2](../data-model.md)

Governs how every agent learns its boundaries and what happens when a boundary is hit. Requirements FR-008..011, FR-014..015; SC-002/SC-003. Constitution constraint: the tool schema set is session-stable and **never filtered by mode** (III; research A4/B2) — awareness rides dynamic surfaces; enforcement rides the dispatch gate.

## 1. Capability delivery matrix (who is told what, when)

| Surface | Cadence | Carries | Bytes |
|---|---|---|---|
| System prompt ENVIRONMENT lines (existing) | Session-static (prefix) | web availability; subagent availability + model | unchanged |
| Task-brief (existing, user-message tail) | Every task | class, `tools~N`, `turns<=N`, `agents<=N` / `agents=0 (no run_subagent)`, reasoning, verify, scope/DONE clauses | unchanged budget (002 SC-004 ≤~50-token tail) |
| Plan block (existing, prepended rider) | Every turn while plan mode on | read-only rule, allowed read-only tool list, `exit_plan_mode` exit | wording aligned to §3 taxonomy, same length class |
| Goal block (existing) | While a goal is active | autonomy + marker protocol | unchanged |
| **Subagent capability statement (new)** | Per mission (subagent system message) | kind role sentence; sorted toolset line "Tools available to you: …"; "Anything not listed is unavailable to you."; kind edit-prohibition; report contract line ("return findings/diffs; flag ambiguity and architectural choices back to the caller instead of deciding them") | per-mission message — never prefix |

**Mid-session change rule (FR-010)**: all riders are recomputed at each `Run`/turn assembly from live engine state; a mode/capability flip is therefore reflected on the agent's next action with no caching. Test obligation: toggle plan mode / exhaust agent budget between turns and assert the next brief reflects it.

## 2. Enforcement gate (order fixed, existing)

`gatedExecute`: H1 arg validation → H2 verbatim-failed cache → plan-mode gate → repeat limiter → duplicate-read guard → cancel check → dispatch. Subagents pass the same gate with their `Allowed` set enforced at both schema issuance and dispatch. This feature changes **feedback precision**, not gate order.

## 3. Corrective-feedback catalogue (reviewed strings)

Existing strings stay unless a row marks a change. Prefix taxonomy: `Blocked:` (policy stop), `[loop guard]` (repetition stop), plain sentence (correctable error). All follow B5's positive-instruction guidance: name the restriction, then the allowed alternative.

| Class | String (→ = changed/new by this feature) |
|---|---|
| Unknown tool | → `unknown tool %s. Closest available: %s.` (nearest offered name, edit-distance ≤3; suggestion clause omitted when no candidate) |
| Dead MCP tool | `MCP tool %s is unavailable (server disconnected). Do not retry it this task; use another approach.` (unchanged) |
| Tool not in subagent allowlist | `tool %s is not available to this agent` (unchanged — the new capability statement makes it rare) |
| Malformed/missing args | existing `validateCallArgs` messages + ` Re-emit the call with well-formed arguments.` (unchanged) |
| Plan-mode mutation (file tools) | `Plan mode is read-only — finish planning and call exit_plan_mode.` + existing escalation ladder (unchanged) |
| Plan-mode `run_shell` non-read-only | existing read-only-probes message (unchanged) |
| Plan-mode MCP tool, **not** read-only-annotated | → `MCP tool %s is unavailable in plan mode (not marked read-only). Plan with the workspace read tools instead.` |
| Plan-mode MCP tool, read-only-annotated | → **allowed through** (no message) — see §4 |
| `run_subagent`, budget exhausted mid-task | first denial corrective, second hard-closed (unchanged ladder) |
| `run_subagent`, `agents=0` for the task | → hard-closed on the **first** denial: `no subagent budget for this task (agents=0). run_subagent is closed for this task — do NOT call it again; continue directly with your own tools.` |
| `propose_changes`, non-interactive session | → verdict labeled `auto-approved by non-interactive policy — no human reviewed this proposal` |
| Permission denial | → existing `Tool %s failed: workspace: permission denied` gains one clause naming the class: `(outside workspace)` / `(protected credential path)` / `(user declined)` / `(unread overwrite — read the file first)` |
| Duplicate read | `Blocked: unchanged result already in context from call %s. Use that result; do not re-read.` (unchanged; breaker-feeding disposition tracked in [audit.md](../audit.md) A5.5) |
| Repeat limiter / verbatim-failed / storm / all-failed-turns / failure terminator | unchanged (existing bounds, §5) |

Security note: the permission-denial clause names the *class* only — never paths outside the workspace, never secret locations beyond what the existing block message already states (reviewed against `docs/security.md`).

## 4. MCP read-only admission (new)

- `mcpclient` captures `annotations.readOnlyHint` at `tools/list`; exposed as `readOnly` on the tool surface (data-model §2.2). Re-captured on every (re)connect; no persistence.
- `isMutation(name)` (both sites: engine inspection + subagent) returns `false` for MCP tools with `readOnly == true`; **absent annotation ⇒ mutating** (fail-closed, IX).
- Plan mode consequently admits read-only-annotated MCP tools; everything else keeps today's block with the §3 wording.
- Auto-accept/trust/permission semantics are untouched — read-only admission affects plan-mode gating only, not approval flows.

## 5. Bounds (unchanged, restated as the reviewed invariant set)

| Guard | Bound |
|---|---|
| Identical-call repeat limiter | blocks after >3 |
| Verbatim-failed short-circuit | immediate on exact repeat of a failed call |
| Failure-storm breaker (per tool+error class) | corrective at 3 |
| All-failed-turns guard | after 2 consecutive all-failed turns |
| Task failure terminator | 8 distinct failures / 6 turns → forced final |
| Plan-violation ladder | soft ×2 → loop-guard stop at ≥3 |
| Subagent denial ladder | §3 rows (first-denial hard close only when `agents=0`) |
| Empty-final retries | ≤2 (shared with [deepseek-wire §5](deepseek-wire.md)) |
| Plan continues | ≤8 |

The same disallowed attempt must not recur more than twice in a session under these bounds (SC-002's recurrence clause) — the mode-scenario suite asserts it end-to-end.

## 6. Continuation-phrase widening (T7 accelerator)

`continuationRE` gains word-boundary phrase alternatives ("proceed with …", "go ahead and …", "execute/run the plan", "do the plan") while staying deliberately narrow — T8 (step-progress detection, [plan-lifecycle §2](plan-lifecycle.md)) is the correctness backstop, so the phrase list optimizes only *when* the plan markdown gets injected into the brief. False-positive guard: match only when phase ∈ {pending, interrupted}.

## 7. Test obligations

Mode-scenario suite: (a) plan-mode file/shell/MCP attempts — blocked with §3 strings, read-only-annotated MCP admitted; (b) `agents=0` first-call hard close; (c) unknown-tool suggestion; (d) subagent receives capability statement and its out-of-set call is rejected; (e) mid-session mode flip reflected next turn; (f) non-interactive `propose_changes` labeling; (g) recurrence ≤2 assertion across a scripted adversarial session.
