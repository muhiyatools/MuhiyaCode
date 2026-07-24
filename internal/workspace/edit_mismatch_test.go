package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 008 T014/T010 (contract DG-9/DG-10, data-model §2): near-miss edit
// outcomes reclassify as tool FAILURES so the orchestrator's loop guards cover
// the edit-fumble loop, while idempotent and partially-applied outcomes stay
// successes. The note text itself must remain verbatim (the model's recovery
// guidance rides inside the error).

func mismatchWorkspace(t *testing.T, content string) *Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	trust := NewMemoryTrustStore()
	_ = trust.Trust(context.Background(), root)
	w, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	// The unread-file overwrite guard applies to edits too; real flows always
	// read before editing, so the fixture does the same.
	if _, err := w.Read(context.Background(), ReadOptions{Path: "main.go"}); err != nil {
		t.Fatal(err)
	}
	return w
}

func rawEdit(t *testing.T, old, new string, replaceAll bool) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"path": "main.go", "oldString": old, "newString": new, "replaceAll": replaceAll})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEditNotFoundIsFailure(t *testing.T) {
	w := mismatchWorkspace(t, "package main\n\nfunc real() {}\n")
	output, err := w.execEdit(context.Background(), rawEdit(t, "func imaginary() {}", "func other() {}", false))
	if err == nil {
		t.Fatalf("near-miss edit must be a failure, got success: %q", output)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "multi_edit failed") {
		t.Fatalf("error must carry the failure-classified prefix, got %q", msg)
	}
	if !strings.Contains(msg, "oldString not found") || !strings.Contains(msg, "closest region") {
		t.Fatalf("recovery note must ride inside the error, got %q", msg)
	}
}

func TestEditAmbiguousIsFailure(t *testing.T) {
	w := mismatchWorkspace(t, "x := 1\nx := 1\n")
	_, err := w.execEdit(context.Background(), rawEdit(t, "x := 1", "y := 1", false))
	if err == nil {
		t.Fatal("ambiguous edit must be a failure")
	}
	if !strings.Contains(err.Error(), "times") || !strings.Contains(err.Error(), "oldString appears") {
		t.Fatalf("ambiguity note must ride inside the error, got %q", err.Error())
	}
}

func TestEditIdempotentStaysSuccess(t *testing.T) {
	w := mismatchWorkspace(t, "package main\n\nfunc current() {}\n")
	// newString already present: the intended state exists — a correct outcome.
	if _, err := w.execEdit(context.Background(), rawEdit(t, "func old() {}", "func current() {}", false)); err != nil {
		t.Fatalf("newString-already-present must stay a success, got %v", err)
	}
	// old == new: a no-op skip, not a fumble.
	if _, err := w.execEdit(context.Background(), rawEdit(t, "func current() {}", "func current() {}", false)); err != nil {
		t.Fatalf("identical old/new must stay a success, got %v", err)
	}
}

func TestEditReplaceAllAmbiguousStaysSuccess(t *testing.T) {
	w := mismatchWorkspace(t, "x := 1\nx := 1\n")
	if _, err := w.execEdit(context.Background(), rawEdit(t, "x := 1", "y := 2", true)); err != nil {
		t.Fatalf("replaceAll over multiple matches is a normal edit, got %v", err)
	}
}

func TestMultiEditPartialApplicationFails(t *testing.T) {
	// One edit lands, one misses: since it's atomic now, it MUST fail.
	w := mismatchWorkspace(t, "alpha\nbeta\n")
	raw, err := json.Marshal(map[string]any{"path": "main.go", "edits": []map[string]any{
		{"oldString": "alpha", "newString": "ALPHA"},
		{"oldString": "gamma", "newString": "GAMMA"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := w.execMultiEdit(context.Background(), raw)
	if execErr == nil {
		t.Fatalf("partially-applied multi_edit must fail due to atomicity, got success")
	}
	if !strings.Contains(execErr.Error(), "oldString not found") {
		t.Fatalf("the miss note must still be visible in the output, got %q", execErr.Error())
	}
}

func TestMultiEditAllMissesIsFailure(t *testing.T) {
	w := mismatchWorkspace(t, "alpha\nbeta\n")
	raw, err := json.Marshal(map[string]any{"path": "main.go", "edits": []map[string]any{
		{"oldString": "gamma", "newString": "GAMMA"},
		{"oldString": "delta", "newString": "DELTA"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, execErr := w.execMultiEdit(context.Background(), raw); execErr == nil {
		t.Fatal("an all-miss multi_edit must be a failure")
	}
}

func TestMultiEditAtomicFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	os.WriteFile(file, []byte("line1\nline2\n"), 0o644)
	ws := patchWorkspace(t, root)
	if _, err := ws.Read(context.Background(), ReadOptions{Path: "file.txt"}); err != nil {
		t.Fatal(err)
	}
	_, err := ws.MultiEdit(context.Background(), "file.txt", []Edit{
		{Old: "line1", New: "new1"},
		{Old: "missing", New: "new2"},
	})
	if err == nil || !strings.Contains(err.Error(), "multi_edit failed") {
		t.Errorf("expected atomic failure, got %v", err)
	}
	content, _ := os.ReadFile(file)
	if string(content) != "line1\nline2\n" {
		t.Errorf("file was modified: %s", content)
	}
}

func TestMultiEditIdempotent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	os.WriteFile(file, []byte("new1\nnew2\n"), 0o644)
	ws := patchWorkspace(t, root)
	if _, err := ws.Read(context.Background(), ReadOptions{Path: "file.txt"}); err != nil {
		t.Fatal(err)
	}
	res, err := ws.MultiEdit(context.Background(), "file.txt", []Edit{
		{Old: "old1", New: "new1"},
		{Old: "old2", New: "new2"},
	})
	if err != nil {
		t.Errorf("expected success for idempotent, got %v", err)
	}
	if len(res.Notes) != 2 || !strings.Contains(res.Notes[0], "already present") {
		t.Errorf("expected idempotent notes, got %v", res.Notes)
	}
}

