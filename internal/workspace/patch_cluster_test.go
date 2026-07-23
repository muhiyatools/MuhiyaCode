package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func patchWorkspace(t *testing.T, root string) *Workspace {
	t.Helper()
	trust := NewMemoryTrustStore()
	_ = trust.Trust(context.Background(), root)
	ws, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// C-1: a hunk that DELETES a line whose content starts with "-- " (a SQL/Lua/Ada
// comment) renders that deletion as "--- comment". The old prefix-bounded parser
// mistook it for a file header and rejected the whole patch. Count-bounded
// consumption reads it as body and applies cleanly.
func TestApplyPatchDeletesDoubleDashCommentLine(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "q.sql")
	if err := os.WriteFile(file, []byte("SELECT 1;\n-- deprecated\nSELECT 2;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "q.sql"}); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/q.sql\n+++ b/q.sql\n@@ -1,3 +1,2 @@\n SELECT 1;\n--- deprecated\n SELECT 2;\n"
	if _, err := ws.ApplyPatch(ctx, patch); err != nil {
		t.Fatalf("apply_patch rejected a valid comment-deleting patch: %v", err)
	}
	got, _ := os.ReadFile(file)
	if want := "SELECT 1;\nSELECT 2;\n"; string(got) != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

// C-1 companion: a hunk that ADDS a line whose content starts with "++"
// (rendered "+++ x") must be applied as an addition, not skipped as a header.
func TestApplyPatchAddsDoublePlusLine(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "c.txt")
	if err := os.WriteFile(file, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "c.txt"}); err != nil {
		t.Fatal(err)
	}
	// Adding a line whose CONTENT is "++ counter" renders as "+++ counter" (the
	// "+" add-prefix plus the "++ counter" content).
	patch := "--- a/c.txt\n+++ b/c.txt\n@@ -1,2 +1,3 @@\n a\n+++ counter\n b\n"
	if _, err := ws.ApplyPatch(ctx, patch); err != nil {
		t.Fatalf("apply_patch rejected a valid ++-adding patch: %v", err)
	}
	got, _ := os.ReadFile(file)
	if want := "a\n++ counter\nb\n"; string(got) != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

// C-5: a file WITHOUT a trailing newline must not gain one when patched. The old
// applier appended a newline whenever any hunk applied.
func TestApplyPatchPreservesMissingTrailingNewline(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f.txt")
	if err := os.WriteFile(file, []byte("alpha\nbeta"), 0o644); err != nil { // no trailing \n
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "f.txt"}); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n alpha\n-beta\n\\ No newline at end of file\n+gamma\n\\ No newline at end of file\n"
	if _, err := ws.ApplyPatch(ctx, patch); err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	got, _ := os.ReadFile(file)
	if strings.HasSuffix(string(got), "\n") {
		t.Fatalf("apply_patch added a trailing newline to a file that had none: %q", got)
	}
	if string(got) != "alpha\ngamma" {
		t.Fatalf("result = %q, want %q", got, "alpha\ngamma")
	}
}
