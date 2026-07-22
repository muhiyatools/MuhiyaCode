package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestApplyPatchPreservesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose a POSIX executable permission bit")
	}
	root := t.TempDir()
	path := filepath.Join(root, "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	if _, err := ws.Read(context.Background(), ReadOptions{Path: "run.sh"}); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/run.sh\n+++ b/run.sh\n@@ -1,2 +1,2 @@\n #!/bin/sh\n-echo old\n+echo new\n"
	if _, err := ws.ApplyPatch(context.Background(), patch); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("patched executable mode = %o, want 755", got)
	}
}

func TestParseUnifiedPatchRejectsUnsatisfiedCounts(t *testing.T) {
	_, err := parseUnifiedPatch("--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n-old\n+new")
	if err == nil {
		t.Fatal("malformed hunk with unsatisfied counts was accepted")
	}
}
