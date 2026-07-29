package contract

import "testing"

func pairedRecord(stream UsageStream, read, miss int, attribution CacheAttribution) UsageRecord {
	return UsageRecord{Stream: stream, CacheReadTokens: &read, CacheMissTokens: &miss, Attribution: attribution}
}

// TestAllStreamHitRateCountsAuxAndMain (013 FR-021 / SC-006) is the whole
// point of the field: the displayed session rate must describe the SESSION, not
// just the main conversation. The fixture is deliberately lopsided — a
// well-cached main stream and a cold aux stream — so a main-only computation
// gives a visibly different (and flattering) answer.
func TestAllStreamHitRateCountsSubagentsAndAux(t *testing.T) {
	aggregate := AggregateUsage([]UsageRecord{
		pairedRecord(UsageStreamMain, 90, 10, CacheAttributionProvider),
		pairedRecord(UsageStreamAux, 0, 100, CacheAttributionNA),
		pairedRecord(UsageStreamAux, 10, 90, CacheAttributionNA),
	})
	if aggregate.AllStreamHitRate == nil {
		t.Fatal("all-stream rate unavailable despite paired records")
	}
	// (90+0+10) read over (100+100+100) total = 33.33%
	if got := *aggregate.AllStreamHitRate; got < 0.3332 || got > 0.3334 {
		t.Fatalf("all-stream rate = %v, want ~0.3333 over every stream", got)
	}
	if aggregate.SessionHitRate == nil || *aggregate.SessionHitRate != 0.9 {
		t.Fatalf("main-only SessionHitRate should be unchanged at 0.9, got %v", aggregate.SessionHitRate)
	}
	if *aggregate.AllStreamHitRate == *aggregate.SessionHitRate {
		t.Fatal("the two rates must differ here, or the fixture cannot prove aux is counted")
	}
}

// Constitution VI: a one-sided provider payload must never become part of a
// denominator. Only records reporting BOTH operands may count.
func TestAllStreamHitRateIgnoresOneSidedRecords(t *testing.T) {
	read := 50
	aggregate := AggregateUsage([]UsageRecord{
		pairedRecord(UsageStreamMain, 80, 20, CacheAttributionProvider),
		{Stream: UsageStreamAux, CacheReadTokens: &read}, // miss unknown: excluded from the rate
	})
	if aggregate.AllStreamHitRate == nil || *aggregate.AllStreamHitRate != 0.8 {
		t.Fatalf("one-sided record polluted the rate: %v", aggregate.AllStreamHitRate)
	}
	// It still contributes to the displayed SUM, which is a reconciling total.
	if aggregate.SumCacheRead != 130 {
		t.Fatalf("display sum should keep every reported value, got %d", aggregate.SumCacheRead)
	}
}

// FR-022: no cache reporting at all is "unavailable", never a fabricated 0%.
func TestAllStreamHitRateUnavailableWithoutCacheFields(t *testing.T) {
	prompt := 100
	aggregate := AggregateUsage([]UsageRecord{{Stream: UsageStreamMain, PromptTokens: &prompt}})
	if aggregate.AllStreamHitRate != nil {
		t.Fatalf("expected unavailable, got %v", *aggregate.AllStreamHitRate)
	}
}

func TestAllStreamHitRateEmptySessionIsUnavailable(t *testing.T) {
	if rate := AggregateUsage(nil).AllStreamHitRate; rate != nil {
		t.Fatalf("empty session should be unavailable, got %v", *rate)
	}
}

// A single-stream session must agree with the main-only KPI, so adopting the
// new field cannot silently shift ordinary sessions' displayed numbers.
func TestAllStreamHitRateMatchesMainOnlyForSingleStreamSessions(t *testing.T) {
	aggregate := AggregateUsage([]UsageRecord{
		pairedRecord(UsageStreamMain, 70, 30, CacheAttributionColdStart),
		pairedRecord(UsageStreamMain, 90, 10, CacheAttributionProvider),
	})
	if aggregate.AllStreamHitRate == nil || aggregate.SessionHitRate == nil {
		t.Fatal("both rates should be available")
	}
	if *aggregate.AllStreamHitRate != *aggregate.SessionHitRate {
		t.Fatalf("single-stream session diverged: all=%v main=%v", *aggregate.AllStreamHitRate, *aggregate.SessionHitRate)
	}
}
