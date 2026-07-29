package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type policyTool struct {
	name  string
	calls int
}

func (tool *policyTool) Definition() contract.ToolDefinition {
	properties := map[string]any{"command": map[string]any{"type": "string"}}
	required := []string{"command"}
	switch tool.name {
	case "list_files":
		properties = map[string]any{"path": map[string]any{"type": "string"}}
		required = nil
	case "write_file":
		properties = map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}
		required = []string{"path", "content"}
	}
	return definition(tool.name, "test", properties, required)
}

func (tool *policyTool) Execute(_ context.Context, _ json.RawMessage) contract.ToolResult {
	tool.calls++
	if tool.name == "list_files" {
		return contract.AdaptToolResult("Listed . (1 entry).", nil)
	}
	if tool.name == "run_shell" {
		return contract.AdaptToolResult("exit code: 0\nsyntax valid", nil)
	}
	return contract.AdaptToolResult("Wrote snake-game/index.html.\n--- diff ---\n+html", nil)
}

func TestSimpleSingleFileTaskStopsAfterOneCheapCheck(t *testing.T) {
	listTool := &policyTool{name: "list_files"}
	writeTool := &policyTool{name: "write_file"}
	shellTool := &policyTool{name: "run_shell"}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{
			ToolCalls: []contract.ToolCall{contract.NewToolCall("list", "list_files", `{"path":"."}`)},
			Usage:     contract.Usage{PromptTokens: 1_500, CompletionTokens: 50, TotalTokens: 1_550},
		},
		{
			ToolCalls: []contract.ToolCall{contract.NewToolCall(
				"mkdir", "run_shell", `{"command":"New-Item -ItemType Directory -Path snake-game -Force"}`,
			)},
			Usage: contract.Usage{PromptTokens: 1_800, CompletionTokens: 100, TotalTokens: 1_900},
		},
		{
			ToolCalls: []contract.ToolCall{contract.NewToolCall(
				"write", "write_file", `{"path":"snake-game/index.html","content":"<html></html>"}`,
			)},
			Usage: contract.Usage{PromptTokens: 2_000, CompletionTokens: 1_000, TotalTokens: 3_000},
		},
		{
			ToolCalls: []contract.ToolCall{contract.NewToolCall(
				"check", "run_shell", `{"command":"node --check snake-game/index.html"}`,
			)},
			Usage: contract.Usage{PromptTokens: 3_000, CompletionTokens: 100, TotalTokens: 3_100},
		},
		{
			Content: "Let me add a comprehensive harness.",
			ToolCalls: []contract.ToolCall{contract.NewToolCall(
				"harness", "write_file", `{"path":"snake-game/_harness.js","content":"tests"}`,
			)},
			Usage: contract.Usage{PromptTokens: 3_500, CompletionTokens: 200, TotalTokens: 3_700},
		},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "snake-policy", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(listTool, writeTool, shellTool),
		History:  NewHistory(HistorySnapshot{Version: 1}, nil),
		Prompt:   PromptContext{Model: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, stats, err := engine.Run(context.Background(), "Make for me a simple snake game in a single html file")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 5 {
		t.Fatalf("requests=%d, want mapping, one rejected mkdir, one write, one check, and one final turn", len(provider.requests))
	}
	if listTool.calls != 1 || writeTool.calls != 1 || shellTool.calls != 1 {
		t.Fatalf("executed list=%d writes=%d shell=%d; mkdir and scratch harness must not execute", listTool.calls, writeTool.calls, shellTool.calls)
	}
	if stats.TaskClass != string(ClassTiny) || stats.ChecksRun != 1 || stats.Turns != 5 {
		t.Fatalf("unexpected task stats: %+v", stats)
	}
	for index, request := range provider.requests {
		if request.Reasoning != contract.ReasoningLow || request.MaxTokens > 32_000 {
			t.Fatalf("request %d escaped tiny-task generation policy: reasoning=%s max=%d", index+1, request.Reasoning, request.MaxTokens)
		}
	}
	foundFinalGovernor := false
	for _, message := range engine.history.All() {
		if strings.Contains(message.Content, "verification budget is complete") {
			foundFinalGovernor = true
		}
	}
	if !foundFinalGovernor {
		t.Fatal("completion governor was not injected after the successful check")
	}
}

func TestCheckRetryMustBeEarnedByFailure(t *testing.T) {
	engine := &Engine{taskBudget: Budget{Class: ClassTiny, MaxChecks: 1}}
	check := contract.NewToolCall("check", "run_shell", `{"command":"node --check index.js"}`)

	if allowed, _ := engine.taskCheckAdmission(check); !allowed {
		t.Fatal("first planned check was rejected")
	}
	if allowed, _ := engine.taskCheckAdmission(check); allowed {
		t.Fatal("unearned second check was admitted")
	}

	engine.grantFailedCheckRetry()
	if allowed, _ := engine.taskCheckAdmission(check); !allowed {
		t.Fatal("failed-check retry was not admitted")
	}
	if allowed, _ := engine.taskCheckAdmission(check); allowed {
		t.Fatal("more than one failed-check retry was admitted")
	}
}
