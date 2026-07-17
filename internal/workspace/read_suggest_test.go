package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Reading a missing file must suggest workspace files with the same base name
// (case-insensitive), so the model can self-correct a wrongly guessed
// directory instead of retrying invented paths.

func suggestWorkspace(t *testing.T) *Workspace {
	t.Helper()
	root := t.TempDir()
	actual := filepath.Join(root, "src", "utils", "exportEngine.ts")
	if err := os.MkdirAll(filepath.Dir(actual), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actual, []byte("export const engine = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trust := NewMemoryTrustStore()
	_ = trust.Trust(context.Background(), root)
	w, err := New(root, Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestReadNotFoundSuggestsBasenameMatch(t *testing.T) {
	w := suggestWorkspace(t)
	_, err := w.Read(context.Background(), ReadOptions{Path: filepath.Join("src", "components", "Export", "exportEngine.ts")})
	if err == nil {
		t.Fatal("expected read of missing file to fail")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("error lacks did-you-mean hint: %v", err)
	}
	if !strings.Contains(err.Error(), "src/utils/exportEngine.ts") {
		t.Fatalf("error lacks suggested workspace-relative path: %v", err)
	}
}

func TestReadNotFoundSuggestsCaseInsensitiveMatch(t *testing.T) {
	w := suggestWorkspace(t)
	_, err := w.Read(context.Background(), ReadOptions{Path: filepath.Join("src", "ExportEngine.TS")})
	if err == nil {
		t.Fatal("expected read of missing file to fail")
	}
	if !strings.Contains(err.Error(), "src/utils/exportEngine.ts") {
		t.Fatalf("case-insensitive basename match missing: %v", err)
	}
}

func TestReadNotFoundWithoutMatchKeepsPlainError(t *testing.T) {
	w := suggestWorkspace(t)
	_, err := w.Read(context.Background(), ReadOptions{Path: filepath.Join("src", "index.tsx")})
	if err == nil {
		t.Fatal("expected read of missing file to fail")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("unexpected suggestion for basename with no match: %v", err)
	}
}
