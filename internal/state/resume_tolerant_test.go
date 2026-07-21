package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// F-2: a crash mid-append can leave the final JSONL line truncated. Reading the
// ledger must return the intact prior records instead of failing the whole file
// and bricking resume.
func TestUsageRecordsToleratesTruncatedTrailingLine(t *testing.T) {
	ctx := context.Background()
	paths := testPaths(t)
	db, err := Open(ctx, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessions := Sessions{DB: db, Secrets: contract.Secrets{ProviderAPIKey: "sk-abcdefghijklmnop"}}
	session, err := sessions.New(ctx, t.TempDir(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.AppendUsage(session.ID, contract.UsageRecord{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := sessions.AppendUsage(session.ID, contract.UsageRecord{Seq: 2}); err != nil {
		t.Fatal(err)
	}
	dir, err := SessionDir(paths, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "usage.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":3,"promptTok`); err != nil { // partial write, no newline
		t.Fatal(err)
	}
	f.Close()

	records, err := sessions.UsageRecords(session.ID)
	if err != nil {
		t.Fatalf("a truncated trailing line must not fail the whole read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected the 2 intact records, got %d", len(records))
	}
}
