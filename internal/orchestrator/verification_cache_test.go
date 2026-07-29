package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type verificationTool struct{ calls int }

func (tool *verificationTool) Definition() contract.ToolDefinition {
	return definition("run_shell", "run", map[string]any{
		"command": map[string]any{"type": "string"},
	}, []string{"command"})
}

func (tool *verificationTool) Execute(context.Context, json.RawMessage) contract.ToolResult {
	tool.calls++
	return contract.AdaptToolResult("ok", nil)
}

func TestVerificationCacheSkipsUnchangedSuccessfulCheck(t *testing.T) {
	settings := engineSettings()
	tool := &verificationTool{}
	fingerprint := "state-a"
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "verify", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{}, Registry: NewRegistry(tool), Prompt: PromptContext{},
		WorkspaceFingerprint: func(context.Context) (string, error) { return fingerprint, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.resetTaskState(BudgetFor(Assessment{Class: ClassSmall}, Profile(contract.EffortLow)))
	call := contract.NewToolCall("check-1", "run_shell", `{"command":"go test ./..."}`)
	definitions := engine.coreDefinitions()
	first := engine.executeCall(context.Background(), call, definitions, Profile(contract.EffortLow))
	call = contract.NewToolCall("check-2", "run_shell", `{"command":"go test ./..."}`)
	second := engine.executeCall(context.Background(), call, definitions, Profile(contract.EffortLow))
	if !first.Succeeded() || !second.Succeeded() || tool.calls != 1 {
		t.Fatalf("first=%+v second=%+v calls=%d", first, second, tool.calls)
	}
	if !strings.Contains(second.Output, "verification cache hit") {
		t.Fatalf("cached output=%q", second.Output)
	}
	fingerprint = "state-b"
	call = contract.NewToolCall("check-3", "run_shell", `{"command":"go test ./..."}`)
	third := engine.executeCall(context.Background(), call, definitions, Profile(contract.EffortLow))
	if !third.Succeeded() || tool.calls != 2 {
		t.Fatalf("changed workspace did not rerun check: outcome=%+v calls=%d", third, tool.calls)
	}
}
