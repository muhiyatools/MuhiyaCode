package command

import (
	"strings"

	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// sessionSkillListings discovers every installed skill once at session start and
// renders the deterministic prompt listing (013 T005, contract skills-autouse
// §1): all roots — workspace, user-configured, and home — so a skill the user
// installed globally can be selected automatically, not only through /skills.
//
// The listing carries the skill's ABSOLUTE path for the engine to load from;
// the rendered prompt section shows only name and description, because
// read_skill resolves by name and a path the model cannot use is prefix weight.
// Same config + files ⇒ identical slice ⇒ byte-identical prompt section.
func sessionSkillListings(root string) []orchestrator.SkillListing {
	discovered, err := workspace.DiscoverSkills(root, nil, 40)
	if err != nil {
		return nil
	}
	listings := make([]orchestrator.SkillListing, 0, len(discovered))
	for _, skill := range discovered {
		listings = append(listings, orchestrator.SkillListing{Name: skill.Name, Path: skill.Path, Description: singleLineDescription(skill.Description)})
	}
	return listings
}

func singleLineDescription(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:199]) + "…"
	}
	return value
}

// listSkills powers the manual /skills modal. It is lazy (003 T036): it does NOT
// load any instruction bodies here, so opening the modal never triggers a burst
// of file reads. The Path travels with each skill; the body is loaded on demand
// for the selected skills only, at submit time (LoadSkill).
func (a *Application) listSkills() ([]tui.Skill, error) {
	a.mu.Lock()
	root := a.runtime.Session.WorkspacePath
	a.mu.Unlock()
	discovered, err := workspace.DiscoverSkills(root, nil, 40)
	if err != nil {
		return nil, err
	}
	result := make([]tui.Skill, 0, len(discovered))
	for _, skill := range discovered {
		result = append(result, tui.Skill{Name: skill.Name, Description: skill.Description, Path: skill.Path})
	}
	return result, nil
}

// SkillBodyLimit bounds one skill's instructions. A skill is guidance, not a
// corpus; without the bound an oversized file could swallow the context window
// the actual task needs. Both paths that load a body — the manual /skills flow
// and the engine's read_skill — go through it.
const SkillBodyLimit = 32 * 1024

// loadSkillBody loads one skill's instructions on demand, used at submit for
// user-selected skills and injected into the engine for read_skill.
func (a *Application) loadSkillBody(path string) (string, error) {
	return workspace.LoadSkillInstructions(workspace.Skill{Path: path}, SkillBodyLimit)
}
