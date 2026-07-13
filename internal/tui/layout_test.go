package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestLayoutSnapshotReflectsPanes (US1/US2 T006) proves layout() records the
// resolved pane geometry: the actual size, a non-empty transcript band, and the
// mode-line row.
func TestLayoutSnapshotReflectsPanes(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(*Model)
	ls := m.layoutSnapshot
	if ls.Width != 100 || ls.Height != 40 {
		t.Fatalf("snapshot size %dx%d, want 100x40", ls.Width, ls.Height)
	}
	if ls.HeaderRows != 4 {
		t.Fatalf("header rows = %d, want 4", ls.HeaderRows)
	}
	if ls.TranscriptHeight <= 0 {
		t.Fatal("snapshot has an empty transcript band")
	}
	if ls.ModeLineRow != 39 {
		t.Fatalf("mode-line row = %d, want 39", ls.ModeLineRow)
	}
	if ls.BelowFloor {
		t.Fatal("100x40 wrongly flagged below floor")
	}
}

// TestFrameStateTracksDirtyAndGeneration (US2 T006/T032) proves the frame state
// marks the transcript dirty on a content update and advances its generation each
// frame (so a stale interaction map is detectable).
func TestFrameStateTracksDirtyAndGeneration(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	gen0 := m.frameState.Generation
	// A content update advances the generation and clears dirty afterward.
	updated, _ = m.Update(streamMsg{text: "hello"})
	m = updated.(*Model)
	if m.frameState.Generation <= gen0 {
		t.Fatal("frame generation did not advance across updates")
	}
	if m.frameState.TranscriptDirty {
		t.Fatal("transcript dirty flag was not cleared after the frame")
	}
}
