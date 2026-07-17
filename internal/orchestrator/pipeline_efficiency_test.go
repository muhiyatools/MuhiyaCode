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

func TestReadOnlySubagentShellGateRunsChecksButBlocksMutation(t *testing.T) {
	settings := engineSettings()
	tool := &recordingTool{name: "run_shell"}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(tool)})
	if err != nil {
		t.Fatal(err)
	}
	definitions := engine.registry.Definitions(map[string]bool{"run_shell": true})
	scope := dispatchScope{
		counters: newCallCounters(),
		readOnly: true,
		dispatch: func(ctx context.Context, call contract.ToolCall) (string, error) {
			return engine.registry.Execute(ctx, call.ToolName(), json.RawMessage(call.ArgumentsJSON()), map[string]bool{"run_shell": true})
		},
	}
	allowed := engine.gatedExecute(context.Background(), contract.NewToolCall("check", "run_shell", `{"command":"go test ./internal/orchestrator"}`), definitions, Profile(contract.EffortLow), scope)
	if allowed.Failed || tool.calls != 1 {
		t.Fatalf("read-only check was blocked: outcome=%+v calls=%d", allowed, tool.calls)
	}
	blocked := engine.gatedExecute(context.Background(), contract.NewToolCall("mutate", "run_shell", `{"command":"Remove-Item important.go"}`), definitions, Profile(contract.EffortLow), scope)
	if !blocked.Failed || !strings.Contains(blocked.Output, "read-only agent") || tool.calls != 1 {
		t.Fatalf("mutating shell escaped read-only gate: outcome=%+v calls=%d", blocked, tool.calls)
	}
	chained := engine.gatedExecute(context.Background(), contract.NewToolCall("chain", "run_shell", `{"command":"go test ./...; Remove-Item important.go"}`), definitions, Profile(contract.EffortLow), scope)
	if !chained.Failed || tool.calls != 1 {
		t.Fatalf("chained mutation escaped read-only shell gate: outcome=%+v calls=%d", chained, tool.calls)
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
