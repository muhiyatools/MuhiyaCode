package state

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func seedEventsForPaging(t *testing.T, db *DB, ctx context.Context, contents []string) string {
	t.Helper()
	session, err := db.CreateSession(ctx, `F:\w`, "paging")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, content := range contents {
		if err := db.AddEvent(ctx, session.ID, "user", "message", content); err != nil {
			t.Fatalf("add event: %v", err)
		}
	}
	return session.ID
}

func contentsOf(entries []contract.TranscriptEvent) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Content
	}
	return out
}

func TestTranscriptPageKeysetDirections(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var contents []string
	for i := 0; i < 10; i++ {
		contents = append(contents, fmt.Sprintf("e%d", i))
	}
	sid := seedEventsForPaging(t, db, ctx, contents)

	// initial-tail: newest 4, ascending, older exists, no newer.
	tail, err := db.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: sid, Direction: contract.PageInitialTail, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := contentsOf(tail.Entries); strings.Join(got, ",") != "e6,e7,e8,e9" {
		t.Fatalf("tail entries = %v", got)
	}
	if !tail.HasOlder || tail.HasNewer {
		t.Fatalf("tail paging flags: older=%v newer=%v", tail.HasOlder, tail.HasNewer)
	}
	// Ascending stable-ID ordering.
	for i := 1; i < len(tail.Entries); i++ {
		if tail.Entries[i].ID <= tail.Entries[i-1].ID {
			t.Fatalf("entries not ascending by ID: %+v", tail.Entries)
		}
	}

	// before the tail's oldest: previous 4, older still exists, newer exists.
	before, err := db.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: sid, Direction: contract.PageBefore, Cursor: tail.OldestID, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := contentsOf(before.Entries); strings.Join(got, ",") != "e2,e3,e4,e5" {
		t.Fatalf("before entries = %v", got)
	}
	if !before.HasOlder || !before.HasNewer {
		t.Fatalf("before flags: older=%v newer=%v", before.HasOlder, before.HasNewer)
	}

	// after the newest: nothing newer.
	after, err := db.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: sid, Direction: contract.PageAfter, Cursor: tail.NewestID, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 0 || after.HasNewer {
		t.Fatalf("after-newest should be empty with no newer: %+v", after)
	}
}

func TestTranscriptPageByteBudgetAndOversized(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Five 100-byte events; a 250-byte budget keeps only the newest two.
	var contents []string
	for i := 0; i < 5; i++ {
		contents = append(contents, strings.Repeat("x", 100))
	}
	sid := seedEventsForPaging(t, db, ctx, contents)
	page, err := db.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: sid, Direction: contract.PageInitialTail, Limit: 100, ByteBudget: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.RawBytes != 200 {
		t.Fatalf("byte budget not enforced: entries=%d bytes=%d", len(page.Entries), page.RawBytes)
	}
	if !page.HasOlder {
		t.Fatal("budget-trimmed page must report older content exists")
	}

	// A single oversized entry still loads (one entry may exceed a page budget).
	db2, _ := Open(ctx, testPaths(t))
	defer db2.Close()
	big := seedEventsForPaging(t, db2, ctx, []string{strings.Repeat("y", 500)})
	one, err := db2.TranscriptPage(ctx, contract.TranscriptPageRequest{SessionID: big, Direction: contract.PageInitialTail, ByteBudget: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Entries) != 1 || one.RawBytes != 500 {
		t.Fatalf("oversized entry not returned: %+v", one)
	}
}

func TestTranscriptPageCancellationAndCompatibility(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sid := seedEventsForPaging(t, db, ctx, []string{"a", "b", "c"})

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.TranscriptPage(cancelled, contract.TranscriptPageRequest{SessionID: sid, Direction: contract.PageInitialTail}); err == nil {
		t.Fatal("expected a cancelled context to fail the page query")
	}

	// The legacy Events() reader is unchanged by the additive paging query.
	events, err := db.Events(ctx, sid, 0)
	if err != nil || len(events) != 3 {
		t.Fatalf("legacy Events broke: n=%d err=%v", len(events), err)
	}
}
