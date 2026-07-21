package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// C-2: editing one line of a mixed-ending file must change ONLY the edited
// region. The old code re-encoded every \n to \r\n whenever the file contained
// any CRLF, so a one-line edit rewrote every other line's terminator.
func TestEditPreservesMixedLineEndings(t *testing.T) {
	root := t.TempDir()
	original := "alpha\nbeta\r\ngamma\ndelta\n" // mostly LF, one CRLF line
	file := filepath.Join(root, "m.txt")
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "m.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Edit(ctx, "m.txt", Edit{Old: "gamma", New: "GAMMA"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	want := "alpha\nbeta\r\nGAMMA\ndelta\n" // beta's CRLF preserved; only gamma changed
	if string(got) != want {
		t.Fatalf("mixed-ending edit corrupted untouched line endings:\n got=%q\nwant=%q", got, want)
	}
}

// A uniformly-CRLF file keeps CRLF on the edited line too (dominant ending).
func TestEditPreservesUniformCRLF(t *testing.T) {
	root := t.TempDir()
	original := "one\r\ntwo\r\nthree\r\n"
	file := filepath.Join(root, "c.txt")
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := patchWorkspace(t, root)
	ctx := context.Background()
	if _, err := ws.Read(ctx, ReadOptions{Path: "c.txt"}); err != nil {
		t.Fatal(err)
	}
	// Replace a whole line including its content; the new line adopts CRLF.
	if _, err := ws.Edit(ctx, "c.txt", Edit{Old: "two", New: "TWO"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if want := "one\r\nTWO\r\nthree\r\n"; string(got) != want {
		t.Fatalf("uniform CRLF not preserved:\n got=%q\nwant=%q", got, want)
	}
}
