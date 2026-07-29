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

// SkillListing is one advertised skill: a name, the absolute path to its
// SKILL.md, and a single-line description (may be empty).
//
// Path is engine-side only — read_skill resolves by NAME (013 contract
// skills-autouse §2.2). Skills now come from home roots too, whose paths the
// model could not use anyway, and a name-keyed tool is what keeps those files
// reachable without widening file-tool containment.
type SkillListing struct {
	Name        string
	Path        string
	Description string
}

// renderSkillsSection returns the deterministic SKILLS block, or "" when no
// skills are advertised. Order is caller-guaranteed stable; each line is
// byte-fixed for identical input (013 contract skills-autouse §2.2).
func renderSkillsSection(skills []SkillListing) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(instructions.SkillsHeaderBody + "\n")
	b.WriteString("Load a relevant skill with read_skill before using it.")
	for _, skill := range skills {
		b.WriteString(fmt.Sprintf("\n- %s", skill.Name))
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
	sections := []string{
		instructions.PromptIdentityBody,
		instructions.PromptCoreBody,
		fmt.Sprintf(instructions.PromptEnvironmentTemplate, c.Workspace, c.OS, c.Shell, c.Model, instructions.ShellCommandGuidance(c.Shell), c.ModelAddendum, web),
	}
	base := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if section := renderSkillsSection(c.Skills); section != "" {
		base += "\n\n" + section
	}
	if c.ProjectMemory {
		base += "\n\n" + projectMemoryInstruction
	}
	// 006: the project-memory instruction is a fixed byte-stable paragraph; the
	// project-context boot block is session-stable (restored verbatim on resume).
	// Both are deterministic functions of PromptContext, so two constructions with
	// identical inputs stay byte-identical (constitution III upgrade break).
	if block := strings.TrimRight(c.ProjectContextBlock, "\n"); strings.TrimSpace(block) != "" {
		base += "\n\n" + block
	}
	return base
}
