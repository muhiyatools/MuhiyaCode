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
		HeaderRows:       lineCount(m.chromeHeader()),
		TranscriptTop:    lineCount(m.chromeHeader()),
		TranscriptHeight: m.viewport.Height(),
		InputRows:        m.input.Height() + 2,
		ModeLineRow:      m.height - 1,
		BelowFloor:       below,
	}
}

// layout recomputes the transcript viewport size from the height actually
// consumed by every other pane. Counting rendered lines (rather than
// re-deriving them) guarantees the panes always sum to the terminal height, so
// the command palette or plan can never push the input off-screen.
func (m *Model) layout() {
	m.input.SetWidth(max(10, m.width-10))
	// Set the viewport width before any chrome renders so the activity tail (whose
	// width derives from the viewport) is measured correctly and the memoized chrome
	// layout() sizes against is byte-identical to what View() draws (A5).
	m.viewport.SetWidth(m.width)
	if m.modal != nil {
		m.viewport.SetHeight(max(3, m.height-lineCount(m.chromeHeader())-lineCount(m.chromeModeLine())))
		m.snapshotLayout()
		return
	}
	// +1 is the blank spacer View() renders directly above the input (Experience
	// Overhaul A2 spacing). It is unconditional in the non-modal path, so it is part
	// of the base height budget here.
	used := lineCount(m.chromeHeader()) + (m.input.Height() + 2) + lineCount(m.chromeModeLine()) + 1
	if activity := m.chromeActivity(); activity != "" {
		used += lineCount(activity)
	}
	if todos := m.chromeTodos(); todos != "" {
		used += lineCount(todos)
	}
	if commands := m.renderCommands(); commands != "" {
		used += lineCount(commands)
	}
	if flash := m.renderNotice(); flash != "" {
		used += lineCount(flash)
	}
	if bar := m.renderPasteBar(); bar != "" {
		used += lineCount(bar)
	}
	m.viewport.SetHeight(max(3, m.height-used))
	m.snapshotLayout() // US1/US2 T006: record the resolved pane geometry
}
