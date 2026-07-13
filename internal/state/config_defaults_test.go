package state

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestDefaultEffortIsHigh (006) locks the new default reasoning effort.
func TestDefaultEffortIsHigh(t *testing.T) {
	if got := DefaultSettings().Effort; got != contract.EffortHigh {
		t.Fatalf("default effort = %q, want high", got)
	}
}

// TestAutoAssignPrefersProMainFlashSub (006) verifies the discovered-model role
// defaults: the Pro-class model runs the main loop and the Flash-class model runs
// subagents, regardless of catalog order.
func TestAutoAssignPrefersProMainFlashSub(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextLimit: 1_000_000},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextLimit: 1_000_000},
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "gemini-2.5-pro" {
		t.Fatalf("main model = %q, want the Pro model", settings.Provider.ActiveModelID)
	}
	if settings.Provider.SubagentModelID != "gemini-2.5-flash" {
		t.Fatalf("subagent model = %q, want the Flash model", settings.Provider.SubagentModelID)
	}
}

// TestAutoAssignFallsBackWithoutFamilyNames (006) verifies the score-based
// fallback still assigns two distinct roles when no name matches Pro/Flash.
func TestAutoAssignFallsBackWithoutFamilyNames(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "alpha", Name: "Alpha", ContextLimit: 2_000_000},
		{ID: "beta", Name: "Beta", ContextLimit: 100_000},
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID == "" || settings.Provider.SubagentModelID == "" {
		t.Fatal("auto-assign left a role unset")
	}
	if settings.Provider.ActiveModelID == settings.Provider.SubagentModelID {
		t.Fatal("main and subagent should differ when two models exist")
	}
}

// TestAutoAssignRespectsExplicitChoice (006) verifies auto-assign never overrides
// a model the user already pinned.
func TestAutoAssignRespectsExplicitChoice(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "flash", Name: "Flash", ContextLimit: 1_000_000},
		{ID: "pro", Name: "Pro", ContextLimit: 1_000_000},
	}
	settings.Provider.ActiveModelID = "flash" // user pinned Flash as main
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "flash" {
		t.Fatalf("auto-assign overrode the pinned main model: %q", settings.Provider.ActiveModelID)
	}
}
