# Phase 0 Research — Enforced Orchestration Pipeline & MiniMax (feature 009)

**Date**: 2026-07-14. Inputs: [spec.md](spec.md) (with 4 clarifications),
[minimax-baseline.md](minimax-baseline.md) (MiniMax docs + Mini-Agent + pricing +
M-risks), constitution v1.0.0, a thorough substrate inventory of the existing
plan-mode / approval / knowledge / classify / subagent / settings machinery (all
file:line-cited below), and the feature-008 delegation work. All Phase-0 unknowns
(R1–R9) are resolved; no NEEDS CLARIFICATION remains.

**Headline finding**: the enforced pipeline is a **composition of primitives that
already exist**, not a new subsystem. Plan phases (9 states, SSOT `engine.planPhase`
under `modeMu`, persisted to `plan_state.json`), a single dispatch chokepoint
(`gatedExecute`), flow-gate approvals that already ignore permission mode, the
knowledge store as a handoff substrate, `planMarkdown`/`WritePlan` for the artifact,
and the four subagent kinds — all present. Feature 009 adds ONE new unit
(`pipeline.go`, the phase state machine) that orchestrates them, plus the MiniMax
provider.

---

## R1. Pipeline state machine — mapped onto existing primitives

**Decision**: a `pipeline.go` state machine drives a task through phases
`classify → research → plan → approve → implement → validate → done`, each phase
expressed via existing engine state, not new persistence:

| Pipeline phase | Built on (evidence) |
|---|---|
| Needs-a-plan decision | `Classify()` (classify.go:66-113); pipeline-required = Class ∈ {standard, large, epic} that is not a pure question; the verdict + reason surface via the existing brief/notice |
| Research | auto-enter plan-mode read-only state (plan.go:193-205 planBlock; goal.go:172 SetPlanMode) + fan out `explore`/`plan`/`review` subagents (subagent.go), whose reports bank into the knowledge store (AddReport, knowledge.go:47) |
| Plan | main agent synthesizes; `update_plan` (engine.go:1817) + `WritePlan` → `plan.md` (planMarkdown, engine.go:2673); `exit_plan_mode` (engine.go:1793 → ErrPlanModeExited) ends planning |
| Approve | the plan-ready flow gate (engine.go:1261-1276 → openPlanReadyModal, actions.go:888) — **already ignores PermissionMode** (flow gate, not permission gate) |
| Implement | on approval, `PlanPhaseExecuting` (engine.go:830-849 proceed flow); implementation subagents (`general` kind) execute plan parts; the SINGLE `gatedExecute` chokepoint (engine.go:1637) is where the "no main-loop mutation before approval" gate lives |
| Validate | a `review` subagent pass + the checks the plan lists, before finalize |
| Done | `stampPlanCompletionPhase` (plan.go:107) → Finished only when `hasIncompletePlan()` is false; extended with a completeness gate (R5) |

**Rationale**: reuses the SSOT lifecycle and its terminal-guard (SetPlanPhase,
plan.go:78-87), so resume/persistence/interruption are free; the state machine only
sequences transitions and enforces gates (Principle VIII — the one justified new
unit, recorded in plan.md Complexity Tracking). **Alternatives**: pure-prompt
orchestration (rejected — that IS today's unreliable model-discretion approach the
spec replaces); overloading plan mode alone (rejected — it is 1 of 5 phases with no
research/implement/validate/dependency semantics).

## R2. Enforcement without a token ceiling, without breaking the fast path

**Decision**: enforcement lives at two existing gates, not in prompt text:
1. **Entry gate** (engine `Run`): a pipeline-required task auto-enters the research
   phase (plan-mode read-only state) with an agent floor ≥1 (WithAgentFloor,
   classify.go:168; already applied for plan mode at engine.go:727) — so a
   pipeline task always has subagent budget.
2. **Implementation gate** (`gatedExecute`, engine.go:1637): for a pipeline-required
   task, main-loop mutating tool calls are blocked until phase == executing (i.e.
   after research-done + plan-exists + approved), reusing the existing plan-mode
   mutation-block machinery (engine.go:1666-1683, recordPlanViolationAndBlock's
   bounded escalation at engine.go:1523). The block text guides the model to finish
   research/plan first — same bounded, loop-guarded shape as plan mode today.

The **simple-task fast path** is preserved by R4's decision: a no-plan task never
enters the pipeline, so nothing gates it. **No token ceiling** is introduced (FR-006,
carrying forward 008's removal); loop safety stays the failure terminator + turn
budget + the 008 trailing-intent guard. **Alternative**: a new dedicated
"implementation lock" flag — rejected, the plan-phase SSOT already expresses exactly
this (drafting/ready/pending = pre-approval; executing = approved).

## R3. Always-pause approval that overrides auto-accept

**Decision**: reuse the **plan-ready flow gate** (engine.go:1261-1276 →
`openPlanReadyModal`, actions.go:888) as the mandatory approval. It is verified to
**ignore `PermissionMode`** (the substrate inventory confirms flow gates —
ask_user/propose_changes/exit_plan_mode/plan-ready — do NOT consult permission mode;
only the workspace + MCP approvers honor auto-accept). So "always ask, even in
auto-accept" (FR-002, clarified) is satisfied by construction — no permission-mode
branch needed. The modal's three existing choices map to the spec's approve / steer
(keep planning) / cancel (later).

**Headless (`-p`) behavior**: `newConsoleCallbacks` sets **no `TaskComplete`**, which
the code already uses as the one-shot signal (maybeSignalPlanReady/planExited resolve
to `PlanPhasePending` + "say proceed"). So a headless pipeline task writes the plan,
saves it pending, and prints the proceed instruction — never auto-implements without
a human. **Rationale**: zero new approval surface; the exact behavior the user asked
for falls out of existing gate semantics. **Alternative**: a new confirm at
implementation start — rejected (redundant with plan-ready; would also need a manual
permission-mode bypass the plan-ready gate already has).

## R4. "Needs a plan" decision (no user override)

**Decision** (per clarification #4 — internal, no override): extend `Classify` with
a pure predicate `NeedsPlan(assessment)` = true for `Class ∈ {standard, large,
epic}` that is not a pure no-workspace question, false for `chat/tiny/small` and
questions. Standard → light pipeline (research + plan + approval mandatory,
implementation may stay main-loop after approval); large/epic → full pipeline
(implementation + validation via subagents). The verdict and its `Reason`
(Assessment.Reason, already computed, classify.go:94-106) render in the session so
routing is explicable (FR-001 visibility). No per-task or global override surface
(FR-001). Effort selects per-phase subagent COUNTS only (classAgents unchanged:
standard 2 / large 5 / epic 8 — classify.go:117).

**Rationale**: the classifier already computes exactly the signals needed (breadth,
paths, bullets, ScopeGuard, class); NeedsPlan is a thin, testable predicate over
them, not a new classifier. **Alternative**: an LLM self-assessment of "do I need a
plan" — rejected (that is model discretion, the unreliability the feature removes).

## R5. Structured handoffs, dependency-aware division, anti-premature-finish

**Decision** — three sub-parts, all on existing substrate:
- **Handoff contract (in)**: each subagent launch is composed from a fixed template
  — role, scoped context (the relevant knowledge-store extract via
  `Briefing`, knowledge.go:102, NOT a transcript), deliverable, and required output
  format — appended to the subagent system prompt (subagent.go:155). The
  `run_subagent` `task` field guidance (008 DG-4) already demands deliverable
  specificity; 009 adds the structured output-format expectation per phase.
- **Structured summary (out)**: subagent reports already bank into the knowledge
  store as digested facts (AddReport digest 500 / full 4000, knowledge.go:47); the
  main agent consumes `Briefing` extracts, so cross-phase flow passes bounded
  summaries by construction (FR-011). Duplicate-read audit (008 runner) enforces
  no re-explore.
- **Dependency-aware division + completeness**: the plan artifact's steps
  (contract.Plan.Steps, ≤12, each with status) are the unit of division; the
  implement phase maps independent steps to subagents (parallel where the plan marks
  them independent). The **completeness/anti-premature-finish gate**: the pipeline
  cannot reach `Done` while `hasIncompletePlan()` is true (engine.go:1935) AND a
  review pass has not confirmed the plan's acceptance checks — extending
  stampPlanCompletionPhase (plan.go:107) so premature finalize → `Interrupted`
  (resumable), never a false `Finished`.

**Rationale**: the knowledge store + plan-step model already express handoffs and
division; the anti-premature-finish gate is a small extension of an existing
terminal-phase stamp. **Alternative**: a bespoke inter-agent message bus — rejected
(Principle VIII; the knowledge store is the message bus).

## R6. MiniMax provider — gateway + client (functionality now, live later)

**Decision** — mirror the DeepSeek provider path (007/008) exactly, with MiniMax's
documented specifics; **verify via a recorded/simulated upstream, no live spend**
(R8, user directive):

Gateway (`F:\MuhiyaWorkspace\MuhiyaWorkspace`):
- Provider + model rows for M3 / M2.7 / M2.5 / M2.1 / M2 (docs: 204.8k ctx, M3 1M),
  OpenAI-compatible base `https://api.minimax.io/v1`, Bearer auth.
- **Tiered pricing** (baseline §pricing): M3 input/output/cached **branch on prompt
  size ≤512k vs >512k** ($0.30/$1.20/$0.06 vs $0.60/$2.40/$0.12) — the cost math
  must be size-aware, unlike DeepSeek's flat rate (a real M-risk, M-pricing).
- **Passive caching** (≥512 input tokens, ~80% cached discount): read
  `prompt_tokens_details.cached_tokens`; derive miss = prompt − cached (labeled
  derived); record in request_logs like DeepSeek; billing distinguishes cached vs
  uncached input. No control surface to add (automatic).
- **Reasoning normalization** (thinking.go): emit MiniMax's documented control
  (`reasoning_split` on the OpenAI path) rather than raw client passthrough,
  consistent with the per-provider normalization policy (M5).
- Function calling: standard OpenAI `tools`/`tool_calls`/`{role:tool}` shapes (M3
  doc + Mini-Agent confirm no legacy `abab` fields) — body-shaping identical to the
  DeepSeek path; no gateway translation needed.

Client (`internal/gateway/model.go` + provider.go/sse.go):
- MiniMax capability profile (008-style): documented limits (204.8k–1M, not the
  stale 128k/16k), tool-call rescue on (family already flagged), reasoning parsing.
- **Per-family reasoning replay policy (the critical M1 risk)**: DeepSeek strips
  reasoning on replay and empties the key on tool-call turns; **MiniMax REQUIRES the
  assistant reasoning (`reasoning_details`) preserved in replayed history for
  interleaved-thinking continuity** (Mini-Agent openai_client.py:160-166, verbatim
  "CRITICAL"). Decision: `replayMessages` becomes **per-family** — a
  MiniMax branch that preserves reasoning content on replay. Prefix-stability
  analysis (below) confirms this stays byte-stable: reasoning content is part of the
  settled assistant turn (append-only), so preserving it does not vary the prefix
  across turns — it makes the MiniMax prefix STABLER (dropping it would change
  settled bytes). Recorded as a cache-affecting change under Principle III.

**Rationale**: least-change parity with the proven DeepSeek path; the one genuinely
MiniMax-specific mechanic (reasoning replay) is isolated behind a family branch.
**Alternative**: MiniMax's Anthropic-dialect endpoint — rejected (out of scope; the
OpenAI-compatible path is the integration surface and matches our wire model).

## R7. Fresh-install defaults: M3 main + V4Pro sub (with the AutoAssign gotcha)

**Decision**: an EXPLICIT fresh-install default rule, NOT reliance on
`AutoAssignModels`. The substrate inventory found `AutoAssignModels`'s score rules
would **not** yield M3-main/V4Pro-sub: `V4Pro` matches the `"pro"` main-preference
substring (scored as a MAIN candidate), and M3's 1M context scores highest by base —
so the score heuristic could pick either as main and would not deterministically
pair them as intended. Decision: on first run, when discovery reports both a MiniMax
M3 model and a DeepSeek V4 Pro model, set ActiveModelID = M3 and SubagentModelID =
V4 Pro explicitly; when MiniMax is absent, fall back to today's `AutoAssignModels`
DeepSeek result unchanged; **never touch an already-configured setting**
(config.go's user-pinned guard, config_defaults_test.go:53). This is a targeted
addition to the discovery/assign path (application.go:1032 addDiscoveredModels →
AutoAssignModels), gated on the specific model pair.

**Rationale**: honors the clarified default without perturbing the general
score-based assignment for everyone else; the explicit pair rule is the only way to
get the requested pairing deterministically. **Alternative**: retune modelScore so
M3>V4Pro — rejected (would ripple into every deployment's role selection; too broad).

## R8. Verifying MiniMax WITHOUT live spend (user directive)

**Decision**: MiniMax gets a **recorded/simulated upstream fixture** — an httptest
server replaying captured/authored MiniMax SSE responses (tool calls, reasoning
blocks, usage with `cached_tokens`) — that exercises the full gateway route + client
replay + accounting path end-to-end, and unit tests for the tiered cost math and the
per-family replay policy. All MiniMax metrics in artifacts are **labeled simulated**
(Principle VI) and never presented as live measurements. **DeepSeek remains the live
benchmark provider** for the pipeline (SC-001/003) and the account's budget windows
are respected. When the user funds MiniMax later, the same conformance session runs
live with the fixture swapped for the real endpoint (one flag). **Rationale**: full
functionality + honest verification without spending money the user said they won't
spend. **Alternative**: skip MiniMax tests — rejected (ships unverified code).

## R9. Reference pass (Principle VII)

- **Reasonix** (constitution reference): its conductor/`plan-exec` pattern —
  "Route each step to the module it belongs to; steps in different modules can run
  in parallel" — validates R5's plan-step-as-division-unit + dependency-parallel
  implementation. Its subagent isolation (one report back, usage forwarded) matches
  our knowledge-store handoff.
- **Claude Code** (behavioral target): the main-agent-as-orchestrator with a plan
  gate and subagent execution is the shape being matched; enforcement + always-pause
  + completeness verification are the specific behaviors.
- **Mini-Agent** (MiniMax wire reference, baseline done): informs R6 wire fidelity
  only (it has NO orchestration — single loop, no subagents, no plans); its ONE
  load-bearing contribution is the `reasoning_details` replay requirement (M1).

**Findings recorded**; no new adoption rows beyond R5/R6 above.

---

## Appendix — substrate evidence index

- **Plan lifecycle**: types.go:290-308 (9 PlanPhase states, IsTerminal),
  engine.go:145 (planPhase SSOT under modeMu:140), goal.go:172-210 (SetPlanMode),
  plan.go:78-116 (SetPlanPhase terminal-guard, stampPlanCompletionPhase),
  plan.go:193-205 (planBlock text), engine.go:1793-1801 (exit_plan_mode),
  types.go:356 (ErrPlanModeExited), engine.go:1217-1276 (planExited flow),
  engine.go:830-849 (proceed flow), engine.go:1935 (hasIncompletePlan).
- **Plan file**: engine.go:50 (WritePlan), engine.go:2673-2686 (planMarkdown),
  types.go:265-282 (Plan/PlanStep/PlanStatus), engine.go:1817-1876 (updatePlan +
  step-progress detection), view.go:319-343 (plan render).
- **Approval**: types.go:457-476 (Callbacks Ask/Confirm), tui/bridge.go:94-118,
  root.go:565-618 (console callbacks; no TaskComplete = one-shot signal),
  engine.go:1878-1933 (ask_user/propose_changes), actions.go:888-917
  (openPlanReadyModal). Flow gates ignore PermissionMode; only workspace+MCP
  approvers honor auto-accept (application.go:572-577, 624-632).
- **Enforcement**: subagent.go:300-305 (isMutation), engine.go:1637-1737
  (gatedExecute), engine.go:1666-1683 (plan-mode blocks), engine.go:1523-1538
  (bounded escalation), classify.go:168-175 (WithAgentFloor), engine.go:727.
- **Knowledge**: knowledge.go:12-138 (KnowledgeFact, AddReport, Reusable epoch-gate,
  Briefing, MarkWorkspaceChanged), subagent.go:151-158 (shared memory into subagent).
- **Classify**: classify.go:24-29 (Assessment: Class/Risky/Reason/ScopeGuard),
  classify.go:95-106 (class boundaries), classify.go:115-138 (budgets; classAgents).
- **Subagent report**: subagent.go:28-32 (result struct), subagent.go:134 (report
  string), subagent.go:313-323 (capabilityStatement), subagent.go:34-53 (specs).
- **Settings defaults**: config.go:15-32 (DefaultSettings, empty Models),
  config.go:155-184 (AutoAssignModels score rules), config.go:357-367 (modelScore),
  application.go:1032-1041 (addDiscoveredModels → AutoAssignModels),
  config_defaults_test.go:19-64 (pro=main/flash=sub, user-pin guard).
- **MiniMax**: [minimax-baseline.md](minimax-baseline.md) (docs, Mini-Agent,
  tiered pricing, M1–M7 + M-pricing).
