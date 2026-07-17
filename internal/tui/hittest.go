package tui

import "github.com/charmbracelet/x/ansi"

// recordCommandTargets adds one click/hover region per visible command row. Screen
// row r within the block (top is the block's Y offset) maps to matches[start+r];
// the trailing count-hint row, when present, is deliberately not recorded because
// it is not a command (mouse-interaction contract §2).
func (m *Model) recordCommandTargets(im *interactionMap, top int) {
	_, start, limit, ok := m.commandLayout()
	if !ok {
		return
	}
	for r := 0; r < limit; r++ {
		im.add(targetCommandItem, start+r, rect{0, top + r, m.width, 1})
	}
}

// recordModalTargets converts the dialog-relative choice rows that renderModal
// recorded (m.modalRows) into absolute screen rectangles, accounting for
// lipgloss.Place centering the dialog inside the body band that begins just below
// the header. rows is the (possibly clamped) rendered dialog split into lines.
func (m *Model) recordModalTargets(im *interactionMap, headerRows, available int, rows []string) {
	if len(m.modalRows) == 0 {
		return
	}
	dialogHeight := len(rows)
	dialogWidth := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r); w > dialogWidth {
			dialogWidth = w
		}
	}
	topPad := max(0, (available-dialogHeight)/2)
	leftPad := max(0, (m.width-dialogWidth)/2)
	screenTop := headerRows + topPad
	for _, span := range m.modalRows {
		if span.row < 0 || span.row >= dialogHeight {
			continue // scrolled/clamped out of the visible dialog
		}
		// The clickable region is the choice's row inside the border (1 col each side).
		im.add(targetModalButton, span.choice, rect{leftPad + 1, screenTop + span.row, max(1, dialogWidth-2), 1})
	}
}
