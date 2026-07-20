package instructions

// Tool descriptions: the top-level natural-language description shown to
// the model for each of the 21 tools (12 workspace registry tools wired by
// internal/workspace/registry.go, all eight synthetic main-loop tools wired by
// internal/orchestrator's sessionDefinitions — update_plan, ask_user,
// propose_changes, exit_plan_mode, run_subagent, save_memory, recall_memory,
// edit_memory — and web_search wired by internal/gateway/web.go). All
// Prefix-class: they are part of the session-stable tool JSON block.
//
// Property-level (per-field) schema description strings — short UI hints
// like "Directory, default ." — are deliberately NOT individually registered
// here: they are not natural-language rules subject to the
// contradiction/capability-reference/stated-in-advance audits this package
// exists to run, and registering several dozen one-line hints would dilute
// the review surface without adding audit value. They stay inline at their
// call sites in workspace/registry.go and orchestrator/engine.go. The
// exceptions are the update_plan "note"/"steps[].title" and run_subagent
// "task" property descriptions, which DO carry format rules the audit must
// see (IS-4/IS-7) — those are registered below alongside their tool.
const (
	ToolListFilesDescription  = "List a directory. Use once to map a workspace."
	ToolReadFileDescription   = "Read a UTF-8 file with line numbers. Use offset/limit for large files."
	ToolGrepDescription       = "Regex/literal search with file, line, and text results."
	ToolSearchTextDescription = "Fast literal text search; prefer grep for regex."
	ToolGlobDescription       = "Find files by doublestar glob, e.g. **/*.go."
	ToolEditFileDescription   = "Replace exact text in a file already read. Returns a compact diff."
	ToolMultiEditDescription  = "Apply several ordered exact replacements to one read file."
	// ToolWriteFileDescription is exactly WriteFilePermissionRuleBody
	// (fix d, IS-5): token-identical to the write_file sentence in the
	// system prompt's CONTEXT AND EDIT DISCIPLINE section.
	ToolWriteFileDescription  = WriteFilePermissionRuleBody
	ToolApplyPatchDescription = "Apply a standard unified diff to files already read."
	// Feature 011 D5/T027: the trailing steering sentence keeps the shell for
	// executing, not reading — read_file/grep are cheaper and cache-tracked.
	ToolRunShellDescription  = "Run a shell command in the workspace with streaming output and cancellation. For reading or searching files use read_file/grep instead — they are cheaper and cache-tracked."
	ToolGitStatusDescription = "Show concise git status."
	ToolGitDiffDescription   = "Show the workspace git diff."
)

var (
	toolListFilesText  = Register(Text{ID: "tool.list_files.desc", Audience: MainStatic, Cache: Prefix, Body: ToolListFilesDescription})
	toolReadFileText   = Register(Text{ID: "tool.read_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolReadFileDescription})
	toolGrepText       = Register(Text{ID: "tool.grep.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGrepDescription})
	toolSearchTextText = Register(Text{ID: "tool.search_text.desc", Audience: MainStatic, Cache: Prefix, Body: ToolSearchTextDescription})
	toolGlobText       = Register(Text{ID: "tool.glob.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGlobDescription})
	toolEditFileText   = Register(Text{ID: "tool.edit_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolEditFileDescription})
	toolMultiEditText  = Register(Text{ID: "tool.multi_edit.desc", Audience: MainStatic, Cache: Prefix, Body: ToolMultiEditDescription})
	toolWriteFileText  = Register(Text{
		ID: "tool.write_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolWriteFileDescription,
		StatesRule: RuleWriteFilePermission, MentionsTools: []string{"write_file", "edit_file", "multi_edit"}, AllowlistCtx: "main-loop",
	})
	toolApplyPatchText = Register(Text{ID: "tool.apply_patch.desc", Audience: MainStatic, Cache: Prefix, Body: ToolApplyPatchDescription})
	toolRunShellText   = Register(Text{ID: "tool.run_shell.desc", Audience: MainStatic, Cache: Prefix, Body: ToolRunShellDescription})
	toolGitStatusText  = Register(Text{ID: "tool.git_status.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGitStatusDescription})
	toolGitDiffText    = Register(Text{ID: "tool.git_diff.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGitDiffDescription})
)

// Synthetic main-loop tool descriptions (engine.go sessionDefinitions).
const (
	ToolAskUserDescription        = "Ask 1-3 blocking multiple-choice questions."
	ToolProposeChangesDescription = "Request approval before broad or risky edits."
)

var (
	toolAskUserText        = Register(Text{ID: "tool.ask_user.desc", Audience: MainStatic, Cache: Prefix, Body: ToolAskUserDescription})
	toolProposeChangesText = Register(Text{ID: "tool.propose_changes.desc", Audience: MainStatic, Cache: Prefix, Body: ToolProposeChangesDescription})
)

// run_subagent (engine.go runSubagentDefinition) is the richest tool
// description: a when-to-use rationale, a worked delegation-task example,
// and — fix (c) — the SAME three canonical report-format field lists the
// subagent's own handoff contract renders (instructions.ReportFormat*),
// instead of the pre-010 paraphrase that used "/" separators and silently
// dropped "/unknowns" from the research format.
const ToolRunSubagentDescription = "Run an agent in its own isolated session; only its final report returns to you. " +
	"general is the executor: EVERY workspace change goes through it, and chaining each follow-up to it continues " +
	"the previous run's warm context, so the second change costs far less than the first. " +
	"explore earns a run only for broad reading of code you have NOT read; review for an independent verdict on " +
	"substantial edits. Never spawn a second agent while one is running. " +
	"Example: '" + DelegationTaskExampleBody + "'. " +
	"Kinds: explore = read-only investigation with grounded findings; " +
	"review = read-only correctness/security review of changes; general = full-tool execution. " +
	"The task brief's agents<=N is your allowance."

const ToolRunSubagentTaskPropertyDescription = "What the subagent must accomplish. It does not see this conversation — " +
	"state the deliverable precisely, name the exact paths/symbols in scope, and say what the report must contain. " +
	"Research reports use " + ReportFormatResearch + " Implementation reports use " + ReportFormatImplementation + " Review reports use " + ReportFormatReview

var (
	toolRunSubagentText = Register(Text{
		ID: "tool.run_subagent.desc", Audience: MainStatic, Cache: Prefix, Body: ToolRunSubagentDescription,
		Example: ExampleDelegationTask.ID, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop",
	})
	toolRunSubagentTaskPropertyText = Register(Text{
		ID: "tool.run_subagent.task-property", Audience: MainStatic, Cache: Prefix, Body: ToolRunSubagentTaskPropertyDescription,
		Example: ExampleDelegationTask.ID,
	})
)

// web_search (gateway/web.go).
const ToolWebSearchDescription = "Search the live web for current or niche facts and return citable sources."

var toolWebSearchText = Register(Text{ID: "tool.web_search.desc", Audience: MainStatic, Cache: Prefix, Body: ToolWebSearchDescription, MentionsTools: []string{"web_search"}, AllowlistCtx: "main-loop"})

// save_memory (orchestrator/memory.go). The description carries the
// what-belongs-in-memory teaching (feature 008 MT-2): durable conventions,
// decisions, and user preferences — never task progress, speculation,
// transcript, or secrets — and that saves are rare and concise. Memory Parity
// N3 adds the check-before-save rule (update via edit_memory, never duplicate)
// and the optional kind tag.
const ToolSaveMemoryDescription = "Save one durable project fact to project memory (loaded from the memory index at the start of every session). " +
	"Memory is for long-lived knowledge only: a project convention, an architecture or tooling decision, a non-obvious constraint you verified, or a preference the user stated. " +
	"Check the index first — when an entry already covers the fact, update it with edit_memory instead of saving a near-duplicate. " +
	"Pass topic (lowercase letters, digits, dashes) to file the entry into a topic file and keep only a one-line pointer in the always-loaded index — use a topic for anything beyond a one-liner. " +
	"Pass type to tag the entry's kind (user, feedback, project, reference). " +
	"Never save task progress, speculation, transcript recaps, or secrets. " +
	"Saves should be rare and concise; repeats deduplicate and report \"already known\"."

var toolSaveMemoryText = Register(Text{ID: "tool.save_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolSaveMemoryDescription, MentionsTools: []string{"save_memory", "edit_memory"}, AllowlistCtx: "main-loop"})

// recall_memory (orchestrator/memory.go, Experience Overhaul B3): read one topic
// file whose pointer appears in the always-loaded index, instead of guessing its
// contents. This is the on-demand half of the index-plus-topics design.
const ToolRecallMemoryDescription = "Read one project-memory topic file. " +
	"The always-loaded memory index marks available topics as \"- [topic] description\" lines — call this when a topic's description matches the task, instead of guessing what it contains. " +
	"Returns the topic's full saved text; entries may reference related topics as [[topic]] links, which you recall the same way. " +
	"Only topics that appear in the index exist."

var toolRecallMemoryText = Register(Text{ID: "tool.recall_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolRecallMemoryDescription, MentionsTools: []string{"recall_memory"}, AllowlistCtx: "main-loop"})

// edit_memory (orchestrator/memory.go, Memory Parity N2): the update/correct/
// consolidate half of durable memory. save_memory is append-only, so this is
// the only path for fixing a stale entry, deleting a wrong one, or thinning an
// over-full topic — the maintenance discipline Claude-style memory expects.
const ToolEditMemoryDescription = "Update or delete one saved project-memory entry. " +
	"Pass enough of the entry's exact line as old to match exactly one bullet; pass the corrected entry as new, or an empty new to delete it. " +
	"Omit topic to edit the always-loaded index (standing facts and \"- [topic] …\" pointers); pass a topic slug to edit that topic file. " +
	"Use it to keep memory truthful: update an entry the moment you verify it changed, delete one proven wrong, and consolidate a topic that grew stale or full. " +
	"Deleting a topic's last entry removes the topic file and its index pointer."

var toolEditMemoryText = Register(Text{ID: "tool.edit_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolEditMemoryDescription, MentionsTools: []string{"edit_memory"}, AllowlistCtx: "main-loop"})

// ToolRunSubagentRolePropertyDescription teaches the model to NAME each
// dispatch for the job it does. The role is display-and-handoff only: the
// capability class, the cache pin, the stable system message, and the context
// record's kind all stay keyed on `agent`, so a novel role name can never
// fragment the provider cache.
const ToolRunSubagentRolePropertyDescription = "What this agent IS for this task, in 2-4 words — \"auth-flow-mapper\", \"settings-page-builder\", \"migration-reviewer\". Name it for the job, never a generic label. Expected on every dispatch."

var toolRunSubagentRolePropertyText = Register(Text{
	ID: "tool.run_subagent.role-property", Audience: MainStatic, Cache: Prefix, Body: ToolRunSubagentRolePropertyDescription,
})
