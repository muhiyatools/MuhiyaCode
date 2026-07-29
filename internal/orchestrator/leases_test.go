package orchestrator

import (
	"path/filepath"
	"testing"
)

func TestFileLeasesRejectOverlappingWriters(t *testing.T) {
	root := t.TempDir()
	leases := newFileLeaseSet()
	if err := leases.acquire("worker-a", root, []string{"src"}); err != nil {
		t.Fatal(err)
	}
	if err := leases.acquire("worker-b", root, []string{filepath.Join("src", "main.go")}); err == nil {
		t.Fatal("overlapping directory/file write leases were allowed")
	}
	if err := leases.acquire("worker-c", root, []string{"docs"}); err != nil {
		t.Fatalf("non-overlapping write lease rejected: %v", err)
	}
	leases.release("worker-a")
	if err := leases.acquire("worker-b", root, []string{filepath.Join("src", "main.go")}); err != nil {
		t.Fatalf("released lease still blocked writer: %v", err)
	}
}

func TestUnscopedLeaseConflictsWithEveryWriter(t *testing.T) {
	root := t.TempDir()
	leases := newFileLeaseSet()
	if err := leases.acquire("shell", root, nil); err != nil {
		t.Fatal(err)
	}
	if err := leases.acquire("file-writer", root, []string{"a.go"}); err == nil {
		t.Fatal("unscoped shell/MCP mutation lease did not block a file writer")
	}
}
