package instructions

// planBlock (orchestrator/plan.go) and goalBlock (orchestrator/goal.go) are
// MainDynamic/Tail texts: they ride the per-turn user-message tail, so their
// literal sentinels ([plan-mode], [active-goal:, [goal:continue] etc.) are
// intentional per-turn markers, not a Prefix-class violation (IS-10 only
// forbids these sentinels in Prefix-class Body).

// RulePlanStepShape / RulePlanNoteShape / RuleReadOnlyShell are stated here
// (planBlock is the FIRST thing a plan-mode task sees) and enforced by the
// gate texts in gates.go (fix c: the plan-step shape and note shape; fix b:
// the read-only shell allowlist, now stated before any violation instead of
// only on rejection).
const PlanBlockBody = `[plan-mode]
PLAN MODE — read-only investigation. Every mutating tool call will be BLOCKED and wasted. Do not attempt edits, writes, patches, or state-changing shell commands.

Allowed read-only tools: read_file, list_files, grep, glob, search_text, git_status, git_diff, run_shell, update_plan, ask_user, and run_subagent with kind=explore|plan|review within the agents budget in the task brief. run_shell here only runs read-only commands: ` + ReadOnlyShellAllowlistBody + ` When that budget reads 0 or a run_subagent call reports the budget exhausted, do not call run_subagent again — investigate directly with the read-only tools and finish the plan yourself.

Deliverable: Produce a concrete ordered plan grounded in banked research. Do not re-read a scope already covered by banked findings. Include implementation steps only; put repository-wide checks in the note. Every step must name an exact file/function scope, cite a [F#] finding, include an observable Acceptance/Verify check, and mark dependency intent ([parallel] or [serial]). The plan note must contain "Verification:" commands/checks and "Risks:" entries. Keep update_plan current as the plan evolves. Example step title: "` + PlanStepExampleBody + `".

Example plan note:
` + PlanNoteExampleBody + `

Exit: When the plan is complete, end by calling the exit_plan_mode tool with a one-paragraph summary. Do not begin implementation.`

var planBlockText = Register(Text{
	ID: "prompt.plan-block", Audience: MainDynamic, Cache: Tail, Body: PlanBlockBody,
	StatesRule: RulePlanStepShape, Example: ExamplePlanStep.ID,
	MentionsTools: []string{"read_file", "list_files", "grep", "glob", "search_text", "git_status", "git_diff", "run_shell", "update_plan", "ask_user", "run_subagent", "exit_plan_mode"}, AllowlistCtx: "main-loop",
})

// planBlockReadOnlyShellStates / planBlockNoteStates are secondary
// registrations pointing at the SAME PlanBlockBody so the stated-in-advance
// audit can find the plan block under each rule ID it states, without
// duplicating the body (IS-5: one Body, multiple rule-linkage entries).
var (
	planBlockShellNoticeText  = Register(Text{ID: "prompt.plan-block.shell-notice", Audience: MainDynamic, Cache: Tail, Body: PlanBlockBody, StatesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "main-loop"})
	planBlockNoteNoticeText   = Register(Text{ID: "prompt.plan-block.note-notice", Audience: MainDynamic, Cache: Tail, Body: PlanBlockBody, StatesRule: RulePlanNoteShape, Example: ExamplePlanNote.ID})
	planBlockReadOnlyModeText = Register(Text{ID: "prompt.plan-block.read-only-mode-notice", Audience: MainDynamic, Cache: Tail, Body: PlanBlockBody, StatesRule: RulePlanModeReadOnly})
)

// GoalBlockInstructionBody is the static suffix of the per-turn goal block;
// orchestrator/goal.go prepends the dynamic "[active-goal: <text>]\n" line.
const GoalBlockInstructionBody = "Work autonomously toward this goal. End every reply with exactly one marker: " +
	"[goal:continue] if more work remains, [goal:complete] once the goal is fully met and verified, " +
	"or [goal:blocked: <reason>] if you cannot proceed."

var goalBlockInstructionText = Register(Text{ID: "prompt.goal-block", Audience: MainDynamic, Cache: Tail, Body: GoalBlockInstructionBody})

// Pipeline phase preludes (orchestrator/pipeline.go preparePipelinePhase).
const (
	PipelineResearchScopeTmpl = "Research scope %d/%d for this requested task:\n%s\n\nUser task:\n%s\n\nDeliver only verified findings with file:line or file:symbol references, risks, and facts the implementation plan must carry. Do not edit."

	PipelinePlanningInputTmpl = "[pipeline planning input]\nThe banked findings below are the authoritative inspection result. Do not re-read covered files; call update_plan directly. Include implementation steps only: every step needs an exact target, cited finding, acceptance check, and dependency marker. Example step title: \"" + PlanStepExampleBody + "\". Put repository-wide checks only in the note's Verification section.\n%s"

	// Feature 012 RG-5: the approval gate is the advance notice for the
	// phase-read rule — it renders BEFORE full-depth implementation can begin,
	// so the read gate never fires unannounced (gate-policy clause a).
	PipelineApprovalGateBody = "[approval gate]\nThe saved plan is awaiting explicit user approval. Do not implement. Once approved, full-depth implementation file reading belongs to the implementation subagents — dispatch work and orchestrate from their reports instead of reading implementation files yourself."

	PipelineLightImplementationBody = "[light implementation]\nThe plan is approved. Implement it in the main conversation, keep every step status current, and run its verification commands before finishing."

	PipelineValidationGateBody = "[validation gate]\nRun the plan's verification commands and review the changed files. Completion remains blocked until a successful check confirms validation."

	PipelineCompleteBody = "[pipeline complete]\nResearch, planning, approved implementation, and validation have completed. Attribute their contributions in the final answer."

	PipelineImplementationRecoveryBody = "[implementation recovery]\nSuccessful subagent plan parts are marked complete. Failed parts remain open; absorb them directly, keep update_plan current, then validate."

	PipelineImplementationCompleteTmpl = "[implementation phase complete]\n%d implementation group(s) executed the approved plan.\n%s"

	PipelineValidationDegradedBody = "[validation degraded]\nRun the plan's verification commands directly; a successful check confirms the fallback and unlocks completion."

	PipelineValidationCompleteBody = "[validation phase complete]\nA review subagent checked the completed plan and reported before finalization."

	PipelineValidationAlreadyDoneBody = "[validation phase complete] Validation was already confirmed."
)

var (
	pipelineResearchScopeText          = Register(Text{ID: "pipeline.research.scope-task", Audience: MainDynamic, Cache: Tail, Body: PipelineResearchScopeTmpl})
	pipelinePlanningInputText          = Register(Text{ID: "pipeline.planning.input", Audience: MainDynamic, Cache: Tail, Body: PipelinePlanningInputTmpl, StatesRule: RulePlanStepShape, Example: ExamplePlanStep.ID, MentionsTools: []string{"update_plan"}, AllowlistCtx: "main-loop"})
	pipelineApprovalGateText           = Register(Text{ID: "pipeline.approval.gate", Audience: MainDynamic, Cache: Tail, Body: PipelineApprovalGateBody, StatesRule: RulePhaseRead})
	pipelineLightImplementationText    = Register(Text{ID: "pipeline.implementing.light", Audience: MainDynamic, Cache: Tail, Body: PipelineLightImplementationBody})
	pipelineValidationGateText         = Register(Text{ID: "pipeline.validating.gate", Audience: MainDynamic, Cache: Tail, Body: PipelineValidationGateBody})
	pipelineCompleteText               = Register(Text{ID: "pipeline.finished", Audience: MainDynamic, Cache: Tail, Body: PipelineCompleteBody})
	pipelineImplementationRecoveryText = Register(Text{ID: "pipeline.implementing.recovery", Audience: MainDynamic, Cache: Tail, Body: PipelineImplementationRecoveryBody})
	pipelineImplementationCompleteText = Register(Text{ID: "pipeline.implementing.complete", Audience: MainDynamic, Cache: Tail, Body: PipelineImplementationCompleteTmpl})
	pipelineValidationDegradedText     = Register(Text{ID: "pipeline.validating.degraded", Audience: MainDynamic, Cache: Tail, Body: PipelineValidationDegradedBody})
	pipelineValidationCompleteText     = Register(Text{ID: "pipeline.validating.complete", Audience: MainDynamic, Cache: Tail, Body: PipelineValidationCompleteBody})
	pipelineValidationAlreadyDoneText  = Register(Text{ID: "pipeline.validating.already-done", Audience: MainDynamic, Cache: Tail, Body: PipelineValidationAlreadyDoneBody})
)

// Pipeline implementation/validation subagent task templates. The
// implementation template reuses ReportFormatImplementation verbatim
// instead of a second hand-copied field list (fix c's canonical-copy
// discipline extends to this site too).
const PipelineImplementTaskTmpl = "Execute only these approved plan steps, in order:\n%s\n\nInspect before editing. Preserve unrelated/user changes. Run focused checks. Report exactly: " + ReportFormatImplementation

var pipelineImplementTaskText = Register(Text{
	ID: "pipeline.implementing.task", Audience: Subagent, Cache: Sidecar, Body: PipelineImplementTaskTmpl,
	MentionsTools: []string{}, AllowlistCtx: "subagent.general",
})

const PipelineValidateTaskTmpl = "Validate the completed approved plan against the changed workspace. Run each Verification command once using the read-only shell capability before doing any extra inspection. Use one git diff/status snapshot when available; do not repeat predecessor per-file reads or greps unless a check fails and creates a concrete ambiguity. End with `VERDICT: PASS` when acceptance checks are satisfied or `VERDICT: FAIL` with exact corrective actions.\n\n%s"

var pipelineValidateTaskText = Register(Text{
	ID: "pipeline.validating.task", Audience: Subagent, Cache: Sidecar, Body: PipelineValidateTaskTmpl,
	MentionsTools: []string{"run_shell", "git_diff", "git_status"}, AllowlistCtx: "subagent.review",
})

// PipelineValidateFocusedScopeBody (feature 011 T022/D6) is appended to a
// FOCUSED-tier validation review: it bounds the surface per
// contracts/review-gating.md §4 (changed files + same-package files + one-hop
// importers, max 15) and requires the machine-parseable Coverage line the
// harness records (CoverageReport).
const PipelineValidateFocusedScopeBody = "\n\nFocused scope: examine ONLY the changed files (one git diff/status snapshot) plus their direct dependents — same-package files and one-hop importers — up to 15 files total; name anything beyond that as skipped instead of reading it. End your report with one line exactly of the form `Coverage: covered=<file list>; skipped=<file list or none>`."

var pipelineValidateFocusedScopeText = Register(Text{
	ID: "pipeline.validating.focused-scope", Audience: Subagent, Cache: Sidecar, Body: PipelineValidateFocusedScopeBody,
	MentionsTools: []string{"git_diff", "git_status"}, AllowlistCtx: "subagent.review",
})

const PipelineValidateRecoveryAppendBody = "\n\nRecovery attempt: restrict inspection to the changed files and the exact verification commands in the plan. Return the required verdict even if blocked."

var pipelineValidateRecoveryAppendText = Register(Text{ID: "pipeline.validating.recovery-append", Audience: Subagent, Cache: Sidecar, Body: PipelineValidateRecoveryAppendBody})
