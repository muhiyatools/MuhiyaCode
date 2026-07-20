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
// v1.1.0 (native agent) baseline 5700 against an actual 5685 in THIS context
// (HasWeb+HasSubagents; the wire golden composes ~14 fewer without web).
// Fifteen chars of headroom, deliberately tight.
//
// This release still ships SMALLER than the 5789 it replaced, while adding a
// plan/execute contract, a tasks.md convention, a PLANNING section, and the
// field-test fixes. The +257 over the mid-release 5428 buys exactly two
// things, both of which stop recurring per-task waste:
//
//   - Trust-the-report (rule 5). The field test exposed a CONTRADICTION: the
//     contract said "run the checks yourself" while DELEGATION said "treat its
//     report as ground truth", and the model resolved it by re-reading every
//     file an agent had just verified. A one-time prompt cost that removes a
//     per-task re-verification loop is the cheapest trade available here.
//   - The executor-ready standard (PLANNING step 3). Items must be runnable by
//     a cheaper model without design decisions — that IS the plan/execute
//     split's payoff, and an under-specified item costs far more in executor
//     turns than the sentence costs in prefix.
//
// Paid for in part by deleting the old rule 5 (duplicated DELEGATION), the
// agent-allowance clauses, and the handoff detail now stated once in
// DELEGATION instead of twice.
const promptBaselineChars = 5700

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
