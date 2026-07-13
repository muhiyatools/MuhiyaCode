package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestResizePreservesState (US2 T028) proves a resize preserves the draft input,
// an open modal, and the transcript items — none are dropped by the reflow.
func TestResizePreservesState(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(*Model)
	m.input.SetValue("a half-typed request")
	m.items = append(m.items, item{kind: "user", content: "earlier turn"})
	m.openChoice("Keep me", "still here?", []contract.QuestionChoice{{Label: "yes"}, {Label: "no"}}, func(int) tea.Cmd { return nil })

	// Resize down then up.
	for _, sz := range [][2]int{{62, 22}, {140, 50}, {80, 24}} {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		m = updated.(*Model)
	}
	if m.input.Value() != "a half-typed request" {
		t.Fatalf("resize lost the draft: %q", m.input.Value())
	}
	if m.modal == nil {
		t.Fatal("resize dropped the open modal")
	}
	if len(m.items) != 1 || m.items[0].content != "earlier turn" {
		t.Fatal("resize disturbed the transcript items")
	}
}

// TestBelowFloorShowsMessage (US2 T028) — below the 60x20 render floor the frame
// is the single too-small message, not a garbled layout.
func TestBelowFloorShowsMessage(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 16})
	m = updated.(*Model)
	content := m.View().Content
	if !strings.Contains(content, "too small") {
		t.Fatal("below-floor frame did not show the too-small message")
	}
}

// TestResizeStoresActualDimensions (US2 T028) — the model tracks the reported
// terminal size (clamped only to a small safety floor), so geometry follows the
// real window.
func TestResizeStoresActualDimensions(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 133, Height: 47})
	m = updated.(*Model)
	if m.width != 133 || m.height != 47 {
		t.Fatalf("stored dimensions %dx%d, want 133x47", m.width, m.height)
	}
}
