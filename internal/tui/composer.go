package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// Composer is the prompt editor (US5 T008). It is a thin, grapheme-aware adapter
// over the Bubbles textarea: by embedding it, every editing method — typing,
// history recall, grapheme navigation, focus/blur, submit, reset, small-paste
// insertion — is promoted unchanged, so existing prompt-editing semantics are
// preserved exactly. Large paste blocks are managed alongside it via the model's
// paste stash (pastes.go); the composer itself stays text-only, which is why the
// atomic paste blocks never disturb caret movement inside ordinary text.
type Composer struct {
	textarea.Model
}

// newComposer builds the composer around a fresh textarea.
func newComposer() Composer {
	return Composer{Model: textarea.New()}
}

// Update advances the underlying editor and returns the Composer so callers keep
// the adapter type. This shadows the embedded textarea.Model.Update, whose return
// type would otherwise be textarea.Model.
func (c Composer) Update(msg tea.Msg) (Composer, tea.Cmd) {
	var cmd tea.Cmd
	c.Model, cmd = c.Model.Update(msg)
	return c, cmd
}

// composerView renders the input content for display (feature 006 T020). For LTR
// content it returns the textarea's own render unchanged. For content containing
// RTL it CUSTOM-RENDERS each logical line — shaped, reordered per run, and
// right-aligned — from the logical Value() (the source of truth), drawing the caret
// at the visual column mapped from the textarea's logical cursor. Editing/movement
// stay logical (the textarea is untouched); only the display is transformed. This
// is why post-processing the textarea's own output is not used (it bakes in the
// cursor column and wrapping — research R2).
func (m *Model) composerView(width int) string {
	value := m.input.Value()
	if !IsRTL(value) {
		return m.input.View()
	}
	if width < 1 {
		width = 1
	}
	mode, align := m.runtime.Settings.RTL.Mode, m.runtime.Settings.RTL.Align
	lines := strings.Split(value, "\n")
	curLine, curCol, focused := m.input.Line(), m.input.Column(), m.input.Focused()
	out := make([]string, len(lines))
	for i, line := range lines {
		dl := renderForDisplay(line, mode, align)
		// Truncate on the side that preserves the logical FIRST word. For a
		// right-aligned RTL line the first word is the visual's rightmost cluster,
		// so an overflowing line is cut from the LEFT (keep the suffix); LTR keeps
		// the prefix as before.
		visual := truncateToWidth(dl.Visual, width)
		leftPad := 0
		if dl.Align == "right" {
			visual = truncateToWidthRight(dl.Visual, width)
			leftPad = max(0, width-displayWidth(visual))
		}
		field := strings.Repeat(" ", leftPad) + visual
		if r := width - displayWidth(field); r > 0 {
			field += strings.Repeat(" ", r)
		}
		if focused && i == curLine {
			field = overlayCursorCell(field, m.composerCaretCol(line, curCol, leftPad, displayWidth(visual)), m.palette.selection)
		}
		out[i] = field
	}
	return strings.Join(out, "\n")
}

// composerCaretCol maps the logical cursor column to a visual column on a shaped,
// right-aligned RTL line. In the reversed visual, the logical prefix [0:col]
// occupies the rightmost columns, so the caret (just before logical char col) sits
// at the LEFT edge of that prefix: leftPad + visualWidth − shapedPrefixWidth. Exact
// for pure-RTL lines (the common Arabic input); best-effort for mixed runs.
func (m *Model) composerCaretCol(logical string, col, leftPad, visualWidth int) int {
	runes := []rune(logical)
	if col > len(runes) {
		col = len(runes)
	}
	if col < 0 {
		col = 0
	}
	prefixW := displayWidth(shapeArabic(string(runes[:col])))
	c := leftPad + visualWidth - prefixW
	if c < 0 {
		c = 0
	}
	return c
}

// overlayCursorCell replaces the grapheme cell at display column col with a styled
// (reverse-video) cursor. When col is at/after the end, a styled space is appended.
func overlayCursorCell(field string, col int, style lipgloss.Style) string {
	if col < 0 {
		return field
	}
	var b strings.Builder
	used, placed := 0, false
	g := uniseg.NewGraphemes(field)
	for g.Next() {
		w := g.Width()
		cl := string(g.Runes())
		if !placed && used == col {
			b.WriteString(style.Render(cl))
			placed = true
		} else {
			b.WriteString(cl)
		}
		used += w
	}
	if !placed {
		b.WriteString(style.Render(" "))
	}
	return b.String()
}
