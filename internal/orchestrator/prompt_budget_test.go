package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/gateway"
)

// 004 US4 (T041/FR-018): the tightened system prompt must not exceed its
// pre-change size. promptBaselineChars is the composed DeepSeek-context prompt
// length recorded before the 004 tightening pass (priority rule + completion
// addendum added, re-read duplication removed). Any future growth past this
// ceiling fails the build so the "do not bloat the prompt" contract holds. The
// baseline is well under the ~1,900-token prompt ceiling asserted in meta_test.go.
const promptBaselineChars = 3642

func TestSystemPromptSizeWithinBudget(t *testing.T) {
	ctx := PromptContext{
		Workspace: "/w", OS: "linux", Shell: "bash", Model: "deepseek-v4-flash",
		HasWeb: true, HasSubagents: true, SubagentModel: "deepseek-v4-flash",
		ModelAddendum: gateway.ResolveModelProfile("deepseek-v4-flash").PromptAddendum,
	}
	if got := len(SystemPrompt(ctx)); got > promptBaselineChars {
		t.Fatalf("system prompt grew to %d chars, over the %d baseline (FR-018: do not bloat)", got, promptBaselineChars)
	}
}

// The DeepSeek completion-honesty addendum is family-gated: it rides the
// DeepSeek profile only, never a generic provider's.
func TestDeepSeekAddendumFamilyGated(t *testing.T) {
	const sentence = "Report only work actually performed"
	deepseek := gateway.ResolveModelProfile("deepseek-v4-flash").PromptAddendum
	if !strings.Contains(deepseek, sentence) {
		t.Fatalf("DeepSeek addendum missing completion-honesty sentence: %q", deepseek)
	}
	generic := gateway.ResolveModelProfile("some-other-model").PromptAddendum
	if strings.Contains(generic, sentence) {
		t.Fatalf("generic profile must not carry the DeepSeek-specific sentence: %q", generic)
	}
}
