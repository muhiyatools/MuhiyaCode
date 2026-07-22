package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

func brokerWorkspaceEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(root, workspace.Options{
		PermissionMode: contract.PermissionNormal, Trust: workspace.NewMemoryTrustStore(),
		Approver: workspace.ApproverFunc(func(context.Context, workspace.ApprovalRequest) (bool, error) { return false, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{settings: &contract.Settings{TokenEconomyMode: "balanced"}, registry: NewRegistry(service.Tools()...),
		skills: NewSkillCatalog(nil), inspection: NewInspection(InspectionSnapshot{Version: 3}, nil, root),
		history: NewHistory(HistorySnapshot{Version: 1}, nil), taskCounters: newCallCounters()}
	return e, root
}

func brokerInvocation(t *testing.T, e *Engine, name string, arguments map[string]any) contract.ToolCall {
	t.Helper()
	var definition contract.ToolDefinition
	for _, candidate := range e.brokerableDefinitions() {
		if candidate.Function.Name == name {
			definition = candidate
			break
		}
	}
	if definition.Function.Name == "" {
		t.Fatalf("missing definition %s", name)
	}
	body, _ := json.Marshal(map[string]any{"name": name, "schemaHash": toolSchemaHash(definition), "arguments": arguments})
	return contract.NewToolCall("broker-"+name, "invoke_tool", string(body))
}

func TestToolBrokerPreservesPermissionContainmentSecretAndShellGates(t *testing.T) {
	e, root := brokerWorkspaceEngine(t)
	definitions := e.sessionDefinitions()
	effort := EffortProfile{ToolOutputCap: 4096}
	read := e.executeCall(context.Background(), brokerInvocation(t, e, "read_file", map[string]any{"path": "safe.txt"}), definitions, effort)
	if read.Failed || read.Call.ToolName() != "read_file" {
		t.Fatalf("read=%+v", read)
	}
	edit := e.executeCall(context.Background(), brokerInvocation(t, e, "edit_file", map[string]any{
		"path": "safe.txt", "oldString": "old", "newString": "new"}), definitions, effort)
	if !edit.Failed || edit.Call.ToolName() != "edit_file" {
		t.Fatalf("permission bypass: %+v", edit)
	}
	secret := e.executeCall(context.Background(), brokerInvocation(t, e, "read_file", map[string]any{"path": ".env"}), definitions, effort)
	if !secret.Failed {
		t.Fatalf("secret read bypass: %+v", secret)
	}
	outside := e.executeCall(context.Background(), brokerInvocation(t, e, "read_file", map[string]any{"path": filepath.Join(filepath.Dir(root), "outside.txt")}), definitions, effort)
	if !outside.Failed {
		t.Fatalf("containment bypass: %+v", outside)
	}
	shell := e.executeCall(context.Background(), brokerInvocation(t, e, "run_shell", map[string]any{"command": "git reset --hard"}), definitions, effort)
	if !shell.Failed {
		t.Fatalf("destructive shell bypass: %+v", shell)
	}
}
