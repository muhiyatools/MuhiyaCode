package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Loaded reports whether the instructions produced an injectable prompt
// block. Feature 010 T038: production call sites (command/runtime_build.go)
// consume .ContentHash/.CanonicalContent/.State directly and never need this
// coarse boolean, so it lives here — its only caller — rather than in
// project_context.go.
func (p ProjectInstructions) Loaded() bool { return p.State == InstructionLoaded }

// TestLoadProjectMemoryFile (006) verifies MEMORY.md loads with the same contained,
// validated form as MUHIYA.md.
func TestLoadProjectMemoryFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectMemoryFile), []byte("# Project Memory\n\n- the prefix cache stays byte-stable\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := LoadMemoryIndex(root)
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
	if got := LoadMemoryIndex(root); got.State != InstructionSecretRejected {
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
	if got := LoadMemoryIndex(root); got.State != InstructionEmpty {
		t.Fatalf("comment-only doc should be empty, got %q", got.State)
	}
}

// --- Memory Parity N1 --------------------------------------------------------

// TestEnsureMemoryIndexTemplateCreatesAndPreserves (N1): every project gets a
// MEMORY.md at session start — created when absent, pristine-empty (never
// injected), and never overwritten once it exists.
func TestEnsureMemoryIndexTemplateCreatesAndPreserves(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects", "demo-1234abcd", "memory")
	if err := EnsureMemoryIndexTemplate(dir); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	target := filepath.Join(dir, ProjectMemoryFile)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("template not created (dir should be made too): %v", err)
	}
	// While pristine it reads as empty — zero prefix tokens.
	if got := LoadMemoryIndex(dir); got.State != InstructionEmpty {
		t.Fatalf("pristine memory template should load as empty, got %q", got.State)
	}
	// Real content is never overwritten by a second ensure.
	custom := "# Project Memory\n\n- a real durable fact\n"
	if err := os.WriteFile(target, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMemoryIndexTemplate(dir); err != nil {
		t.Fatalf("ensure (existing): %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != custom {
		t.Fatal("EnsureMemoryIndexTemplate overwrote an existing index")
	}
	// An empty memoryDir is a no-op, never an error.
	if err := EnsureMemoryIndexTemplate(""); err != nil {
		t.Fatalf("empty dir should be a silent no-op, got %v", err)
	}
}

// TestLoadMemoryIndexStripsComments (N1): once real entries exist, the injected
// form excludes HTML comments (the template guidance costs zero tokens forever),
// and the content hash covers only the injected (stripped) form.
func TestLoadMemoryIndexStripsComments(t *testing.T) {
	dir := t.TempDir()
	content := "<!--\ntemplate guidance for humans\n-->\n# Project Memory\n\n- the prefix cache stays byte-stable\n"
	if err := os.WriteFile(filepath.Join(dir, ProjectMemoryFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	mem := LoadMemoryIndex(dir)
	if mem.State != InstructionLoaded {
		t.Fatalf("expected loaded, got %q (%s)", mem.State, mem.Diagnostic)
	}
	if strings.Contains(mem.CanonicalContent, "<!--") || strings.Contains(mem.CanonicalContent, "guidance for humans") {
		t.Fatalf("comment leaked into the injected form:\n%s", mem.CanonicalContent)
	}
	if !strings.Contains(mem.CanonicalContent, "byte-stable") {
		t.Fatalf("real content missing from the injected form:\n%s", mem.CanonicalContent)
	}
	// A comment-only edit must not change the hash (no spurious memory-update).
	commentEdited := "<!--\nDIFFERENT guidance\n-->\n# Project Memory\n\n- the prefix cache stays byte-stable\n"
	if err := os.WriteFile(filepath.Join(dir, ProjectMemoryFile), []byte(commentEdited), 0o600); err != nil {
		t.Fatal(err)
	}
	if again := LoadMemoryIndex(dir); again.ContentHash != mem.ContentHash {
		t.Fatal("a comment-only edit changed the injected hash")
	}
	// MUHIYA.md-style docs are NOT comment-stripped (only the memory index is).
	root := t.TempDir()
	withComment := "## Project\nReal instructions.\n<!-- a note to self -->\n"
	if err := os.WriteFile(filepath.Join(root, ProjectInstructionsFile), []byte(withComment), 0o600); err != nil {
		t.Fatal(err)
	}
	if instr := LoadProjectInstructions(root); !strings.Contains(instr.CanonicalContent, "<!-- a note to self -->") {
		t.Fatalf("project instructions must keep their comments verbatim:\n%s", instr.CanonicalContent)
	}
}

// TestLoadMemoryIndexSecretInCommentStillRejected (N1): secret screening runs
// over the FULL file before comment-stripping — a credential hidden inside a
// comment still rejects the whole index.
func TestLoadMemoryIndexSecretInCommentStillRejected(t *testing.T) {
	dir := t.TempDir()
	content := "<!-- api_key = sk-abcdefghijklmnop1234 -->\n# Project Memory\n\n- innocuous fact\n"
	if err := os.WriteFile(filepath.Join(dir, ProjectMemoryFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadMemoryIndex(dir); got.State != InstructionSecretRejected {
		t.Fatalf("comment-hidden credential should reject the file, got %q", got.State)
	}
}
