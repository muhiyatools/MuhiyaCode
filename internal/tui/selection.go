package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"
)

// Transcript text selection + OSC52 copy (US4 T058). The selection lives in
// content coordinates (full-transcript rows/columns, independent of scroll). It is
// highlighted at View() time over only the visible lines — so it never re-renders
// the cached transcript — and copied as ANSI-free plain text via OSC52.

// transcriptRect returns the transcript viewport's screen rectangle from the
// current frame's interaction map, so mouse coordinates map to exactly the
// geometry that was drawn.
func (m *Model) transcriptRect() (rect, bool) {
	if m.hits == nil {
		return rect{}, false
	}
	for _, t := range m.hits.targets {
		if t.kind == targetTranscript {
			return t.rect, true
		}
	}
	return rect{}, false
}

// contentPosAt converts a screen coordinate to a transcript content (row, col),
// returning ok=false when the point is outside the transcript viewport.
func (m *Model) contentPosAt(x, y int) (row, col int, ok bool) {
	r, ok := m.transcriptRect()
	if !ok || !r.contains(x, y) {
		return 0, 0, false
	}
	row = (y - r.y) + m.viewport.YOffset()
	col = max(0, x-r.x)
	return row, col, true
}

func (m *Model) beginSelection(row, col int) {
	m.sel = textSelection{dragging: true, anchorRow: row, anchorCol: col, endRow: row, endCol: col}
}

func (m *Model) extendSelection(row, col int) {
	if !m.sel.dragging {
		return
	}
	m.sel.endRow, m.sel.endCol = row, col
	m.sel.active = m.sel.anchorRow != row || m.sel.anchorCol != col
}

func (m *Model) clearSelection() { m.sel = textSelection{} }

// selectionText returns the ANSI-free plain text of the current selection, ready
// for the clipboard, or "" when there is no active selection.
func (m *Model) selectionText() string {
	if !m.sel.active {
		return ""
	}
	lines := strings.Split(m.transcriptContent, "\n")
	r0, c0, r1, c1 := m.sel.ordered()
	if r0 < 0 {
		r0, c0 = 0, 0
	}
	if r1 >= len(lines) {
		r1, c1 = len(lines)-1, 1<<30
	}
	if r0 > r1 || r0 >= len(lines) {
		return ""
	}
	out := make([]string, 0, r1-r0+1)
	for row := r0; row <= r1; row++ {
		plain := []rune(ansi.Strip(lines[row]))
		start, end := 0, len(plain)
		if row == r0 {
			start = min(c0, len(plain))
		}
		if row == r1 {
			end = min(c1, len(plain))
		}
		if start > end {
			start = end
		}
		out = append(out, recoverLogical(strings.TrimRight(string(plain[start:end]), " ")))
	}
	return strings.Join(out, "\n")
}

// recoverLogical maps a displayed (possibly shaped/reordered) line back to logical
// Unicode for the clipboard (feature 006 T028, FR-015). Pure-LTR and off/native-mode
// lines carry no Arabic presentation forms and are returned unchanged. A shaped
// line is un-shaped (presentation forms → base letters via NFKC); a predominantly-
// RTL line was fully reversed for display, so it is also un-reversed. Exact for
// pure-LTR and pure-Arabic lines (the common copy cases); best-effort for a
// partial selection across mixed-direction runs.
func recoverLogical(s string) string {
	if !hasPresentationForms(s) {
		return s
	}
	if dominantRTL(s) {
		return norm.NFKC.String(reverseGraphemes(s))
	}
	return norm.NFKC.String(s)
}

// hasPresentationForms reports whether s contains Arabic presentation-form code
// points (the display-only shaped glyphs that must never reach the clipboard).
func hasPresentationForms(s string) bool {
	for _, r := range s {
		if (r >= 0xFB50 && r <= 0xFDFF) || (r >= 0xFE70 && r <= 0xFEFF) {
			return true
		}
	}
	return false
}

// copySelection returns the OSC52 command to copy the selection and true when
// there was non-empty text to copy.
func (m *Model) copySelection() (tea.Cmd, bool) {
	text := m.selectionText()
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	return tea.SetClipboard(text), true
}

// highlightSelection overlays the selection style on the visible viewport lines
// only. yOffset is the viewport scroll offset, so visible line i is content row
// yOffset+i. It is O(visible lines) and never touches the cached transcript render.
func (m *Model) highlightSelection(vp string, yOffset int) string {
	if !m.sel.active {
		return vp
	}
	r0, c0, r1, c1 := m.sel.ordered()
	lines := strings.Split(vp, "\n")
	for i := range lines {
		contentRow := yOffset + i
		if contentRow < r0 || contentRow > r1 {
			continue
		}
		plain := []rune(ansi.Strip(lines[i]))
		start, end := 0, len(plain)
		if contentRow == r0 {
			start = min(c0, len(plain))
		}
		if contentRow == r1 {
			end = min(c1, len(plain))
		}
		if start >= end {
			continue
		}
		lines[i] = string(plain[:start]) + m.palette.selection.Render(string(plain[start:end])) + string(plain[end:])
	}
	return strings.Join(lines, "\n")
}
