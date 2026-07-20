package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestApplyAgentEventKindsUpdateAgentView (feature 010 US5, WI-7) pins all six
// contract.AgentEvent.Kind values applyAgent (ingest.go) switches on.
// TestSubagentRunningState (application_state_test.go) already exercised
// "start"; tool_start/tool_end/usage/text/done had no direct consumer-side
// test anywhere — pipeline_gates_test.go/pipeline_fastpath_test.go in
// internal/orchestrator only prove the PRODUCER side (that subagent.go emits
// them), not that the TUI's consumer switch does the right thing with each
// one. This closes that gap.
func TestApplyAgentEventKindsUpdateAgentView(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m.applyAgent(contract.AgentEvent{Kind: "start", RunID: "r1", Agent: "explorer", Title: "Map code", Task: "explore"})
	agent := m.agentByID["r1"]
	if agent == nil || agent.status != "running" {
		t.Fatalf("start did not register a running agent: %+v", agent)
	}

	m.applyAgent(contract.AgentEvent{Kind: "tool_start", RunID: "r1", Tool: "read_file", Arguments: `{"path":"a.go"}`})
	if agent.active == "" || len(agent.items) != 1 || agent.items[0].tool == nil || agent.items[0].tool.state != "running" {
		t.Fatalf("tool_start did not append a running tool: active=%q items=%+v", agent.active, agent.items)
	}

	m.applyAgent(contract.AgentEvent{Kind: "tool_end", RunID: "r1", Tool: "read_file", Output: "package main"})
	if agent.active != "" || agent.items[0].tool.state != "ok" {
		t.Fatalf("tool_end did not finish the running tool: active=%q state=%q", agent.active, agent.items[0].tool.state)
	}

	m.applyAgent(contract.AgentEvent{Kind: "text", RunID: "r1", Content: "found the entrypoint"})
	if len(agent.items) != 2 || agent.items[1].kind != "assistant" || agent.items[1].content != "found the entrypoint" {
		t.Fatalf("text did not append an assistant item: %+v", agent.items)
	}

	m.applyAgent(contract.AgentEvent{Kind: "usage", RunID: "r1", Usage: contract.Usage{TotalTokens: 500}})
	if agent.usage.TotalTokens != 500 {
		t.Fatalf("usage did not update agent.usage: %+v", agent.usage)
	}

	m.applyAgent(contract.AgentEvent{Kind: "done", RunID: "r1", Status: "completed", Report: "entrypoint is main.go"})
	if agent.status != "completed" || len(agent.items) != 3 || agent.items[2].title != "report" || agent.items[2].content != "entrypoint is main.go" {
		t.Fatalf("done did not finalize status/report: status=%q items=%+v", agent.status, agent.items)
	}

	// An event for a RunID with no prior "start" must be a no-op, not a panic
	// or a phantom agent (ingest.go's `if agent == nil { return }` guard).
	before := len(m.agents)
	m.applyAgent(contract.AgentEvent{Kind: "tool_start", RunID: "does-not-exist", Tool: "read_file"})
	if len(m.agents) != before {
		t.Fatalf("event for an unregistered RunID mutated m.agents")
	}
}

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
