package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestUpdatePlanPreservesNoteWhenOmitted(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	const note = "Verification:\n- go test ./...\nRisks:\n- preserve compatibility"
	engine.plan = contract.Plan{Note: note}
	_, err = engine.updatePlan(context.Background(), json.RawMessage(`{"steps":[{"title":"internal/orchestrator/engine.go function Run [F1] Acceptance: tests pass","status":"pending"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.CurrentPlan().Note; got != note {
		t.Fatalf("omitted note erased plan contract: %q", got)
	}
}


func TestPipelineValidationBriefOmitsResearchTranscript(t *testing.T) {
	plan := contract.Plan{
		Steps: []contract.PlanStep{{Title: "internal/orchestrator/pipeline.go function Run [F1] Acceptance: package tests pass", Status: contract.PlanCompleted}},
		Note:  "Verification:\n- go test ./internal/orchestrator\nRisks:\n- preserve direct path",
	}
	brief := pipelineValidationBrief(plan)
	if !strings.Contains(brief, "go test ./internal/orchestrator") || !strings.Contains(brief, plan.Steps[0].Title) {
		t.Fatalf("validation brief lost executable evidence: %q", brief)
	}
	if strings.Contains(brief, "Research Findings") || strings.Contains(brief, "Risks:") {
		t.Fatalf("validation brief retransmitted unrelated plan sections: %q", brief)
	}
}
