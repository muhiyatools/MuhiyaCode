package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestTaskDefinitionsUseStableIntegrationBroker(t *testing.T) {
	engine := newHydrationTestEngine(t)
	first := engine.taskDefinitions()
	second := engine.taskDefinitions()
	if !hasDefinition(first, "read_file") || !hasDefinition(first, "integration_tools") {
		t.Fatalf("stable core is incomplete: %v", toolNames(first))
	}
	if hasDefinition(first, "mcp__github__search_code") {
		t.Fatalf("MCP schema was eagerly advertised: %v", toolNames(first))
	}
	firstRaw, _ := json.Marshal(first)
	secondRaw, _ := json.Marshal(second)
	if string(firstRaw) != string(secondRaw) {
		t.Fatal("stable integration broker changed the tool prefix")
	}
}

func TestIntegrationBrokerListsAndCallsMCPTool(t *testing.T) {
	integration := &readOnlyHydrationTool{name: "mcp__github__search_code"}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "hydrate", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{},
		Registry: NewRegistry(&recordingTool{name: "read_file"}, integration),
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := engine.integrationTools(context.Background(), json.RawMessage(`{"action":"list","query":"github"}`))
	if err != nil || !strings.Contains(catalog, integration.name) {
		t.Fatalf("list integrations: %v %s", err, catalog)
	}
	output, err := engine.integrationTools(context.Background(), json.RawMessage(`{"action":"call","name":"mcp__github__search_code","arguments":{}}`))
	if err != nil || output != `{"matches":[]}` || integration.calls != 1 {
		t.Fatalf("call integration: output=%q err=%v calls=%d", output, err, integration.calls)
	}
}

func TestRunCallsIntegrationWithoutPrefixHydration(t *testing.T) {
	integration := &readOnlyHydrationTool{name: "mcp__github__search_code"}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("lookup", "integration_tools", `{"action":"call","name":"mcp__github__search_code","arguments":{}}`)}},
		{Content: "lookup complete"},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "hydrate-run", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "read_file"}, integration),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "look up integration details"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || integration.calls != 1 {
		t.Fatalf("requests=%d integration calls=%d", len(provider.requests), integration.calls)
	}
	first, _ := json.Marshal(provider.requests[0].Tools)
	second, _ := json.Marshal(provider.requests[1].Tools)
	if string(first) != string(second) || hasDefinition(provider.requests[0].Tools, integration.name) {
		t.Fatal("integration call mutated the prompt tool prefix")
	}
}

type readOnlyHydrationTool struct {
	name  string
	calls int
}

func (t *readOnlyHydrationTool) Definition() contract.ToolDefinition {
	return definition(t.name, "Search integration code.", map[string]any{}, nil)
}

func (t *readOnlyHydrationTool) Execute(context.Context, json.RawMessage) contract.ToolResult {
	t.calls++
	return contract.AdaptToolResult(`{"matches":[]}`, nil)
}

func (t *readOnlyHydrationTool) DeclaresReadOnly() bool { return true }

func newHydrationTestEngine(t *testing.T) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "hydrate", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{},
		Registry: NewRegistry(
			&recordingTool{name: "read_file"},
			&recordingTool{name: "mcp__github__search_code"},
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
