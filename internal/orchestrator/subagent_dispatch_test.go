package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// The v1.1.0 field test produced a deadlock: the user typed "Go" after a plan
// was presented, it classified as chat, the brief said agents=0, and the
// plan/execute split forbids the main model from editing — so the agent had no
// legal action and asked the user to send another message. These tests pin the
// fix: there is no allowance, so no turn can be starved into that corner.

// TestChatClassTurnCanDispatch is the exact transcript regression.
func TestChatClassTurnCanDispatch(t *testing.T) {
	if got := Classify("Go", ClassChat); got.Class != ClassChat {
		t.Fatalf("fixture drifted: %q classifies as %q, expected chat", "Go", got.Class)
	}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("d1", "run_subagent", `{"agent":"general","role":"page-builder","task":"Apply the queued layout changes to index.html."}`),
		}},
		{Content: "Applied the layout changes. Verification: build passed. STATUS: COMPLETE"},
		{Content: "Done — the layout changes are in."},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "go-turn", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.previous = ClassChat // the plan turn before "Go"
	answer, stats, err := engine.Run(context.Background(), "Go")
	if err != nil {
		t.Fatalf("a chat-classified continuation could not run: %v", err)
	}
	if stats.AgentRuns != 1 {
		t.Fatalf("dispatch was refused on a chat-class turn: AgentRuns=%d answer=%q", stats.AgentRuns, answer)
	}
}

// TestNoBudgetVocabularyReachesTheModel is the tombstone: the brief and the
// system prompt must never again speak in allowances.
func TestNoBudgetVocabularyReachesTheModel(t *testing.T) {
	forbidden := []string{"agents<=", "agents=0", "budget exhausted", "run(s) remaining", "no subagent budget"}
	for _, class := range []TaskClass{ClassChat, ClassTiny, ClassSmall, ClassStandard, ClassLarge, ClassEpic} {
		brief := BudgetFor(Assessment{Class: class}, Profile(contract.EffortMax)).Brief
		for _, term := range forbidden {
			if strings.Contains(brief, term) {
				t.Errorf("class %q brief still speaks in budgets (%q): %s", class, term, brief)
			}
		}
	}
	prompt := SystemPrompt(PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", HasSubagents: true, SubagentModel: "sub"})
	for _, term := range forbidden {
		if strings.Contains(prompt, term) {
			t.Errorf("system prompt still speaks in budgets: %q", term)
		}
	}
}

// TestDelegationIsUncapped: many dispatches in one task all succeed. The
// liveness guards, not a quota, are what stop a runaway.
func TestDelegationIsUncapped(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "uncapped", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "report body that is long enough to bank and be returned to the parent as a normal completion"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.resetTaskState(BudgetFor(Assessment{Class: ClassTiny}, Profile(contract.EffortLow)))
	for i := 0; i < 6; i++ {
		out, err := engine.runSubagentInput(context.Background(), subagentInput{Agent: "general", Task: "step"})
		if err != nil {
			t.Fatalf("dispatch %d was refused: %v", i+1, err)
		}
		if strings.Contains(out, "remaining") || strings.Contains(out, "budget") {
			t.Fatalf("dispatch %d report carries budget vocabulary: %s", i+1, out)
		}
	}
	if engine.taskAgentRuns != 6 {
		t.Fatalf("AgentRuns = %d, want 6 — every dispatch must count", engine.taskAgentRuns)
	}
}
