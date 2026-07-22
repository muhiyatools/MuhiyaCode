package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAggregateUsagePreservesAvailabilityAndColdStart(t *testing.T) {
	zero, ten, two, eight := 0, 10, 2, 8
	records := []UsageRecord{
		{PromptTokens: &ten, CompletionTokens: &zero, CacheReadTokens: &zero, CacheMissTokens: &ten, Attribution: CacheAttributionColdStart},
		{PromptTokens: &ten, CompletionTokens: &two, CacheReadTokens: &eight, CacheMissTokens: &two, Attribution: CacheAttributionProvider},
		{Attribution: CacheAttributionNA},
	}
	aggregate := AggregateUsage(records)
	if aggregate.Requests != 3 || aggregate.UnavailableRequests != 1 || aggregate.SumPrompt != 20 {
		t.Fatalf("unexpected aggregate: %+v", aggregate)
	}
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.4 {
		t.Fatalf("session rate = %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != 0.8 {
		t.Fatalf("steady-state rate = %v", aggregate.SteadyStateHitRate)
	}
}

func TestHitRateZeroDenominatorIsUnavailable(t *testing.T) {
	zero := 0
	if got := HitRate(&zero, &zero); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
}

func TestAggregateUsageSumsPartialValuesWithoutUsingThemInRates(t *testing.T) {
	five, seven, three, one := 5, 7, 3, 1
	records := []UsageRecord{
		{CacheReadTokens: &five, Attribution: CacheAttributionNA},
		{CacheMissTokens: &seven, Attribution: CacheAttributionNA},
		{CacheReadTokens: &three, CacheMissTokens: &one, Attribution: CacheAttributionProvider},
		{CacheReadTokens: &one, CacheMissTokens: &three, Attribution: CacheAttributionColdStart},
	}

	aggregate := AggregateUsage(records)
	if aggregate.SumCacheRead != 9 || aggregate.SumCacheMiss != 11 {
		t.Fatalf("independent sums were not preserved: %+v", aggregate)
	}
	// Rate arithmetic (including the per-task delta) must draw on the paired
	// sums only — a one-sided record cannot contribute to a denominator.
	if aggregate.PairedCacheRead != 4 || aggregate.PairedCacheMiss != 4 {
		t.Fatalf("paired sums must exclude one-sided records: %+v", aggregate)
	}
	if aggregate.CacheAvailable != 2 || aggregate.UnavailableRequests != 2 {
		t.Fatalf("unexpected availability counts: %+v", aggregate)
	}
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.5 {
		t.Fatalf("session rate included a partial record: %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != 0.75 {
		t.Fatalf("steady-state rate = %v", aggregate.SteadyStateHitRate)
	}
}

func TestAggregateUsageHasNoSteadyStateRateWithOnlyColdStarts(t *testing.T) {
	read, miss := 9, 1
	aggregate := AggregateUsage([]UsageRecord{{
		CacheReadTokens: &read,
		CacheMissTokens: &miss,
		Attribution:     CacheAttributionColdStart,
	}})
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.9 {
		t.Fatalf("session rate = %v", aggregate.SessionHitRate)
	}
	if aggregate.SteadyStateHitRate != nil {
		t.Fatalf("expected unavailable steady-state rate, got %v", *aggregate.SteadyStateHitRate)
	}
}

func TestAggregateUsageComputesPrefixStabilityFromMainStreamOnly(t *testing.T) {
	zero, hundred, oneHundredTen, ninetyEight, twelve, auxRead, auxMiss := 0, 100, 110, 98, 12, 0, 1000
	tail := 10
	records := []UsageRecord{
		{Stream: UsageStreamMain, PromptTokens: &hundred, CacheReadTokens: &zero, CacheMissTokens: &hundred, Attribution: CacheAttributionColdStart},
		{Stream: UsageStreamAux, PromptTokens: &hundred, CacheReadTokens: &auxRead, CacheMissTokens: &auxMiss, Attribution: CacheAttributionNA},
		{Stream: UsageStreamMain, PromptTokens: &oneHundredTen, CacheReadTokens: &ninetyEight, CacheMissTokens: &twelve, NewTailTokens: &tail, Attribution: CacheAttributionProvider},
	}
	aggregate := AggregateUsage(records)
	if aggregate.PrefixStabilityRate == nil || *aggregate.PrefixStabilityRate != 0.98 {
		t.Fatalf("prefix stability=%v aggregate=%+v", aggregate.PrefixStabilityRate, aggregate)
	}
	if aggregate.MainRequests != 2 || aggregate.AuxRequests != 1 || aggregate.SubagentRequests != 0 {
		t.Fatalf("stream counts=%+v", aggregate)
	}
	if aggregate.SteadyStateHitRate == nil || *aggregate.SteadyStateHitRate != float64(ninetyEight)/float64(ninetyEight+twelve) {
		t.Fatalf("aux usage polluted main rate: %+v", aggregate)
	}
}

func TestUsageRecordBackwardAndForwardCompatibleJSON(t *testing.T) {
	oldRow := []byte(`{"seq":7,"at":"2026-07-22T00:00:00Z","model":"legacy","prompt_tokens":10,"completion_tokens":2,"cache_read_tokens":null,"cache_miss_tokens":0,"miss_derived":false,"hit_rate":null,"prefix_changed":false,"change_reasons":[],"attribution":"provider","future_field":{"safe":"ignored"}}`)
	var old UsageRecord
	if err := json.Unmarshal(oldRow, &old); err != nil {
		t.Fatal(err)
	}
	if old.Seq != 7 || old.Model != "legacy" || old.CacheMissTokens == nil || *old.CacheMissTokens != 0 {
		t.Fatalf("legacy row changed meaning: %+v", old)
	}
	if old.CacheReadTokens != nil || old.CacheWriteTokens != nil || old.CacheCreationTokens != nil || old.UncachedInputTokens != nil {
		t.Fatalf("missing cache members must remain unavailable: %+v", old)
	}
	if old.TaskEpochID != nil || old.Phase != nil || old.Transport != nil || old.RetryOf != nil {
		t.Fatalf("legacy economy identity must remain absent: %+v", old)
	}
}

func TestUsageRecordNewEconomyFieldsRoundTripNullVersusZero(t *testing.T) {
	zero, five, retry := 0, 5, 41
	epoch, phase, transport := "epoch-2", "verify", "minimax-anthropic"
	record := UsageRecord{
		Seq: 42, At: time.Unix(1, 2).UTC(), Model: "model", Stream: UsageStreamMain,
		TaskEpochID: &epoch, Phase: &phase, Transport: &transport,
		FinishReason: "stop", RetryOf: &retry,
		CacheReadTokens: &zero, CacheWriteTokens: &zero, CacheCreationTokens: nil, UncachedInputTokens: &five,
		ManifestHash: "sha256:manifest", BudgetDecision: "override", CacheUsageSchema: "anthropic-v1", CacheUsageDerivation: "direct",
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded UsageRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.CacheWriteTokens == nil || *decoded.CacheWriteTokens != 0 {
		t.Fatalf("reported zero write was lost: %s", raw)
	}
	if decoded.CacheCreationTokens != nil {
		t.Fatalf("unavailable creation became zero: %s", raw)
	}
	if decoded.RetryOf == nil || *decoded.RetryOf != retry || decoded.TaskEpochID == nil || *decoded.TaskEpochID != epoch {
		t.Fatalf("identity fields did not round trip: %+v", decoded)
	}
	if decoded.ManifestHash != record.ManifestHash || decoded.BudgetDecision != record.BudgetDecision || decoded.CacheUsageDerivation != "direct" {
		t.Fatalf("economy metadata did not round trip: %+v", decoded)
	}
}

func TestAggregateUsageReplayAndExtendedCacheMembers(t *testing.T) {
	ten, twenty, read, write, create, uncached := 10, 20, 8, 2, 3, 4
	records := []UsageRecord{
		{Stream: UsageStreamMain, PromptTokens: &ten, CacheReadTokens: &read, CacheWriteTokens: &write, UncachedInputTokens: &uncached},
		{Stream: UsageStreamMain, PromptTokens: &twenty, CacheCreationTokens: &create},
		{Stream: UsageStreamAux, PromptTokens: &ten},
	}
	got := AggregateUsage(records)
	if got.SumMainPrompt != 30 || got.MaxMainPromptTokens == nil || *got.MaxMainPromptTokens != 20 {
		t.Fatalf("main prompt aggregate=%+v", got)
	}
	if got.ReplayAmplification == nil || *got.ReplayAmplification != 1.5 {
		t.Fatalf("replay amplification=%v", got.ReplayAmplification)
	}
	if got.SumCacheWrite != 2 || got.SumCacheCreation != 3 || got.SumUncachedInput != 4 || got.CacheWriteAvailable != 1 || got.CacheCreateAvailable != 1 || got.UncachedAvailable != 1 {
		t.Fatalf("extended cache aggregate=%+v", got)
	}

	missing := AggregateUsage(append(records, UsageRecord{Stream: UsageStreamMain}))
	if missing.ReplayAmplification != nil || missing.MaxMainPromptTokens != nil || missing.MainUsageUnavailable != 1 {
		t.Fatalf("partial main ledger fabricated replay values: %+v", missing)
	}
}
