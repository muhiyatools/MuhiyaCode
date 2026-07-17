package instructions

// Model-facing workspace tool errors (internal/workspace/files.go and
// patch.go). These are Gate-class: returned as a failed tool-call result,
// read and acted on by whichever reader made the call (main loop or
// subagent). Only the errors that teach a rule or a recovery step are
// registered here (IS-1); purely internal/security-boundary sentinel errors
// (ErrPermissionDenied, ErrSensitivePath, ErrOutsideWorkspace, the
// PermissionMode validator) stay inline in workspace/permissions.go and
// workspace/types.go — they gate access control, not a model-actionable
// format or capability rule, so folding them into this audited registry
// would not add review value.
const (
	WorkspaceEditIdenticalBody      = "oldString and newString are identical; skipped."
	WorkspaceEditAlreadyPresentBody = "newString is already present; skipped stale edit."
	WorkspaceEditNotFoundPrefix     = "oldString not found; closest region: "
	WorkspaceEditAmbiguousTmpl      = "oldString appears %d times; add surrounding context or set replaceAll"
	WorkspaceEditEmptyOldStringTmpl = "edit %d: oldString is empty"
)

var (
	workspaceEditIdenticalText      = Register(Text{ID: "workspace.edit.identical", Audience: Gate, Cache: Sidecar, Body: WorkspaceEditIdenticalBody, MentionsTools: []string{"edit_file", "multi_edit"}, AllowlistCtx: "main-loop"})
	workspaceEditAlreadyPresentText = Register(Text{ID: "workspace.edit.already-present", Audience: Gate, Cache: Sidecar, Body: WorkspaceEditAlreadyPresentBody, MentionsTools: []string{"edit_file", "multi_edit"}, AllowlistCtx: "main-loop"})
	workspaceEditNotFoundText       = Register(Text{ID: "workspace.edit.not-found", Audience: Gate, Cache: Sidecar, Body: WorkspaceEditNotFoundPrefix, MentionsTools: []string{"edit_file", "multi_edit"}, AllowlistCtx: "main-loop"})
	workspaceEditAmbiguousText      = Register(Text{ID: "workspace.edit.ambiguous", Audience: Gate, Cache: Sidecar, Body: WorkspaceEditAmbiguousTmpl, MentionsTools: []string{"edit_file", "multi_edit"}, AllowlistCtx: "main-loop"})
	workspaceEditEmptyOldStringText = Register(Text{ID: "workspace.edit.empty-old-string", Audience: Gate, Cache: Sidecar, Body: WorkspaceEditEmptyOldStringTmpl, MentionsTools: []string{"multi_edit"}, AllowlistCtx: "main-loop"})
)

// WorkspaceUnreadOverwriteBody is ErrUnreadOverwrite's message (write_file,
// edit_file/multi_edit, apply_patch all return "%w: %s" with this sentinel).
// It restates the same rule as instructions.WriteFilePermissionRuleBody at
// the moment of violation, so the two are deliberately worded to agree.
const WorkspaceUnreadOverwriteBody = "workspace: refusing to overwrite an unread file"

var workspaceUnreadOverwriteText = Register(Text{
	ID: "workspace.unread-overwrite", Audience: Gate, Cache: Sidecar, Body: WorkspaceUnreadOverwriteBody,
	EnforcesRule: RuleWriteFilePermission, MentionsTools: []string{"write_file", "edit_file", "multi_edit", "apply_patch"}, AllowlistCtx: "main-loop",
})

// WorkspaceGrepInvalidPatternTmpl teaches the fix, not just the failure:
// literal emoji in a character class is a live recurring model mistake.
const WorkspaceGrepInvalidPatternTmpl = "invalid search pattern: %w — patterns use Go RE2 syntax; for unicode ranges write \\x{1F600}-style escapes (e.g. [\\x{1F300}-\\x{1FAFF}]) — literal emoji range endpoints are invalid; or set literal=true to match exact text"

var workspaceGrepInvalidPatternText = Register(Text{ID: "workspace.grep.invalid-pattern", Audience: Gate, Cache: Sidecar, Body: WorkspaceGrepInvalidPatternTmpl, MentionsTools: []string{"grep"}, AllowlistCtx: "main-loop"})

const (
	WorkspaceFileExceedsLimitTmpl  = "file exceeds %d-byte limit"
	WorkspaceBinaryUnsupportedBody = "binary or non-UTF-8 file is not supported"
	WorkspaceListNotDirectoryTmpl  = "list path is not a directory: %s"
)

var (
	workspaceFileExceedsLimitText  = Register(Text{ID: "workspace.read.file-exceeds-limit", Audience: Gate, Cache: Sidecar, Body: WorkspaceFileExceedsLimitTmpl, MentionsTools: []string{"read_file"}, AllowlistCtx: "main-loop"})
	workspaceBinaryUnsupportedText = Register(Text{ID: "workspace.read.binary-unsupported", Audience: Gate, Cache: Sidecar, Body: WorkspaceBinaryUnsupportedBody, MentionsTools: []string{"read_file"}, AllowlistCtx: "main-loop"})
	workspaceListNotDirectoryText  = Register(Text{ID: "workspace.list.not-directory", Audience: Gate, Cache: Sidecar, Body: WorkspaceListNotDirectoryTmpl, MentionsTools: []string{"list_files"}, AllowlistCtx: "main-loop"})
)
