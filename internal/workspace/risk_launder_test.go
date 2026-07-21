package workspace

import "testing"

// I-2: the rm backstop used to anchor on a start/separator/space before `rm`, so
// a path prefix (`/bin/rm`), an alias-bypass backslash (`\rm`), or a subshell
// (`(rm …)`) put the verb somewhere the anchor never matched. Command-position
// tokenizing closes every one of these.
func TestClassifyShellCatchesPathAndSubshellLaunderedDelete(t *testing.T) {
	for _, command := range []string{
		"/bin/rm -rf /",
		"/usr/bin/rm -rf x",
		`\rm -rf /`,
		"(rm -rf /)",
		"; (rm -rf .)",
		"git status && (rm -rf .)",
		"RM.EXE -rf .",
		"sudo /bin/rm -rf /",
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); !risk.Blocked {
				t.Errorf("not blocked: %q", command)
			}
		})
	}
}

// I-3: the PowerShell alias branch required the literal substrings `-recurse` and
// `-force`, so the perfectly valid abbreviations `-r`/`-fo` sailed through. The
// switch matcher now accepts any unambiguous prefix of the real switch name.
func TestClassifyShellCatchesAbbreviatedPowershellDelete(t *testing.T) {
	for _, command := range []string{
		"ri -r -fo x",
		"rd -r -fo /",
		"ri -rec -force build",
		"del -Recurse -Force build",
		"Remove-Item -r -f node_modules",
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); !risk.Blocked {
				t.Errorf("not blocked: %q", command)
			}
		})
	}
}

// The prefix matcher must stay precise: a force-only or recurse-only delete is
// approval-gated, not hard-blocked, and `-force` must not be misread as recursion
// just because "force" contains an r.
func TestClassifyShellDoesNotOverblockPartialDeleteSwitches(t *testing.T) {
	for _, command := range []string{
		"ri -force file.txt",   // force only
		"ri -recurse emptydir", // recurse only
		"Remove-Item -Force one.log",
		"rd -r build", // recurse only
	} {
		t.Run(command, func(t *testing.T) {
			if risk := ClassifyShell(command); risk.Blocked {
				t.Errorf("blocked a partial (non recurse+force) delete: %q (%s)", command, risk.Reason)
			}
		})
	}
}
