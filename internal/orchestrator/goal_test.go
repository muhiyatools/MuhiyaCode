package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestGoalStateNoRaceUnderConcurrentAccess (T004 / REV B1) exercises the single
// modeMu that now guards ALL goal/plan mode state. A scripted Run executes in
// one goroutine while a second goroutine drives the user-side setters
// (SetGoal/ClearGoal/SetPlanMode) and a third drives the engine-loop read
// helpers (goalBlock/scanGoalMarker/advanceGoal) — the exact contention that
// the prior two-mutex split (modeMu setters vs goalMu reads) left unsynchronized
// on e.goal. This test is meaningful under `CGO_ENABLED=1 go test -race`; it must
// also pass (no panic, no deadlock) without the race detector.
func TestGoalStateNoRaceUnderConcurrentAccess(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "working [goal:continue]"},
		{Content: "working still [goal:continue]"},
		{Content: "done and verified [goal:complete]"},
	}}
	engine := newGoalEngine(t, provider)
	engine.SetGoal("initial objective")

	var wg sync.WaitGroup

	// User-side mutations concurrent with the running task (/goal, /plan).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			engine.SetGoal("objective")
			engine.GoalSnapshot()
			engine.ClearGoal()
			engine.PlanMode()
			engine.LastGoalResult()
			engine.SetPendingPlan(i%3 == 0)
			engine.PendingPlan()
		}
	}()

	// Engine-loop read helpers concurrent with the setters.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			engine.goalBlock()
			engine.scanGoalMarker("progress [goal:continue]")
			engine.advanceGoal("progress [goal:continue]", i%2 == 0)
			engine.ResetGoalTaskCounter()
		}
	}()

	// The actual task loop, concurrent with both goroutines above.
	if _, _, err := engine.Run(context.Background(), "start"); err != nil {
		t.Fatalf("run under concurrent goal mutation: %v", err)
	}
	wg.Wait()
}

func newGoalEngine(t *testing.T, provider contract.Provider) *Engine {
	t.Helper()
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{}})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

// A goal keeps the agent working until it emits [goal:complete].
func TestGoalAutoContinuesUntilComplete(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Step one done. [goal:continue]"},
		{Content: "All finished and verified. [goal:complete]"},
	}}
	engine := newGoalEngine(t, provider)
	engine.SetGoal("make the tests pass")
	answer, _, err := engine.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("goal should have auto-continued once, saw %d requests", len(provider.requests))
	}
	if !strings.Contains(answer, "finished") {
		t.Fatalf("unexpected final answer: %q", answer)
	}
	// G2: a completed goal is removed from active state — GoalSnapshot must
	// report no active goal, and the tombstone (LastGoalResult) carries the
	// completed status for /goal status to display.
	if goal, active := engine.GoalSnapshot(); active {
		t.Fatalf("completed goal should be inactive, still active: %+v", goal)
	}
	if last, ok := engine.LastGoalResult(); !ok || last.Status != GoalComplete {
		t.Fatalf("completed goal tombstone missing or wrong status: %+v ok=%v", last, ok)
	}
	// The goal instruction must ride on a user message (cache-safe), not the
	// system prompt.
	if !strings.Contains(provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content, "active-goal") {
		t.Fatal("goal block did not ride on the user message")
	}
	if strings.Contains(provider.requests[0].Messages[0].Content, "active-goal") {
		t.Fatal("goal leaked into the system prompt — would bust the cache")
	}
}

// TestGoalMissingMarkerNudgesThenStops (T022 / REV B4) verifies a reply with no
// marker nudges once, and a second consecutive markerless reply stops (hands
// back to the user) instead of force-continuing to the cap.
func TestGoalMissingMarkerNudgesThenStops(t *testing.T) {
	engine := newGoalEngine(t, &scriptedProvider{})
	engine.SetGoal("do it")
	p1 := engine.advanceGoal("progress, forgot the marker", false)
	if p1 == "" || !strings.Contains(p1, "exactly one goal marker") {
		t.Fatalf("first missing-marker reply should nudge: %q", p1)
	}
	p2 := engine.advanceGoal("still no marker", false)
	if p2 != "" {
		t.Fatalf("second consecutive markerless reply should stop, got %q", p2)
	}
	if _, active := engine.GoalSnapshot(); active {
		t.Fatal("goal should be inactive after two consecutive markerless replies")
	}
}

// TestGoalToolProgressResetsIdle (T022 / REV B4) verifies the idle counter is
// CONSECUTIVE, not cumulative: a productive tool turn between two no-tool
// continues resets it, so the goal is not wrongly stopped for "no forward
// motion".
func TestGoalToolProgressResetsIdle(t *testing.T) {
	engine := newGoalEngine(t, &scriptedProvider{})
	engine.SetGoal("do it")
	if p := engine.advanceGoal("continue [goal:continue]", false); p == "" { // IdleTurns=1
		t.Fatal("first continue should keep going")
	}
	engine.markGoalToolProgress()                              // a tool turn resets idle
	p := engine.advanceGoal("continue [goal:continue]", false) // IdleTurns=1 again, not 2
	if p == "" {
		t.Fatal("idle counter should have reset after tool progress; goal must keep going")
	}
	if _, active := engine.GoalSnapshot(); !active {
		t.Fatal("goal should still be active")
	}
}

// TestGoalCompletesOnToolCallTurn (T021 / REV B3) verifies a [goal:complete]
// marker emitted in the SAME turn as a tool call is detected and clears the
// goal — the tool-call branch used to be a path where completion was dropped.
func TestGoalCompletesOnToolCallTurn(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "Finishing up. [goal:complete]", ToolCalls: []contract.ToolCall{contract.NewToolCall("c", "read_file", `{"path":"x"}`)}},
		{Content: "final wrap-up"},
	}}
	engine := newGoalEngine(t, provider)
	engine.SetGoal("do the thing")
	answer, _, err := engine.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if goal, active := engine.GoalSnapshot(); active {
		t.Fatalf("goal should be complete after a marker on a tool-call turn: %+v", goal)
	}
	if strings.Contains(answer, "[goal:") {
		t.Fatalf("goal marker leaked into the displayed answer: %q", answer)
	}
}

// TestGoalMarkerStrippedFromFinalAnswer (T021 / REV B3) verifies goal control
// markers are removed from the DISPLAYED final answer while the goal still
// terminates. Markers are the engine's parser signal, not user-facing text.
func TestGoalMarkerStrippedFromFinalAnswer(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "All done and verified. [goal:complete]"},
	}}
	engine := newGoalEngine(t, provider)
	engine.SetGoal("finish it")
	answer, _, err := engine.Run(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer, "[goal:") {
		t.Fatalf("goal marker must be stripped from the displayed answer: %q", answer)
	}
	if !strings.Contains(answer, "All done and verified") {
		t.Fatalf("answer content lost during marker stripping: %q", answer)
	}
	if _, active := engine.GoalSnapshot(); active {
		t.Fatal("goal should be inactive after completion")
	}
}

// TestGoalSidecarPersistsActiveGoal (G4) verifies that SetGoal writes the
// active-goal sidecar and that a fresh engine built with that snapshot as
// InitialGoal restores the active goal plus a one-shot TUI notice.
func TestGoalSidecarPersistsActiveGoal(t *testing.T) {
	settings := engineSettings()
	var persisted contract.GoalSnapshot
	wrote := false
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "g4a", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		Persistence: Persistence{
			WriteGoal: func(_ context.Context, snapshot contract.GoalSnapshot) error {
				persisted = snapshot
				wrote = true
				return nil
			},
			ClearGoal: func(_ context.Context) error { return nil },
		},
	})
	engine.SetGoal("ship the release notes")
	if !wrote {
		t.Fatal("SetGoal did not persist the sidecar")
	}
	if persisted.Status != string(GoalActive) || persisted.Text != "ship the release notes" {
		t.Fatalf("sidecar snapshot wrong: %+v", persisted)
	}
	// Simulate a restart: build a fresh engine with the persisted snapshot as
	// InitialGoal. Only an active goal resurrects.
	restored, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "g4a", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		InitialGoal: &persisted,
	})
	goal, active := restored.GoalSnapshot()
	if !active || goal.Text != "ship the release notes" || goal.Status != GoalActive {
		t.Fatalf("active goal not restored from sidecar: %+v active=%v", goal, active)
	}
	notice := restored.RestoredGoalNotice()
	if !strings.Contains(notice, "Restored active goal") || !strings.Contains(notice, "ship the release notes") || !strings.Contains(notice, "/goal clear to drop") {
		t.Fatalf("restore notice wrong: %q", notice)
	}
	// The notice is one-shot: a second read is empty.
	if restored.RestoredGoalNotice() != "" {
		t.Fatal("restore notice should be one-shot")
	}
}

// TestGoalSidecarClearedOnCompletion (G4) verifies that a completed goal drops
// the sidecar so it does not resurrect on resume, and that a completed
// snapshot passed as InitialGoal is NOT restored.
func TestGoalSidecarClearedOnCompletion(t *testing.T) {
	settings := engineSettings()
	cleared := false
	engine, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "g4b", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{
			{Content: "Step done. [goal:continue]"},
			{Content: "All finished. [goal:complete]"},
		}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		Persistence: Persistence{
			WriteGoal: func(_ context.Context, snapshot contract.GoalSnapshot) error { return nil },
			ClearGoal: func(_ context.Context) error { cleared = true; return nil },
		},
	})
	engine.SetGoal("make tests pass")
	if _, _, err := engine.Run(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("sidecar not cleared after goal completion")
	}
	// A completed snapshot must not resurrect.
	completed := contract.GoalSnapshot{Text: "make tests pass", Status: string(GoalComplete)}
	restored, _ := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "g4b", WorkspacePath: t.TempDir()},
		Provider: &scriptedProvider{responses: []contract.ChatResponse{{Content: "ok"}}},
		Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{},
		InitialGoal: &completed,
	})
	if goal, active := restored.GoalSnapshot(); active {
		t.Fatalf("completed goal resurrected: %+v", goal)
	}
	if restored.RestoredGoalNotice() != "" {
		t.Fatalf("completed goal should not produce a restore notice: %q", restored.RestoredGoalNotice())
	}
}

// Ultimate Polish P1: TestPlanModeClearsGoalSidecar was removed with SetPlanMode.
// The plan⇄goal exclusion now runs the other direction (SetGoal takes over a
// read-only planning phase — see lifecycle_test.go) plus the DG2 refusal + brief
// backstop (goal_hardening_test.go); there is no manual plan toggle to clear a goal.

// Plan mode blocks mutating tools and lets read-only tools through.
func TestPlanModeBlocksMutations(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "write_file", `{"path":"a.txt","content":"x"}`)}},
		{Content: "Here is the plan; may I proceed?"},
	}}
	settings := engineSettings()
	writer := &recordingTool{name: "write_file"}
	engine, _ := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "s", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(writer), Prompt: PromptContext{}})
	engine.SetLifecycleState(contract.LifecyclePlanning)
	if _, _, err := engine.Run(context.Background(), "add a file"); err != nil {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatalf("plan mode allowed a mutation: %d calls", writer.calls)
	}
	// The plan-mode instruction rides on the user message.
	last := provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Content
	if !strings.Contains(last, "plan-mode") {
		t.Fatalf("plan-mode marker missing from user message: %q", last)
	}
}
