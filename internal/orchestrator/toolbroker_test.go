package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type brokerTestTool struct {
	definition contract.ToolDefinition
	output     string
}

type brokerIdentityTool struct{ brokerTestTool }

func (t brokerIdentityTool) ToolServerFingerprint() string { return "server-account-fingerprint" }
func (t brokerIdentityTool) ToolAvailability() string      { return "ready" }

func (t brokerTestTool) Definition() contract.ToolDefinition { return t.definition }
func (t brokerTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

func brokerTestEngine() *Engine {
	deferred := definition("rare_lookup", "Lookup rare indexed records.", map[string]any{"query": map[string]any{"type": "string"}}, []string{"query"})
	return &Engine{settings: &contract.Settings{TokenEconomyMode: "balanced"}, registry: NewRegistry(brokerTestTool{definition: deferred, output: "ok"}),
		skills: NewSkillCatalog(nil), inspection: NewInspection(InspectionSnapshot{Version: 3}, nil),
		history: NewHistory(HistorySnapshot{Version: 1}, nil), taskCounters: newCallCounters()}
}

func TestToolBrokerDiscoveryResolutionAndStaleSchema(t *testing.T) {
	e := brokerTestEngine()
	output, err := e.discoverTools(json.RawMessage(`{"query":"rare records","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Tools []ToolDescriptor `json:"tools"`
	}
	if json.Unmarshal([]byte(output), &result) != nil || len(result.Tools) == 0 {
		t.Fatalf("output=%s", output)
	}
	descriptor := result.Tools[0]
	if descriptor.CanonicalName != "rare_lookup" {
		t.Fatalf("ranking=%+v", result.Tools)
	}
	arguments, _ := json.Marshal(map[string]any{"name": descriptor.CanonicalName, "schemaHash": descriptor.SchemaHash,
		"arguments": map[string]any{"query": "x"}})
	resolved, definitions, err := e.resolveBrokerCall(contract.NewToolCall("call-1", "invoke_tool", string(arguments)))
	if err != nil || resolved.ToolName() != "rare_lookup" || resolved.ID != "call-1" {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
	if err := validateCallArgs(resolved, definitions); err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(string(arguments), descriptor.SchemaHash, strings.Repeat("0", len(descriptor.SchemaHash)), 1)
	if _, _, err := e.resolveBrokerCall(contract.NewToolCall("call-2", "invoke_tool", stale)); err == nil {
		t.Fatal("stale schema accepted")
	}
	if _, _, err := e.resolveBrokerCall(contract.NewToolCall("call-3", "invoke_tool", `{"name":"invoke_tool","schemaHash":"x","arguments":{}}`)); err == nil {
		t.Fatal("recursive broker call accepted")
	}
}

func TestToolBrokerCanonicalResultIdentity(t *testing.T) {
	e := brokerTestEngine()
	descriptor := descriptorFor(e.registry.BaseDefinitions(nil)[0], false)
	arguments, _ := json.Marshal(map[string]any{"name": descriptor.CanonicalName, "schemaHash": descriptor.SchemaHash,
		"arguments": map[string]any{"query": "x"}})
	outcome := e.executeCall(context.Background(), contract.NewToolCall("call-1", "invoke_tool", string(arguments)), e.sessionDefinitions(), EffortProfile{ToolOutputCap: 1000})
	if outcome.Failed || outcome.Output != "ok" || outcome.Call.ToolName() != "rare_lookup" {
		t.Fatalf("outcome=%+v", outcome)
	}
}

func TestToolBrokerRequiresExactServerFingerprint(t *testing.T) {
	definition := definition("mcp__docs__search", "Search docs.", map[string]any{"query": map[string]any{"type": "string"}}, []string{"query"})
	e := &Engine{settings: &contract.Settings{TokenEconomyMode: "balanced"},
		registry: NewRegistry(brokerIdentityTool{brokerTestTool{definition: definition, output: "ok"}}),
		skills:   NewSkillCatalog(nil), inspection: NewInspection(InspectionSnapshot{Version: 3}, nil),
		history: NewHistory(HistorySnapshot{Version: 1}, nil), taskCounters: newCallCounters()}
	descriptors := e.brokerDescriptors("docs search", 5)
	if len(descriptors) != 1 || descriptors[0].ServerFingerprint == "" {
		t.Fatalf("descriptors=%+v", descriptors)
	}
	base := map[string]any{"name": definition.Function.Name, "schemaHash": descriptors[0].SchemaHash, "arguments": map[string]any{"query": "x"}}
	body, _ := json.Marshal(base)
	if _, _, err := e.resolveBrokerCall(contract.NewToolCall("one", "invoke_tool", string(body))); err == nil {
		t.Fatal("missing server fingerprint accepted")
	}
	base["serverFingerprint"] = descriptors[0].ServerFingerprint
	body, _ = json.Marshal(base)
	if _, _, err := e.resolveBrokerCall(contract.NewToolCall("two", "invoke_tool", string(body))); err != nil {
		t.Fatal(err)
	}
}
