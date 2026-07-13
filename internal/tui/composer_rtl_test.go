package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestComposerRTLShapedRightAlignedLogicalValue (006 T018/US2): typed Arabic is
// custom-rendered (shaped, reordered, right-aligned) while the logical Value()
// stays the clean source of truth.
func TestComposerRTLShapedRightAlignedLogicalValue(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.input.Focus()
	m.input.SetValue(corpusPlain) // "مرحبا بالعالم"

	view := m.composerView(40)

	// Value() (what is submitted) must stay logical and unchanged.
	if m.input.Value() != corpusPlain {
		t.Fatalf("composer Value() changed: %q", m.input.Value())
	}
	first := stripANSI(strings.Split(view, "\n")[0])
	// Transformed for display (not the raw logical order).
	if strings.Contains(first, corpusPlain) {
		t.Fatalf("RTL input not shaped/reordered: %q", first)
	}
	// Right-aligned in the 40-wide field (leading spaces).
	if !strings.HasPrefix(first, "  ") {
		t.Fatalf("RTL input not right-aligned: %q", first)
	}
}

// TestComposerLTRUnchanged (006 T018/US2): pure-LTR input uses the textarea's own
// render unchanged.
func TestComposerLTRUnchanged(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.input.SetValue("hello world")
	if m.composerView(40) != m.input.View() {
		t.Fatal("LTR input must use the textarea's own render unchanged")
	}
}

// TestComposerRTLCaretMovesWithCursor (006 T020): the caret's visual column shifts
// as the logical cursor moves (end vs start of an RTL line).
func TestComposerRTLCaretMovesWithCursor(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m.input.Focus()
	m.input.SetValue("مرحبا")
	visW := displayWidth(renderForDisplay("مرحبا", "visual", "auto").Visual)
	// cursor at end → caret at the left edge of the text (leftPad).
	end := m.composerCaretCol("مرحبا", 5, 35, visW)
	// cursor at start → caret at the right edge (leftPad + visW).
	start := m.composerCaretCol("مرحبا", 0, 35, visW)
	if !(end < start) {
		t.Fatalf("RTL caret should move right as cursor moves to start: end=%d start=%d", end, start)
	}
}
