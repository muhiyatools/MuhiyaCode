package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestLoadWorkloadAndDerivedCost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workload.md")
	if err := os.WriteFile(path, []byte("# test\n\n1. first\n2. second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	turns, err := loadWorkload(path)
	if err != nil || len(turns) != 2 || turns[1] != "second" {
		t.Fatalf("turns=%v err=%v", turns, err)
	}
	aggregate := contract.SessionUsageAggregate{SumCacheMiss: 1_000_000, SumCacheRead: 2_000_000, SumCompletion: 500_000}
	cost := deriveCost(aggregate, priceTable{UncachedInputPerMillion: 2, CacheReadPerMillion: .2, OutputPerMillion: 4, Source: "test"})
	if cost == nil || *cost != 4.4 {
		t.Fatalf("cost=%v", cost)
	}
}

func TestCountUnattributedRequiresLedgerForAgentChange(t *testing.T) {
	miss := 5
	records := []contract.UsageRecord{
		{Seq: 1, CacheMissTokens: &miss, Attribution: contract.CacheAttributionColdStart},
		{Seq: 2, CacheMissTokens: &miss, Attribution: contract.CacheAttributionAgent},
		{Seq: 3, CacheMissTokens: &miss, Attribution: contract.CacheAttributionProvider},
	}
	if got := countUnattributed(records, nil); got != 1 {
		t.Fatalf("unattributed=%d", got)
	}
	events := []contract.InvalidationEvent{{RequestSeq: 2}}
	if got := countUnattributed(records, events); got != 0 {
		t.Fatalf("unattributed with event=%d", got)
	}
}
