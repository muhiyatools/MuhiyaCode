package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// failingTool always returns an error, simulating a tool that never succeeds.
type failingTool struct {
	name string
}

func (t *failingTool) Definition() contract.ToolDefinition {
	return definition(t.name, "test failing tool", map[string]any{
		"path":    map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
	}, nil)
}

func (t *failingTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	return "", fmt.Errorf("simulated failure")
}

// repeatedFailingCalls builds n responses, each carrying two distinct failing
// write_file calls (different paths) so two failures accumulate per turn.
func repeatedFailingCalls(n int) []contract.ChatResponse {
	responses := make([]contract.ChatResponse, n)
	for i := range responses {
		responses[i] = contract.ChatResponse{
			ToolCalls: []contract.ToolCall{
				contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"x"}`),
				contract.NewToolCall("c2", "write_file", `{"path":"b.txt","content":"y"}`),
			},
		}
	}
	return responses
}

// TestDistinctFailureTerminatorStopsTask (H5) verifies that a rigged provider
// returning failing tool calls forever terminates well before the 120-turn
// ceiling. Without the terminator the old consecutiveFailures>=3 nudge reset to
// 0 and looped forever; the windowed count is not reset by interspersed
// successes or the nudge.
func TestDistinctFailureTerminatorStopsTask(t *testing.T) {
	provider := &scriptedProvider{responses: repeatedFailingCalls(20)}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "h5-fail", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&failingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	_, stats, err := engine.Run(context.Background(), "fix the bug in main.go")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Turns >= hardTurnCeiling {
		t.Fatalf("distinct-failure terminator did not fire: ran %d turns", stats.Turns)
	}
	// Two failures per turn → 8 failures at turn 4 → terminator fires.
	if stats.Turns > maxTaskFailures/2+2 {
		t.Fatalf("terminator fired too late: %d turns (expected ~%d)", stats.Turns, maxTaskFailures/2)
	}
	if stats.TerminatedReason == "" || !strings.Contains(stats.TerminatedReason, "repeated") {
		t.Fatalf("terminator reason wrong: %q", stats.TerminatedReason)
	}
}
