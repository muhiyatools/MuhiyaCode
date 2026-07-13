package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// 004 US1 (T013): when a task ends, any tool or subagent still marked "running"
// is reconciled to a terminal state so nothing keeps spinning; finished items
// are left untouched, and the sweep is idempotent.
func TestTaskEndSweepCancelsRunningWork(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.items = append(m.items,
		item{kind: "tool", tool: &toolView{name: "read_file", state: "running"}},
		item{kind: "tool", tool: &toolView{name: "grep", state: "ok"}},
		item{kind: "tool", tool: &toolView{name: "write_file", state: "fail"}},
	)
	m.agents = append(m.agents, &agentView{id: "a1", status: "running", items: []item{
		{kind: "tool", tool: &toolView{name: "read_file", state: "running"}},
	}})

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
	if got := m.agents[0].status; got != "cancelled" {
		t.Fatalf("running subagent not cancelled: %q", got)
	}
	if got := m.agents[0].items[0].tool.state; got != "cancelled" {
		t.Fatalf("nested running tool not cancelled: %q", got)
	}

	// Idempotent: a second sweep changes nothing.
	m.sweepRunningWork()
	if m.items[1].tool.state != "ok" || m.agents[0].status != "cancelled" {
		t.Fatal("second sweep was not idempotent")
	}
}
