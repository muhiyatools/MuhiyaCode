package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceCacheReusesContentIdentifiedOutline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.go")
	writeTestSource(t, path, "package sample\n\nfunc Alpha() {}\n")
	cache := newSourceCache(8, 1<<20)

	first, err := cache.outline(path)
	if err != nil {
		t.Fatalf("first outline: %v", err)
	}
	second, err := cache.outline(path)
	if err != nil {
		t.Fatalf("second outline: %v", err)
	}
	if first.Symbols[0].Name != "Alpha" || second.Symbols[0].Name != "Alpha" {
		t.Fatalf("unexpected outlines: %#v %#v", first.Symbols, second.Symbols)
	}
	stats := cache.stats()
	if stats.Misses != 1 || stats.Hits != 1 {
		t.Fatalf("expected one miss and one hit, got %+v", stats)
	}
}

func TestSourceCacheRejectsSameSizeSameModTimeStaleEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.go")
	original := "package sample\n\nfunc Alpha() {}\n"
	replacement := "package sample\n\nfunc Bravo() {}\n"
	if len(original) != len(replacement) {
		t.Fatal("test sources must have equal lengths")
	}
	writeTestSource(t, path, original)
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatalf("set original times: %v", err)
	}
	cache := newSourceCache(8, 1<<20)
	if _, err := cache.outline(path); err != nil {
		t.Fatalf("original outline: %v", err)
	}

	writeTestSource(t, path, replacement)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatalf("restore replacement times: %v", err)
	}
	outline, err := cache.outline(path)
	if err != nil {
		t.Fatalf("replacement outline: %v", err)
	}
	if got := outline.Symbols[0].Name; got != "Bravo" {
		t.Fatalf("stale outline returned: got %q, want Bravo", got)
	}
	stats := cache.stats()
	if stats.Hits != 0 || stats.Misses != 2 || stats.Entries != 1 {
		t.Fatalf("unexpected replacement stats: %+v", stats)
	}
}

func TestSourceCacheReturnsOwnedOutline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.go")
	writeTestSource(t, path, "package sample\n\nfunc Alpha() {}\n")
	cache := newSourceCache(8, 1<<20)
	first, err := cache.outline(path)
	if err != nil {
		t.Fatalf("first outline: %v", err)
	}
	first.Symbols[0].Name = "Corrupted"
	first.Errors = append(first.Errors, "caller mutation")

	second, err := cache.outline(path)
	if err != nil {
		t.Fatalf("second outline: %v", err)
	}
	if got := second.Symbols[0].Name; got != "Alpha" {
		t.Fatalf("cache alias allowed caller mutation: %q", got)
	}
	if len(second.Errors) != 0 {
		t.Fatalf("cache errors aliased caller data: %#v", second.Errors)
	}
}

func TestSourceCacheEvictsToConfiguredBounds(t *testing.T) {
	root := t.TempDir()
	cache := newSourceCache(1, 1<<20)
	for _, name := range []string{"one.go", "two.go"} {
		path := filepath.Join(root, name)
		writeTestSource(t, path, "package sample\n\nfunc Symbol() {}\n")
		if _, err := cache.outline(path); err != nil {
			t.Fatalf("outline %s: %v", name, err)
		}
	}
	stats := cache.stats()
	if stats.Entries != 1 || stats.Evictions != 1 {
		t.Fatalf("cache exceeded entry bound: %+v", stats)
	}
}

func writeTestSource(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
