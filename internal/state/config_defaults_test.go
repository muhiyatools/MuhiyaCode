package state

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestDefaultEffortIsHigh(t *testing.T) {
	if got := DefaultSettings().Effort; got != contract.EffortHigh {
		t.Fatalf("default effort = %q, want high", got)
	}
}

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

func TestFreshDefaultsNeverOverrideExplicitModel(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "explicit", Name: "Explicit"},
		{ID: "minimax-m3", Name: "MiniMax M3"},
	}
	settings.Provider.ActiveModelID = "explicit"
	if AssignFreshDefaultModels(&settings) {
		t.Fatal("an explicit model must not be replaced")
	}
	if settings.Provider.ActiveModelID != "explicit" {
		t.Fatalf("explicit model changed to %q", settings.Provider.ActiveModelID)
	}
}

func TestFreshDefaultsRecognizeBareM3Name(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{
		{ID: "m3", Name: "M3", ContextLimit: 1_000_000},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", ContextLimit: 1_000_000},
	}
	if !AssignFreshDefaultModels(&settings) {
		t.Fatal("bare-M3 catalog entry was not recognized")
	}
	if settings.Provider.ActiveModelID != "m3" {
		t.Fatalf("fresh default = %q, want m3", settings.Provider.ActiveModelID)
	}
}

func TestNormalizeDoesNotChooseOrReplaceModel(t *testing.T) {
	settings := DefaultSettings()
	settings.Provider.Models = []contract.Model{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	normalizeSettings(&settings)
	if settings.Provider.ActiveModelID != "" {
		t.Fatalf("normalization selected %q", settings.Provider.ActiveModelID)
	}
	settings.Provider.ActiveModelID = "missing-but-explicit"
	normalizeSettings(&settings)
	if settings.Provider.ActiveModelID != "missing-but-explicit" {
		t.Fatalf("normalization replaced explicit model with %q", settings.Provider.ActiveModelID)
	}
}
