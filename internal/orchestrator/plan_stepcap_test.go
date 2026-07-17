package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func planStepsJSON(n int) json.RawMessage {
	steps := make([]string, n)
	for i := range steps {
		steps[i] = fmt.Sprintf(`{"title":"internal/x%d.go: do step %d — Verify: go test","status":"pending"}`, i, i)
	}
	return json.RawMessage(`{"steps":[` + strings.Join(steps, ",") + `],"note":"Verification:\n- go test\nRisks:\n- none"}`)
}

// TestUpdatePlanStepCapIsBounded pins the D5 fix: the step cap guides but never
// walls. ≤12 is clean, 13–24 accepts with a soft note, >24 rejects ONCE then
// accepts (steps never destroyed, no loop), and an empty plan is the one hard stop.
func TestUpdatePlanStepCapIsBounded(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "cap", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 12 steps: clean accept, no telemetry.
	if _, err := engine.updatePlan(ctx, planStepsJSON(12)); err != nil {
		t.Fatalf("12 steps must be accepted cleanly: %v", err)
	}
	if len(engine.HarnessEvents()) != 0 {
		t.Fatalf("a within-ideal plan must emit no friction: %+v", engine.HarnessEvents())
	}

	// 13 steps: accepted, with a soft note.
	if _, err := engine.updatePlan(ctx, planStepsJSON(13)); err != nil {
		t.Fatalf("13 steps must be accepted (bounded, not walled): %v", err)
	}
	if !hasHarnessEvent(engine.HarnessEvents(), contract.HarnessGate, "plan-steps-soft-exceeded") {
		t.Fatalf("13 steps should record a soft over-granularity note: %+v", engine.HarnessEvents())
	}

	// 25 steps: rejected the first time...
	if _, err := engine.updatePlan(ctx, planStepsJSON(25)); err == nil {
		t.Fatal("a >24-step plan must be rejected once with the mechanical fix")
	}
	// ...then accepted on the second attempt (never loops, never destroys steps).
	if _, err := engine.updatePlan(ctx, planStepsJSON(25)); err != nil {
		t.Fatalf("the second oversized attempt must be accepted: %v", err)
	}
	if !hasHarnessEvent(engine.HarnessEvents(), contract.HarnessRecovery, "plan-steps-waived") {
		t.Fatalf("the accepted-after-guidance case must be telemetered: %+v", engine.HarnessEvents())
	}
	if len(engine.CurrentPlan().Steps) != 25 {
		t.Fatalf("all 25 steps must be preserved, got %d", len(engine.CurrentPlan().Steps))
	}

	// Empty plan: the one hard stop.
	if _, err := engine.updatePlan(ctx, json.RawMessage(`{"steps":[]}`)); err == nil {
		t.Fatal("an empty plan must be rejected (a plan needs steps)")
	}
}

// TestUpdatePlanResultUsesTodoWording pins the A4 language split: update_plan's
// result speaks in to-dos ("To-dos updated: N/M done."), never the old
// "Plan updated:" wording that conflated the plan (proposal) with execution.
func TestUpdatePlanResultUsesTodoWording(t *testing.T) {
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "todo-wording", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.updatePlan(context.Background(), planStepsJSON(2))
	if err != nil {
		t.Fatalf("a 2-step plan must be accepted: %v", err)
	}
	if !strings.HasPrefix(out, "To-dos updated:") {
		t.Fatalf("result must use to-do wording, got %q", out)
	}
	if strings.Contains(out, "Plan updated") {
		t.Fatalf("old 'Plan updated' wording leaked: %q", out)
	}
}
