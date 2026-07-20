package orchestrator

// Regression tests for the feature-009 audit fixes: the bounded plan content
// bar (the live exit_plan_mode deadlock), the stale-approve park, the
// /plan-clear pipeline reset, depth persistence, the terminal-plan revival
// guard, and research-provenance findings.

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestPlanContentBarAcceptsFirstTryAndNotesGaps locks in the fix for the live
// failure "Tool exit_plan_mode failed: … fix ALL of the following … then call
// exit_plan_mode again" that forced a second attempt: the bar is NON-BLOCKING and
// accepts on the FIRST call, recording a plan-phase degradation for what fell
// short (PL-8 — the pipeline never deadlocks or loops on formatting; the human
// approval pause is the real quality gate).
func TestPlanContentBarAcceptsFirstTryAndNotesGaps(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	// A real plan the model plausibly writes: steps exist but lack the strict
	// target/[F#]/Verify format the bar would prefer.
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "Run gofmt and go vet across the repository", Status: contract.PlanPending}}}

	if err := engine.pipelinePlanContentBar(context.Background()); err != nil {
		t.Fatalf("the bar must accept on the FIRST call (no guidance loop), got: %v", err)
	}
	if !engine.Lifecycle().IsDegraded(contract.LifecyclePlanning) {
		t.Fatal("an accepted-but-gappy plan must record a plan-phase degradation")
	}
	// The accepted plan proceeds to approval exactly like a clean one.
	if err := engine.transitionLifecycle(context.Background(), contract.LifecycleApproval); err != nil {
		t.Fatalf("accepted plan could not reach the approval pause: %v", err)
	}
}

// TestPlanContentBarNeverAcceptsEmptyPlan: the structural minimum (≥1 step)
// stays hard — an empty plan cannot be approved, so accepting it would only move
// the dead end one call later. The fix is always mechanical (call update_plan).
func TestPlanContentBarNeverAcceptsEmptyPlan(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
	for attempt := 0; attempt < 4; attempt++ {
		if err := engine.pipelinePlanContentBar(context.Background()); err == nil {
			t.Fatalf("attempt %d: an empty plan must never pass the bar", attempt+1)
		}
	}
}

// TestUnrelatedDirectTaskParksApprovePipeline locks in the stale-approve fix:
// a pipeline stranded at the approval pause is parked when an unrelated direct
// task arrives — its gate text and mutation block must not leak into the new
// task — while the saved plan stays pending and resumable.
func TestUnrelatedDirectTaskParksApprovePipeline(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "saved step", Status: contract.PlanPending}}}

	engine.parkPipelineForDirectTask(context.Background(), PlanNeedVerdict{Reason: "simple unrelated question"})

	if engine.pipelineActive() {
		t.Fatalf("pipeline not parked: %+v", engine.Lifecycle())
	}
	if !engine.PendingPlan() || engine.LifecycleState() != contract.LifecyclePending {
		t.Fatalf("saved plan must stay pending/resumable, got pending=%v state=%s", engine.PendingPlan(), engine.LifecycleState())
	}
	if block := engine.pipelineBlock(); block != "" {
		t.Fatalf("parked pipeline must not inject gate text, got %q", block)
	}
	prelude, err := engine.preparePipelinePhase(context.Background(), "unrelated", Budget{}, Profile(contract.EffortMedium))
	if err != nil || prelude != "" {
		t.Fatalf("parked pipeline must not prepare a phase prelude, got %q err=%v", prelude, err)
	}
}

// TestDiscardPlanResetsPipeline locks in the /plan clear fix: discarding the
// plan releases the pipeline gating that guarded it (previously a hard
// deadlock — pipeline stayed at approve while proceed was guarded against
// Discarded).
func TestDiscardPlanResetsPipeline(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleApproval, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}

	engine.DiscardPlan()

	if engine.LifecycleState() != contract.LifecycleDiscarded {
		t.Fatalf("plan state = %s, want discarded", engine.LifecycleState())
	}
	if engine.pipelineActive() {
		t.Fatalf("discard must release the pipeline, got %+v", engine.Lifecycle())
	}
	if block := engine.pipelineBlock(); block != "" {
		t.Fatalf("discarded plan must not keep gate text, got %q", block)
	}
}

// TestPipelineDepthPersistsAcrossRestore locks in the depth fix: a light
// pipeline resumes light (previously restore hardcoded full and spawned
// subagents the task never scoped). The legacy-sidecar depth default (no depth
// → full) now lives in the state package's migration loader (T013).
func TestPipelineDepthPersistsAcrossRestore(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleImplementing, Depth: PipelineDepthLight, ResearchCompleted: true, PlanWritten: true, Approved: true}
	snapshot := engine.planStateSnapshot()
	if snapshot.PipelineDepth != PipelineDepthLight {
		t.Fatalf("snapshot depth = %q, want light", snapshot.PipelineDepth)
	}
	restored, _ := restoreLifecycle(snapshot, contract.Plan{Steps: []contract.PlanStep{{Title: "s", Status: contract.PlanPending}}})
	if restored.Depth != PipelineDepthLight {
		t.Fatalf("restored depth = %q, want light", restored.Depth)
	}
	if !restored.Orchestrated() || restored.State != contract.LifecycleImplementing {
		t.Fatalf("light pipeline must resume orchestrated at implementing, got %+v", restored)
	}
}

// TestApprovePipelineRefusesTerminalPlan locks in the revival guard: a terminal
// (superseded/discarded) plan can never be approved into execution. With the
// unified lifecycle the stale-approve desync is unrepresentable — a terminal
// plan is simply not in the approval state — so ApprovePipeline no-ops and the
// state stays terminal.
func TestApprovePipelineRefusesTerminalPlan(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecycleSuperseded, Depth: PipelineDepthFull, ResearchCompleted: true, PlanWritten: true}

	if err := engine.ApprovePipeline(context.Background()); err != nil {
		t.Fatalf("terminal-plan approval must no-op, got error: %v", err)
	}
	if engine.LifecycleState() != contract.LifecycleSuperseded {
		t.Fatalf("terminal plan was approved/overwritten: %s", engine.LifecycleState())
	}
}

// TestExitPlanModeEscapesPlanModePhase locks in the live-stall fix: exit_plan_mode
// from an orchestrated planning phase runs the content bar and advances
// planning → approval. Before the unification the legacy planMode flag and the
// pipeline phase could desync and strand exit_plan_mode as a no-op; the single
// lifecycle state makes that stall unrepresentable, so this now verifies the
// straightforward exit path.
func TestExitPlanModeEscapesPlanModePhase(t *testing.T) {
	settings := engineSettings()
	knowledge := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	knowledge.AddPhaseReport("explore", "research", "research-scope", "finding", "scope", strings.Repeat("grounded finding ", 8))
	engine, err := NewEngine(EngineConfig{Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), Knowledge: knowledge})
	if err != nil {
		t.Fatal(err)
	}
	engine.lifecycle = Lifecycle{State: contract.LifecyclePlanning, Depth: PipelineDepthFull, ResearchCompleted: true}
	engine.plan = contract.Plan{
		Steps: []contract.PlanStep{{Title: "[serial] internal/orchestrator/engine.go function Run [F1] Verify: go test ./internal/orchestrator", Status: contract.PlanPending}},
		Note:  "Verification:\n- go test ./...\nRisks:\n- none",
	}

	_, execErr := engine.executeOne(context.Background(), contract.NewToolCall("x", "exit_plan_mode", "{}"), nil)
	if execErr != contract.ErrPlanModeExited {
		t.Fatalf("exit_plan_mode must exit the pipeline plan phase, got %v", execErr)
	}
	if engine.LifecycleState() != contract.LifecycleApproval {
		t.Fatalf("lifecycle state = %s, want awaiting-approval", engine.LifecycleState())
	}
}

// TestResearchFindingsFilterProvenance locks in the banking fix: only
// current-epoch research-phase reports count as plan-grounding evidence —
// implementation/review reports and pre-edit stale findings do not.
func TestResearchFindingsFilterProvenance(t *testing.T) {
	knowledge := NewKnowledge(KnowledgeSnapshot{Version: 1}, nil)
	knowledge.AddPhaseReport("general", "implement", "implement-step", "impl", "task-a", strings.Repeat("implementation report ", 8))
	knowledge.AddPhaseReport("review", "validate", "review", "rev", "task-b", strings.Repeat("review report ", 8))
	if got := len(knowledge.ResearchFindings(12)); got != 0 {
		t.Fatalf("non-research reports counted as findings: %d", got)
	}
	knowledge.AddPhaseReport("explore", "research", "research-scope", "res", "task-c", strings.Repeat("grounded research ", 8))
	if got := len(knowledge.ResearchFindings(12)); got != 1 {
		t.Fatalf("research finding not counted: %d", got)
	}
	// Live fix: an investigation subagent launched from the PLAN phase is
	// research evidence too — provenance is the role, not the phase timing.
	knowledge.AddPhaseReport("plan", "plan", "research-scope", "res2", "task-d", strings.Repeat("plan-phase investigation ", 8))
	if got := len(knowledge.ResearchFindings(12)); got != 2 {
		t.Fatalf("plan-phase investigation must count as research evidence: %d", got)
	}
	// A workspace edit advances the epoch; stale findings stop counting.
	knowledge.MarkWorkspaceChanged()
	if got := len(knowledge.ResearchFindings(12)); got != 0 {
		t.Fatalf("stale pre-edit findings still counted: %d", got)
	}
}

