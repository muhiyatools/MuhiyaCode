# UNIFIED SESSION OVERHAUL PLAN — remove subagents, one session, model ladder

**Status: READY FOR EXECUTION — audit complete (2 removal-inventory
investigators + firsthand design verification, 2026-07-21, branch
feature/native-agent-v1.1.0, on top of the uncommitted Phases 0-2 tree)**

Scope in one line: delete ~1,650 orchestrator lines + ~220 TUI production
lines of subagent machinery across ~30 live production files (117 files
reference it, 62 of them tests), rewrite the system prompt for the unified
flow in ONE cache epoch, then land the per-task model ladder on the existing
`applyModelSwitch` mechanism with hard pin/fit/catalog gates.

Executor: Opus 4.8. Owner directive 2026-07-21: **remove the subagent system
entirely** — modules, abilities, flow references, prompts, logic — and redesign
around ONE unified main session, with the model chosen per task complexity and
context transferred across model switches so provider caching keeps working.

> **This supersedes two standing directives.** (1) The plan/execute role split
> (owner directive 2026-07-20, NATIVE_AGENT_PLAN §7 F0) is REVERSED by the same
> owner. (2) The "model-driven dispatch" directive survives only in its negative
> half: no harness auto-dispatch machinery may return — there is nothing to
> dispatch. Memory files enshrining the old split must be updated when this
> plan lands (see §5).
>
> **Why (the evidence, from the live 2026-07-21 session):** the split's worst
> failure mode is structural — the main model generated a complete game file
> (12,551 output tokens, 133 s), the execution role gate then REFUSED it, and
> the dead payload was replayed as input on every later request (+12.6 k/turn
> at near-zero cache read). In a unified session that write simply succeeds.
> The split also forced every change through a dispatch round-trip (handoff +
> report + re-verification surface) that a single session does not pay.

---

## §0 Mission, non-negotiables, execution discipline

### Mission

One session. The main model plans AND executes with the full tool surface.
Task complexity picks the model (an LLM decision at task boundaries, with hard
harness gates); switches preserve append-only history so each model's
provider-side prefix cache warms independently and stays warm. The system
prompt is rewritten coherently for the unified flow. Everything subagent dies.

### Non-negotiables (Constitution digest)

- **I Correctness before optimization** — every change lands with a regression
  test; removal must not orphan machinery the main loop depends on.
- **III Deterministic stable prefix** — byte-identical prompt/tool JSON across
  turns *per model*. A model switch is a RECORDED invalidation, never silent.
- **IV/V Dynamic content rides tails; history is append-only** — a switch NEVER
  rewrites settled history (that is precisely what lets a previously-used
  model's cache re-warm on return).
- **VI Honest measurement** — provider usage fields and scripted byte counts.
- **VIII Improve, don't rewrite** — reuse `applyModelSwitch`, the shared gate,
  the existing trim/fold machinery; delete, don't re-invent.
- **X Verified improvements** — before/after on the fieldtest scenarios.

### Cache-epoch discipline

The prompt rewrite + tool-surface change (run_subagent deleted) is by far the
largest prefix change in the project's history — and it CANNOT be split: the
instructions audit ties every text naming a tool to the live registry, so the
prompt must change in the same commit that removes the tool. Therefore:

- **Phase 1 is one atomic commit** ("the great excision"): all code removal,
  the full prompt rewrite, every affected test deleted/rewritten, the three
  goldens regenerated ONCE, the prompt budget re-baselined (direction: see
  WS-P — system prompt grows a little; tool JSON shrinks a lot; net fixed
  prefix DOWN ≈3–4 KB).
- Every later phase is cache-safe (engine logic, TUI, model ladder) and lands
  in small commits.
- After every phase: `gofmt` / `go vet` / `go build` / `go test ./... -count=1`
  green. No exceptions, including mid-excision.

### Execution order

0. **Checkpoint + baseline.** The tree already holds three uncommitted bodies
   of work (stability pass, audit remediation, token-efficiency Phases 0–2).
   RECOMMENDED: owner commits the current tree first so the excision diff is
   reviewable. Then re-run `TestFieldTestSnakeGameBaseline` and record the
   pre-unification numbers in §4.
1. **The great excision** (WS-R + WS-P + WS-T in one commit, epoch).
2. **Unified-loop hardening** (WS-U): context pressure, stop-at-done, review
   nudge, steering.
3. **Model ladder** (WS-M): per-task advisor, switch gates, liveSettingsMu.
4. **TUI/state simplification + legacy tolerance** (WS-V).
5. **Verification** (§3) + docs + memory updates (§5).

### Out of scope

- The gateway repo (provider cache behavior; the MiniMax near-zero-read issue
  is gateway-side — evidence in TOKEN_EFFICIENCY_OVERHAUL_PLAN.md TE08).
- `-race` on this host (no CGO — CI gate).
- Re-adding ANY delegation/fan-out machinery in any form.

---

## §1 Audit

### §1.1 Design constraints — verified firsthand (file:line, this session)

**The switch mechanism already exists and is cache-correct.**
`applyModelSwitch` (`engine.go:462-505`): refreshes model-dependent prompt
fields (name + family addendum), records `InvalidationModelSwitch` BEFORE the
next request can transmit the new prefix, rolls back on record failure, re-arms
prefix-shape persistence (`:501-503`), and no-ops on same-model (`:469,477`).
`SwitchModel` (`engine.go:441-449`) refuses mid-task. The per-task ladder
reuses this verbatim with role="main"; the "subagent" branch (`:467-474`) dies.

**The freeze is policy, not mechanism.** `shouldRunAdvisor`
(`advisor.go:61-73`) hard-stops after the first request (`Requests > 0`) — one
line to change. The doctrine comment block (`advisor.go:16-29`, and the
turnloop mirror at `turnloop.go:62-72` "models are decided HERE… and never
again") must be rewritten, not just bypassed: the REASON it existed (a switch
cold-starts the main prefix and breaks the execution chain) half-dies with the
execution chain; the surviving half (cold start) becomes a weighed cost, not a
prohibition.

**The advisor is already the right shape.** One flash-class call
(`utilityModelID`, `advisor.go:47-55` — NOTE: falls back to SubagentModelID,
which dies; new fallback = flash heuristic → ActiveModelID), 8 s timeout,
200-token answer (`advisor.go:195-206`), catalog rendering with per-model
window/family (`advisorCatalog`, `advisor.go:78-94` — the "continuation" column
is subagent-era and dies), name→catalog resolution (`resolveCatalogModel`,
`:263-274`), fence-tolerant JSON parse (`parseAdvisorDecision`, `:247-259`).
`advisorDecision` (`:36-41`) loses `Sub`. `maybeAdviseFreshSession`
(`advisor.go:293-305`) is the free mid-session escape hatch — KEEP as-is (it
advises `/new`, still correct advice when a session's cache carries unrelated
context).

**Concurrency hazard, amplified.** `applyModelSwitch` mutates
`Settings.Provider.ActiveModelID` under `e.mu` (`engine.go:481`), but the TUI
reads Settings fields directly on its own goroutine; the F-1 audit fix created
`liveSettingsMu` for exactly this class (Effort, PermissionMode —
`engine.go:150-155`). Once switching happens at EVERY task boundary instead of
once per session, ActiveModelID joins the live-mutated set → it must go behind
`liveSettingsMu` (WS-M4).

**Window-fit is a hard gate, not advice.** Catalog models carry real windows
(MiniMax M3 1,000,000 vs DeepSeek 128,000 — `advisor_test.go:19-21`,
`ContextLimit` resolution `command/actions.go:197-199`). A down-switch with a
history that no longer fits would hit `assembleRequestWithStart`'s silent
oldest-unit window-drop (history.go:506-534) — silent context destruction.
WS-M2 therefore gates switches on fit-with-headroom, with boundary compaction
as the mitigation.

**Per-family replay determinism is the cache-transfer mechanism.** History
stores canonical messages; `replayMessages` (`provider.go:374-396`) computes
each family's deterministic projection (MiniMax preserves reasoning_details;
DeepSeek strips + re-adds empty reasoning_content on tool turns). So: bytes are
stable per model across turns; a switch A→B cold-starts B once; returning to A
replays A's exact prior bytes and **re-warms A's still-live cache** (DeepSeek
caches are per-model — owner-confirmed — which this design treats as the
general rule for all providers). The one rule that keeps all of this true:
never rewrite settled history (Constitution V), and record every switch as an
invalidation so measurement stays honest.

**What the unified session inherits for free** (landed, uncommitted,
token-efficiency Phases 0–2): truncation integrity TB01–TB04 (the main loop
never dispatches a cap-cut call, stores `{"truncated":true}`, class-keyed storm
breaker, chunked-write recovery text) — MORE important now that main writes
files; explicit `MaxTokens` per request (TB02, 32k non-MiniMax); the active
checklist reader + target-directory tasks.md (TA01/TA02); the tool-JSON diet
(TG01 partial); the measurement harness (TM01/TM02). Landed items that DIE with
subagents: TA03/TC04 handoff checklist line, TD01 runReadLedger, TD02/TC01/TC02
executor clauses, TE05 executor supersede wiring (main-path equivalent at
`dispatch.go:159-161` becomes live again naturally), and the dead half-wired
`sanitizeRoleGatedCalls` in truncation.go (delete; the role gate it served is
gone — its failure class cannot exist).

### §1.2 Removal inventory — orchestrator/engine/instructions/state (VERIFIED)

Scale: **~1,650 orchestrator production lines delete outright** — subagent.go
(714) + contextlink.go (407) + contextrecord.go (364) + rolesplit.go (131,
~35 lines MOVE first) + runreads.go (66) — before instruction/test cuts. 117
files under internal/ match "subagent" (62 tests, 22 tui, 3 goldens, ~30 live
production).

**MOVE FIRST (symbols in doomed files with main-loop callers — deleting the
file before moving these breaks the build):**
- `isMutation` (subagent.go:691-697) → callers `dispatch.go:162`,
  `gates.go:64,141`, `turnhelpers.go:229` (trackKnowledge).
- `hardTurnCeiling()` (rolesplit.go:96-103) → main liveness, `turnloop.go:249`.
- `noteChangedFiles` (rolesplit.go:105-122) → shared gate `gates.go:159`; the
  ONE write tally now that the main loop mutates directly.
- `mergeChangedFiles` (rolesplit.go:124-131) → `turnloop.go:158,262,526`.

**Whole-file DELETE:** subagent.go (specs, executeSubagent, handoff machinery,
runSubagentTool incl. run_start/run_summary writers :214-221,236-239,
subagentSystemMessage, takeSteering's twin, emitAgent — also called from
`engine.go:561` RouteShellOutput, cut that branch; capabilityStatement;
turnBudgetPartialReport). contextlink.go (planDispatch is the SOLE builder of
subagent wire messages; decideLink; digestCarryForward; `resetLinkTaskState`
called from `engine.go:397` and `finalizeLinkStats` from `turnloop.go:164` —
cut both call sites). contextrecord.go (agentRunCapture, terminal shapes,
finalizeAgentRecord → Persistence.WriteAgentRecord, RestoreAgentRecords ←
`runtime_build.go:585-587`). runreads.go (TD01 executor dedupe — main uses the
inspection ledger). rolesplit.go after the moves (workspaceMutation,
executionRoleGate ← sole caller `gates.go:75-77`; `taskRoleBlocks` field
`engine.go:225-228` + reset `:395` die with it; `toolOutcome.GateRejected`
KEEPS — H1/H2/repeat-limiter/truncatedOutcomes still set it).

**Also dead-on-arrival:** `checklistHandoffLine` (checklist.go:100-134, only
caller subagent.go:135), `parseReportStatus`/ReportStatus
(checklist.go:235-270, only caller subagent.go:237), the half-wired
`sanitizeRoleGatedCalls`+`roleGateBlockedMarker` (truncation.go:86-129, zero
callers — confirmed), `Knowledge.AddReport`/`Findings`/`ResearchFindings`
(knowledge.go:53-55,221-264, zero callers today).

**dispatch.go/gates.go MODIFY:** executeBatch's all-run_subagent announce path
(:61-89) collapses to a plain loop; dispatchScope loses `runReads`+`readOnly`
(main sets neither); gates.go loses the read-only-shell gate (:42-57), the
continuation-review mask (:58-70), the role-gate call (:71-77), the runReads
guard+record (:103-109,139-146). KEEP byte-for-byte in behavior: H1 (:27-32),
H2 (:33-41), repeat limiter (:78-88), inspection dedupe (:89-101), cancel
pairing, checklist adoption, noteChangedFiles. truncation.go KEEPS (minus the
dead RG-1 pair).

**Engine/turnloop:** DELETE fields `taskAgentUsage`, `taskAgentRuns/Reused`,
`runCounter`, `agentRecords/agentRecordOrder/taskLinks/taskSeq`
(engine.go:197,208-209,235,252-258), `shellScopeMu/shellScopeRun` (:134-140) +
enter/exit (:569-582); `RouteShellOutput` (:551-567) keeps only its main
branch (command wiring — §1.3). `applyModelSwitch` loses the "subagent" role
branch (:465-475). Onboarding (turnloop.go:74-92) KEEPS as aux — re-point its
model + keep the `:sub:onboarding` pin string (pin strings are routing labels;
renaming churns nothing of value). `recordIsolatedUsage` (usage.go:125-141)
and `addTaskAgentUsage` die; `recordAuxUsage` + `:sub:advisor` pin survive.
**Knowledge KEEPS** (compaction facts, RelatedToSession, NoteFile/
MarkWorkspaceChanged); `AddPhaseReport`/`Reusable`/`BriefingForScope` die.
AutoReview: `Decide` gating KEEPS; the injected nudge text (turnloop.go:541)
rewrites to the inline self-review (WS-U3); `updateTaskReviewOutcome`
(reviewgate.go:388-400, sole caller in subagent.go) dies. classify.go's
`agentRE` + "subagents explicitly requested" upgrade branch (:53,138-141)
die. effort.go: `AgentTurnScale` KEEPS (dual use: main hardTurnCeiling),
`AgentReasoning` dies. toolhandlers.go: run_subagent dispatch case (:32-33) +
definitions.go registration (:18) + runSubagentDefinition (:51-74) die;
`SessionToolNames` count drops 8→7 (wiring-inventory data updates with it).
proposeChanges verdict strings naming dispatch (toolhandlers.go:132,151-156)
reword.

**Instructions (the epoch's content):** DELETE explore/review/general
descriptions+systems, ReportFormat*, HandoffDeliverable*, capability texts,
HandoffContractHeader/NoOverlapNote, SubagentReviewReadOnlyNotice,
SubagentInterjectionPrefix, GateSubagentShellBlocked,
RuleContinuationReviewOnly + mask body, RuleExecutionBelongsToAgent + both
role-gate bodies, run_subagent tool texts + example, SubagentSkills*;
`SkillsHintBody` loses its run_subagent sentence; PLANNING step 2 loses "Use
an explore agent…". **MOVE to the main prompt:** EditDisciplineBody (absorbed
into PromptContextEditDisciplineBody), ChunkedWriteRuleBody (IS-4: the rule
must be stated to the reader who now hits the cap — the main model),
ExecutorScopeBody adapted (WS-P2); SubagentGeneralSystem's
verification/STATUS + preserve-user-work clauses fold into the new contract
rules 4-5 (WS-P1) — flag, never silently drop (epoch_clauses_test pins three
of them). **KEEP:** ReadOnlyShellAllowlistBody + Examples (they pin
IsReadOnlyShell, which survives in H7/read-probe classification); the two
subagent-context registrations around them die. AllowlistCtx values dying:
subagent.general/explore/review/read-only/interjection; `gate.truncated-write`
and `rule.chunked-write.sentence` re-home to main-loop. audit_test
realToolAllowlists (:146-164) rewrites; forbidden-capability test (:198-213)
dies; `Audience: Subagent` prunes or stays as an empty enum (dump_test
audienceOrder decides).

**Contract/gateway/state:** `ModelProfile.ContinuationLinking` + consts die
(last users are advisor's catalog column + substituteFor).
`Settings.Provider.SubagentModelID`: **KEEP the field, retire the semantics**
(legacy settings decode + `state_test.go:55` fixture; deprecation comment;
nothing consults it) — this supersedes §1.3's DELETE verdict for the field.
`Settings.ContextLinking` handling dies (tolerant decode keeps old files
parsing). WriteAgentRecord/ReadAgentRecords/pruneAgentRecords
(session.go:253-335) die; legacy agents/ sidecars are inert once unread.
Resumed history.json containing `run_subagent` assistant tool_calls: replay
does NOT require the tool in the registry (tool_calls replay as opaque
assistant content; verify with a resume test over a legacy history fixture).

**Test verdicts (orchestrator/instructions/state/gateway):** DELETE
subagent_ceiling/dispatch/env/wrapup/dedupe tests, skills_subagent,
handoff_test, handoff_fidelity, contextlink_test + integration,
delegation_batch_announce, shell_scope, shellgate, rolesplit, progress_ladder,
tailcoherence, state/contextlink_state. REWRITE dispatch_gate (drive H1/H2
through the main scope), cachehit_guard (drop the subagent shape-guard test),
h5_circuit_breaker (re-target GateRejected source), truncated_call (KEEP —
re-stage the write scenario as main-loop), fieldtest_snake (unified flow),
fieldtest_e2e, delegation_regression + delegation_prompt (prompt content),
gauntlet_offline, faultinjection (FI-5 row), instructions_wiring,
mixed_provider (sub stream dies, aux remains), usage_record, advisor +
catalog_preflight (single role), meta/prompt_budget/prompt_stability/
skills_prefix_stability/skills_prompt, engine_test (settings line),
instructions/audit + dump + epoch_clauses, gateway/model_capability,
state/config_defaults, command/application + prune_models. KEEP unchanged:
shellbypass, inspection, reviewgate, contract cache/pairing/allstream/
tooltarget (legacy-pin data), state_test, checklist_test +
checklist_path_test, workspace suite.

### §1.3 Removal inventory — TUI/command/contract + legacy tolerance (VERIFIED)

Scale: internal/tui has 36 files referencing agent machinery (19 prod + 17
test), ~416 matching lines. 17 agent-only TUI test funcs die; 2 mixed rewrite;
8 more test files need assertion edits.

**Two break-risks with top billing (violating either is an execution failure):**
1. **ShellOutput must be REWIRED, never deleted.** The closure at
   `command/runtime_build.go:352-375` serves MAIN-loop shell streaming; with
   agent cards gone its body becomes the existing fallback
   `a.callbacks.ToolOutput("run_shell", chunk)` and `engineRef` + the binding
   (`:579-581`) die, along with engine-side `RouteShellOutput`/`enterShellScope`
   /`exitShellScope` (`orchestrator/engine.go:551-582`).
2. **SubagentModelID is quietly the utility-model slot.** Onboarding questions
   run on it (`turnloop.go:76-83`), `utilityModelID` falls back to it
   (`advisor.go:54`), review spend attribution scans `:sub:review`. Re-point
   every consumer (WS-M6) BEFORE deleting the field, or they resolve to "".

**TUI DELETE (whole units):** `agentView` + `provisional` (model.go:111-123);
`Model.agents/agentByID/viewAgent` (:214-216) + `hoveredAgent` (:259-263);
`agentMsg` + bridge `Callbacks().Agent` (bridge.go:26,109 — the SOLE consumer
of `contract.Callbacks.Agent`); update.go agent branches (:193-194, :132,
:341-354 viewAgent repaint/follow, :381-425 cycleAgent/Back); `applyAgent`
whole (ingest.go:161-208, incl. the D2 tool_output case);
`overrideDelegateState` + call sites (ingest.go:112-131,97,107 — live-path
only, cannot affect replayed rows); `renderAgentChip` (render_tool.go:278-314);
the viewAgent overlay swap (render_transcript.go:24-29) + `case "agent"` chip
emit (:85-89); header line-1 running-agent count (render_header.go:41-61) AND
line-2 focused-agent view (:63-83) — **header becomes a fixed 3 rows**; the
"←/→ agents" hint (:189-191) + `arrowLeft/arrowRight` glyphs
(render.go:118-127, no other user); Esc viewAgent branch (keys.go:52-55);
left/right cycling (keys.go:125-138 — delete the whole branch, do NOT leave a
dangling case); alt+1..9 jump (keys.go:146-154); `targetAgentChip` machinery
(mouse.go:26,116-122,192-193 + agent halves of setHover/clearHover
:203-238, wheel case :293-299); `linkSummaryPart` + call
(format.go:106-150); paging `ensureAgentView`/`rehydrateAgent`
(paging.go:250-305); trimTranscript's agent-chip keep-loop (:200-208).

**TUI MODIFY:** `chipSpan.agentID`/`item.agentID` fields drop (structs stay for
tool chips); view.go chip projection always-tool (:63-78); `formatContextReport`
loses the subagentModel param + "Sub-agent:" line (format.go:347-385) and
`/context` model names collapse to one (slash.go:66-68,200-217); modals.go:83
session-reset drops agent fields; `NewModel` init drops agentByID.

**Contract:** DELETE `AgentEvent` (types.go:352-370), `Callbacks.Agent`
(:513), `TaskStats.AgentUsage/AgentRuns/AgentRunsReused` (:396,403-404 — TUI
renders NONE of them; consumers are benchjson + orchestrator tests),
`TaskStats.Links` + `LinkOutcome` (:457-490). **KEEP** `UsageStreamSubagent` +
`SessionUsageAggregate.SubagentRequests` (cache.go:21,279,323-324) — legacy
usage.jsonl sidecars carry `"stream":"subagent"` records forever and the
constant is the cheapest tolerance; nothing new emits it. tooltarget's
`"title","agent"` fallback keys (tooltarget.go:28) may go (paged rows use the
persisted target column); `toolLabel["run_subagent"]="Delegate"`
(render_tool.go:332) KEEP for legacy paged rows.

**Command:** runtime_build.go — drop `state.SubagentModel` (:434), the
PromptContext.SubagentModel field (:536), `Persistence.WriteAgentRecord`
(:528-532), `ReadAgentRecords`+`RestoreAgentRecords` (:582-587; stale agents/
dirs orphan harmlessly, removed with the session dir). actions.go
`addDiscoveredModels`/prune keep-guards go active-only (:189-241); root.go:281
discover prints main only; benchjson.go loses `benchLink`/Links/AgentUsage
fallback (:48-61,75,107-117) and the `:sub:review` spend scan (:142-150 — dies
with the review dispatch, WS-U3).

**Settings (reconciled with §1.2 and the KEPT per-task advisor — investigator
assumed the advisor dies; the design keeps it):** `Provider.SubagentModelID`
**stays as a tolerated-legacy field** (deprecation comment, never consulted —
§1.2's verdict supersedes the DELETE here: a state fixture pins its decode and
keeping it is the zero-risk tolerance); `ContextLinking` handling dies
(tolerant decode); **KEEP `Advisor` and `RolesPinned`** (:63-68) —
RolesPinned becomes the single-model pin (WS-M3).
Old settings.json parse unchanged (unknown keys ignored); `config set
subagentModel` (state/config.go:269-277) now errors "unknown key" — changelog
note. `SubagentModel()` (:145-152) dies; `AutoAssignModels` keeps its main
half (:160-168), sub half (:169-182) dies; `AssignFreshDefaultModels`
(:185-212) collapses to main-only. `config_defaults_test.go` rewrites
main-only.

**Legacy tolerance (exact, all six):** (1) SQLite `role='agent'`
run_start/run_summary rows surface via `loadEvents` (paging.go:214-235) and
`pageItems` (:41-57) — deleting the case in each no-default switch yields
silent skip BY CONSTRUCTION; add the two replacement tests (loadEvents +
pageItems with agent rows render nothing, no panic). (2) agents/<runID>.json —
sole reader dies with it. (3) `Kind=="run_subagent"` tool rows page in as
generic tool rows (keep the Delegate label). (4) usage.jsonl
`"stream":"subagent"` keeps aggregating (constant kept). (5) old settings keys
ignored by Go's unmarshal. (6) resumed history.json containing `run_subagent`
assistant tool_calls must replay without the tool existing in the registry
(orchestrator-side verification — §1.2).

**Tests:** DELETE agent_rehydrate_test.go (replace with the 2 legacy tests),
keys_test agent funcs (:149,161,189,241; rewrite :174,:200 without the
helper), ingest_test TestApplyAgentEventKinds, steering_test's
FailedDelegateRow + SubagentShellOutput (KEEP SteeringNotLostInBusyEngineGap),
application_state TestSubagentRunningState, mouse_test ClickAgentChip,
visibility_test RunningAgentCountRidesLineOne, linksummary_test whole.
REWRITE sweep_test (tool half only), wiring_inventory_test (drop
liveAgentEventKinds/Fields + alt+1..9 row AND the matching rows in
`specs/010-ultimate-consolidation/wiring-inventory-data.md` — a TEST reads
that spec file). MODIFY context_panel/context_modal_fit/actions/tui_test
helper/usage_display/cross_surface + command's application_test:232,
prune_models_test.

**Docs:** 118 files mention subagents; specs/ are frozen history (exempt,
EXCEPT wiring-inventory-data.md above). Update: docs/agent-design.md (22
hits), docs/prompt-caching.md (14), docs/architecture.md (8),
docs/migration.md (5), README.md (3), docs/security.md (1).

---

## §2 Workstreams

### WS-R — The excision (Phase 1, epoch commit, with WS-P and WS-T)

Driven by the §1.2/§1.3 inventory tables. Structural rules for Opus:

**R0 — order of operations inside the commit (the four break chains):**
(1) MOVE first: `isMutation`, `hardTurnCeiling`, `noteChangedFiles`,
`mergeChangedFiles` out of doomed files (§1.2 list). (2) M6's utility
re-pointing lands BEFORE any SubagentModelID semantics are cut. (3) The
`AgentEvent`/`Callbacks.Agent`/`TaskStats` field deletions land TOGETHER with
the TUI cut (§1.3) — contract types are the coupling point. (4) The prompt
rewrite, tool deletion, audit-allowlist rewrite, and golden regen are all in
THIS commit (the audit ties texts to the live registry — a split commit
cannot be green).

**R1.** Delete whole-file: subagent.go, contextlink.go, contextrecord.go,
rolesplit.go (after the moves), runreads.go. Delete the `run_subagent`
synthetic tool end to end (definitions.go:18,51-74, toolhandlers.go:32-33,
instructions texts, audit registrations, wiring-inventory data rows —
SessionToolNames count 8→7).
**R2.** dispatch.go/gates.go: collapse `dispatchScope` to the main scope's
shape (the sub-scope fields readOnly/runReads and the all-subagents announce
path in executeBatch die); keep the shared gate order (validation → failed-
cache → repeat limiter → dedupe → dispatch → storm breaker) byte-for-byte in
behavior for the main loop — every H-guard test must stay green.
**R3.** Persistence: stop WRITING run_start/run_summary events and
agents/<runID>.json sidecars; READING must tolerate all of them silently
(legacy sessions — exact tolerance points from §1.3).
**R4.** Settings: `Provider.SubagentModelID` stays in the schema
(parse-tolerated, never consulted); `RolesPinned` becomes the model-pin flag
(WS-M3). No settings-file migration required.
**R5.** Stats/usage: `TaskStats.AgentRuns` and per-`:sub:` pin attribution stay
in the TYPES (historical records must render) but the engine stops producing
them; aux pins (`:sub:advisor`, `:sub:onboarding`) survive — rename to `:aux:`
ONLY if the gateway pin routing is confirmed inert for old names (§1.2 verdict;
otherwise keep the strings — pin strings are cache identity, not cosmetics).

### WS-P — System prompt overhaul (inside the Phase-1 epoch)

The rewrite is a COHERENCE pass, not a style pass: every rule the unified flow
needs, stated once, no rule referencing machinery that no longer exists.

**P1 — Operating contract** (`prompt.go:42-55` today). New draft (Opus lands
exact bytes; keep the numbered-rule shape and priority preamble):

```
OPERATING CONTRACT
When rules conflict, order priority: safety, the user's explicit request, this contract, then style.
1. Read the final [task-brief] and size the work to it. Questions, analysis, and conversation you answer directly.
2. You do the work yourself: investigate, change files, run checks — all in this session. Search first, then read only the ranges you need, batching independent reads into one turn.
3. For work of three or more steps keep a tasks.md checklist current (see PLANNING for item shape and location) and state "DONE =" criteria before the first change. Skip it for small tasks; never claim completion while an item is open.
4. Verify what you change: run the check that proves each change works before ticking its item. Never claim a result you did not verify.
5. Wrap up flat: when every item is checked and the checks passed, tick tasks.md, answer, stop — no extra validation pass, no re-reading what you already verified.
6. Final answer: outcome, verification performed, genuine remaining risk.
```

**P2 — CONTEXT AND EDIT DISCIPLINE reunifies**: the main prompt regains the
editing half (EditDisciplineBody + ChunkedWriteRuleBody move from
subagents.go into prompt.go registrations — Audience: MainStatic, Cache:
Prefix; the write_file permission sentence is already shared/verbatim). The
executor scope clause (ExecutorScopeBody) is REWRITTEN for the main session:
keep "confine glob/list_files to the narrowest directory" and "continue from
the real state — never redo a checked item or rewrite an existing file that
already matches"; drop assignment-framing ("your assignment names").
**P3 — DELETE sections**: DELEGATION (on+off), trust-the-report (old rule 5),
the run_subagent description + task/role/skills property texts, handoff
labels/deliverables, capability statements, read-only-shell subagent notices
(the MAIN read-only-shell allowlist for plan-mode/gates survives if the main
loop still uses it — §1.2 verdict), GateRoleExecution*.
**P4 — MODEL section (new, small)**: one paragraph telling the model the
session's model can change between tasks when complexity demands it, chosen by
the system — it should neither ask for switches nor mention them; continuity
comes from the shared history. (Prevents the model narrating/requesting
switches — the harness owns the ladder.)
**P5 — Audit hygiene**: every deleted text's audit registration goes with it;
`AllowlistCtx: "subagent.*"` contexts die; `gate.truncated-write` re-homes to
the main-loop context; `RuleExecutionBelongsToAgent` dies; `RuleChunkedWrite`'s
stating text moves with EditDiscipline. `epoch_clauses_test.go` pins the new
contract rules. Budget: re-baseline `promptBaselineChars` with the documented
comment (expect ≈ +650 edit-discipline / −250 delegation+trust in the SYSTEM
prompt, and ≈ −4.5 KB in tool JSON: run_subagent desc ≈2.9 KB + task/role/
skills properties + subagent gate texts). Wire-golden section sizes recorded
in §4 as the epoch's measured outcome.

### WS-U — Unified-loop hardening (Phase 2, cache-safe)

**U1 — Context pressure is now existential.** All tool traffic lands in ONE
history. (a) Make the absolute trim floor land (TE04 as specified in the
token plan: boundary maintenance at `min(60% window, 120k tokens)`); (b)
verify task-boundary fold (`history.go` fold of completed-task outputs) fires
in the unified flow and KeepFullToolOutputs (4–8) is measured against the
snake scenario — tighten only with byte evidence; (c) supersede now works
naturally (main mutations → `dispatch.go:159-161`) — add the regression test
that an edited file's earlier read gets trimmed first.
**U2 — Stop-at-done** (TF02 from the token plan, unchanged design): checklist
all-checked + checks passed → the no-calls branch finalizes without the retry/
review nudges. Now cheaper to satisfy: the "most recent executor report"
condition becomes "the task's own checks ran" (checksRun > 0).
**U3 — Review becomes an inline nudge.** The AutoReview path keeps `Decide`
gating (tiers/risk paths) but the nudge text becomes "review the diff you made
across the changed files now — verify correctness, then finish"; the review-
subagent dispatch and `updateTaskReviewOutcome` report-parsing die. Review
stats keep recording the DECISION (tier, rationale) for continuity.
**U4 — Steering unifies.** `takeSteering` dies; `drainSteering` (turnloop) is
the only consumer — mid-task user messages land in main history with no digest
hop. The D1 busy-gap protection (TUI restore-to-composer) is untouched.
**U5 — Shell streaming simplifies.** `RouteShellOutput`/shellScope die; the
ShellOutput closure reverts to `callbacks.ToolOutput("run_shell", …)` directly
(runtime_build.go). The D2 mis-routing bug cannot exist without a second scope.

### WS-M — The model ladder (Phase 3, cache-safe)

**M1 — Per-task advisor.** `shouldRunAdvisor` drops the `Requests > 0` freeze;
it now runs in Run's prologue at EVERY task boundary (before the task's first
request — the only safe moment), same utility model, same 8 s/200-token bounds.
New decision shape `{keep, model, why}`. `AdvisorSystemBody`
(`subagents.go:193-207` today — it teaches the dead main/sub pairing and the
session-fixed constraint) is REPLACED; draft (Opus lands exact bytes, keeping
the JSON-only answer contract and the fenced-answer tolerance of
`parseAdvisorDecision`):

```
You are the task advisor for MuhiyaCode, a coding agent. A new task is starting
in an ongoing session. Decide whether the CURRENT model should handle it, or
name a better one from AVAILABLE MODELS.

Respond with ONLY one JSON object:
  {"keep": true}
or
  {"model": "<id>", "why": "<one short line>"}

Rules:
- {"keep": true} is the right answer unless this task clearly outgrows the
  current model — a capability jump (deep multi-file reasoning, tricky
  debugging) or a context window it lacks.
- Switching has a real cost: provider caches are per-model, so a switch pays a
  one-time cold start on the new model. Never switch for a task the current
  model can do adequately. Never propose a premium model for chat, questions,
  or a small fix; prefer stepping back down when heavy work is done.
- The session's history moves with the task either way; a recently used model
  may still hold its cache.
- Choose ONLY from AVAILABLE MODELS and copy the id exactly.
```

The user prompt keeps CONFIGURED→CURRENT, AVAILABLE MODELS (window/family —
the continuation column dies), WORKSPACE, TASK (`advisor.go:192-193`).
**M2 — Hard switch gates (code, not trust).** In order: (a) user pin
(`RolesPinned`→ model pin) → never switch; (b) same model → no-op; (c)
window-fit, measured the way `/context` measures in-use (the E-2 pattern,
`contextreport.go:56-67`): `inUse := max(history.EstimatedTokens(),
e.latestPromptTokens)`; require `inUse × 1.3 + outputBudget(new) ≤ new model's
usable window (ContextLimit − outputReserveTokens)`, else EITHER run boundary
maintenance/compaction first and re-check, or refuse the switch with a
recorded reason — NEVER switch into `assembleRequestWithStart`'s silent
oldest-unit window-drop; (d) catalog existence (`resolveCatalogModel`); (e)
apply via `applyModelSwitch(role="main")` — invalidation event + addendum swap
+ shape re-arm all come free.
**M3 — Pin semantics.** `/model <id>` (user action) sets the pin; the advisor
never overrides a pin; `/model auto` clears it. Settings field rename in
MEANING only (`RolesPinned` comment), no schema change.
**M4 — Concurrency.** ActiveModelID reads/writes join `liveSettingsMu`
(engine accessor + the TUI read sites from §1.3). Add the uithread-guard
allowlist entry if a new engine getter appears.
**M5 — Observability.** Each switch emits the existing model-switch
invalidation + ONE notice: "switched to <name> for this task — <why>". The
per-model cache stats already attribute by (model, pin) (`PerPairingRates`) —
`/context` gains a line per recently-used model showing its steady-state cache
read share, so a dead provider cache is VISIBLE (folds in TE02's intent:
main-stream read-share < 5% on ≥ 8k prompts → once-per-task notice).

**M6 — Utility-model re-pointing (BLOCKS the excision — do before WS-R
deletes semantics).** `SubagentModelID` is quietly the utility slot; every
consumer re-points to `utilityModelID()` (whose own fallback becomes: flash
heuristic → cheapest catalog window → ActiveModelID): onboarding questions
(`turnloop.go:81,83`), the advisor call itself (`advisor.go:54` fallback),
`reconcileCatalog`'s subagent row (`advisor.go:116-121` — row dies),
`substituteFor`'s executor branch (`advisor.go:152-176` — branch dies),
PromptContext.SubagentModel (`runtime_build.go:434,536` — field dies),
discover output (`root.go:281`), prune keep-guard (`actions.go:226-231`),
`/context` name pair (`tui/slash.go:216`). Aux pins `:sub:onboarding` /
`:sub:advisor` keep their strings (routing labels; legacy usage rows decode
against them).

### WS-V — TUI/state simplification + legacy tolerance (Phase 4)

Driven by §1.3. Structural rules: agent cards, agent overlay, arrow-key agent
cycling, chip rendering/rehydration, `Callbacks.Agent`/`AgentEvent`, and the
header's focused-agent line die; the header becomes permanently 3 rows; resume
paths SKIP `role=="agent"` events and agents/ sidecars silently; `/context`
loses agent rows and gains M5's per-model cache lines; the tool-activity
surface (main-loop tool rows) is untouched.

### WS-T — Test migration (inside Phase 1, then per-phase)

From §1.2/§1.3 verdicts. Rules: DELETE tests of deleted machinery; REWRITE the
fieldtests to the unified flow — the snake scenario becomes: plan-write
(tasks.md in target dir) → read → write game → run check → tick → final answer,
asserting FEWER requests than the split baseline and byte totals recorded in
§4; the truncation/checklist/steering/H-guard regression tests all stay green
against the main loop. `TestRequestBytes…`/`logAccount` harness survives
unchanged (drop the `:sub:` split column or keep it reading zero — keep, for
aux calls).

---

## §3 Verification gauntlet

1. Per-phase: gofmt/vet/build/`go test ./... -count=1` green.
2. Grep-clean: after Phase 1, `grep -ri "subagent\|run_subagent" internal/`
   returns ONLY: legacy-tolerance comments, the parse-tolerated settings field,
   historical-type comments (§R5), and test fixtures for legacy resume. Any
   other hit is an execution failure.
3. Offline before/after (fieldtest bytes): split-era baseline vs unified snake
   scenario — expect fewer requests (no dispatch round-trip) and a smaller
   fixed prefix (no run_subagent tool JSON). Record in §4.
4. Cache-epoch: exactly ONE golden regen (Phase 1); diff reviewed line-by-line
   — every changed line traces to WS-P/WS-R.
5. Model-ladder offline tests: switch gates (pin/fit/no-op/catalog), switch
   invalidation event recorded, A→B→A byte-stability (scripted provider:
   request N+2's settled prefix for model A byte-identical to request N's —
   the re-warm property), window-fit refusal path, liveSettingsMu race test
   pattern (mirrors settings_race_test.go).
6. UTF-8 corruption sweep over every changed file (Edit/Write tools only for
   non-ASCII — the PowerShell Set-Content hazard is documented and was hit
   once this session).
7. Live (owner-gated): one real "build a game in a subdirectory" session —
   count requests, watch per-model cache read share across a forced switch and
   a return-switch; the 12.5k-refused-write failure class must be structurally
   impossible (no gate exists to refuse it).

---

## §4 Execution log

### Phases 1–3 — the excision, the prompt, the model ladder (2026-07-21) ✅

**Gauntlet: 15/15 packages green. gofmt clean. vet clean. Binary builds and
runs (v1.1.0). No UTF-8 corruption.**

**Removed (production):** subagent.go (714), contextlink.go (407),
contextrecord.go (364), rolesplit.go (131), runreads.go (66),
instructions/subagents.go — plus the run_subagent tool end to end (definition,
dispatch case, all tool texts, its example), the execution role gate, the
read-only-shell subagent gate, the continuation-review mask, the batch-announce
path, the dispatchScope split flags (dedupe/trackStats/readOnly/runReads), the
shell-scope routing, the skills equipping path, and Persistence.WriteAgentRecord
+ RestoreAgentRecords. ~1,700 production lines.

**Moved first (main-loop callers, per R0):** `isMutation`, `hardTurnCeiling`,
`noteChangedFiles`, `mergeChangedFiles` → new `taskledger.go`. The read-only
shell allowlist + chunked-write rule + advisor system text → new
`instructions/readonlyshell.go` (the allowlist survives for plan mode).

**Prompt rewritten (one epoch, goldens regenerated once):** the operating
contract now says the session does the work itself and stops when done; the
edit discipline REUNIFIED into the main prefix (edit_file contract, write_file
permission rule, chunked-write rule, scope discipline, continue-from-real-state);
the D-1 safety clauses (preserve uncommitted work, no destructive commands,
confirm the target) folded into TOOLS AND RECOVERY — flagged in the plan as
must-not-drop and verified by `epoch_clauses_test.go`; DELEGATION and
trust-the-report deleted; a short MODEL section added so the model never
narrates or requests a switch.

**MEASURED — the fixed prefix on every request:**

| | system prompt | tool JSON | total |
|---|---|---|---|
| split era | 5,372 | 19,021 | 24,393 |
| unified | 6,101 | 15,807 | **21,908 (−10.2%)** |

The prompt grew 729 (it regained the editing rules the model actually uses);
the tool JSON fell 3,214 (run_subagent's schema + its task/role/skills property
texts). `promptBaselineChars` re-baselined 5393 → 6120 with that reasoning
recorded in the constant's comment.

**MEASURED — the snake scenario end to end:**

| | requests | total bytes | main | executor |
|---|---|---|---|---|
| split era (Phases 1–2) | 7 | 105,243 | 76,217 | 29,026 |
| unified | **4** | **68,465** | 68,465 | 0 |
| delta | **−43%** | **−35%** | | |

The handoff and report round-trips are simply gone: no dispatch, no report to
parse, no re-verification surface. This is the same work in one warm
conversation.

**Model ladder (WS-M):** `runTaskAdvisor` replaces `runSessionAdvisor` and runs
at EVERY task boundary (the `Requests > 0` freeze is deleted); decision shape is
`{keep, model, why}`; the chosen model is fixed for that task. Hard gates, in
order: advisor off → user pin (`RolesPinned`) → same-model no-op → catalog
existence → **window fit** (`historyFitsModel`: in-use context measured the
`/context` way — provider-reported prompt size when available, else the local
estimate — ×1.3 plus the output budget must fit the candidate's window minus
reserve) → `applyModelSwitch`, which already records the invalidation before the
new prefix can transmit. Tests pin all three behaviors including the refusal to
switch into a window the conversation cannot fit (MiniMax 1M → DeepSeek 128k
with a 400k conversation stays put).

**Utility slot re-pointed (M6):** `utilityModelID` no longer falls back to the
vestigial `SubagentModelID`; it prefers Flash, then the smallest-window catalog
entry, then the active model — so a single-model catalog can never leave an aux
call with an empty id. Onboarding now shares it. The `:sub:advisor` /
`:sub:onboarding` pin strings are deliberately unchanged: pins are cache
identity, and renaming them would orphan recorded usage rows.

**Legacy tolerance verified:** `role="agent"` events and run_start/run_summary
rows fall through the no-default switches and are skipped silently; agents/
sidecars are inert once unread; `UsageStreamSubagent` kept so old usage.jsonl
still aggregates; old settings keys ignored by Go's unmarshal;
`SubagentModelID` kept as a parse-tolerated field.

**Deviations from plan, with reasons:**
1. **The wire golden was rebuilt, not just regenerated.** Its test lived in the
   deleted wiring file, so `-update-prefix-golden` vanished with it. Recreated
   standalone (`prefix_wire_test.go`) — and corrected: my first version used an
   empty registry and would have pinned a fiction (7 KB instead of 15.8 KB of
   tools). It now builds a real workspace registry.
2. **TUI agent-card machinery is NOT yet removed** (WS-V). It is dead code —
   nothing emits `AgentEvent` — so the suite is green and behavior is correct,
   but ~220 lines of unreachable rendering remain. Listed below.
3. **`contract.AgentEvent`/`TaskStats.AgentRuns`/`LinkOutcome` types remain**
   for the same reason: deleting them requires the TUI cut in the same commit.

**Still open:** WS-V (TUI agent cards, header/keys/mouse/paging cleanup, the
contract type deletions), WS-U1/U2/U3 (absolute trim floor, stop-at-done, the
inline review nudge), docs (README + 5 docs files), and the memory-file updates
in §5.

---

## §5 Post-landing bookkeeping

- Update memory: `muhiyacode-model-driven-dispatch` (directive reversed
  2026-07-21 — subagents REMOVED; keep only "no auto-fan-out machinery"),
  `muhiyacode-native-agent-plan` (plan/execute split retired),
  `muhiyacode-token-efficiency-plan` (mark executor-scoped tasks superseded:
  TB06/TC05/TC03/TE01/TE06/TG02 die with the machinery; TE02→M5, TE04→U1,
  TF02→U2 absorbed here).
- Docs: README, docs/agent-design.md, docs/prompt-caching.md (model-ladder
  section replaces continuation-linking), specs/ index note pointing feature
  012/013 docs at this plan as their retirement record.
- TOKEN_EFFICIENCY_OVERHAUL_PLAN.md gains a header note: superseded in part by
  this plan; its §4 log remains the measurement record.
