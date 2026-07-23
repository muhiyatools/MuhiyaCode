package workspace

import "testing"

// ClassifyShell is the LAST line of defense: it is the only destructive check
// that still runs under auto-accept mode, and for the general executor subagent
// it is the only one at all. Its rm rule was newline-blind, so the single most
// iconic destructive command reached exec unmediated whenever it appeared on any
// line but the first.
func TestClassifyShellCatchesLaunderedRm(t *testing.T) {
	for _, command := range []string{
		"rm -rf .",
		"rm -rf /",
		"git status; rm -rf .",
		"git status && rm -rf .",
		// The newline family — every one of these passed before.
		"git status\nrm -rf .",
		"git status\nrm -rf /",
		"echo hi\r\nrm -rf /",
		"go build ./...\nrm -rf .",
		"git status\nrm --recursive --force /",
		// Wrappers, which put rm after a space rather than a separator.
		"sudo rm -rf /",
		"env rm -rf .",
		"xargs rm -rf /",
		"nohup rm -rf .",
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); !risk.Blocked {
				t.Errorf("not blocked: %q", command)
			}
		})
	}
}

// The widened separator class is only safe because quoted spans are blanked
// first. Without that, searching for the STRING "rm -rf" would be refused as if
// it were the command — the false-positive class that made the original
// allowlist parser unusable.
func TestClassifyShellDoesNotBlockQuotedRmAsData(t *testing.T) {
	for _, command := range []string{
		`git log --grep "rm -rf"`,
		`grep -r "rm -rf --no-preserve-root" .`,
		`rg 'rm -rf' internal/`,
		`echo "how to rm -rf safely"`,
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); risk.Blocked {
				t.Errorf("blocked a search for quoted text, not a command: %q (%s)", command, risk.Reason)
			}
		})
	}
}

// Ordinary work must stay unblocked; a backstop that blocks builds is one users
// will turn off.
func TestClassifyShellLeavesRealWorkAlone(t *testing.T) {
	for _, command := range []string{
		"go build ./...",
		"go test ./...\ngo vet ./...",
		"git status\ngit diff",
		"npm ci && npm test",
		"rm file.txt", // a plain delete is approval-gated, not hard-blocked
		"rm -r build", // recursive WITHOUT force
		"cat a.txt\ncat b.txt",
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); risk.Blocked {
				t.Errorf("blocked legitimate work: %q (%s)", command, risk.Reason)
			}
		})
	}
}
