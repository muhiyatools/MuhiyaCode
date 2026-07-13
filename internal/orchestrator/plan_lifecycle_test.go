package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// 004 US2 tests: the plan lifecycle phase machine. These codify the reported
// defect — a completed plan must never keep advertising itself as executable —
// and the full transition table (T1–T12) plus the resume/legacy corrections.

func lifecycleEngine(t *testing.T, id string, resp ...contract.ChatResponse) (*Engine, *scriptedProvider) {
	t.Helper()
	if len(resp) == 0 {
		resp = []contract.ChatResponse{{Content: "ok"}}
	}
	provider := &scriptedProvider{responses: resp}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: id, WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, provider
}

func incompletePlan() contract.Plan {
	return contract.Plan{Steps: []contract.PlanStep{{Title: "Step one", Status: contract.PlanInProgress}}, UpdatedAt: time.Now().UTC()}
}

// T3/T10: proceed-now executes and, once all steps complete, the plan is
// finished — and NO executable notice survives. The headline defect.
func TestPlanLifecycleProceedThenFinished(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-finish", contract.ChatResponse{
		ToolCalls: []contract.ToolCall{contract.NewToolCall("u", "update_plan", `{"steps":[{"title":"Step one","status":"completed"}]}`)},
	}, contract.ChatResponse{Content: "all done"})
	engine.plan = incompletePlan()
	engine.SetPlanPhase(contract.PlanPhaseExecuting) // T3: modal Proceed now
	if _, _, err := engine.Run(context.Background(), "Proceed with the approved plan."); err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseFinished {
		t.Fatalf("phase after execution = %q, want finished", got)
	}
	if engine.PendingPlan() {
		t.Fatal("a finished plan must not remain pending")
	}
	if notice := planRestoreNotice(engine.PlanPhase(), engine.CurrentPlan()); notice != "" {
		t.Fatalf("finished plan produced an executable notice: %q", notice)
	}
}

// T7: the widened matcher catches "proceed with the plan" (which misses the
// bare-token continuation regex) and injects + executes the saved plan.
func TestPlanLifecycleWidenedProceedExecutes(t *testing.T) {
	engine, provider := lifecycleEngine(t, "pl-widened", contract.ChatResponse{Content: "working"})
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true) // T4: proceed-later → pending
	if engine.PlanPhase() != contract.PlanPhasePending {
		t.Fatalf("SetPendingPlan did not establish pending phase, got %q", engine.PlanPhase())
	}
	if _, _, err := engine.Run(context.Background(), "proceed with the plan"); err != nil {
		t.Fatal(err)
	}
	last := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if !strings.Contains(last, "[executing saved plan]") {
		t.Fatalf("widened proceed did not inject the saved plan: %q", last)
	}
	if engine.PendingPlan() {
		t.Fatal("pending flag not cleared after execution began")
	}
}

// T8: a go-ahead phrasing that misses BOTH matchers still flips pending→executing
// the moment the model advances a plan step. Phrasing-independent backstop.
func TestPlanLifecycleStepProgressDetectsExecution(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-t8", contract.ChatResponse{
		ToolCalls: []contract.ToolCall{contract.NewToolCall("u", "update_plan", `{"steps":[{"title":"Step one","status":"completed"}]}`)},
	}, contract.ChatResponse{Content: "done"})
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true)
	// "let's get this done" matches neither continuationRE nor planProceedRE.
	if continuationRE.MatchString("let's get this done") || planProceedRE.MatchString("let's get this done") {
		t.Fatal("test premise broken: phrase should miss both matchers")
	}
	if _, _, err := engine.Run(context.Background(), "let's get this done"); err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseFinished {
		t.Fatalf("step-progress detection failed: phase = %q, want finished", got)
	}
}

// T11: a run that ends with steps still open leaves the plan interrupted
// (resumable), not finished and not freshly executable.
func TestPlanLifecycleIncompleteEndInterrupts(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-interrupt", contract.ChatResponse{Content: "stopping here"})
	engine.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "Step one", Status: contract.PlanCompleted},
		{Title: "Step two", Status: contract.PlanInProgress},
	}, UpdatedAt: time.Now().UTC()}
	engine.SetPlanPhase(contract.PlanPhaseExecuting)
	if _, _, err := engine.Run(context.Background(), "keep going"); err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseInterrupted {
		t.Fatalf("phase = %q, want interrupted", got)
	}
	notice := planRestoreNotice(engine.PlanPhase(), engine.CurrentPlan())
	if !strings.Contains(notice, "partially executed") || !strings.Contains(notice, "1/2") {
		t.Fatalf("interrupted notice wrong: %q", notice)
	}
}

// T9: approving/drafting a new plan while one is pending supersedes the old one,
// withdrawing its executable affordance.
func TestPlanLifecycleSupersede(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-supersede")
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true)
	engine.SetPlanMode(true) // enter plan mode over a pending plan
	if got := engine.PlanPhase(); got != contract.PlanPhaseSuperseded {
		t.Fatalf("phase after re-entering plan mode = %q, want superseded", got)
	}
	if planRestoreNotice(engine.PlanPhase(), engine.CurrentPlan()) != "" {
		t.Fatal("a superseded plan must not offer an executable notice")
	}
}

// T12: /plan clear discards the plan; the phase is terminal and no affordance
// remains.
func TestPlanLifecycleDiscard(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-discard")
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true)
	engine.DiscardPlan()
	if got := engine.PlanPhase(); got != contract.PlanPhaseDiscarded {
		t.Fatalf("phase = %q, want discarded", got)
	}
	if engine.PendingPlan() {
		t.Fatal("discard must clear the pending flag")
	}
}

// A terminal phase never transitions back to an executable state.
func TestPlanLifecycleTerminalIsSticky(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-sticky")
	engine.SetPlanPhase(contract.PlanPhaseFinished)
	engine.SetPlanPhase(contract.PlanPhaseExecuting) // must be a no-op
	if got := engine.PlanPhase(); got != contract.PlanPhaseFinished {
		t.Fatalf("finished plan was revived to %q", got)
	}
	// A brand-new plan (drafting) is allowed to start a fresh lifecycle.
	engine.SetPlanPhase(contract.PlanPhaseDrafting)
	if got := engine.PlanPhase(); got != contract.PlanPhaseDrafting {
		t.Fatalf("drafting a new plan after terminal failed: %q", got)
	}
}

// T021: a legacy sidecar (pre-004, no phase) whose pending plan has all steps
// completed was actually executed under an old build — resume corrects it to
// finished so no stale hint resurrects.
func TestPlanLifecycleLegacyPendingCompletedCorrectsToFinished(t *testing.T) {
	settings := engineSettings()
	legacy := contract.PlanStateSnapshot{PendingPlan: true} // no Phase field
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-legacy", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      contract.Plan{Steps: []contract.PlanStep{{Title: "Step one", Status: contract.PlanCompleted}}},
		InitialPlanState: &legacy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseFinished {
		t.Fatalf("legacy completed-pending plan resumed as %q, want finished", got)
	}
	if engine.PendingPlan() {
		t.Fatal("legacy correction must clear the pending flag")
	}
	if engine.RestoredPlanNotice() != "" {
		t.Fatal("a finished plan must resume silently")
	}
}

// T021: a plan persisted as interrupted resumes with the resumable-partial
// notice, not silence and not a fresh executable hint.
func TestPlanLifecycleInterruptedResumeNotice(t *testing.T) {
	settings := engineSettings()
	snap := contract.PlanStateSnapshot{Phase: contract.PlanPhaseInterrupted}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-resume-int", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan: contract.Plan{Steps: []contract.PlanStep{
			{Title: "a", Status: contract.PlanCompleted},
			{Title: "b", Status: contract.PlanPending},
		}},
		InitialPlanState: &snap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseInterrupted {
		t.Fatalf("phase = %q, want interrupted", got)
	}
	notice := engine.RestoredPlanNotice()
	if !strings.Contains(notice, "partially executed") || !strings.Contains(notice, "1/2") {
		t.Fatalf("interrupted resume notice wrong: %q", notice)
	}
}

// T021: a plan caught mid-execution by a crash (phase executing persisted) with
// steps still open resumes interrupted, never as freshly executable.
func TestPlanLifecycleExecutingCrashResumesInterrupted(t *testing.T) {
	settings := engineSettings()
	snap := contract.PlanStateSnapshot{Phase: contract.PlanPhaseExecuting}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-crash", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanInProgress}}},
		InitialPlanState: &snap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseInterrupted {
		t.Fatalf("crashed executing plan resumed as %q, want interrupted", got)
	}
}

// T021: a finished plan resumes silently and stays finished.
func TestPlanLifecycleFinishedResumesSilent(t *testing.T) {
	settings := engineSettings()
	snap := contract.PlanStateSnapshot{Phase: contract.PlanPhaseFinished}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-fin-resume", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanCompleted}}},
		InitialPlanState: &snap,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.PlanPhase(); got != contract.PlanPhaseFinished {
		t.Fatalf("phase = %q, want finished", got)
	}
	if engine.RestoredPlanNotice() != "" {
		t.Fatal("finished plan must resume silently")
	}
}

// T2: a plan-ready signal requires at least one incomplete step, so re-entering
// plan mode over an already-complete plan cannot re-arm the executable hint.
func TestPlanLifecycleReadyRequiresIncompleteStep(t *testing.T) {
	engine, _ := lifecycleEngine(t, "pl-ready-guard", contract.ChatResponse{Content: "Here is the plan. Proceed?"})
	engine.SetPlanMode(true)
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "done already", Status: contract.PlanCompleted}}, UpdatedAt: time.Now().UTC()}
	_, stats, err := engine.Run(context.Background(), "plan it")
	if err != nil {
		t.Fatal(err)
	}
	if stats.PlanReady {
		t.Fatal("a fully-completed plan must not signal plan-ready (stale-hint guard)")
	}
}
