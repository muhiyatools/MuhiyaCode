package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPatchJournalRecoversInterruptedTransactionToOldState(t *testing.T) {
	root := t.TempDir()
	sessionDir := t.TempDir()
	writeFixture(t, filepath.Join(root, "a.txt"), "old-a\n")
	writeFixture(t, filepath.Join(root, "b.txt"), "old-b\n")

	store := &CheckpointStore{SessionDir: sessionDir, Workspace: root}
	workspace := newAutoWorkspace(t, root, store)
	changes := prepareJournalFixture(t, workspace)
	transaction, err := workspace.patchJournal.begin(changes)
	if err != nil {
		t.Fatal(err)
	}
	transaction.manifest.State = patchCommitting
	if err := transaction.persist(); err != nil {
		t.Fatal(err)
	}
	if err := applyPatchChange(root, changes[0]); err != nil {
		t.Fatal(err)
	}

	_ = newAutoWorkspace(t, root, store)
	assertFileContent(t, filepath.Join(root, "a.txt"), "old-a\n")
	assertFileContent(t, filepath.Join(root, "b.txt"), "old-b\n")
	assertTransactionDirectoryEmpty(t, sessionDir)
}

func TestPatchJournalKeepsTransactionWithDurableCommitMarker(t *testing.T) {
	root := t.TempDir()
	sessionDir := t.TempDir()
	writeFixture(t, filepath.Join(root, "a.txt"), "old-a\n")
	writeFixture(t, filepath.Join(root, "b.txt"), "old-b\n")

	store := &CheckpointStore{SessionDir: sessionDir, Workspace: root}
	workspace := newAutoWorkspace(t, root, store)
	changes := prepareJournalFixture(t, workspace)
	transaction, err := workspace.patchJournal.begin(changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if err := applyPatchChange(root, change); err != nil {
			t.Fatal(err)
		}
	}
	transaction.manifest.State = patchCommitted
	if err := transaction.persist(); err != nil {
		t.Fatal(err)
	}

	_ = newAutoWorkspace(t, root, store)
	assertFileContent(t, filepath.Join(root, "a.txt"), "new-a\n")
	assertFileContent(t, filepath.Join(root, "b.txt"), "new-b\n")
	assertTransactionDirectoryEmpty(t, sessionDir)
}

func TestSafeRootedWriteRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	target := filepath.Join(link, "secret.txt")
	if err := safeWriteTarget(root, target, []byte("leak"), 0o600); err == nil {
		t.Fatal("rooted write followed a symlink outside the workspace")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside target was created: %v", err)
	}
}

func newAutoWorkspace(t *testing.T, root string, store *CheckpointStore) *Workspace {
	t.Helper()
	workspace, err := New(root, Options{
		PermissionMode: contract.PermissionAutoAccept,
		Trust:          NewMemoryTrustStore(),
		Checkpoints:    store,
	})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func prepareJournalFixture(t *testing.T, workspace *Workspace) []patchChange {
	t.Helper()
	for _, path := range []string{"a.txt", "b.txt"} {
		if _, err := workspace.Read(context.Background(), ReadOptions{Path: path}); err != nil {
			t.Fatal(err)
		}
	}
	patches, err := parseUnifiedPatch("--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old-a\n+new-a\n--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-old-b\n+new-b\n")
	if err != nil {
		t.Fatal(err)
	}
	changes, err := workspace.preparePatchChanges(context.Background(), patches)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", path, content, want)
	}
}

func assertTransactionDirectoryEmpty(t *testing.T, sessionDir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(sessionDir, "transactions"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("transaction journals were not cleaned: %v", entries)
	}
}
