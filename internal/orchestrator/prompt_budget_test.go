package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/workspace"
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
//
// Token-efficiency epoch (+53 net, actual 5379): PLANNING step 3 now says WHERE
// tasks.md goes — the directory the work targets, the workspace root only for
// workspace-wide work. Without it the model wrote the checklist to the root of
// whatever workspace happened to be open, beside code it had nothing to do with,
// and the reader was pinned there too (TA01). +107 for the rule, −54 by
// condensing CACHE DISCIPLINE without dropping any of its rules.
//
// Note for the next reader: this budget guards the SYSTEM PROMPT only. The
// tool-definitions JSON is larger still and rides every request identically —
// that saving is measured by the wire golden's section header, not here, so do
// not expect this number to fall when tool prose is cut.
//
// Unified-session epoch (+708, actual 6101). The subagent system was removed
// and the session does its own work again, so the prompt regained what the
// executor used to be told: the edit_file/multi_edit contract, the write_file
// permission rule, the chunked-write rule, the scope discipline, and the
// preserve-user-work clauses. It also gained a short MODEL section, because the
// session's model can now change between tasks and the model must not narrate
// or request that.
//
// The FIXED PREFIX — what actually rides every request — FELL in the same
// change: 24,393 bytes to 21,908 (-10.2%), because deleting run_subagent's
// schema (its description plus the task/role/skills property texts) cut the
// tool JSON by 3,214 bytes, more than paying for this section's growth. Judge
// prefix cost by the wire golden, not by this constant alone.
//
// Verification-discipline epoch (+166, actual 6267). Rule 5 used to say only
// "run the check that proves it works", which is silent on WHAT check is
// appropriate. A live session took that literally on a plain single-file HTML
// page: it wrote a scratch .mjs harness and ran a Node version check against a
// project that has no Node in it, then billed the user for both. The rule now
// bounds verification to the tooling the project already has and names the
// fallback for a project with none, so the model has somewhere to land instead
// of inventing a test rig.
//
// 166 cached chars against a failure mode that costs several turns and a
// scratch file every time it fires is the right side of this trade.
const promptBaselineChars = 6280

func TestSystemPromptSizeWithinBudget(t *testing.T) {
	ctx := PromptContext{
		Workspace: "/w", OS: "linux", Shell: "bash", Model: "deepseek-v4-flash",
		HasWeb:        true,
		ModelAddendum: gateway.ResolveModelProfile("deepseek-v4-flash").PromptAddendum,
	}
	if got := len(SystemPrompt(ctx)); got > promptBaselineChars {
		t.Fatalf("system prompt grew to %d chars, over the %d baseline (FR-018: do not bloat)", got, promptBaselineChars)
	}
}

func TestBalancedWirePrefixBudgetAndUnusedConfigurationStability(t *testing.T) {
	service, err := workspace.New(t.TempDir(), workspace.Options{PermissionMode: contract.PermissionAutoAccept, Trust: workspace.NewMemoryTrustStore()})
	if err != nil {
		t.Fatal(err)
	}
	settings := &contract.Settings{TokenEconomyMode: "balanced"}
	base := &Engine{settings: settings, registry: NewRegistry(service.Tools()...), skills: NewSkillCatalog(nil)}
	prompt := SystemPrompt(PromptContext{Workspace: "/w", OS: "linux", Shell: "bash", Model: "model", Lean: true})
	encoded, err := json.Marshal(base.sessionDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	wireBytes := len(prompt) + len(encoded)
	if wireBytes > 10_000 || EstimateTokens(prompt+string(encoded)) > 2_500 {
		t.Fatalf("balanced fixed prefix too large: bytes=%d tokens=%d prompt=%d tools=%d", wireBytes, EstimateTokens(prompt+string(encoded)), len(prompt), len(encoded))
	}
	withDeferred := NewRegistry(service.Tools()...)
	for i := 0; i < 100; i++ {
		name := "mcp__unused_" + strings.Repeat("x", i%5) + string(rune('a'+i%26))
		withDeferred.Add(brokerTestTool{definition: definition(name, "unused remote integration schema", map[string]any{"value": map[string]any{"type": "string"}}, nil)})
	}
	many := &Engine{settings: settings, registry: withDeferred, skills: NewSkillCatalog([]SkillListing{{Name: "one"}, {Name: "two"}})}
	manyEncoded, _ := json.Marshal(many.sessionDefinitions())
	if delta := len(manyEncoded) - len(encoded); delta > 256 || delta < -256 {
		t.Fatalf("unused definitions changed core by %d bytes", delta)
	}
	if string(encoded) == "" || string(manyEncoded) == "" {
		t.Fatal("empty serialized definitions")
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
