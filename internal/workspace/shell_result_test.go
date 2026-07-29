package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// C-4: a timed-out or cancelled run_shell must say so — with the elapsed time and
// that the process tree was killed — instead of an ambiguous bare "exit code: N".
// The model-facing prefix is diagnostic; typed errors drive control flow.
func TestFormatShellResultSurfacesTimeoutAndCancel(t *testing.T) {
	timedOut := formatShellResult(ShellResult{ExitCode: 143, Output: "partial", TimedOut: true, Duration: 30 * time.Second})
	if !strings.HasPrefix(timedOut, "tool failed:") {
		t.Errorf("timeout output must identify the failure: %q", timedOut)
	}
	if !strings.Contains(timedOut, "30s") || !strings.Contains(timedOut, "killed") {
		t.Errorf("timeout output must name the elapsed time and the kill: %q", timedOut)
	}
	if !strings.Contains(timedOut, "exit code: 143") {
		t.Errorf("timeout output must still carry the exit envelope: %q", timedOut)
	}

	cancelled := formatShellResult(ShellResult{ExitCode: 130, Output: "", Cancelled: true, Duration: 1500 * time.Millisecond})
	if !strings.HasPrefix(cancelled, "tool failed:") || !strings.Contains(cancelled, "cancelled") || !strings.Contains(cancelled, "1.5s") {
		t.Errorf("cancel output must name the cancellation and elapsed time: %q", cancelled)
	}

	nonzero := formatShellResult(ShellResult{ExitCode: 1, Output: "test failure"})
	if !strings.HasPrefix(nonzero, "tool failed:") || !strings.Contains(nonzero, "exit code: 1") || !strings.Contains(nonzero, "test failure") {
		t.Errorf("a non-zero result must be classified as a failure without losing its output: %q", nonzero)
	}

	normal := formatShellResult(ShellResult{ExitCode: 0, Output: "ok"})
	if normal != "exit code: 0\nok" {
		t.Errorf("a normal result must keep the plain exit envelope: %q", normal)
	}
}

func TestShellResultErrorDrivesTypedOutcome(t *testing.T) {
	cases := []struct {
		name   string
		shell  ShellResult
		status contract.ToolOutcomeStatus
	}{
		{name: "success", shell: ShellResult{}, status: contract.ToolOutcomeSucceeded},
		{name: "nonzero", shell: ShellResult{ExitCode: 2}, status: contract.ToolOutcomeFailed},
		{name: "timeout", shell: ShellResult{TimedOut: true}, status: contract.ToolOutcomeFailed},
		{name: "cancelled", shell: ShellResult{Cancelled: true}, status: contract.ToolOutcomeCancelled},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			outcome := contract.AdaptToolResult("", shellResultError(testCase.shell))
			if outcome.Status != testCase.status {
				t.Fatalf("status = %s, want %s", outcome.Status, testCase.status)
			}
			if testCase.shell.Cancelled && !errors.Is(outcome.Err, context.Canceled) {
				t.Fatalf("cancellation identity was lost: %v", outcome.Err)
			}
		})
	}
}
