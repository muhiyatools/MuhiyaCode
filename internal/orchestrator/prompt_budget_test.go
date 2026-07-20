package orchestrator

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/gateway"
)

// 004 US4 (T041/FR-018): the system prompt must not exceed its recorded
// baseline; any growth past this ceiling fails the build so the "do not bloat
// the prompt" contract holds. Re-baselined ONCE for feature 010 US3's recorded
// cache epoch (contracts/instruction-system.md IS-9/IS-11): incoherence fix
// (d) aligned the write_file permission sentence between the tool description
// and this system prompt's CONTEXT AND EDIT DISCIPLINE section (they
// previously stated different rules for when write_file may replace an
// existing file) — +80 chars. This is the ONE sanctioned prefix delta for the
// whole instruction-consolidation feature; every other moved text is
// byte-identical (internal/instructions/dump_test.go's Prefix-bytes golden
// pins the exact rendered result). Prior baselines: 3642 (feature 004), 4522
// (feature 008), 5709 (feature 009), 5789 (feature 010).
//
// v1.1.0 (native agent) RE-BASELINED DOWNWARD to 4900: deleting the
// ORCHESTRATION PIPELINE section shrank the rendered prompt from 5777 to 4798
// chars; naming the tasks.md checklist format in the operating contract added
// 88 back (4886). The ratchet is a ceiling, so shrinkage never fails CI — this
// tightening is deliberate, so the budget keeps meaning something.
const promptBaselineChars = 4900

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
