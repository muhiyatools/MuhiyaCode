package orchestrator

import (
	"strings"
	"testing"
)

// TestSkillsSectionDeterministic (003 T037, updated by 013 T006) verifies the
// advertised skills listing is byte-identical across constructions,
// order-preserving, empty-set safe, and renders the fixed no-description form.
func TestSkillsSectionDeterministic(t *testing.T) {
	base := PromptContext{Workspace: `F:\ws`, OS: "Windows", Shell: "pwsh", Model: "m"}

	t.Run("byte identical across constructions", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{
			{Name: "code-review", Path: `F:\ws\.agents\skills\code-review\SKILL.md`, Description: "Review changed code."},
			{Name: "release-notes", Path: `C:\Users\dev\.agents\skills\release-notes\SKILL.md`, Description: "Draft release notes."},
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

	// 013 T006: paths left the rendered listing. read_skill resolves by name, and
	// skills now come from home roots whose absolute paths would leak a user's
	// directory layout into the cached prefix for no benefit.
	t.Run("listing renders names only, never descriptions or paths", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{{Name: "frontend-design", Path: `C:\Users\dev\.agents\skills\frontend-design\SKILL.md`, Description: "Build distinctive frontends."}}
		out := SystemPrompt(ctx)
		if !strings.Contains(out, "- frontend-design") {
			t.Fatalf("skill name missing:\n%s", out)
		}
		if strings.Contains(out, "Build distinctive frontends.") {
			t.Fatalf("skill descriptions must load on demand:\n%s", out)
		}
		if strings.Contains(out, "SKILL.md") || strings.Contains(out, `C:\Users`) {
			t.Fatalf("skill paths must not reach the prompt:\n%s", out)
		}
	})

	t.Run("frontmatter-less skill uses the fixed no-description form", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{{Name: "bare", Path: `F:\ws\.agents\skills\bare\SKILL.md`}}
		out := SystemPrompt(ctx)
		if !strings.Contains(out, "\n- bare") {
			t.Fatalf("no-description form missing:\n%s", out)
		}
		if strings.Contains(out, "- bare:") {
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
		if strings.Index(out, "- a") > strings.Index(out, "- b") {
			t.Fatal("listing did not preserve caller order")
		}
	})

	// The hint is the whole mechanism of automatic use: it must name read_skill
	// and the before-you-start timing, or the model has a catalog and no rule.
	t.Run("hint teaches read-before-work through read_skill", func(t *testing.T) {
		ctx := base
		ctx.Skills = []SkillListing{{Name: "x", Description: "y"}}
		out := SystemPrompt(ctx)
		// The equip-a-delegate clause went with the subagent system: the session
		// reads its own skills, so there is no one to name a skill for.
		for _, want := range []string{"read_skill", "before using"} {
			if !strings.Contains(out, want) {
				t.Fatalf("skills hint missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "read its file with read_file") {
			t.Fatal("the superseded read_file skill instruction is still present")
		}
	})
}
