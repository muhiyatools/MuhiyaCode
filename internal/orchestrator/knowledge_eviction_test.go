package orchestrator

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

// filePath joins with the host OS so the canonicalKey pass-through matches the
// live call site (filepath.Clean, lowercase on Windows).
func filePath(parts ...string) string {
	return filepath.Join(parts...)
}

func expectedFirst() string {
	return filePath("workspace", "file-05.go")
}

func expectedEvicted(i int) string {
	return filePath("workspace", fmt.Sprintf("file-%02d.go", i))
}
func expectedRetained(i int) string {
	return filePath("workspace", fmt.Sprintf("file-%02d.go", i))
}

// TestKnowledgeNoteFileEvictionDeterministic verifies that files are evicted in
// insert order regardless of Go's randomized map iteration, so the snapshot is
// stable across runs.
func TestKnowledgeNoteFileEvictionDeterministic(t *testing.T) {
	k := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	for i := 0; i < 85; i++ {
		k.NoteFile(expectedRetained(i), "read")
	}
	if len(k.files) != 80 {
		t.Fatalf("expected 80 files, got %d", len(k.files))
	}
	if len(k.filesOrder) != 80 {
		t.Fatalf("expected filesOrder length 80, got %d", len(k.filesOrder))
	}
	for i := 0; i < 5; i++ {
		if _, ok := k.files[expectedEvicted(i)]; ok {
			t.Fatalf("expected %s to be evicted", expectedEvicted(i))
		}
	}
	for i := 5; i < 85; i++ {
		if _, ok := k.files[expectedRetained(i)]; !ok {
			t.Fatalf("expected %s to be retained", expectedRetained(i))
		}
	}
	if runtime.GOOS == "windows" {
		// The eviction order is deterministic regardless of the host, but the
		// key string here is OS-formatted for clarity in failure messages.
		expected := "workspace\\file-05.go"
		if k.filesOrder[0] != expected {
			t.Fatalf("expected filesOrder[0] = %s, got %s", expected, k.filesOrder[0])
		}
	}
}

func TestKnowledgeNoteFileUpdatesPreservesOrder(t *testing.T) {
	k := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	k.NoteFile(filePath("workspace", "a.go"), "read")
	k.NoteFile(filePath("workspace", "b.go"), "read")
	k.NoteFile(filePath("workspace", "a.go"), "edited")

	wantFirst := filePath("workspace", "a.go")
	wantSecond := filePath("workspace", "b.go")
	if k.filesOrder[0] != wantFirst || k.filesOrder[1] != wantSecond {
		t.Fatalf("expected order [%s, %s], got %v", wantFirst, wantSecond, k.filesOrder)
	}
}
