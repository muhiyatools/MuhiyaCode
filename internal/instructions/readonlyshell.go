package instructions

// ReadOnlyShellAllowlistBody is the canonical description of commands allowed
// by the read-only shell gate.
const ReadOnlyShellAllowlistBody = "You may run any inspection, build, or test command, and chain commands freely with 'cd', &&, ||, ;, & and pipes (e.g. cd path && go build ./..., dir /s /b 2>nul | head, git status && npm test). Stderr silencers (2>/dev/null, 2>nul, 2>&1) and input redirects (<) are fine. Only genuinely destructive operations are refused: file-writing or -deleting commands (rm, del, mv, cp, mkdir, touch, tee, sed, Remove-Item, and git add/commit/push/checkout/reset), in-place or file-writing flags (--fix, --in-place, --output, find -delete/-exec), and output redirection that writes a file (>, >>). To change a file use edit_file/write_file, not the shell."

// ReadOnlyShellExamples pins the prose to IsReadOnlyShell enforcement.
var ReadOnlyShellExamples = []struct {
	Command string
	Allowed bool
}{
	{`cd path && go build ./...`, true},
	{`dir /s /b 2>nul | head`, true},
	{`git status && npm test`, true},
	{`grep format main.go`, true},
	{`rg kill internal/`, true},
	{`git log --grep "rm -rf"`, true},
	{`rm -rf x`, false},
	{`git commit -m x`, false},
	{`grep a > out.txt`, false},
	{`npx eslint --fix .`, false},
	{`format c:`, false},
	{`kill -9 123`, false},
}

const RuleReadOnlyShell = "rule.read-only-shell"

var _ = Register(Text{
	ID: "rule.read-only-shell.allowlist", Audience: MainStatic, Cache: Sidecar,
	Body: ReadOnlyShellAllowlistBody, StatesRule: RuleReadOnlyShell, MentionsTools: []string{"run_shell"}, AllowlistCtx: "main-loop",
})

// ChunkedWriteRuleBody keeps large writes recoverable and below model output
// limits without repeating already written content.
const ChunkedWriteRuleBody = "A file larger than roughly 400 lines is written in parts: write_file the opening section ending with a unique marker line, extend it with edit_file replacing that marker with the next section plus the marker again, and delete the marker in the final edit — never resend content already written."

var _ = Register(Text{
	ID: "rule.chunked-write.sentence", Audience: MainStatic, Cache: Prefix, Body: ChunkedWriteRuleBody,
	StatesRule: RuleChunkedWrite, MentionsTools: []string{"write_file", "edit_file"}, AllowlistCtx: "main-loop",
})
