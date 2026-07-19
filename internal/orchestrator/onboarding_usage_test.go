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
