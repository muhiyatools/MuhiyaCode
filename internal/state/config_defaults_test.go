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

// The session runs on one model, so auto-assign picks the capable one: a
// Pro-class name beats a Flash-class name regardless of catalog order.
func TestAutoAssignPrefersTheProClassModel(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextLimit: 1_000_000},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextLimit: 1_000_000},
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "gemini-2.5-pro" {
		t.Fatalf("model = %q, want the Pro model", settings.Provider.ActiveModelID)
	}
}

// With no family name to go on, the score-based fallback still picks something —
// the largest window wins.
func TestAutoAssignFallsBackWithoutFamilyNames(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "alpha", Name: "Alpha", ContextLimit: 2_000_000},
		{ID: "beta", Name: "Beta", ContextLimit: 100_000},
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "alpha" {
		t.Fatalf("score fallback picked %q, want the larger window", settings.Provider.ActiveModelID)
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
	settings.Provider.ActiveModelID = "flash" // user pinned Flash
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "flash" {
		t.Fatalf("auto-assign overrode the pinned model: %q", settings.Provider.ActiveModelID)
	}
}

// A fresh install lands on M3: the largest window in the catalog, which is what
// a single-model session wants before it knows what the user will ask for.
func TestFreshDefaultsPickMiniMaxM3(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
		{ID: "model-minimax-m3", Name: "MiniMax M3", ContextLimit: 1_000_000},
	}
	if !AssignFreshDefaultModels(&settings) {
		t.Fatal("fresh-install default was not recognized")
	}
	if settings.Provider.ActiveModelID != "model-minimax-m3" {
		t.Fatalf("fresh default = %q, want MiniMax M3", settings.Provider.ActiveModelID)
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
		t.Fatal("a catalog with no M3 matched the fresh-install rule")
	}
	AutoAssignModels(&got)
	if got.Provider.ActiveModelID != want.Provider.ActiveModelID {
		t.Fatalf("DeepSeek-only default changed: got %q want %q", got.Provider.ActiveModelID, want.Provider.ActiveModelID)
	}
}

func TestFreshDefaultsNeverOverrideAPinnedModel(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{{ID: "pinned", Name: "Pinned"}, {ID: "minimax-m3", Name: "MiniMax M3"}}
	settings.Provider.ActiveModelID = "pinned"
	if AssignFreshDefaultModels(&settings) {
		t.Fatal("a pinned model matched the fresh-install rule")
	}
	AutoAssignModels(&settings)
	if settings.Provider.ActiveModelID != "pinned" {
		t.Fatalf("pinned model changed to %q", settings.Provider.ActiveModelID)
	}
}

// TestFreshDefaultsRecognizeBareM3Name locks in the detector consolidation:
// the default uses the SAME M3 detector as the capability profile
// (contract.IsMiniMaxM3Name), so a catalog entry named bare "M3" — which the
// profile resolver already treats as MiniMax-M3 — is recognized instead of
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
	if settings.Provider.ActiveModelID != "m3" {
		t.Fatalf("fresh default = %q, want the M3 entry", settings.Provider.ActiveModelID)
	}
}

func TestTokenEconomyModeDefaultsToOff(t *testing.T) {
	if got := DefaultSettings().TokenEconomyMode; got != "off" {
		t.Fatalf("token economy mode=%q, want off", got)
	}
}

func TestNormalizeTokenEconomyModeDeterministic(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"off", "off", true},
		{" OBSERVE ", "observe", true},
		{"Balanced", "balanced", true},
		{"AGGRESSIVE", "aggressive", true},
		{"", "off", true},
		{"maximum", "", false},
		{"on", "", false},
	}
	for _, test := range tests {
		got, ok := normalizeTokenEconomyMode(test.input)
		if got != test.want || ok != test.ok {
			t.Fatalf("normalizeTokenEconomyMode(%q)=(%q,%v), want (%q,%v)", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestSetConfigTokenEconomyModeRejectsUnknown(t *testing.T) {
	settings := DefaultSettings()
	secrets := DefaultSecrets()
	if err := SetConfig("tokenEconomyMode", "balanced", &settings, &secrets); err != nil {
		t.Fatal(err)
	}
	if settings.TokenEconomyMode != "balanced" {
		t.Fatalf("mode=%q", settings.TokenEconomyMode)
	}
	if err := SetConfig("tokenEconomyMode", "mystery", &settings, &secrets); err == nil {
		t.Fatal("unknown mode was accepted")
	}
	if settings.TokenEconomyMode != "balanced" {
		t.Fatalf("invalid update mutated mode to %q", settings.TokenEconomyMode)
	}
}
