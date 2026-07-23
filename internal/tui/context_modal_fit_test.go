package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// H-1: the /context essentials card is a dozen lines. At 80×24 the info modal
// used to truncate its last rows with no way to scroll to them. It must now fit.
func TestContextModalShowsAllRowsAt80x24(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)
	model.modal = &modalState{
		title:   "Context",
		message: formatContextReport(contextPanelReport(), "Reasonix Pro"),
	}
	// Render through the real View path so the reserve/clamp (H-3) is exercised
	// too, not just renderModal's own budget.
	view := model.View().Content
	for _, want := range []string{
		"In use:", "Hit rate:", "Running:   Reasonix Pro",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("context modal at 80×24 dropped %q:\n%s", want, view)
		}
	}
}
