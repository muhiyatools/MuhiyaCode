# NATIVE AGENT PLAN — MuhiyaCode as a natural, autonomous coding assistant

**Status: PLANNED — awaiting execution approval.**
**Target release: v1.1.0** (this is a breaking-behavior release: commands removed, one prefix-cache epoch).
**Grounding: every file:line below verified against working tree at commit f087ad8 (clean) by a seven-agent codebase survey + a three-critic adversarial verification pass.**
**Executor: Claude Opus 4.8**, phase by phase (§12 order). Per-phase gate: `go build ./... && go vet ./... && go test ./...` green plus staticcheck (`go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...`). Never touch the crown-jewel files unless a section names them explicitly: `contextlink.go`, `contextrecord.go`, `prefixshape.go`, `cacheresilience.go`, `state/session.go` agent-record functions, `usage.go`, and the entire cache test suite. When a section names exact lines, verify them against the tree before editing (they were checked at f087ad8; drift is possible). Golden regeneration commands are in §11.

---

## 1. Vision

MuhiyaCode stops being a workflow machine and becomes a native coding assistant, like Claude
Code: the user says what they want, the agent does it. No Planning Mode, no lifecycle states, no
"Proceed with Plan" modal, no `/goal`, no `/model`. The agent tracks its work in a plain
`tasks.md` checklist in the project folder. Planning is an internal skill it reaches for only
when the task genuinely needs one or the user asks.

**The model architecture is a session-stable two-role split, with caching as the top priority**
(owner directive, 2026-07-20): the MAIN model (user's preference, default **MiniMax M3**) does
analysis, planning, and orchestration in the main session — and nothing else; the EXECUTION
model (default **DeepSeek V4 Pro**) does ALL task execution inside sub-agent sessions whose
cache persists across the whole session; a cheap UTILITY model (default **DeepSeek V4 Flash**)
handles instructing-class aux calls (session advisor, onboarding). Models never change
mid-session — that is what maximizes cache reuse; a significantly different workload gets a
"start a fresh session" advisory instead of a model switch.

**Non-negotiable invariants carried into this transformation:**

1. **Standing directives (features 013/014, amended 2026-07-20)**: subagents are launched by the
   MODEL, never auto-dispatched by the harness; ONE subagent at a time, strictly serial; raw
   transport errors never reach the user (`gateway.FriendlyRequestError` stays the single
   mapping). **Amendment**: feature 014's "standard tasks run direct" is SUPERSEDED by the
   owner's plan/execute split — the main model no longer executes; every workspace mutation
   (except `tasks.md`) is delegated to the execution model (§7 F0). Launching remains
   model-driven: the instruction layer directs the delegation; the gate layer enforces it.
2. **The crown jewel — feature 012 context linking and the whole prompt-cache discipline — is
   preserved byte-for-byte**: `contextlink.go`, `contextrecord.go`, `prefixshape.go`,
   `cacheresilience.go`, the agents/`<runID>.json` compact sidecars, the per-pin usage ledger,
   the prefix-shape guard, and the entire cache test suite (`prefixshape_test.go`,
   `rtl_cache_test.go`, `cacheresilience_test.go`, `invalidation_test.go`,
   `session_cache_test.go`, `prefix_shape_sidecar_test.go`, `toolcache_test.go`) are untouched.
3. **Exactly one prefix-cache epoch — at RELEASE granularity.** Every change to cached prefix
   bytes (tool schemas, system-prompt sections, model-name templates) ships in this one release,
   so users experience one recorded epoch and one attributed cold start. Mechanically, each
   phase commit regenerates goldens and the wiring inventory for its own byte changes (keeping
   CI green); phase 7 reviews the cumulative prefix diff as the recorded epoch (§9).
4. **Constitution VI**: cache/usage figures are provider-reported or "unavailable" — never
   estimated. The session advisor inherits this: it reports what it chose and why, never
   invented numbers.

---

## 2. Workstream A — Delete the goal system

The goal machine (`/goal`, auto-continue markers, goal sidecar) is Reasonix-era machinery that
rides the per-turn user tail, so deletion is **cache-neutral**. It is entangled with the plan
lifecycle (shared `modeMu`, mutual-exclusion rules), so A and B land together.

**Delete outright:**

- `internal/orchestrator/goal.go` (all 408 lines) — but `PlanMode()` at goal.go:188-196 is the
  lifecycle read-only predicate used by `turnloop.go:630` and `render_header.go:165`; it dies
  with the lifecycle in Workstream B, so A and B must be one change.
- Goal branches in `turnloop.go` (132-133 reset, 260-281 brief assembly + DG2 backstop, 533-538
  final-turn scan, 603-624 auto-continue, 656-666 tool-turn scan) and `turnhelpers.go:71-90`
  (finalize scan + `StripGoalMarkers`).
- `ResetGoalTaskCounter` (`plan.go:18-27`), the goal-clear block in `beginPipeline`
  (`pipeline.go:57-75`).
- Engine wiring: `Persistence.WriteGoal/ClearGoal` (engine.go:42-45), `EngineConfig.InitialGoal`
  (92-94), `goal`/`lastGoal`/`restoredGoalNotice` fields, NewEngine restore (386-393). **Keep
  `writeMu`** — it is shared with plan-state, project-cursor, and prefix-shape sidecar writes.
- `contract.GoalSnapshot` (types.go:400-411); `state` goal sidecar functions; restore reads in
  `runtime_build.go:455-459`, `root.go:89-91`, `modals.go:105-107`.
- TUI: `/goal` in busy-allowlist (slash.go:60), dispatch (100-101), `handleGoalCommand`
  (204-240), palette entry (model.go:392), goal footer badge (render_header.go:170-174).
- Instructions: `GoalBlockInstructionBody` + `prompt.goal-block` registration
  (instructions/pipeline.go:42-48).
- Tests: `goal_test.go`, `goal_hardening_test.go`, `state/goal_sidecar_test.go`.

**Keep:** `maintenance.go:58`'s compaction "GOAL/STATE/FILES" heading is the compaction summary
schema, not the goal feature — do not sweep it.

---

## 3. Workstream B — Delete Planning Mode and the pipeline

The biggest deletion. The unified lifecycle state machine (11 states, feature 010), the
research→planning→approval→implementing→validating pipeline, the plan artifact + tools, the
approval modal, and all persistence die. The **shared machinery threaded through the same files
survives** — this workstream is surgery, not demolition.

### B1. Files deleted whole

| File | What dies |
|---|---|
| `orchestrator/lifecycle.go` | state machine + transition table |
| `orchestrator/plan.go` | plan-mode setters, sidecars, plan-ready signaling |
| `orchestrator/pipeline.go` | all transitions + approval verbs + `[orchestration-pipeline]` block |
| `orchestrator/phaserunners.go` | phase preludes, `observeOrchestratedSubagent`, plan-update advancement |
| `orchestrator/planbar.go` | plan content bar |
| `orchestrator/readplan.go` | `read_plan` tool (tasks.md is workspace-local; plain `read_file` replaces it) |
| `orchestrator/restore.go` | lifecycle resume reconstruction |
| `state/lifecycle_migrate.go` | three-era plan_state.json migration |
| `instructions/pipeline.go` | every phase prelude / plan block text (minus goal text handled in A) |
| `instructions/examples.go` plan examples | `PlanStepExampleBody`, `PlanNoteExampleBody` |

### B2. Relocation traps — move BEFORE deleting

- `planrender.go` hides four non-plan helpers: `encodeAnswers` (ask_user serialization),
  `extractDoneCriteria` (DONE stat), `isCheckCall` (checksRun counter), `trailingIntent`
  (DeepSeek narration guard FR-004b). Relocate to `turnhelpers.go` or a new `answers.go`.
- `rolegate.go`: delete `phaseReadGate`/`notePhaseReadExempt`/`markImplementFailureDiagnosis`,
  but **relocate `resetLinkTaskState` and the `stats.Links` half of `finalizeLinkStats` into
  `contextlink.go`** — they are the feature-012 lineage ledger (`taskSeq`); losing the reset
  makes every dispatch cross-task and kills same-task continuation chains.
- Test helper `lifecycleEngine()` (plan_lifecycle_test.go:21) is used by
  `completion_audit_test.go` and `lifecycle_test.go`; `todoSteps()` (render_todos_test.go:22) is
  used by `render_language_test.go`. Relocate or rewrite consumers in the same change.
- `updateTaskReviewOutcome` (reviewgate.go:417) loses its only caller
  (`observeOrchestratedSubagent`). Give it a new call site on the `run_subagent` review-report
  path in `subagent.go` (when a review report parses a `VERDICT:` trailer) so review
  Coverage/CeilingHit stamping keeps working.

### B3. Surgical excisions (file survives, plan veins removed)

- **`classify.go` — the classifier SURVIVES.** `TaskClass` ladder, `Budget`, `BudgetFor`,
  `buildBrief`, `EscalateClass` are load-bearing for review gating, turn caps, and subagent
  budgets. Delete only: `NeedsPlan`, `PlanNeedVerdict`, `PipelineDepth`,
  `PlanRequest`/`PlanDoc`/`PlanDocPath` fields, `planProceedRE`/`planDiscardRE`/
  `planRequestRE`/`planDocRE`, plan-intent branches, `WithAgentFloor`. Reword the brief's
  hardcoded "finish all plan steps" (classify.go:267) to reference the tasks.md checklist.
  Keep `reviewLegacyMode` (`MUHIYA_BENCH_LEGACY_REVIEW=1`) working — benchmark baselines use it.
- **`gates.go` — the shared dispatch gate SURVIVES.** Delete `recordPlanViolationAndBlock`,
  `recordPipelineViolationAndBlock`, the `phaseReadGate` hook (116-118), the `BlocksMutation`
  lifecycle branch (128-150), and `ErrPlanModeExited` special-cases. **Keep untouched:** H1 arg
  validation, H2 failed-call cache, the read-only-subagent shell gate (86-97), the feature-012
  continuation-review mutation mask R-D2 (104-110), repeat limiter, duplicate-read dedupe,
  cancel-safe pairing, output cap.
- **`reviewgate.go` — KEEP** except `decideValidationReview` (364-395). The AutoReview nudge and
  end-of-task rationale fire for direct tasks; after removing `!e.PlanMode()` from the guard at
  turnloop.go:630, verify the nudge still fires.
- **`turnloop.go`** — remove ~15 plan/pipeline sites (discard/proceed routing 67-101, saved-plan
  injection 224-258, phase prelude call, plan/pipeline brief blocks, planExited block 674-740,
  plan-continue + validation nudges 585-602, `stampPlanCompletionPhase` defer call). Keep the
  loop, steering, compaction, prefix-shape guard (455-506), governor, AutoReview nudge, B7/H5
  breakers. The defer at :185 also stamps review/link stats — remove the call, not the defer.
- **`toolhandlers.go`** — delete `update_plan` + `exit_plan_mode` cases, `updatePlan`,
  `hasIncompletePlan`, `appendCompletionDisclosure`, `appendPipelineAttribution`. Keep the
  dispatch frame, `askUser`, `proposeChanges`, memory tools, `run_subagent`,
  `repairJSONStringEscapes`. (Completion disclosure is REBUILT against tasks.md in Workstream C.)
- **`definitions.go`** — drop `updatePlanDefinition` + `exitPlanModeDefinition` from
  `sessionDefinitions`; `SessionToolNames` auto-feeds the TUI wiring guard.
- **`subagent.go`** — remove plan-mode general block (126-131), per-phase budget branches
  (148-186/208-214 → collapses to plain per-task `taskAgentCap`), `pipelineLabel` report tags,
  the `observeOrchestratedSubagent` call (197), the 'plan' kind from `subagentSpecs`,
  `read_plan` from the read allowlist. (The per-class budget TABLE changes separately in
  §7 F0 — tiny/small go from 0 to 1 so the delegated-execution model has room.)
- **`knowledge.go`** — keep banking/reuse/briefings/`scopeTerms` (shared with the linker's
  relatedness predicate); delete `ResearchFindings` (245-264, its consumers are all pipeline).
- **`engine.go`** — strip plan/lifecycle/goal fields, `WritePlan`/`WritePlanState`/
  `ClearPlanState` hooks, `InitialPlan`/`InitialPlanState`, `readPlanTool` registration,
  `taskPhaseAgentRuns`, `readGate`/`readGateState`, `maxPlanContinues`. **Preserve untouched:**
  prefix-shape fields, usage/invalidation ledgers, `agentRecords`/`taskLinks`/`taskSeq`.
- **`contract/types.go`** — delete `PlanPhase`, `PipelinePhase`, `LifecycleState` + predicates,
  `PlanStateSnapshot`, `ErrPlanModeExited` (remove all FOUR thread-through sites together:
  gates.go, turnloop.go executeBatch, toolhandlers.go, and dispatch.go:176 where the
  postDispatch invalidation guard collapses to plain `isMutation(name)`), `TaskStats.PlanReady`,
  `AgentEvent.Phase`, `ReadGateStats`. **Keep `Plan`/`PlanStep`/`PlanStatus`** — they become the
  checklist carrier type for Workstream C. Keep `LinkOutcome`, `ContextLinking`, `Callbacks`
  (with `PlanUpdate` renamed/repurposed in C).
- **`AgentEvent.Phase` TUI consumer cluster** (each a compile or behavior break if missed):
  `ingest.go:140` (`phase: event.Phase`), `model.go:158` (agentView `phase` field),
  `render_tool.go:276-277` — the phase branch also references `contract.LifecycleDirect`, a
  symbol this workstream deletes (compile break); identity falls back to kind/role. Persisted
  agent-event payloads from pre-1.1.0 sessions contain a `"phase"` key (`paging.go:231,249`) —
  rehydration keeps TOLERATING the key and ignores its value; update the
  `agent_rehydrate_test.go:17` fixture. The stale `"update_plan": "To-dos"` entry in
  `toolLabel` (render_tool.go:309) is KEPT so old-session transcripts still render readably.
- **`gatepolicy.go`** — prune the four dead gate-inventory rows (plan content bar, empty-plan
  structural gate, update_plan step-cap, plan-mode/pipeline mutation gate) so the normative
  gate-policy contract matches the surviving gate set.
- **`benchmarks/delegationbench`** — lives in the ROOT Go module (no nested go.mod), so it
  breaks `go build ./...` the moment `TaskStats.PlanReady` and `AgentEvent.Phase` are deleted:
  main.go:302 gates the two-step plan-approve flow on `stats.PlanReady`, audit.go parses
  `"Pipeline phase → "` notices (audit.go:98), consumes `event.Phase` (129-137, 192-201), and
  scans session dirs for plan.md mtimes (166-169). Retire the "delegation" workload's
  `pipelineAudit` and the proceed re-run in phase 1; rebuild the audit later around the new
  invariants (tasks.md-before-first-mutation, serial subagent count, role names). Note: the
  audit's `.Handoff` reference keeps a wiring-inventory row alive — flipping it flips the row.
- **`state/session.go`** — delete `WritePlan`/`ReadPlan` (including the session-dir `tasks.md`
  twin at 156-166 — note the naming collision with the NEW project-folder tasks.md; the session
  twin dies), `WritePlanState`/`ReadPlanState`/`ClearPlanState`, the "No task has been planned
  yet." seed. Stale `plan.md`/`plan_state.json`/session `tasks.md` files on users' disks are
  ignored, never treated as corruption.
- **TUI** — delete `openPlanReadyModal` (modals.go:201-239) and the whole plan-modal message
  family (`planProceedResultMsg`/`planDeferredMsg`/`planKeepPlanningMsg`, update.go:220-242 incl.
  the canned "Proceed with the approved plan" auto-submit), the `PlanReady` stats branch
  (update.go:209-213 — keep resultMsg's busy-clear as the sole path), the planning badge
  (render_header.go:162-168), `RestoredPlanNotice` reads. Keep the generic modal system
  (services Confirm/Ask), `renderTodos` presentation (rewired in C).
- **`command/`** — delete plan restore (runtime_build.go:449-471), plan Persistence closures
  (557-565), `RestoredPlanNotice` in one-shot (root.go:87-92). **Keep `parsePlan`**
  (actions.go:177-194) — it is renamed and reused as the tasks.md checklist parser.

### B4. Instruction-registry deletions (part of the single epoch)

- `instructions/prompt.go`: delete `PromptOrchestrationPipelineBody` (157-169); rewrite
  `PromptOperatingContractBody` bullet 3 ("keep update_plan current" → tasks.md convention).
- `instructions/gates.go`: delete plan-mode/pipeline/phase-read/plan-quality gate texts + rules;
  keep `GateContinuationReviewMutationBody` (R-D2 — re-home its `EnforcesRule`), repeat/dedupe/
  budget/shell texts; delete `GateSubagentPlanModeGeneralBody`.
- `instructions/tools.go`: delete `ToolReadPlanDescription`, `ToolUpdatePlanDescription`,
  `ToolExitPlanModeDescription`, `RulePlanStepShape`; rewrite `ToolRunSubagentDescription` (in F).
- `instructions/subagents.go`: delete `SubagentPlanDescription`/`SubagentPlanSystem` (73-74) and
  their `subagent.plan.*` registrations (98-101); reword the "explore/plan/review" kind lists in
  the read-only-shell prose (49, 61) to "explore/review"; rewrite
  `HandoffDeliverableImplementation` (136 — "Complete the assigned approved plan step(s)" is a
  dead concept; new wording targets tasks.md checklist items) and `ReportFormatResearch` (122 —
  drop "Plan implications."). These are per-kind SYSTEM-message bytes: changing them invalidates
  the subagent pins' cached prefixes, so they belong to the §9 epoch bundle.
- The registry is self-auditing: remove texts + rule consts + examples as a set or
  `audit_test.go` fails on dangling references. `TestRegistrySanity` floors the registry at 40
  texts — adjust the floor deliberately if mass deletion dips below it.

### B5. Behavior notes

- Every announced tool_call must still receive a tool result (providers 400 otherwise) — the
  `ErrPlanModeExited` pairing discipline (turnloop.go:675-683) is removed with the sentinel, but
  the invariant itself lives on in cancel-safe pairing; any future early-finalize mechanism must
  respect it.
- Pre-transformation sessions resuming with an active pipeline sidecar simply resume idle.

---

## 4. Workstream C — The `tasks.md` checklist

The replacement for the plan artifact: a plain markdown checklist **in the workspace root**,
owned by the model through ordinary file tools. This is the only viable location — the
per-project memory store (`~/.muhiya/projects/<id>/memory`) is a hard-blocked sensitive root
(workspace/permissions.go:98-108) and the session dir triggers outside-workspace approval.

### C1. Design

- **No new tool.** The model writes/edits `tasks.md` with `write_file`/`edit_file`/`read_file`,
  exactly like any project file. Subagents can read and update it too (they hold the same
  workspace tools) — no `read_plan` replacement machinery needed.
- **Division of labor under the plan/execute split (§7 F0)**: the MAIN model creates and
  reshapes `tasks.md` — it is the execution strategy artifact, and tasks.md is the ONE
  workspace file the main model is allowed to mutate (the role gate carve-out). The EXECUTION
  subagent checks items off as it completes them (its handoff instructions say so). The
  checklist feed observes both scopes (shared-gate hook below), so the panel stays live
  whichever side writes.
- **Format**: the plain `- [ ] title` / `- [x] title` GitHub checklist, with an OPTIONAL
  `(in progress)` marker. **This is a parser REWRITE, not a relocation**: the existing
  `parsePlan` regex (command/actions.go:177) makes a `(pending|in_progress|completed)` suffix
  MANDATORY — a plain GitHub checklist parses to ZERO steps under it, which would leave the
  panel empty and the disclosure guard inert. The new `ParseChecklist` derives status from the
  checkbox alone (`[ ]`=pending, `[x]`=completed) with `(in progress)` (space, not underscore)
  as an optional override; test fixtures cover plain checklists, the legacy suffixed form, and
  mixed files. Status vocabulary never becomes mandatory in the file — that would reintroduce
  plan-status ceremony.
- **Live to-do panel feed** (replaces the `update_plan → PlanUpdate` chain): the check runs in
  the SHARED `gatedExecute` post-dispatch sequence — the one point where main-loop AND subagent
  tool calls converge — keyed on a successful mutation whose path set includes the workspace
  `tasks.md`. (Hooking only the main scope's ToolEnd would silently miss subagent updates,
  which C1 explicitly supports; subagents have their own dispatch scope with a separate
  postDispatch — dispatch.go:166 vs subagent.go:277-299.) Path matching uses a path-SET
  extractor, not the display-oriented `contract.ToolTarget`: for multi-file `apply_patch` the
  display helper returns `"first-file (+N more)"` (tooltarget.go:64-70) and would miss a patch
  that edits code AND checks off a tasks.md item in one call — export the file list
  (`PatchTargetFiles`) and match tasks.md against the full set, and against the normalized
  `path` arg for write_file/edit_file/multi_edit. On match, re-parse the file once and emit a
  `ChecklistUpdate` callback (renamed from `PlanUpdate`) carrying `contract.Plan`.
  Race-safety is inherited: tool calls execute strictly serially (dispatch.go:106-110) and
  `run_subagent` runs inline, so parse-after-result cannot race a concurrent write. The TUI's
  `renderTodos` collapse/glyph rendering is reused as-is; its lifecycle predicates
  (`IsTerminal`/`InvitesProceed`) are replaced by simple rules: hide when all items checked or
  the file is absent; show while items are open and the engine is busy. **No file watching** —
  the project-context contract forbids watchers; the feed is tool-call-driven only, matching how
  the todo panel works today.
- **Cache safety**: `tasks.md` is NEVER injected into the byte-stable prefix. The model reads it
  on demand. If a boot-time surfacing is wanted later, it must ride the existing hash-gated
  one-shot tail probe pattern (extend `ProjectContextProbe` + bump
  `contract.ProjectContextVersion`) — explicitly out of scope for v1.1.0.
- **Conventions text** (Prefix class, registered, part of the single epoch): a short paragraph in
  the operating contract: *for multi-step work, keep a `tasks.md` checklist in the project root
  current — add items when scope is discovered, check them off as they complete, and never claim
  completion while items are open. For small tasks, skip it.*
- **Completion honesty rebuilt**: `appendCompletionDisclosure` returns keyed to tasks.md — if the
  final answer would imply completion while the parsed checklist has open items, append the
  honest disclosure line. (Research B3: the model over-claims completion; this guard caught real
  regressions.)
- **Templates**: never scaffold `tasks.md` proactively and never overwrite user content —
  same never-overwrite discipline as MEMORY.md/MUHIYA.md.
- Secret screening: tasks.md is read by the model via `read_file` (already permission-governed),
  not auto-loaded by the harness, so the MEMORY.md whole-file secret-drop policy does not apply.

### C2. Touch list

| Change | Where |
|---|---|
| `ParseChecklist` (REWRITTEN — checkbox-derived status, optional `(in progress)`) | orchestrator or contract + rewrite `application_test.go` parser test |
| `PatchTargetFiles` path-set extractor | contract/tooltarget.go |
| ChecklistUpdate callback (rename PlanUpdate) | contract/types.go Callbacks, bridge.go:110/23, update.go:184, three Callbacks implementations (Bridge, console, subagent internal) |
| tasks.md match → parse → emit in the SHARED gate post-dispatch path | gates.go gatedExecute (covers main loop + subagents) |
| renderTodos rewire | tui/render_todos.go (keep rows/collapse; new gating) |
| completion disclosure v2 | turnhelpers.go finalize |
| instructions convention text | instructions/prompt.go (epoch) |
| wiring inventory rows | tasks.md feed + renamed callback = new "wired" rows |

---

## 5. Workstream D — Planning as an internal skill

Planning stops being a mode and becomes knowledge the agent applies when warranted.

- **Built-in skill, riding the existing skill system** (workspace/skills.go discovery → prefix
  listing → load on demand). Ship a built-in `planning` skill entry that the skill discovery
  layer always includes (a virtual entry served from the instructions registry, not a file the
  user must create): name `planning`, description "structure a non-trivial task into an
  actionable tasks.md checklist before executing".
- **Skill body** (Sidecar/lazy class — loaded only when invoked, zero prefix cost): instructs the
  model to (1) consult project memory first — `recall_memory` + the MEMORY.md index — for
  constraints, past decisions, and SIMILAR PAST PLANS worth reusing; (2) investigate before
  planning (read the code it is about to change; at most ONE scoped explore subagent if the
  workspace is genuinely unknown); (3) write the plan as `tasks.md` checklist items that are
  verifiable actions, each naming its target files; (4) keep it short — steps the size of one
  commit; (5) when the user asked for a plan and not execution, present the plan as the answer
  and STOP (deliverable-is-the-plan rule); otherwise start executing immediately, checking items
  off; (6) **write back** — when a plan is accepted or a large task completes, save durable
  decisions, discovered constraints, and deferred items to memory via `save_memory` (topic file
  + index pointer), so the memory connection is read AND write, not a one-way consult. The
  skill-body test asserts both the recall directive and the save_memory directive.
- **Trigger paths**: (a) the user explicitly asks ("plan …", "make a plan") — the model simply
  does it; the operating contract mentions the skill exists; (b) the model's own judgment on
  genuinely large work; (c) the user queues it via `/skills` like any other skill. There is NO
  harness-side intent regex and NO classification verdict — that is the point.
- Registered through `instructions/types.go` so the audit suite governs its text; discovery-side
  plumbing in `command/skills.go` + `workspace/skills.go` gains the built-in entry (keep the
  deterministic, session-pinned listing discipline so the prefix stays byte-stable).

---

## 6. Workstream E — Session-stable model roles + the Flash session advisor (remove `/model` UI)

**Owner directive (2026-07-20): caching is the top priority.** The main and execution models are
chosen at session start and NEVER change until the session ends — that is what keeps the main
stream's prefix cache and the execution chain's continuation cache warm for the whole session.
There is no per-task routing. A significantly different or highly complex mid-session workload
gets a one-line "start a fresh session" advisory, never a model switch.

### E0. The three roles (defaults are the owner's stated preference)

| Role | Settings field | Default | Used for |
|---|---|---|---|
| **Main** | `Provider.ActiveModelID` | **MiniMax M3** | The main session ONLY: analyze the user's request, plan, maintain tasks.md, compose subagent handoffs, synthesize answers. Never executes (§7 F0). |
| **Execution** | `Provider.SubagentModelID` | **DeepSeek V4 Pro** | ALL task execution, inside sub-agent sessions (`run_subagent`), riding the session-long continuation chain (§7 F3). Also review/explore subagents. |
| **Utility** | resolved, not persisted | **DeepSeek V4 Flash** | Instructing-class aux calls: the session advisor (E2) and onboarding questions. Resolved per call via `firstModelMatching(models, "flash")` within the deepseek family (state/config.go:218-228); fallback: `SubagentModelID`. Never hardcoded — if no flash-class model exists in the catalog, the aux call uses the fallback or is skipped. |

The first-run seed ALREADY pins exactly this pairing: `AssignFreshDefaultModels`
(state/config.go:189-212) assigns MiniMax-M3 main + DeepSeek-V4-Pro sub — keep it verbatim.
**The user selects the main model** via the KEPT CLI path `muhiyacode config set model` /
`config set subagentModel` (root.go:225-261) — that is the "selected by the user" surface; there
is no TUI command and no ambient display. Onboarding's aux call switches from `SubagentModelID`
to the utility resolver (onboarding.go:28-41, turnloop.go:112-130).

### E1. Deletions (UI only — the settings fields and CLI stay)

- TUI: `/model` palette entry (model.go:395), slash handlers + `openModelList`/`chooseModel`/
  `buildModelSwitchWarning` (slash.go:108-109, 303-434), `models-refresh` modal action
  (modals.go:53-55), header model segments (render_header.go:19-32 — both `model` and `sub`
  segments and the `subSeg` wide-width append), `modelDisplay` (format.go:174-187),
  `Actions.SetModel` (model.go:47, actions.go:32) and the TUI dispatch into
  `Application.setModel`.
- `/model` mentions in notices rewritten: actions.go:123 (stranded-model advisory →
  "run `muhiyacode config discover`"), runtime_build.go:686, root.go:554-566 configurationNotice,
  login.go:85-86.
- **KEEP (revised from the earlier draft): `config set model` + `config set subagentModel`**
  (root.go:225-261) — the user's pinning mechanism. Keep `Application.setModel`
  (runtime_build.go:229-260) reachable from the CLI path only; it still calls the exported
  `SwitchModel`, which is correct there (idle CLI context).
- Keep: `ListModels` discovery, `formatByModelRows` in `/context` (per-model usage attribution),
  `formatCapabilityProfile` (renders the CURRENT main model's profile — /context is the one
  deliberate diagnostic surface where model identity is disclosed), `config discover`,
  `config set contextLimit`, `AutoAssignModels`/`AssignFreshDefaultModels`/`modelScore` (seed
  policy), `ActiveModelID`/`SubagentModelID` persisted fields (deleting them breaks
  `normalizeSettings`, gateway `resolveModel`'s empty-ID fallback, `contextLimit`, doctor).
- **Automatic catalog refresh (NEW — keeps the model list genuinely dynamic).** Today discovery
  runs only when no valid active model exists (runtime_build.go:108-123) or after login with an
  empty catalog; the only RECURRING refresh was `/model`'s Refresh flow, which dies. Add a
  TTL-gated `ListModels` at session open (refresh when the last successful discovery is >24h
  old; timestamp persisted in settings or a small state sidecar), reusing `addDiscoveredModels`
  + stranded-model pruning (actions.go:110-126, 202-249), failure-silent. Test: a model added to
  the gateway after first run is visible next session.

### E2. The session advisor (replaces the per-task router)

A cheap, silent sanity check that the session's model pairing fits the workload — run by the
UTILITY model ONCE, on the first prompt of a session, before the first main-model request (when
everything is cold and a switch is free). After the first main request, models are frozen.

- **Aux-call mechanics** (modeled on onboarding, turnloop.go:112-130 / onboarding.go:28-41):
  dedicated pin `:sub:advisor`, `ReasoningLow`, `MaxTokens ≈ 200`, 8-second timeout,
  `recordAuxUsage` attribution, JSON-object response format. Malformed/timeout/error → keep the
  configured models silently (harness event only; the advisor must never be a point of failure
  or add visible latency beyond its timeout).
- **When it runs**: in the `Engine.Run` prologue, ONLY when `UsageAggregate().Requests == 0`
  (first task of the session) AND `Provider.Advisor != "off"` AND the user has not explicitly
  pinned models this session. Skipped entirely in every other case — so it adds zero cost to
  every subsequent task.
- **Inputs** (compact, harness-assembled): the first user prompt (truncated ~2000 chars); the
  live catalog (`settings.Provider.Models`: id, name, context limit) annotated with static
  profile facts (family, continuation support, window); the CONFIGURED pairing; a one-line
  workspace signal (file-count bucket + top languages, reusing `countWorkspaceSourceFiles`).
- **Output schema**: `{"keep": true}` or `{"main": "<id>", "sub": "<id>", "why": "<one line>"}`.
  The expected common case is `{"keep": true}` — the configured M3/V4-Pro pairing covers almost
  everything (M3's 1M window, V4 Pro's execution strength).
- **Apply path — SwitchModel must be SPLIT, not wrapped.** `Engine.Run` claims the task slot
  (sets `e.cancel`) at turnloop.go:26-35, BEFORE the prologue — and `SwitchModel` unconditionally
  refuses while `e.cancel != nil` (engine.go:446-451). An advisor that "wraps SwitchModel" from
  the prologue would be refused on EVERY session and, under the silent-fallback rule, never
  apply anything with no visible symptom. Split it: an unexported
  `applyModelSwitch(ctx, role, id, name, addendum)` carries the full discipline (record
  `InvalidationModelSwitch` BEFORE the next request, roll back on persist failure, re-arm
  prefix-shape persistence) WITHOUT the busy check; the exported `SwitchModel` keeps the busy
  check for the CLI path. The advisor calls `applyModelSwitch` from the prologue — safe because
  it runs before this session's first Chat, which is the exact hazard the busy check exists to
  prevent. It is per-role (main+sub change = two calls → two invalidation events), and it is
  HARD-GUARDED by `Requests == 0`: after the first request the apply path refuses
  unconditionally, making "models never change mid-session" a mechanical invariant, not a
  convention. **The advisor test suite must exercise the switch THROUGH `Engine.Run`**, not in
  isolation, so the busy-refusal mode can never regress silently.
- **Mid-session: the fresh-session advisory (no model calls, no switching).** When a NEW task in
  an ongoing session is classified Large/Epic AND shares no scope terms with the session's
  banked knowledge (reuse `scopeTerms`, knowledge.go:183-193 — the same primitive the linker's
  relatedness predicate uses), emit ONE notice: *"This looks like a different kind of work than
  this session — a fresh session (`/new`) would fit it with a clean model choice and a fresh
  cache."* Never blocks, never repeats within a session (one-shot flag on the engine). This is
  the owner's "significantly different or highly complex → better to start a new session" rule,
  implemented with zero extra model calls.
- **Prompt surgery (part of the single epoch)**: remove `model: %s` from
  `PromptEnvironmentTemplate` (instructions/prompt.go:111-115) and the model name from
  `PromptDelegationOnTemplate` (141-151), AND the render/construction sites in lockstep —
  dropping a `%s` verb while the Sprintf still passes the old arg count renders
  `%!(EXTRA string=…)` garbage into the cached prefix (compiles fine; only goldens catch it):
  orchestrator/prompt.go:106 (ENVIRONMENT Sprintf) and :90 (DELEGATION Sprintf),
  `PromptContext.Model`/`SubagentModel` field deletions (prompt.go:21/25), construction at
  runtime_build.go:580, and the `applyModelSwitch`/`SwitchModel` prompt-field refresh
  (engine.go:462/470 — only `ModelAddendum` survives). The family `PromptAddendum` is
  family-specific and MUST remain. With models session-frozen this surgery is belt-and-braces,
  but it makes the prefix uniform across same-family models and removes the last ambient
  model-name display surface.
- **Continuation stability**: `subagentModelID()` (contextlink.go:313-319) reads
  `SubagentModelID`, which is now frozen for the session — so the execution chain (§7 F3) is
  model-stable for the WHOLE session by construction, which is precisely what session-long
  sub-agent cache reuse requires.
- **On "combination of models"**: the per-session main+sub+utility split IS the deliberate,
  cache-safe realization of "optimal model or combination" — a strong-reasoning planner (M3)
  composed with a fast executor (V4 Pro) and a cheap instructor (Flash). Within-session model
  changes and within-task composition are explicitly rejected: they fragment the caches this
  architecture exists to protect (§15).
- **Profile coverage**: `ResolveModelProfile` matches by name substring; the advisor chooses
  only catalog ids, so any id it returns resolves to a real profile (unknown names fall to the
  generic digest-only profile — acceptable degradation, never an error).

### E3. Session advisor system prompt (v1 draft — utility model, `:sub:advisor` pin)

```
You are the session advisor for MuhiyaCode, a coding agent. A new session is starting.
Decide whether the CONFIGURED model pairing fits this session's first task, or propose a
better pairing from the AVAILABLE MODELS list.

Respond with ONLY one JSON object:
  {"keep": true}
or
  {"main": "<id>", "sub": "<id>", "why": "<one short line>"}

"main" plans and orchestrates the whole session: it analyzes requests, writes the task
checklist, and instructs the execution agent. It needs strong reasoning and a large context.
"sub" executes: it edits files, runs commands, and reviews, on a long-lived cached stream.
It needs strong coding execution and cheap tokens; prefer continuation-support models.

Rules:
- The pairing is FIXED for the entire session — judge the session's likely needs from the
  first task: complexity, number of components, required output quality, and what breaks
  if a model falls short.
- {"keep": true} is the right answer unless the configured pairing clearly cannot serve
  this session (e.g. the workspace or task needs a context window the configured main
  lacks, or a language/domain the executor is weak in).
- Choose ONLY from AVAILABLE MODELS. Copy ids exactly.
- Never propose a premium model for a session that starts with chat, questions, or small
  fixes — the configured defaults already handle those well.
```

The harness appends: `CONFIGURED` (main/sub with windows), `AVAILABLE MODELS` (id — window,
family, continuation), `WORKSPACE` (size bucket, languages), `TASK` (the first prompt). The
prompt is registered in the instructions registry (Sidecar class — its own isolated stream,
zero main-prefix cost) so the audit suite and dump golden govern it.

### E4. Config & fallbacks

- `settings.Provider.Advisor` (new): `"auto"` (default) | `"off"` (never runs; the configured/
  seeded models are used as-is). Settable via `muhiyacode config set advisor off`. New settings
  field ⇒ new wiring-inventory row.
- First-run seed unchanged: MiniMax-M3 main + DeepSeek-V4-Pro sub (`AssignFreshDefaultModels`),
  so a fresh install runs the owner's preferred pairing before the advisor ever fires.
- User pinning wins: `config set model` / `config set subagentModel` marks the roles
  user-pinned (`Provider.RolesPinned bool`, new) — the advisor then never overrides them
  (it can still be consulted and ignored, or skipped entirely; skip is simpler — spec: skip).
- One-shot/line mode: identical behavior (the advisor lives in the engine, not the TUI).
---

## 7. Workstream F — Plan/execute role split, subagent role names, session-long execution chain

### F0. Strict plan/execute split (owner directive 2026-07-20 — supersedes "standard runs direct")

The MAIN model plans; the EXECUTION model executes. The main model must not perform execution
or any work unrelated to planning: its job is to analyze the user's request, create the
execution strategy (tasks.md), and give the execution subagent detailed instructions. This
holds for EVERY task involving workspace changes — including small ones ("fix this typo" =
main composes a one-line handoff, the execution chain applies it on its warm cache). Chat,
questions, and analysis are answered by the main model directly (that IS its job).

**Two layers, both required:**

1. **Instruction layer** (part of the epoch): rewrite the operating contract
   (instructions/prompt.go `PromptOperatingContractBody`) around the split: *"You plan and
   orchestrate. The execution agent executes. For any workspace change beyond tasks.md,
   compose a detailed handoff — goal, relevant files, constraints, done-criteria — and launch
   ONE `general` subagent (`run_subagent`). Read freely; never edit directly."* The
   DELEGATION prompt section and `ToolRunSubagentDescription` reinforce it (F2).
2. **Gate layer** (replaces the deleted plan-mode `BlocksMutation` branch with a permanent
   role rule): in `gatedExecute`, when the scope is the MAIN loop (not a subagent) and the
   call is a WORKSPACE mutation — `edit_file`, `write_file`, `multi_edit`, `apply_patch`, or
   a mutating `run_shell` (reuse the existing `IsReadOnlyShell` classification) — and the
   call's path set does not include the workspace `tasks.md` (reuse Workstream C's path-set
   extractor), block with a new registered gate text `gate.role.execution`: *"Execution runs
   in the execution agent. Launch `run_subagent` (kind `general`) with instructions for this
   change; the main session plans and reviews."* The existing repeat limiter naturally bounds
   retry loops. `save_memory`/`edit_memory` are NOT workspace mutations — memory writes are
   planning work and stay allowed. `propose_changes` stays available to the main loop (it is
   an approval flow, not a direct mutation). Gate text registered with a `StatesRule` pair in
   instructions/gates.go so the audit suite links it.

**Budget consequence (a deadlock trap if missed)**: `classAgents` is currently
`0/0/0/2/5/8` (chat/tiny/small/standard/large/epic — classify.go:223). Tiny/small tasks with
zero agent budget + a main loop that cannot mutate = an unfixable task. New table:
**`0/1/1/2/5/8`** — every class that can involve mutations affords at least the one execution
agent. The `agents=0 (no run_subagent)` brief line then applies only to chat class; update
`buildBrief` (classify.go:249-268) and the pinned brief tests (`subagent_budget_test.go`
TestZeroAgentBriefStatesProhibition re-fixtures to chat class,
`delegation_regression_test.go` class-cap assertions, meta_test budgets).

**Review flow**: the AutoReview nudge and review subagents already run on the execution model
— unchanged. The main model synthesizes the final answer from reports — that is planning-side
work and allowed.

### F1. LLM-generated role names WITHOUT breaking the cache

The kind string is load-bearing in eleven places (schema enum, H1 validation, capability map,
readOnly predicate, MaxTurns, pin suffix `:sub:<kind>`, stable system message → SystemHash,
`SubagentContextRecord.Kind`, CL-1 kind-pair matching, knowledge `TaskKey` reuse, handoff role
mapping). A free-form name in any of them fragments the provider cache permanently. Therefore:

- **Internal capability kinds shrink to three and stay fixed**: `explore` (read-only),
  `general` (full tools), `review` (read-only + R-D2 continuation masking). The `plan` kind dies
  with Workstream B. Kinds keep determining: Allowed map + readOnly flag, MaxTurns, pin suffix,
  per-kind stable system message, record Kind, CL-1 matching, knowledge reuse. Fail-closed
  semantics stay: unknown kind → most-restricted treatment.
- **New optional `role` argument on `run_subagent`** (e.g. `"auth-flow-mapper"`,
  `"settings-page-builder"`): flows ONLY into per-run surfaces — `HandoffContract.Role` (already
  per-run user-message content, exactly where PH-1 says per-run content belongs),
  `AgentEvent.Role` for the TUI chip, `LinkOutcome`, knowledge fact titles. The existing `title`
  argument already proves this pattern (model-chosen, display-only, zero cache impact).
- TUI chip (render_tool.go:280-282) shows the role name; the `(model)` suffix is dropped with
  the model display (Workstream E) — `AgentEvent.Model` plumbing stays for internals.
- **The user asked for the END of generic identifiers on screen, so the fallback matters**: the
  `role` argument stays schema-optional (cache-safe), but the rewritten tool description states
  a role is expected on EVERY dispatch, and the chip's display fallback when role is absent is
  the already-mandatory free-form `title` — the bare kind string (`general`/`explore`/`review`)
  is never rendered as the identity label. A render test pins this.
- Old persisted records with `Kind: "plan"` simply never match again — clean degradation;
  `RestoreAgentRecords` already tolerates unknown shapes.

### F2. Delegation discipline (every subagent justified — but execution is always delegated)

Rewrite `ToolRunSubagentDescription` (epoch change, once) around three points:

1. **Execution always goes to the execution agent; extra agents must earn their place.** The
   ONE serial `general` chain is the workhorse for all workspace changes (F0). Additional
   dispatches — an `explore` for a genuinely unknown area, a `review` for verification — are
   justified only when isolation pays (a large read that would bloat the main context, an
   independent verdict). Never spawn an agent whose job the execution chain's next
   continuation could do.
2. Name the agent for its actual job (`role`), state exactly one deliverable, serial only.
3. Harvest the serial-chain + cheap-continuation guidance from the deleted
   `PipelineImplementDelegateBody` into this description — same-kind follow-ups CONTINUE the
   predecessor's warm stream (feature 012), so the chained execution dispatch is the cheap
   path, not a new cost.

Budgets: `BudgetFor`'s min(effort, class) derivation stays; the class table changes to
`0/1/1/2/5/8` (F0). The "agents<=N" brief line remains the per-task governor.

### F3. Session-long execution-chain continuity (the owner's sub-agent cache directive)

The execution sub-agent's cache and context must persist across the WHOLE session: when the
user continues the session with a new prompt, the new execution dispatch reuses the previous
sub-agent's stream and context whenever possible. Feature 012 already built 90% of this —
continuation-first dispatch over the predecessor's verbatim transcript on the stable
`:sub:general` pin. One rule changes:

- **CL-1 amendment — cross-task continuation becomes the DEFAULT for the execution chain.**
  Today `decideLink` prefers same-task candidates (`taskSeq`) and accepts a cross-task
  predecessor only when `relatedFollowUp` finds scope-term overlap (contextlink.go:67-75,
  178-192). Amended: for `Kind == "general"`, a cross-task predecessor is eligible WITHOUT the
  relatedness requirement (new reason code `session-chain`) — the workspace itself is the
  shared subject, and the session-frozen execution model (§6 E2) guarantees the model-identity
  criterion holds all session. For `explore` and `review`, the relatedness predicate stays
  (research context is topic-specific; carrying it across unrelated topics pollutes).
- **Every other CL-1 guard stays untouched and does the safety work**: terminal-shape
  clean-done, staleness majority-decline against end-of-run fingerprints (self-edits never
  stale), window fit with 20% headroom (an over-long chain declines to digest-seeding
  automatically — that is the correct pressure valve), byte-identical replay with drift-abort,
  provider `ContinuationSupported`. DeepSeek V4 Pro is `ContinuationSupported` +
  `EffortPinned` (gateway/model.go:111-112) — the default execution model rewards exactly this
  design with token-0 identity matching in 64-token blocks.
- **Handoff on a continued chain**: unchanged mechanics (one appended user message carries the
  new instructions — contextlink.go planDispatch:287-291); the main model's handoff text now
  says what changed since the last dispatch rather than re-explaining the workspace. The
  scoped knowledge briefing already rides it.
- Tests: new `decideLink` rows (general cross-task continues with reason `session-chain`;
  explore cross-task still requires relatedness; window-overflow chain falls back to digest);
  re-fixture `TestFollowUpRelatednessPredicate` for the split behavior; a live-shaped E2E
  (offline scripted) where task 2's execution dispatch replays task 1's transcript verbatim.
- `/context` and the bench JSON already surface per-pin pairing rates and LinkOutcome reasons
  — the session-chain hit rate is measurable from day one (Constitution VI: provider-reported
  or "unavailable", never estimated).

### F4. Context efficiency (already built — verify, don't rebuild)

The "stop re-reading files / resending context" ask is served by machinery that survives B's
surgery; this workstream just verifies each piece post-surgery: duplicate-read dedupe
(gates.go:163-174), H2 failed-call short-circuit, knowledge reuse fast path (now explore-only),
scoped briefings on handoff, context linking continuation (the big one), history fold/compaction,
and the per-pin pairing measurement that makes regressions visible in `/context`.

---

## 8. Workstream G — Core/TUI split (desktop-ready core)

The boundary is already 7/10: orchestrator never imports tui, `contract` is dependency-free,
three frontends (TUI, RunLine, console one-shot) share the engine. The remaining work, ranked:

1. **New package `internal/app`** (layering position: above orchestrator, beside tui/command;
   add to `allowedInternalImports` in `arch/layering_test.go`). Move the frontend-service types
   OUT of package tui: `Runtime`, `Actions`, `HydratedRuntime`, `Skill`, `UsageData`/
   `UsageWindow`, `MCPActions`/`MCPServerInfo`/`MCPAddSpec`. `internal/command` then implements
   `app` interfaces and stops importing `internal/tui` except in root.go's launch call.
2. **`app.Session` facade** wrapping `*orchestrator.Engine` with exactly the surface the TUI
   uses: `StartTurn(prompt, skills)` async, `QueueSteering`, `Cancel`, `Close` (Cancel+WaitIdle —
   currently duplicated in tui/run.go:52-57 and Application.Close), `Compact`, `ContextReport`,
   `SetEffort`, `HarnessEvents`, `IsBusy`. The `uithread_guard_test.go` allowlist is the
   ready-made spec of the synchronous-read subset. TUI's `Runtime.Engine` pointer is replaced by
   the facade; the tui→orchestrator import shrinks to zero (after A/B delete `GoalActive` and
   the lifecycle reads, only `ContextReport`/`NormalizeEffort` remain — both move behind `app`).
3. **Transport-agnostic event types in `app`**: lift Bridge's reified messages (StatusEvent,
   StreamEvent, ToolStart/Output/End, UsageEvent, ContextEvent, AgentEvent, ChecklistEvent,
   TaskCompleteEvent, ModalRequest-with-reply) so `contract.Callbacks` is adapted ONCE, with the
   35ms coalescer in the shared adapter. The tea-specific `program.Send` shim stays thin. The
   desktop app subscribes to the same typed stream.
4. **Prompt assembly moves into `StartTurn`**: skill `<skill>` wrapping, paste expansion, NFC
   normalization (today in tui/keys.go:205-277; RunLine only does NFC — an existing frontend
   divergence this FIXES). Note: this changes one-shot/line-mode prompt bytes; prompt-stability
   tests may need fixture updates.
5. **`SessionOpened` bundle**: session open/switch becomes one core operation returning
   {runtime, recent events, notices, context report, usage} — consumed by modals.go's new/resume
   handler and applyHydration, eliminating the scattered per-field reads (the reset-list class of
   leak documented at modals.go:79-115).
6. **Preserved hazards** (each has a live incident behind it): the Send-deadlock discipline
   (mutations off the UI thread — keep `uithread_guard_test.go` green), Confirm/Ask parking +
   `replyModalOpen` guard, the coalescer, the nil-engine hydration window, `WaitIdle` as the
   DB/MCP close gate.
7. **RunLine as the conformance test**: rewrite it against the `app` facade with zero
   orchestrator imports — if RunLine can, the desktop app can.

Deferred (not v1.1.0): actually building the desktop frontend; IPC/JSON-RPC serialization of the
event stream. The package boundary is the deliverable.

---

## 9. Workstream H — Chrome cleanup: no welcome text, an update checker instead

### H1. Remove the "Welcome to MuhiyaCode" text

- `internal/tui/render_transcript.go:97-109` — delete `firstRunHint()` entirely and its call
  site in `renderTranscript` (the empty-session branch). An empty session shows an empty
  transcript; the composer placeholder and the header already orient the user. If the
  empty-transcript branch then renders nothing, keep the layout height accounting correct
  (`layout()` counts rendered lines — return "" and verify no negative-height math).
- `internal/tui/modals.go:260` — the ONBOARDING modal title is also "Welcome to MuhiyaCode".
  The owner asked for the welcome message and "any related introductory text" gone: retitle the
  onboarding modal to a plain functional title (e.g. "Set up MuhiyaCode") — do NOT delete the
  onboarding flow itself (it gates first-run login/config).
- Tests to update: `application_state_test.go:25-29` (TestFirstRunState — invert to assert the
  cue is ABSENT, following the plan_command_removed_test tombstone pattern),
  `robustness_test.go:102-114` (TestFirstRunCueShownThenReplaced — delete or invert),
  `onboarding_test.go:24,104,126,148` (retitle assertions).

### H2. Update checker in the header

Small, quiet, top-of-interface: when a newer version exists, header line 1 gains a segment
`⟳ Update available 1.2.0 (you have 1.1.0)`; when up to date it renders nothing at all.

- **Where the version comes from**: npm is the distribution channel, so query the registry —
  `https://registry.npmjs.org/muhiyacode/latest`, read `.version`. (The npm-postinstall script
  already downloads by package version, so the registry is authoritative for "latest".) No new
  dependency: `net/http` + `encoding/json`.
- **Where it runs**: NOT in the TUI and NOT on the engine's request path. Add
  `internal/updatecheck` (leaf package; layering entry alongside buildinfo — imports only
  stdlib + buildinfo). `command/runtime_build.go` fires it once at application open in its own
  goroutine with a 3-second timeout, writes the result to a cached sidecar under the state dir
  (`update_check.json`: `{checked_at, latest_version}`), and passes the resolved state into
  `tui.Options`/`Runtime`. **TTL: 24h** — one network call per day, never per session start.
  Failure (offline, timeout, non-200, malformed) is silent and cached as "unknown"; the
  segment simply does not render. Never blocks startup, never prints an error.
- **Comparison**: semver-compare `latest` vs `buildinfo.Version` with a small pure helper
  (`updatecheck.Newer(latest, current string) bool`) handling `1.10.0 > 1.9.0` correctly and
  returning false on any unparsable input. Unit-test the comparator table (equal, older,
  newer, double-digit minor, malformed, empty).
- **Render**: in `renderHeader` (render_header.go:25-34), after the brand/version segment —
  reuse the existing segment slice so narrow widths drop it before the workspace path is
  truncated (same discipline as the deleted `subSeg`). Style: `m.palette.brandSoft` for the
  label, `faint` for the versions. Line 1 already loses two segments (model, sub) in
  Workstream E, so there is room.
- **Opt-out**: `MUHIYACODE_NO_UPDATE_CHECK=1` skips the network call entirely (respects
  air-gapped/CI use). Document in README.
- **Privacy**: an unauthenticated GET to the public npm registry — no telemetry, no user data
  in the request. State it plainly in the README line.
- Tests: comparator table; a fake-registry HTTP test for the fetch+cache path (the
  `application_test.go` fake-endpoint pattern applies); a header render test with the
  update-available state and one without; TTL respected (a fresh sidecar suppresses the call).
- Wiring inventory: new settings/state surface + header segment ⇒ new "wired" rows.

---

## 10. The single cache epoch — mechanics

**"Exactly one epoch" is a RELEASE-level guarantee, not a single-commit mechanic.** The three
byte-compare goldens and the bidirectional wiring-inventory guard are ordinary CI tests — a
phase commit that changes prefix bytes without regenerating them is red. So: **each phase
regenerates the goldens and flips its own wiring-inventory rows in that phase's commit** (every
branch commit stays green), and users — who only ever see released binaries — still experience
exactly one shipped prefix change and one attributed cold start, because all phases ship
together in v1.1.0. Phase 7 is a verification/consolidation pass, not the sole regeneration
point.

**Prefix-byte changes accumulated across the release**: remove
`update_plan`/`exit_plan_mode`/`read_plan` schemas; reshape `run_subagent` (drop 'plan' kind,
add `role`, new description); delete `PromptOrchestrationPipelineBody`; rewrite
operating-contract bullet 3 (tasks.md); remove `model: %s` from ENVIRONMENT and the model name
from DELEGATION; new tasks.md convention text; per-kind subagent system-text rewrites (§B4).

**Per-phase regeneration (whenever that phase touched prefix bytes):**

1. `go test ./internal/instructions/... -run TestDump -update` → `instructions_dump.golden` +
   `prefix_bytes.golden`
2. `go test ./internal/orchestrator/... -run TestWiring_PrefixBytesGolden
   -update-prefix-golden` → `prefix_bytes_wire.golden`
3. `specs/010-ultimate-consolidation/wiring-inventory-data.md`: flip to "removed" — read_plan
   (row 52), update_plan (64), exit_plan_mode (71), /goal (92), /model (94), Callbacks.PlanUpdate
   (210); update run_subagent (70) and provider.* rows (152-154); ADD wired rows for: tasks.md
   checklist feed + ChecklistUpdate callback, `role` argument, `provider.advisor` +
   `provider.rolesPinned` settings, the advisor aux call, the planning skill, the update-check
   state, the `gate.role.execution` text. The guard derives live surfaces from real code
   (AST/reflection), so it fires the moment production changes land — the doc edit rides the
   same commit as the code.

**Phase-7 consolidation pass:**

4. Review the CUMULATIVE prefix diff (old release goldens vs new) as one reviewed artifact —
   this is the recorded epoch.
5. Re-baseline `promptBaselineChars` (currently 5789, prompt_budget_test.go:22) DOWNWARD.
   Note: the ratchet is ceiling-only (`got > promptBaselineChars` fails; shrinkage is green),
   so CI will NOT force this — it is a deliberate manual tightening; extend the doc-comment
   history (3642→4522→5709→5789→new).
6. Changelog: disclose the upgrade cost — one cold start per resumed session after updating
   (the prefix-shape drift notice attributes it).

**Do not suppress the wire prefix-shape guard (turnloop.go:471-486)** — it hard-errors on
mid-session settled-byte changes by design; all epoch changes are across-process-restart only.

---

## 11. Test plan (from the 53-file blast-radius audit: 16 DELETE / 24 REWRITE / 13 KEEP)

**Delete (16 files)**: goal_test, goal_hardening_test, plan_lifecycle_test (relocate
`lifecycleEngine` first), plan_mode_phase3_test, plan_intent_test, plan_artifact_test,
plan_stepcap_test, pipeline_test, pipeline_gates_test, lifecycle_test, state/plan_state_sidecar,
state/goal_sidecar, state/lifecycle_migrate, arch/compat_boundary, tui/model_switch_test,
tui/plan_command_removed_test.

**Rewrite highlights (24 artifacts)**:
- `faultinjection_test.go`: excise ~11 plan/pipeline rows + 5 standalone plan funcs; the
  three-outcome `assertRecoveryInvariant` harness and `faultTurnCeiling=80` survive unweakened;
  add new rows: tasks.md-write-fails-recovers, advisor-timeout-keeps-configured-models,
  main-loop-mutation-blocked-then-delegated, tiny-task-delegates-and-completes,
  session-chain-continuation-after-external-edit.
- `gauntlet_offline_test.go`: scenario 1 (pipeline E2E) → new E2E: standard task runs direct,
  maintains tasks.md, one serial subagent, honest completion. `forbiddenStrings` is an
  append-only incident ledger — ADD "Proceed with Plan", "Plan mode", "/model" as tombstones,
  don't prune old entries.
- **Prefix-stability invariants must survive their plan-shaped carriers** (re-fixture, never
  delete): `TestPipelineStaticPrefixStableAcrossPhases` → prefix-stable-across-a-session,
  `TestToolsArrayStableAcrossModeToggles` → re-fixture with effort/permission toggles,
  `TestPlainFollowUpTailStaysWithinBudget` (banned-blocks list update),
  `TestPrefixStableAcrossTurnsAndClasses`, the whole `cachehit_guard` suite (two fixture lines).
- `contextlink_test.go`: KEEP verbatim minus TestReadPlanTool, 3 TestPhaseReadGate*, and
  TestResearchAutoTransitionsWithoutDispatch.
- Relocate the two generic keepers out of pipeline_polish_test.go; keep
  TestReadOnlySubagentShellGate* out of pipeline_efficiency_test.go.
- `completion_audit_test.go`: re-express the disclosure invariant against tasks.md.
- New removal pins (template: plan_command_removed_test.go): TestGoalCommandRemoved,
  TestModelCommandRemoved.
- New suites:
  - **Session advisor**: decision parse (`{"keep":true}` and the switch shape),
    fallback-on-timeout/malformed keeps configured models, invalidation-event recorded on an
    applied switch **exercised THROUGH `Engine.Run`** (never the advisor function in
    isolation), runs only when `Requests == 0`, skipped when `Advisor == "off"` or roles are
    user-pinned, catalog-annotation rendering, TTL catalog refresh picks up a newly added
    gateway model, utility-model resolution falls back when no Flash-class model exists.
  - **Session model stability (the headline invariant)**: a multi-task scripted session ends
    with the same `ActiveModelID`/`SubagentModelID` it started with, and `applyModelSwitch`
    refuses after the first request.
  - **Session-chain continuation**: task 2's `general` dispatch continues task 1's transcript
    with reason `session-chain` and no relatedness overlap; `explore` cross-task still
    requires relatedness; an over-long chain declines to digest on window fit; re-fixture
    `TestFollowUpRelatednessPredicate`.
  - **Role gate (F0)**: main-loop `edit_file`/`write_file`/`multi_edit`/`apply_patch`/mutating
    shell is blocked with the `gate.role.execution` text; the same call from a subagent scope
    passes; a main-loop write to tasks.md passes; `save_memory` passes; a tiny-class task
    delegates its mutation and completes (no deadlock).
  - **Checklist feed**: write→parse→callback, plain-GitHub-checklist fixtures, a SUBAGENT edit
    to tasks.md fires ChecklistUpdate, one apply_patch touching main.go + tasks.md fires it,
    malformed file tolerated, panel gating.
  - **Planning skill**: listed, loads, recall AND save_memory directives present.
  - **Role names**: record.Kind stable while Role varies; chip falls back to title, never
    renders the bare kind.
  - **Chrome (H)**: welcome-string absence pin; semver comparator table; fake-registry
    fetch+cache test; header renders the update segment when newer and nothing when current;
    TTL suppression; `MUHIYACODE_NO_UPDATE_CHECK=1` skips the call.
- `state/config_defaults_test.go`: auto-assign family stays as the SEED policy (the M3 +
  V4-Pro first-run pairing test becomes a headline pin, not a deletion).
- `instructions_wiring_test.go` is touched TWICE: it hosts the epoch golden
  (TestWiring_PrefixBytesGolden) AND loses the two plan-example validator tests
  (TestWiring_PlanStepExamplePassesRealValidator / PlanNote…, 105-124 — they call the REAL
  `missingPlanStepRequirements`/`labeledPlanSection` from pipeline.go, deleted in B1), the
  `"subagent.plan": "plan"` allowlist-context entry (:63), and `exit_plan_mode` in the
  synthetic-tool list (:98).
- `mixed_provider_test.go`: drop the `engine.lifecycle = …LifecycleImplementing` fixture (:58 —
  it existed only to bypass pipeline entry).
- `pipeline_fastpath_test.go`: BOTH tests need action — TestPipelineStaticPrefixStableAcrossPhases
  re-expresses as prefix-stable-across-a-session, and TestPipelineFastPathStaysDirect
  (:12-33, LifecycleState assertions) gets a named replacement: "standard task stays direct".
- `handoff_test.go`, `delegation_prompt_test.go`, `subagent_budget_test.go`,
  `delegation_regression_test.go`, `request_assembly_test.go`, `meta_test.go`,
  `reviewgate_test.go`, tui suites (tui_test, ingest_test, render_todos_test +
  render_language_test together, mouse_test /goal probe swap, agent_rehydrate_test phase-key
  fixture), `telemetry_emit_test.go` (RecordPipelineDegradation API re-key),
  `contract/tooltarget_test.go` fixture swap, `state/state_test.go:136` session-dir assertion,
  `benchmarks/delegationbench` (retire the delegation workload's pipelineAudit — §B3).

**Keep untouched**: the entire feature-012 cache suite, engine prefix tests,
prompt_stability_test, arch size/layering tests (layering gains the `internal/app` entry),
uithread_guard (prune stale allowlist keys; any new on-thread engine read needs a justified
entry), wiring_inventory_test (code side auto-adapts; the DOC changes).

**Full verification per phase**: `go build ./... && go vet ./... && go test ./...` plus
staticcheck (`go run honnef.co/go/tools/cmd/staticcheck@2025.1.1`); 800-line budget stays
allowlist-free (new advisor/checklist/updatecheck files must fit).

---

## 12. Sequencing — eleven phases, each independently green

| Phase | Content | Depends on |
|---|---|---|
| **0. Prep** | Relocate trapped helpers (planrender survivors, resetLinkTaskState/finalizeLinkStats.Links, lifecycleEngine, generic tests out of pipeline_* files); new call site for updateTaskReviewOutcome | — |
| **1. Goal + plan deletion** | Workstreams A + B complete (they share modeMu and mutual-exclusion edges — one change). Suite green with plan tests deleted/rewritten | 0 |
| **2. tasks.md checklist** | Workstream C: parser rewrite, ChecklistUpdate, shared-gate feed, renderTodos rewire, completion disclosure v2 | 1 |
| **3. Plan/execute split** | Workstream F0: role gate + operating-contract rewrite + `classAgents` 0/1/1/2/5/8; main model can no longer mutate the workspace | 2 |
| **4. Planning skill** | Workstream D: built-in skill entry + body (recall + write-back) | 3 |
| **5. `/model` UI removal** | Workstream E1: TUI/model deletions, `SwitchModel` split into `applyModelSwitch`, notice copy, TTL catalog refresh. CLI `config set model` KEPT | 1 |
| **6. Session advisor** | Workstream E2-E4: utility-model aux call gated on `Requests == 0`, apply path, fresh-session advisory, `Provider.Advisor`/`RolesPinned` config | 5 |
| **7. Role names + session chain** | Workstream F1-F3: `role` argument + chip fallback, delegation text, CL-1 `session-chain` amendment | 3, 6 |
| **8. EPOCH CONSOLIDATION** | Review the cumulative prefix diff (the recorded epoch); tighten promptBaselineChars; verify goldens/wiring doc consistent across 1-7 | 1-7 |
| **9. Chrome** | Workstream H: delete `firstRunHint` + retitle onboarding modal; `internal/updatecheck` + header segment | 5 (header segments freed) |
| **10. Core/TUI split** | Workstream G: internal/app, facade, events, prompt assembly, SessionOpened; RunLine conformance rewrite | 1-9 |
| **11. Ship** | docs/agent-design.md + architecture.md + prompt-caching.md + **docs/migration.md** (rewrite the "Plans/tasks" compat row: session plan.md/plan_state.json/tasks.md retired, stale sidecars ignored, new workspace tasks.md) updates, README (update-checker + opt-out env var), CHANGELOG, memory files, version 1.1.0, tag, npm | 10 |

Every phase commit is green on its own: a phase that changes prefix bytes regenerates the
goldens and edits the wiring-inventory doc IN that commit (§10). Phases land on a feature
branch; phase 8 reviews the cumulative prefix diff as the recorded epoch and tightens the
prompt-budget baseline; the branch merges as one release so users see a single prefix change.

---

## 13. Risk register

| # | Risk | Mitigation |
|---|---|---|
| 1 | Cache damage from incremental prefix drift | Release-level epoch (§9): per-phase golden regeneration keeps CI green, one shipped prefix change; wire guard stays armed |
| 2 | Deleting a shared helper with its plan-named file (planrender, rolegate, reviewgate outcome stamping) | Phase 0 relocations before any deletion |
| 3 | A mid-session model change silently degrades every continuation to digest ("model-changed") and cold-starts the main prefix | Models are FROZEN after the first request — `applyModelSwitch` refuses unconditionally once `Requests > 0` (§6 E2); contextlink_test is the tripwire; LinkOutcome reasons surface in bench JSON |
| 4 | taskSeq reset lost in rolegate deletion → same-task chaining dies quietly | Explicit relocation + TestDecideLink* keep pinning lineage |
| 5 | Checklist feed misses a write path (multi-file apply_patch, subagent scope, shell redirect) | Shared-gate hook covers both scopes; path-SET extractor covers multi-file patches; tests pin both; shell writes to tasks.md acceptable gap (documented) |
| 5b | Advisor apply path silently refused (Run claims the task slot before the prologue; SwitchModel busy check) | applyModelSwitch split (§6 E2); advisor tests run THROUGH Engine.Run |
| 6 | Model over-claims completion once the disclosure guard is gone mid-transformation | Disclosure v2 lands in the SAME phase (2) that removes v1's carrier |
| 7 | **Plan/execute split deadlocks small tasks**: main loop can't mutate and `classAgents` gives tiny/small ZERO agent budget | `classAgents` becomes 0/1/1/2/5/8 in the SAME phase as the role gate (§7 F0); fault row: a tiny task's mutation is delegated and completes |
| 7b | The role gate blocks tasks.md itself, or blocks memory writes → the main model can't plan | Carve-outs are explicit and tested: tasks.md path-set match, `save_memory`/`edit_memory` are not workspace mutations, `propose_changes` allowed |
| 8 | E2E/fault suites rescripted for the old flow mask regressions | Every deleted scenario gets a named replacement covering the surviving invariant (§11) |
| 9 | Stale on-disk sidecars (plan_state.json, session tasks.md, goal.json) confuse resume | Readers deleted, not erroring; leftover files ignored; drift notice attributes the one-time cold start |
| 10 | app-package extraction breaks the Send-deadlock discipline | uithread_guard test survives and gates the facade; mutations stay off the UI thread by construction |
| 11 | Cross-task `session-chain` continuation carries stale or wrong context into a new task | Every other CL-1 guard stays: staleness majority-decline on end-of-run fingerprints, clean-done terminal shape, window fit with headroom, replay-drift abort. Narrowed to `general` only — explore/review keep the relatedness requirement |
| 12 | Update checker adds startup latency or leaks data | Own goroutine, 3s timeout, 24h TTL sidecar, silent failure, `MUHIYACODE_NO_UPDATE_CHECK=1`; unauthenticated public-registry GET only |

---

## 14. Acceptance criteria

1. `/goal`, `/model`, Planning Mode, the plan pipeline, `update_plan`/`read_plan`/
   `exit_plan_mode`, and the "Proceed with Plan" modal do not exist anywhere (removal pins).
2. No planning ceremony: "fix these two bugs" produces no plan and no approval step — the main
   model reads, decides, and hands the change to the execution agent. "Build me a site"
   executes immediately with a tasks.md checklist filling in. "Make me a plan for X" produces
   a plan as the answer and stops.
3. tasks.md in the project root drives the live to-do panel; completion answers never contradict
   open checklist items.
4. **Plan/execute split holds**: with the main model active, no workspace file except tasks.md
   is ever mutated from the main loop (gate test); every mutation lands through the execution
   subagent; no class of task deadlocks for lack of agent budget.
5. **Models are session-stable**: `ActiveModelID`/`SubagentModelID` are identical at the first
   and last request of a session (invariant test); no `InvalidationModelSwitch` event is ever
   recorded after `Requests > 0`; defaults are MiniMax M3 main + DeepSeek V4 Pro execution, and
   the utility/instructing calls resolve to a Flash-class model.
6. **Session-long execution cache**: in a scripted two-task session, the second task's
   execution dispatch continues the first task's transcript (LinkOutcome reason
   `session-chain`, provider-reported cache share > 0), with no relatedness requirement between
   the tasks.
7. Users never manage models in the UI: no model name in ambient chrome (header, chips,
   notices, command palette) and no TUI command; `muhiyacode config set model` remains the
   user's pinning surface and `/context` remains the diagnostic surface where the current
   models and per-model usage are disclosed.
8. Subagent chips show LLM-chosen role names (falling back to `title`, never the bare kind);
   pins/records/continuation-eligibility unchanged (contextlink suite green, verbatim).
9. No "Welcome to MuhiyaCode" string anywhere in the TUI (absence pin); the header shows
   "Update available <latest> (you have <current>)" only when a newer npm version exists, and
   renders nothing otherwise, offline, or with `MUHIYACODE_NO_UPDATE_CHECK=1`.
10. Exactly one recorded prefix epoch; goldens/baseline/wiring-doc consistent; post-upgrade
    resumed sessions show one attributed cold start, then normal pairing rates.
11. `internal/tui` imports zero `internal/orchestrator` symbols; RunLine runs entirely against
    `internal/app`; layering test enforces the new edge set.
12. Full suite green: build, vet, staticcheck, all tests, 800-line budget (no allowlist entries),
    prompt budget re-baselined downward.

---

## 15. Out of scope (explicitly deferred)

- The desktop app itself (this release delivers the shared core only).
- Boot-time tasks.md surfacing via ProjectContextProbe (needs a version bump — separate feature).
- Advisor learning/telemetry-driven policy (v1 is a static-prompt advisor; the per-model usage
  ledger already collects the data a v2 could learn from).
- **Mid-session model switching of any kind** — deliberately rejected, not deferred: it
  cold-starts the main prefix and breaks the execution chain's continuation, which is the
  opposite of this release's priority. The per-session main+execution+utility split IS the
  "combination of models"; a different workload gets a fresh session (§6 E2 advisory).
- Within-task multi-model composition (per checklist item) — same reason.
- In-app self-update (downloading/installing a new version). The checker only INFORMS; the
  user updates through npm.
- Live cost-gated probes/baselines from feature 012 (still awaiting owner approval — unchanged).
