package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestUsagePersistenceDoesNotBlockSnapshots(t *testing.T) {
	persistStarted := make(chan struct{})
	releasePersist := make(chan struct{})
	engine := &Engine{persistence: Persistence{AppendUsage: func(context.Context, contract.UsageRecord) error {
		close(persistStarted)
		<-releasePersist
		return nil
	}}}
	persistDone := make(chan error, 1)
	go func() {
		persistDone <- engine.recordUsageAndEmit(func() error {
			return engine.recordAuxUsage(context.Background(), "utility", ":aux", contract.Usage{}, nil)
		})
	}()
	<-persistStarted

	snapshotDone := make(chan struct{})
	go func() {
		engine.UsageAggregate()
		close(snapshotDone)
	}()
	select {
	case <-snapshotDone:
	case <-time.After(time.Second):
		close(releasePersist)
		t.Fatal("usage snapshot blocked on persistence")
	}
	close(releasePersist)
	if err := <-persistDone; err != nil {
		t.Fatal(err)
	}
}

func TestUsageAggregateSnapshotDoesNotAliasEngine(t *testing.T) {
	rate := 0.75
	engine := &Engine{usageAggregate: contract.SessionUsageAggregate{AllStreamHitRate: &rate}}
	snapshot := engine.UsageAggregate()
	*snapshot.AllStreamHitRate = 0
	if *engine.usageAggregate.AllStreamHitRate != rate {
		t.Fatal("usage snapshot mutated engine-owned all-stream hit rate")
	}
}

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

func TestModelSwitchIsRejectedAfterSessionStarts(t *testing.T) {
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
	if err := engine.SwitchModel(context.Background(), "main", "next", "Next", "next addendum"); err == nil {
		t.Fatal("established session allowed a model switch that would break cache continuity")
	}
	if settings.Provider.ActiveModelID == "next" || engine.prompt.Model == "Next" {
		t.Fatalf("rejected model switch mutated settings=%+v prompt=%+v", settings.Provider, engine.prompt)
	}
	if events := engine.InvalidationEvents(); len(events) != 0 {
		t.Fatalf("rejected model switch recorded an invalidation: %+v", events)
	}
}

// TestTaskUsageDeltaCoversEveryRequestAndLiveEmissionMatches: the per-task usage
// (and therefore the summary's cache %) must aggregate EVERY request the task
// made, and the live Usage callback must land on exactly the same
// task-cumulative numbers, not the last request's.
func TestTaskUsageDeltaCoversEveryRequestAndLiveEmissionMatches(t *testing.T) {
	read1, miss1 := 0, 100
	read2, miss2 := 40, 60
	read3, miss3 := 200, 8
	provider := &scriptedProvider{responses: []contract.ChatResponse{
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r1", "read_file", `{"path":"config.go"}`)}, Usage: reportedUsage(100, 5, &read1, &miss1)},
		{ToolCalls: []contract.ToolCall{contract.NewToolCall("r2", "read_file", `{"path":"loader.go"}`)}, Usage: reportedUsage(90, 4, &read2, &miss2)},
		{Content: "All done.", Usage: reportedUsage(208, 6, &read3, &miss3)},
	}}
	settings := engineSettings()
	settings.Effort = contract.EffortHigh
	var emitted []contract.Usage
	engine, err := NewEngine(EngineConfig{
		Settings: &settings, Session: contract.Session{ID: "lifecycle", WorkspacePath: t.TempDir()},
		Provider: provider, Registry: NewRegistry(&recordingTool{name: "read_file"}),
		Prompt:    PromptContext{Model: "Test"},
		Callbacks: contract.Callbacks{Usage: func(u contract.Usage) { emitted = append(emitted, u) }},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, stats, err := engine.Run(context.Background(), "Implement a new config loader module with validation across the package and verify it in config.go, loader.go, validate.go and main.go")
	if err != nil {
		t.Fatal(err)
	}
	wantRead := read1 + read2 + read3 // 240
	wantMiss := miss1 + miss2 + miss3 // 168
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
	// The live line must be task-cumulative MID-task too: the emission after the
	// second request already carries the first two requests summed, not just the
	// second one's numbers.
	sawMidCumulative := false
	for _, u := range emitted {
		if u.CacheReadTokens != nil && u.CacheMissTokens != nil && *u.CacheReadTokens == read1+read2 && *u.CacheMissTokens == miss1+miss2 {
			sawMidCumulative = true
		}
	}
	if !sawMidCumulative {
		t.Fatalf("no emission carried the mid-task cumulative usage: %+v", emitted)
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
