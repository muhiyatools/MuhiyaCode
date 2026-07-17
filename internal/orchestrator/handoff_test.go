package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestHandoffContractCarriesAllFieldsPerRole(t *testing.T) {
	for _, agent := range []string{"explore", "plan", "general", "review"} {
		handoff := handoffFor(subagentInput{Agent: agent, Task: "internal/orchestrator/engine.go Run"}, "banked context")
		rendered := handoff.Render()
		for _, field := range []string{"Role:", "Scope:", "Context:", "Deliverable:", "OutputFormat:"} {
			if !strings.Contains(rendered, field) {
				t.Fatalf("%s handoff missing %s: %s", agent, field, rendered)
			}
		}
		if agent == "general" && !strings.Contains(rendered, "Changes made; Validation performed; Problems; Remaining concerns") {
			t.Fatalf("implementation output contract missing: %s", rendered)
		}
	}
}

func TestKnowledgeBriefingIsPhaseTaggedAndScopeFiltered(t *testing.T) {
	k := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	k.AddPhaseReport("explore", "research", "research-scope", "Engine", "internal/orchestrator/engine.go Run", "engine routing fact")
	k.AddPhaseReport("explore", "research", "research-scope", "Gateway", "internal/gateway/provider.go replay", "gateway replay fact")
	brief := k.BriefingForScope("change internal/orchestrator/engine.go Run", 1500)
	if !strings.Contains(brief, "research/research-scope") || !strings.Contains(brief, "engine routing fact") || strings.Contains(brief, "gateway replay fact") {
		t.Fatalf("scope-filtered briefing wrong: %q", brief)
	}
}

func TestPipelineDivisionHonorsDependencyMarkers(t *testing.T) {
	plan := contract.Plan{Steps: []contract.PlanStep{
		{Title: "[parallel] a.go [F1] Acceptance: a"},
		{Title: "[parallel] b.go [F2] Acceptance: b"},
		{Title: "[serial] c.go depends on a.go [F3] Acceptance: c"},
	}}
	groups := groupPipelineSteps(plan, 3)
	if len(groups) != 3 || !groups[0].Independent || !groups[1].Independent || groups[2].Independent {
		t.Fatalf("dependency grouping wrong: %+v", groups)
	}
}

func TestValidationRecoveryRescopesExactlyOnce(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: ""},
		{Content: "Verified findings: re-scoped review checked the exact changed file and command. Checks performed: focused test. VERDICT: PASS. Remaining concerns: none."},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortMedium // phase allowance 2: first attempt + one recovery
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "recovery", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "done", Status: contract.PlanCompleted}}}
	engine.taskAgentCap = 2
	engine.taskPhaseAgentRuns = make(map[contract.LifecycleState]int)
	if _, err := engine.runPipelineValidation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.LifecycleState() != contract.LifecycleFinished || engine.taskPhaseAgentRuns[contract.LifecycleValidating] != 2 {
		t.Fatalf("bounded recovery failed: state=%s runs=%d", engine.LifecycleState(), engine.taskPhaseAgentRuns[contract.LifecycleValidating])
	}
}

func TestValidationRequiresExplicitPassVerdict(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Verified finding: an acceptance check failed. VERDICT: FAIL. Corrective action: repair it."},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortLow
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "validation-fail", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
	engine.taskAgentCap = 1
	engine.taskPhaseAgentRuns = make(map[contract.LifecycleState]int)
	if _, err := engine.runPipelineValidation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if engine.LifecycleState() != contract.LifecycleValidating || engine.Lifecycle().Validated {
		t.Fatalf("failed review unlocked completion: %+v", engine.Lifecycle())
	}
}
