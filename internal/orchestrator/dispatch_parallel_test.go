package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type overlapReadTool struct {
	mu         sync.Mutex
	active     int
	maxActive  int
	completion []string
}

func (tool *overlapReadTool) Definition() contract.ToolDefinition {
	return contract.ToolDefinition{
		Type: "function",
		Function: contract.FunctionDefinition{
			Name: "read_file",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []string{"path"}, "additionalProperties": false,
			},
		},
	}
}

func (tool *overlapReadTool) Execute(ctx context.Context, raw json.RawMessage) contract.ToolResult {
	var input struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &input)
	tool.mu.Lock()
	tool.active++
	tool.maxActive = max(tool.maxActive, tool.active)
	tool.mu.Unlock()
	delay := 30 * time.Millisecond
	if input.Path == "first" {
		delay = 70 * time.Millisecond
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return contract.AdaptToolResult("", ctx.Err())
	}
	tool.mu.Lock()
	tool.active--
	tool.completion = append(tool.completion, input.Path)
	tool.mu.Unlock()
	return contract.AdaptToolResult("result:"+input.Path, nil)
}

func TestExecuteBatchOverlapsReadsAndPreservesProviderOrder(t *testing.T) {
	tool := &overlapReadTool{}
	engine := gateTestEngine(t, tool)
	engine.taskCounters = newCallCounters()
	calls := []contract.ToolCall{
		contract.NewToolCall("1", "read_file", `{"path":"first"}`),
		contract.NewToolCall("2", "read_file", `{"path":"second"}`),
	}
	outcomes := engine.executeBatch(context.Background(), calls, engine.registry.Definitions(nil), Profile(contract.EffortMedium))
	if tool.maxActive < 2 {
		t.Fatalf("independent reads did not overlap: max active=%d", tool.maxActive)
	}
	if got := fmt.Sprint([]string{outcomes[0].Output, outcomes[1].Output}); got != "[result:first result:second]" {
		t.Fatalf("provider order was not preserved: %s", got)
	}
	if fmt.Sprint(tool.completion) != "[second first]" {
		t.Fatalf("test did not exercise out-of-order completion: %v", tool.completion)
	}
}

func TestExecuteBatchSerializesMixedMutationBatch(t *testing.T) {
	calls := []contract.ToolCall{
		contract.NewToolCall("1", "read_file", `{"path":"a"}`),
		contract.NewToolCall("2", "write_file", `{"path":"b","content":"x"}`),
	}
	if parallelReadBatch(calls) {
		t.Fatal("a batch containing a mutation must never use the parallel-read scheduler")
	}
}
