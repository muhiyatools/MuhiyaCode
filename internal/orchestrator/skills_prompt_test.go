package orchestrator

import (
	"strings"
	"testing"
)

// TestSkillsSectionDeterministic (003 T037) verifies the advertised skills
// listing is byte-identical across constructions, order-independent (the caller
// sorts), empty-set safe, and renders a fixed no-description form.
func TestSkillsSectionDeterministic(t *testing.T) {
	base := PromptContext{Workspace: `F:\ws`, OS: "Windows", Shell: "pwsh", Model: "m"}

	t.Run("byte identical across constructions", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{
			{Name: "code-review", Path: ".agents/skills/code-review/SKILL.md", Description: "Review changed code."},
			{Name: "release-notes", Path: ".agents/skills/release-notes/SKILL.md", Description: "Draft release notes."},
		}
		first, second := SystemPrompt(ctx), SystemPrompt(ctx)
		if first != second {
			t.Fatal("skills section is not byte-identical across constructions")
		}
		if !strings.Contains(SystemPrompt(ctx), "SKILLS") {
			t.Fatal("skills section missing when skills present")
		}
	})

	t.Run("empty set omits the section", func(t *testing.T) {
		if strings.Contains(SystemPrompt(base), "SKILLS") {
			t.Fatal("empty skill set must not emit a SKILLS section")
		}
	})

	t.Run("frontmatter-less skill uses the fixed no-description form", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{{Name: "bare", Path: ".agents/skills/bare/SKILL.md"}}
		out := SystemPrompt(ctx)
		if !strings.Contains(out, "- bare (.agents/skills/bare/SKILL.md)") {
			t.Fatalf("no-description form missing:\n%s", out)
		}
		if strings.Contains(out, "- bare (.agents/skills/bare/SKILL.md):") {
			t.Fatalf("no-description skill must not have a trailing colon:\n%s", out)
		}
	})

	t.Run("section content depends only on the provided order", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{
			{Name: "a", Path: "p/a", Description: "one"},
			{Name: "b", Path: "p/b", Description: "two"},
		}
		out := renderSkillsSection(ctx.Skills)
		if strings.Index(out, "- a ") > strings.Index(out, "- b ") {
			t.Fatal("listing did not preserve caller order")
		}
	})
}
