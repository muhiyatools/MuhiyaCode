package instructions

// Gate/denial/loop-guard texts (Audience: Gate, Cache: Sidecar): returned as
// tool-call results when the engine blocks or denies an action. Templates
// keep their original Sprintf verbs; orchestrator/engine.go and
// orchestrator/subagent.go fill them at the call site exactly as before —
// this file only relocates the literal template strings so they are named,
// registered, and auditable in one place (IS-1).

// RuleContinuationReviewOnly is the feature-012 R-D2 rule: a review-after-
// implement continuation carries the implementer's tool array on the wire for
// cache identity, but is review-only in effect — mutations are refused
// harness-side. Stated in advance by the review subagent's system text.
const RuleContinuationReviewOnly = "rule.continuation-review-only"

// GateContinuationReviewMutationBody masks mutating tools inside a
// review-after-implement continuation (feature 012 R-D2).
const GateContinuationReviewMutationBody = "This continuation is review-only: mutations are refused here. Report verified findings with file:line and a VERDICT line; do not edit."

var gateContinuationReviewMutationText = Register(Text{ID: "gate.continuation-review.mutation", Audience: Gate, Cache: Sidecar, Body: GateContinuationReviewMutationBody, EnforcesRule: RuleContinuationReviewOnly})

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
)

var (
	gateSubagentBudgetExhaustedText = Register(Text{ID: "gate.subagent-budget.exhausted", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetExhaustedTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetZeroText      = Register(Text{ID: "gate.subagent-budget.zero", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetZeroBody, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetClosedText    = Register(Text{ID: "gate.subagent-budget.closed", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetClosedTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
	gateSubagentBudgetSoftText      = Register(Text{ID: "gate.subagent-budget.soft", Audience: Gate, Cache: Sidecar, Body: GateSubagentBudgetSoftTmpl, EnforcesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop"})
)

// Read-only-agent run_shell rejection (subagent gate; distinct from the
// main-loop plan-mode variant above because a subagent's readOnly flag
// gates it directly rather than through the mutation-block lifecycle check).
const GateSubagentShellBlockedBody = "Blocked: this read-only agent's run_shell only runs read-only commands. " + ReadOnlyShellAllowlistBody + " Do not retry the same command. For searching or counting file contents your grep tool is faster; for reading files use read_file — neither needs the shell."

var gateSubagentShellBlockedText = Register(Text{
	ID: "gate.subagent.run_shell-blocked", Audience: Gate, Cache: Sidecar, Body: GateSubagentShellBlockedBody,
	EnforcesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell", "grep", "read_file"}, AllowlistCtx: "subagent.read-only",
})

// RuleExecutionBelongsToAgent is the plan/execute split (owner directive
// 2026-07-20): the main session plans and instructs; the execution agent does
// the work. Stated in advance by the operating contract and the run_subagent
// description, enforced by the execution role gate.
const RuleExecutionBelongsToAgent = "rule.execution-belongs-to-agent"

// GateRoleExecutionBody fires when the main model tries to change a workspace
// file itself. It names the exact reason and the one affordable next action.
const GateRoleExecutionBody = "Execution runs in the execution agent, not here. Call run_subagent (agent \"general\") with the specific change, the files involved, and the check that proves it works. This session plans, instructs, and reviews — tasks.md is the one file you edit directly."

var gateRoleExecutionText = Register(Text{
	ID: "gate.role.execution", Audience: Gate, Cache: Sidecar, Body: GateRoleExecutionBody,
	EnforcesRule: RuleExecutionBelongsToAgent, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop",
})

// GateRoleExecutionEscalatedTmpl replaces the standard refusal once the model
// has attempted several direct mutations. A gate that repeats one sentence
// forever reads as a loop; escalating proves the harness is responding and
// converges the model onto the delegation path.
const GateRoleExecutionEscalatedTmpl = "[loop guard] %d direct edits attempted in this task. They will all be refused — this session cannot change workspace files. Call run_subagent (agent \"general\") with the full change now, or give the final answer explaining what is blocked."

var gateRoleExecutionEscalatedText = Register(Text{
	ID: "gate.role.execution-escalated", Audience: Gate, Cache: Sidecar, Body: GateRoleExecutionEscalatedTmpl,
	EnforcesRule: RuleExecutionBelongsToAgent, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop",
})
