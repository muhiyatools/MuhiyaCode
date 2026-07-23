package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, desc string) {
	t.Helper()
	writeSkillBody(t, dir, name, "---\nname: "+name+"\ndescription: "+desc+"\n---\n\nInstructions.")
}

func writeSkillBody(t *testing.T, dir, name, body string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateHomeRoots points the home-derived roots at empty temp dirs so a
// developer's real ~/.agents/skills cannot leak into these assertions.
func isolateHomeRoots(t *testing.T) {
	t.Helper()
	empty := t.TempDir()
	t.Setenv("HOME", empty)
	t.Setenv("USERPROFILE", empty)
	t.Setenv("CODEX_HOME", filepath.Join(empty, ".codex"))
	t.Setenv("MUHIYA_SKILLS_DIR", "")
}

// TestDiscoverSkillsSpansEveryRootInOrder (013 T003, contract skills-autouse
// §1.1) is the core of automatic skill use: the catalog must span workspace,
// configured, and home roots — a home-root skill that never appears here can
// never be auto-selected by the model.
func TestDiscoverSkillsSpansEveryRootInOrder(t *testing.T) {
	isolateHomeRoots(t)
	ws := t.TempDir()
	home := t.TempDir()
	configured := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MUHIYA_SKILLS_DIR", configured)

	writeSkill(t, filepath.Join(ws, ".agents", "skills"), "zebra", "workspace agents root")
	writeSkill(t, filepath.Join(ws, ".codex", "skills"), "mango", "workspace codex root")
	writeSkill(t, configured, "papaya", "configured root")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "alpha", "home agents root")

	skills, err := DiscoverSkills(ws, nil, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 4 {
		t.Fatalf("expected every root represented, got %d: %+v", len(skills), skills)
	}
	// Presentation order is deterministic: stable sort by lowercased name.
	want := []string{"alpha", "mango", "papaya", "zebra"}
	for i, skill := range skills {
		if skill.Name != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, skill.Name, want[i])
		}
	}
}

// TestDiscoverSkillsWorkspaceShadowsGlobal pins the first-wins precedence rule:
// a project-local skill deliberately overrides a same-named global one.
func TestDiscoverSkillsWorkspaceShadowsGlobal(t *testing.T) {
	isolateHomeRoots(t)
	ws := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeSkill(t, filepath.Join(ws, ".agents", "skills"), "shared", "the workspace copy")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "shared", "the home copy")

	skills, err := DiscoverSkills(ws, nil, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("duplicate names must collapse to one entry, got %+v", skills)
	}
	if skills[0].Description != "the workspace copy" {
		t.Fatalf("workspace root must win, got %q", skills[0].Description)
	}
}

// TestDiscoverSkillsSkipsMalformedWithoutFailing (FR-009): one broken skill
// must never take down the catalog.
func TestDiscoverSkillsSkipsMalformedWithoutFailing(t *testing.T) {
	isolateHomeRoots(t)
	ws := t.TempDir()
	root := filepath.Join(ws, ".agents", "skills")
	writeSkill(t, root, "healthy", "fine")
	// No frontmatter at all: readSkill falls back to the directory name, so the
	// entry survives — what must NOT happen is an error or a lost sibling.
	writeSkillBody(t, root, "no-frontmatter", "just prose, no frontmatter")
	// A directory that looks like a skill but holds no SKILL.md.
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	skills, err := DiscoverSkills(ws, nil, 40)
	if err != nil {
		t.Fatalf("a malformed skill must not fail discovery: %v", err)
	}
	names := map[string]bool{}
	for _, skill := range skills {
		names[skill.Name] = true
	}
	if !names["healthy"] {
		t.Fatalf("the healthy skill was lost alongside the malformed one: %+v", skills)
	}
	if names["empty-dir"] {
		t.Fatal("a directory without SKILL.md must not become a catalog entry")
	}
}

// TestDiscoverSkillsHonorsLimit keeps the catalog bounded so a machine with
// hundreds of installed skills cannot flood the cached prefix.
func TestDiscoverSkillsHonorsLimit(t *testing.T) {
	isolateHomeRoots(t)
	ws := t.TempDir()
	root := filepath.Join(ws, ".agents", "skills")
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		writeSkill(t, root, name, name+" skill")
	}
	skills, err := DiscoverSkills(ws, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 3 {
		t.Fatalf("limit not honored: got %d", len(skills))
	}
}

// TestSkillRootsPrecedenceOrder pins the documented root order itself, so a
// future edit cannot silently reshuffle which copy of a name wins.
func TestSkillRootsPrecedenceOrder(t *testing.T) {
	isolateHomeRoots(t)
	home := t.TempDir()
	configured := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("MUHIYA_SKILLS_DIR", configured)

	roots := SkillRoots("/ws", []string{"/extra"})
	want := []string{
		filepath.Join("/ws", ".agents", "skills"),
		filepath.Join("/ws", ".codex", "skills"),
		"/extra",
		configured,
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	if len(roots) != len(want) {
		t.Fatalf("root count = %d, want %d: %+v", len(roots), len(want), roots)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("root[%d] = %q, want %q", i, roots[i], want[i])
		}
	}
}
