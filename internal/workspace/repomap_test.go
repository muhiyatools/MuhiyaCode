package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateRepoMap(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root, Options{})
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Create sample Go and TS files
	goFile := filepath.Join(root, "main.go")
	os.WriteFile(goFile, []byte("package main\n\nfunc RunApp() {}\ntype Config struct{}\n"), 0644)

	tsFile := filepath.Join(root, "service.ts")
	os.WriteFile(tsFile, []byte("export function fetchData() {}\nexport interface User {}\n"), 0644)

	repoMap, err := ws.GenerateRepoMap(root, 500)
	if err != nil {
		t.Fatalf("GenerateRepoMap failed: %v", err)
	}

	if !strings.Contains(repoMap, "Workspace Scope Map") {
		t.Errorf("expected header in repo map, got:\n%s", repoMap)
	}

	if !strings.Contains(repoMap, "main.go") || !strings.Contains(repoMap, "service.ts") {
		t.Errorf("expected main.go and service.ts in repo map, got:\n%s", repoMap)
	}

	if _, err := ws.GenerateRepoMap(root, 500); err != nil {
		t.Fatalf("second GenerateRepoMap failed: %v", err)
	}
	stats := ws.SourceCacheStats()
	if stats.Hits == 0 {
		t.Fatalf("expected repeated repo map to reuse cached outlines, got %+v", stats)
	}
}

func TestRankedRepoMapPrioritizesQueryMatches(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "deep", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.go"), []byte("package x\nfunc Other() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deep", "nested", "auth.go"), []byte("package x\nfunc ValidateToken() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repoMap, err := ws.GenerateRankedRepoMap(root, 500, "token auth")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(repoMap, "auth.go") > strings.Index(repoMap, "root.go") {
		t.Fatalf("query-relevant file was not ranked first:\n%s", repoMap)
	}
}
