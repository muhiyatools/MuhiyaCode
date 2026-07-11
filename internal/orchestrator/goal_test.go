package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func newGoalEngine(t *testing.T, provider contract.Provider) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// A goal keeps the agent working until it emits [goal:complete].
func TestGoalAutoContinuesUntilComplete(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Step one done. [goal:continue]"},
		{Content: "All finished and verified. [goal:complete]"},
	}}
	engine := newGoalEngine(t, provider)
	engine.SetGoal("make the tests pass")
	answer, _, err := engine.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("goal should have auto-continued once, saw %d requests", len(provider.requests))
	}
	if !strings.Contains(answer, "finished") {
		t.Fatalf("unexpected final answer: %q", answer)
	}
	if goal, _ := engine.GoalSnapshot(); goal.Status != GoalComplete {
		t.Fatalf("goal not marked complete: %+v", goal)
	}
	// The goal instruction must ride on a user message (cache-safe), not the
	// system prompt.
	if !strings.Contains(provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content, "active-goal") {
		t.Fatal("goal block did not ride on the user message")
	}
	if strings.Contains(provider.requests[0].Messages[0].Content, "active-goal") {
		t.Fatal("goal leaked into the system prompt — would bust the cache")
	}
}

// Plan mode blocks mutating tools and lets read-only tools through.
func TestPlanModeBlocksMutations(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"x"}`)}},
		{Content: "Here is the plan; may I proceed?"},
	}}
	settings := engineSettings()
	writer := &recordingTool{name: "write_file"}
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(writer), Prompt: PromptContext{}})
	engine.SetPlanMode(true)
	if _, _, err := engine.Run(context.Background(), "add a file"); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatalf("plan mode allowed a mutation: %d calls", writer.calls)
	}
	// The plan-mode instruction rides on the user message.
	last := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if !strings.Contains(last, "plan-mode") {
		t.Fatalf("plan-mode marker missing from user message: %q", last)
	}
}
