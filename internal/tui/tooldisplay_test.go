package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestToolOutcomeDerivation (003 T023/T025) checks the per-tool collapsed outcome
// measures and the diff-count edge cases.
func TestToolOutcomeDerivation(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	diff := "\n--- diff ---\n--- a/x\n+++ b/x\n@@ -1 +1,2 @@\n-old\n+new1\n+new2"
	cases := []struct {
		name string
		tool *toolView
		want string
	}{
		{"edit +/-", &toolView{name: "edit_file", state: "ok", output: "edited" + diff}, "+2 −1"},
		{"read lines", &toolView{name: "read_file", state: "ok", output: "line1\nline2\nline3"}, "3 lines"},
		{"grep matches", &toolView{name: "grep", state: "ok", output: "a.go:1: hit\nb.go:2: hit"}, "2 matches"},
		{"glob entries", &toolView{name: "glob", state: "ok", output: "a.go\nb.go\nc.go"}, "3 entries"},
		{"no diff falls back to summary", &toolView{name: "edit_file", state: "ok", output: "patched cleanly"}, "patched cleanly"},
		{"running", &toolView{name: "grep", state: "running"}, "running…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.toolOutcome(tc.tool); got != tc.want {
				t.Fatalf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestToolCollapsedIsOneLineExpandedShowsDetail (003 T024/FR-006/007/008).
func TestToolCollapsedIsOneLineExpandedShowsDetail(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	diff := "\n--- diff ---\n@@ -1 +1 @@\n-old line\n+new line"
	tool := &toolView{name: "edit_file", target: "main.go", state: "ok", output: "edited" + diff}

	// Collapsed: exactly one line, no diff body.
	tool.expanded = false
	collapsed := m.renderTool(tool, 90)
	if strings.Contains(collapsed, "\n") {
		t.Fatalf("collapsed tool entry spans multiple lines:\n%s", collapsed)
	}
	if strings.Contains(stripANSI(collapsed), "new line") {
		t.Fatalf("collapsed entry leaked diff content:\n%s", collapsed)
	}
	if !strings.Contains(stripANSI(collapsed), "+1 −1") {
		t.Fatalf("collapsed entry missing outcome measure:\n%s", stripANSI(collapsed))
	}

	// Expanded (this tool's own toggle): same header plus the diff body.
	tool.expanded = true
	expanded := m.renderTool(tool, 90)
	if !strings.Contains(stripANSI(expanded), "new line") {
		t.Fatalf("expanded entry missing diff detail:\n%s", expanded)
	}
}
