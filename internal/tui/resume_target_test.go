package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestResumeKeepsToolTarget pins Fix R1 on the render side: a resumed tool event
// carrying a persisted target renders that target in its collapsed row, so a
// reopened session names WHAT each call acted on exactly as it did live — even for
// non-path tools (shell/grep/read) whose target cannot be recovered from output.
func TestResumeKeepsToolTarget(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.loadEvents([]contract.Event{
		{Role: "tool", Type: "grep", Content: "3 matches", Target: "TODO_MARKER"},
		{Role: "tool", Type: "run_shell", Content: "exit code: 0\nok", Target: "go build ./..."},
	})
	if len(m.items) != 2 || m.items[0].tool == nil {
		t.Fatalf("resumed tool items not built: %+v", m.items)
	}
	if m.items[0].tool.target != "TODO_MARKER" {
		t.Fatalf("grep row lost its persisted target: %q", m.items[0].tool.target)
	}
	m.refreshViewport(true)
	row := stripANSI(m.renderTool(m.items[0].tool, 100))
	if !strings.Contains(row, "TODO_MARKER") {
		t.Fatalf("resumed grep row does not render its target: %q", row)
	}
	shellRow := stripANSI(m.renderTool(m.items[1].tool, 100))
	if !strings.Contains(shellRow, "go build ./...") {
		t.Fatalf("resumed shell row does not render its command: %q", shellRow)
	}
}
