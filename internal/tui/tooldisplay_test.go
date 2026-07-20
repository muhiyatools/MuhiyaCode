package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

// TestAskUserRendersTheExchangeNotItsJSON is the field-test regression: the
// ask_user tool result is the MODEL-facing payload (verbose JSON the model
// reads well), and rendering it verbatim put a wall of braces in the
// transcript. The row now shows the question and the choice the user made.
func TestAskUserRendersTheExchangeNotItsJSON(t *testing.T) {
	payload := `{"type":"user_answers","answers":[` +
		`{"question":"What kind of Excel online tool do you want?","selected_index":0,"selected_label":"View & edit uploaded xlsx","selected_description":"A web app that opens .xlsx files.","recommended":false},` +
		`{"question":"How should it be served?","selected_index":1,"selected_label":"Single HTML + SheetJS","selected_description":"Zero install.","recommended":false}]}`
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	tool := &toolView{name: "ask_user", state: "ok", output: payload}

	collapsed := ansi.Strip(m.renderTool(tool, 100))
	if !strings.Contains(collapsed, "2 answered") {
		t.Fatalf("collapsed row does not summarize the exchange:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "{") || strings.Contains(collapsed, "selected_index") {
		t.Fatalf("raw JSON leaked into the collapsed row:\n%s", collapsed)
	}

	tool.expanded = true
	expanded := ansi.Strip(m.renderTool(tool, 100))
	for _, want := range []string{
		"What kind of Excel online tool do you want?",
		"View & edit uploaded xlsx",
		"A web app that opens .xlsx files.",
		"How should it be served?",
		"Single HTML + SheetJS",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("expanded row missing %q:\n%s", want, expanded)
		}
	}
	// Model bookkeeping must never reach the screen.
	for _, forbidden := range []string{"selected_index", "selected_label", "recommended", `"type"`, "{"} {
		if strings.Contains(expanded, forbidden) {
			t.Fatalf("payload internals leaked into the expanded row (%q):\n%s", forbidden, expanded)
		}
	}
}

// TestAskUserFallsBackOnUnparseableOutput: an unexpected payload must degrade
// to the generic rendering, never crash or render a half-parsed exchange.
func TestAskUserFallsBackOnUnparseableOutput(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, output := range []string{"", "not json at all", `{"type":"something_else"}`, `{"type":"user_answers","answers":[]}`} {
		tool := &toolView{name: "ask_user", state: "ok", output: output, expanded: true}
		if got := m.renderTool(tool, 100); got == "" {
			t.Fatalf("renderTool produced nothing for output %q", output)
		}
	}
}
