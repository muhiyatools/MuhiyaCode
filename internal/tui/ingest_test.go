package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// The AgentEvent consumer test retired with the subagent system: applyAgent
// and the six event kinds it switched on are gone, because no agent emits them.

// TestContextPlanAndMCPStatusMessagesUpdateModel (feature 010 US5, WI-7)
// closes the one real gap the wiring-inventory pass found among the
// Callbacks members: contract.Callbacks.Context/PlanUpdate/MCPStatus (wired
// producer-side in internal/orchestrator and internal/command, and
// translated to a Bubble Tea message by internal/tui/bridge.go's
// Callbacks()) had no test anywhere driving their consumer half —
// update.go's contextMsg/planMsg/mcpStatusMsg cases. TaskComplete/statsMsg,
// Agent/agentMsg, Token+ReasoningToken/streamMsg, and
// ToolStart+ToolOutput+ToolEnd all already have consumer-side tests
// elsewhere (sweep_test.go, TestApplyAgentEventKindsUpdateAgentView above,
// bridge_test.go, tui_test.go's TestToolLifecycleUpdatesActiveMap-shaped
// coverage); this test is what the other three were missing.
func TestContextPlanAndMCPStatusMessagesUpdateModel(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m = mustUpdate(t, m, contextMsg(contract.ContextInfo{HistoryTokens: 1200, LastRequestTokens: 300, ContextLimit: 64000, Percent: 2.5}))
	if m.context.HistoryTokens != 1200 || m.context.ContextLimit != 64000 {
		t.Fatalf("contextMsg did not update m.context: %+v", m.context)
	}

	m = mustUpdate(t, m, planMsg(contract.Plan{Steps: []contract.PlanStep{{Title: "ship it", Status: contract.PlanInProgress}}}))
	if len(m.plan.Steps) != 1 || m.plan.Steps[0].Title != "ship it" {
		t.Fatalf("planMsg did not update m.plan: %+v", m.plan)
	}

	m = mustUpdate(t, m, mcpStatusMsg("filesystem: connected"))
	if m.flash.level != "warn" || m.flash.text != "MCP · filesystem: connected" {
		t.Fatalf("mcpStatusMsg did not surface the expected warn notice: level=%q text=%q", m.flash.level, m.flash.text)
	}
}
