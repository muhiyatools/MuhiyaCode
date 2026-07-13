package tui

// LayoutSnapshot (US1/US2 T006) records the pane geometry that layout() resolved
// for the current frame: the actual terminal size and each pane's row span. It is
// produced by layout() every cycle so the geometry the interaction map and the
// renderer rely on has one computed source rather than being re-derived ad hoc.
type LayoutSnapshot struct {
	Width, Height    int
	HeaderRows       int
	TranscriptTop    int
	TranscriptHeight int
	InputRows        int
	ModeLineRow      int
	BelowFloor       bool
}

// FrameState (US2 T006/T032) tracks which panes changed since the last render so
// unrelated updates can skip re-rendering stable panes. The transcript pane is the
// expensive one; the others are cheap chrome re-rendered every frame regardless.
type FrameState struct {
	TranscriptDirty bool
	Generation      int
}

// markDirty records that the transcript pane must be re-rendered this frame.
func (f *FrameState) markDirty() { f.TranscriptDirty = true }

// clear resets the per-frame dirty flags after a render and advances the frame
// generation so a stale interaction map can be detected.
func (f *FrameState) clear() {
	f.TranscriptDirty = false
	f.Generation++
}

// snapshotLayout captures the current pane geometry from the model's resolved
// dimensions and viewport. Called at the end of layout().
func (m *Model) snapshotLayout() {
	below := m.width < m.theme.FloorWidth || m.height < m.theme.FloorHeight
	m.layoutSnapshot = LayoutSnapshot{
		Width:            m.width,
		Height:           m.height,
		HeaderRows:       lineCount(m.renderHeader()),
		TranscriptTop:    lineCount(m.renderHeader()),
		TranscriptHeight: m.viewport.Height(),
		InputRows:        m.input.Height() + 2,
		ModeLineRow:      m.height - 1,
		BelowFloor:       below,
	}
}
