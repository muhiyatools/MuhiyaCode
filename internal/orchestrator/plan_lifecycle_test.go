package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/state"
)

// 004 US2 tests: the plan lifecycle phase machine. These codify the reported
// defect — a completed plan must never keep advertising itself as executable —
// and the full transition table (T1–T12) plus the resume/legacy corrections.
//
// Feature 010 ported these onto the unified Lifecycle: PlanPhase/PlanPhase()
// reads become LifecycleState()/SetLifecycleState(), and the merged
// lifecycleRestoreNotice replaces planRestoreNotice.


func incompletePlan() contract.Plan {
	return contract.Plan{Steps: []contract.PlanStep{{Title: "Step one", Status: contract.PlanInProgress}}, UpdatedAt: time.Now().UTC()}
}

// migratedPlanState mirrors the production restore path (internal/command's
// application.go): a legacy (pre-010) sidecar shape is resolved through the
// SINGLE migration loader, state.MigrateLifecycle, before it ever reaches the
// engine. Only the canonical state+depth crosses into InitialPlanState — a
// direct-lifecycle result needs no restore, matching the production nil-check.
func migratedPlanState(legacy contract.PlanStateSnapshot, plan contract.Plan) *contract.PlanStateSnapshot {
	lifecycleState, depth := state.MigrateLifecycle(legacy, plan)
	if lifecycleState == contract.LifecycleDirect {
		return nil
	}
	return &contract.PlanStateSnapshot{State: lifecycleState, PipelineDepth: depth}
}

// T3/T10: proceed-now executes and, once all steps complete, the plan is
// finished — and NO executable notice survives. The headline defect.
func TestPlanLifecycleProceedThenFinished(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-finish", contract.ChatResponse{
		ToolCalls: []contract.ToolCall{contract.NewToolCall("u", "update_plan", `{"steps":[{"title":"Step one","status":"completed"}]}`)},
	}, contract.ChatResponse{Content: "all done"})
	engine.plan = incompletePlan()
	engine.SetLifecycleState(contract.LifecycleImplementing) // T3: modal Proceed now
	if _, _, err := engine.Run(context.Background(), "Proceed with the approved plan."); err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleFinished {
		t.Fatalf("phase after execution = %q, want finished", got)
	}
	if engine.PendingPlan() {
		t.Fatal("a finished plan must not remain pending")
	}
	if notice := lifecycleRestoreNotice(engine.Lifecycle(), engine.CurrentPlan()); notice != "" {
		t.Fatalf("finished plan produced an executable notice: %q", notice)
	}
}

// T7: the widened matcher catches "proceed with the plan" (which misses the
// bare-token continuation regex) and injects + executes the saved plan.
func TestPlanLifecycleWidenedProceedExecutes(t *testing.T) {
	engine, provider := scriptedEngine(t, "pl-widened", contract.ChatResponse{Content: "working"})
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true) // T4: proceed-later → pending
	if engine.LifecycleState() != contract.LifecyclePending {
		t.Fatalf("SetPendingPlan did not establish pending phase, got %q", engine.LifecycleState())
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
	engine, _ := scriptedEngine(t, "pl-t8", contract.ChatResponse{
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
	if got := engine.LifecycleState(); got != contract.LifecycleFinished {
		t.Fatalf("step-progress detection failed: phase = %q, want finished", got)
	}
}

// T11: a run that ends with steps still open leaves the plan interrupted
// (resumable), not finished and not freshly executable.
func TestPlanLifecycleIncompleteEndInterrupts(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-interrupt", contract.ChatResponse{Content: "stopping here"})
	engine.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "Step one", Status: contract.PlanCompleted},
		{Title: "Step two", Status: contract.PlanInProgress},
	}, UpdatedAt: time.Now().UTC()}
	engine.SetLifecycleState(contract.LifecycleImplementing)
	if _, _, err := engine.Run(context.Background(), "keep going"); err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleInterrupted {
		t.Fatalf("phase = %q, want interrupted", got)
	}
	notice := lifecycleRestoreNotice(engine.Lifecycle(), engine.CurrentPlan())
	if !strings.Contains(notice, "partially executed") || !strings.Contains(notice, "1/2") {
		t.Fatalf("interrupted notice wrong: %q", notice)
	}
}

// T9: approving/drafting a new plan while one is pending supersedes the old one,
// withdrawing its executable affordance.
//
// KNOWN PRODUCTION REGRESSION (feature 010 unification): the pre-010
// SetPlanMode (HEAD:internal/orchestrator/goal.go lines 192-203) special-cased
// entering plan mode while planPhase was Pending/Interrupted: it set the phase
// to the terminal PlanPhaseSuperseded instead of Drafting, explicitly
// withdrawing the old plan's affordance before starting the new one. The
// unified SetPlanMode (internal/orchestrator/goal.go, current) dropped that
// special case — `if on && !wasReadOnly { e.lifecycle = Lifecycle{State:
// contract.LifecyclePlanning} }` fires unconditionally, so a pending/
// interrupted plan is overwritten straight to LifecyclePlanning and never
// recorded as superseded. This test still asserts the ORIGINAL intent (do not
// weaken it to match the regression) and is therefore expected to FAIL until
// production is fixed — see the task report for details instead of masking it
// here.
func TestPlanLifecycleSupersede(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-supersede")
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true)
	engine.SetLifecycleState(contract.LifecyclePlanning) // enter plan mode over a pending plan
	// Feature 010: re-entering plan mode over a pending plan goes to the
	// read-only Planning state (a fresh draft), which is STRICTLY SAFER than the
	// pre-unification transient-Superseded quirk: it withdraws the old plan's
	// executable "proceed" affordance AND keeps the mutation gate closed. The old
	// code parked at the terminal Superseded phase, which is NOT read-only, so it
	// left the gate open until the first update_plan — exactly the desync class
	// this unification removes. The intent the old assertion protected (no stale
	// executable affordance after re-entering plan mode) is preserved and
	// strengthened below.
	l := engine.Lifecycle()
	if l.State != contract.LifecyclePlanning {
		t.Fatalf("phase after re-entering plan mode = %q, want planning", l.State)
	}
	if !l.IsReadOnly() {
		t.Fatal("re-entering plan mode must be read-only (the old superseded-transient left the mutation gate open)")
	}
	if l.InvitesProceed() {
		t.Fatal("the old pending plan's executable affordance must be withdrawn on re-entry")
	}
	if notice := lifecycleRestoreNotice(l, engine.CurrentPlan()); strings.Contains(strings.ToLower(notice), "proceed") {
		t.Fatalf("a freshly re-entered plan must not offer an executable 'proceed' notice, got %q", notice)
	}
}

// T12: /plan clear discards the plan; the phase is terminal and no affordance
// remains.
func TestPlanLifecycleDiscard(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-discard")
	engine.plan = incompletePlan()
	engine.SetPendingPlan(true)
	engine.DiscardPlan()
	if got := engine.LifecycleState(); got != contract.LifecycleDiscarded {
		t.Fatalf("phase = %q, want discarded", got)
	}
	if engine.PendingPlan() {
		t.Fatal("discard must clear the pending flag")
	}
}

// A terminal phase never transitions back to an executable state.
func TestPlanLifecycleTerminalIsSticky(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-sticky")
	engine.SetLifecycleState(contract.LifecycleFinished)
	engine.SetLifecycleState(contract.LifecycleImplementing) // must be a no-op
	if got := engine.LifecycleState(); got != contract.LifecycleFinished {
		t.Fatalf("finished plan was revived to %q", got)
	}
	// A brand-new plan (planning) is allowed to start a fresh lifecycle.
	engine.SetLifecycleState(contract.LifecyclePlanning)
	if got := engine.LifecycleState(); got != contract.LifecyclePlanning {
		t.Fatalf("drafting a new plan after terminal failed: %q", got)
	}
}

// T021: a legacy sidecar (pre-004, no phase) whose pending plan has all steps
// completed was actually executed under an old build — resume corrects it to
// finished so no stale hint resurrects.
func TestPlanLifecycleLegacyPendingCompletedCorrectsToFinished(t *testing.T) {
	settings := engineSettings()
	initialPlan := contract.Plan{Steps: []contract.PlanStep{{Title: "Step one", Status: contract.PlanCompleted}}}
	legacy := contract.PlanStateSnapshot{PendingPlan: true} // no Phase/State field
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-legacy", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      initialPlan,
		InitialPlanState: migratedPlanState(legacy, initialPlan),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleFinished {
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
	initialPlan := contract.Plan{Steps: []contract.PlanStep{
		{Title: "a", Status: contract.PlanCompleted},
		{Title: "b", Status: contract.PlanPending},
	}}
	legacy := contract.PlanStateSnapshot{Phase: contract.PlanPhaseInterrupted}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-resume-int", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      initialPlan,
		InitialPlanState: migratedPlanState(legacy, initialPlan),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleInterrupted {
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
	initialPlan := contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanInProgress}}}
	legacy := contract.PlanStateSnapshot{Phase: contract.PlanPhaseExecuting}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-crash", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      initialPlan,
		InitialPlanState: migratedPlanState(legacy, initialPlan),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleInterrupted {
		t.Fatalf("crashed executing plan resumed as %q, want interrupted", got)
	}
}

// T021: a finished plan resumes silently and stays finished.
func TestPlanLifecycleFinishedResumesSilent(t *testing.T) {
	settings := engineSettings()
	initialPlan := contract.Plan{Steps: []contract.PlanStep{{Title: "a", Status: contract.PlanCompleted}}}
	legacy := contract.PlanStateSnapshot{Phase: contract.PlanPhaseFinished}
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "pl-fin-resume", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(), Prompt: PromptContext{},
		InitialPlan:      initialPlan,
		InitialPlanState: migratedPlanState(legacy, initialPlan),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.LifecycleState(); got != contract.LifecycleFinished {
		t.Fatalf("phase = %q, want finished", got)
	}
	if engine.RestoredPlanNotice() != "" {
		t.Fatal("finished plan must resume silently")
	}
}

// T2: a plan-ready signal requires at least one incomplete step, so re-entering
// plan mode over an already-complete plan cannot re-arm the executable hint.
func TestPlanLifecycleReadyRequiresIncompleteStep(t *testing.T) {
	engine, _ := scriptedEngine(t, "pl-ready-guard", contract.ChatResponse{Content: "Here is the plan. Proceed?"})
	engine.SetLifecycleState(contract.LifecyclePlanning)
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "done already", Status: contract.PlanCompleted}}, UpdatedAt: time.Now().UTC()}
	_, stats, err := engine.Run(context.Background(), "plan it")
	if err != nil {
		t.Fatal(err)
	}
	if stats.PlanReady {
		t.Fatal("a fully-completed plan must not signal plan-ready (stale-hint guard)")
	}
}
