package instructions

import (
	"strings"
	"testing"
)

// The Phase-2 cache-epoch clauses (D-1/D-3/D-4). These pin the intent of the
// prefix change beyond the byte-for-byte goldens, so a future edit that drops a
// clause fails with a clear reason rather than only a golden mismatch.
func TestEpochClausesPresent(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"D-4 edit exactness/recovery", ToolEditFileDescription, "match exactly once"},
		{"D-4 edit nearest-region recovery", ToolEditFileDescription, "nearest region"},
		{"D-3 run_shell cwd non-persistence", ToolRunShellDescription, "runs fresh at the workspace root"},
		// D-1's safety clauses were delivered to the execution agent while one
		// existed. The agent that runs commands is the main session now, so the
		// clauses moved into its prompt — they must never be lost with the
		// machinery that used to carry them.
		{"D-1 preserves user work", PromptToolsAndRecoveryBody, "Preserve the user's uncommitted work"},
		{"D-1 avoids destructive commands", PromptToolsAndRecoveryBody, "git reset --hard"},
		{"D-1 verifies targets", PromptToolsAndRecoveryBody, "confirm its target exists"},
		// The unified session's own contract: it does the work, and stops when done.
		{"unified execution", PromptOperatingContractBody, "You do the work yourself"},
		{"stop at done", PromptOperatingContractBody, "answer and stop"},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.text, tc.want) {
			t.Errorf("%s: expected clause %q missing", tc.name, tc.want)
		}
	}
}
