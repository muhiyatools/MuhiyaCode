package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadProjectMemoryFile (006) verifies MEMORY.md loads with the same contained,
// validated form as MUHIYA.md.
func TestLoadProjectMemoryFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectMemoryFile), []byte("# Project Memory\n\n- the prefix cache stays byte-stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := LoadProjectMemory(root)
	if !mem.Loaded() {
		t.Fatalf("expected loaded memory, got state=%q diag=%q", mem.State, mem.Diagnostic)
	}
	if mem.ContentHash == "" || !strings.Contains(mem.CanonicalContent, "byte-stable") {
		t.Fatalf("memory content/hash missing: %+v", mem)
	}
}

// TestMemoryFileSecretRejected (006) confirms a MEMORY.md that looks like it holds
// a credential is not injected.
func TestMemoryFileSecretRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectMemoryFile), []byte("api_key = sk-abcdefghijklmnop1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadProjectMemory(root); got.State != InstructionSecretRejected {
		t.Fatalf("secret-bearing memory should be rejected, got %q", got.State)
	}
}

// TestEnsureProjectInstructionsTemplateCreatesAndPreserves (006) verifies the
// MUHIYA.md template is created when absent, is treated as empty while pristine
// (so it never rides the cached prefix), is injected once edited, and is never
// overwritten.
func TestEnsureProjectInstructionsTemplateCreatesAndPreserves(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, ProjectInstructionsFile)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("precondition: MUHIYA.md should not exist")
	}
	if err := EnsureProjectInstructionsTemplate(root); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("template not created: %v", err)
	}
	// A pristine (comment-only) template must not be injected.
	if loaded := LoadProjectInstructions(root); loaded.State != InstructionEmpty {
		t.Fatalf("pristine template should load as empty, got %q", loaded.State)
	}
	// Editing it activates it, and re-running Ensure never overwrites it.
	custom := "## Project\nMy real instructions.\n"
	if err := os.WriteFile(target, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProjectInstructionsTemplate(root); err != nil {
		t.Fatalf("ensure (existing): %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != custom {
		t.Fatal("EnsureProjectInstructionsTemplate overwrote an existing file")
	}
	if got := LoadProjectInstructions(root); got.State != InstructionLoaded {
		t.Fatalf("edited instructions should load, got %q", got.State)
	}
}

// TestCommentOnlyDocIsEmpty (006) verifies a doc containing only HTML-comment
// guidance is treated as empty (never injected).
func TestCommentOnlyDocIsEmpty(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectMemoryFile), []byte("<!-- just guidance, no real content -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadProjectMemory(root); got.State != InstructionEmpty {
		t.Fatalf("comment-only doc should be empty, got %q", got.State)
	}
}
