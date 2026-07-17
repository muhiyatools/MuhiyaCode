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

func TestFreshDefaultsPairMiniMaxM3WithDeepSeekV4Pro(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
		{ID: "model-minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
	}
	if !AssignFreshDefaultModels(&settings) {
		t.Fatal("mixed fresh-install pair was not recognized")
	}
	if settings.Provider.ActiveModelID != "model-minimax-m3" || settings.Provider.SubagentModelID != "deepseek-v4-pro" {
		t.Fatalf("fresh pair = main %q sub %q", settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
	}
}

func TestFreshDefaultsDeepSeekOnlyKeepsAutoAssign(t *testing.T) {
	models := []contract.Model{
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash", ContextLimit: 1_000_000},
	}
	want := DefaultSettings()
	want.Provider.Models = append([]contract.Model(nil), models...)
	AutoAssignModels(&want)
	got := DefaultSettings()
	got.Provider.Models = append([]contract.Model(nil), models...)
	if AssignFreshDefaultModels(&got) {
		t.Fatal("DeepSeek-only catalog matched the mixed-provider rule")
	}
	AutoAssignModels(&got)
	if got.Provider.ActiveModelID != want.Provider.ActiveModelID || got.Provider.SubagentModelID != want.Provider.SubagentModelID {
		t.Fatalf("DeepSeek-only defaults changed: got %q/%q want %q/%q", got.Provider.ActiveModelID, got.Provider.SubagentModelID, want.Provider.ActiveModelID, want.Provider.SubagentModelID)
	}
}

func TestFreshDefaultsNeverOverridePinnedRoles(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{{ID: "pinned-main", Name: "Pinned Main"}, {ID: "pinned-sub", Name: "Pinned Sub"}, {ID: "minimax-m3", Name: "MiniMax M3"}, {ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro"}}
	settings.Provider.ActiveModelID = "pinned-main"
	settings.Provider.SubagentModelID = "pinned-sub"
	if AssignFreshDefaultModels(&settings) {
		t.Fatal("pinned settings matched the fresh-install rule")
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "pinned-main" || settings.Provider.SubagentModelID != "pinned-sub" {
		t.Fatalf("pinned roles changed to %q/%q", settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
	}
}

// TestFreshDefaultsRecognizeBareM3Name locks in the detector consolidation:
// the pairing uses the SAME M3 detector as the capability profile
// (gateway.IsMiniMaxM3Name), so a catalog entry named bare "M3" — which the
// profile resolver already treats as MiniMax-M3 — pairs correctly instead of
// silently falling through to score-based assignment.
func TestFreshDefaultsRecognizeBareM3Name(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "m3", Name: "M3", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
	}
	if !AssignFreshDefaultModels(&settings) {
		t.Fatal("bare-M3 catalog entry was not recognized as MiniMax-M3")
	}
	if settings.Provider.ActiveModelID != "m3" || settings.Provider.SubagentModelID != "deepseek-v4-pro" {
		t.Fatalf("fresh pair = main %q sub %q", settings.Provider.ActiveModelID, settings.Provider.SubagentModelID)
	}
}

func TestFreshDefaultsMiniMaxOnlyFallsBackToScores(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{{ID: "minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000}, {ID: "minimax-m2.7", Name: "MiniMax M2.7", ContextLimit: 204_800}}
	if AssignFreshDefaultModels(&settings) {
		t.Fatal("MiniMax-only catalog matched the mixed-provider rule")
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID == "" || settings.Provider.SubagentModelID == "" {
		t.Fatalf("score fallback left roles unset: %+v", settings.Provider)
	}
}
