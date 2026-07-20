package orchestrator

import "testing"

// Every string here was ALLOWED by IsReadOnlyShell before the 2026-07-20
// security review, and every one of them executes as two commands in the shells
// run_shell actually spawns (`sh -c` / `powershell -Command`). A read-only
// subagent — architecturally forbidden from mutating anything — could run any of
// them, and the plan/execute role gate inherited the same hole when it started
// routing through this function.
//
// Four independent defects produced them:
//
//	newline was whitespace, not a separator, so every word after a line break
//	  was classified as an argument and the destructive-word check skips arguments
//	wrappers (env/sudo/xargs) consumed command position, demoting the real verb
//	interpreters with -c hid their payload inside one opaque quoted token
//	no normalization, so \rm and /bin/rm missed the map, and git's global flags
//	  hid the subcommand from a check that only looked one token ahead
func TestReadOnlyShellRejectsKnownBypasses(t *testing.T) {
	for _, command := range []string{
		// Newline as a command separator, in every shell dialect.
		"git status\nrm -rf .",
		"ls\nRemove-Item -Recurse -Force .",
		"echo hi\r\nrm -rf /",
		"ls\nmv a b",
		"ls\nsed -i s/a/b/ f",
		"ls\ngit commit -am x",
		"ls\ntouch newfile",
		"ls\nkill -9 1",
		// Wrappers that run another program.
		"env rm -rf .",
		"sudo rm -rf /",
		"xargs rm -rf",
		"nice rm -rf .",
		"timeout 5 rm -rf .",
		"env FOO=bar rm -rf .",
		"sudo env xargs rm -rf .", // chained wrappers
		// Interpreters carrying inline code.
		`bash -c "rm -rf /tmp/x"`,
		`sh -c 'rm -rf /tmp/x'`,
		`powershell -Command "Remove-Item -Recurse -Force ."`,
		`bash -c "echo pwned > /etc/passwd"`,
		`python -c "import shutil; shutil.rmtree('.')"`,
		`node -e "require('fs').rmSync('.',{recursive:true})"`,
		// Path and alias-escape spellings of the same program.
		`\rm -rf .`,
		"/bin/rm -rf .",
		"./rm -rf .",
		// git global flags hiding a mutating subcommand.
		"git -C . commit -m x",
		"git -c user.name=x commit -m y",
		"git --git-dir=.git push",
	} {
		t.Run(command, func(t *testing.T) {
			if IsReadOnlyShell(command) {
				t.Errorf("classified as read-only, but it mutates: %q", command)
			}
		})
	}
}

// The counterweight. A denylist that blocks real work is worse than useless — it
// drove the false-positive wave that replaced the original allowlist parser. A
// read-only agent must still be able to inspect, build, and test freely.
func TestReadOnlyShellStillAllowsRealWork(t *testing.T) {
	for _, command := range []string{
		"go build ./...",
		"go test ./internal/orchestrator",
		"cd path && go build ./...",
		"git status && npm test",
		"git log --oneline -20",
		"git diff HEAD~1",
		"git -C . status",               // read-only subcommand behind a global flag
		"git -c core.pager=cat log",     // ditto
		"grep format main.go",           // destructive word as an ARGUMENT
		`git log --grep "rm -rf"`,       // destructive words as quoted data
		"rg kill internal/",             //
		"dir /s /b 2>nul | head",        //
		"cat a.txt\ncat b.txt",          // newline chaining harmless commands
		"ls -la\ngo vet ./...",          //
		"echo hi > /dev/null",           // null redirect is not a file write
		"go test ./... 2>&1",            // fd duplication
		"python script.py",              // interpreter running a FILE, no inline code
		"node server.js",                //
		"timeout 5 go test ./...",       // wrapper in front of a safe program
		"env GOFLAGS=-mod=mod go build", // wrapper with an assignment
	} {
		t.Run(command, func(t *testing.T) {
			if !IsReadOnlyShell(command) {
				t.Errorf("blocked a legitimate read-only command: %q", command)
			}
		})
	}
}
