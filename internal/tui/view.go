package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m *Model) View() tea.View {
	// 003 (T017/D13): below the render floor, show one clean message instead of a
	// collapsed, garbled layout. 60×20 is the floor; 80×24 is the design baseline.
	if m.width < m.theme.FloorWidth || m.height < m.theme.FloorHeight {
		msg := m.palette.warning.Render(fmt.Sprintf("terminal too small %s MuhiyaCode needs at least %dx%d", m.glyphs.bullet, m.theme.FloorWidth, m.theme.FloorHeight))
		view := tea.NewView(lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, msg))
		view.AltScreen = true
		m.hits = &interactionMap{generation: m.frameState.Generation} // nothing is clickable below the floor
		return view
	}
	// Experience Overhaul A5: invalidate the chrome memo at the start of View() as
	// well as Update(), so a frame always reflects the current model state even if
	// state was mutated without an intervening Update (the render funcs are cheap;
	// this just dedupes the several accessor calls made within one View()).
	m.chrome.valid = false
	header := m.chromeHeader()
	// US4: rebuild the InteractionMap for THIS frame so click/hover coordinates
	// resolve against exactly what is drawn (mouse-interaction contract §1). Pane Y
	// offsets are implicit in the join order, so accumulate lineCount as panes are
	// assembled — the map and the screen are produced in one pass and cannot drift.
	im := &interactionMap{generation: m.frameState.Generation}
	var content string
	if m.modal != nil {
		hint := m.chromeModeLine()
		available := max(4, m.height-lineCount(header)-lineCount(hint))
		dialog := m.renderModal(available) // also records m.modalRows (dialog-relative choice rows)
		// renderModal sizes itself to the terminal, but clamp as a hard
		// invariant: an oversized dialog is cut rather than allowed to push
		// the rest of the interface off-screen.
		rows := strings.Split(dialog, "\n")
		// Hard horizontal invariant to match the vertical one below: no dialog
		// row may exceed the terminal width, or the overflow wraps and shears
		// the box (and the click map) apart on narrow terminals.
		for i, row := range rows {
			rows[i] = fitLine(row, m.width)
		}
		if len(rows) > available {
			rows = rows[:available]
		}
		dialog = strings.Join(rows, "\n")
		m.recordModalTargets(im, lineCount(header), available, rows)
		body := lipgloss.Place(m.width, available, lipgloss.Center, lipgloss.Center, dialog)
		content = header + "\n" + body + "\n" + hint
	} else {
		parts := []string{header}
		y := lineCount(header)
		vp := m.viewport.View()
		vpHeight := lineCount(vp)
		// US4 T058: overlay the text-selection highlight on the visible lines only.
		vp = m.highlightSelection(vp, m.viewport.YOffset())
		im.add(targetTranscript, 0, rect{0, y, m.width, vpHeight})
		// US4: project each visible transcript chip's header row to a screen rect so
		// clicking a tool chip toggles it. Content rows are converted with the live
		// viewport offset and clipped to the visible transcript band, so a
		// scrolled-away chip has no hit region.
		yOffset := m.viewport.YOffset()
		for _, chip := range m.transcriptChips {
			screenRow := y + chip.row - yOffset
			if screenRow < y || screenRow >= y+vpHeight {
				continue
			}
			im.addChip(targetToolChip, rect{0, screenRow, m.width, 1}, chip.tool)
		}
		parts = append(parts, vp)
		y += vpHeight
		if commands := m.renderCommands(); commands != "" {
			m.recordCommandTargets(im, y)
			parts = append(parts, commands)
			y += lineCount(commands)
		}
		if flash := m.renderNotice(); flash != "" {
			parts = append(parts, flash)
			y += lineCount(flash)
		}
		if activity := m.chromeActivity(); activity != "" {
			parts = append(parts, activity)
			y += lineCount(activity)
		}
		if todos := m.chromeTodos(); todos != "" {
			parts = append(parts, todos)
			y += lineCount(todos)
		}
		if bar := m.renderPasteBar(); bar != "" {
			im.add(targetPasteBar, 0, rect{0, y, m.width, lineCount(bar)})
			parts = append(parts, bar)
			y += lineCount(bar)
		}
		// One blank line above the input gives the floating status/to-do zone room
		// to breathe (Experience Overhaul A2 spacing). It is an explicit, height-
		// accounted spacer (mirrored in layout()) so the interaction map stays exact.
		parts = append(parts, "")
		y++
		input := m.renderInput()
		im.add(targetInput, 0, rect{0, y, m.width, lineCount(input)})
		parts = append(parts, input, m.chromeModeLine())
		content = strings.Join(parts, "\n")
	}
	m.hits = im
	view := tea.NewView(content)
	view.AltScreen = true
	// US4: all-motion mouse mode so unpressed movement is reported and hover can
	// highlight command/modal rows. (Cell-motion reports only press/release/drag,
	// so hover is impossible under it.) Capturing the mouse means click-drag goes to
	// the app, so native terminal text selection uses the terminal's modifier
	// override (Shift+drag in most terminals); keyboard scrolling remains. Hover only
	// re-renders cheap chrome on an actual target change, so idle movement is light.
	view.MouseMode = tea.MouseModeAllMotion
	view.WindowTitle = "MuhiyaCode · " + filepath.Base(m.runtime.Session.WorkspacePath)
	// 003 (D4): do not force a full-screen background/foreground. Respecting the
	// terminal's native background removes the Warp alt-screen repaint-seam class
	// of artifacts and lets the palette work on light terminals; styled content
	// carries its own foreground, so unstyled whitespace simply uses the
	// terminal default.
	return view
}
