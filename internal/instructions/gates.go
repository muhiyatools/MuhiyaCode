package instructions

// Gate/denial/loop-guard texts (Audience: Gate, Cache: Sidecar): returned as
// tool-call results when the engine blocks or denies an action. Templates
// keep their original Sprintf verbs; orchestrator/engine.go and
// orchestrator/subagent.go fill them at the call site exactly as before —
// this file only relocates the literal template strings so they are named,
// registered, and auditable in one place (IS-1).

// RulePlanModeReadOnly is the rule ID for "no mutating tool succeeds in a
// read-only lifecycle state" — stated in advance by planBlock (Tail) and the
// read-only subagent notices, enforced by the plan-mode/pipeline mutation
// gates below.
const RulePlanModeReadOnly = "rule.plan-mode-read-only"

const (
	GatePlanModeMutationBody  = "Plan mode is read-only — finish planning and call exit_plan_mode."
	GatePlanModeShellBlocked  = "run_shell blocked in plan mode. " + ReadOnlyShellAllowlistBody
	GatePlanModeMCPBlocked    = "MCP tools are unavailable in plan mode. Plan with the workspace read tools (read_file, grep, glob, git_status, git_diff) instead."
	GatePlanModeSoftSuffix    = " No mutating tool will succeed in plan mode."
	GatePlanModeEscalatedTmpl = "[loop guard] You have attempted %d mutations in plan mode across %s class. STOP attempting changes. Finish the plan now and call exit_plan_mode."
)

var (
	gatePlanModeMutationText = Register(Text{
		ID: "gate.plan-mode.mutation", Audience: Gate, Cache: Sidecar, Body: GatePlanModeMutationBody,
		EnforcesRule: RulePlanModeReadOnly, MentionsTools: []string{"exit_plan_mode"}, AllowlistCtx: "main-loop",
	})
	gatePlanModeShellText = Register(Text{
		ID: "gate.plan-mode.run_shell-blocked", Audience: Gate, Cache: Sidecar, Body: GatePlanModeShellBlocked,
		EnforcesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "main-loop",
	})
	gatePlanModeMCPText = Register(Text{
		ID: "gate.plan-mode.mcp-blocked", Audience: Gate, Cache: Sidecar, Body: GatePlanModeMCPBlocked,
		EnforcesRule: RulePlanModeReadOnly, MentionsTools: []string{"read_file", "grep", "glob", "git_status", "git_diff"}, AllowlistCtx: "main-loop",
	})
	gatePlanModeEscalatedText = Register(Text{
		ID: "gate.plan-mode.escalated", Audience: Gate, Cache: Sidecar, Body: GatePlanModeEscalatedTmpl,
		EnforcesRule: RulePlanModeReadOnly, MentionsTools: []string{"exit_plan_mode"}, AllowlistCtx: "main-loop",
	})
)

const (
	GatePipelineMutationTmpl  = "Pipeline phase %s blocks implementation. Finish research and the execution-grade plan, then await explicit approval; no mutating tool will succeed before implementation."
	GatePipelineEscalatedTmpl = "[loop guard] You attempted %d mutations while pipeline phase=%s. STOP mutating; finish research/plan and await approval."
)

var (
	gatePipelineMutationText  = Register(Text{ID: "gate.pipeline.mutation", Audience: Gate, Cache: Sidecar, Body: GatePipelineMutationTmpl, EnforcesRule: RulePlanModeReadOnly})
	gatePipelineEscalatedText = Register(Text{ID: "gate.pipeline.escalated", Audience: Gate, Cache: Sidecar, Body: GatePipelineEscalatedTmpl, EnforcesRule: RulePlanModeReadOnly})
)

// RulePhaseRead is the feature-012 role-separation rule (contracts/role-gate.md):
// during full-depth implementation phases, file reading belongs to the
// implementation subagents; the main model orchestrates from reports.
const RulePhaseRead = "rule.phase-read"

const (
	// GatePhaseReadBlockTmpl fires when the main model reads an implementation
	// file while full-depth implementation is active (RG-3). It names the exact
	// reason and the affordable next action, and shows the bounded allowance.
	GatePhaseReadBlockTmpl = "Implementation phase: file reading belongs to the implementation subagents. Dispatch the work (run_subagent) or await the phase report. [read attempt %d of 2 — the third proceeds with a recorded waiver]"
	// GateContinuationReviewMutationBody masks mutating tools inside a
	// review-after-implement continuation (feature 012 R-D2): the wire tool
	// array stays the implementer's (cache identity), so the refusal happens
	// harness-side.
	GateContinuationReviewMutationBody = "This continuation is review-only: mutations are refused here. Report verified findings with file:line and a VERDICT line; do not edit."
)

var (
	gatePhaseReadBlockText             = Register(Text{ID: "gate.phase-read.block", Audience: Gate, Cache: Sidecar, Body: GatePhaseReadBlockTmpl, EnforcesRule: RulePhaseRead})
	gateContinuationReviewMutationText = Register(Text{ID: "gate.continuation-review.mutation", Audience: Gate, Cache: Sidecar, Body: GateContinuationReviewMutationBody, EnforcesRule: RulePhaseRead})
)

// RuleNoRepeatFailedCall / RuleNoDuplicateRead are the terse-gate rule IDs
// covered by the gate-message-quality audit (IS-8).
const (
	RuleNoRepeatFailedCall = "rule.no-repeat-failed-call"
	RuleNoDuplicateRead    = "rule.no-duplicate-read"
)

const (
	// GateRepeatLimiterBody fires after three identical calls. It names the
	// exact trigger (repeated three times) and an affordable next action
	// (change action, or finish) — IS-8.
	GateRepeatLimiterBody = "Blocked: identical call repeated three times; take a different action or finish."
	// GateDuplicateReadTmpl fires when a read's result is already in
	// unchanged context. It names the exact reason (call %s already holds
	// the result) and the affordable next action (use that result) — IS-8.
	GateDuplicateReadTmpl = "Blocked: unchanged result already in context from call %s. Use that result; do not re-read."
)

var (
	gateRepeatLimiterText = Register(Text{ID: "gate.repeat-limiter", Audience: Gate, Cache: Sidecar, Body: GateRepeatLimiterBody, EnforcesRule: RuleNoRepeatFailedCall})
	gateDuplicateReadText = Register(Text{ID: "gate.duplicate-read", Audience: Gate, Cache: Sidecar, Body: GateDuplicateReadTmpl, EnforcesRule: RuleNoDuplicateRead})
)

// RuleSubagentBudget is the rule ID for the per-task/per-phase run_subagent
// allowance.
const RuleSubagentBudget = "rule.subagent-budget"

const (
	GateSubagentBudgetExhaustedTmpl = "subagent budget exhausted (%d of %d run(s) used)"
	GateSubagentBudgetZeroBody      = "no subagent budget for this task (agents=0)"
	GateSubagentBudgetClosedTmpl    = "%s. run_subagent is closed for the rest of this task and every further call will fail — do NOT call it again. Continue the remaining work directly with your own read/edit tools now"
	GateSubagentBudgetSoftTmpl      = "%s; complete the remaining work directly with your own tools instead of delegating"
	GateSubagentPlanModeGeneralBody = "blocked: plan mode is read-only; only explore/plan/review subagents are available. Use them to investigate, then finish your plan."
)

var (
	gateSubagentBudgetExhaustedText = Register(Text{ID: "gate.subagent-budget.exhausted", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetExhaustedTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetZeroText      = Register(Text{ID: "gate.subagent-budget.zero", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetZeroBody, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetClosedText    = Register(Text{ID: "gate.subagent-budget.closed", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetClosedTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetSoftText      = Register(Text{ID: "gate.subagent-budget.soft", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetSoftTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentPlanModeGeneralText = Register(Text{ID: "gate.subagent.plan-mode-general-blocked", Audience: Gate, Cache: Sidecar, Body: GateSubagentPlanModeGeneralBody, EnforcesRule: RulePlanModeReadOnly, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
)

// Read-only-agent run_shell rejection (subagent gate; distinct from the
// main-loop plan-mode variant above because a subagent's readOnly flag
// gates it directly rather than through the mutation-block lifecycle check).
const GateSubagentShellBlockedBody = "Blocked: this read-only agent's run_shell only runs read-only commands. " + ReadOnlyShellAllowlistBody + " Do not retry the same command. For searching or counting file contents your grep tool is faster; for reading files use read_file — neither needs the shell."

var gateSubagentShellBlockedText = Register(Text{
	ID: "gate.subagent.run_shell-blocked", Audience: Gate, Cache: Sidecar, Body: GateSubagentShellBlockedBody,
	EnforcesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell", "grep", "read_file"}, AllowlistCtx: "subagent.read-only",
})

// RulePlanStepShape / RulePlanNoteShape (fix c/e): the plan-step title shape
// and the plan-note Verification:/Risks: sections. GatePlanQualityGapTmpl is
// the single ALL-unmet-requirements-at-once rejection pipelinePlanContentBar
// returns when exit_plan_mode is called on a plan that fails the content
// bar; it now cites the ONE canonical plan-step example (fix c) instead of
// the pre-010 divergent second example.
const RulePlanNoteShape = "rule.plan-note-shape"

const GatePlanQualityGapTmpl = "plan not accepted yet — fix ALL of the following with ONE update_plan call, then call exit_plan_mode again:\n- %s\nRequired step shape: \"<file or function target>: <change> [F#] — Verify: <observable check>\" (example: \"" + PlanStepExampleBody + "\"). Repository-wide commands belong in the note's Verification: section, not in a step. Do NOT paste the plan into your reply — store it with update_plan"

var gatePlanQualityGapText = Register(Text{
	ID: "gate.plan-quality-gap", Audience: Gate, Cache: Sidecar, Body: GatePlanQualityGapTmpl,
	EnforcesRule: RulePlanStepShape, Example: ExamplePlanStep.ID, MentionsTools: []string{"update_plan", "exit_plan_mode"}, AllowlistCtx: "main-loop",
})

const (
	GatePlanGapNoResearchBody     = "pipeline plan is not research-grounded: bank at least one research finding (run an explore/plan subagent) or record why research degraded"
	GatePlanGapStepMissingTmpl    = "pipeline plan step %d is missing %s"
	GatePlanGapNoVerificationBody = "pipeline plan note needs a non-empty `Verification:` section with concrete commands/checks"
	GatePlanGapNoRisksBody        = "pipeline plan note needs a non-empty `Risks:` section"
)

var (
	gatePlanGapNoResearchText     = Register(Text{ID: "gate.plan-quality-gap.no-research", Audience: Gate, Cache: Sidecar, Body: GatePlanGapNoResearchBody, EnforcesRule: RulePlanStepShape, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gatePlanGapStepMissingText    = Register(Text{ID: "gate.plan-quality-gap.step-missing", Audience: Gate, Cache: Sidecar, Body: GatePlanGapStepMissingTmpl, EnforcesRule: RulePlanStepShape, Example: ExamplePlanStep.ID})
	gatePlanGapNoVerificationText = Register(Text{ID: "gate.plan-quality-gap.no-verification", Audience: Gate, Cache: Sidecar, Body: GatePlanGapNoVerificationBody, EnforcesRule: RulePlanNoteShape, Example: ExamplePlanNote.ID})
	gatePlanGapNoRisksText        = Register(Text{ID: "gate.plan-quality-gap.no-risks", Audience: Gate, Cache: Sidecar, Body: GatePlanGapNoRisksBody, EnforcesRule: RulePlanNoteShape, Example: ExamplePlanNote.ID})
)
