package orchestrator

import (
	"fmt"
	"strings"
)

// projectMemoryInstruction is the fixed, byte-stable paragraph that teaches the
// model to treat MEMORY.md as its durable, file-based project memory and to keep
// it current through ordinary file tools (feature 006). It replaces the old
// answer-trailer contract: no side-channel, no new tool, so the prefix stays lean.
const projectMemoryInstruction = `PROJECT MEMORY
MEMORY.md at the workspace root is your durable project memory across sessions. When it exists it is shown under PROJECT CONTEXT below — read it for standing project facts. When you learn something durable worth keeping (a convention, a non-obvious constraint, an architecture decision), record it by editing MEMORY.md with your normal file tools, creating it if absent, and update or remove entries that become wrong. Keep it short and factual — never task progress, speculation, raw transcript, or secrets — since it loads into context every session.`

type PromptContext struct {
	Workspace     string
	OS            string
	Shell         string
	Model         string
	ModelAddendum string
	HasWeb        bool
	HasSubagents  bool
	SubagentModel string
	// Skills is the session-pinned, deterministically ordered listing of
	// workspace-resident skills advertised in the stable prefix (003 D12). It is
	// discovered once at session start; identical configuration ⇒ byte-identical
	// section, so it never invalidates the prefix within a session.
	Skills []SkillListing
	// ProjectMemory enables the fixed, byte-stable project-memory instruction (006).
	// When true the prompt tells the model that MEMORY.md is its durable memory file
	// to read and keep current with its file tools. It carries no dynamic value.
	ProjectMemory bool
	// ProjectContextBlock is the exact rendered "## PROJECT CONTEXT" boot block
	// (005 US3), empty when the workspace has no instructions or memory. It is
	// computed once at session start and restored verbatim from the sidecar on
	// resume, so it is session-stable and rides the cache after one upgrade break.
	ProjectContextBlock string
}

// SkillListing is one advertised skill: a name, its workspace-relative SKILL.md
// path (forward slashes), and a single-line description (may be empty).
type SkillListing struct {
	Name        string
	Path        string
	Description string
}

// renderSkillsSection returns the deterministic ## Skills block, or "" when no
// skills are advertised. Order is caller-guaranteed stable; each line is
// byte-fixed for identical input (contract skills-delivery.md §2).
func renderSkillsSection(skills []SkillListing) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("SKILLS\n")
	b.WriteString("Workspace skills you can apply. When a task matches a skill's purpose, read its file with read_file and follow its instructions. Otherwise ignore skills entirely.")
	for _, skill := range skills {
		if skill.Description == "" {
			b.WriteString(fmt.Sprintf("\n- %s (%s)", skill.Name, skill.Path))
		} else {
			b.WriteString(fmt.Sprintf("\n- %s (%s): %s", skill.Name, skill.Path, skill.Description))
		}
	}
	return b.String()
}

// SystemPrompt is the single, session-stable instruction block. It is composed
// only from session-invariant facts (workspace, OS, shell, model,
// capabilities) so its serialized bytes never change between turns or tasks in
// a session. That byte-stability is what lets DeepSeek's implicit prefix cache
// hit on every request after the first. All per-turn state — task class,
// budgets, reasoning effort, active goal, plan mode — rides on the user message
// (see BudgetFor.Brief and the goal/plan blocks), never here.
func SystemPrompt(c PromptContext) string {
	web := "Live web search is unavailable; do not guess current facts."
	if c.HasWeb {
		web = "Use web_search only for changing or niche facts and cite returned sources."
	}
	agents := "Subagents are unavailable."
	if c.HasSubagents {
		agents = fmt.Sprintf("Subagents (%s): delegate only independent exploration, review, or isolated work that saves several main-context reads, and only when the task brief allows agent runs. Give each a focused task and the minimum context; expect one concise structured report back. Parallelize only genuinely independent runs.", c.SubagentModel)
	}
	base := strings.TrimSpace(fmt.Sprintf(`You are MuhiyaCode, a terminal coding agent made by Muhiya. Solve engineering work end to end: understand, inspect, change, verify, report. If asked your identity, say exactly that; never claim another vendor identity.

OPERATING CONTRACT
When rules conflict, order priority: safety, the user's explicit request, this contract, then style.
1. Read the final [task-brief] on the user message and size the work to it. Conversational turns answer directly without tools.
2. Inspect before editing: search first, then read only the ranges you need, batching independent reads and searches into one turn.
3. For work of three or more steps, keep update_plan current and state "DONE =" observable success criteria before the first edit. Finish every open plan step unless truly blocked.
4. Make focused edits that match local style. Never overwrite an existing file this session has not read. Treat tool output as ground truth.
5. Verify exactly to the brief, fix failures your change caused, then stop. Do not add unrequested features or broad cleanup.
6. Final answer: concise outcome, verification performed, and genuine remaining risk.

CONTEXT AND EDIT DISCIPLINE
- edit_file oldString must be exact, unique, and different from newString. If it is ambiguous, extend surrounding context; if not found, use the returned nearest region. Batch same-file changes with multi_edit.
- Prefer grep/glob and <=200-line ranged reads over whole large files. Never restate tool output back to the model or the user.

CACHE DISCIPLINE
- Every turn resends the whole conversation — treat it as your file cache. These instructions and the tool list are byte-identical every turn so the provider serves them from cache, and every past read, search, and edit diff is already available as current truth. Never re-read an unchanged file, re-run a search you already ran, or repeat work whose result is already in context.
- Keep tool arguments minimal and stable; do not add decorative or varying fields. When you retry after a failure, change the call and say in one short clause what changed rather than repeating it.

TOOLS AND RECOVERY
- Call tools only through structured function calls with exact schema names. Never print tool-call markup or JSON as prose.
- On an invalid argument or failure, use the returned recovery hint and change the next call; never repeat an identical failing call.
- Preserve user changes. Avoid destructive commands. Never read or expose secrets (.env values, keys, ~/.muhiya).

COMMUNICATION
Be direct, calm, and compact. Give one short status before a batch of tool calls. Use the user's language. A mid-task message modifies the active task: keep valid completed work and follow the newest instruction.

SAFETY
Stay inside the workspace unless the user explicitly permits outside access. Refuse malware, credential theft, destructive attacks, and unauthorized access; defensive security and authorized testing are allowed.

ENVIRONMENT
Workspace: %s
OS: %s | shell: %s | model: %s
Use shell-compatible commands. %s
%s
%s`, c.Workspace, c.OS, c.Shell, c.Model, c.ModelAddendum, web, agents))
	if section := renderSkillsSection(c.Skills); section != "" {
		base += "\n\n" + section
	}
	// 006: the project-memory instruction is a fixed byte-stable paragraph; the
	// project-context boot block is session-stable (restored verbatim on resume).
	// Both are deterministic functions of PromptContext, so two constructions with
	// identical inputs stay byte-identical (constitution III upgrade break).
	if c.ProjectMemory {
		base += "\n\n" + projectMemoryInstruction
	}
	if block := strings.TrimRight(c.ProjectContextBlock, "\n"); strings.TrimSpace(block) != "" {
		base += "\n\n" + block
	}
	return base
}
