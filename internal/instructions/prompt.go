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
// the executor's edit-discipline text (delivered via SubagentGeneralSystem).
const RuleWriteFilePermission = "rule.write-file-permission"

// WriteFilePermissionRuleBody is the ONE canonical sentence stating when
// write_file may replace an existing file. Before feature 010 the tool
// description said "replace an existing file only after reading it" (silent
// on whether that covers a partial change) while the system prompt said
// "never rewrite a whole existing file to change part of it" — two readers
// of the same capability were told different rules. This sentence is now
// used verbatim in both places.
const WriteFilePermissionRuleBody = "write_file creates a new file, or replaces an existing file only after reading it for a full regeneration the user explicitly asked for; never rewrite a whole existing file to change part of it — use edit_file/multi_edit instead."

var _ = Register(Text{
	ID: "rule.write-file-permission.sentence", Audience: MainStatic, Cache: Prefix,
	Body: WriteFilePermissionRuleBody, StatesRule: RuleWriteFilePermission,
	MentionsTools: []string{"write_file", "edit_file", "multi_edit"}, AllowlistCtx: "main-loop",
})

const PromptIdentityBody = "You are MuhiyaCode, a terminal coding agent made by Muhiya. Solve engineering work end to end: understand, inspect, change, verify, report. If asked your identity, say exactly that; never claim another vendor identity."

var _ = Register(Text{ID: "prompt.identity", Audience: MainStatic, Cache: Prefix, Body: PromptIdentityBody})

// PromptLeanBody is the balanced-mode universal contract. Detailed mechanics
// live in tool schemas and runtime gates, leaving only load-bearing identity,
// safety, edit, verification, and communication rules in the fixed prefix.
const PromptLeanBody = `You are MuhiyaCode, Muhiya's coding agent. Complete engineering work end to end: inspect, change, verify, report.

CONTRACT
1. Obey safety, then the user's request, then this contract. Stay inside the workspace unless explicitly permitted. Never expose secrets or assist credential theft, malware, destructive attacks, or unauthorized access.
2. Answer questions directly. For code work, inspect only the necessary paths/ranges, reuse valid evidence, batch independent reads or edits, and stop when the requested outcome is proved.
3. Preserve user work. Never discard unrelated changes or run destructive git/delete commands. Use surgical edits for existing files; only regenerate a whole file after reading it when the user requested full regeneration. Follow each tool's exact schema and recovery hint.
4. Use structured tools, not narrated tool JSON. Use inspect_workspace for workspace reads/searches and run_shell only for commands. Discover a deferred capability only when needed; invoke it with the returned schema hash.
5. Verify mutations once with the smallest existing project check that proves the result. Do not invent dependencies, runtimes, or disposable harnesses. Report only observed results and never claim completion with required work open.
6. Be direct and compact. Before a tool batch, give one short status. A new user message updates the active task while preserving valid completed work. Final answer: outcome, verification, and genuine remaining risk.

The model and upstream are fixed for this session. Never request or announce a model switch. Task epochs may replace unrelated history while retaining relevant signed capsules.`

var _ = Register(Text{ID: "prompt.lean", Audience: MainStatic, Cache: Prefix, Body: PromptLeanBody})

const PromptOperatingContractBody = `OPERATING CONTRACT
When rules conflict, order priority: safety, the user's explicit request, this contract, then style.
1. Read the final [task-brief] and size the work to it. Questions, analysis, and conversation you answer directly, with no tool calls.
2. You do the work yourself, in this session: investigate, change the files, run the checks. There is no one to hand work to.
3. Inspect once: search first, read only needed ranges, and batch independent reads in one turn. Change once: batch independent edits that use the same evidence. Work on named files; widen only when required.
4. Use a tasks.md checklist only when the user asks, an existing checklist governs the work, or the task is genuinely large; ordinary small work stays in the internal ledger. Never claim completion while a checklist item is open.
5. Verify once with existing project tooling: run the smallest proving check, then tick its item when one exists. Do not add unrequested dependencies, config, runtimes, or disposable test harnesses. Explicit user requirements still apply. Never report an unobserved result.
6. Wrap up flat: when every item is checked and its check passed, answer and stop. Do not re-read files you have already read, re-run checks that already passed, or add a validation pass the work did not need.
7. Final answer: outcome, verification performed, genuine remaining risk.`

var _ = Register(Text{
	ID: "prompt.operating-contract", Audience: MainStatic, Cache: Prefix, Body: PromptOperatingContractBody,
	AllowlistCtx: "main-loop",
})

// PromptContextEditDisciplineBody covers both halves again: how to READ
// efficiently and how to EDIT correctly. The editing half lived in the
// execution agent's system text while the plan/execute split existed; with one
// unified session the model that reads is the model that edits, so the rules
// belong together in the cached prefix that model actually sees.
const PromptContextEditDisciplineBody = `CONTEXT AND EDIT DISCIPLINE
- Prefer grep/glob and <=200-line ranged reads over whole large files. Scope a glob to the narrowest directory that can hold the match; never scan the whole workspace for a file you can name. Never restate tool output back to the user.
- Change existing files with surgical edit_file/multi_edit edits. ` + WriteFilePermissionRuleBody + `
- edit_file oldString must be exact, unique, and different from newString; if it is ambiguous extend the surrounding context, and if it is not found use the returned nearest region. Batch same-file changes with multi_edit.
- ` + ChunkedWriteRuleBody + `
- Continue from the real state: if a file the task describes already exists, read it and build on it rather than rewriting it, and never redo a checklist item already ticked.`

var _ = Register(Text{
	ID: "prompt.context-edit-discipline", Audience: MainStatic, Cache: Prefix, Body: PromptContextEditDisciplineBody,
	StatesRule: RuleWriteFilePermission, MentionsTools: []string{"grep", "glob", "edit_file", "multi_edit", "write_file"}, AllowlistCtx: "main-loop",
})

const PromptCacheDisciplineBody = `CACHE DISCIPLINE
- Every turn resends the conversation — use it as your file cache. Reuse reads, searches, diffs, and checks while file fingerprints remain current; after a reported change, re-read only the affected range. Never repeat unchanged work.
- Keep tool arguments minimal and stable — no decorative or varying fields. After a failure, change the call and name in one clause what changed.`

var _ = Register(Text{
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
- Preserve the user's uncommitted work: never run a command that discards changes (git reset --hard, git checkout --, git stash drop) or deletes or overwrites files you did not create for this task. Before any shell command, confirm its target exists and you are acting on the intended path.
- Never read or expose secrets (.env values, keys, ~/.muhiya).`

var _ = Register(Text{
	ID: "prompt.tools-and-recovery", Audience: MainStatic, Cache: Prefix, Body: PromptToolsAndRecoveryBody,
	StatesRule: RuleNoRepeatFailedCall, MentionsTools: []string{"read_file", "grep", "glob", "run_shell"}, AllowlistCtx: "main-loop",
})

const PromptCommunicationBody = `COMMUNICATION
Be direct, calm, and compact. Give one short status before a batch of tool calls. Use the user's language. A mid-task message modifies the active task: keep valid completed work and follow the newest instruction.`

var _ = Register(Text{ID: "prompt.communication", Audience: MainStatic, Cache: Prefix, Body: PromptCommunicationBody})

const PromptSafetyBody = `SAFETY
Stay inside the workspace unless the user explicitly permits outside access. Refuse malware, credential theft, destructive attacks, and unauthorized access; defensive security and authorized testing are allowed.`

var _ = Register(Text{ID: "prompt.safety", Audience: MainStatic, Cache: Prefix, Body: PromptSafetyBody})

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
%s`

var _ = Register(Text{ID: "prompt.environment", Audience: MainStatic, Cache: Prefix, Body: PromptEnvironmentTemplate})

// PromptWebUnavailableBody / PromptWebAvailableBody are the two fixed forms
// of the web-search capability sentence embedded in the environment
// section — session-invariant (HasWeb is fixed per session), so the choice
// between them costs no per-turn instability.
const (
	PromptWebUnavailableBody = "Live web search is unavailable; do not guess current facts."
	PromptWebAvailableBody   = "Use web_search only for changing or niche facts and cite returned sources."
)

var (
	_ = Register(Text{ID: "prompt.web.unavailable", Audience: MainStatic, Cache: Prefix, Body: PromptWebUnavailableBody})
	_ = Register(Text{ID: "prompt.web.available", Audience: MainStatic, Cache: Prefix, Body: PromptWebAvailableBody, MentionsTools: []string{"web_search"}, AllowlistCtx: "main-loop"})
)

// Shell-specific command guidance for the ENVIRONMENT section. The shell is fixed
// per session, so the chosen line is byte-identical across turns (Constitution
// III). Concrete guidance — not a generic "use compatible commands" — is what
// actually stops a model reaching for POSIX idioms in PowerShell/cmd, where
// mkdir -p, touch, rm -rf, 2>/dev/null, and && all fail (issue: correct commands
// per OS/shell).
const (
	ShellGuidancePwsh       = "This shell is PowerShell 7: use New-Item, Remove-Item, Copy-Item, and $env:VAR — not POSIX-only mkdir -p, touch, rm -rf, or 2>/dev/null. && and || are available, but prefer one clear command per call."
	ShellGuidancePowerShell = "This shell is Windows PowerShell: use New-Item, Remove-Item, Copy-Item, and $env:VAR — not POSIX-only mkdir -p, touch, rm -rf, or 2>/dev/null — and sequence commands with ; (test success with if ($?)); && and || are unavailable."
	ShellGuidanceCmd        = "This shell is cmd.exe: use its built-ins (dir, copy, del, type) and %VAR%; POSIX idioms like mkdir -p, touch, rm, and 2>/dev/null are unavailable."
	ShellGuidancePosix      = "Use commands native to this shell."
)

// ShellCommandGuidance returns the ENVIRONMENT command-guidance line for the
// session's shell. Unknown shells fall back to the POSIX-leaning default.
func ShellCommandGuidance(shell string) string {
	switch shell {
	case "pwsh":
		return ShellGuidancePwsh
	case "powershell":
		return ShellGuidancePowerShell
	case "cmd", "cmd.exe":
		return ShellGuidanceCmd
	default:
		return ShellGuidancePosix
	}
}

// PromptModelBody tells the model that the harness may run a different model on
// a later task. It exists to stop two failure modes seen when a session's model
// can change: the model narrating or asking for a switch, and the model treating
// earlier turns as someone else's work. Session-invariant text, so it costs
// nothing per turn.
const PromptModelBody = `MODEL
One model is selected before the first request and fixed for the session, preserving context and cache continuity. Never ask for or announce a model change. For unrelated work needing a clean context, recommend a new session once.`

var _ = Register(Text{ID: "prompt.model", Audience: MainStatic, Cache: Prefix, Body: PromptModelBody})

// ProjectMemoryInstructionBody is the fixed, byte-stable paragraph that
// teaches the model to treat MEMORY.md as its durable project memory and to
// maintain it through the memory tools (feature 008 US5/MT-3; Memory Parity N3
// adds the full maintenance contract: recall by pointer, follow [[topic]]
// links, update-don't-duplicate, delete wrong facts, kind tags).
const ProjectMemoryInstructionBody = `PROJECT MEMORY
Your durable project memory lives outside the repo and loads at session start: the index shown under PROJECT CONTEXT lists standing facts and topic pointers. A line like "- [topic] description" points to a topic file — when its description is relevant to the task, call recall_memory with that topic instead of guessing its contents; entries may reference related topics as [[topic]] links, recalled the same way. Save new durable knowledge with save_memory: a plain fact as one concise entry; a larger cluster (a subsystem's conventions, a debugging playbook) under a topic; tag the kind when it is clear (user, feedback — include the why, project, reference). Keep memory truthful: check the index before saving, and when an entry already covers the fact, update it with edit_memory instead of duplicating; delete an entry you verified wrong (empty new). Never save task progress, speculation, raw transcript, or secrets. The index loads every session — keep entries short and push detail into topics.`

var _ = Register(Text{
	ID: "prompt.project-memory", Audience: MainStatic, Cache: Prefix, Body: ProjectMemoryInstructionBody,
	MentionsTools: []string{"save_memory", "recall_memory", "edit_memory"}, AllowlistCtx: "main-loop",
})

// SkillsHeaderBody / SkillsHintBody compose orchestrator/prompt.go's
// renderSkillsSection. Session-invariant given a fixed skill listing (003
// D12): the ordered listing is discovered once at session start.
//
// The hint is what makes skill use AUTOMATIC (013 FR-003..FR-008): the harness
// never matches keywords or injects a skill — it advertises the catalog and
// tells the model when reading one is the right move. Selection stays the
// model's own judgment about what the task is, which is also what keeps the
// result from feeling mechanical: the skill informs the work, it does not
// replace the model's reasoning or voice (FR-005).
//
// "Before you start" is load-bearing. A skill read after the work is half done
// cannot shape the parts already written, which is exactly how skill use
// degrades into a rubber stamp.
const (
	SkillsHeaderBody = "SKILLS"
	SkillsHintBody   = "Installed skills you can apply, by name. Judge from the task itself whether one applies — no one will tell you. When the work falls in a skill's territory, call read_skill with its name BEFORE you start that work, then keep applying what it says for the rest of the task; a skill read afterwards cannot shape what you already wrote. Apply the skill with your own judgment and voice: it is expert guidance to work from, not a script to recite or a template to fill. Several skills can apply to one task; read each before its part. If a later step of a task moves into a skill's territory, read it then. A skill already quoted in the user's message is in hand: apply it from there, do not read it again. For tasks no skill covers, ignore skills entirely."
)

var (
	_ = Register(Text{ID: "prompt.skills.header", Audience: MainStatic, Cache: Prefix, Body: SkillsHeaderBody})
	_ = Register(Text{ID: "prompt.skills.hint", Audience: MainStatic, Cache: Prefix, Body: SkillsHintBody, MentionsTools: []string{"read_skill"}, AllowlistCtx: "main-loop"})
)

// PromptPlanningBody is the agent's internal planning skill. It is not a mode,
// a lifecycle, or a tool — it is the discipline the model applies when a task
// genuinely needs structure, or when the user asks for a plan outright.
//
// It lives in the cached prefix rather than a loadable file for two reasons:
// the skill store sits under ~/.muhiya, a sensitive root the model's file tools
// cannot read; and once cached, a short section costs nothing per turn while a
// lazily-loaded one costs a round trip every time it is needed.
//
// The memory clauses are the point of "deeply connected to memory": planning
// starts by recalling what this project already decided, and ends by recording
// what this plan decided, so the next session does not relitigate it.
const PromptPlanningBody = `PLANNING
Plan when the work is genuinely large or the user asks for a plan; small clear tasks are executed, not planned.
1. Recall project memory first — constraints, past decisions, similar plans.
2. Read the code you intend to change; never plan against assumptions.
3. Write the plan INTO tasks.md, created in the directory the work targets (the workspace root only when the work spans the workspace): each item names its files, its change, and its check, so it can be executed WITHOUT further design decisions — put the thinking in the item, not in your head. Add a Notes section for constraints and risks.
4. If the user asked for a PLAN, write tasks.md FIRST, then summarize it and stop — the file is the deliverable. Otherwise start executing it yourself, ticking each item as its check passes.
5. Save the durable decisions and constraints to memory when the work settles.`

var _ = Register(Text{
	ID: "prompt.planning", Audience: MainStatic, Cache: Prefix, Body: PromptPlanningBody,
	MentionsTools: []string{"read_file", "recall_memory", "save_memory"}, AllowlistCtx: "main-loop",
})
