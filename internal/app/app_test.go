package app

import (
	"context"
	"strings"
	"testing"
)

// eAcute is "e" with a combining acute accent, built from explicit code points
// so no editor normalizing this source file can turn a test into a tautology.
// decomposedE is the NFD form (e + U+0301); composedE is the NFC form (U+00E9).
var (
	decomposedE = string([]rune{0x0065, 0x0301})
	composedE   = string(rune(0x00e9))
)

// TestAssemblePromptNormalizesForEveryFrontend pins the fix for a real
// divergence: the terminal UI normalized to NFC and line mode did too, but
// one-shot did not — so identical input produced different prompt bytes (and
// therefore different cache behavior) depending on how you launched. Assembly
// now lives in one place every frontend calls.
func TestAssemblePromptNormalizesForEveryFrontend(t *testing.T) {
	decomposed := "caf" + decomposedE + " refactor"
	composed := "caf" + composedE + " refactor"
	if decomposed == composed {
		t.Fatal("fixture is not actually decomposed — the test would prove nothing")
	}
	got := AssemblePrompt(context.Background(), decomposed, nil, nil, nil)
	if got != composed {
		t.Fatalf("normalized prompt = %q, want %q", got, composed)
	}
	// Idempotent: assembling an already-composed prompt changes nothing.
	if again := AssemblePrompt(context.Background(), got, nil, nil, nil); again != got {
		t.Fatalf("assembly is not idempotent: %q", again)
	}
}

// TestAssemblePromptExpandsPastesBeforeNormalizing pins the ORDER: a pasted
// block is reconstructed first and the whole prompt normalized after, so
// decomposed text arriving through a paste is composed too — not just what the
// user typed directly.
func TestAssemblePromptExpandsPastesBeforeNormalizing(t *testing.T) {
	expand := func(s string) string { return strings.ReplaceAll(s, "[#1]", "caf"+decomposedE) }
	got := AssemblePrompt(context.Background(), "look at [#1]", nil, expand, nil)
	if got != "look at caf"+composedE {
		t.Fatalf("paste expansion did not feed normalization: %q", got)
	}
}

// TestAssemblePromptWrapsSkillsDeterministically: skill order must not depend
// on map iteration, or two identical turns would send different bytes and miss
// the prompt cache.
func TestAssemblePromptWrapsSkillsDeterministically(t *testing.T) {
	skills := []Skill{
		{Name: "zebra", Instructions: "z body"},
		{Name: "alpha", Instructions: "a body"},
	}
	first := AssemblePrompt(context.Background(), "do the thing", skills, nil, nil)
	second := AssemblePrompt(context.Background(), "do the thing", []Skill{skills[1], skills[0]}, nil, nil)
	if first != second {
		t.Fatalf("skill order leaked into the prompt:\n%q\n%q", first, second)
	}
	if strings.Index(first, "alpha") > strings.Index(first, "zebra") {
		t.Fatal("skills are not sorted by name")
	}
	if !strings.Contains(first, `<skill name="alpha">`) || !strings.Contains(first, "a body") {
		t.Fatalf("skill section malformed: %q", first)
	}
	if !strings.HasSuffix(first, "User prompt:\ndo the thing") {
		t.Fatalf("the user prompt must come last: %q", first)
	}
}

// TestAssemblePromptLoadsSkillBodiesOnDemand covers the lazy path: a skill
// listed with only a path gets its body fetched, and a failed fetch falls back
// to the description rather than emitting an empty section.
func TestAssemblePromptLoadsSkillBodiesOnDemand(t *testing.T) {
	load := func(_ context.Context, path string) (string, error) {
		if path == "good/SKILL.md" {
			return "loaded body", nil
		}
		return "", context.DeadlineExceeded
	}
	got := AssemblePrompt(context.Background(), "task", []Skill{
		{Name: "good", Path: "good/SKILL.md"},
		{Name: "bad", Path: "bad/SKILL.md", Description: "fallback description"},
	}, nil, load)
	if !strings.Contains(got, "loaded body") {
		t.Fatalf("on-demand body not loaded: %q", got)
	}
	if !strings.Contains(got, "fallback description") {
		t.Fatalf("failed load did not fall back to the description: %q", got)
	}
}

func TestAssemblePromptWithoutSkillsAddsNoPreamble(t *testing.T) {
	if got := AssemblePrompt(context.Background(), "just do it", nil, nil, nil); got != "just do it" {
		t.Fatalf("a skill-less prompt was decorated: %q", got)
	}
}
