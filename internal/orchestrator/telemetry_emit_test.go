package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// erroringProvider (request_assembly_test.go) fails every Chat call — reused here
// for the provider-error path.

func hasHarnessEvent(events []contract.HarnessEvent, class contract.HarnessEventClass, codePrefix string) bool {
	for _, e := range events {
		if e.Class == class && strings.HasPrefix(e.Code, codePrefix) {
			return true
		}
	}
	return false
}

// TestHarnessEmissionsCoverClasses proves the T011 instrumentation actually fires:
// the recovery, tool, gate, and provider classes each land in the ring through a
// real path. (The ui class is emitted by T052's panic-recovery, per the plan's
// "reserved for Phase 2" note.)
func TestHarnessEmissionsCoverClasses(t *testing.T) {
	// recovery: a recorded pipeline degradation.
	settings := engineSettings()
	rec, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "rec", WorkspacePath: t.TempDir()}, Provider: &scriptedProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	_ = rec.RecordPipelineDegradation(context.Background(), contract.LifecycleResearch, "greenfield skip")
	if !hasHarnessEvent(rec.HarnessEvents(), contract.HarnessRecovery, "degradation") {
		t.Fatalf("degradation did not emit a recovery event: %+v", rec.HarnessEvents())
	}

	// tool + gate: a failing tool called twice identically. The first call fails
	// (tool-failure), the second identical call is short-circuited by H2
	// (repeat-failed-call gate).
	tool := &failingTool{name: "do_thing"}
	prov := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c1", "do_thing", `{}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("c2", "do_thing", `{}`)}},
		{Content: "Reporting the blocker; cannot proceed."},
	}}
	tg, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "tg", WorkspacePath: t.TempDir()}, Provider: prov, Registry: NewRegistry(tool)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tg.Run(context.Background(), "Fix the failing widget in the dashboard module."); err != nil {
		t.Fatalf("run: %v", err)
	}
	events := tg.HarnessEvents()
	if !hasHarnessEvent(events, contract.HarnessTool, "tool-failure") {
		t.Fatalf("failing tool did not emit a tool event: %+v", events)
	}
	if !hasHarnessEvent(events, contract.HarnessGate, "repeat-failed-call") {
		t.Fatalf("repeated identical failing call did not emit a gate event: %+v", events)
	}

	// provider: the gateway errors after retries.
	pe, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "pe", WorkspacePath: t.TempDir()}, Provider: erroringProvider{}, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = pe.Run(context.Background(), "hello there")
	if !hasHarnessEvent(pe.HarnessEvents(), contract.HarnessProvider, "chat-error") {
		t.Fatalf("provider error did not emit a provider event: %+v", pe.HarnessEvents())
	}
}

// TestRepeatLimiterTelemetry (T033 audit) confirms the repeat limiter records a
// gate event when the model makes the same call more than three times. (Duplicate
// -read is deliberately NOT telemetered as friction — it is the cache working; see
// the gate-policy compliance table in gatepolicy.go.)
func TestRepeatLimiterTelemetry(t *testing.T) {
	settings := engineSettings()
	tool := &recordingTool{name: "ping"}
	prov := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("1", "ping", `{}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("2", "ping", `{}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("3", "ping", `{}`)}},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("4", "ping", `{}`)}},
		{Content: "done"},
	}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{ID: "rl", WorkspacePath: t.TempDir()}, Provider: prov, Registry: NewRegistry(tool)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "call ping over and over"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !hasHarnessEvent(engine.HarnessEvents(), contract.HarnessGate, "repeat-limiter") {
		t.Fatalf("the repeat limiter did not telemeter: %+v", engine.HarnessEvents())
	}
}
