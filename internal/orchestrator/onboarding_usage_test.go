package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// Feature 011 T045 (audit F15): the onboarding aux call runs BEFORE the
// per-task counters reset, and lands inside the emitted task usage delta only
// because taskUsageStart is captured before it. This pins that ordering — and
// the onboarding stream's dedicated ":sub:onboarding" cache pin (D4).
func TestOnboardingUsageLandsInsideTaskDelta(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		// Onboarding response: not JSON, so no questions — usage still counts.
		{Content: "not json", Usage: contract.Usage{PromptTokens: 300, CompletionTokens: 50, PromptTokensAvailable: true, CompletionTokensAvailable: true}},
		{Content: "done.", Usage: contract.Usage{PromptTokens: 800, CompletionTokens: 40, PromptTokensAvailable: true, CompletionTokensAvailable: true}},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortMedium // Onboarding: true
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "onboard-usage", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt:    PromptContext{Model: "Test"},
		Callbacks: contract.Callbacks{Ask: func(context.Context, []contract.Question) ([]contract.Answer, error) { return nil, nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "improve the project error handling"
	if !ShouldConsiderOnboarding(prompt) {
		t.Fatal("fixture prompt must trigger onboarding")
	}
	_, stats, err := engine.Run(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	// The task delta must cover BOTH requests (onboarding + main).
	if stats.Usage.PromptTokens != 1100 || stats.Usage.CompletionTokens != 90 {
		t.Fatalf("task usage delta = %d/%d, want 1100/90 (onboarding included)", stats.Usage.PromptTokens, stats.Usage.CompletionTokens)
	}
	// The onboarding request rode its own per-kind pin.
	if len(provider.requests) < 1 || !strings.HasSuffix(provider.requests[0].SessionID, ":sub:onboarding") {
		t.Fatalf("onboarding pin = %q, want :sub:onboarding suffix", provider.requests[0].SessionID)
	}
	// And appears as its own pairing row.
	found := false
	for _, pairing := range stats.PerPairing {
		if pairing.Pin == ":sub:onboarding" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no :sub:onboarding pairing row: %+v", stats.PerPairing)
	}
}

func TestEconomyOnboardingSuppressesClearLowComplexityTasks(t *testing.T) {
	tests := []string{
		"hello",
		"Fix the typo in README.md: change teh to the",
		"In web/styles.css change the submit button color to green",
		"In internal/user.go return ErrNotFound when lookup misses",
	}
	for _, prompt := range tests {
		decision := DecideOnboarding(prompt, Classify(prompt, ""))
		if decision.Action != OnboardingSuppressed || decision.UseAuxiliaryModel {
			t.Fatalf("prompt %q admitted onboarding: %+v", prompt, decision)
		}
	}
}

func TestEconomyOnboardingUsesDirectQuestionForExplicitMaterialAmbiguity(t *testing.T) {
	prompt := "Update login to use either passkeys or OAuth; I have not chosen which authentication method."
	decision := DecideOnboarding(prompt, Classify(prompt, ""))
	if decision.Action != OnboardingAskDirect || decision.UseAuxiliaryModel {
		t.Fatalf("decision=%+v, want direct ask_user without an auxiliary model", decision)
	}
	if len(decision.Questions) != 1 || !strings.Contains(strings.ToLower(decision.Questions[0].Question), "authentication") {
		t.Fatalf("direct material question missing: %+v", decision.Questions)
	}
}

func TestEconomyOnboardingAdmissionDoesNotSpendAuxiliaryRequest(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		wantAsks int
	}{
		{name: "clear named-file task", prompt: "Fix the typo in README.md: change teh to the"},
		{name: "explicit material choice", prompt: "Update login to use either passkeys or OAuth; I have not chosen which authentication method.", wantAsks: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "done", Usage: guardUsage(0, 20)}}}
			settings := engineSettings()
			settings.Effort = contract.EffortMedium
			askQuestions := 0
			engine, err := NewEngine(EngineConfig{
				Settings: &settings, Session: contract.Session{ID: "admission", WorkspacePath: t.TempDir()},
				Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"},
				Callbacks: contract.Callbacks{Ask: func(context.Context, []contract.Question) ([]contract.Answer, error) {
					askQuestions++
					return nil, nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := engine.Run(context.Background(), test.prompt); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("provider requests=%d, want one main request", len(provider.requests))
			}
			if askQuestions != test.wantAsks {
				t.Fatalf("direct asks=%d, want %d", askQuestions, test.wantAsks)
			}
		})
	}
}
