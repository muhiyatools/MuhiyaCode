package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// P3: in plan mode, run_subagent with kind=general is rejected at the gate.
func TestPlanModeBlocksGeneralSubagent(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"general","task":"do work"}`)}},
		{Content: "ok"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p3", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "run_subagent"}),
		Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	if _, _, err := engine.Run(context.Background(), "plan it"); err != nil {
		t.Fatal(err)
	}
	// Inspect the recorded response shape: an executed-but-blocked outcome.
	for _, req := range provider.requests {
		_ = req
	}
	// Walk the history and look for a tool result that failed (the redacted
	// read-only shell/escape block path returns a Failed:true outcome).
	sawBlocked := false
	for _, msg := range engine.history.All() {
		if msg.Role == contract.RoleTool && strings.Contains(msg.Content, "plan mode") {
			sawBlocked = true
		}
	}
	if !sawBlocked {
		t.Fatalf("expected a blocked subagent-general result, history=%+v", engine.history.All())
	}
}

// P4: read-only shell calls pass in plan mode.
func TestPlanModeAllowsReadOnlyShell(t *testing.T) {
	shell := &recordingTool{name: "run_shell"}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r1", "run_shell", `{"command":"git log --oneline"}`)}},
		{Content: "ok"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p4a", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(shell),
		Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	if _, _, err := engine.Run(context.Background(), "plan it"); err != nil {
		t.Fatal(err)
	}
	if shell.calls != 1 {
		t.Fatalf("read-only shell was blocked in plan mode: %d calls", shell.calls)
	}
}

// P4: a mutating shell in plan mode is blocked and counted.
func TestPlanModeBlocksMutatingShellAndEscalates(t *testing.T) {
	shell := &recordingTool{name: "run_shell"}
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r1", "run_shell", `{"command":"rm -rf /tmp/x"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r2", "edit_file", `{"path":"a","old_string":"x","new_string":"y"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r3", "write_file", `{"path":"b","content":"z"}`)}},
		{Content: "ok"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p4b", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(shell, &recordingTool{name: "edit_file"}, &recordingTool{name: "write_file"}),
		Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	if _, _, err := engine.Run(context.Background(), "plan it"); err != nil {
		t.Fatal(err)
	}
	if shell.calls != 0 {
		t.Fatalf("mutating shell was not blocked: %d calls", shell.calls)
	}
	if engine.taskCounters.planViolations < 3 {
		t.Fatalf("expected at least 3 violations, got %d", engine.taskCounters.planViolations)
	}
	for _, msg := range engine.history.All() {
		if msg.Role == contract.RoleTool && strings.Contains(msg.Content, "loop guard") {
			return // success
		}
	}
	t.Fatalf("loop-guard escalation not present, history=%+v", engine.history.All())
}

// P2: exit_plan_mode triggers task finalization and clears plan mode.
func TestExitPlanModeFinalizesPlanTask(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ship it"}`)}},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(),
		Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	if !engine.PlanMode() {
		t.Fatal("plan mode did not engage")
	}
	answer, _, err := engine.Run(context.Background(), "plan it")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "Plan ready") {
		t.Fatalf("expected 'Plan ready' final answer, got: %q", answer)
	}
	if engine.PlanMode() {
		t.Fatal("plan mode was not switched off after exit_plan_mode")
	}
}

// H3: subagentKind returns "general" when the JSON is malformed.
func TestSubagentKindFailsClosedOnBadArgs(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{`{"agent":""}`, "general"},
		{`{`, "general"},
		{`{"agent":"explore"}`, "explore"},
	}
	for _, c := range cases {
		got := subagentKind(contract.NewToolCall("c", "run_subagent", c.args))
		if got != c.want {
			t.Errorf("subagentKind(%q)=%q want %q", c.args, got, c.want)
		}
	}
}

// TestPendingPlanContinuationInjectsPlan (P2 step 4) verifies that a bare
// "proceed" against a saved pending plan injects the plan content into the
// task brief (cache-safe — it rides the user-message tail) and clears the flag
// so the plan executes exactly once.
func TestPendingPlanContinuationInjectsPlan(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "done"}}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2-inject", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt: PromptContext{},
	})
	engine.SetPendingPlan(true)
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "Step one", Status: contract.PlanCompleted}}, UpdatedAt: time.Now().UTC()}
	if _, _, err := engine.Run(context.Background(), "proceed"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(provider.requests))
	}
	last := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if !strings.Contains(last, "[executing saved plan]") {
		t.Fatalf("saved plan not injected into brief: %q", last)
	}
	if !strings.Contains(last, "Step one") {
		t.Fatalf("plan step text missing from brief: %q", last)
	}
	if engine.PendingPlan() {
		t.Fatal("pending plan flag was not cleared after injection")
	}
}

// TestPlanStateRestoredOnRestart (P2 step 6) verifies that the plan-mode and
// pending-plan flags survive a restart via InitialPlanState, with a one-shot
// TUI notice for each.
func TestPlanStateRestoredOnRestart(t *testing.T) {
	settings := engineSettings()
	// planMode on → restored + notice.
	planModeOn := contract.PlanStateSnapshot{PlanMode: true}
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2-restore", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		InitialPlanState: &planModeOn,
	})
	if !engine.PlanMode() {
		t.Fatal("plan mode not restored from sidecar")
	}
	notice := engine.RestoredPlanNotice()
	if !strings.Contains(notice, "Plan mode restored") {
		t.Fatalf("plan-mode restore notice wrong: %q", notice)
	}
	// pendingPlan on → restored + notice; bare "proceed" would inject it.
	pending := contract.PlanStateSnapshot{PendingPlan: true}
	engine2, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2-restore-pending", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		InitialPlanState: &pending,
	})
	if !engine2.PendingPlan() {
		t.Fatal("pending plan not restored from sidecar")
	}
	notice = engine2.RestoredPlanNotice()
	if !strings.Contains(notice, "Saved plan pending") || !strings.Contains(notice, "proceed") {
		t.Fatalf("pending-plan restore notice wrong: %q", notice)
	}
	// The notice is one-shot: a second read is empty.
	if engine2.RestoredPlanNotice() != "" {
		t.Fatal("restore notice should be one-shot")
	}
}

// TestExitPlanModeSetsPlanReadyAndPendingPlan (P2 steps 2–4) verifies that in a
// non-interactive run (no TaskComplete callback) exit_plan_mode sets
// stats.PlanReady, clears plan mode, and sets pendingPlan so a later "proceed"
// executes the saved plan.
func TestExitPlanModeSetsPlanReadyAndPendingPlan(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"ship it"}`)}},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2-ready", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	_, stats, err := engine.Run(context.Background(), "plan it")
	if err != nil {
		t.Fatal(err)
	}
	if !stats.PlanReady {
		t.Fatal("exit_plan_mode did not set stats.PlanReady")
	}
	if engine.PlanMode() {
		t.Fatal("plan mode should be off after exit_plan_mode in non-interactive run")
	}
	if !engine.PendingPlan() {
		t.Fatal("pending plan should be set after exit_plan_mode in non-interactive run")
	}
}

// TestExitPlanModeInBatchPairsAllToolResults (T019 / REV B2b) verifies that when
// exit_plan_mode is not the last call in a batch, every announced tool_call still
// receives a tool result before the plan-ready finalize — so the persisted
// history is a well-formed assistant/tool sequence (no provider 400 next turn).
func TestExitPlanModeInBatchPairsAllToolResults(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("a", "read_file", `{"path":"x"}`),
			contract.NewToolCall("b", "exit_plan_mode", `{"summary":"done"}`),
			contract.NewToolCall("c", "write_file", `{"path":"x","content":"y"}`),
		}},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "b2b", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}, &recordingTool{name: "write_file"}), Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	_, stats, err := engine.Run(context.Background(), "plan it")
	if err != nil {
		t.Fatal(err)
	}
	if !stats.PlanReady {
		t.Fatal("exit_plan_mode in a batch did not set PlanReady")
	}
	got := map[string]bool{}
	for _, m := range engine.history.All() {
		if m.Role == contract.RoleTool {
			got[m.ToolCallID] = true
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if !got[id] {
			t.Fatalf("tool_call %q has no matching tool result — malformed pairing: %+v", id, got)
		}
	}
}

// TestExitPlanModeOffModeIsHarmlessNoOp (T019 / REV B2a) verifies a stray
// exit_plan_mode call while plan mode is OFF neither ends the task nor sets a
// phantom pending plan.
func TestExitPlanModeOffModeIsHarmlessNoOp(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("x", "exit_plan_mode", `{"summary":"oops"}`)}},
		{Content: "carrying on with the task"},
	}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "b2a", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{},
	})
	// plan mode intentionally OFF
	answer, stats, err := engine.Run(context.Background(), "do the task")
	if err != nil {
		t.Fatal(err)
	}
	if stats.PlanReady {
		t.Fatal("stray exit_plan_mode off-mode must not set PlanReady")
	}
	if engine.PendingPlan() {
		t.Fatal("stray exit_plan_mode off-mode must not set a phantom pending plan")
	}
	if !strings.Contains(answer, "carrying on") {
		t.Fatalf("task should have continued after the no-op exit, got %q", answer)
	}
	sawNoOp := false
	for _, m := range engine.history.All() {
		if m.Role == contract.RoleTool && strings.Contains(m.Content, "not in plan mode") {
			sawNoOp = true
		}
	}
	if !sawNoOp {
		t.Fatal("expected the 'not in plan mode' no-op result in history")
	}
}

// TestFreeTextPlanFinishSignalsPlanReady (P2 belt-and-suspenders, step 5)
// verifies that a plan-mode task that ends with free text (no exit_plan_mode)
// and a plan in place is also marked plan-ready.
func TestFreeTextPlanFinishSignalsPlanReady(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "Here is the plan. May I proceed?"}}}
	settings := engineSettings()
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "p2-freetext", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
	})
	engine.SetPlanMode(true)
	// 004 US2: a freshly proposed plan has at least one incomplete step (planning
	// does not complete steps — execution does). This also satisfies the new
	// plan-ready rule that requires an incomplete step, closing the stale-hint bug.
	engine.plan = contract.Plan{Steps: []contract.PlanStep{{Title: "Investigate the bug", Status: contract.PlanInProgress}}, UpdatedAt: time.Now().UTC()}
	_, stats, err := engine.Run(context.Background(), "plan the bug fix")
	if err != nil {
		t.Fatal(err)
	}
	if !stats.PlanReady {
		t.Fatal("free-text plan finish did not set stats.PlanReady (belt-and-suspenders)")
	}
}
