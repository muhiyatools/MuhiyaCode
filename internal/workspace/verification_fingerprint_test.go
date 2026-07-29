package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVerificationFingerprintChangesWithSourceAndIgnoresBuildOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "main.go")
	if err := os.WriteFile(source, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := VerificationFingerprint(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := VerificationFingerprint(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("source edit did not invalidate verification fingerprint")
	}
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "artifact.bin"), []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := VerificationFingerprint(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if second != third {
		t.Fatal("ignored build output invalidated verification fingerprint")
	}
}
