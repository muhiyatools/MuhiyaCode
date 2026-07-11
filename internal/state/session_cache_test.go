package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func TestSessionUsageAndInvalidationJSONLRoundTrip(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db}
	session, err := sessions.New(ctx, filepath.Join(t.TempDir(), "workspace"), "cache")
	if err != nil {
		t.Fatal(err)
	}
	read, miss := 12, 3
	record := contract.UsageRecord{Seq: 1, At: time.Unix(1, 0).UTC(), Model: "model", CacheReadTokens: &read, CacheMissTokens: &miss, ChangeReasons: []string{}, Attribution: contract.CacheAttributionColdStart}
	if err := sessions.AppendUsage(session.ID, record); err != nil {
		t.Fatal(err)
	}
	pressure := 0.75
	event := contract.InvalidationEvent{At: time.Unix(2, 0).UTC(), Cause: contract.InvalidationTrim, Trigger: contract.InvalidationPressure, Scope: "trimmed tool payload", Pressure: &pressure, RequestSeq: 2}
	if err := sessions.AppendInvalidation(session.ID, event); err != nil {
		t.Fatal(err)
	}
	records, err := sessions.UsageRecords(session.ID)
	if err != nil || len(records) != 1 || records[0].CacheReadTokens == nil || *records[0].CacheReadTokens != read {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	events, err := sessions.InvalidationEvents(session.ID)
	if err != nil || len(events) != 1 || events[0].RequestSeq != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
