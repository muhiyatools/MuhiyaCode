package gateway

import "testing"

// TestDeepSeekProfileLimits pins the four DeepSeek token values against the
// documented API (feature 007). The operational pair (MaxOutputTokens /
// DefaultContextWindow) and the documented-ceiling pair (OutputTokenLimit /
// ContextWindowLimit) are deliberately distinct and must not be conflated.
func TestDeepSeekProfileLimits(t *testing.T) {
	profile := ResolveModelProfile("deepseek-chat")
	if profile.Family != "deepseek" {
		t.Fatalf("expected deepseek family, got %q", profile.Family)
	}
	if profile.OutputTokenLimit != 384_000 {
		t.Errorf("OutputTokenLimit = %d, want 384000", profile.OutputTokenLimit)
	}
	if profile.ContextWindowLimit != 1_000_000 {
		t.Errorf("ContextWindowLimit = %d, want 1000000", profile.ContextWindowLimit)
	}
	if profile.MaxOutputTokens != 16_000 {
		t.Errorf("MaxOutputTokens = %d, want 16000", profile.MaxOutputTokens)
	}
	if profile.DefaultContextWindow != 128_000 {
		t.Errorf("DefaultContextWindow = %d, want 128000", profile.DefaultContextWindow)
	}
}

// TestClampOutputTokens covers the three clamp outcomes (CP-3): an over-large
// request is reduced to the documented ceiling, an in-range request passes
// through untouched, and a family with an unknown (0) limit is never clamped so
// non-DeepSeek providers degrade gracefully (CP-7).
func TestClampOutputTokens(t *testing.T) {
	deepseek := ResolveModelProfile("deepseek-chat")
	if value, clamped := deepseek.ClampOutputTokens(500_000); value != 384_000 || !clamped {
		t.Errorf("ClampOutputTokens(500000) = (%d, %v), want (384000, true)", value, clamped)
	}
	if value, clamped := deepseek.ClampOutputTokens(16_000); value != 16_000 || clamped {
		t.Errorf("ClampOutputTokens(16000) = (%d, %v), want (16000, false)", value, clamped)
	}
	generic := ResolveModelProfile("minimax-01")
	if generic.OutputTokenLimit != 0 {
		t.Fatalf("expected an unknown (0) output-token limit for the minimax family, got %d", generic.OutputTokenLimit)
	}
	if value, clamped := generic.ClampOutputTokens(500_000); clamped {
		t.Errorf("an unknown limit must never clamp: got (%d, %v)", value, clamped)
	}
}

// TestDeprecatedParamsNeverSupported asserts the deprecated DeepSeek sampling
// penalties are recognised as deprecated (CP-2) and that no deprecated param is
// simultaneously advertised as supported.
func TestDeprecatedParamsNeverSupported(t *testing.T) {
	profile := ResolveModelProfile("deepseek-chat")
	for _, name := range []string{"frequency_penalty", "presence_penalty"} {
		if !profile.IsDeprecatedParam(name) {
			t.Errorf("IsDeprecatedParam(%q) = false, want true", name)
		}
	}
	supported := make(map[string]bool, len(profile.SupportedParams))
	for _, name := range profile.SupportedParams {
		supported[name] = true
	}
	for _, name := range profile.DeprecatedParams {
		if supported[name] {
			t.Errorf("deprecated param %q must not appear in SupportedParams", name)
		}
	}
}

// TestBetaFeaturesRecorded checks the three DeepSeek beta features are recorded
// with a decision (CP-6): each must carry a non-empty Status and Rationale so a
// later reevaluation has the context in one place.
func TestBetaFeaturesRecorded(t *testing.T) {
	profile := ResolveModelProfile("deepseek-chat")
	byName := make(map[string]BetaFeature, len(profile.BetaFeatures))
	for _, feature := range profile.BetaFeatures {
		byName[feature.Name] = feature
	}
	for _, name := range []string{"chat_prefix_completion", "fim_completion", "strict_tool_schemas"} {
		feature, ok := byName[name]
		if !ok {
			t.Errorf("BetaFeatures missing %q", name)
			continue
		}
		if feature.Status == "" {
			t.Errorf("BetaFeature %q has an empty Status", name)
		}
		if feature.Rationale == "" {
			t.Errorf("BetaFeature %q has an empty Rationale", name)
		}
	}
}
