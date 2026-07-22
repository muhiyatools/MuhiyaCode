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
	Lean                bool
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
func renderSkillsSection(skills []SkillListing, lean ...bool) string {
	if len(skills) == 0 {
		return ""
	}
	if len(lean) > 0 && lean[0] {
		names := make([]string, 0, min(8, len(skills)))
		for _, skill := range skills {
			if len(names) == 8 {
				break
			}
			names = append(names, skill.Name)
		}
		body := "SKILLS\nWhen an applicable skill is named or evident, discover and invoke read_skill before that work. Available: " + strings.Join(names, ", ")
		if len(skills) > len(names) {
			body += fmt.Sprintf(" (+%d more)", len(skills)-len(names))
		}
		if len(body) > 240 {
			body = body[:240]
		}
		return body
	}
	var b strings.Builder
	b.WriteString(instructions.SkillsHeaderBody + "\n")
	b.WriteString(instructions.SkillsHintBody)
	for _, skill := range skills {
		if skill.Description == "" {
			b.WriteString(fmt.Sprintf("\n- %s", skill.Name))
		} else {
			b.WriteString(fmt.Sprintf("\n- %s: %s", skill.Name, skill.Description))
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
	environmentRules := []string{instructions.ShellCommandGuidance(c.Shell)}
	if strings.TrimSpace(c.ModelAddendum) != "" {
		environmentRules = append(environmentRules, c.ModelAddendum)
	}
	environmentRules = append(environmentRules, web)
	sections := []string{
		instructions.PromptIdentityBody,
		instructions.PromptOperatingContractBody,
		instructions.PromptContextEditDisciplineBody,
		instructions.PromptCacheDisciplineBody,
		instructions.PromptPlanningBody,
		instructions.PromptToolsAndRecoveryBody,
		instructions.PromptModelBody,
		instructions.PromptCommunicationBody,
		instructions.PromptSafetyBody,
		fmt.Sprintf(instructions.PromptEnvironmentTemplate, c.Workspace, c.OS, c.Shell, c.Model, strings.Join(environmentRules, "\n")),
	}
	if c.Lean {
		sections = []string{
			instructions.PromptLeanBody,
			fmt.Sprintf(instructions.PromptEnvironmentTemplate, c.Workspace, c.OS, c.Shell, c.Model, strings.Join(environmentRules, "\n")),
		}
	}
	base := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if section := renderSkillsSection(c.Skills, c.Lean); section != "" {
		base += "\n\n" + section
	}
	// 006: the project-memory instruction is a fixed byte-stable paragraph; the
	// project-context boot block is session-stable (restored verbatim on resume).
	// Both are deterministic functions of PromptContext, so two constructions with
	// identical inputs stay byte-identical (constitution III upgrade break).
	if c.ProjectMemory {
		if c.Lean {
			base += "\n\nPROJECT MEMORY\nUse recall_memory/save_memory/edit_memory only when durable project knowledge is relevant; never store progress, speculation, raw transcripts, or secrets."
		} else {
			base += "\n\n" + projectMemoryInstruction
		}
	}
	if block := strings.TrimRight(c.ProjectContextBlock, "\n"); strings.TrimSpace(block) != "" {
		base += "\n\n" + block
	}
	return base
}
