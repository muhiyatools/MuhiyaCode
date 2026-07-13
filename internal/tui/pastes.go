package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// Large-paste blocks (US5). A large paste is stashed and shown in the composer as
// a compact `[#pasteN NN lines]` placeholder (the atomic block); this file adds the
// visible "pasted blocks" bar and the inspect/remove UI on top of that stash, so a
// block can be reviewed or dropped without disturbing the surrounding text. The
// exact bytes are always reconstructed at submit (expandPastes).

// activePastes returns the paste IDs whose placeholder is currently present in the
// composer, in first-seen order.
func (m *Model) activePastes() []int {
	if len(m.pastes) == 0 {
		return nil
	}
	var ids []int
	seen := map[int]bool{}
	for _, match := range pastePlaceholderRE.FindAllStringSubmatch(m.input.Value(), -1) {
		id, _ := strconv.Atoi(match[1])
		if _, ok := m.pastes[id]; ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// renderPasteBar draws the compact one-line summary of active paste blocks above
// the composer, or "" when there are none.
func (m *Model) renderPasteBar() string {
	ids := m.activePastes()
	if len(ids) == 0 {
		return ""
	}
	parts := []string{" " + m.palette.faint.Render(m.glyphs.bullet+" pasted")}
	for _, id := range ids {
		parts = append(parts, m.palette.brandSoft.Render(fmt.Sprintf("[paste%d · %d lines]", id, logicalLineCount(m.pastes[id]))))
	}
	parts = append(parts, m.palette.faint.Render("· click to view or remove"))
	return fitLine(strings.Join(parts, " "), m.width)
}

// openPasteManager lists the active paste blocks so one can be inspected or
// removed. It is reached by clicking the paste bar or the `/paste` command.
func (m *Model) openPasteManager() {
	ids := m.activePastes()
	if len(ids) == 0 {
		m.notify("No pasted blocks in the current message.")
		return
	}
	choices := make([]contract.QuestionChoice, 0, len(ids)+1)
	for _, id := range ids {
		choices = append(choices, contract.QuestionChoice{
			Label:       fmt.Sprintf("paste%d · %d lines", id, logicalLineCount(m.pastes[id])),
			Description: firstPasteLine(m.pastes[id]),
		})
	}
	choices = append(choices, contract.QuestionChoice{Label: "Close"})
	captured := append([]int(nil), ids...)
	m.openChoice("Pasted blocks", "Select a block to inspect or remove.", choices, func(index int) tea.Cmd {
		if index < 0 || index >= len(captured) {
			return nil
		}
		m.openPasteInspect(captured[index])
		return nil
	})
}

// openPasteInspect shows a bounded preview of one paste block with a Remove action.
func (m *Model) openPasteInspect(id int) {
	raw, ok := m.pastes[id]
	if !ok {
		return
	}
	preview := pastePreview(raw, 40)
	choices := []contract.QuestionChoice{
		{Label: "Remove this block", Description: "Delete it from the message"},
		{Label: "Keep", Recommended: true},
	}
	m.openChoice(fmt.Sprintf("paste%d · %d lines", id, logicalLineCount(raw)), preview, choices, func(index int) tea.Cmd {
		if index == 0 {
			m.removePaste(id)
			m.notify(fmt.Sprintf("Removed paste%d.", id))
		}
		return nil
	})
}

// removePaste drops the stash and removes the block's placeholder from the composer.
func (m *Model) removePaste(id int) {
	if _, ok := m.pastes[id]; !ok {
		return
	}
	re := regexp.MustCompile(`\[#paste` + strconv.Itoa(id) + ` \d+ lines\]`)
	m.input.SetValue(strings.TrimSpace(re.ReplaceAllString(m.input.Value(), "")))
	delete(m.pastes, id)
	if len(m.pastes) == 0 {
		m.pasteSeq = 0
	}
}

// firstPasteLine returns a short single-line summary of a paste's first content.
func firstPasteLine(raw string) string {
	first := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	return oneLine(first, 60)
}

// pastePreview renders up to maxLines of a paste, control-safe, for the inspect
// dialog. A truncated paste shows a trailing marker.
func pastePreview(raw string, maxLines int) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	for i, line := range lines {
		lines[i] = strings.Map(func(r rune) rune {
			if r == '\t' {
				return ' '
			}
			if r < 0x20 {
				return '·'
			}
			return r
		}, line)
	}
	out := strings.Join(lines, "\n")
	if truncated {
		out += "\n…"
	}
	return out
}
