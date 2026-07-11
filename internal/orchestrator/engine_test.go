package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestEngineExecutesToolAndFinalizes(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "DONE = a.txt exists", ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"ok"}`)}, Usage: contract.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}},
		{Content: "Created `a.txt` and verified the write.", Usage: contract.Usage{PromptTokens: 120, CompletionTokens: 12, TotalTokens: 132}},
	}}
	tool := &recordingTool{name: "write_file"}
	settings := engineSettings()
	var events []string
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "s1", WorkspacePath: t.TempDir()}, Provider: provider,
		Registry: NewRegistry(tool), History: NewHistory(HistorySnapshot{Version: 1}, nil),
		Persistence: Persistence{AddEvent: func(_ context.Context, role, kind, _ string) error {
			events = append(events, role+":"+kind)
			return nil
		}},
		Prompt: PromptContext{Shell: "pwsh", Date: "2026-07-10", Model: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	answer, stats, err := engine.Run(context.Background(), "create a.txt with ok")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "Created `a.txt` and verified the write." || tool.calls != 1 {
		t.Fatalf("answer=%q calls=%d", answer, tool.calls)
	}
	if stats.ToolCalls != 1 || stats.DoneCriteria != "a.txt exists" || stats.Usage.TotalTokens != 242 {
		t.Fatalf("stats=%+v", stats)
	}
	if len(events) < 4 {
		t.Fatalf("events not persisted: %v", events)
	}
	messages := engine.history.All()
	if len(messages) < 4 || messages[2].Role != contract.RoleTool || messages[2].ToolCallID != "c1" {
		t.Fatalf("tool pairing not retained: %+v", messages)
	}
}

// A chat turn now uses the SAME session-stable system prompt and tool set as a
// coding turn (no lite/full split), which is what keeps the DeepSeek prefix
// cache warm across mixed conversations.
func TestEngineChatUsesStablePromptAndTools(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "Hello."}}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Date: "2026-07-10"}})
	answer, stats, err := engine.Run(context.Background(), "hi")
	if err != nil || answer != "Hello." || stats.TaskClass != "chat" {
		t.Fatalf("answer=%q stats=%+v err=%v", answer, stats, err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(provider.requests))
	}
	if len(provider.requests[0].Tools) == 0 {
		t.Fatal("chat turn should carry the full stable tool set for cache stability")
	}
	if !strings.Contains(provider.requests[0].Messages[0].Content, "OPERATING CONTRACT") {
		t.Fatal("chat turn did not use the full session system prompt")
	}
	if provider.requests[0].Reasoning != contract.ReasoningMedium {
		t.Fatalf("reasoning should be the user's effort level sent raw, got %q", provider.requests[0].Reasoning)
	}
}

// TestEngineHiTwiceKeepsStablePrefix drives the exact "hi" then "hi" scenario
// the user measured. The second request's message prefix must equal the first
// request's messages byte-for-byte (system prompt, first user turn, assistant
// reply) — that identical prefix is what DeepSeek's cache reuses, taking the
// hit rate from ~73% toward ~99%.
func TestEngineHiTwiceKeepsStablePrefix(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "Hi there!"}, {Content: "Hello again!"}}}
	settings := engineSettings()
	settings.Effort = contract.EffortLow
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Date: "2026-07-11", Model: "deepseek-v4-pro"}})
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("expected two requests, got %d", len(provider.requests))
	}
	r1, r2 := provider.requests[0], provider.requests[1]
	if len(r2.Messages) <= len(r1.Messages) {
		t.Fatalf("second turn did not append to a stable prefix: %d vs %d", len(r2.Messages), len(r1.Messages))
	}
	for i := range r1.Messages {
		if r1.Messages[i].Role != r2.Messages[i].Role || r1.Messages[i].Content != r2.Messages[i].Content {
			t.Fatalf("prefix message %d changed between turns:\n  turn1=%q\n  turn2=%q", i, r1.Messages[i].Content, r2.Messages[i].Content)
		}
	}
	// The tool schema block must also be identical across turns.
	if len(r1.Tools) != len(r2.Tools) {
		t.Fatalf("tool schema set changed between turns: %d vs %d", len(r1.Tools), len(r2.Tools))
	}
}

type scriptedProvider struct {
	mu        sync.Mutex
	responses []contract.ChatResponse
	requests  []contract.ChatRequest
}

func (p *scriptedProvider) Chat(_ context.Context, request contract.ChatRequest) (contract.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	if len(p.responses) == 0 {
		return contract.ChatResponse{Content: "done"}, nil
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func (p *scriptedProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }

type recordingTool struct {
	name  string
	calls int
}

func (t *recordingTool) Definition() contract.ToolDefinition {
	return definition(t.name, "test", map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, nil)
}

func (t *recordingTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	t.calls++
	return "Wrote a.txt.", nil
}

func engineSettings() contract.Settings {
	var settings contract.Settings
	settings.Version = 1
	settings.Provider.Type = "openai-compatible"
	settings.Provider.ActiveModelID = "main"
	settings.Provider.SubagentModelID = "main"
	settings.Provider.Models = []contract.Model{{ID: "main", Name: "Test", ContextLimit: 128000}}
	settings.Effort = contract.EffortMedium
	settings.PermissionMode = contract.PermissionAutoAccept
	return settings
}
