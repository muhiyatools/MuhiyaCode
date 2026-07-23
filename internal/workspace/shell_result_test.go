package workspace

import (
	"strings"
	"testing"
	"time"
)

// C-4: a timed-out or cancelled run_shell must say so — with the elapsed time and
// that the process tree was killed — instead of an ambiguous bare "exit code: N".
// A timeout also gets a recognized failure prefix so the loop guard engages.
func TestFormatShellResultSurfacesTimeoutAndCancel(t *testing.T) {
	timedOut := formatShellResult(ShellResult{ExitCode: 143, Output: "partial", TimedOut: true, Duration: 30 * time.Second})
	if !strings.HasPrefix(timedOut, "tool failed:") {
		t.Errorf("timeout output must lead with a recognized failure prefix so IsToolFailure engages: %q", timedOut)
	}
	if !strings.Contains(timedOut, "30s") || !strings.Contains(timedOut, "killed") {
		t.Errorf("timeout output must name the elapsed time and the kill: %q", timedOut)
	}
	if !strings.Contains(timedOut, "exit code: 143") {
		t.Errorf("timeout output must still carry the exit envelope: %q", timedOut)
	}

	cancelled := formatShellResult(ShellResult{ExitCode: 130, Output: "", Cancelled: true, Duration: 1500 * time.Millisecond})
	if !strings.Contains(cancelled, "cancelled") || !strings.Contains(cancelled, "1.5s") {
		t.Errorf("cancel output must name the cancellation and elapsed time: %q", cancelled)
	}

	normal := formatShellResult(ShellResult{ExitCode: 0, Output: "ok"})
	if normal != "exit code: 0\nok" {
		t.Errorf("a normal result must keep the plain exit envelope: %q", normal)
	}
}
