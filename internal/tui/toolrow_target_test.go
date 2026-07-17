package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// 004 US5 (T045): the write/edit tool row names its target file, and paths are
// middle-truncated so the filename survives on narrow terminals. The target
// DERIVATION logic itself now lives in and is tested by internal/contract
// (ToolTarget / PatchTargetFile); these tests cover the tui RENDER of it.

// The collapsed row renders label → path → counts, in that order, for every
// file-mutation tool including apply_patch.
func TestToolRowPathBeforeCounts(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	diff := "\n--- diff ---\n@@ -1 +1 @@\n-old\n+new"
	for _, name := range []string{"write_file", "edit_file", "multi_edit"} {
		tool := &toolView{name: name, target: "internal/tui/view.go", state: "ok", output: "x" + diff}
		row := stripANSI(m.renderTool(tool, 100))
		pathIdx := strings.Index(row, "view.go")
		countIdx := strings.Index(row, "+1")
		if pathIdx < 0 {
			t.Fatalf("%s row missing the file path: %q", name, row)
		}
		if countIdx < 0 {
			t.Fatalf("%s row missing the +/- counts: %q", name, row)
		}
		if pathIdx > countIdx {
			t.Fatalf("%s row shows counts before the path: %q", name, row)
		}
	}
}

// TestToolRowPathDerivedFromDiffWhenTargetUnset (006) proves a resumed/paged Edit
// row — which carries no live target — still names its file in the MAIN line by
// deriving it from the diff's file headers, not only inside the expanded detail.
func TestToolRowPathDerivedFromDiffWhenTargetUnset(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	diff := "\n--- diff ---\n--- a/internal/app/main.go\n+++ b/internal/app/main.go\n@@ -1 +1 @@\n-old\n+new"
	tool := &toolView{name: "edit_file", state: "ok", output: "edited" + diff} // note: no target
	row := stripANSI(m.renderTool(tool, 100))
	if !strings.Contains(row, "main.go") {
		t.Fatalf("path not derived from the diff for a targetless edit row: %q", row)
	}
	if strings.Contains(row, "\n") {
		t.Fatalf("collapsed row should be a single line: %q", row)
	}
}

func TestTruncateMiddlePreservesFilename(t *testing.T) {
	full := "internal/orchestrator/engine.go"
	out := truncateMiddle(full, 20, "…")
	if n := len([]rune(out)); n > 20 {
		t.Fatalf("truncateMiddle exceeded width: %q (%d runes)", out, n)
	}
	if !strings.HasSuffix(out, "engine.go") {
		t.Fatalf("filename not preserved: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("ellipsis missing: %q", out)
	}
	// A value that already fits is returned unchanged.
	if got := truncateMiddle("a.go", 20, "…"); got != "a.go" {
		t.Fatalf("short value was altered: %q", got)
	}
	// Even at the 12-cell floor the filename stays whole when it fits.
	tight := truncateMiddle("internal/tui/render.go", 12, "…")
	if !strings.HasSuffix(tight, "render.go") {
		t.Fatalf("filename lost at narrow width: %q", tight)
	}
}
