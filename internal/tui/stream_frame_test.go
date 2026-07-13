package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCompletedRowsStayByteIdenticalDuringStreaming (US2 T027/T032) proves a
// completed transcript row's rendered bytes never change while a later response
// streams — the per-item cache reuses it, so unrelated updates cannot reflow it.
func TestCompletedRowsStayByteIdenticalDuringStreaming(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "completed message alpha stays fixed"})
	m.refreshViewport(true)
	before := m.items[0].cachedBlock
	if before == "" {
		t.Fatal("completed row was not cached")
	}
	// Stream a new response: a draft item is appended and grows.
	m.busy = true
	for i := 0; i < 5; i++ {
		m.appendStream(streamMsg{text: "streaming token "})
		m.refreshViewport(true)
	}
	after := m.items[0].cachedBlock
	if after != before {
		t.Fatalf("completed row changed during streaming:\n before %q\n after  %q", before, after)
	}
}

// TestActiveEntryGrowsWhileStreaming (US2 T027) proves the active draft entry
// accumulates streamed text (its content grows).
func TestActiveEntryGrowsWhileStreaming(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.busy = true
	m.appendStream(streamMsg{text: "first chunk "})
	firstLen := len(m.draft.String())
	m.appendStream(streamMsg{text: "second chunk "})
	if len(m.draft.String()) <= firstLen {
		t.Fatal("active draft did not grow across stream chunks")
	}
	if !strings.Contains(m.draft.String(), "first chunk") || !strings.Contains(m.draft.String(), "second chunk") {
		t.Fatal("active draft lost streamed content")
	}
}

// TestScrollAwayStopsFollowOutput (US2 T027/T033) restates that scrolling up while
// streaming detaches from the bottom (guard also lives in robustness_test.go).
func TestScrollAwayStopsFollowOutput(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(*Model)
	var lines []string
	for i := 0; i < 120; i++ {
		lines = append(lines, "filler line of content")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	m.busy = true
	m.refreshViewport(true)
	renderFrame(m)
	updated, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 8, Button: tea.MouseWheelUp})
	m = updated.(*Model)
	if m.followOutput {
		t.Fatal("scrolling up did not detach follow-output")
	}
}
