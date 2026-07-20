package contract

import "testing"

// TestPatchTargetFile pins the unified-diff target derivation (moved from the tui
// package, where it originated as feature 004 US5).
func TestPatchTargetFile(t *testing.T) {
	cases := []struct{ name, patch, want string }{
		{"single file", "--- a/internal/x.go\n+++ b/internal/x.go\n@@ -1 +1 @@\n-a\n+b\n", "internal/x.go"},
		{"new file uses the +++ side", "--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+x\n", "new.go"},
		{"deletion falls back to the --- side", "--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", "gone.go"},
		{"multi-file gets a (+N more) suffix", "--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-a\n+b\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-c\n+d\n", "one.go (+1 more)"},
		{"malformed omitted cleanly", "not a patch at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PatchTargetFile(tc.patch); got != tc.want {
				t.Fatalf("PatchTargetFile = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestToolTarget pins the input→target extraction shared by the live TUI and the
// engine's durable-event persistence: they must agree so a resumed tool row names
// exactly what it named live.
func TestToolTarget(t *testing.T) {
	cases := []struct{ name, tool, input, want string }{
		{"shell command", "run_shell", `{"command":"go build ./..."}`, "go build ./..."},
		{"read path", "read_file", `{"path":"internal/x.go"}`, "internal/x.go"},
		{"grep pattern", "grep", `{"pattern":"TODO"}`, "TODO"},
		{"web query", "web_search", `{"query":"bubbletea v2"}`, "bubbletea v2"},
		{"subagent title", "run_subagent", `{"agent":"explore","title":"scan auth"}`, "scan auth"},
		{"apply_patch derives from diff", "apply_patch", `{"patch":"--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-a\n+b\n"}`, "f.go"},
		{"apply_patch garbage is empty", "apply_patch", `{"patch":"garbage"}`, ""},
		{"no target field", "list_directory", `{"depth":2}`, ""},
		{"malformed json is empty", "read_file", `{not json`, ""},
		{"empty string field ignored", "read_file", `{"path":""}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolTarget(tc.tool, []byte(tc.input)); got != tc.want {
				t.Fatalf("ToolTarget(%s) = %q, want %q", tc.tool, got, tc.want)
			}
		})
	}
}
