package orchestrator

import (
	"strings"
	"testing"
)

func TestHandoffContractCarriesAllFieldsPerRole(t *testing.T) {
	for _, agent := range []string{"explore", "general", "review"} {
		handoff := handoffFor(subagentInput{Agent: agent, Task: "internal/orchestrator/engine.go Run"}, "banked context")
		rendered := handoff.Render()
		for _, field := range []string{"Role:", "Scope:", "Context:", "Deliverable:", "OutputFormat:"} {
			if !strings.Contains(rendered, field) {
				t.Fatalf("%s handoff missing %s: %s", agent, field, rendered)
			}
		}
		// v1.1.0: the implementation format ends with a machine-readable STATUS
		// line — that line is what lets the caller trust the report instead of
		// re-verifying the work itself.
		if agent == "general" {
			for _, want := range []string{"Changes made with file:line", "Verification:", "STATUS: COMPLETE", "STATUS: NEEDS-VERIFY", "STATUS: BLOCKED"} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("implementation output contract missing %q: %s", want, rendered)
				}
			}
		}
	}
}

// TestKnowledgeBriefingIsRoleTaggedAndScopeFiltered: the briefing labels each
// banked fact with its role and passes only facts overlapping the requested
// scope. The phase dimension is gone with the pipeline — AddPhaseReport's phase
// argument is always empty now, so the label degrades to "role: title".
func TestKnowledgeBriefingIsRoleTaggedAndScopeFiltered(t *testing.T) {
	k := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	k.AddPhaseReport("explore", "", "research-scope", "Engine", "internal/orchestrator/engine.go Run", "engine routing fact")
	k.AddPhaseReport("explore", "", "research-scope", "Gateway", "internal/gateway/provider.go replay", "gateway replay fact")
	brief := k.BriefingForScope("change internal/orchestrator/engine.go Run", 1500)
	if !strings.Contains(brief, "research-scope: Engine") || !strings.Contains(brief, "engine routing fact") || strings.Contains(brief, "gateway replay fact") {
		t.Fatalf("scope-filtered briefing wrong: %q", brief)
	}
}
