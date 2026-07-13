package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// Scroll-back transcript paging (US1 T020/T023). The TUI keeps a bounded window of
// the durable transcript in m.items and loads older pages from SQLite on demand
// when the user scrolls to the top. m.items stays the single render source, so the
// mouse hit-testing, selection, chip spans, and caret placement are untouched; an
// older page is prepended and the viewport is re-anchored to the same content.

const transcriptOverscanRows = 4 // load older when within this many rows of the top

// transcriptEventToItem converts a durable transcript event into a render item,
// mirroring loadEvents but carrying the stable SQLite id for the keyset cursor.
func transcriptEventToItem(e contract.TranscriptEvent) (item, bool) {
	switch e.Role {
	case "user":
		return item{kind: "user", content: e.Content, eventID: e.ID}, true
	case "assistant":
		return item{kind: "assistant", content: e.Content, eventID: e.ID}, true
	case "tool":
		state := "ok"
		if isFailure(e.Content) {
			state = "fail"
		}
		return item{kind: "tool", eventID: e.ID, tool: &toolView{name: e.Kind, state: state, output: e.Content, summary: summarizeTool(e.Content), started: e.CreatedAt}}, true
	}
	return item{}, false
}

// pageItems converts a page's entries to render items (dropping non-transcript rows).
func pageItems(page contract.TranscriptPage) []item {
	items := make([]item, 0, len(page.Entries))
	for _, e := range page.Entries {
		if it, ok := transcriptEventToItem(e); ok {
			items = append(items, it)
		}
	}
	return items
}

// loadInitialPageCmd loads the newest page from SQLite to establish the keyset
// cursor and the has-older flag (US1 T020). It is a no-op when paging is disabled.
func (m *Model) loadInitialPageCmd() tea.Cmd {
	if m.actions.TranscriptPage == nil || m.runtime.Session.ID == "" || m.initialPaged {
		return nil
	}
	action, ctx := m.actions.TranscriptPage, m.ctx
	req := contract.TranscriptPageRequest{SessionID: m.runtime.Session.ID, Generation: m.pageGen, Direction: contract.PageInitialTail}
	return func() tea.Msg {
		page, err := action(ctx, req)
		return transcriptPageMsg{page: page, initial: true, err: err}
	}
}

// loadOlderPageCmd loads the page before the current oldest item (US1 T020). It
// guards against duplicate in-flight loads and a missing cursor.
func (m *Model) loadOlderPageCmd() tea.Cmd {
	if m.actions.TranscriptPage == nil || m.loadingOlder || !m.hasOlderEvents || m.oldestEventID <= 0 {
		return nil
	}
	m.loadingOlder = true
	action, ctx := m.actions.TranscriptPage, m.ctx
	req := contract.TranscriptPageRequest{SessionID: m.runtime.Session.ID, Generation: m.pageGen, Direction: contract.PageBefore, Cursor: m.oldestEventID}
	return func() tea.Msg {
		page, err := action(ctx, req)
		return transcriptPageMsg{page: page, initial: false, err: err}
	}
}

// maybeLoadOlder returns a page-load command when the viewport is near the top and
// older durable history remains unloaded.
func (m *Model) maybeLoadOlder() tea.Cmd {
	if m.loading || m.modal != nil || !m.hasOlderEvents || m.loadingOlder {
		return nil
	}
	if m.viewport.YOffset() > transcriptOverscanRows {
		return nil
	}
	return m.loadOlderPageCmd()
}

// applyPage handles a keyset page result (US1 T020). A stale-generation result
// (after a session switch) or an error is discarded without touching the frame.
func (m *Model) applyPage(msg transcriptPageMsg) {
	if msg.err != nil || msg.page.Generation != m.pageGen {
		m.loadingOlder = false
		return
	}
	if msg.initial {
		m.applyInitialPage(msg.page)
		return
	}
	m.applyOlderPage(msg.page)
}

// applyInitialPage adopts the newest page as the render window and records the
// paging cursor. It replaces the recent window (identical content, now id-tagged)
// only before any live turn, so it never disturbs an active session.
func (m *Model) applyInitialPage(page contract.TranscriptPage) {
	m.initialPaged = true
	m.oldestEventID = page.OldestID
	m.hasOlderEvents = page.HasOlder
	if m.busy || len(m.items) > len(page.Entries) {
		return // a turn already advanced the window; keep it, just record the cursor
	}
	if items := pageItems(page); len(items) > 0 {
		m.items = items
		m.refreshViewport(true)
	}
}

// applyOlderPage prepends an older page and arms the anchor compensation so the
// viewport keeps showing the same content after the re-render.
func (m *Model) applyOlderPage(page contract.TranscriptPage) {
	m.loadingOlder = false
	m.hasOlderEvents = page.HasOlder
	older := pageItems(page)
	if len(older) == 0 {
		return
	}
	m.oldestEventID = page.OldestID
	m.pendingPrepend = pagePrepend{active: true, beforeLines: lineCount(m.transcriptContent), beforeY: m.viewport.YOffset()}
	m.items = append(older, m.items...)
	m.clearSelection() // prepending shifts every content row
}

// resetPaging clears paging state on a session switch and bumps the generation so
// any in-flight page result is discarded (US1 T020 cancellation).
func (m *Model) resetPaging() {
	m.pageGen++
	m.oldestEventID = 0
	m.hasOlderEvents = false
	m.loadingOlder = false
	m.initialPaged = false
	m.pendingPrepend = pagePrepend{}
}
