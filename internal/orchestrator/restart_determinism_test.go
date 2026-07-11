package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestRestartDeterminism(t *testing.T) {
	settings := engineSettings()
	firstProvider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "first"}}}
	registry := NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "mcp__demo__b"}, &recordingTool{name: "mcp__demo__a"})
	prompt := PromptContext{Workspace: t.TempDir(), OS: "windows", Shell: "pwsh", Model: "Test"}
	first, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: prompt.Workspace}, Provider: firstProvider, Registry: registry, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}

	resumedProvider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "second"}}}
	resumed, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: prompt.Workspace}, Provider: resumedProvider, Registry: registry, History: NewHistory(first.history.Snapshot(), nil), Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resumed.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	one, two := firstProvider.requests[0], resumedProvider.requests[0]
	toolsOne, _ := json.Marshal(one.Tools)
	toolsTwo, _ := json.Marshal(two.Tools)
	if string(toolsOne) != string(toolsTwo) {
		t.Fatal("R2 changed across restart")
	}
	if len(two.Messages) <= len(one.Messages) {
		t.Fatalf("history did not append across restart: %d <= %d", len(two.Messages), len(one.Messages))
	}
	for index := range one.Messages {
		left, _ := json.Marshal(one.Messages[index])
		right, _ := json.Marshal(two.Messages[index])
		if string(left) != string(right) {
			t.Fatalf("R1/R3 message %d changed across restart", index)
		}
	}
}
