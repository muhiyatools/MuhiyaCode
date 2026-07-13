package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nInstructions."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDiscoverWorkspaceSkillsScopeAndOrder (003 T034) verifies discovery is
// workspace-scoped (.agents/skills, .codex/skills), deterministic (sorted by
// lowercased name), and excludes external roots.
func TestDiscoverWorkspaceSkillsScopeAndOrder(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, filepath.Join(ws, ".agents", "skills"), "zebra", "Z skill")
	writeSkill(t, filepath.Join(ws, ".agents", "skills"), "alpha", "A skill")
	writeSkill(t, filepath.Join(ws, ".codex", "skills"), "mango", "M skill")
	// An external root that must NOT be discovered by the workspace scan.
	external := t.TempDir()
	writeSkill(t, filepath.Join(external, "skills"), "outsider", "external")
	t.Setenv("MUHIYA_SKILLS_DIR", filepath.Join(external, "skills"))

	skills, err := DiscoverWorkspaceSkills(ws, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 3 {
		t.Fatalf("expected 3 workspace skills, got %d: %+v", len(skills), skills)
	}
	// Deterministic order: alpha, mango, zebra.
	want := []string{"alpha", "mango", "zebra"}
	for i, s := range skills {
		if s.Name != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, s.Name, want[i])
		}
		if s.Name == "outsider" {
			t.Fatal("external-root skill leaked into the workspace listing")
		}
	}
}
