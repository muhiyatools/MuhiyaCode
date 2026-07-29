package tui

import (
	"context"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// fakeTranscriptPager returns a keyset pager over `total` synthetic events
// (ids 1..total), 20 per page, mirroring DB.TranscriptPage semantics.
func fakeTranscriptPager(total int) func(context.Context, contract.TranscriptPageRequest) (contract.TranscriptPage, error) {
	events := make([]contract.TranscriptEvent, total)
	for i := 0; i < total; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		events[i] = contract.TranscriptEvent{ID: int64(i + 1), Role: role, Content: fmt.Sprintf("event %d", i+1)}
	}
	const pageSize = 20
	page := func(entries []contract.TranscriptEvent, gen uint64, hasOlder bool) contract.TranscriptPage {
		if len(entries) == 0 {
			return contract.TranscriptPage{Generation: gen, HasOlder: false}
		}
		return contract.TranscriptPage{Generation: gen, Entries: entries, OldestID: entries[0].ID, NewestID: entries[len(entries)-1].ID, HasOlder: hasOlder}
	}
	return func(_ context.Context, req contract.TranscriptPageRequest) (contract.TranscriptPage, error) {
		switch req.Direction {
		case contract.PageInitialTail:
			start := max(0, total-pageSize)
			return page(events[start:], req.Generation, start > 0), nil
		case contract.PageBefore:
			end := len(events)
			for i, e := range events {
				if e.ID >= req.Cursor {
					end = i
					break
				}
			}
			start := max(0, end-pageSize)
			return page(events[start:end], req.Generation, start > 0), nil
		}
		return contract.TranscriptPage{Generation: req.Generation}, nil
	}
}

func pagingModel(t *testing.T, total int) *Model {
	t.Helper()
	m := NewModel(Options{Runtime: testRuntime(t), Actions: Actions{TranscriptPage: fakeTranscriptPager(total)}, Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return updated.(*Model)
}

// TestInitialPageEstablishesCursor (US1 T020) proves the initial-tail load sets the
// window, the keyset cursor, and the has-older flag.
func TestInitialPageEstablishesCursor(t *testing.T) {
	m := pagingModel(t, 100)
	cmd := m.loadInitialPageCmd()
	if cmd == nil {
		t.Fatal("no initial page command")
	}
	updated, _ := m.Update(cmd())
	m = updated.(*Model)
	if len(m.items) != 20 {
		t.Fatalf("window = %d items, want 20", len(m.items))
	}
	if m.oldestEventID != 81 {
		t.Fatalf("cursor = %d, want 81", m.oldestEventID)
	}
	if !m.hasOlderEvents {
		t.Fatal("has-older not set for a 100-event / 20-window session")
	}
}

func TestInitialPageDoesNotReplaceJournalRecentWindow(t *testing.T) {
	recent := []contract.Event{{Role: "assistant", Type: "message", Content: "journal authority"}}
	m := NewModel(Options{
		Runtime: testRuntime(t),
		Actions: Actions{TranscriptPage: fakeTranscriptPager(10)},
		Version: "test",
		Recent:  recent,
	})
	m.applyInitialPage(contract.TranscriptPage{
		Entries:  []contract.TranscriptEvent{{ID: 10, Role: "assistant", Content: "stale legacy projection"}},
		OldestID: 10,
	})
	if len(m.items) != 1 || m.items[0].content != "journal authority" {
		t.Fatalf("legacy page replaced journal recent window: %+v", m.items)
	}
}

// TestScrollToTopLoadsOlderPage (US1 T020/T023) proves scrolling to the top loads
// and prepends the previous page, advances the cursor, and shifts the viewport so
// the same content stays in view (anchor compensation).
func TestScrollToTopLoadsOlderPage(t *testing.T) {
	m := pagingModel(t, 100)
	updated, _ := m.Update(m.loadInitialPageCmd()())
	m = updated.(*Model)
	// Scroll to the very top and let Update trigger the older-page load.
	m.viewport.SetYOffset(0)
	cmd := m.maybeLoadOlder()
	if cmd == nil {
		t.Fatal("no older-page load near the top")
	}
	updated, _ = m.Update(cmd())
	m = updated.(*Model)
	if len(m.items) != 40 {
		t.Fatalf("after prepend = %d items, want 40", len(m.items))
	}
	if m.oldestEventID != 61 {
		t.Fatalf("cursor after older page = %d, want 61", m.oldestEventID)
	}
	if m.items[0].content != "event 61" {
		t.Fatalf("oldest item = %q, want 'event 61'", m.items[0].content)
	}
	if m.viewport.YOffset() == 0 {
		t.Fatal("viewport not re-anchored after prepend (would jump to the top)")
	}
}

// TestPagingStopsWhenExhausted (US1 T020) proves paging reports no-older once the
// beginning of the transcript is reached.
func TestPagingStopsWhenExhausted(t *testing.T) {
	m := pagingModel(t, 30) // two pages: tail(11..30) then before → 1..10
	updated, _ := m.Update(m.loadInitialPageCmd()())
	m = updated.(*Model)
	m.viewport.SetYOffset(0)
	updated, _ = m.Update(m.maybeLoadOlder()())
	m = updated.(*Model)
	if m.hasOlderEvents {
		t.Fatal("should have no older events after loading back to id 1")
	}
	m.viewport.SetYOffset(0)
	if m.maybeLoadOlder() != nil {
		t.Fatal("should not load again with no older events")
	}
}

// TestStalePageDiscardedAfterSessionSwitch (US1 T020) proves a page result from a
// prior generation (session) is dropped without touching the frame.
func TestStalePageDiscardedAfterSessionSwitch(t *testing.T) {
	m := pagingModel(t, 100)
	updated, _ := m.Update(m.loadInitialPageCmd()())
	m = updated.(*Model)
	before := len(m.items)
	m.resetPaging() // simulate a session switch bumping the generation
	// A page tagged with the OLD generation must be ignored.
	stale := transcriptPageMsg{page: contract.TranscriptPage{Generation: 0, Entries: []contract.TranscriptEvent{{ID: 1, Role: "user", Content: "stale"}}, OldestID: 1, HasOlder: false}}
	updated, _ = m.Update(stale)
	m = updated.(*Model)
	if len(m.items) != before {
		t.Fatal("a stale-generation page mutated the transcript")
	}
}
