package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C-7 (and C-6): a change to a file too large to checkpoint has no automatic
// rollback, so the tool result must warn. write_file additionally warns that it
// replaces the whole file.
func TestOversizeChangeWarnsNoCheckpoint(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("filler line here\n", 150_000) + "UNIQUE_MARKER\n" // ~2.55 MB, over the 2 MB snapshot cap
	file := filepath.Join(root, "big.txt")
	if err := os.WriteFile(file, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "big.txt"}); err != nil {
		t.Fatal(err)
	}

	edit, err := ws.Edit(ctx, "big.txt", Edit{Old: "UNIQUE_MARKER", New: "REPLACED"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(edit.Notes, "\n"), "cannot be auto-rolled back") {
		t.Fatalf("edit of a >2MB file must warn about the missing checkpoint: %+v", edit.Notes)
	}

	write, err := ws.Write(ctx, "big.txt", "brand new small content\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(write.Summary, "cannot be auto-rolled back") || !strings.Contains(write.Summary, "ENTIRE file") {
		t.Fatalf("write_file overwrite of a >2MB file must warn about no rollback and whole-file replacement:\n%s", write.Summary)
	}
}
