package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// diffCountRE matches the "+N −N" collapsed diff outcome that file-edit rows
// render — a Memory row must never produce it (MT-13).
var diffCountRE = regexp.MustCompile(`\+\d+ −\d+`)

// TestMemoryRowRendering (008 T031, contracts/memory-tool.md MT-11..13): a
// save_memory call renders as its own labeled "Memory" row — collapsed line
// names the saved title, the outcome measure is the entry state ("saved"),
// and no diff count ever appears.
func TestMemoryRowRendering(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	tool := &toolView{name: "save_memory", state: "ok", target: "pref", output: "saved\n- **pref**: user prefers X"}
	row := stripANSI(m.renderTool(tool, 100))
	if strings.Contains(row, "\n") {
		t.Fatalf("collapsed Memory row spans multiple lines:\n%s", row)
	}
	if !strings.Contains(row, "Memory") {
		t.Fatalf("save_memory row lost its Memory label: %q", row)
	}
	if strings.Contains(row, "Save Memory") || strings.Contains(row, "Write") {
		t.Fatalf("save_memory rendered as a generic tool row: %q", row)
	}
	if !strings.Contains(row, "pref") {
		t.Fatalf("collapsed row does not name the saved title: %q", row)
	}
	if !strings.Contains(row, "saved") {
		t.Fatalf("outcome measure is not the entry state: %q", row)
	}
	if diffCountRE.MatchString(row) {
		t.Fatalf("Memory row rendered a diff count: %q", row)
	}

	// MT-12: the expanded view shows the full saved entry.
	tool.expanded = true
	expanded := stripANSI(m.renderTool(tool, 100))
	if !strings.Contains(expanded, "user prefers X") {
		t.Fatalf("expanded Memory row missing the saved entry:\n%s", expanded)
	}
}

// TestMemoryRowAlreadyKnownAndFailure: a dedupe hit reports "already known" as
// the outcome (MT-13 via MT-5), and failed saves keep the standard danger
// styling instead of an entry state.
func TestMemoryRowAlreadyKnownAndFailure(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	known := &toolView{name: "save_memory", state: "ok", target: "pref", output: "already known"}
	if got := m.toolOutcome(known); got != "already known" {
		t.Fatalf("dedupe outcome = %q, want %q", got, "already known")
	}

	failed := &toolView{name: "save_memory", state: "fail", output: "tool save_memory failed: entry too large"}
	rendered := m.renderTool(failed, 100)
	if !strings.Contains(rendered, m.palette.danger.Render(oneLine("tool save_memory failed: entry too large", 33))) {
		t.Fatalf("failed save lost the standard danger outcome:\n%q", rendered)
	}
}

// TestMemoryRowNeverRendersDiffCounts: even an output that embeds a
// diff-looking payload must keep the entry-state outcome — save_memory is
// structurally excluded from the "+N −N" tool class (MT-13).
func TestMemoryRowNeverRendersDiffCounts(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	tool := &toolView{name: "save_memory", state: "ok", target: "pref", output: "saved\n--- diff ---\n@@ -1 +1 @@\n-old\n+new"}
	if got := m.toolOutcome(tool); got != "saved" {
		t.Fatalf("outcome = %q, want %q", got, "saved")
	}
	if row := stripANSI(m.renderTool(tool, 100)); diffCountRE.MatchString(row) {
		t.Fatalf("Memory row rendered a diff count: %q", row)
	}
}

// TestMemoryRowTitleFallback: with no captured title, the collapsed row falls
// back to the first words of the saved entry — never the bare state word twice.
func TestMemoryRowTitleFallback(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	tool := &toolView{name: "save_memory", state: "ok", output: "saved\n- user prefers tabs over spaces"}
	row := stripANSI(m.renderTool(tool, 100))
	if !strings.Contains(row, "user prefers tabs") {
		t.Fatalf("fallback title missing from collapsed row: %q", row)
	}
	if strings.Count(row, "saved") != 1 {
		t.Fatalf("state word duplicated into the title segment: %q", row)
	}
}
