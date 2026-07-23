package instructions

// Tool descriptions: the top-level natural-language description shown to
// the model for each live tool (12 workspace registry tools wired by
// internal/workspace/registry.go, the synthetic session tools wired by
// internal/orchestrator's sessionDefinitions — ask_user, propose_changes,
// save_memory, recall_memory, edit_memory, read_skill — and web_search wired
// by internal/gateway/web.go). All Prefix-class: they are part of the
// session-stable tool JSON block.
//
// Property-level (per-field) schema description strings — short UI hints
// like "Directory, default ." — are deliberately NOT individually registered
// here: they are not natural-language rules subject to the
// contradiction/capability-reference/stated-in-advance audits this package
// exists to run, and registering several dozen one-line hints would dilute
// the review surface without adding audit value. They stay inline at their
// call sites in workspace/registry.go and orchestrator/engine.go.
const (
	ToolListFilesDescription  = "List a directory. Use once to map a workspace."
	ToolReadFileDescription   = "Read a UTF-8 file with line numbers. Use offset/limit for large files."
	ToolGrepDescription       = "Regex/literal search with file, line, and text results. Patterns are Go RE2: lookaround ((?= and (?!) and backreferences do not exist — express the intent another way, or set literal=true for exact text."
	ToolSearchTextDescription = "Fast literal text search; prefer grep for regex."
	// TD03: the example is directory-scoped on purpose. The old one ("**/*.go")
	// taught a whole-workspace scan as the default shape, and that is exactly what
	// a live session did for a single-file task.
	ToolGlobDescription      = "Find files by doublestar glob, scoped to the narrowest directory that can hold them, e.g. src/**/*.go. Prefer a directory prefix over a bare **/ scan."
	ToolEditFileDescription  = "Replace exact text in a file already read. Returns a compact diff. oldString must match exactly once; if it is not found the result names the nearest region — retry from that, adding surrounding lines to disambiguate a repeated match."
	ToolMultiEditDescription = "Apply several ordered exact replacements to one read file."
	// ToolWriteFileDescription is exactly WriteFilePermissionRuleBody
	// (fix d, IS-5): token-identical to the write_file sentence in the
	// system prompt's CONTEXT AND EDIT DISCIPLINE section.
	ToolWriteFileDescription  = WriteFilePermissionRuleBody
	ToolApplyPatchDescription = "Apply a standard unified diff to files already read."
	// Feature 011 D5/T027: the trailing steering sentence keeps the shell for
	// executing, not reading — read_file/grep are cheaper and cache-tracked.
	ToolRunShellDescription  = "Run a shell command in the workspace with streaming output and cancellation. Each call runs fresh at the workspace root; a cd affects only that one command, so combine cd and the command in a single call. For reading or searching files use read_file/grep instead — they are cheaper and cache-tracked."
	ToolGitStatusDescription = "Show concise git status."
	ToolGitDiffDescription   = "Show the workspace git diff."
	ToolInspectCodeDescription = "Inspect Go source structure: outline (declarations), definition (symbol lookup), or references (usage sites). Returns compact signatures with line numbers — no bodies."
)

var (
	_ = Register(Text{ID: "tool.list_files.desc", Audience: MainStatic, Cache: Prefix, Body: ToolListFilesDescription})
	_ = Register(Text{ID: "tool.read_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolReadFileDescription})
	_ = Register(Text{ID: "tool.grep.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGrepDescription})
	_ = Register(Text{ID: "tool.search_text.desc", Audience: MainStatic, Cache: Prefix, Body: ToolSearchTextDescription})
	_ = Register(Text{ID: "tool.glob.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGlobDescription})
	_ = Register(Text{ID: "tool.edit_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolEditFileDescription})
	_ = Register(Text{ID: "tool.multi_edit.desc", Audience: MainStatic, Cache: Prefix, Body: ToolMultiEditDescription})
	_ = Register(Text{
		ID: "tool.write_file.desc", Audience: MainStatic, Cache: Prefix, Body: ToolWriteFileDescription,
		StatesRule: RuleWriteFilePermission, MentionsTools: []string{"write_file", "edit_file", "multi_edit"}, AllowlistCtx: "main-loop",
	})
	_ = Register(Text{ID: "tool.apply_patch.desc", Audience: MainStatic, Cache: Prefix, Body: ToolApplyPatchDescription})
	_ = Register(Text{ID: "tool.run_shell.desc", Audience: MainStatic, Cache: Prefix, Body: ToolRunShellDescription})
	_ = Register(Text{ID: "tool.git_status.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGitStatusDescription})
	_ = Register(Text{ID: "tool.git_diff.desc", Audience: MainStatic, Cache: Prefix, Body: ToolGitDiffDescription})
	_ = Register(Text{ID: "tool.inspect_code.desc", Audience: MainStatic, Cache: Prefix, Body: ToolInspectCodeDescription})
)

// Synthetic main-loop tool descriptions (engine.go sessionDefinitions).
const (
	ToolAskUserDescription        = "Ask 1-3 blocking multiple-choice questions."
	ToolProposeChangesDescription = "Request approval before broad or risky edits."
)

var (
	_ = Register(Text{ID: "tool.ask_user.desc", Audience: MainStatic, Cache: Prefix, Body: ToolAskUserDescription})
	_ = Register(Text{ID: "tool.propose_changes.desc", Audience: MainStatic, Cache: Prefix, Body: ToolProposeChangesDescription})
)

// read_skill (orchestrator/skills_tool.go) is how an advertised skill's full
// instructions reach the model (013 US1). It takes a NAME from the SKILLS
// listing, never a path — that is what lets skills installed outside the
// workspace be readable at all without widening file-tool containment.
//
// The description repeats the timing rule from the SKILLS hint on purpose: tool
// descriptions are the surface a model consults at the moment it decides to
// call, so "before you start" has to be legible right here too.
const (
	ToolReadSkillDescription = "Read one installed skill's full instructions by name, from the SKILLS list. " +
		"Call this BEFORE starting work that falls in that skill's territory, then apply what it says for the rest of the task. " +
		"Names come from the SKILLS list; there are no paths and no other skill sources."
	ToolReadSkillNamePropertyDescription = "The skill's name exactly as it appears in the SKILLS list."
	// Failure bodies. Each names the one thing the model should do next, and
	// none disclose a filesystem path (skills live outside the workspace).
	ToolReadSkillEmptyNameBody       = "read_skill needs a name from the SKILLS list."
	ToolReadSkillUnknownTmpl         = "unknown skill %q — only the skills listed under SKILLS are available; check the exact name there."
	ToolReadSkillUnreadableTmpl      = "skill %q could not be read (missing, unreadable, or over the size limit). Continue without it."
	ToolReadSkillEmptyBodyTmpl       = "skill %q has no instructions. Continue without it."
	ToolReadSkillAlreadyProvidedTmpl = "skill %q is already in this task's context — apply it from where it appears above rather than reading it again."
)

var (
	_ = Register(Text{
		ID: "tool.read_skill.desc", Audience: MainStatic, Cache: Prefix, Body: ToolReadSkillDescription,
		MentionsTools: []string{"read_skill"}, AllowlistCtx: "main-loop",
	})
	_ = Register(Text{ID: "tool.read_skill.name-property", Audience: MainStatic, Cache: Prefix, Body: ToolReadSkillNamePropertyDescription})
	_ = Register(Text{ID: "tool.read_skill.unknown", Audience: Gate, Cache: Sidecar, Body: ToolReadSkillUnknownTmpl, MentionsTools: []string{"read_skill"}, AllowlistCtx: "main-loop"})
	_ = Register(Text{ID: "tool.read_skill.already-provided", Audience: Gate, Cache: Sidecar, Body: ToolReadSkillAlreadyProvidedTmpl, MentionsTools: []string{"read_skill"}, AllowlistCtx: "main-loop"})
)

// web_search (gateway/web.go).
const ToolWebSearchDescription = "Search the live web for current or niche facts and return citable sources."

var _ = Register(Text{ID: "tool.web_search.desc", Audience: MainStatic, Cache: Prefix, Body: ToolWebSearchDescription, MentionsTools: []string{"web_search"}, AllowlistCtx: "main-loop"})

// save_memory (orchestrator/memory.go). The description carries the
// what-belongs-in-memory teaching (feature 008 MT-2): durable conventions,
// decisions, and user preferences — never task progress, speculation,
// transcript, or secrets — and that saves are rare and concise. Memory Parity
// N3 adds the check-before-save rule (update via edit_memory, never duplicate)
// and the optional kind tag.
// TG01: condensed from ~700 chars. Every RULE survives (long-lived knowledge
// only, check-before-save, topic filing, kind tag, the never-save list, rarity);
// the prose around them does not. Tool definitions are ~78% of the fixed prefix
// and ride every request of every session, so wording weight here is the most
// expensive prose in the product.
const ToolSaveMemoryDescription = "Save one durable project fact to project memory (loaded at session start). " +
	"Long-lived knowledge only: a convention, an architecture or tooling decision, a verified constraint, or a stated preference. " +
	"Check the index first — if an entry already covers it, update that with edit_memory instead of near-duplicating. " +
	"Pass topic (lowercase, digits, dashes) to file it into a topic file, leaving a one-line pointer in the index; use one for anything beyond a one-liner. " +
	"Pass type to tag its kind (user, feedback, project, reference). " +
	"Never save task progress, speculation, transcript recaps, or secrets. Saves are rare and concise; repeats report \"already known\"."

var _ = Register(Text{ID: "tool.save_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolSaveMemoryDescription, MentionsTools: []string{"save_memory", "edit_memory"}, AllowlistCtx: "main-loop"})

// recall_memory (orchestrator/memory.go, Experience Overhaul B3): read one topic
// file whose pointer appears in the always-loaded index, instead of guessing its
// contents. This is the on-demand half of the index-plus-topics design.
// TG01: condensed; the rules (index-listed topics only, recall instead of guess,
// follow [[topic]] links) are intact.
const ToolRecallMemoryDescription = "Read one project-memory topic file. " +
	"The index lists topics as \"- [topic] description\" — call this when a description matches the task instead of guessing its contents. " +
	"Returns the topic's full text; entries may link related topics as [[topic]], recalled the same way. Only indexed topics exist."

var _ = Register(Text{ID: "tool.recall_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolRecallMemoryDescription, MentionsTools: []string{"recall_memory"}, AllowlistCtx: "main-loop"})

// edit_memory (orchestrator/memory.go, Memory Parity N2): the update/correct/
// consolidate half of durable memory. save_memory is append-only, so this is
// the only path for fixing a stale entry, deleting a wrong one, or thinning an
// over-full topic — the maintenance discipline Claude-style memory expects.
// TG01: condensed; every rule (exact-line match, empty new deletes, index vs
// topic targeting, keep-memory-truthful, last-entry cleanup) is intact.
const ToolEditMemoryDescription = "Update or delete one saved project-memory entry. " +
	"Pass enough of its exact line as old to match one bullet; pass the correction as new, or an empty new to delete it. " +
	"Omit topic to edit the index (standing facts and \"- [topic] …\" pointers); pass a topic slug to edit that file. " +
	"Keep memory truthful: update an entry the moment you verify it changed, delete one proven wrong, consolidate a stale or full topic. " +
	"Deleting a topic's last entry removes the file and its pointer."

var _ = Register(Text{ID: "tool.edit_memory.desc", Audience: MainStatic, Cache: Prefix, Body: ToolEditMemoryDescription, MentionsTools: []string{"edit_memory"}, AllowlistCtx: "main-loop"})

// The run_subagent role-property text taught the model to name each dispatch
// for the job it did. It is gone with the tool it described.
