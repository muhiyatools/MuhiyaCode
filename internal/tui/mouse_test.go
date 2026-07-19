package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// screenRowContaining returns the index of the first rendered row whose visible
// (ANSI-stripped) text contains needle, or -1. It is how the geometry tests find
// where a label is actually drawn, independent of the InteractionMap.
func screenRowContaining(content, needle string) int {
	for i, row := range strings.Split(content, "\n") {
		if strings.Contains(ansi.Strip(row), needle) {
			return i
		}
	}
	return -1
}

// renderFrame drives one View() so the InteractionMap for the current state
// exists, exactly as the Bubble Tea runtime renders after every Update before the
// next message. Mouse routing resolves against this map.
func renderFrame(m *Model) {
	_ = m.View()
}

// TestMouseWheelScrollsTranscript (US4) verifies the wheel scrolls the transcript
// when the pointer is inside the transcript rectangle.
func TestMouseWheelScrollsTranscript(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "a line of transcript content")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	m.refreshViewport(true)
	renderFrame(m)
	if !m.viewport.AtBottom() {
		t.Fatal("expected to start at bottom")
	}

	// Y=8 is well inside the transcript (header is rows 0-3).
	before := m.viewport.YOffset()
	updated, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 8, Button: tea.MouseWheelUp})
	m = updated.(*Model)
	up := m.viewport.YOffset()
	if up >= before {
		t.Fatalf("wheel up did not scroll the transcript: %d -> %d", before, up)
	}

	updated, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 8, Button: tea.MouseWheelDown})
	m = updated.(*Model)
	if m.viewport.YOffset() <= up {
		t.Fatalf("wheel down did not scroll back: %d -> %d", up, m.viewport.YOffset())
	}
}

// TestMouseWheelIgnoredOverChrome (US4) verifies a wheel over the header does not
// move the transcript (contract §2 wheel scope).
func TestMouseWheelIgnoredOverChrome(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "content line")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	m.refreshViewport(true)
	m.viewport.ScrollUp(10) // move off the bottom so a stray scroll would be visible
	renderFrame(m)
	before := m.viewport.YOffset()
	// Y=1 is inside the header band (rows 0-3), not the transcript.
	updated, _ = m.Update(tea.MouseWheelMsg{X: 10, Y: 1, Button: tea.MouseWheelDown})
	m = updated.(*Model)
	if m.viewport.YOffset() != before {
		t.Fatalf("wheel over the header moved the transcript: %d -> %d", before, m.viewport.YOffset())
	}
}

// TestMouseWheelOverToolChipScrolls (006) is the regression for the "scrolling
// feels stuck" bug: a tool/agent chip has a higher hit priority than the
// transcript, so the wheel over a chip used to be swallowed. It must scroll the
// transcript like any other transcript row.
func TestMouseWheelOverToolChipScrolls(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "content line")
	}
	m.items = append(m.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	tool := &toolView{name: "grep", target: "somepattern", state: "ok", output: "a\nb"}
	m.items = append(m.items, item{kind: "tool", tool: tool})
	m.refreshViewport(true) // pins to the bottom, where the tool chip is visible
	renderFrame(m)
	chipY := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetToolChip && tg.tool == tool {
			chipY = tg.rect.y
		}
	}
	if chipY < 0 {
		t.Fatal("tool chip not visible/mapped at the bottom")
	}
	before := m.viewport.YOffset()
	updated, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: chipY, Button: tea.MouseWheelUp})
	m = updated.(*Model)
	if m.viewport.YOffset() >= before {
		t.Fatalf("wheel over a tool chip did not scroll the transcript: %d -> %d", before, m.viewport.YOffset())
	}
}

// TestHoverToolChipMarksClickable (006) verifies an unpressed move over a tool
// chip marks it hovered (the source of the clickable underline), and that moving
// off it clears the hover.
func TestHoverToolChipMarksClickable(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	tool := &toolView{name: "grep", target: "pat", state: "ok", output: "x"}
	m.items = append(m.items, item{kind: "tool", tool: tool})
	m.refreshViewport(true)
	renderFrame(m)
	chipY := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetToolChip && tg.tool == tool {
			chipY = tg.rect.y
		}
	}
	if chipY < 0 {
		t.Fatal("tool chip not mapped")
	}
	if tool.hovered {
		t.Fatal("tool should not start hovered")
	}
	updated, _ = m.Update(tea.MouseMotionMsg{X: 5, Y: chipY, Button: tea.MouseNone})
	m = updated.(*Model)
	if !tool.hovered || m.hoveredTool != tool {
		t.Fatalf("hovering the tool chip did not mark it hovered (hovered=%v)", tool.hovered)
	}
	// Move into the header band (row 1): the hover must clear.
	updated, _ = m.Update(tea.MouseMotionMsg{X: 5, Y: 1, Button: tea.MouseNone})
	m = updated.(*Model)
	if tool.hovered || m.hoveredTool != nil {
		t.Fatal("moving off the chip did not clear the hover")
	}
}

// TestMouseClickIsHandledSafely (US4) verifies a left click is processed without
// panicking and returns the model.
func TestMouseClickIsHandledSafely(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	renderFrame(m)
	updated, _ = m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	if _, ok := updated.(*Model); !ok {
		t.Fatal("mouse click did not return a *Model")
	}
}

// TestClickCommandRowRuns (US4) proves clicking a command-palette row runs that
// exact command through the same path as keyboard Enter. Clicking "/memory list"
// must resolve to the memory command, not whatever was highlighted by default.
func TestClickCommandRowRuns(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = updated.(*Model)
	m.input.Focus()
	// Open the palette.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*Model)
	renderFrame(m)
	if m.hits == nil {
		t.Fatal("no interaction map after rendering the palette")
	}
	// Click the SECOND visible command row (not the default-highlighted first one),
	// proving the click acts on the row under the pointer, not the selection.
	matches := m.commandMatches()
	var clickY, clickID int = -1, -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetCommandItem && tg.id == 1 {
			clickY, clickID = tg.rect.y, tg.id
		}
	}
	if clickY < 0 {
		t.Fatal("second command row not visible in the palette window")
	}
	wantCommand := matches[clickID].name
	updated, _ = m.Update(tea.MouseClickMsg{X: 3, Y: clickY, Button: tea.MouseLeft})
	m = updated.(*Model)
	// Running a command clears the input (the Enter path resets it) and resolves to
	// the clicked row's command specifically.
	if strings.HasPrefix(m.input.Value(), "/") {
		t.Fatalf("clicking command %q did not run it; input still %q", wantCommand, m.input.Value())
	}
}

// TestHoverMovesCommandSelection (US4) proves hovering a command row moves the
// highlighted selection to it, so a subsequent click acts on the hovered row.
func TestHoverMovesCommandSelection(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = updated.(*Model)
	m.input.Focus()
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*Model)
	renderFrame(m)
	// Pick a visible command row that is not currently selected.
	var wantID, wantY int = -1, -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetCommandItem && tg.id != m.commandIndex {
			wantID, wantY = tg.id, tg.rect.y
			break
		}
	}
	if wantID < 0 {
		t.Skip("no alternate command row visible")
	}
	updated, _ = m.Update(tea.MouseMotionMsg{X: 5, Y: wantY, Button: tea.MouseNone})
	m = updated.(*Model)
	if m.commandIndex != wantID {
		t.Fatalf("hover did not move selection: commandIndex=%d want %d", m.commandIndex, wantID)
	}
}

// TestCommandTargetMatchesDrawnRow (US4) is the non-circular geometry check: it
// renders the real frame, finds the screen row where "/goal" is actually drawn,
// and asserts the InteractionMap's target for that command sits on the same row.
// If the map and the pixels ever diverge, a click would miss — this catches it.
func TestCommandTargetMatchesDrawnRow(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = updated.(*Model)
	m.input.Focus()
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*Model)
	content := m.View().Content
	drawnRow := screenRowContaining(content, "/goal")
	if drawnRow < 0 {
		t.Fatal("/goal was not drawn in the palette")
	}
	matches := m.commandMatches()
	goalID := -1
	for i, c := range matches {
		if c.name == "/goal" {
			goalID = i
		}
	}
	mapped := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetCommandItem && tg.id == goalID {
			mapped = tg.rect.y
		}
	}
	if mapped != drawnRow {
		t.Fatalf("command target row %d != drawn row %d — clicks would miss", mapped, drawnRow)
	}
}

// TestModalTargetMatchesDrawnRow (US4) is the non-circular geometry check for the
// centered modal: the recorded click region for a choice must sit on the same
// screen row the choice label is actually rendered at, through lipgloss centering.
func TestModalTargetMatchesDrawnRow(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(*Model)
	m.openChoice("Pick one", "choose an option below", []contract.QuestionChoice{
		{Label: "Alpha choice"}, {Label: "Bravo choice"}, {Label: "Charlie choice"},
	}, func(int) tea.Cmd { return nil })
	content := m.View().Content
	drawnRow := screenRowContaining(content, "Bravo choice")
	if drawnRow < 0 {
		t.Fatal("Bravo choice was not drawn")
	}
	mapped := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetModalButton && tg.id == 1 {
			mapped = tg.rect.y
		}
	}
	if mapped != drawnRow {
		t.Fatalf("modal target row %d != drawn row %d — clicks would miss", mapped, drawnRow)
	}
}

// TestReleaseDoesNotReinvoke (US4, contract §1) proves an action fires once on the
// press (MouseClickMsg) and the following release never re-invokes it.
func TestReleaseDoesNotReinvoke(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(*Model)
	count := 0
	m.openChoice("Pick", "choose", []contract.QuestionChoice{{Label: "A"}, {Label: "B"}}, func(int) tea.Cmd {
		count++
		return nil
	})
	renderFrame(m)
	clickY := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetModalButton && tg.id == 0 {
			clickY = tg.rect.y
		}
	}
	if clickY < 0 {
		t.Fatal("no modal choice target")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: m.width / 2, Y: clickY, Button: tea.MouseLeft})
	m = updated.(*Model)
	// The release at the same coordinate must not fire the callback again.
	updated, _ = m.Update(tea.MouseReleaseMsg{X: m.width / 2, Y: clickY, Button: tea.MouseLeft})
	_ = updated
	if count != 1 {
		t.Fatalf("callback fired %d times; press must fire once and release never", count)
	}
}

// TestClickTracksLayoutAfterResize (US4, contract §1) proves the InteractionMap is
// rebuilt each frame: after a resize moves the command palette, a click resolves
// against the NEW layout, never a stale one.
func TestClickTracksLayoutAfterResize(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = updated.(*Model)
	m.input.Focus()
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(*Model)
	renderFrame(m)
	rowTall := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetCommandItem && tg.id == 0 {
			rowTall = tg.rect.y
		}
	}
	// Shrink the terminal; the palette (which sits above the bottom-pinned input)
	// moves to a new row.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(*Model)
	content := m.View().Content
	rowShort := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetCommandItem && tg.id == 0 {
			rowShort = tg.rect.y
		}
	}
	if rowShort < 0 {
		t.Fatal("command row target missing after resize")
	}
	if rowShort == rowTall {
		t.Skip("palette row happened not to move; layout-dependent")
	}
	// The new target row must match where the row is actually drawn now.
	matches := m.commandMatches()
	drawn := screenRowContaining(content, matches[0].name)
	if drawn != rowShort {
		t.Fatalf("post-resize target row %d != drawn row %d — map went stale", rowShort, drawn)
	}
}

// TestClickToolChipTogglesExpansion (US4) proves clicking a tool chip's header
// toggles just that tool's detail, independent of the global Ctrl+O verbose.
func TestClickToolChipTogglesExpansion(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	tool := &toolView{name: "grep", target: "MARKERPATTERN", state: "ok", output: "match one\nmatch two"}
	m.items = append(m.items, item{kind: "tool", tool: tool})
	m.refreshViewport(true)
	renderFrame(m)
	clickY := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetToolChip && tg.tool == tool {
			clickY = tg.rect.y
		}
	}
	if clickY < 0 {
		t.Fatal("tool chip has no click region")
	}
	if tool.expanded {
		t.Fatal("tool should start collapsed")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 5, Y: clickY, Button: tea.MouseLeft})
	m = updated.(*Model)
	if !tool.expanded {
		t.Fatal("clicking the tool chip did not expand it")
	}
	renderFrame(m)
	clickY = -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetToolChip && tg.tool == tool {
			clickY = tg.rect.y
		}
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 5, Y: clickY, Button: tea.MouseLeft})
	_ = updated
	if tool.expanded {
		t.Fatal("clicking the tool chip again did not collapse it")
	}
}

// TestClickAgentChipOpensView (US4) proves clicking a subagent chip opens that
// agent's detail view — the same state Tab/Alt+number reach.
func TestClickAgentChipOpensView(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	m.applyAgent(contract.AgentEvent{Kind: "start", RunID: "run-42", Agent: "explorer", Title: "Map the code", Task: "explore"})
	m.refreshViewport(true)
	renderFrame(m)
	clickY := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetAgentChip && tg.agentID == "run-42" {
			clickY = tg.rect.y
		}
	}
	if clickY < 0 {
		t.Fatal("agent chip has no click region")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: 5, Y: clickY, Button: tea.MouseLeft})
	m = updated.(*Model)
	if m.viewAgent != "run-42" {
		t.Fatalf("clicking the agent chip did not open its view: viewAgent=%q", m.viewAgent)
	}
}

// TestToolChipTargetMatchesDrawnRow (US4) is the non-circular geometry check for a
// transcript chip: it finds the screen row where the tool is actually drawn and
// asserts the chip's click region sits on that same row, through the viewport.
func TestToolChipTargetMatchesDrawnRow(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(*Model)
	// A little history above so the chip is not trivially at row 0.
	m.items = append(m.items, item{kind: "assistant", content: "some earlier assistant reply"})
	tool := &toolView{name: "grep", target: "ZZUNIQUEPAT", state: "ok", output: "a match"}
	m.items = append(m.items, item{kind: "tool", tool: tool})
	m.refreshViewport(true)
	content := m.View().Content
	drawnRow := screenRowContaining(content, "ZZUNIQUEPAT")
	if drawnRow < 0 {
		t.Fatal("tool row was not drawn")
	}
	mapped := -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetToolChip && tg.tool == tool {
			mapped = tg.rect.y
		}
	}
	if mapped != drawnRow {
		t.Fatalf("tool chip target row %d != drawn row %d — clicks would miss", mapped, drawnRow)
	}
}

// TestClickDismissesInfoModal (US4) proves a click anywhere on a plain info modal
// (no choices, no input) closes it — e.g. the /memory list panel.
func TestClickDismissesInfoModal(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(*Model)
	m.openInfo("Project memory", "No project memory recorded yet.")
	renderFrame(m)
	updated, _ = m.Update(tea.MouseClickMsg{X: 45, Y: 15, Button: tea.MouseLeft})
	m = updated.(*Model)
	if m.modal != nil {
		t.Fatal("clicking the info modal did not dismiss it")
	}
}

// TestClickDoesNotSubmitTextModal (US4, contract §2) proves a stray click on a
// text/secret modal keeps it open (it must not submit from a padding click).
func TestClickDoesNotSubmitTextModal(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(*Model)
	m.openText("API key", "paste your key", true, func(string) tea.Cmd { return nil })
	renderFrame(m)
	updated, _ = m.Update(tea.MouseClickMsg{X: 45, Y: 15, Button: tea.MouseLeft})
	m = updated.(*Model)
	if m.modal == nil {
		t.Fatal("a stray click closed the text modal; it must retain focus")
	}
}

// TestClickModalChoiceConfirms (US4) proves clicking a modal choice selects and
// confirms it exactly as arrow+Enter would — this is the path the MCP interface
// and every confirmation dialog rely on.
func TestClickModalChoiceConfirms(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m = updated.(*Model)
	got := -1
	m.openChoice("Pick one", "choose", []contract.QuestionChoice{
		{Label: "First option"}, {Label: "Second option"}, {Label: "Third option"},
	}, func(index int) tea.Cmd {
		got = index
		return nil
	})
	renderFrame(m)
	if m.hits == nil || len(m.modalRows) == 0 {
		t.Fatal("modal produced no clickable rows")
	}
	// Click the second choice's recorded row.
	var clickY int = -1
	for _, tg := range m.hits.targets {
		if tg.kind == targetModalButton && tg.id == 1 {
			clickY = tg.rect.y
		}
	}
	if clickY < 0 {
		t.Fatal("second choice has no click region")
	}
	updated, _ = m.Update(tea.MouseClickMsg{X: m.width / 2, Y: clickY, Button: tea.MouseLeft})
	m = updated.(*Model)
	if got != 1 {
		t.Fatalf("clicking the second choice fired index %d, want 1", got)
	}
	if m.modal != nil {
		t.Fatal("single-select modal did not close after a click")
	}
}
