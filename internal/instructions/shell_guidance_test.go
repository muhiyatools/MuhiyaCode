package instructions

import (
	"strings"
	"testing"
)

// TestShellCommandGuidanceIsConcreteAndShellSpecific pins issue #2: the ENVIRONMENT
// command guidance must be concrete per shell (naming the idioms to use and the
// POSIX ones that fail), not a generic "use compatible commands" — that generic
// line did not stop models reaching for mkdir -p / touch / 2>/dev/null on Windows.
func TestShellCommandGuidanceIsConcreteAndShellSpecific(t *testing.T) {
	ps := ShellCommandGuidance("powershell")
	for _, want := range []string{"PowerShell", "New-Item", "mkdir -p", "2>/dev/null"} {
		if !strings.Contains(ps, want) {
			t.Fatalf("PowerShell guidance must mention %q: %q", want, ps)
		}
	}
	if ShellCommandGuidance("pwsh") != ps {
		t.Fatal("pwsh and powershell must share the PowerShell guidance")
	}
	if c := ShellCommandGuidance("cmd"); !strings.Contains(c, "cmd.exe") {
		t.Fatalf("cmd guidance must name cmd.exe: %q", c)
	}
	if d := ShellCommandGuidance("bash"); d != ShellGuidancePosix {
		t.Fatalf("an unknown/POSIX shell gets the lean default, got %q", d)
	}
	// The guidance is a package constant, not a per-call composition, which is
	// what keeps it byte-identical across turns (Constitution III). Comparing a
	// call against itself would prove nothing; comparing it against the constant
	// proves nothing dynamic can be spliced in.
	if ps != ShellGuidancePowerShell {
		t.Fatalf("guidance must be the fixed constant, got %q", ps)
	}
	if c := ShellCommandGuidance("cmd.exe"); c != ShellGuidanceCmd {
		t.Fatalf("cmd.exe must resolve to the fixed cmd constant, got %q", c)
	}
}
