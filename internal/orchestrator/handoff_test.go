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

// validationEngine builds an engine parked at full-depth validating whose plan
// touches an auth path, so decideValidationReview never lands on the skip tier
// and the model-launched review instruction always renders (feature 013).
func validationEngine(t *testing.T, provider *scriptedProvider) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "validation", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "internal/auth/handler.go: add token expiry check (done)", Status: contract.PlanCompleted}}}
	engine.taskAgentCap = 2
	engine.taskPhaseAgentRuns = make(map[contract.LifecycleState]int)
	return engine
}

// TestValidationInstructsModelLaunchedReview pins feature 013's validation
// flow: the harness composes the review task but launches nothing — the
// prelude instructs ONE run_subagent review, and the provider sees no request.
func TestValidationInstructsModelLaunchedReview(t *testing.T) {
	provider := &scriptedProvider{}
	engine := validationEngine(t, provider)
	out, err := engine.runPipelineValidation(context.Background(), ClassLarge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[validation]") || !strings.Contains(out, `agent="review"`) {
		t.Fatalf("validating prelude must instruct a model-launched review, got %q", out)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("the harness must not dispatch the review itself; %d requests fired", len(provider.requests))
	}
	if engine.Lifecycle().Validated {
		t.Fatal("validation must stay blocked until the model's review passes")
	}
}

// TestModelLaunchedReviewConfirmsValidation: the model's own review dispatch
// returning VERDICT: PASS confirms validation and finishes the pipeline via
// the observeOrchestratedSubagent hook.
func TestModelLaunchedReviewConfirmsValidation(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Verified findings: none. Checks performed: go test ./... green, diff reviewed. VERDICT: PASS. Remaining concerns: none."},
	}}
	engine := validationEngine(t, provider)
	if _, err := engine.runSubagentInput(context.Background(), subagentInput{Agent: "review", Task: "validate the completed plan against the changed workspace"}); err != nil {
		t.Fatal(err)
	}
	if engine.LifecycleState() != contract.LifecycleFinished || !engine.Lifecycle().Validated {
		t.Fatalf("passing model-launched review must finish the pipeline: %+v", engine.Lifecycle())
	}
}

// TestValidationRequiresExplicitPassVerdict: a FAIL verdict from the model's
// review never unlocks completion.
func TestValidationRequiresExplicitPassVerdict(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Verified finding: an acceptance check failed. VERDICT: FAIL. Corrective action: repair it."},
	}}
	engine := validationEngine(t, provider)
	if _, err := engine.runSubagentInput(context.Background(), subagentInput{Agent: "review", Task: "validate the completed plan"}); err != nil {
		t.Fatal(err)
	}
	if engine.LifecycleState() != contract.LifecycleValidating || engine.Lifecycle().Validated {
		t.Fatalf("failed review unlocked completion: %+v", engine.Lifecycle())
	}
}
