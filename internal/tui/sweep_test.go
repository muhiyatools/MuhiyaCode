package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// 004 US1 (T013): when a task ends, any tool still marked "running" is
// reconciled to a terminal state so nothing keeps spinning; finished items are
// left untouched, and the sweep is idempotent. (The nested subagent half of
// this test retired with the agent cards it swept.)
func TestTaskEndSweepCancelsRunningWork(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.items = append(m.items,
		item{kind: "tool", tool: &toolView{name: "read_file", state: "running"}},
		item{kind: "tool", tool: &toolView{name: "grep", state: "ok"}},
		item{kind: "tool", tool: &toolView{name: "write_file", state: "fail"}},
	)

	m = mustUpdate(t, m, statsMsg(contract.TaskStats{}))

	if got := m.items[0].tool.state; got != "cancelled" {
		t.Fatalf("running tool not cancelled at task end: %q", got)
	}
	if got := m.items[1].tool.state; got != "ok" {
		t.Fatalf("finished tool was disturbed by the sweep: %q", got)
	}
	if got := m.items[2].tool.state; got != "fail" {
		t.Fatalf("failed tool was disturbed by the sweep: %q", got)
	}

	// Idempotent: a second sweep changes nothing.
	m.sweepRunningWork()
	if m.items[1].tool.state != "ok" || m.items[0].tool.state != "cancelled" {
		t.Fatal("second sweep was not idempotent")
	}
}
