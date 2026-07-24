package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// C-3: a long line truncated at byte 500 splits a multi-byte rune, emitting an
// invalid UTF-8 fragment that marshals to U+FFFD. Truncation must fall on a rune
// boundary.
func TestReadTruncatesLongLineOnRuneBoundary(t *testing.T) {
	root := t.TempDir()
	longLine := strings.Repeat("—", 600) // 600 runes / 1800 bytes; byte 500 is mid-em-dash
	file := filepath.Join(root, "wide.txt")
	if err := os.WriteFile(file, []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	res, err := ws.Read(context.Background(), ReadOptions{Path: "wide.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) == 0 {
		t.Fatal("no lines returned")
	}
	got := res.Lines[0]
	if !utf8.ValidString(got) {
		t.Fatalf("read produced invalid UTF-8 (byte-sliced mid-rune): %q", got)
	}
	if strings.ContainsRune(got, '\ufffd') {
		t.Fatalf("read produced a U+FFFD replacement char: %q", got)
	}
}

func TestReadFullLines(t *testing.T) {
	root := t.TempDir()
	longLine := strings.Repeat("A", 600)
	file := filepath.Join(root, "wide.txt")
	if err := os.WriteFile(file, []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	
	res, err := ws.Read(context.Background(), ReadOptions{Path: "wide.txt", FullLines: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines[0]) >= 600 {
		t.Errorf("expected line to be truncated, got len %d", len(res.Lines[0]))
	}

	res, err = ws.Read(context.Background(), ReadOptions{Path: "wide.txt", FullLines: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines[0]) < 600 {
		t.Errorf("expected line to NOT be truncated, got len %d", len(res.Lines[0]))
	}
}
