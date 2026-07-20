package orchestrator

import (
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/instructions"
)

// projectMemoryInstruction is the fixed, byte-stable paragraph that teaches the
// model to treat MEMORY.md as its durable project memory and to maintain it
// through the dedicated memory tools — save_memory / recall_memory / edit_memory
// (feature 008 US5/MT-3, Memory Parity N2/N3). The literal text is registered as
// instructions.ProjectMemoryInstructionBody (feature 010 US3).
const projectMemoryInstruction = instructions.ProjectMemoryInstructionBody

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
	// When true the prompt tells the model that MEMORY.md is its durable memory,
	// recalled and maintained through the memory tools. It carries no dynamic value.
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
	b.WriteString(instructions.SkillsHeaderBody + "\n")
	b.WriteString(instructions.SkillsHintBody)
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
	web := instructions.PromptWebUnavailableBody
	if c.HasWeb {
		web = instructions.PromptWebAvailableBody
	}
	// DELEGATION (feature 008 US1, contract DG-1..3): the affirmative, criteria-based
	// delegation section. Session-invariant (SubagentModel is fixed per session), so
	// it is byte-stable in the prefix; the dynamic allowance number stays on the
	// user-message tail as the task brief's "agents<=N". Positioned as its own
	// section — not a trailing environment clause — because instruction salience is
	// part of the contract for the driven model family.
	agents := instructions.PromptDelegationOffBody
	if c.HasSubagents {
		agents = fmt.Sprintf(instructions.PromptDelegationOnTemplate, c.SubagentModel)
	}
	sections := []string{
		instructions.PromptIdentityBody,
		instructions.PromptOperatingContractBody,
		instructions.PromptContextEditDisciplineBody,
		instructions.PromptCacheDisciplineBody,
		instructions.PromptToolsAndRecoveryBody,
		agents,
		instructions.PromptCommunicationBody,
		instructions.PromptSafetyBody,
		fmt.Sprintf(instructions.PromptEnvironmentTemplate, c.Workspace, c.OS, c.Shell, c.Model, c.ModelAddendum, web),
	}
	base := strings.TrimSpace(strings.Join(sections, "\n\n"))
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
