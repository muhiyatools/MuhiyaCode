package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func catalogEngine(t *testing.T, models []contract.Model, configured string) *Engine {
	t.Helper()
	settings := engineSettings()
	settings.Provider.Models = models
	settings.Provider.ActiveModelID = configured
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "catalog", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{},
		Registry: NewRegistry(),
		Prompt:   PromptContext{Model: "Test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestCatalogPreflightRejectsMissingModelWithoutSubstitution(t *testing.T) {
	engine := catalogEngine(t, []contract.Model{
		{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextLimit: 128_000},
	}, "deepseek-v4-pro")

	err := engine.reconcileCatalog()
	if err == nil || !strings.Contains(err.Error(), "deepseek-v4-pro") {
		t.Fatalf("missing explicit model was not rejected: %v", err)
	}
	if got := engine.settings.Provider.ActiveModelID; got != "deepseek-v4-pro" {
		t.Fatalf("preflight substituted %q", got)
	}
}

func TestCatalogPreflightAcceptsExplicitAvailableModel(t *testing.T) {
	engine := catalogEngine(t, []contract.Model{
		{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
	}, "minimax-m3")
	if err := engine.reconcileCatalog(); err != nil {
		t.Fatal(err)
	}
	if engine.settings.Provider.ActiveModelID != "minimax-m3" {
		t.Fatal("catalog validation changed the explicit model")
	}
}

func TestCatalogPreflightAllowsManualEndpointWithoutCatalog(t *testing.T) {
	engine := catalogEngine(t, nil, "private-model")
	if err := engine.reconcileCatalog(); err != nil {
		t.Fatal(err)
	}
}
