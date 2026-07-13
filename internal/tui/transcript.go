package tui

import "github.com/muhiya/muhiyacode/internal/contract"

// TranscriptWindow is the bounded presentation cache over a session's durable
// transcript (feature 005 US1, data-model §1.4). It holds only the visible page
// plus overscan — never the whole history — so per-event work and memory stay
// independent of total session length. Pages are merged by stable ID; entries
// beyond the budgets are evicted from the oldest end (the window follows the
// tail). Full durable content always remains queryable from SQLite.
const (
	defaultEntryBudget   = 384
	defaultRawByteBudget = 8 << 20 // 8 MiB
)

// ViewportAnchor is the logical scroll position: which entry is anchored at the
// top of the viewport and the wrapped row within it (data-model §1.3). It is
// independent of a giant line offset, so it survives eviction and reflow.
type ViewportAnchor struct {
	EventID       int64
	IntraEntryRow int
	FollowOutput  bool // true only when the user is pinned to the newest content
}

type TranscriptWindow struct {
	SessionID   string
	Generation  uint64
	entries     []contract.TranscriptEvent // ascending by ID
	anchor      ViewportAnchor
	hasOlder    bool
	hasNewer    bool
	entryBudget int
	byteBudget  int
	rawBytes    int
}

func NewTranscriptWindow(sessionID string, generation uint64) *TranscriptWindow {
	return &TranscriptWindow{
		SessionID:   sessionID,
		Generation:  generation,
		entryBudget: defaultEntryBudget,
		byteBudget:  defaultRawByteBudget,
	}
}

// Ingest merges a keyset page. A page for a different session or generation is a
// stale async result and is rejected (returns false) without touching the window.
func (w *TranscriptWindow) Ingest(page contract.TranscriptPage) bool {
	if page.SessionID != w.SessionID || page.Generation != w.Generation {
		return false
	}
	oldestBefore, newestBefore := w.oldestID(), w.newestID()
	for _, event := range page.Entries {
		w.upsert(event)
	}
	// Adopt the page's paging flags only for the edge it actually extended.
	if len(page.Entries) == 0 || page.OldestID <= oldestBefore || oldestBefore == 0 {
		w.hasOlder = page.HasOlder
	}
	if len(page.Entries) == 0 || page.NewestID >= newestBefore {
		w.hasNewer = page.HasNewer
	}
	w.evict()
	return true
}

// AppendLive adds a newly-observed live event at the tail. The live event is the
// newest, so there is nothing newer to page.
func (w *TranscriptWindow) AppendLive(event contract.TranscriptEvent) {
	w.upsert(event)
	w.hasNewer = false
	w.evict()
}

// Reconcile replaces a live event's temporary identity with its persisted form
// (data-model §1.1: a live event and its durable replacement never coexist).
func (w *TranscriptWindow) Reconcile(liveID int64, durable contract.TranscriptEvent) bool {
	for i := range w.entries {
		if w.entries[i].ID == liveID {
			w.rawBytes += len(durable.Content) - len(w.entries[i].Content)
			w.entries[i] = durable
			w.resortFrom(i)
			return true
		}
	}
	return false
}

func (w *TranscriptWindow) Entries() []contract.TranscriptEvent { return w.entries }
func (w *TranscriptWindow) HasOlder() bool                      { return w.hasOlder }
func (w *TranscriptWindow) HasNewer() bool                      { return w.hasNewer }
func (w *TranscriptWindow) Len() int                            { return len(w.entries) }
func (w *TranscriptWindow) RawBytes() int                       { return w.rawBytes }
func (w *TranscriptWindow) Anchor() ViewportAnchor              { return w.anchor }
func (w *TranscriptWindow) SetAnchor(a ViewportAnchor)          { w.anchor = a }

func (w *TranscriptWindow) oldestID() int64 {
	if len(w.entries) == 0 {
		return 0
	}
	return w.entries[0].ID
}

func (w *TranscriptWindow) newestID() int64 {
	if len(w.entries) == 0 {
		return 0
	}
	return w.entries[len(w.entries)-1].ID
}

// upsert inserts or replaces an entry, keeping entries ascending by ID.
func (w *TranscriptWindow) upsert(event contract.TranscriptEvent) {
	for i := range w.entries {
		if w.entries[i].ID == event.ID {
			w.rawBytes += len(event.Content) - len(w.entries[i].Content)
			w.entries[i] = event
			return
		}
	}
	w.entries = append(w.entries, event)
	w.rawBytes += len(event.Content)
	w.resortFrom(len(w.entries) - 1)
}

// resortFrom bubbles the entry at index toward its ascending-ID position. Pages
// arrive near-sorted, so this is effectively O(1) amortized.
func (w *TranscriptWindow) resortFrom(index int) {
	for i := index; i > 0 && w.entries[i].ID < w.entries[i-1].ID; i-- {
		w.entries[i], w.entries[i-1] = w.entries[i-1], w.entries[i]
	}
	for i := index; i < len(w.entries)-1 && w.entries[i].ID > w.entries[i+1].ID; i++ {
		w.entries[i], w.entries[i+1] = w.entries[i+1], w.entries[i]
	}
}

// evict drops entries from the oldest end until both budgets hold, always keeping
// at least one entry. Eviction implies older content exists to page back in.
func (w *TranscriptWindow) evict() {
	for (len(w.entries) > w.entryBudget || w.rawBytes > w.byteBudget) && len(w.entries) > 1 {
		dropped := w.entries[0].ID
		w.rawBytes -= len(w.entries[0].Content)
		w.entries = w.entries[1:]
		w.hasOlder = true
		// Keep a non-following anchor valid: if its entry was evicted, re-anchor to
		// the new oldest retained entry so the scroll position never dangles.
		if !w.anchor.FollowOutput && w.anchor.EventID == dropped {
			w.anchor.EventID = w.entries[0].ID
			w.anchor.IntraEntryRow = 0
		}
	}
}
