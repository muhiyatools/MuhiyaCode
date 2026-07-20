package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestLowEffortGrantsOneSubagentRun: the Low effort tier must allow one
// delegated run for classes that support delegation — a 0 budget at Low made
// every plan-mode "Delegate" attempt fail with "budget exhausted (0 run(s))".
func TestLowEffortGrantsOneSubagentRun(t *testing.T) {
	if Profile(contract.EffortLow).MaxAgentRuns != 1 {
		t.Fatalf("EffortLow.MaxAgentRuns = %d, want 1", Profile(contract.EffortLow).MaxAgentRuns)
	}
	budget := BudgetFor(Assessment{Class: ClassStandard}, Profile(contract.EffortLow))
	if budget.MaxAgentRuns != 1 {
		t.Fatalf("low-effort standard budget grants %d agent run(s), want 1", budget.MaxAgentRuns)
	}
	if !strings.Contains(budget.Brief, "agents<=1") {
		t.Fatalf("brief does not advertise the granted budget: %s", budget.Brief)
	}
}

// TestZeroAgentBriefStatesProhibition: when the budget is zero the brief must
// state an explicit prohibition, not a bare "agents<=0" the model reads as an
// estimate and ignores.
func TestZeroAgentBriefStatesProhibition(t *testing.T) {
	budget := BudgetFor(Assessment{Class: ClassChat}, Profile(contract.EffortMax))
	if budget.MaxAgentRuns != 0 {
		t.Fatalf("chat class should grant no agents, got %d", budget.MaxAgentRuns)
	}
	if !strings.Contains(budget.Brief, "agents=0 (no run_subagent)") {
		t.Fatalf("zero-agent brief lacks the explicit prohibition: %s", budget.Brief)
	}
}

// TestWithAgentFloorRegeneratesBrief and TestPlanModeGuaranteesAgentFloor were
// deleted with Budget.WithAgentFloor and plan mode: the floor existed only to
// guarantee plan mode a delegated run, and both the method and the lifecycle
// state it keyed on are gone.

// TestSubagentDenialEscalates: 004 US3 (T035) splits the two denial paths. With
// no budget at all (agents=0) the first call hard-closes immediately. A genuine
// mid-task exhaustion still teaches "work directly" first and escalates to an
// unambiguous stop only on a repeat, so a delegation retry loop converges.
func TestSubagentDenialEscalates(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "denied", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"agent":"explore","task":"scan the code"}`)

	// agents=0: closed from the first call.
	engine.taskAgentCap = 0
	_, first := engine.runSubagentTool(context.Background(), input)
	if first == nil || !strings.Contains(first.Error(), "no subagent budget") || !strings.Contains(first.Error(), "do NOT call it again") {
		t.Fatalf("agents=0 first denial should hard-close immediately, got %v", first)
	}

	// Mid-task exhaustion (a real budget that ran out): soft first (used-of-cap),
	// hard on the repeat.
	engine.taskAgentCap, engine.taskAgentRuns, engine.taskAgentDenied = 1, 1, 0
	_, soft := engine.runSubagentTool(context.Background(), input)
	if soft == nil || !strings.Contains(soft.Error(), "1 of 1 run(s) used") || !strings.Contains(soft.Error(), "complete the remaining work directly") {
		t.Fatalf("exhausted first denial should teach direct completion with used-of-cap, got %v", soft)
	}
	_, hard := engine.runSubagentTool(context.Background(), input)
	if hard == nil || !strings.Contains(hard.Error(), "do NOT call it again") {
		t.Fatalf("repeat exhausted denial should escalate to a stop instruction, got %v", hard)
	}
}

// TestSubagentReportIncludesRemainingBudget: every successful run tells the
// model how much budget remains so it can plan the switch to direct work.
func TestSubagentReportIncludesRemainingBudget(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "findings recorded"}}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "remaining", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	engine.taskAgentCap = 2
	report, err := engine.runSubagentTool(context.Background(), json.RawMessage(`{"agent":"explore","task":"scan the code"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "1 subagent run(s) remaining") {
		t.Fatalf("report does not carry the remaining budget: %q", report)
	}

	provider.mu.Lock()
	provider.responses = []contract.ChatResponse{{Content: "second findings"}}
	provider.mu.Unlock()
	report, err = engine.runSubagentTool(context.Background(), json.RawMessage(`{"agent":"review","task":"review the diff"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "budget now exhausted") {
		t.Fatalf("final run should announce exhaustion: %q", report)
	}
}
