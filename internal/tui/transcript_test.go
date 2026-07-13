package tui

import (
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

func ev(id int64, content string) contract.TranscriptEvent {
	return contract.TranscriptEvent{ID: id, SessionID: "s", Role: "user", Kind: "message", Content: content}
}

func page(entries ...contract.TranscriptEvent) contract.TranscriptPage {
	p := contract.TranscriptPage{SessionID: "s", Generation: 1, Entries: entries}
	if len(entries) > 0 {
		p.OldestID, p.NewestID = entries[0].ID, entries[len(entries)-1].ID
	}
	return p
}

func TestTranscriptWindowIngestAscendingAndFlags(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	p := page(ev(3, "c"), ev(4, "d"))
	p.HasOlder = true
	if !w.Ingest(p) {
		t.Fatal("valid page rejected")
	}
	if w.Len() != 2 || !w.HasOlder() || w.HasNewer() {
		t.Fatalf("flags: len=%d older=%v newer=%v", w.Len(), w.HasOlder(), w.HasNewer())
	}
	if w.Entries()[0].ID != 3 || w.Entries()[1].ID != 4 {
		t.Fatalf("not ascending: %+v", w.Entries())
	}
}

func TestTranscriptWindowRejectsStale(t *testing.T) {
	w := NewTranscriptWindow("s", 2)
	if w.Ingest(contract.TranscriptPage{SessionID: "s", Generation: 1, Entries: []contract.TranscriptEvent{ev(1, "x")}}) {
		t.Fatal("stale generation should be rejected")
	}
	if w.Ingest(contract.TranscriptPage{SessionID: "other", Generation: 2, Entries: []contract.TranscriptEvent{ev(1, "x")}}) {
		t.Fatal("wrong session should be rejected")
	}
	if w.Len() != 0 {
		t.Fatal("a rejected page must not mutate the window")
	}
}

func TestTranscriptWindowDedupMerge(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	w.Ingest(page(ev(2, "b"), ev(3, "c")))
	w.Ingest(page(ev(1, "a"), ev(2, "b")))
	if w.Len() != 3 {
		t.Fatalf("dedup failed: %d", w.Len())
	}
	got := w.Entries()
	if got[0].ID != 1 || got[1].ID != 2 || got[2].ID != 3 {
		t.Fatalf("merge order wrong: %+v", got)
	}
}

func TestTranscriptWindowEvictsToEntryBudget(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	w.entryBudget = 3
	var entries []contract.TranscriptEvent
	for i := int64(1); i <= 10; i++ {
		entries = append(entries, ev(i, "x"))
	}
	w.Ingest(page(entries...))
	if w.Len() != 3 || !w.HasOlder() {
		t.Fatalf("not evicted to budget: len=%d older=%v", w.Len(), w.HasOlder())
	}
	if w.Entries()[w.Len()-1].ID != 10 {
		t.Fatal("newest entry must be retained (tail-following)")
	}
}

func TestTranscriptWindowEvictsToByteBudget(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	w.byteBudget = 250
	var entries []contract.TranscriptEvent
	for i := int64(1); i <= 5; i++ {
		entries = append(entries, ev(i, strings.Repeat("x", 100)))
	}
	w.Ingest(page(entries...))
	if w.RawBytes() > 250 || w.Len() < 1 {
		t.Fatalf("byte budget not enforced: bytes=%d len=%d", w.RawBytes(), w.Len())
	}
	if !w.HasOlder() {
		t.Fatal("byte eviction should set hasOlder")
	}
}

func TestTranscriptWindowAnchorSurvivesEviction(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	w.entryBudget = 3
	var entries []contract.TranscriptEvent
	for i := int64(1); i <= 5; i++ {
		entries = append(entries, ev(i, "x"))
	}
	w.Ingest(page(entries[:3]...)) // ids 1,2,3
	w.SetAnchor(ViewportAnchor{EventID: 1, IntraEntryRow: 2})
	w.Ingest(page(entries...)) // grows to 1..5, evicts to newest 3 (ids 3,4,5)
	if w.Anchor().EventID == 1 {
		t.Fatal("anchor still points at an evicted entry")
	}
	if w.Anchor().EventID != w.Entries()[0].ID {
		t.Fatalf("anchor not re-pinned to oldest retained: anchor=%d oldest=%d", w.Anchor().EventID, w.Entries()[0].ID)
	}
	// A following anchor is left alone (it tracks the newest, not a fixed ID).
	w2 := NewTranscriptWindow("s", 1)
	w2.entryBudget = 2
	w2.SetAnchor(ViewportAnchor{FollowOutput: true})
	w2.Ingest(page(entries...))
	if !w2.Anchor().FollowOutput {
		t.Fatal("following anchor must stay following through eviction")
	}
}

func TestTranscriptWindowAppendLiveAndReconcile(t *testing.T) {
	w := NewTranscriptWindow("s", 1)
	w.AppendLive(ev(-1, "live"))
	if w.Len() != 1 || w.HasNewer() {
		t.Fatalf("append live: len=%d newer=%v", w.Len(), w.HasNewer())
	}
	if !w.Reconcile(-1, ev(5, "live")) {
		t.Fatal("reconcile failed")
	}
	if w.Entries()[0].ID != 5 {
		t.Fatalf("reconcile did not swap identity: %d", w.Entries()[0].ID)
	}
	w.AppendLive(ev(-2, "live2"))
	w.Reconcile(-2, ev(3, "live2"))
	if w.Entries()[0].ID != 3 || w.Entries()[1].ID != 5 {
		t.Fatalf("ordering wrong after reconcile: %d,%d", w.Entries()[0].ID, w.Entries()[1].ID)
	}
}
