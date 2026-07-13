package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstructions(t *testing.T, root string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ProjectInstructionsFile), data, 0o600); err != nil {
		t.Fatalf("write MUHIYA.md: %v", err)
	}
}

func TestLoadProjectInstructionsMissing(t *testing.T) {
	got := LoadProjectInstructions(t.TempDir())
	if got.State != InstructionMissing {
		t.Fatalf("state = %q, want missing", got.State)
	}
	if got.CanonicalContent != "" || got.Diagnostic != "" {
		t.Fatalf("missing must be silent and empty: %+v", got)
	}
}

func TestLoadProjectInstructionsEmptyAndWhitespace(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "whitespace": "   \n\t \n"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeInstructions(t, root, []byte(body))
			got := LoadProjectInstructions(root)
			if got.State != InstructionEmpty {
				t.Fatalf("state = %q, want empty", got.State)
			}
			if got.CanonicalContent != "" || got.Diagnostic != "" {
				t.Fatalf("empty must be silent: %+v", got)
			}
		})
	}
}

func TestLoadProjectInstructionsBOMAndNewlineNormalization(t *testing.T) {
	root := t.TempDir()
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte("line1\r\nline2\rline3\n")...)
	writeInstructions(t, root, raw)
	got := LoadProjectInstructions(root)
	if got.State != InstructionLoaded {
		t.Fatalf("state = %q, want loaded (%s)", got.State, got.Diagnostic)
	}
	want := "line1\nline2\nline3\n"
	if got.CanonicalContent != want {
		t.Fatalf("canonical = %q, want %q", got.CanonicalContent, want)
	}
	sum := sha256.Sum256([]byte(want))
	if got.ContentHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash mismatch: %s", got.ContentHash)
	}
}

func TestLoadProjectInstructionsHashStableAcrossNewlineStyle(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	writeInstructions(t, rootA, []byte("a\r\nb\r\n"))
	writeInstructions(t, rootB, []byte("a\nb\n"))
	a, b := LoadProjectInstructions(rootA), LoadProjectInstructions(rootB)
	if a.State != InstructionLoaded || b.State != InstructionLoaded {
		t.Fatalf("both should load: %q %q", a.State, b.State)
	}
	if a.ContentHash != b.ContentHash {
		t.Fatalf("hash differs across newline style: %s vs %s", a.ContentHash, b.ContentHash)
	}
}

func TestLoadProjectInstructionsExact32KiBLoads(t *testing.T) {
	root := t.TempDir()
	writeInstructions(t, root, []byte(strings.Repeat("a", MaxProjectInstructionsBytes)))
	got := LoadProjectInstructions(root)
	if got.State != InstructionLoaded {
		t.Fatalf("exact 32 KiB should load, got %q (%s)", got.State, got.Diagnostic)
	}
	if got.RawSize != MaxProjectInstructionsBytes {
		t.Fatalf("RawSize = %d, want %d", got.RawSize, MaxProjectInstructionsBytes)
	}
}

func TestLoadProjectInstructionsOversized(t *testing.T) {
	root := t.TempDir()
	writeInstructions(t, root, []byte(strings.Repeat("a", MaxProjectInstructionsBytes+1)))
	got := LoadProjectInstructions(root)
	if got.State != InstructionOversized {
		t.Fatalf("state = %q, want oversized", got.State)
	}
	if got.CanonicalContent != "" {
		t.Fatalf("oversized must not inject content")
	}
	if got.Diagnostic == "" {
		t.Fatalf("oversized must expose a diagnostic")
	}
}

func TestLoadProjectInstructionsInvalidUTF8AndNUL(t *testing.T) {
	cases := map[string][]byte{
		"invalid-utf8": {0xff, 0xfe, 0x00, 'h', 'i'},
		"embedded-nul": []byte("valid\x00text"),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeInstructions(t, root, body)
			got := LoadProjectInstructions(root)
			if got.State != InstructionInvalid {
				t.Fatalf("state = %q, want invalid", got.State)
			}
			if got.CanonicalContent != "" {
				t.Fatalf("invalid must not inject content")
			}
		})
	}
}

func TestLoadProjectInstructionsSecretRejected(t *testing.T) {
	root := t.TempDir()
	writeInstructions(t, root, []byte("Setup notes\napi_key=sk-abcdefghijklmnop1234\n"))
	got := LoadProjectInstructions(root)
	if got.State != InstructionSecretRejected {
		t.Fatalf("state = %q, want secret-rejected", got.State)
	}
	if got.CanonicalContent != "" {
		t.Fatalf("secret-rejected must not inject content")
	}
}

func TestLoadProjectInstructionsConfiguredSecretRejected(t *testing.T) {
	root := t.TempDir()
	writeInstructions(t, root, []byte("The deploy token is DEPLOYTOKEN-abcdefgh in staging.\n"))
	got := LoadProjectInstructions(root, "DEPLOYTOKEN-abcdefgh")
	if got.State != InstructionSecretRejected {
		t.Fatalf("state = %q, want secret-rejected via configured value", got.State)
	}
}

func TestLoadProjectInstructionsNonRegularFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ProjectInstructionsFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := LoadProjectInstructions(root)
	if got.State != InstructionUnreadable {
		t.Fatalf("state = %q, want unreadable for a directory", got.State)
	}
}

func TestLoadProjectInstructionsOutsideSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secretFile := filepath.Join(outside, "external.md")
	if err := os.WriteFile(secretFile, []byte("external content"), 0o600); err != nil {
		t.Fatalf("write external: %v", err)
	}
	if err := os.Symlink(secretFile, filepath.Join(root, ProjectInstructionsFile)); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}
	got := LoadProjectInstructions(root)
	if got.State != InstructionUntrustedPath {
		t.Fatalf("state = %q, want untrusted-path", got.State)
	}
	if got.CanonicalContent != "" {
		t.Fatalf("untrusted-path must not inject content")
	}
}

func TestWorkspaceKeyStableAndNormalized(t *testing.T) {
	root := t.TempDir()
	key1, err := WorkspaceKey(root)
	if err != nil {
		t.Fatalf("WorkspaceKey: %v", err)
	}
	key2, err := WorkspaceKey(root + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatalf("WorkspaceKey trailing: %v", err)
	}
	if key1 != key2 {
		t.Fatalf("workspace key not stable: %q vs %q", key1, key2)
	}
	if key1 == "" {
		t.Fatalf("workspace key must not be empty")
	}
}
