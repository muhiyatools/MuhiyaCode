package state

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectID pins the store directory naming (B1 T060): deterministic,
// basename-legible, hash-disambiguated, sanitized, lowercased, and length-capped.
func TestProjectID(t *testing.T) {
	same := `c:\users\me\my project`
	first, second := ProjectID(same), ProjectID(same)
	if first != second {
		t.Fatal("ProjectID is not deterministic")
	}
	if ProjectID(`c:\a\shared`) == ProjectID(`c:\b\shared`) {
		t.Fatal("distinct paths sharing a basename collided")
	}
	id := ProjectID(`c:\work\My Cool.Repo!!`)
	if len(id) < 10 || id[len(id)-9] != '-' {
		t.Fatalf("id shape wrong (want <slug>-<8hex>): %q", id)
	}
	head, hash := id[:len(id)-9], id[len(id)-8:]
	if head != "my-cool-repo" {
		t.Fatalf("slug not sanitized/lowercased: %q", head)
	}
	if len(hash) != 8 {
		t.Fatalf("hash suffix should be 8 hex chars: %q", hash)
	}
	// A very long basename is capped at 32 characters.
	long := ProjectID(`c:\` + strings.Repeat("x", 100))
	if h := long[:len(long)-9]; len(h) > 32 {
		t.Fatalf("basename slug not capped: %d chars", len(h))
	}
	// An empty/rootish basename still yields a valid id.
	if got := ProjectID(``); !strings.HasPrefix(got, "project-") {
		t.Fatalf("empty key should fall back to 'project': %q", got)
	}
}

// TestProjectMemoryDir pins the store path shape (B1 T060).
func TestProjectMemoryDir(t *testing.T) {
	p := Paths{ProjectsDir: filepath.Join("X:", "home", "projects")}
	dir := ProjectMemoryDir(p, `c:\work\repo`)
	if filepath.Base(dir) != "memory" {
		t.Fatalf("memory dir must end in /memory: %q", dir)
	}
	if !strings.Contains(dir, ProjectID(`c:\work\repo`)) {
		t.Fatalf("memory dir must contain the project id: %q", dir)
	}
}
