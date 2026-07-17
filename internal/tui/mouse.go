package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Mouse interaction (005 US4). The TUI builds an immutable InteractionMap during
// every View() render so a click's screen coordinates always resolve against the
// exact frame the user is looking at (mouse-interaction contract §1). Every mouse
// action dispatches through the SAME code path as its keyboard equivalent, so the
// two can never drift (contract §4). This layer is presentation-only: it moves the
// selection and runs existing commands, and never touches the orchestrator request
// path or the prefix cache.

// targetKind classifies an interaction region. The integer order encodes the
// contract's precedence (§1): when two regions overlap, the higher value wins
// (modal button > command item > input > chip > transcript).
type targetKind int

const (
	targetNone targetKind = iota
	targetTranscript
	targetAgentChip
	targetToolChip
	targetInput
	targetPasteBar
	targetCommandItem
	targetModalButton
)

// rect is a zero-based, half-open screen rectangle: it covers x in [x, x+w) and
// y in [y, y+h).
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// interactionTarget is one clickable/hoverable region in the current frame. id's
// meaning depends on kind: a command match index or a modal choice index. Tool and
// agent chips instead carry a direct reference (tool pointer / agent run ID) so a
// click acts on the exact chip even if item indices shift.
type interactionTarget struct {
	kind    targetKind
	id      int
	rect    rect
	tool    *toolView
	agentID string
}

// interactionMap is the hit map for one rendered frame. It is rebuilt on every
// View() so its rectangles always match what is on screen; generation lets a
// caller reason about staleness across a resize (contract §1).
type interactionMap struct {
	generation int
	targets    []interactionTarget
}

// add appends a target, ignoring degenerate (zero-area) rectangles so an empty
// pane never captures clicks.
func (im *interactionMap) add(kind targetKind, id int, r rect) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	im.targets = append(im.targets, interactionTarget{kind: kind, id: id, rect: r})
}

// addChip appends a transcript tool/agent chip target carrying its direct
// reference so a click acts on the exact chip regardless of index shifts.
func (im *interactionMap) addChip(kind targetKind, r rect, tool *toolView, agentID string) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	im.targets = append(im.targets, interactionTarget{kind: kind, rect: r, tool: tool, agentID: agentID})
}

// hit returns the highest-priority target whose rectangle contains (x, y).
func (im *interactionMap) hit(x, y int) (interactionTarget, bool) {
	best := interactionTarget{kind: targetNone}
	found := false
	for _, t := range im.targets {
		if t.rect.contains(x, y) && t.kind >= best.kind {
			best, found = t, true
		}
	}
	return best, found
}

// handleMouseClick routes a left-button press to the target under the pointer,
// dispatching to the same action the keyboard uses (contract §1, §4). Only
// MouseClickMsg (the press) invokes an action; release/repeat never re-fire it,
// which is what guarantees single invocation.
func (m *Model) handleMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.hits == nil {
		return nil
	}
	if t, ok := m.hits.hit(msg.X, msg.Y); ok {
		switch t.kind {
		case targetModalButton:
			m.clearSelection()
			return m.activateModalChoice(t.id)
		case targetCommandItem:
			m.clearSelection()
			return m.activateCommandIndex(t.id)
		case targetToolChip:
			// Toggle just this tool's detail and re-render the transcript.
			m.clearSelection()
			if t.tool != nil {
				t.tool.expanded = !t.tool.expanded
				m.needsTranscriptRefresh = true
			}
			return nil
		case targetAgentChip:
			// Open this subagent's detail view — the same state Tab/Alt+number reach.
			m.clearSelection()
			if t.agentID != "" {
				m.viewAgent = t.agentID
				m.needsTranscriptRefresh = true
			}
			return nil
		case targetTranscript:
			// Begin a potential text selection at the press point. A release without
			// any drag falls back to focusing the composer (handleMouseRelease).
			if row, col, ok := m.contentPosAt(msg.X, msg.Y); ok {
				m.beginSelection(row, col)
			}
			return nil
		case targetInput:
			// Focus the composer and place the caret at the nearest grapheme boundary
			// to the click (US4 T059). Clicks past a line end clamp to that line's end.
			m.clearSelection()
			m.placeInputCaret(msg.X, msg.Y, t.rect)
			return m.input.Focus()
		case targetPasteBar:
			// US5: open the pasted-blocks manager to inspect or remove a paste.
			m.clearSelection()
			m.openPasteManager()
			return nil
		}
		return nil
	}
	// No target under the pointer. A click on a plain info modal (no choices, no
	// text input) dismisses it like an OK button; a text/secret modal keeps focus
	// and never submits from a stray padding click (contract §2 modal buttons).
	if m.modal != nil && len(m.modal.choices) == 0 && !m.modal.input {
		m.closeModal(-1)
	}
	return nil
}

// handleMouseMotion implements hover: an unpressed move highlights the row under
// the pointer — a command/modal row moves the selection, and a tool/agent chip
// gets a "clickable" underline affordance. It returns true when something visible
// changed, so idle same-target motion stays cheap. A pressed move (drag) extends
// the text selection instead.
func (m *Model) handleMouseMotion(msg tea.MouseMotionMsg) (changed bool) {
	// A left-button drag extends an in-progress text selection (US4 T058). The
	// highlight is applied at View() time, so no transcript re-render is needed.
	if msg.Button == tea.MouseLeft && m.sel.dragging {
		if row, col, ok := m.contentPosAt(msg.X, msg.Y); ok {
			m.extendSelection(row, col)
		}
		return true
	}
	if msg.Button != tea.MouseNone || m.hits == nil {
		return false
	}
	t, ok := m.hits.hit(msg.X, msg.Y)
	if !ok {
		return m.clearHover()
	}
	switch t.kind {
	case targetCommandItem:
		changed = m.clearHover()
		matches := m.commandMatches()
		if t.id >= 0 && t.id < len(matches) && m.commandIndex != t.id {
			m.commandIndex = t.id
			changed = true
		}
		return changed
	case targetModalButton:
		changed = m.clearHover()
		if m.modal != nil && t.id >= 0 && t.id < len(m.modal.choices) && m.modal.selected != t.id {
			m.modal.selected = t.id
			changed = true
		}
		return changed
	case targetToolChip:
		return m.setHover(t.tool, "")
	case targetAgentChip:
		return m.setHover(nil, t.agentID)
	default:
		return m.clearHover()
	}
}

// setHover marks the tool or agent chip under the pointer as hovered (the source
// of the clickable underline affordance) and clears any previously hovered row.
// It returns true and requests a transcript refresh only when the hovered row
// actually changed, so moving within one chip stays free.
func (m *Model) setHover(tool *toolView, agentID string) bool {
	changed := false
	if m.hoveredTool != tool {
		if m.hoveredTool != nil {
			m.hoveredTool.hovered = false
		}
		if tool != nil {
			tool.hovered = true
		}
		m.hoveredTool = tool
		changed = true
	}
	if m.hoveredAgent != agentID {
		if prev := m.agentByID[m.hoveredAgent]; prev != nil {
			prev.hovered = false
		}
		if next := m.agentByID[agentID]; next != nil {
			next.hovered = true
		}
		m.hoveredAgent = agentID
		changed = true
	}
	if changed {
		m.needsTranscriptRefresh = true
	}
	return changed
}

// clearHover drops any hover affordance. It returns true (and asks for a refresh)
// only when a row was actually hovered.
func (m *Model) clearHover() bool {
	if m.hoveredTool == nil && m.hoveredAgent == "" {
		return false
	}
	return m.setHover(nil, "")
}

// handleMouseRelease finalizes a drag: a real selection is kept for copy, while a
// press+release with no drag is treated as a plain click that focuses the composer
// (US4 T058). This is why the composer focus moved from press to release for the
// transcript.
func (m *Model) handleMouseRelease(msg tea.MouseReleaseMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || !m.sel.dragging {
		return nil
	}
	m.sel.dragging = false
	if !m.sel.active {
		m.clearSelection()
		return m.input.Focus()
	}
	return nil
}

// handleMouseWheel routes a wheel detent to the scrollable surface under the
// pointer (contract §2). A modal or the command menu takes the wheel to move its
// selection window; everywhere else the wheel scrolls the transcript — including
// over tool/agent chips, which have a higher hit priority and previously swallowed
// the wheel, making scrolling feel stuck wherever the pointer sat on a tool row.
func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) {
	up := msg.Button == tea.MouseWheelUp
	down := msg.Button == tea.MouseWheelDown
	if !up && !down {
		return
	}
	// A modal owns the wheel: move its selection window.
	if m.modal != nil {
		if n := len(m.modal.choices); n > 0 {
			if up {
				m.modal.selected = max(0, m.modal.selected-1)
			} else {
				m.modal.selected = min(n-1, m.modal.selected+1)
			}
		}
		return
	}
	if m.hits == nil {
		return
	}
	t, ok := m.hits.hit(msg.X, msg.Y)
	if !ok {
		return // over the header or an unmapped gap: the wheel does nothing
	}
	switch t.kind {
	case targetCommandItem:
		matches := m.commandMatches()
		if up {
			m.commandIndex = max(0, m.commandIndex-1)
		} else {
			m.commandIndex = min(len(matches)-1, m.commandIndex+1)
		}
	case targetTranscript, targetToolChip, targetAgentChip:
		// Scroll the transcript. Chips are transcript rows too, so including them is
		// the fix — before, a chip's higher hit priority swallowed the wheel and made
		// scrolling feel stuck wherever the pointer sat on a tool/agent row. Three
		// rows per detent is the terminal-standard step; the viewport clamps at the
		// ends so this is stable. Chrome (input, paste bar, mode line) does not scroll.
		const wheelStep = 3
		if up {
			m.viewport.ScrollUp(wheelStep)
		} else {
			m.viewport.ScrollDown(wheelStep)
		}
	}
}

// placeInputCaret moves the composer caret to the click point (US4 T059). The
// input is a bordered, single-padding box, so content begins one cell in and one
// row down from the box origin. The caret is positioned by resetting to the start
// and stepping down the clicked visual row, then setting the column — the textarea
// clamps the column to the line, giving the nearest valid grapheme boundary. This
// is exact for the common unwrapped composer and best-effort (nearest) when the
// input has wrapped/scrolled beyond its visible rows.
func (m *Model) placeInputCaret(x, y int, box rect) {
	contentX := box.x + 2 // border (1) + horizontal padding (1)
	contentY := box.y + 1 // border top
	line := max(0, y-contentY)
	cell := max(0, x-contentX)
	m.input.MoveToBegin()
	for i := 0; i < line; i++ {
		m.input.CursorDown()
	}
	// Convert the clicked cell offset to a rune column so a double-width CJK/emoji
	// glyph maps to the nearest grapheme boundary, not a raw cell index. Exact for
	// the common unwrapped composer; SetCursorColumn clamps otherwise, so a wrapped
	// or short line simply snaps to its nearest valid boundary.
	col := cell
	if logical := strings.Split(m.input.Value(), "\n"); line < len(logical) {
		col = cellToRuneColumn(logical[line], cell)
	}
	m.input.SetCursorColumn(col)
}

// cellToRuneColumn returns the rune index in s at the given terminal-cell offset,
// snapping to the boundary before a glyph the offset lands inside. Measured by
// grapheme cluster (feature 010 T036: the one uniseg width family), not
// per-rune go-runewidth, so a combining-mark cluster's cell offset resolves to
// the index right after its whole cluster rather than mid-cluster.
func cellToRuneColumn(s string, cell int) int {
	width, runes := 0, 0
	clusters, widths := graphemeClusters(s)
	for i, w := range widths {
		if width+w > cell {
			break
		}
		width += w
		runes += len([]rune(clusters[i]))
	}
	return runes
}

// activateCommandIndex runs the command at match index, mirroring the keyboard
// Enter path (resolveSlash → runSlash). Typed arguments are preserved only when
// the click targets the exact command already typed as the first token.
func (m *Model) activateCommandIndex(index int) tea.Cmd {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return nil
	}
	index = max(0, min(index, len(matches)-1))
	chosen := matches[index].name
	typed := strings.TrimSpace(m.input.Value())
	value := chosen
	if fields := strings.Fields(typed); len(fields) > 0 && strings.EqualFold(fields[0], chosen) {
		value = typed
	}
	m.input.Reset()
	m.commandIndex = 0
	return m.runSlash(value)
}

// activateModalChoice selects and confirms a modal choice, mirroring the
// keyboard "arrow-to + Enter" path (actions.go handleModalKey). A multi-select
// choice toggles like Space and stays open; a single-select choice fires its
// callback and closes exactly as Enter does.
func (m *Model) activateModalChoice(index int) tea.Cmd {
	modal := m.modal
	if modal == nil || index < 0 || index >= len(modal.choices) {
		return nil
	}
	modal.selected = index
	if modal.multi {
		if modal.checked == nil {
			modal.checked = make(map[int]bool)
		}
		modal.checked[index] = !modal.checked[index]
		return nil
	}
	callback := modal.onSelect
	m.closeModal(index)
	if callback != nil {
		return callback(index)
	}
	return nil
}
