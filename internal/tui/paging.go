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
		return item{kind: "tool", eventID: e.ID, tool: &toolView{name: e.Kind, target: e.Target, state: state, output: e.Content, summary: summarizeTool(e.Content), started: e.CreatedAt}}, true
	}
	return item{}, false
}

// pageItems converts a page's entries to render items.
//
// LEGACY: sessions created before the unified-session change still hold
// role="agent" rows (run_start / run_summary) in their transcript. They simply
// do not convert — transcriptEventToItem has no "agent" case — so they are
// skipped silently. That is the intended behavior: the runs they described were
// real, but there is no agent card left to render them into, and an old session
// must still reopen without error.
func (m *Model) pageItems(page contract.TranscriptPage) []item {
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
	if items := m.pageItems(page); len(items) > 0 {
		m.items = items
		m.refreshViewport(true)
	}
}

// applyOlderPage prepends an older page and arms the anchor compensation so the
// viewport keeps showing the same content after the re-render.
func (m *Model) applyOlderPage(page contract.TranscriptPage) {
	m.loadingOlder = false
	m.hasOlderEvents = page.HasOlder
	older := m.pageItems(page)
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

// Transcript render-window budgets (M1). These bound the in-memory items the TUI
// re-renders and joins each frame; the durable transcript in the session DB is
// never touched. The bounds are deliberately generous so a typical session never
// trims — only marathon sessions with thousands of turns/tools hit them.
const (
	maxTranscriptItems = 1500
	maxTranscriptBytes = 3 << 20 // ~3 MiB of item + tool-output content
)

// itemBytes is the cheap size measure used for the byte budget: the visible
// content plus any tool output (which the collapsed view hides but the render
// still carries).
func itemBytes(it *item) int {
	n := len(it.content)
	if it.tool != nil {
		n += len(it.tool.output)
	}
	return n
}

// trimTranscript bounds the retained transcript to the render-window budgets by
// evicting the oldest items, leaving a single leading marker so the trim is
// visible rather than silent. It is a no-op until a session actually exceeds the
// budgets. Front eviction is render-safe: items are indexed only positionally
// during rendering, while tool/agent lookups use maps keyed by name/ID.
func (m *Model) trimTranscript() {
	total := 0
	for i := range m.items {
		total += itemBytes(&m.items[i])
	}
	if len(m.items) <= maxTranscriptItems && total <= maxTranscriptBytes {
		return
	}
	drop := 0
	for drop < len(m.items) && (len(m.items)-drop > maxTranscriptItems || total > maxTranscriptBytes) {
		total -= itemBytes(&m.items[drop])
		drop++
	}
	if drop <= 0 {
		return
	}
	marker := item{kind: "system", content: "⋯ earlier messages trimmed to keep the interface responsive — the full transcript is saved in this session's log ⋯"}
	trimmed := make([]item, 0, len(m.items)-drop+1)
	trimmed = append(trimmed, marker)
	trimmed = append(trimmed, m.items[drop:]...)
	m.items = trimmed
	// US4 T058: front-eviction shifts content rows, so any selection is now stale.
	m.clearSelection()
}

func (m *Model) loadEvents(events []contract.Event) {
	// Only user, assistant, and tool turns belong in the transcript. Other
	// system/setup events surface through the transient notice line.
	//
	// LEGACY: an old session's events include role="agent" rows from the removed
	// subagent system. The switch has no case for them and no default, so they
	// are skipped silently — an old transcript reopens cleanly, just without the
	// agent cards there is no longer any machinery to render.
	for _, event := range events {
		switch event.Role {
		case "user":
			m.items = append(m.items, item{kind: "user", content: event.Content})
		case "assistant":
			m.items = append(m.items, item{kind: "assistant", content: event.Content})
		case "tool":
			m.items = append(m.items, item{kind: "tool", tool: &toolView{name: event.Type, target: event.Target, state: map[bool]string{true: "fail", false: "ok"}[isFailure(event.Content)], output: event.Content, summary: summarizeTool(event.Content), started: event.CreatedAt}})
		}
	}
}

// ensureAgentView and rehydrateAgent rebuilt a finished subagent's card from its
// persisted run_start / run_summary events on resume. Both are gone with the
// cards they built; the events they read are now skipped (see loadEvents).
