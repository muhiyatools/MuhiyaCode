package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/workspace"
)

func TestToolSurfaceSelectionLiveParity(t *testing.T) {
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	settings.TokenEconomyMode = "balanced"

	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "surface-check", WorkspacePath: dir},
		Provider: &scriptedProvider{},
		Registry: NewRegistry(service.Tools()...),
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionDefs := engine.sessionDefinitions()
	sessionToolMap := make(map[string]bool)
	for _, def := range sessionDefs {
		sessionToolMap[def.Function.Name] = true
	}

	// Verify required core tools are in session definitions
	requiredCore := []string{
		"inspect_workspace", "edit_file", "write_file", "apply_patch",
		"run_shell", "ask_user", "discover_tools", "invoke_tool",
	}
	for _, name := range requiredCore {
		if !sessionToolMap[name] {
			t.Errorf("expected core session tool %q not found in sessionDefinitions()", name)
		}
	}

	// Verify brokerable tools not in session definitions can be discovered
	brokerable := engine.brokerableDefinitions()
	var deferredCount int
	for _, def := range brokerable {
		name := def.Function.Name
		if !sessionToolMap[name] && name != "discover_tools" && name != "invoke_tool" {
			deferredCount++
		}
	}
	if deferredCount == 0 {
		t.Errorf("expected deferred brokerable tools, got 0")
	}

	descriptors := engine.brokerDescriptors("", 50)
	if len(descriptors) < deferredCount {
		t.Errorf("brokerDescriptors returned %d descriptors, want at least %d", len(descriptors), deferredCount)
	}
}

func TestToolBrokerParityAllDeferredToolsResolvable(t *testing.T) {
	dir := t.TempDir()
	trust := workspace.NewMemoryTrustStore()
	if err := trust.Trust(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	service, err := workspace.New(dir, workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: trust})
	if err != nil {
		t.Fatal(err)
	}
	settings := engineSettings()
	settings.TokenEconomyMode = "balanced"

	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "broker-parity-check", WorkspacePath: dir},
		Provider: &scriptedProvider{},
		Registry: NewRegistry(service.Tools()...),
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionDefs := engine.sessionDefinitions()
	sessionMap := make(map[string]bool)
	for _, def := range sessionDefs {
		sessionMap[def.Function.Name] = true
	}

	descriptors := engine.brokerDescriptors("", 100)
	for _, desc := range descriptors {
		if sessionMap[desc.CanonicalName] {
			t.Errorf("brokered tool %q should not be directly present in core session definitions", desc.CanonicalName)
		}
		if desc.SchemaHash == "" {
			t.Errorf("brokered tool %q has empty schemaHash", desc.CanonicalName)
		}
	}
}
