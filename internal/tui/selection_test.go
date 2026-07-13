package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// dragSelect renders a frame, finds the screen row where needle is drawn, and
// simulates a left press→drag→release across that row, returning the model.
func dragSelect(t *testing.T, m *Model, needle string) *Model {
	t.Helper()
	content := m.View().Content
	y := screenRowContaining(content, needle)
	if y < 0 {
		t.Fatalf("%q was not drawn in the transcript", needle)
	}
	updated, _ := m.Update(tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	m = updated.(*Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: 70, Y: y, Button: tea.MouseLeft})
	m = updated.(*Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 70, Y: y, Button: tea.MouseLeft})
	m = updated.(*Model)
	return m
}

// TestClickPlacesInputCaret (US4 T053/T059) proves clicking inside the composer
// moves the caret to the clicked position — typing then inserts there.
func TestClickPlacesInputCaret(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	m.input.SetValue("hello\nworld")
	_ = m.View() // build the InteractionMap so the input rect exists
	var box rect
	found := false
	for _, tg := range m.hits.targets {
		if tg.kind == targetInput {
			box, found = tg.rect, true
		}
	}
	if !found {
		t.Fatal("no input target in the interaction map")
	}
	// Click on the first row (box.y+1), column 2 (content starts at box.x+2).
	updated, _ = m.Update(tea.MouseClickMsg{X: box.x + 4, Y: box.y + 1, Button: tea.MouseLeft})
	m = updated.(*Model)
	if m.input.Line() != 0 {
		t.Fatalf("caret landed on line %d, want 0", m.input.Line())
	}
	// Type a marker; it must insert at column 2 of line 0.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = updated.(*Model)
	if !strings.HasPrefix(m.input.Value(), "heXllo") {
		t.Fatalf("caret not placed at (0,2); value = %q", m.input.Value())
	}
}

// TestClickCaretCJKBoundary (US4 T053/T059) proves the cell→rune mapping places
// the caret at a grapheme boundary after double-width CJK glyphs.
func TestClickCaretCJKBoundary(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.input.Focus()
	m.input.SetValue("你好ABC") // 你 好 are 2 cells each → 4 cells, then A B C
	_ = m.View()
	var box rect
	for _, tg := range m.hits.targets {
		if tg.kind == targetInput {
			box = tg.rect
		}
	}
	// Click at cell offset 4 (just past 你好), which is rune index 2.
	updated, _ = m.Update(tea.MouseClickMsg{X: box.x + 2 + 4, Y: box.y + 1, Button: tea.MouseLeft})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = updated.(*Model)
	if !strings.HasPrefix(m.input.Value(), "你好X") {
		t.Fatalf("caret not at the CJK boundary; value = %q", m.input.Value())
	}
}

// TestTranscriptDragSelectionExtractsText (US4 T058) proves a left drag over the
// transcript builds a selection whose plain text matches what was drawn.
func TestTranscriptDragSelectionExtractsText(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "SELECTME the quick brown fox"})
	m.refreshViewport(true)
	m = dragSelect(t, m, "SELECTME")
	if !m.sel.active {
		t.Fatal("drag did not create an active selection")
	}
	text := m.selectionText()
	if !strings.Contains(text, "SELECTME the quick brown fox") {
		t.Fatalf("selection text missing content: %q", text)
	}
}

// TestSelectionCopyReturnsClipboardCommand (US4 T058) proves Ctrl+C on an active
// selection produces an OSC52 clipboard command and clears the selection.
func TestSelectionCopyReturnsClipboardCommand(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "COPYME payload text"})
	m.refreshViewport(true)
	m = dragSelect(t, m, "COPYME")
	if !m.sel.active {
		t.Fatal("no selection to copy")
	}
	cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl, Text: ""})
	if cmd == nil {
		t.Fatal("Ctrl+C on a selection returned no clipboard command")
	}
	if m.sel.active {
		t.Fatal("selection should clear after copy")
	}
}

// TestPlainClickFocusesInputNoSelection (US4 T058) proves a press+release with no
// drag does not create a selection (it falls back to focusing the composer).
func TestPlainClickFocusesInputNoSelection(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "just some text"})
	m.refreshViewport(true)
	content := m.View().Content
	y := screenRowContaining(content, "just some text")
	if y < 0 {
		t.Fatal("content not drawn")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	m = updated.(*Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 4, Y: y, Button: tea.MouseLeft})
	m = updated.(*Model)
	if m.sel.active || m.sel.dragging {
		t.Fatal("a plain click must not create a selection")
	}
}

// TestEscClearsSelection (US4 T058) proves Esc dismisses an active selection.
func TestEscClearsSelection(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "ESCME selection target"})
	m.refreshViewport(true)
	m = dragSelect(t, m, "ESCME")
	if !m.sel.active {
		t.Fatal("no selection to clear")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.sel.active {
		t.Fatal("Esc did not clear the selection")
	}
}

// TestSelectionHighlightVisible (US4 T058) proves the selected span is rendered
// with the selection style (reverse video), visible without color.
func TestSelectionHighlightVisible(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.items = append(m.items, item{kind: "assistant", content: "HILITE selection styling"})
	m.refreshViewport(true)
	m = dragSelect(t, m, "HILITE")
	before := m.viewport.View()
	after := m.highlightSelection(before, m.viewport.YOffset())
	if before == after {
		t.Fatal("selection produced no visible highlight change")
	}
}
