package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestInspectCodeDedup verifies that two identical inspect_code calls are
// deduplicated by the inspection ledger, while different modes or symbols are
// not.
func TestInspectCodeDedup(t *testing.T) {
	root := t.TempDir()
	if err := writeWorkspaceFile(root, "foo.go", "package foo\n\nfunc Baz() {}\n"); err != nil {
		t.Fatal(err)
	}
	ledger := NewInspection(InspectionSnapshot{Version: 3}, nil, root)
	intact := func(string) bool { return true }

	outline := contract.NewToolCall("c1", "inspect_code", `{"mode":"outline","path":"foo.go"}`)
	ledger.Record(outline, "Outline of foo.go (package foo)\n1 symbol shown.")

	duplicate := contract.NewToolCall("c2", "inspect_code", `{"mode":"outline","path":"foo.go"}`)
	if entry, ok := ledger.Duplicate(duplicate, intact); !ok || entry.CallID != "c1" {
		t.Fatalf("expected inspect_code outline dedup, got entry=%+v ok=%v", entry, ok)
	}

	differentMode := contract.NewToolCall("c3", "inspect_code", `{"mode":"definition","path":"foo.go","symbol":"Bar"}`)
	if _, ok := ledger.Duplicate(differentMode, intact); ok {
		t.Fatalf("definition mode should not dedup against outline")
	}

	differentSymbol := contract.NewToolCall("c4", "inspect_code", `{"mode":"definition","path":"foo.go","symbol":"Baz"}`)
	ledger.Record(differentSymbol, "Definition of Baz: foo.go:10")
	duplicateSymbol := contract.NewToolCall("c5", "inspect_code", `{"mode":"definition","path":"foo.go","symbol":"Baz"}`)
	if entry, ok := ledger.Duplicate(duplicateSymbol, intact); !ok || entry.CallID != "c4" {
		t.Fatalf("expected identical symbol dedup, got entry=%+v ok=%v", entry, ok)
	}
}

// TestInspectCodeInvalidatedOnMutation ensures that editing a file clears the
// inspect_code signature for that path.
func TestInspectCodeInvalidatedOnMutation(t *testing.T) {
	root := t.TempDir()
	if err := writeWorkspaceFile(root, "foo.go", "package foo\n"); err != nil {
		t.Fatal(err)
	}
	ledger := NewInspection(InspectionSnapshot{Version: 3}, nil, root)
	intact := func(string) bool { return true }

	outline := contract.NewToolCall("c1", "inspect_code", `{"mode":"outline","path":"foo.go"}`)
	ledger.Record(outline, "Outline of foo.go (package foo)\n")

	edit := contract.NewToolCall("c2", "edit_file", `{"path":"foo.go","oldString":"package foo\n","newString":"package bar\n"}`)
	ledger.InvalidateFor(edit)

	if _, ok := ledger.Duplicate(outline, intact); ok {
		t.Fatalf("inspect_code should not dedup after the file was edited")
	}
}

// TestWebSearchDedup verifies that two identical web_search queries are
// deduplicated, and that a query with different arguments is not.
func TestWebSearchDedup(t *testing.T) {
	root := t.TempDir()
	ledger := NewInspection(InspectionSnapshot{Version: 3}, nil, root)
	intact := func(string) bool { return true }

	q1 := contract.NewToolCall("c1", "web_search", `{"query":"muhiyacode inspect dedup","maxResults":3}`)
	ledger.Record(q1, "Web search results for \"muhiyacode inspect dedup\":\n[1] Example.")

	q2 := contract.NewToolCall("c2", "web_search", `{"query":"muhiyacode inspect dedup","maxResults":3}`)
	if entry, ok := ledger.Duplicate(q2, intact); !ok || entry.CallID != "c1" {
		t.Fatalf("expected web_search dedup, got entry=%+v ok=%v", entry, ok)
	}

	q3 := contract.NewToolCall("c3", "web_search", `{"query":"muhiyacode review gate","maxResults":3}`)
	if _, ok := ledger.Duplicate(q3, intact); ok {
		t.Fatalf("different query should not dedup")
	}
}

// TestInspectSignatureIsStableAcrossFieldOrder confirms that reordering the
// inspect_code JSON fields still routes to the same dedup key.
func TestInspectSignatureIsStableAcrossFieldOrder(t *testing.T) {
	original := contract.NewToolCall("c1", "inspect_code", `{"mode":"outline","path":"foo.go"}`)
	reordered := contract.NewToolCall("c2", "inspect_code", `{"path":"foo.go","mode":"outline"}`)
	if inspectSignature(original) != inspectSignature(reordered) {
		t.Fatalf("inspectSignature must be stable across field order: %q vs %q",
			inspectSignature(original), inspectSignature(reordered))
	}
}

func writeWorkspaceFile(root, name, content string) error {
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
