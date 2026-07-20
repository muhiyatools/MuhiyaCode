package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestBudgetExhaustedSubagentReportsViaWrapUp locks in the no-hard-failure
// directive: a subagent that burns its whole turn budget on tool calls is not
// failed — it gets a wrap-up prompt telling it to stop calling tools, and the
// text it returns on the extra turn is accepted as a "done" report.
// v1.1.0: "budget exhausted" now means STALLED. The turn cap is a ladder rung —
// a run still opening new files earns another rung — so the way to reach the
// wrap-up is to stop producing new evidence, which is what turns 3-4 below do by
// re-reading files the run already has.
func TestBudgetExhaustedSubagentReportsViaWrapUp(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},   // turn 1: new file
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "read_file", `{"path":"b.go"}`)}},   // turn 2: new file — earns a rung
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c3", "read_file", `{"path":"a.go"}`)}},   // turn 3: nothing new
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c4", "read_file", `{"path":"b.go"}`)}},   // turn 4: nothing new — stalled, wrap up
		{Content: "Partial findings: a.go wires the loader; b.go holds defaults. Not fully verified."}, // wrap-up turn
	}}
	settings := engineSettings() // EffortMedium: AgentTurnScale 1, so MaxTurns is used as-is
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "wrapup", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	spec := engine.subagentSpecs()["explore"]
	spec.MaxTurns = 2
	result := engine.executeSubagent(context.Background(), "r1", subagentInput{Agent: "explore", Title: "wrap-up", Task: "find the config loader"}, spec)
	if result.Status != "done" {
		t.Fatalf("stalled subagent must succeed via wrap-up: status=%q report=%q", result.Status, result.Report)
	}
	if !strings.Contains(result.Report, "Partial findings") {
		t.Fatalf("wrap-up text was not accepted as the report: %q", result.Report)
	}
	if result.Turns != 5 || result.ToolCalls != 4 {
		t.Fatalf("expected 2 rungs of work + 1 wrap-up turn (4 tool calls), got turns=%d toolCalls=%d", result.Turns, result.ToolCalls)
	}
	// The wrap-up nudge must have been appended as a user message before the
	// final request.
	last := provider.requests[len(provider.requests)-1].Messages
	sawNudge := false
	for _, msg := range last {
		if msg.Role == contract.RoleUser && strings.Contains(msg.Content, "Turn budget reached: stop calling tools") {
			sawNudge = true
		}
	}
	if !sawNudge {
		t.Fatalf("wrap-up prompt missing from the final request: %+v", last)
	}
}

// TestWrapUpStillEmptyReturnsGuidedPartialNotForbiddenString (T035, INV-3): a
// subagent that returns nothing even after the wrap-up prompt no longer surfaces
// the forbidden "bounded turn limit" string. It returns a GUIDED PARTIAL (still
// status "failed" so the parent's recovery path absorbs the unfinished work) and
// records a recovery telemetry event. This test REPLACED an earlier one that
// pinned the forbidden wording — that wording is now prohibited by INV-3.
func TestWrapUpStillEmptyReturnsGuidedPartialNotForbiddenString(t *testing.T) {
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "read_file", `{"path":"a.go"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "read_file", `{"path":"b.go"}`)}}, // earns one rung
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c3", "read_file", `{"path":"a.go"}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c4", "read_file", `{"path":"b.go"}`)}}, // stalled → wrap up
		{Content: "   "}, // wrap-up turn: still nothing
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "wrapup-empty", WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	spec := engine.subagentSpecs()["explore"]
	spec.MaxTurns = 2
	result := engine.executeSubagent(context.Background(), "r2", subagentInput{Agent: "explore", Title: "wrap-up empty", Task: "find the config loader"}, spec)
	if result.Status != "failed" {
		t.Fatalf("empty wrap-up should keep the recovery-triggering failed status: status=%q report=%q", result.Status, result.Report)
	}
	if strings.Contains(strings.ToLower(result.Report), "bounded turn limit") {
		t.Fatalf("INV-3 violated: report surfaced the forbidden 'bounded turn limit' string: %q", result.Report)
	}
	// The synthetic partial must carry a STATUS line: without it the caller
	// reads "no verification shown" and has no named next action. It says
	// BLOCKED + dispatch-a-continuation, because under the role split the caller
	// cannot finish the work itself.
	if !strings.Contains(result.Report, "partial progress") {
		t.Fatalf("expected a guided partial report, got: %q", result.Report)
	}
	if !strings.Contains(result.Report, "STATUS: BLOCKED") || !strings.Contains(result.Report, "dispatch a continuation") {
		t.Fatalf("partial report must state a status and an actionable next step: %q", result.Report)
	}
	if strings.Contains(result.Report, "continue it directly") {
		t.Fatalf("the partial report tells the caller to do work the role split forbids: %q", result.Report)
	}
	if !hasHarnessEvent(engine.HarnessEvents(), contract.HarnessRecovery, "subagent-turn-budget") {
		t.Fatalf("turn-budget exhaustion was not telemetered: %+v", engine.HarnessEvents())
	}
}
