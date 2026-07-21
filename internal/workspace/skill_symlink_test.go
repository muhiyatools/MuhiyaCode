package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I-5: a SKILL.md symlinked into a protected credential root must be refused, not
// read and returned. Skips where the OS forbids unprivileged symlinks.
func TestLoadSkillInstructionsRejectsSymlinkToSensitiveRoot(t *testing.T) {
	sensitive := t.TempDir()
	t.Setenv("MUHIYA_HOME", sensitive) // makes `sensitive` a protected root
	secret := filepath.Join(sensitive, "id_rsa")
	if err := os.WriteFile(secret, []byte("-----BEGIN OPENSSH PRIVATE KEY-----"), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := t.TempDir()
	link := filepath.Join(skillDir, "SKILL.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unsupported on this host: %v", err)
	}
	if _, err := LoadSkillInstructions(Skill{Name: "evil", Path: link}, 1024); err == nil {
		t.Fatal("read_skill served a SKILL.md symlinked into a protected root")
	} else if !strings.Contains(err.Error(), "protected") {
		t.Fatalf("unexpected error: %v", err)
	}

	// A normal skill file still loads.
	good := filepath.Join(skillDir, "real.md")
	if err := os.WriteFile(good, []byte("# House style\nBe terse."), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := LoadSkillInstructions(Skill{Name: "style", Path: good}, 1024)
	if err != nil || !strings.Contains(body, "House style") {
		t.Fatalf("a normal skill must still load: body=%q err=%v", body, err)
	}
}
