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
// v1.1.0 (native agent) baseline 5270 against an actual 5253 in THIS context
// (HasWeb+HasSubagents; the wire golden composes 5246 without web). Seventeen
// chars of headroom, deliberately tight.
//
// The release ships 536 chars SMALLER than the 5789 it replaced while ADDING a
// plan/execute contract, a tasks.md convention, a PLANNING section, and the
// trust-the-report protocol. Two deletions paid for all of it:
//
//   - The ORCHESTRATION PIPELINE section, gone with the pipeline itself.
//   - CONTEXT AND EDIT DISCIPLINE's editing half (~640 chars), which moved to
//     the EXECUTOR. Under the plan/execute split it was teaching edit_file's
//     oldString contract, multi_edit batching, and the write_file rule to the
//     one model forbidden from editing — every session, in cached bytes —
//     while the agent that actually edits received none of it. A coherence
//     audit found it; the move fixes the audit finding and the budget at once.
//
// What the additions buy, so a future reader knows what NOT to trim first:
// trust-the-report (rule 5) resolves a contradiction that made the model
// re-read every file an agent had just verified, and PLANNING step 3's
// executor-ready standard is the whole payoff of the split — an
// under-specified item costs far more in executor turns than the sentence
// costs in prefix.
// v1.1.0 hardening epoch (+73, actual 5326): rule 5 gained the BLOCKED:
// QUESTION branch. The executor cannot reach the user, so before this an
// executor blocked on a decision only the user could make had two bad options —
// guess, or return BLOCKED with no route to an answer. The clause costs 73
// cached chars and buys the missing edge in the main/executor protocol: the
// executor names the decision, the planner asks with ask_user, and re-dispatches
// with the answer. Still 463 chars below the 5789 this release replaced.
const promptBaselineChars = 5340

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
