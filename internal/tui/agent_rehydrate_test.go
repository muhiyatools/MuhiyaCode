package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestRehydrateFinishedSubagentOnResume pins U2: a persisted run_summary event is
// rebuilt into a clickable/tab-able subagent chip whose view shows the report, so
// finished subagents no longer vanish after a resume.
func TestRehydrateFinishedSubagentOnResume(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	payload := `{"runId":"a1","agent":"explore","phase":"research","role":"explore","title":"scan auth","status":"done","report":"Found 3 error-handling gaps.","task":"audit auth/"}`
	m.loadEvents([]contract.Event{{Role: "agent", Type: "run_summary", Content: payload}})

	if len(m.agents) != 1 || m.agentByID["a1"] == nil {
		t.Fatalf("finished subagent not rehydrated: agents=%d", len(m.agents))
	}
	chip := false
	for _, it := range m.items {
		if it.kind == "agent" && it.agentID == "a1" {
			chip = true
		}
	}
	if !chip {
		t.Fatal("agent chip item missing from the transcript")
	}
	// Tab reaches it, and its view shows the persisted report.
	m.cycleAgent()
	if m.viewAgent != "a1" {
		t.Fatalf("Tab did not reach the rehydrated agent: viewAgent=%q", m.viewAgent)
	}
	m.refreshViewport(true)
	if !strings.Contains(m.renderTranscript(), "Found 3 error-handling gaps") {
		t.Fatal("the rehydrated agent view does not show its persisted report")
	}
	// A duplicate run_summary (or a live run of the same id) does not double-register.
	m.loadEvents([]contract.Event{{Role: "agent", Type: "run_summary", Content: payload}})
	if len(m.agents) != 1 {
		t.Fatalf("duplicate rehydration created %d agents, want 1", len(m.agents))
	}
}

// TestTrimTranscriptKeepsAgentChips pins U3: front-eviction never drops a subagent
// chip, so its click affordance survives a marathon session.
func TestTrimTranscriptKeepsAgentChips(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m.agentByID["old"] = &agentView{id: "old", agent: "explore", status: "done"}
	m.items = append(m.items, item{kind: "agent", agentID: "old"})
	// Push far past the item cap with plain user rows so trim front-evicts.
	for i := 0; i < maxTranscriptItems+50; i++ {
		m.items = append(m.items, item{kind: "user", content: "row"})
	}
	m.trimTranscript()
	kept := false
	for _, it := range m.items {
		if it.kind == "agent" && it.agentID == "old" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("trimTranscript evicted a subagent chip that must be preserved")
	}
}
