package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderModal builds the dialog inside an explicit row budget derived from the
// terminal height, so the box always fits: on short terminals it drops the
// vertical padding, blank separators, and description row, and it truncates
// the message before it can squeeze out the choices or the hint.
func (m *Model) renderModal(budgetRows int) string {
	modal := m.modal
	m.modalRows = m.modalRows[:0] // rebuilt below so mouse targets match this render
	width := min(max(38, m.width-12), 78)
	// The frame adds 6 columns (border + horizontal padding), so the rendered
	// box is width+6 wide. The max(38, …) floor can push that past a narrow
	// terminal; clamp so the box always fits the screen.
	width = min(width, max(10, m.width-6))
	inner := width - 6
	// Budget is the rows View actually reserves for the dialog (m.height minus the
	// header and mode line), NOT m.height-4 — otherwise the box overflows the space
	// View allocates and its bottom border/padding is sheared off (H-3).
	budget := max(6, budgetRows)
	pad := 0
	if budget >= 16 {
		pad = 1
	}
	body := max(4, budget-2-2*pad) // rows inside the border
	sep := 0
	if body >= 10 {
		sep = 1
	}
	blank := func(lines []string) []string {
		if sep == 1 {
			return append(lines, "")
		}
		return lines
	}

	// Reserve rows for the title, hint, separators, and a minimum of visible
	// content so a long message can never squeeze them out.
	minContent := 0
	switch {
	case modal.input:
		minContent = 1
	case len(modal.choices) > 0:
		// Reserve a row for EVERY choice (plus the selected description row in
		// roomy layouts) so a long message shrinks before it can scroll a choice
		// out of view — an invisible choice also loses its clickable rect.
		minContent = len(modal.choices) + sep
	}
	message := wrapMessage(modal.message, inner)
	// Info modals (no input, no choices) carry only a title and a one-line hint, so
	// they get the largest message budget. When the content is tall — the /context
	// essentials card is 13 lines — they also drop the decorative title/hint blank
	// separators to reclaim two rows, so the last rows (Models) stay visible at
	// 80×24 instead of being truncated (H-1).
	infoModal := !modal.input && len(modal.choices) == 0
	titleSep, hintSep := sep, sep
	limit := max(1, body-2-3*sep-minContent)
	if infoModal {
		limit = max(1, body-2-2*sep)
		if len(message) > limit && len(message) <= body-2 {
			titleSep, hintSep = 0, 0
			limit = body - 2
		}
	}
	if len(message) > limit {
		message = message[:limit]
		message[limit-1] += " …"
	}

	lines := []string{m.palette.brand.Render(modal.title)}
	if titleSep == 1 {
		lines = append(lines, "")
	}
	for _, line := range message {
		lines = append(lines, m.palette.text.Render(m.rtl(line)))
	}
	switch {
	case modal.input:
		value := modal.value
		if modal.secret {
			value = strings.Repeat("•", len([]rune(value)))
		}
		if value == "" {
			value = m.palette.faint.Render("Type here…")
		}
		lines = blank(lines)
		lines = append(lines, m.palette.surface2.Padding(0, 1).Width(inner).Render(value))
		lines = blank(lines)
		lines = append(lines, m.palette.faint.Render("Enter confirms · Esc cancels"))
	case len(modal.choices) == 0:
		if hintSep == 1 {
			lines = append(lines, "")
		}
		lines = append(lines, m.palette.faint.Render("Enter or Esc closes"))
	default:
		lines = blank(lines)
		// Scroll a window around the selection. The window never exceeds the
		// number of choices, so one- and two-choice dialogs show exactly what
		// they have.
		selected := max(0, min(modal.selected, len(modal.choices)-1))
		descRows := sep // show the highlighted choice's description only in roomy layouts
		remaining := body - len(lines) - sep - 1
		visible := min(len(modal.choices), max(2, remaining-descRows))
		start := max(0, min(selected-visible/2, len(modal.choices)-visible))
		for index := start; index < start+visible; index++ {
			choice := modal.choices[index]
			numTag := fmt.Sprintf("%d. ", index+1)
			marker := "  " + numTag
			if modal.multi {
				marker = "[" + fmt.Sprintf("%d", index+1) + "] "
				if modal.checked[index] {
					marker = "[×] "
				}
			}
			style := m.palette.text
			if index == selected {
				style, marker = m.palette.brandSoft, "› "+marker
			}
			label := style.Render(marker + m.rtl(oneLineEllipsis(choice.Label, width-12)))
			if choice.Recommended {
				label += " " + m.palette.brand.Render("recommended")
			}
			// Record this choice's dialog-relative row (top border + vertical padding
			// + inner line index) so recordModalTargets can place a click region on it.
			m.modalRows = append(m.modalRows, modalRowSpan{choice: index, row: 1 + pad + len(lines)})
			lines = append(lines, label)
			if choice.Description != "" && index == selected && descRows > 0 {
				lines = append(lines, m.palette.faint.Render("    "+oneLineEllipsis(choice.Description, width-10)))
			}
		}
		hint := "1-9/↑/↓ choose · Enter confirms · Esc cancels"
		if modal.multi {
			hint = "1-9 toggle · Space toggles · Enter confirms"
		}
		if len(modal.choices) > visible {
			hint = fmt.Sprintf("%d/%d · ", selected+1, len(modal.choices)) + hint
		}
		lines = blank(lines)
		lines = append(lines, m.palette.faint.Render(hint))
	}
	// Modal frame from the palette/glyph tables: accent focus border over the
	// overlay background (no hardcoded hex — visual-system.md §5).
	frame := lipgloss.NewStyle().Border(m.glyphs.border).BorderForeground(m.palette.focus.GetForeground()).Padding(pad, 2).Width(width)
	if bg := m.palette.surface2.GetBackground(); bg != nil {
		frame = frame.Background(bg)
	}
	return frame.Render(strings.Join(lines, "\n"))
}

// wrapMessage wraps a possibly multi-line message to the given width,
// preserving intentional line breaks.
func wrapMessage(message string, width int) []string {
	var result []string
	for _, part := range strings.Split(message, "\n") {
		result = append(result, wrapPlain(part, width)...)
	}
	return result
}
