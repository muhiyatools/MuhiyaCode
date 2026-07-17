package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestExecutionPlanArtifactContainsRequiredSections(t *testing.T) {
	settings := engineSettings()
	knowledge := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	knowledge.AddPhaseReport("explore", "research", "research-scope", "Engine flow", "internal/orchestrator/engine.go Run", "Verified internal/orchestrator/engine.go Run owns request routing and tool dispatch; preserve the direct path.")
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), Knowledge: knowledge})
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: `[serial] internal/orchestrator/engine.go function Run [F1] Acceptance: exact routing gate passes`, Status: contract.PlanPending}}, Note: "Verification:\n- go test ./internal/orchestrator\nRisks:\n- preserve direct behavior"}
	markdown := engine.executionPlanMarkdown(engine.plan)
	for _, required := range []string{"## Research Findings", "[F1]", "## Implementation Steps", "Acceptance:", "## Verification", "go test ./internal/orchestrator", "## Risks"} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("artifact missing %q:\n%s", required, markdown)
		}
	}
	if err := engine.pipelinePlanContentBar(context.Background()); err != nil {
		t.Fatalf("valid execution plan blocked: %v", err)
	}
}

// TestPlanContentBarAcceptsGreenfieldPlan locks in the live fix: a NEW project
// (no banked research findings) must NOT be blocked for missing [F#] citations
// or research grounding — there is no existing code to cite. Steps with a
// target + Verify check and a Verification/Risks note are execution-grade for a
// from-scratch build, so the bar accepts on the first call (no strike loop).
func TestPlanContentBarAcceptsGreenfieldPlan(t *testing.T) {
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull}
	// A realistic greenfield plan: new files, targets + Verify, no [F#] (nothing
	// to cite), and a note with Verification/Risks.
	engine.plan = contract.Plan{
		Steps: []contract.PlanStep{
			{Title: "cmd/taskflow/main.go: create the CLI entry with add/list/done/rm subcommands — Verify: go build ./... succeeds", Status: contract.PlanPending},
			{Title: "internal/store/store.go: implement JSON-file task storage with atomic writes — Verify: go test ./internal/store passes", Status: contract.PlanPending},
		},
		Note: "Verification:\n- go build ./...\n- go test ./...\nRisks:\n- file corruption on concurrent writes (mitigated by atomic write)",
	}
	if err := engine.pipelinePlanContentBar(context.Background()); err != nil {
		t.Fatalf("greenfield plan (no findings, no [F#]) must be accepted on the first call, got: %v", err)
	}
}

// TestPipelinePlanContentBarNotesGapsButAcceptsFirstTry locks in the live fix:
// the content bar is NON-BLOCKING. A vague plan is accepted on the FIRST call
// (exit_plan_mode must never loop), but what fell short is recorded as a
// plan-phase degradation so the relaxed quality bar is visible in the session.
func TestPipelinePlanContentBarNotesGapsButAcceptsFirstTry(t *testing.T) {
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "vague step", Status: contract.PlanPending}}}
	if err := engine.pipelinePlanContentBar(context.Background()); err != nil {
		t.Fatalf("content bar must accept on the first call (no exit_plan_mode loop), got: %v", err)
	}
	if !engine.Lifecycle().IsDegraded(contract.LifecyclePlanning) {
		t.Fatal("a gappy plan must record a plan-phase degradation even though it is accepted")
	}
}

func TestPipelineSupersedesWithoutSilentOverwrite(t *testing.T) {
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecyclePending}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "prior unrelated step", Status: contract.PlanPending}}, Note: "Verification:\n- old check\nRisks:\n- old risk"}
	engine.beginPipeline(context.Background(), PlanNeedVerdict{NeedsPlan: true, Depth: PipelineDepthFull, Reason: "new task"})
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "new step", Status: contract.PlanPending}}}
	markdown := engine.executionPlanMarkdown(engine.plan)
	if !strings.Contains(markdown, "## Superseded Prior Plan") || !strings.Contains(markdown, "prior unrelated step") {
		t.Fatalf("prior plan was silently lost:\n%s", markdown)
	}
}
