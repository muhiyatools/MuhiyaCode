package tui

import "testing"

// TestShellOutcome pins Fix R2: a successful run_shell row shows its output
// summary (or "ok"), never the noisy "exit code: 0"; a failure shows a tight
// "exit N"; unrecognized output falls back to the generic summary.
func TestShellOutcome(t *testing.T) {
	cases := []struct{ name, output, want string }{
		{"success with output", "exit code: 0\nBuild succeeded.\nmore", "Build succeeded."},
		{"success no output", "exit code: 0\n", "ok"},
		{"success only whitespace", "exit code: 0\n   \n\t", "ok"},
		{"success skips blank leading line", "exit code: 0\n\nreal line", "real line"},
		{"failure shows exit code", "exit code: 1\nsome error", "exit 1"},
		{"failure high code", "exit code: 127\n", "exit 127"},
		{"non-envelope falls back", "just some text\nsecond", "just some text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shellOutcome(tc.output); got != tc.want {
				t.Fatalf("shellOutcome(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

// TestRunShellRowNoNoisyExitZero pins that the collapsed run_shell row never
// renders "exit code: 0" through the full toolOutcome path.
func TestRunShellRowNoNoisyExitZero(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	ok := &toolView{name: "run_shell", state: "ok", output: "exit code: 0\ndone"}
	if got := m.toolOutcome(ok); got == "exit code: 0" {
		t.Fatalf("successful shell still renders the noisy exit code: %q", got)
	}
	if got := m.toolOutcome(ok); got != "done" {
		t.Fatalf("successful shell outcome = %q, want the output summary", got)
	}
	fail := &toolView{name: "run_shell", state: "ok", output: "exit code: 2\nboom"}
	if got := m.toolOutcome(fail); got != "exit 2" {
		t.Fatalf("failing shell outcome = %q, want \"exit 2\"", got)
	}
}
