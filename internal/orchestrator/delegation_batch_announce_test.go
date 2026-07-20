package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestDelegateBatchAnnouncesAllToolStartsUpFront guards the live-session fix
// for the frozen-transcript delegate fan-out: when one assistant turn carries
// multiple run_subagent calls, EVERY "Delegate … running" ToolStart must fire
// when the batch launches — before any delegate executes. Before the fix, the
// serial path (taken whenever the batch contains a "general" delegate, or
// ParallelAgents is off) fired delegate N+1's ToolStart inside gatedExecute
// only after delegate N fully completed, so the announced fan-out stayed
// invisible for minutes.
func TestDelegateBatchAnnouncesAllToolStartsUpFront(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
	}{
		// A "general" delegate forces the safe serial path — the regression case.
		{"serial batch with a general delegate", []string{"explore", "general"}},
		// Read-only kinds take the concurrent path; announcements still all
		// precede every launch.
		{"parallel read-only delegate batch", []string{"explore", "review"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var events []string
			provider := &scriptedProvider{}
			settings := engineSettings()
			settings.Effort = contract.EffortHigh // ParallelAgents on
			engine, err := NewEngine(EngineConfig{
				Settings: &settings,
				Session:  contract.Session{ID: "announce", WorkspacePath: t.TempDir()},
				Provider: provider,
				Registry: NewRegistry(),
				Callbacks: contract.Callbacks{
					ToolStart: func(name string, _ json.RawMessage) {
						mu.Lock()
						events = append(events, "tool:"+name)
						mu.Unlock()
					},
					Agent: func(event contract.AgentEvent) {
						if event.Kind != "start" {
							return
						}
						mu.Lock()
						events = append(events, "agent:"+event.Agent)
						mu.Unlock()
					},
				},
				Prompt: PromptContext{},
			})
			if err != nil {
				t.Fatal(err)
			}
			engine.taskCounters = newCallCounters()
			calls := make([]contract.ToolCall, len(tc.kinds))
			for i, kind := range tc.kinds {
				calls[i] = contract.NewToolCall(fmt.Sprintf("c%d", i), "run_subagent", fmt.Sprintf(`{"agent":%q,"title":"part %d","task":"inspect part %d"}`, kind, i, i))
			}
			outcomes := engine.executeBatch(context.Background(), calls, engine.sessionDefinitions(), Profile(contract.EffortHigh))
			if len(outcomes) != len(calls) {
				t.Fatalf("expected %d outcomes, got %d", len(calls), len(outcomes))
			}
			for i, outcome := range outcomes {
				if outcome.Failed {
					t.Fatalf("delegate %d unexpectedly failed: %s", i, outcome.Output)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			starts := 0
			for _, event := range events {
				if event == "tool:run_subagent" {
					starts++
				}
			}
			if starts != len(calls) {
				t.Fatalf("expected exactly %d ToolStart announcements (no duplicates), got %d: %v", len(calls), starts, events)
			}
			firstAgent, lastTool := -1, -1
			for i, event := range events {
				if event == "tool:run_subagent" {
					lastTool = i
				}
				if strings.HasPrefix(event, "agent:") && firstAgent == -1 {
					firstAgent = i
				}
			}
			if firstAgent == -1 {
				t.Fatalf("no subagent launched; events: %v", events)
			}
			if lastTool > firstAgent {
				t.Fatalf("a ToolStart fired only after a delegate already launched (staggered starts): %v", events)
			}
		})
	}
}

// TestSingleAndMixedBatchesKeepInPlaceToolStarts: the up-front announcement is
// scoped to all-run_subagent batches. A single call or a mixed batch keeps the
// original in-gate ToolStart (strict start/end nesting for ordinary tools, and
// live ToolOutput routing stays unambiguous).
func TestSingleAndMixedBatchesKeepInPlaceToolStarts(t *testing.T) {
	var mu sync.Mutex
	var starts []string
	provider := &scriptedProvider{}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "mixed", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Callbacks: contract.Callbacks{ToolStart: func(name string, _ json.RawMessage) {
			mu.Lock()
			starts = append(starts, name)
			mu.Unlock()
		}},
		Prompt: PromptContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.taskCounters = newCallCounters()
	calls := []contract.ToolCall{
		contract.NewToolCall("c1", "read_file", `{"path":"a.txt"}`),
		contract.NewToolCall("c2", "run_subagent", `{"agent":"explore","title":"look","task":"inspect"}`),
	}
	engine.executeBatch(context.Background(), calls, engine.sessionDefinitions(), Profile(contract.EffortHigh))
	mu.Lock()
	defer mu.Unlock()
	if len(starts) != 2 || starts[0] != "read_file" || starts[1] != "run_subagent" {
		t.Fatalf("mixed batch must keep one in-place ToolStart per call, got %v", starts)
	}
}
