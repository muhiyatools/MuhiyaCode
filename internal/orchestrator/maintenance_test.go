package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestPressureInputSwitchesFromEstimateToProviderTokens(t *testing.T) {
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	history.Append(contract.Message{Role: contract.RoleUser, Content: strings.Repeat("x", 400)})
	bootstrap := history.PressureInput(0, false)
	if !bootstrap.Estimated || bootstrap.Tokens <= 0 {
		t.Fatalf("bootstrap pressure=%+v", bootstrap)
	}
	reported := history.PressureInput(123, true)
	if reported.Estimated || reported.Tokens != 123 {
		t.Fatalf("reported pressure=%+v", reported)
	}
}

func TestCombinedMaintenanceUsesOneRewriteAndAntiThrashLatch(t *testing.T) {
	promptTokens := 100_000
	settings := engineSettings()
	history := NewHistory(HistorySnapshot{Version: 1}, nil)
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Provider: &scriptedProvider{}, Registry: NewRegistry(), History: history,
		InitialUsageRecords: []contract.UsageRecord{{Seq: 1, PromptTokens: &promptTokens}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addCompletedToolPayload(history, "c1")
	history.MarkTaskStart()
	before := history.RewriteVersion()
	first, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || !first.Changed || history.RewriteVersion() != before+1 {
		t.Fatalf("first=%+v version=%d err=%v", first, history.RewriteVersion(), err)
	}
	addCompletedToolPayload(history, "c2")
	history.MarkTaskStart()
	second, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || !second.Changed {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if report := engine.ContextReport(); !report.MaintenanceLatched || report.PressureEstimated {
		t.Fatalf("maintenance diagnostics=%+v", report)
	}
	addCompletedToolPayload(history, "c3")
	history.MarkTaskStart()
	version := history.RewriteVersion()
	third, err := engine.runMaintenanceBoundary(context.Background(), Profile(contract.EffortMedium))
	if err != nil || third.Changed || history.RewriteVersion() != version {
		t.Fatalf("latched pass changed history: result=%+v version=%d err=%v", third, history.RewriteVersion(), err)
	}
}

func TestUserCompactRecordsInvalidation(t *testing.T) {
	settings := engineSettings()
	provider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "summary", Usage: contract.Usage{PromptTokens: 10, PromptTokensAvailable: true}}}}
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Compact(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := engine.InvalidationEvents()
	if len(events) != 1 || events[0].Cause != contract.InvalidationUserCompact || events[0].RequestSeq != 2 {
		t.Fatalf("events=%+v", events)
	}
}

func addCompletedToolPayload(history *History, id string) {
	history.Append(contract.Message{Role: contract.RoleUser, Content: "task"})
	history.Append(contract.Message{Role: contract.RoleAssistant, ToolCalls: []contract.ToolCall{contract.NewToolCall(id, "read_file", `{"path":"file"}`)}})
	history.Append(contract.Message{Role: contract.RoleTool, ToolCallID: id, Content: "file\n" + strings.Repeat("payload", 300)})
}
