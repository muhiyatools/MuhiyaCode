package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPipelineEndToEndHeadlessApprovalAndConfiguredRouting(t *testing.T) {
	// Feature 013 flow: the MODEL launches every subagent. Run 1 (planning):
	// the model chooses one explore dispatch, then plans and exits. Run 2
	// (proceed): the model delegates the implementation step to one general
	// subagent, marks it complete, launches the instructed review, and answers.
	validStep := `[serial] internal/orchestrator/engine.go function Run [F1] Acceptance: pipeline reaches validated completion`
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("scout", "run_subagent", `{"agent":"explore","task":"map internal/orchestrator/engine.go task routing for the overhaul"}`)}},
		{Content: "Findings: internal/orchestrator/engine.go function Run owns task routing; exact reference verified. Risks include preserving the direct fast path and plan approval."},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("plan", "update_plan", `{"steps":[{"title":"`+validStep+`","status":"pending"}],"note":"Verification:\n- go test ./internal/orchestrator\nRisks:\n- preserve the direct fast path"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("exit", "exit_plan_mode", `{"summary":"research-backed plan ready"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("impl", "run_subagent", `{"agent":"general","task":"Execute this approved plan step and report: `+validStep+`"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("edit", "edit_file", `{"path":"internal/orchestrator/engine.go","content":"focused change"}`)}},
		{Content: "Changes made: focused engine update. Validation performed: focused package check. Problems: none. Remaining concerns: final independent review."},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("done", "update_plan", `{"steps":[{"title":"`+validStep+`","status":"completed"}]}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("rev", "run_subagent", `{"agent":"review","task":"Validate the completed approved plan against the changed workspace"}`)}},
		{Content: "Verified findings: implementation matches the approved step and the direct path remains isolated. Checks performed: plan command inspected. VERDICT: PASS. Remaining concerns: none."},
		{Content: "Pipeline complete: research grounded the plan, implementation executed the approved step, and validation passed."},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortLow // one deterministic run per phase
	settings.PermissionMode = contract.PermissionAutoAccept
	settings.Provider.ActiveModelID = "main-model"
	settings.Provider.SubagentModelID = "worker-model"
	settings.Provider.Models = []contract.Model{{ID: "main-model", Name: "Main"}, {ID: "worker-model", Name: "Worker"}}
	var (
		agentMu sync.Mutex
		agents  []contract.AgentEvent
		plans   []string
	)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "pipeline-e2e", WorkspacePath: seededWorkspace(t)},
		Provider: provider,
		Registry: NewRegistry(&recordingTool{name: "edit_file"}),
		Prompt:   PromptContext{Model: "Main", SubagentModel: "Worker", HasSubagents: true},
		Callbacks: contract.Callbacks{Agent: func(event contract.AgentEvent) {
			agentMu.Lock()
			agents = append(agents, event)
			agentMu.Unlock()
		}},
		Persistence: Persistence{WritePlan: func(_ context.Context, markdown string) error {
			plans = append(plans, markdown)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	complexPrompt := "Overhaul the entire orchestration architecture across every module with an end-to-end migration and audit. " + strings.Repeat("Coordinate repository implementation, persistence, validation, and regression coverage. ", 20)
	answer, stats, err := engine.Run(context.Background(), complexPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.PlanReady || engine.LifecycleState() != contract.LifecyclePending {
		t.Fatalf("headless plan did not pause: answer=%q stats=%+v state=%s", answer, stats, engine.LifecycleState())
	}
	if strings.Contains(strings.ToLower(answer), "implemented") {
		t.Fatalf("headless approval pause claimed implementation: %q", answer)
	}
	if len(plans) == 0 || !strings.Contains(plans[len(plans)-1], "## Research Findings") || !strings.Contains(plans[len(plans)-1], "## Verification") {
		t.Fatalf("execution-grade plan was not persisted: %v", plans)
	}

	answer, executionStats, err := engine.Run(context.Background(), "proceed")
	if err != nil {
		t.Fatal(err)
	}
	if executionStats.AgentRuns != 2 {
		t.Fatalf("execution run must delegate implement + review (2 agent runs): stats=%+v", executionStats)
	}
	if engine.LifecycleState() != contract.LifecycleFinished {
		t.Fatalf("pipeline did not finish: state=%s answer=%q", engine.LifecycleState(), answer)
	}
	if !strings.Contains(answer, "Pipeline complete") {
		t.Fatalf("unexpected final answer: %q", answer)
	}
	if !strings.Contains(answer, "Phase contributions: research") {
		t.Fatalf("final answer omitted deterministic phase attribution: %q", answer)
	}
	for _, request := range provider.requests {
		if !strings.HasSuffix(request.SessionID, ":main") {
			continue
		}
		for _, message := range request.Messages {
			if strings.Contains(message.Content, "[executing saved plan]") {
				t.Fatalf("successful full pipeline leaked the legacy execution payload into main: %q", message.Content)
			}
		}
	}

	explore, implement, validate := 0, 0, 0
	agentMu.Lock()
	for _, event := range agents {
		if event.Kind != "start" {
			continue
		}
		if event.Model != "Worker" {
			t.Fatalf("subagent routed to %q, want configured Worker", event.Model)
		}
		switch event.Agent {
		case "explore":
			explore++
		case "general":
			implement++
		case "review":
			validate++
		}
	}
	agentMu.Unlock()
	if explore != 1 || implement != 1 || validate != 1 {
		t.Fatalf("model-launched agent counts = explore %d implement %d review %d", explore, implement, validate)
	}
	for _, request := range provider.requests {
		if strings.Contains(request.SessionID, ":sub:") && request.ModelID != "worker-model" {
			t.Fatalf("subagent request ModelID = %q", request.ModelID)
		}
	}
}

func TestPipelinePlanGateReportsOnlyMissingComponents(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.plan = contract.Plan{
		Steps: []contract.PlanStep{{Title: "[serial] [F1] Verify: go test ./... succeeds", Status: contract.PlanPending}},
		Note:  "Verification:\n- go test ./...\nRisks:\n- none",
	}
	if engine.knowledge != nil {
		engine.knowledge.AddPhaseReport("explore", "research", "research-scope", "finding", "scope", strings.Repeat("grounded finding ", 8))
	}
	// The content bar is non-blocking, but planQualityGaps must still pinpoint
	// ONLY the genuinely-missing component: this step lacks a file/function
	// target, yet it HAS a [F1] citation and a Verify check, so neither of those
	// may be blamed. (The gaps feed the recorded plan-phase degradation, not a
	// rejection.)
	joined := strings.Join(engine.planQualityGaps(engine.plan), " | ")
	if !strings.Contains(joined, "missing an exact file/function target") {
		t.Fatalf("gap detection missed the absent target: %q", joined)
	}
	if strings.Contains(joined, "missing an exact file/function target, a [F#]") || strings.Contains(joined, "observable Acceptance") {
		t.Fatalf("gap detection blamed components that were present: %q", joined)
	}
}

func TestDirectTaskResetsCompletedPipelineRuntimePhase(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleFinished, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true, Validated: true}
	engine.recordDirectVerdict(context.Background(), PlanNeedVerdict{Reason: "fresh small task"})
	if engine.LifecycleState() != contract.LifecycleDirect || engine.pipelineActive() {
		t.Fatalf("completed phase leaked into fresh task: %+v", engine.Lifecycle())
	}
}

func TestPipelineBlocksMutationBeforeApproval(t *testing.T) {
	settings := engineSettings()
	tool := &recordingTool{name: "write_file"}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "pipeline-block", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry(tool)})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.taskCounters = newCallCounters()
	definitions := engine.sessionDefinitions()
	outcome := engine.gatedExecute(context.Background(), contract.NewToolCall("w", "write_file", `{"path":"x.txt","content":"x"}`), definitions, Profile(contract.EffortLow), engine.mainScope(definitions))
	if !outcome.Failed || !strings.Contains(outcome.Output, "await explicit approval") || tool.calls != 0 {
		t.Fatalf("pre-approval mutation gate failed: outcome=%+v calls=%d", outcome, tool.calls)
	}
}

func TestPipelineResumeFromEveryPersistedPhase(t *testing.T) {
	// Every orchestrated phase restores its state and a one-shot notice. Migration
	// from the legacy pipeline_phase sidecar is covered in the state package's
	// lifecycle-migration test (T013); here the sidecar already carries the
	// canonical state+depth.
	for _, state := range []contract.LifecycleState{
		contract.LifecycleResearch,
		contract.LifecyclePlanning,
		contract.LifecycleApproval,
		contract.LifecycleImplementing,
		contract.LifecycleValidating,
	} {
		t.Run(string(state), func(t *testing.T) {
			settings := engineSettings()
			snapshot := contract.PlanStateSnapshot{State: state, PipelineDepth: PipelineDepthFull}
			engine, err := NewEngine(EngineConfig{
				Settings: &settings, Session: contract.Session{ID: "resume-" + string(state), WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry(),
				InitialPlan: contract.Plan{Steps: []contract.PlanStep{{Title: "step", Status: contract.PlanPending}}}, InitialPlanState: &snapshot,
			})
			if err != nil {
				t.Fatal(err)
			}
			if engine.LifecycleState() != state {
				t.Fatalf("restored state = %s", engine.LifecycleState())
			}
			if engine.RestoredPlanNotice() == "" {
				t.Fatalf("state %s produced no restore notice", state)
			}
		})
	}
}

func TestPipelinePrematureFinishIsInterrupted(t *testing.T) {
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	engine.lifecycle = Lifecycle{State: contract.LifecycleValidating, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true, Approved: true, StepsComplete: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "done", Status: contract.PlanCompleted}}}
	engine.stampPlanCompletionPhase()
	if engine.LifecycleState() != contract.LifecycleInterrupted {
		t.Fatalf("premature finish state = %s", engine.LifecycleState())
	}
}
