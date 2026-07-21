package instructions

// The read-only shell allowlist. It survived the removal of the subagent
// system because its OTHER reader outlived it: the main loop in manual plan
// mode, whose run_shell gate and advance notice both quote this same text.
//
// Kept in exact sync with orchestrator's IsReadOnlyShell — a change to one must
// update the other; the shellgate sync test pins them together.

// ReadOnlyShellAllowlistBody is the ONE canonical description of which shell
// commands a read-only reader may run via run_shell. It is stated in advance
// (the plan-mode notice) rather than disclosed only on rejection.
const ReadOnlyShellAllowlistBody = "You may run any inspection, build, or test command, and chain commands freely with 'cd', &&, ||, ;, & and pipes (e.g. cd path && go build ./..., dir /s /b 2>nul | head, git status && npm test). Stderr silencers (2>/dev/null, 2>nul, 2>&1) and input redirects (<) are fine. Only genuinely destructive operations are refused: file-writing or -deleting commands (rm, del, mv, cp, mkdir, touch, tee, sed, Remove-Item, and git add/commit/push/checkout/reset), in-place or file-writing flags (--fix, --in-place, --output, find -delete/-exec), and output redirection that writes a file (>, >>). To change a file use edit_file/write_file, not the shell."

// ReadOnlyShellExamples pins the prose above to the actual IsReadOnlyShell
// enforcement: the shellgate sync test asserts the classifier agrees with every
// row, so the text a model reads and the gate that judges it cannot drift.
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

// RuleReadOnlyShell links the advance-notice text (StatesRule) to the plan-mode
// gate text (EnforcesRule) for the stated-in-advance audit (IS-4).
const RuleReadOnlyShell = "rule.read-only-shell"

var _ = Register(Text{
	ID: "rule.read-only-shell.allowlist", Audience: MainStatic, Cache: Sidecar,
	Body: ReadOnlyShellAllowlistBody, StatesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "main-loop",
})

// ChunkedWriteRuleBody is the ONE canonical sentence stating how to produce a
// file too large for a single call. It is registered on its own so the
// truncation gates have a rule stated in advance (IS-4), and it is composed
// into the main prompt's edit discipline so the model learns it BEFORE it hits
// the output cap rather than after a wasted turn.
//
// Line counts, not tokens: a model cannot count its own tokens, but it can size
// a section. The mechanics are what the tools support — a successful write_file
// enters the read ledger, so the follow-up edits need no re-read, and edit_file
// matches its marker anywhere in the file.
const ChunkedWriteRuleBody = "A file larger than roughly 400 lines is written in parts: write_file the opening section ending with a unique marker line, extend it with edit_file replacing that marker with the next section plus the marker again, and delete the marker in the final edit — never resend content already written."

var _ = Register(Text{
	ID: "rule.chunked-write.sentence", Audience: MainStatic, Cache: Prefix, Body: ChunkedWriteRuleBody,
	StatesRule: RuleChunkedWrite, MentionsTools: []string{"write_file", "edit_file"}, AllowlistCtx: "main-loop",
})

// AdvisorSystemBody is the task advisor's system text. It runs once at each
// task boundary on the cheap utility model and answers with one JSON object.
//
// The old version taught a main/sub PAIRING that no longer exists. This one
// chooses a single model for the task about to start, and states the cost that
// makes the choice non-obvious: provider caches are per-model, so a switch pays
// a one-time cold start on the new model.
const AdvisorSystemBody = `You are the task advisor for MuhiyaCode, a coding agent. A new task is starting in an ongoing session. Decide whether the CURRENT model should handle it, or name a better one from AVAILABLE MODELS.

Respond with ONLY one JSON object:
  {"keep": true}
or
  {"model": "<id>", "why": "<one short line>"}

Rules:
- {"keep": true} is the right answer unless this task clearly outgrows the current model — a capability jump (deep multi-file reasoning, tricky debugging, large refactors) or a context window it lacks.
- Read SWITCH COST before deciding. Provider caches are per-model: a model this session has not used starts cold and re-reads the whole conversation at full price, every token. The larger that number, the stronger the reason to move must be. Early in a session it is small and a switch is cheap; deep in a long session it is the most expensive thing you can choose.
- Returning to a model listed as already warm is far cheaper than lighting up a cold one — prefer it when either would do.
- Never switch for a task the current model can do adequately, and never propose a premium model for chat, questions, or a small fix. Prefer stepping back down once heavy work is done, but only if that model is already warm or the conversation is still small.
- Choose ONLY from AVAILABLE MODELS and copy the id exactly.`

var _ = Register(Text{ID: "advisor.system", Audience: MainStatic, Cache: Sidecar, Body: AdvisorSystemBody})
