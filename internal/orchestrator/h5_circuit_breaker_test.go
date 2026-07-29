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

func (t *failingTool) Execute(_ context.Context, _ json.RawMessage) contract.ToolResult {
	return contract.AdaptToolResult("", fmt.Errorf("simulated failure"))
}

// repeatedFailingCalls builds n responses, each carrying two GENUINELY failing
// read_file calls with paths distinct per turn, so two real tool failures
// accumulate per turn. read_file (not a mutation) is used deliberately: a
// write_file from the main loop is refused by the execution role gate BEFORE it
// runs, and a role-gate rejection is recoverable guidance that (post-G-1) does
// not feed this terminator — only tools that actually run and fail do.
func repeatedFailingCalls(n int) []contract.ChatResponse {
	responses := make([]contract.ChatResponse, n)
	for i := range responses {
		responses[i] = contract.ChatResponse{
			ToolCalls: []contract.ToolCall{
				contract.NewToolCall(fmt.Sprintf("c%da", i), "read_file", fmt.Sprintf(`{"path":"a%d.txt"}`, i)),
				contract.NewToolCall(fmt.Sprintf("c%db", i), "read_file", fmt.Sprintf(`{"path":"b%d.txt"}`, i)),
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
		Provider: provider, Registry: NewRegistry(&failingTool{name: "read_file"}),
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
	if stats.Status != contract.TaskStatusIncomplete {
		t.Fatalf("breaker outcome must be incomplete, got %q", stats.Status)
	}
}

// TestGateRejectionsDoNotForceFinalize (G-1) pins the invariant: a turn-1 batch
// of PRE-DISPATCH gate rejections is recoverable guidance ("fix the arguments"),
// not genuine tool failure, so it must NOT trip the H5 distinct-failure
// terminator — which would force-finalize the task with a misleading "genuine
// blocker" report before the model could act on a single message.
//
// It used the execution role gate as its rejection source while that gate
// existed. H1 argument validation is the surviving pre-dispatch rejection, and
// the invariant it guards is identical.
func TestGateRejectionsDoNotForceFinalize(t *testing.T) {
	batch := make([]contract.ToolCall, maxTaskFailures)
	for i := range batch {
		// Missing the required "path" field: H1 refuses each before dispatch.
		batch[i] = contract.NewToolCall(fmt.Sprintf("c%d", i), "configure_widget", `{"mode":"a"}`)
	}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: batch},
		{Content: "Corrected the arguments and moved on."},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "g1-gate", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&schemaTool{name: "configure_widget"}),
		Prompt: PromptContext{},
	})
	answer, stats, err := engine.Run(context.Background(), "configure every widget")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stats.TerminatedReason, "repeated tool failures") {
		t.Fatalf("gate rejections force-finalized the task via H5: %q", stats.TerminatedReason)
	}
	if answer != "Corrected the arguments and moved on." {
		t.Fatalf("the model was not allowed to act on the gate guidance: answer=%q", answer)
	}
}
