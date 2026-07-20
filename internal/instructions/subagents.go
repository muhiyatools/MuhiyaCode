package instructions

// ReadOnlyShellAllowlistBody is the ONE canonical description of which shell
// commands a read-only reader (a read-only subagent, or the main loop in
// manual plan mode) may run via run_shell. Feature 010 US3 fix (b): before
// this feature the allowlist was disclosed only on rejection (the subagent
// gate text below) — a read-only reader learned the rule by violating it.
// The identical clause now also appears in the plan-mode advance notice
// (orchestrator plan.go planBlock, Tail) and the read-only subagent system
// texts below (Sidecar), both BEFORE any violation, and in the main-loop
// plan-mode run_shell gate text (previously an abbreviated "etc." list).
// Kept in exact sync with orchestrator's IsReadOnlyShell — a change to one
// must update the other.
const ReadOnlyShellAllowlistBody = "You may run any inspection, build, or test command, and chain commands freely with 'cd', &&, ||, ;, & and pipes (e.g. cd path && go build ./..., dir /s /b 2>nul | head, git status && npm test). Stderr silencers (2>/dev/null, 2>nul, 2>&1) and input redirects (<) are fine. Only genuinely destructive operations are refused: file-writing or -deleting commands (rm, del, mv, cp, mkdir, touch, tee, sed, Remove-Item, and git add/commit/push/checkout/reset), in-place or file-writing flags (--fix, --in-place, --output, find -delete/-exec), and output redirection that writes a file (>, >>). To change a file use edit_file/write_file, not the shell."

// ReadOnlyShellExamples pins the prose in ReadOnlyShellAllowlistBody to the
// actual IsReadOnlyShell enforcement: orchestrator's shellgate sync test (T034)
// asserts the classifier agrees with every row here, so the text a model reads
// and the gate that judges it can never drift apart. Each row is a real command
// and whether a read-only agent may run it.
var ReadOnlyShellExamples = []struct {
	Command string
	Allowed bool
}{
	{`cd path && go build ./...`, true},
	{`dir /s /b 2>nul | head`, true},
	{`git status && npm test`, true},
	{`grep format main.go`, true},     // "format" is an argument, not the command
	{`rg kill internal/`, true},       // "kill" is an argument
	{`git log --grep "rm -rf"`, true}, // "rm -rf" is quoted data
	{`rm -rf x`, false},
	{`git commit -m x`, false},
	{`grep a > out.txt`, false},   // writes a file
	{`npx eslint --fix .`, false}, // --fix mutates
	{`format c:`, false},          // "format" at command position
	{`kill -9 123`, false},
}

// RuleReadOnlyShell is the rule ID the shell allowlist states and enforces,
// linking the advance-notice texts (StatesRule) to the gate texts
// (EnforcesRule) for the stated-in-advance audit (IS-4).
const RuleReadOnlyShell = "rule.read-only-shell"

var readOnlyShellAllowlistText = Register(Text{
	ID: "rule.read-only-shell.allowlist", Audience: Subagent, Cache: Sidecar,
	Body: ReadOnlyShellAllowlistBody, StatesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "subagent.read-only",
})

// SubagentReadOnlyShellNoticeBody is the sentence explore/review
// subagents' System text appends so the read-only shell rule is stated
// before any call, not only on rejection.
const SubagentReadOnlyShellNoticeBody = "Your run_shell only runs read-only commands: " + ReadOnlyShellAllowlistBody

var subagentReadOnlyShellNoticeText = Register(Text{
	ID: "subagent.read-only.run_shell-notice", Audience: Subagent, Cache: Sidecar,
	Body: SubagentReadOnlyShellNoticeBody, StatesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "subagent.read-only",
})

// Subagent kind Description + System bodies (Sidecar-class: composed fresh
// per subagent run into that run's own system message, never the main
// loop's cached prefix). Read-only kinds (explore/review) each end with
// the read-only-shell advance notice (fix b); general's schema is the real
// workspace-tool registry filtered by its Allowed map, so its Description
// deliberately does not claim run_subagent/ask_user/
// propose_changes — see internal/instructions/audit_test.go's capability-
// reference check and internal/orchestrator/instructions_wiring_test.go,
// which wires the real subagentSpecs() allowlists against every Text naming
// a tool (fix a; IS-6).
const (
	SubagentExploreDescription = "Read-only codebase exploration that returns grounded findings."
	SubagentExploreSystem      = "Explore the requested code paths efficiently. Search first, batch reads, cite exact files and symbols, and report only verified findings. You cannot edit. " + SubagentReadOnlyShellNoticeBody

	SubagentReviewDescription = "Read-only correctness and security review with ranked findings."
	SubagentReviewSystem      = "Review the diff and surrounding code. Report only verified correctness, security, or reliability issues ranked by severity with file:line and a failure scenario. No style nits; do not edit. " + SubagentReadOnlyShellNoticeBody

	// SubagentGeneralDescription deliberately says only "full-tool" over the
	// workspace registry — it never names run_subagent,
	// ask_user, or propose_changes, none of which are in the general
	// subagent's real Allowed map (subagent.go subagentSpecs: Allowed is
	// built from e.registry.Names(), and those four are dispatched only
	// through the main loop's synthetic-tool switch in engine.go
	// executeOne, never registered on e.registry). Naming them here would be
	// exactly the incoherence fix (a) closes: an advertised capability the
	// executor cannot honor.
	SubagentGeneralDescription = "Full-tool agent for an isolated, self-contained coding subtask."
	SubagentGeneralSystem      = EditDisciplineBody + " Complete the isolated subtask end to end. Inspect before editing, make focused changes, and RUN the check that proves the change works. Your caller trusts your report instead of re-checking your work, so it must earn that: end with STATUS: COMPLETE only when you ran the check and it passed. If you could not run it, end with STATUS: NEEDS-VERIFY: <the exact command>. If something stopped you, end with STATUS: BLOCKED: <what>. Never claim COMPLETE for work you did not verify. Do not ask the user questions."
)

var (
	subagentExploreDescText   = Register(Text{ID: "subagent.explore.description", Audience: Subagent, Cache: Sidecar, Body: SubagentExploreDescription})
	subagentExploreSystemText = Register(Text{
		ID: "subagent.explore.system", Audience: Subagent, Cache: Sidecar, Body: SubagentExploreSystem,
		StatesRule: RuleReadOnlyShell, MentionsTools: []string{"list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell"}, AllowlistCtx: "subagent.explore",
	})
	subagentReviewDescText   = Register(Text{ID: "subagent.review.description", Audience: Subagent, Cache: Sidecar, Body: SubagentReviewDescription})
	subagentReviewSystemText = Register(Text{
		ID: "subagent.review.system", Audience: Subagent, Cache: Sidecar, Body: SubagentReviewSystem,
		StatesRule: RuleReadOnlyShell, MentionsTools: []string{"list_files", "read_file", "grep", "search_text", "glob", "git_status", "git_diff", "run_shell"}, AllowlistCtx: "subagent.review",
	})
	subagentGeneralDescText   = Register(Text{ID: "subagent.general.description", Audience: Subagent, Cache: Sidecar, Body: SubagentGeneralDescription, AllowlistCtx: "subagent.general"})
	subagentGeneralSystemText = Register(Text{ID: "subagent.general.system", Audience: Subagent, Cache: Sidecar, Body: SubagentGeneralSystem, AllowlistCtx: "subagent.general"})
)

// Report-format field lists (IS-5's canonical-copy discipline, fix c). Each
// is the ONE literal shape a subagent's HandoffContract.OutputFormat uses
// for its role, referenced from BOTH the handoff contract rendered into the
// subagent's own system message (subagent.go handoffFor) and the
// run_subagent tool description's format guidance shown to the delegating
// caller (engine.go runSubagentDefinition) — before feature 010 the tool
// description paraphrased these with "/" separators and dropped
// "/unknowns" from the research format, so the caller was told a different
// shape than the one the subagent actually produced.
const (
	ReportFormatResearch = "Findings; Exact references; Risks/unknowns; Plan implications."
	// The STATUS line is what lets the caller stop re-verifying. It must be the
	// LAST line, and it is a claim about evidence, not confidence: COMPLETE means
	// the check named in Verification actually ran and passed.
	ReportFormatImplementation = "Changes made with file:line; Verification: each check you ran and its result; Problems; Remaining concerns; then a final line — STATUS: COMPLETE, or STATUS: NEEDS-VERIFY: <the exact command the caller should run>, or STATUS: BLOCKED: <what stopped you>."
	ReportFormatReview         = "Verified findings by severity with file:line; Checks performed; VERDICT: PASS or VERDICT: FAIL; Remaining concerns."
)

var (
	reportFormatResearchText       = Register(Text{ID: "subagent.report-format.research", Audience: Subagent, Cache: Sidecar, Body: ReportFormatResearch})
	reportFormatImplementationText = Register(Text{ID: "subagent.report-format.implementation", Audience: Subagent, Cache: Sidecar, Body: ReportFormatImplementation})
	reportFormatReviewText         = Register(Text{ID: "subagent.report-format.review", Audience: Subagent, Cache: Sidecar, Body: ReportFormatReview})
)

// HandoffDeliverable bodies, one per handoff role.
const (
	HandoffDeliverableResearch       = "Grounded findings and exact references for the named research scope."
	HandoffDeliverableImplementation = "Complete the assigned change without widening scope; check off the tasks.md items you finish."
	HandoffDeliverableReview         = "Independently verify the assigned changes and acceptance checks."
)

// CapabilityStatementPrefix/Suffix compose the per-run capability statement
// told to every subagent: its exact sorted toolset, that nothing else is
// available, and the escalate-ambiguity instruction (004 US3 T038). The
// caller (subagent.go capabilityStatement) inserts the sorted tool-name join
// between them.
const (
	CapabilityStatementPrefix = "Tools available to you: "
	CapabilityStatementSuffix = ". Anything not listed is unavailable to you — do not attempt it. Report what you did and what you verified; when a genuine architectural choice has no defensible default, say so in your report rather than guessing."
)

// HandoffContract label bodies (subagent.go HandoffContract.Render).
const (
	HandoffContractHeader        = "HANDOFF CONTRACT"
	HandoffContractNoOverlapNote = "No predecessor finding overlaps this scope; inspect only the named scope."
)

// SubagentReviewReadOnlyNoticeBody states the review-only rule in advance
// (IS-4): a review run never edits, whether it starts fresh or continues an
// implementer's stream. The gate that refuses mutations inside a review
// continuation (gate.continuation-review.mutation) enforces exactly this.
const SubagentReviewReadOnlyNoticeBody = "You review; you never edit. Report verified findings with file:line and end with a VERDICT line."

var subagentReviewReadOnlyNoticeText = Register(Text{
	ID: "subagent.review.read-only-notice", Audience: Subagent, Cache: Sidecar, Body: SubagentReviewReadOnlyNoticeBody,
	StatesRule: RuleContinuationReviewOnly, AllowlistCtx: "subagent.review",
})

// AdvisorSystemBody is the session advisor's system prompt (v1.1.0). It runs
// ONCE per session on the utility model, before any cached work exists, and
// its expected answer is "keep" — the configured pairing covers almost
// everything. Sidecar class: its own isolated stream, so it costs the main
// prefix nothing.
//
// The prompt states the session-fixed constraint explicitly, because a model
// that thinks it can re-decide later would optimize for the wrong thing.
const AdvisorSystemBody = `You are the session advisor for MuhiyaCode, a coding agent. A new session is starting. Decide whether the CONFIGURED model pairing fits it, or propose a better one from AVAILABLE MODELS.

Respond with ONLY one JSON object:
  {"keep": true}
or
  {"main": "<id>", "sub": "<id>", "why": "<one short line>"}

"main" plans and orchestrates the whole session: it analyzes requests, writes the task checklist, and instructs the execution agent. It needs strong reasoning and a large context window.
"sub" executes: it edits files, runs commands, and reviews, on one long-lived cached stream. It needs strong coding execution and cheap tokens; prefer continuation support.

Rules:
- The pairing is FIXED for the entire session. Judge from this first task what the session will need: complexity, number of components, required output quality, and what breaks if a model falls short.
- {"keep": true} is the right answer unless the configured pairing clearly cannot serve this session — for example the work needs a context window the configured main lacks.
- Choose ONLY from AVAILABLE MODELS and copy ids exactly.
- Never propose a premium model for a session that starts with chat, questions, or a small fix.`

var advisorSystemText = Register(Text{ID: "advisor.system", Audience: Subagent, Cache: Sidecar, Body: AdvisorSystemBody})

// EditDisciplineBody is the editing half of the old CONTEXT AND EDIT
// DISCIPLINE section, delivered to the agent that actually edits files. Under
// the plan/execute split these rules sat in the MAIN model's cached prefix —
// taught, at ~640 bytes per session, exclusively to the one model forbidden
// from using them, while the executor learned edit_file's contract only by
// violating it.
const EditDisciplineBody = "Change existing files with surgical edit_file/multi_edit edits only. " + WriteFilePermissionRuleBody +
	" edit_file oldString must be exact, unique, and different from newString; if it is ambiguous extend the surrounding context, and if it is not found use the returned nearest region. Batch same-file changes with multi_edit."

var editDisciplineText = Register(Text{
	ID: "subagent.edit-discipline", Audience: Subagent, Cache: Sidecar, Body: EditDisciplineBody,
	StatesRule: RuleWriteFilePermission, MentionsTools: []string{"edit_file", "multi_edit", "write_file"}, AllowlistCtx: "subagent.general",
})
