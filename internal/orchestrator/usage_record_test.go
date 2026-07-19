package orchestrator

import (
	"context"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestEnginePersistsUsageSequenceAttributionAndResumeAggregate(t *testing.T) {
	read1, miss1, read2, miss2 := 0, 100, 90, 10
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "first", Usage: reportedUsage(100, 5, &read1, &miss1)},
		{Content: "second", Usage: reportedUsage(100, 5, &read2, &miss2)},
	}}
	settings := engineSettings()
	var persisted []contract.UsageRecord
	engine, err := NewEngine(EngineConfig{
		Settings: &settings,
		Session:  contract.Session{ID: "usage", WorkspacePath: t.TempDir()},
		Provider: provider,
		Registry: NewRegistry(),
		History:  NewHistory(HistorySnapshot{Version: 1}, nil),
		Prompt:   PromptContext{Model: "Test"},
		Persistence: Persistence{AppendUsage: func(_ context.Context, record contract.UsageRecord) error {
			persisted = append(persisted, record)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 || persisted[0].Seq != 1 || persisted[1].Seq != 2 {
		t.Fatalf("records=%+v", persisted)
	}
	if persisted[0].Attribution != contract.CacheAttributionColdStart || persisted[1].Attribution != contract.CacheAttributionProvider {
		t.Fatalf("attributions=%q,%q", persisted[0].Attribution, persisted[1].Attribution)
	}
	if persisted[1].HitRate == nil || *persisted[1].HitRate != 0.9 {
		t.Fatalf("second hit rate=%v", persisted[1].HitRate)
	}

	resumedProvider := &scriptedProvider{responses: []contract.ChatResponse{{Content: "resumed", Usage: reportedUsage(100, 5, &read2, &miss2)}}}
	resumed, err := NewEngine(EngineConfig{
		Settings:            &settings,
		Session:             contract.Session{ID: "usage", WorkspacePath: t.TempDir()},
		Provider:            resumedProvider,
		Registry:            NewRegistry(),
		History:             NewHistory(engine.history.Snapshot(), nil),
		Prompt:              PromptContext{Model: "Test"},
		InitialUsageRecords: persisted,
		Persistence: Persistence{AppendUsage: func(_ context.Context, record contract.UsageRecord) error {
			persisted = append(persisted, record)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if aggregate := resumed.UsageAggregate(); aggregate.Requests != 2 || aggregate.SumPrompt != 200 || aggregate.SumCacheRead != 90 {
		t.Fatalf("restored aggregate=%+v", aggregate)
	}
	if _, _, err := resumed.Run(context.Background(), "after restart"); err != nil {
		t.Fatal(err)
	}
	last := persisted[len(persisted)-1]
	if last.Seq != 3 || last.Attribution != contract.CacheAttributionColdStart {
		t.Fatalf("resumed record=%+v", last)
	}
}

func TestEngineRejectsToolShapeChangeWithoutLedgerEvent(t *testing.T) {
	read, miss := 80, 20
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "first", Usage: reportedUsage(100, 1, &read, &miss)},
		{Content: "second", Usage: reportedUsage(100, 1, &read, &miss)},
	}}
	settings := engineSettings()
	registry := NewRegistry()
	var records []contract.UsageRecord
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "shape", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: registry, Prompt: PromptContext{Model: "Test"},
		Persistence: Persistence{AppendUsage: func(_ context.Context, record contract.UsageRecord) error {
			records = append(records, record)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	registry.Add(&recordingTool{name: "late_tool"})
	if _, _, err := engine.Run(context.Background(), "again"); err == nil {
		t.Fatal("expected unrecorded tool shape change to fail")
	}
	if len(records) != 1 {
		t.Fatalf("records=%+v", records)
	}
	if len(engine.InvalidationEvents()) != 0 {
		t.Fatalf("unexpected invalidation events=%+v", engine.InvalidationEvents())
	}
}

func TestEngineMarksUnexplainedPromptShrinkAgentSuspect(t *testing.T) {
	read1, miss1, read2, miss2 := 0, 1000, 256, 644
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "first", Usage: reportedUsage(1000, 1, &read1, &miss1)},
		{Content: "second", Usage: reportedUsage(900, 1, &read2, &miss2)},
	}}
	settings := engineSettings()
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	records := engine.UsageRecords()
	if records[1].Attribution != contract.CacheAttributionAgentSuspect {
		t.Fatalf("shrink attribution=%q record=%+v", records[1].Attribution, records[1])
	}
}

func TestModelSwitchRefreshesPromptAndRecordsBoundary(t *testing.T) {
	read, miss := 90, 10
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{Content: "first", Usage: reportedUsage(100, 1, &read, &miss)},
		{Content: "second", Usage: reportedUsage(100, 1, &read, &miss)},
	}}
	settings := engineSettings()
	settings.Provider.Models = append(settings.Provider.Models, contract.Model{ID: "next", Name: "Next", ContextLimit: 128000})
	engine, err := NewEngine(EngineConfig{Settings: &settings, Session: contract.Session{WorkspacePath: t.TempDir()}, Provider: provider, Registry: NewRegistry(), Prompt: PromptContext{Model: "Test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Run(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if err := engine.SwitchModel(context.Background(), "main", "next", "Next", "next addendum"); err != nil {
		t.Fatal(err)
	}
	if settings.Provider.ActiveModelID != "next" || engine.prompt.Model != "Next" || engine.prompt.ModelAddendum != "next addendum" {
		t.Fatalf("settings=%+v prompt=%+v", settings.Provider, engine.prompt)
	}
	if _, _, err := engine.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	records := engine.UsageRecords()
	if len(records) != 2 || records[1].Attribution != contract.CacheAttributionAgent {
		t.Fatalf("records=%+v", records)
	}
	events := engine.InvalidationEvents()
	if len(events) != 1 || events[0].Cause != contract.InvalidationModelSwitch || events[0].RequestSeq != 2 {
		t.Fatalf("events=%+v", events)
	}
}

// TestTaskUsageDeltaCoversAllStreamsAndLiveEmissionMatches: the per-task usage
// (and therefore the summary's cache %) must aggregate EVERY request the task
// made — main turns AND subagent runs — and the live Usage callback must land
// on exactly the same task-cumulative numbers, not the last request's.
func TestTaskUsageDeltaCoversAllStreamsAndLiveEmissionMatches(t *testing.T) {
	mainRead1, mainMiss1 := 0, 100
	subRead, subMiss := 40, 60
	mainRead2, mainMiss2 := 200, 8
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("s", "run_subagent", `{"agent":"explore","task":"survey the config loader"}`)}, Usage: reportedUsage(100, 5, &mainRead1, &mainMiss1)},
		{Content: "Findings: the loader is in config.go; validated.", Usage: reportedUsage(90, 4, &subRead, &subMiss)}, // subagent stream
		{Content: "All done.", Usage: reportedUsage(208, 6, &mainRead2, &mainMiss2)},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh // grants a subagent budget
	var emitted []contract.Usage
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "lifecycle", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "run_subagent"}, &recordingTool{name: "read_file"}),
		Prompt:    PromptContext{Model: "Test"},
		Callbacks: contract.Callbacks{Usage: func(u contract.Usage) { emitted = append(emitted, u) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Large-class prompt so the task grants an agent budget. Since feature 011
	// (D2), Large needs TWO corroborating size signals — here a breadth phrase
	// plus four named file paths.
	_, stats, err := engine.Run(context.Background(), "Implement a new config loader module with validation across the package and verify it in config.go, loader.go, validate.go and main.go")
	if err != nil {
		t.Fatal(err)
	}
	wantRead := mainRead1 + subRead + mainRead2 // 240
	wantMiss := mainMiss1 + subMiss + mainMiss2 // 168
	if stats.Usage.CacheReadTokens == nil || stats.Usage.CacheMissTokens == nil {
		t.Fatalf("task usage lost cache operands: %+v", stats.Usage)
	}
	if *stats.Usage.CacheReadTokens != wantRead || *stats.Usage.CacheMissTokens != wantMiss {
		t.Fatalf("task cache delta = %d/%d, want %d/%d (all three requests)", *stats.Usage.CacheReadTokens, *stats.Usage.CacheMissTokens, wantRead, wantMiss)
	}
	rate := contract.HitRate(stats.Usage.CacheReadTokens, stats.Usage.CacheMissTokens)
	wantRate := float64(wantRead) / float64(wantRead+wantMiss)
	if rate == nil || *rate != wantRate {
		t.Fatalf("summary hit rate = %v, want %v", rate, wantRate)
	}
	if len(emitted) == 0 {
		t.Fatal("no live usage emissions")
	}
	last := emitted[len(emitted)-1]
	if last.CacheReadTokens == nil || last.CacheMissTokens == nil || *last.CacheReadTokens != wantRead || *last.CacheMissTokens != wantMiss {
		t.Fatalf("live emission diverged from the task summary: %+v", last)
	}
	if last.TotalTokens != stats.Usage.TotalTokens {
		t.Fatalf("live tokens %d != summary tokens %d", last.TotalTokens, stats.Usage.TotalTokens)
	}
	// The live line must be task-cumulative mid-task too: the emission after the
	// subagent's request already includes the first main turn AND the subagent.
	sawSubCumulative := false
	for _, u := range emitted {
		if u.CacheReadTokens != nil && u.CacheMissTokens != nil && *u.CacheReadTokens == mainRead1+subRead && *u.CacheMissTokens == mainMiss1+subMiss {
			sawSubCumulative = true
		}
	}
	if !sawSubCumulative {
		t.Fatalf("no emission carried the cumulative main+subagent usage: %+v", emitted)
	}
}

func reportedUsage(prompt, completion int, read, miss *int) contract.Usage {
	return contract.Usage{
		PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion,
		CacheReadTokens: read, CacheMissTokens: miss, CachedTokens: valueOrZero(read),
		PromptTokensAvailable: true, CompletionTokensAvailable: true,
	}
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
