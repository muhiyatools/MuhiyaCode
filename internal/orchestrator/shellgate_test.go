package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestReadOnlySubagentShellGateRunsChecksButBlocksMutation pins the read-only
// subagent shell gate: checks run, mutations are refused, and a mutation
// chained onto a legal command cannot smuggle itself through.
func TestReadOnlySubagentShellGateRunsChecksButBlocksMutation(t *testing.T) {
	settings := engineSettings()
	tool := &recordingTool{name: "run_shell"}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(tool)})
	if err != nil {
		t.Fatal(err)
	}
	definitions := engine.registry.Definitions(map[string]bool{"run_shell": true})
	scope := dispatchScope{
		counters: newCallCounters(),
		readOnly: true,
		dispatch: func(ctx context.Context, call contract.ToolCall) (string, error) {
			return engine.registry.Execute(ctx, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), map[string]bool{"run_shell": true})
		},
	}
	allowed := engine.gatedExecute(context.Background(), contract.NewToolCall("check", "run_shell", `{"command":"go test ./internal/orchestrator"}`), definitions, Profile(contract.EffortLow), scope)
	if allowed.Failed || tool.calls != 1 {
		t.Fatalf("read-only check was blocked: outcome=%+v calls=%d", allowed, tool.calls)
	}
	blocked := engine.gatedExecute(context.Background(), contract.NewToolCall("mutate", "run_shell", `{"command":"Remove-Item important.go"}`), definitions, Profile(contract.EffortLow), scope)
	if !blocked.Failed || !strings.Contains(blocked.Output, "read-only agent") || tool.calls != 1 {
		t.Fatalf("mutating shell escaped read-only gate: outcome=%+v calls=%d", blocked, tool.calls)
	}
	chained := engine.gatedExecute(context.Background(), contract.NewToolCall("chain", "run_shell", `{"command":"go test ./...; Remove-Item important.go"}`), definitions, Profile(contract.EffortLow), scope)
	if !chained.Failed || tool.calls != 1 {
		t.Fatalf("chained mutation escaped read-only shell gate: outcome=%+v calls=%d", chained, tool.calls)
	}
}
