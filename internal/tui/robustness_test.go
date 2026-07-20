package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestRenderSurvivesExtremeResizes drives the full render path across a barrage of
// terminal sizes — below the floor, at the floor, and very large — with and
// without a modal open, asserting View() never panics and always yields a frame.
// This is the stability guard for the "unstable after resizing" class of reports.
func TestRenderSurvivesExtremeResizes(t *testing.T) {
	sizes := [][2]int{{1, 1}, {40, 14}, {59, 19}, {60, 20}, {61, 21}, {80, 24}, {120, 40}, {300, 100}, {2, 60}, {200, 2}}
	scenarios := []func(m *Model){
		func(m *Model) {}, // plain
		func(m *Model) { // busy with a stream
			m.busy = true
			m.appendStream(streamMsg{text: "streaming assistant text "})
		},
		func(m *Model) { // command palette open
			m.input.SetValue("/")
		},
		func(m *Model) { // a choice modal open
			m.openChoice("Title", strings.Repeat("long message ", 40), []contract.QuestionChoice{
				{Label: "One"}, {Label: "Two", Description: "with a description"}, {Label: "Three"},
			}, func(int) tea.Cmd { return nil })
		},
		func(m *Model) { m.openChoice("Empty", "no choices", nil, func(int) tea.Cmd { return nil }) }, // 0-choice modal
		func(m *Model) { m.openText("Secret", "enter key", true, func(string) tea.Cmd { return nil }) },
	}
	for _, setup := range scenarios {
		for _, sz := range sizes {
			m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			m = updated.(*Model)
			// Some transcript content so the viewport has something to lay out.
			m.items = append(m.items, item{kind: "assistant", content: strings.Repeat("word ", 200)})
			m.items = append(m.items, item{kind: "tool", tool: &toolView{name: "read_file", target: "some/very/long/path/to/a/file.go", state: "ok", output: "12 lines"}})
			m.refreshViewport(true)
			setup(m)
			// Re-render at the same size, then resize again — the sequence that
			// exercises layout() + View() under the scenario's state.
			for _, again := range sizes {
				u2, _ := m.Update(tea.WindowSizeMsg{Width: again[0], Height: again[1]})
				m = u2.(*Model)
				view := m.View()
				if view.Content == "" && again[0] >= 1 && again[1] >= 1 {
					t.Fatalf("empty frame at %dx%d under scenario", again[0], again[1])
				}
			}
		}
	}
}

// TestTranscriptTrimsToBudget (M1) proves a marathon session's in-memory
// transcript is bounded: after adding far more than the item budget, the retained
// slice stays within the cap and a single leading marker records the trim, so the
// per-frame render/join cost and memory can't grow without limit.
func TestTranscriptTrimsToBudget(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	for i := 0; i < maxTranscriptItems+400; i++ {
		m.items = append(m.items, item{kind: "assistant", content: "a turn of content"})
		m.trimTranscript()
	}
	if len(m.items) > maxTranscriptItems+1 { // +1 for the marker
		t.Fatalf("transcript not bounded: %d items", len(m.items))
	}
	if m.items[0].kind != "system" || !strings.Contains(m.items[0].content, "trimmed") {
		t.Fatalf("expected a leading trim marker, got %+v", m.items[0])
	}
	// The most recent content must be retained (we evict the oldest, not the newest).
	if last := m.items[len(m.items)-1]; last.content != "a turn of content" {
		t.Fatalf("newest item lost after trim: %+v", last)
	}
}

// TestTranscriptTrimsByBytes (M1) proves the byte budget also bounds a session of
// a few very large items even when the item count is small.
func TestTranscriptTrimsByBytes(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	big := strings.Repeat("x", 512*1024) // 512 KiB each
	for i := 0; i < 12; i++ {            // ~6 MiB total, over the 3 MiB budget
		m.items = append(m.items, item{kind: "assistant", content: big})
		m.trimTranscript()
	}
	total := 0
	for i := range m.items {
		total += itemBytes(&m.items[i])
	}
	if total > maxTranscriptBytes+len(big) {
		t.Fatalf("byte budget not enforced: %d bytes retained", total)
	}
}

// TestFirstRunTranscriptIsEmptyThenFills (v1.1.0) proves an empty session
// renders nothing (the welcome cue was removed) and that a real turn fills it.
func TestFirstRunTranscriptIsEmptyThenFills(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.refreshViewport(true)
	if strings.TrimSpace(m.transcriptContent) != "" {
		t.Fatalf("empty session rendered content: %q", m.transcriptContent)
	}
	m.items = append(m.items, item{kind: "user", content: "hello"})
	m.refreshViewport(true)
	if !strings.Contains(m.transcriptContent, "hello") {
		t.Fatal("a real turn did not render")
	}
}

// TestScrollUpDuringStreamingSticks (US2 T033) proves that scrolling up while a
// response is streaming stops auto-follow, so incoming chunks no longer yank the
// reader back to the bottom.
func TestScrollUpDuringStreamingSticks(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(*Model)
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "streaming content line")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	m.busy = true
	m.refreshViewport(true)
	renderFrame(m)
	if !m.viewport.AtBottom() {
		t.Fatal("should start pinned to the bottom")
	}
	// User scrolls up over the transcript (through Update so follow intent updates).
	updated, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 8, Button: tea.MouseWheelUp})
	m = updated.(*Model)
	if m.viewport.AtBottom() {
		t.Fatal("wheel up should leave the bottom")
	}
	if m.followOutput {
		t.Fatal("scrolling up mid-stream should stop auto-follow")
	}
	// Streamed chunks keep arriving; they must NOT pull the view back down.
	for i := 0; i < 5; i++ {
		updated, _ = m.Update(streamMsg{text: " more streamed output text that grows the draft"})
		m = updated.(*Model)
	}
	if m.viewport.AtBottom() {
		t.Fatal("streaming yanked the scrolled-up reader back to the bottom (T033 regression)")
	}
}

// TestMouseOnExtremeSizesDoesNotPanic fires clicks/hover/wheel at coordinates that
// may be off-screen after a shrink, across sizes, ensuring the InteractionMap hit
// path is bounds-safe.
func TestMouseOnExtremeSizesDoesNotPanic(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(*Model)
	m.input.SetValue("/")
	for i := 0; i < 50; i++ {
		m.items = append(m.items, item{kind: "assistant", content: "line of content here"})
	}
	m.refreshViewport(true)
	_ = m.View()
	// Shrink hard, then poke mouse events at now-out-of-range coordinates.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = updated.(*Model)
	_ = m.View()
	coords := [][2]int{{0, 0}, {119, 39}, {59, 19}, {200, 200}, {-1, -1}, {30, 10}, {5, 5}}
	for _, c := range coords {
		x, y := c[0], c[1]
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseNone})
		m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
		m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	}
}
