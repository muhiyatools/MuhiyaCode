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

func TestEngineAttributesToolShapeChangeToAgent(t *testing.T) {
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
	if _, _, err := engine.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[1].Attribution != contract.CacheAttributionAgent || !records[1].PrefixChanged {
		t.Fatalf("records=%+v", records)
	}
	if len(records[1].ChangeReasons) != 1 || records[1].ChangeReasons[0] != PrefixReasonTools {
		t.Fatalf("change reasons=%v", records[1].ChangeReasons)
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
