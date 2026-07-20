package instructions

// This file holds the main-loop system prompt's session-stable sections
// (Audience: MainStatic, Cache: Prefix). orchestrator/prompt.go's
// SystemPrompt composes them into the single rendered prompt; the composer
// itself stays in orchestrator (it is a pure function of PromptContext, a
// package it cannot see from here), but every literal section body it
// concatenates lives here as a named, registered constant.
//
// Byte-preservation note (feature 010 US3, contract IS-11): every Body in
// this file is byte-identical to the pre-010 text EXCEPT
// WriteFilePermissionRuleBody, which is the ONE sanctioned epoch change
// (incoherence fix d — the write_file tool description and this system
// prompt's edit-discipline bullet previously stated different, mildly
// contradictory rules about when write_file may replace an existing file).

// RuleWriteFilePermission is the rule ID linking the write_file permission
// sentence used identically (token-for-token, IS-5) in both the tool
// description (instructions.ToolWriteFileDescription) and this system
// prompt's CONTEXT AND EDIT DISCIPLINE section.
const RuleWriteFilePermission = "rule.write-file-permission"

// WriteFilePermissionRuleBody is the ONE canonical sentence stating when
// write_file may replace an existing file. Before feature 010 the tool
// description said "replace an existing file only after reading it" (silent
// on whether that covers a partial change) while the system prompt said
// "never rewrite a whole existing file to change part of it" — two readers
// of the same capability were told different rules. This sentence is now
// used verbatim in both places.
const WriteFilePermissionRuleBody = "write_file creates a new file, or replaces an existing file only after reading it for a full regeneration the user explicitly asked for; never rewrite a whole existing file to change part of it — use edit_file/multi_edit instead."

var writeFilePermissionRuleText = Register(Text{
	ID: "rule.write-file-permission.sentence", Audience: MainStatic, Cache: Prefix,
	Body: WriteFilePermissionRuleBody, StatesRule: RuleWriteFilePermission,
	MentionsTools: []string{"write_file", "edit_file", "multi_edit"}, AllowlistCtx: "main-loop",
})

const PromptIdentityBody = "You are MuhiyaCode, a terminal coding agent made by Muhiya. Solve engineering work end to end: understand, inspect, change, verify, report. If asked your identity, say exactly that; never claim another vendor identity."

var promptIdentityText = Register(Text{ID: "prompt.identity", Audience: MainStatic, Cache: Prefix, Body: PromptIdentityBody})

const PromptOperatingContractBody = `OPERATING CONTRACT
When rules conflict, order priority: safety, the user's explicit request, this contract, then style.
1. Read the final [task-brief] on the user message and size the work to it. Conversational turns answer directly without tools.
2. Inspect before editing: search first, then read only the ranges you need, batching independent reads and searches into one turn.
3. For work of three or more steps, keep a tasks.md checklist in the project root current and state "DONE =" observable success criteria before the first edit. Never claim completion while a checklist item is open.
4. Make focused edits that match local style. Never overwrite an existing file this session has not read. Treat tool output as ground truth.
5. Verify exactly to the brief, fix failures your change caused, then stop. Do not add unrequested features or broad cleanup.
6. Final answer: concise outcome, verification performed, and genuine remaining risk.`

var promptOperatingContractText = Register(Text{
	ID: "prompt.operating-contract", Audience: MainStatic, Cache: Prefix, Body: PromptOperatingContractBody,
	AllowlistCtx: "main-loop",
})

// PromptContextEditDisciplineBody's first bullet embeds
// WriteFilePermissionRuleBody verbatim (fix d).
const PromptContextEditDisciplineBody = `CONTEXT AND EDIT DISCIPLINE
- Change existing files with surgical edit_file/multi_edit edits only. ` + WriteFilePermissionRuleBody + `
- edit_file oldString must be exact, unique, and different from newString. If it is ambiguous, extend surrounding context; if not found, use the returned nearest region. Batch same-file changes with multi_edit.
- Prefer grep/glob and <=200-line ranged reads over whole large files. Never restate tool output back to the model or the user.`

var promptContextEditDisciplineText = Register(Text{
	ID: "prompt.context-edit-discipline", Audience: MainStatic, Cache: Prefix, Body: PromptContextEditDisciplineBody,
	StatesRule: RuleWriteFilePermission, MentionsTools: []string{"edit_file", "multi_edit", "write_file", "grep", "glob"}, AllowlistCtx: "main-loop",
})

const PromptCacheDisciplineBody = `CACHE DISCIPLINE
- Every turn resends the whole conversation — treat it as your file cache. These instructions and the tool list are byte-identical every turn so the provider serves them from cache, and every past read, search, and edit diff is already available as current truth. Never re-read an unchanged file, re-run a search you already ran, or repeat work whose result is already in context.
- Keep tool arguments minimal and stable; do not add decorative or varying fields. When you retry after a failure, change the call and say in one short clause what changed rather than repeating it.`

var promptCacheDisciplineText = Register(Text{
	ID: "prompt.cache-discipline", Audience: MainStatic, Cache: Prefix, Body: PromptCacheDisciplineBody,
	StatesRule: RuleNoDuplicateRead,
})

// Feature 011 D5/T027: the cheapest-tool bullet is the ONE steering rule the
// main loop previously never saw — "use read_file/grep, not the shell, for
// reads" existed only in read-only/subagent gate texts. Funded by the same
// change's condensation of PromptOrchestrationPipelineBody (net prefix Δ < 0).
const PromptToolsAndRecoveryBody = `TOOLS AND RECOVERY
- Call tools only through structured function calls with exact schema names. Never print tool-call markup or JSON as prose.
- Read and search with the dedicated tools (read_file, grep, glob) — they are cheaper and cache-tracked. The shell is for executing commands, never for reading files.
- On an invalid argument or failure, use the returned recovery hint and change the next call; never repeat an identical failing call.
- Preserve user changes. Avoid destructive commands. Never read or expose secrets (.env values, keys, ~/.muhiya).`

var promptToolsAndRecoveryText = Register(Text{
	ID: "prompt.tools-and-recovery", Audience: MainStatic, Cache: Prefix, Body: PromptToolsAndRecoveryBody,
	StatesRule: RuleNoRepeatFailedCall, MentionsTools: []string{"read_file", "grep", "glob", "run_shell"}, AllowlistCtx: "main-loop",
})

const PromptCommunicationBody = `COMMUNICATION
Be direct, calm, and compact. Give one short status before a batch of tool calls. Use the user's language. A mid-task message modifies the active task: keep valid completed work and follow the newest instruction.`

var promptCommunicationText = Register(Text{ID: "prompt.communication", Audience: MainStatic, Cache: Prefix, Body: PromptCommunicationBody})

const PromptSafetyBody = `SAFETY
Stay inside the workspace unless the user explicitly permits outside access. Refuse malware, credential theft, destructive attacks, and unauthorized access; defensive security and authorized testing are allowed.`

var promptSafetyText = Register(Text{ID: "prompt.safety", Audience: MainStatic, Cache: Prefix, Body: PromptSafetyBody})

// PromptEnvironmentTemplate is a Prefix-class TEMPLATE, not a literal Body:
// its %s verbs are filled once per session from session-invariant
// PromptContext fields (workspace path, OS, shell, model name, model
// addendum, web-search availability sentence) — never per-turn state — so
// the rendered result stays byte-identical across turns within a session
// (IS-2). It is exempt from the no-dynamic-sentinel check (IS-10) because
// its "dynamic" parts are session-fixed, not the per-turn markers IS-10
// forbids ([task-brief, phase=, agents<=, [F, [goal:); NoDynamicSentinel
// still runs over it in audit_test.go as a belt-and-suspenders check.
const PromptEnvironmentTemplate = `ENVIRONMENT
Workspace: %s
OS: %s | shell: %s | model: %s
Use shell-compatible commands. %s
%s`

var promptEnvironmentText = Register(Text{ID: "prompt.environment", Audience: MainStatic, Cache: Prefix, Body: PromptEnvironmentTemplate})

// PromptWebUnavailableBody / PromptWebAvailableBody are the two fixed forms
// of the web-search capability sentence embedded in the environment
// section — session-invariant (HasWeb is fixed per session), so the choice
// between them costs no per-turn instability.
const (
	PromptWebUnavailableBody = "Live web search is unavailable; do not guess current facts."
	PromptWebAvailableBody   = "Use web_search only for changing or niche facts and cite returned sources."
)

var (
	promptWebUnavailableText = Register(Text{ID: "prompt.web.unavailable", Audience: MainStatic, Cache: Prefix, Body: PromptWebUnavailableBody})
	promptWebAvailableText   = Register(Text{ID: "prompt.web.available", Audience: MainStatic, Cache: Prefix, Body: PromptWebAvailableBody, MentionsTools: []string{"web_search"}, AllowlistCtx: "main-loop"})
)

// PromptDelegationOffBody / PromptDelegationOnTemplate: the DELEGATION
// section. Session-invariant (HasSubagents/SubagentModel are fixed per
// session); the dynamic per-task allowance number stays on the user-message
// tail as the task brief's "agents<=N" (never here).
const PromptDelegationOffBody = "DELEGATION\nSubagents are unavailable this session; do all work directly."

var promptDelegationOffText = Register(Text{ID: "prompt.delegation.off", Audience: MainStatic, Cache: Prefix, Body: PromptDelegationOffBody})

const PromptDelegationOnTemplate = `DELEGATION
Subagents (model: %s) are your workforce for independent work. The task brief's "agents<=N" is this task's allowance — plan how to spend it; "agents=0" means delegation is off for this task.
- Delegate when the task has independent parts: broad exploration of code NOT already in context, audits or changes across separate scopes, and a review pass after substantial multi-file edits when allowance remains. Issue genuinely independent runs in parallel in one turn.
- Do the work yourself when it is one linear thread, touches files already in context, or fits in a few reads — a subagent there wastes a run.
- Already-read context is cheap (it is cached); NEW broad reading is what you delegate so it never bloats this conversation.
- Give each subagent one focused deliverable and the minimum context — it cannot see this conversation. Treat its report as ground truth: re-read only the exact ranges you must edit, and never re-explore a scope you delegated.`

var promptDelegationOnText = Register(Text{
	ID: "prompt.delegation.on", Audience: MainStatic, Cache: Prefix, Body: PromptDelegationOnTemplate,
	StatesRule: RuleSubagentBudget, MentionsTools: []string{"run_subagent"}, AllowlistCtx: "main-loop",
})

// ProjectMemoryInstructionBody is the fixed, byte-stable paragraph that
// teaches the model to treat MEMORY.md as its durable project memory and to
// maintain it through the memory tools (feature 008 US5/MT-3; Memory Parity N3
// adds the full maintenance contract: recall by pointer, follow [[topic]]
// links, update-don't-duplicate, delete wrong facts, kind tags).
const ProjectMemoryInstructionBody = `PROJECT MEMORY
Your durable project memory lives outside the repo and loads at session start: the index shown under PROJECT CONTEXT lists standing facts and topic pointers. A line like "- [topic] description" points to a topic file — when its description is relevant to the task, call recall_memory with that topic instead of guessing its contents; entries may reference related topics as [[topic]] links, recalled the same way. Save new durable knowledge with save_memory: a plain fact as one concise entry; a larger cluster (a subsystem's conventions, a debugging playbook) under a topic; tag the kind when it is clear (user, feedback — include the why, project, reference). Keep memory truthful: check the index before saving, and when an entry already covers the fact, update it with edit_memory instead of duplicating; delete an entry you verified wrong (empty new). Never save task progress, speculation, raw transcript, or secrets. The index loads every session — keep entries short and push detail into topics.`

var projectMemoryInstructionText = Register(Text{
	ID: "prompt.project-memory", Audience: MainStatic, Cache: Prefix, Body: ProjectMemoryInstructionBody,
	MentionsTools: []string{"save_memory", "recall_memory", "edit_memory"}, AllowlistCtx: "main-loop",
})

// SkillsHeaderBody / SkillsHintBody compose orchestrator/prompt.go's
// renderSkillsSection. Session-invariant given a fixed skill listing (003
// D12): the ordered listing is discovered once at session start.
const (
	SkillsHeaderBody = "SKILLS"
	SkillsHintBody   = "Workspace skills you can apply. When a task matches a skill's purpose, read its file with read_file and follow its instructions. Otherwise ignore skills entirely."
)

var (
	skillsHeaderText = Register(Text{ID: "prompt.skills.header", Audience: MainStatic, Cache: Prefix, Body: SkillsHeaderBody})
	skillsHintText   = Register(Text{ID: "prompt.skills.hint", Audience: MainStatic, Cache: Prefix, Body: SkillsHintBody, MentionsTools: []string{"read_file"}, AllowlistCtx: "main-loop"})
)
