package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/instructions"
)

// TestReadOnlyShellMatchesDocumentedExamples (T034) locks the shell-gate PROSE to
// its ENFORCEMENT: every example the instructions advertise must be classified by
// IsReadOnlyShell exactly as documented. If the gate logic or the advertised
// examples ever drift apart, this fails — the sync discipline the D1 incident (a
// gate that blocked commands the prose implied were fine) needed.
func TestReadOnlyShellMatchesDocumentedExamples(t *testing.T) {
	if len(instructions.ReadOnlyShellExamples) == 0 {
		t.Fatal("no documented shell examples to check against")
	}
	for _, example := range instructions.ReadOnlyShellExamples {
		if got := IsReadOnlyShell(example.Command); got != example.Allowed {
			t.Errorf("IsReadOnlyShell(%q) = %v, but the docs say Allowed=%v", example.Command, got, example.Allowed)
		}
	}
}

// TestReadOnlyShellProseNamesRealExamples keeps the human-readable allowlist body
// honest: the concrete commands it cites as allowed must actually pass the gate.
func TestReadOnlyShellProseNamesRealExamples(t *testing.T) {
	// Commands the prose body cites (in its "e.g." list) as things a read-only
	// agent may run.
	for _, cited := range []string{"go build ./...", "npm test"} {
		if !strings.Contains(instructions.ReadOnlyShellAllowlistBody, cited) {
			t.Fatalf("prose no longer cites %q — update the sync examples", cited)
		}
	}
	if !IsReadOnlyShell("cd path && go build ./...") || !IsReadOnlyShell("git status && npm test") {
		t.Fatal("a command the prose cites as allowed is blocked by the gate")
	}
}
