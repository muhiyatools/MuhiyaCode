package contract

import "time"

// Transcript paging value types (feature 005 US1). These let the TUI hold only a
// bounded window of the durable transcript in memory and page the rest from
// SQLite on demand, so per-event work stays independent of total history length.
// Keyset (cursor) pagination only — offset pagination is prohibited by contract.

// TranscriptEvent is one durable transcript entry with its stable SQLite ID.
type TranscriptEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	// Target is the tool call's display target (file/command/query), persisted so
	// a paged-in tool row names WHAT it acted on as it did live. Empty for
	// non-tool events and pre-column sessions.
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"createdAt"`
}

// PageDirection selects which keyset slice to load.
type PageDirection string

const (
	PageInitialTail PageDirection = "initial-tail" // newest N entries
	PageBefore      PageDirection = "before"       // entries with id < Cursor (scroll up)
	PageAfter       PageDirection = "after"        // entries with id > Cursor (scroll down)
)

// TranscriptPageRequest is a keyset page request. Generation ties a result to the
// window that asked for it: a stale-generation result (after a session switch) is
// discarded without touching the frame.
type TranscriptPageRequest struct {
	SessionID  string
	Generation uint64
	Direction  PageDirection
	Cursor     int64 // exclusive stable event ID; ignored for initial-tail
	Limit      int   // max entries; <=0 uses a default
	ByteBudget int   // max total content bytes; <=0 uses the 8 MiB default
}

// TranscriptPage is a keyset-paginated slice returned by the state layer, always
// in ascending ID order.
type TranscriptPage struct {
	SessionID  string            `json:"sessionId"`
	Generation uint64            `json:"generation"`
	Entries    []TranscriptEvent `json:"entries"`
	OldestID   int64             `json:"oldestId"`
	NewestID   int64             `json:"newestId"`
	HasOlder   bool              `json:"hasOlder"`
	HasNewer   bool              `json:"hasNewer"`
	RawBytes   int               `json:"rawBytes"`
}
