package command

import (
	"path/filepath"
	"strings"

	"github.com/muhiya/muhiyacode/internal/orchestrator"
	"github.com/muhiya/muhiyacode/internal/tui"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

// workspaceSkillListings discovers workspace-resident skills once and renders
// them as deterministic prompt listings (003 T034/T035): workspace-relative
// forward-slash paths, single-line descriptions truncated to 200 chars. Same
// config + files ⇒ identical slice ⇒ byte-identical prompt section.
func workspaceSkillListings(root string) []orchestrator.SkillListing {
	discovered, err := workspace.DiscoverWorkspaceSkills(root, 40)
	if err != nil {
		return nil
	}
	listings := make([]orchestrator.SkillListing, 0, len(discovered))
	for _, skill := range discovered {
		rel := skill.Path
		if r, relErr := filepath.Rel(root, skill.Path); relErr == nil {
			rel = r
		}
		rel = filepath.ToSlash(rel)
		listings = append(listings, orchestrator.SkillListing{Name: skill.Name, Path: rel, Description: singleLineDescription(skill.Description)})
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

// loadSkillBody loads one skill's instructions on demand (32 KiB cap), used at
// submit for user-selected skills only.
func (a *Application) loadSkillBody(path string) (string, error) {
	return workspace.LoadSkillInstructions(workspace.Skill{Path: path}, 32*1024)
}
