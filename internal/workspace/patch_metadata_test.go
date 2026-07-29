package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestApplyPatchPreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX execute bits")
	}
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	workspace := trustedTestWorkspace(t, root)
	if _, err := workspace.Read(ctx, ReadOptions{Path: "script.sh"}); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/script.sh\n+++ b/script.sh\n@@ -1,2 +1,2 @@\n #!/bin/sh\n-echo old\n+echo new\n"
	if _, err := workspace.ApplyPatch(ctx, patch); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o750 {
		t.Fatalf("patch changed file mode to %o", got)
	}
}

func TestCheckpointRestorePreservesFileMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX execute bits")
	}
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "script.sh")
	if err := os.WriteFile(path, []byte("original\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	modTime := time.Unix(1_700_000_000, 0).UTC()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	store := &CheckpointStore{
		SessionID: "metadata", SessionDir: t.TempDir(), Workspace: root, Metadata: &memoryCheckpoint{},
	}
	if _, err := store.Create(ctx, "metadata checkpoint", []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RestoreLatest(ctx); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o750 || !fileInfo.ModTime().Equal(modTime) {
		t.Fatalf("metadata not restored: mode=%o mtime=%s", fileInfo.Mode().Perm(), fileInfo.ModTime())
	}
}

func trustedTestWorkspace(t *testing.T, root string) *Workspace {
	t.Helper()
	trust := NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	workspace, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}
