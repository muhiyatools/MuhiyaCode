package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
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
	header := m.renderHeader()
	// US4: rebuild the InteractionMap for THIS frame so click/hover coordinates
	// resolve against exactly what is drawn (mouse-interaction contract §1). Pane Y
	// offsets are implicit in the join order, so accumulate lineCount as panes are
	// assembled — the map and the screen are produced in one pass and cannot drift.
	im := &interactionMap{generation: m.frameState.Generation}
	var content string
	if m.modal != nil {
		hint := m.renderModeLine()
		available := max(4, m.height-lineCount(header)-lineCount(hint))
		dialog := m.renderModal() // also records m.modalRows (dialog-relative choice rows)
		// renderModal sizes itself to the terminal, but clamp as a hard
		// invariant: an oversized dialog is cut rather than allowed to push
		// the rest of the interface off-screen.
		rows := strings.Split(dialog, "\n")
		if len(rows) > available {
			rows = rows[:available]
			dialog = strings.Join(rows, "\n")
		}
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
		// clicking a tool/agent chip toggles/opens it. Content rows are converted with
		// the live viewport offset and clipped to the visible transcript band, so a
		// scrolled-away chip has no hit region.
		yOffset := m.viewport.YOffset()
		for _, chip := range m.transcriptChips {
			screenRow := y + chip.row - yOffset
			if screenRow < y || screenRow >= y+vpHeight {
				continue
			}
			kind := targetToolChip
			if chip.agentID != "" {
				kind = targetAgentChip
			}
			im.addChip(kind, rect{0, screenRow, m.width, 1}, chip.tool, chip.agentID)
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
		if activity := m.renderActivity(); activity != "" {
			parts = append(parts, activity)
			y += lineCount(activity)
		}
		if bar := m.renderPasteBar(); bar != "" {
			im.add(targetPasteBar, 0, rect{0, y, m.width, lineCount(bar)})
			parts = append(parts, bar)
			y += lineCount(bar)
		}
		input := m.renderInput()
		im.add(targetInput, 0, rect{0, y, m.width, lineCount(input)})
		parts = append(parts, input, m.renderModeLine())
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

func (m *Model) renderHeader() string {
	contextLabel := "context —"
	if m.context.ContextLimit > 0 {
		contextLabel = fmt.Sprintf("context %.1f%%", m.context.Percent)
	}
	main := modelDisplay(m.runtime.Settings, m.runtime.Settings.Provider.ActiveModelID)
	sub := modelDisplay(m.runtime.Settings, m.runtime.Settings.Provider.SubagentModelID)
	dot := m.palette.border.Render(m.glyphs.bullet)
	// Header segments as a slice so narrow widths can drop the reasoning then
	// Subagent segments (visual-system.md §4) before the path is truncated.
	seg := []string{
		" " + m.palette.brand.Render(m.glyphs.brand+" MuhiyaCode") + " " + m.palette.faint.Render("v"+m.version),
		dot + " " + m.palette.faint.Render("model") + " " + m.palette.text.Render(main),
	}
	subSeg := m.palette.faint.Render("Subagent") + " " + m.palette.muted.Render(sub)
	reasonSeg := m.palette.faint.Render("reasoning") + " " + m.palette.brandSoft.Render(string(m.runtime.Settings.Effort))
	if m.width >= m.theme.WideWidth {
		seg = append(seg, subSeg)
	}
	if m.width >= m.theme.ExtraWidth {
		seg = append(seg, reasonSeg)
	}
	seg = append(seg, dot+" "+m.palette.text.Render(contextLabel))
	line1 := strings.Join(seg, "  ")
	// Full workspace path, left-truncated (leading ellipsis) only when it cannot
	// fit — the rightmost, most-specific segments stay visible (FR-021).
	workspace := m.runtime.Session.WorkspacePath
	if workspace == "" {
		workspace = "."
	}
	workspace = truncateLeft(workspace, max(10, m.width-2), m.glyphs.ellipsis)
	parts := []string{m.palette.muted.Render(workspace)}
	if m.viewAgent != "" {
		if agent := m.agentByID[m.viewAgent]; agent != nil {
			parts = append(parts, m.palette.faint.Render("· view"), m.palette.brandSoft.Render(fmt.Sprintf("%s [%d]", agent.agent, agent.index)), m.palette.muted.Render(agent.title), m.palette.faint.Render(agent.status))
			if agent.usage.TotalTokens > 0 {
				parts = append(parts, m.palette.faint.Render(formatTokens(agent.usage.TotalTokens)+" tokens"))
			}
		}
	} else {
		running := 0
		for _, agent := range m.agents {
			if agent.status == "running" {
				running++
			}
		}
		if running > 0 {
			parts = append(parts, m.palette.brandSoft.Render(fmt.Sprintf("· %d agent(s) running", running)))
		}
	}
	line2 := " " + strings.Join(parts, "  ")
	rule := m.palette.border.Render(strings.Repeat(m.glyphs.ruleH, max(0, m.width)))
	// One blank spacer row above the brand line keeps the top bar off the
	// terminal's upper edge. layout() counts rendered lines, so the spacer is
	// automatically part of the height budget.
	return "\n" + fitLine(line1, m.width) + "\n" + fitLine(line2, m.width) + "\n" + rule
}

// truncateMiddle (004 US5) shortens a path-like value by cutting from the
// middle, keeping the leading root and — with priority — the trailing filename
// around a single ellipsis. When the final path segment fits within the budget
// it is kept whole, so a file/edit row never loses the one identifying part
// (the filename) the way a head-anchored cut would. Plain (ANSI-free) input.
func truncateMiddle(value string, width int, ellipsis string) string {
	if width <= 0 || runewidth.StringWidth(value) <= width {
		return value
	}
	ellWidth := runewidth.StringWidth(ellipsis)
	if width <= ellWidth {
		// No room for content around the ellipsis; right-anchor so the filename
		// tail is what survives.
		return truncateLeft(value, width, ellipsis)
	}
	avail := width - ellWidth
	tailBudget := avail - avail/3 // ~2/3 of the room goes to the filename tail
	// Keep the final path segment whole when it fits within the available room.
	if sep := strings.LastIndexAny(value, "/\\"); sep >= 0 {
		if fileWidth := runewidth.StringWidth(value[sep+1:]); fileWidth <= avail && fileWidth > tailBudget {
			tailBudget = fileWidth
		}
	}
	headBudget := avail - tailBudget
	runes := []rune(value)
	tailUsed, tailStart := 0, len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		w := runewidth.RuneWidth(runes[i])
		if tailUsed+w > tailBudget {
			break
		}
		tailUsed += w
		tailStart = i
	}
	headUsed, headEnd := 0, 0
	for i := 0; i < tailStart; i++ {
		w := runewidth.RuneWidth(runes[i])
		if headUsed+w > headBudget {
			break
		}
		headUsed += w
		headEnd = i + 1
	}
	return string(runes[:headEnd]) + ellipsis + string(runes[tailStart:])
}

// isPathTargetTool reports whether a tool's target is a filesystem path, and so
// should be middle-truncated (filename preserved) rather than head-truncated.
func isPathTargetTool(name string) bool {
	switch name {
	case "edit_file", "multi_edit", "write_file", "apply_patch", "read_file", "git_diff":
		return true
	}
	return false
}

// truncateLeft keeps the rightmost characters of value, prefixing an ellipsis
// when it must cut, so the most-specific path segments stay visible.
func truncateLeft(value string, width int, ellipsis string) string {
	if width <= 0 || runewidth.StringWidth(value) <= width {
		return value
	}
	runes := []rune(value)
	ellWidth := runewidth.StringWidth(ellipsis)
	target := max(0, width-ellWidth)
	used := 0
	cut := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		w := runewidth.RuneWidth(runes[i])
		if used+w > target {
			break
		}
		used += w
		cut = i
	}
	return ellipsis + string(runes[cut:])
}

// renderActivity is the single live-work component above the input: while a
// task runs it shows the spinner, status, elapsed time, token count, a
// thinking snippet, and the plan's progress. When idle it renders nothing —
// no standing "Ready" line.

// contentWidth (T3) is the single shared width formula for thinking, input,
// and transcript blocks. The transcript remains the source of truth — it
// already accounts for the gutter and right-margin constants there.
func (m *Model) contentWidth() int {
	return max(20, m.viewport.Width()-len(transcriptGutter)-1)
}

// spinner returns the current spinner frame from the glyph table.
func (m *Model) spinner() string {
	frames := m.glyphs.spinner
	if len(frames) == 0 {
		return ""
	}
	return frames[m.frame%len(frames)]
}

func (m *Model) renderActivity() string {
	// Yield to the command palette while the user is browsing commands.
	if strings.HasPrefix(m.input.Value(), "/") {
		return ""
	}
	// US1 T021: during two-stage launch, show the loading cue above the composer so
	// the shell never looks frozen while the runtime hydrates in the background.
	if m.loading {
		return fitLine(" "+m.palette.brand.Render(m.spinner())+" "+m.palette.muted.Render("Loading workspace…"), m.width)
	}
	// 003 (T019/FR-009): the thinking region is strictly ephemeral — it renders
	// only while a task is busy. On completion it disappears entirely; there is
	// no post-task "thought for Ns" residue.
	if !m.busy {
		return ""
	}
	mark := m.palette.brand.Render(m.spinner())
	lines := []string{}
	if m.busy {
		status := m.status
		if !m.started.IsZero() {
			status += "  " + m.palette.faint.Render(formatDuration(time.Since(m.started)))
		}
		if m.usage.TotalTokens > 0 {
			status += "  " + m.palette.faint.Render(formatTokens(m.usage.TotalTokens)+" tokens")
		}
		if tag := cacheTag(m.usage); tag != "" {
			status += "  " + m.palette.brandSoft.Render(tag)
		}
		lines = append(lines, fitLine(" "+mark+" "+m.palette.muted.Render(status), m.width))
	}
	// T2 thinking row: live tail during streaming, single "thought for Ns"
	// after completion. Width comes from contentWidth so the gutter, input,
	// and transcript always agree.
	width := m.contentWidth()
	if m.reasoning != "" && m.busy {
		lines = append(lines, "") // blank-line separation above the thinking block
		text := RenderRTL(oneLine(m.reasoning, width), m.runtime.Settings.RTL.Mode)
		rendered := lipgloss.NewStyle().Padding(0, 1).Width(width).Render(m.palette.faint.Render(m.glyphs.gutter + " thinking… " + text))
		lines = append(lines, m.palette.faint.Render("  ")+rendered)
	}
	// 004 US2 (T029): the plan progress line is gated on the lifecycle phase. A
	// finished/superseded/discarded plan-mode plan retires the line entirely; an
	// interrupted plan is labelled as such. A regular multi-step task (phase none,
	// no plan mode) keeps showing progress exactly as before.
	planPhase := contract.PlanPhaseNone
	if m.runtime.Engine != nil {
		planPhase = m.runtime.Engine.PlanPhase()
	}
	if steps := len(m.plan.Steps); steps > 0 && !planPhase.IsTerminal() {
		done, current := 0, ""
		for _, step := range m.plan.Steps {
			if step.Status == contract.PlanCompleted {
				done++
			} else if step.Status == contract.PlanInProgress && current == "" {
				current = step.Title
			}
		}
		progress := fmt.Sprintf("plan %d/%d", done, steps)
		if planPhase == contract.PlanPhaseInterrupted {
			progress += " · interrupted"
		} else if current != "" {
			progress += "  " + current
		}
		lines = append(lines, fitLine("   "+m.palette.brandSoft.Render(m.glyphs.bullet+" ")+m.palette.muted.Render(progress), m.width))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderInput() string {
	// 006: for Arabic/RTL input, custom-render the content (shaped, reordered,
	// right-aligned, mapped caret); LTR keeps the textarea's own render. Content
	// width = terminal minus the rounded border (2) and horizontal padding (2).
	input := m.composerView(max(1, m.width-4))
	// T048: the input is a bordered box spanning the full terminal width, so it
	// deliberately uses m.width-2 (terminal minus the rounded border) rather than
	// contentWidth (which is viewport-relative for gutter-indented content).
	// Border color and glyphs come from the palette/glyph tables (no hardcoded
	// hex): border.default at rest, border.focus (accent) while busy/steering.
	style := lipgloss.NewStyle().Border(m.glyphs.border).BorderForeground(m.palette.border.GetForeground()).Padding(0, 1).Width(max(10, m.width-2))
	if m.busy {
		style = style.BorderForeground(m.palette.focus.GetForeground())
	}
	return style.Render(input)
}

// renderModeLine sits directly below the input box: the live permission mode
// first, then the key hints.
func (m *Model) renderModeLine() string {
	mode := "normal"
	if m.runtime.Settings != nil {
		mode = string(m.runtime.Settings.PermissionMode)
	}
	modeStyle := m.palette.brandSoft
	if mode == string(contract.PermissionAutoAccept) {
		modeStyle = m.palette.warning
	}
	// 003 (T026/FR-014): drop the "Enter send" and "Ctrl+P commands" hints — the
	// placeholder teaches "/" for commands. Keep a minimal set of the less
	// obvious affordances; tool detail is now reached by clicking a tool row.
	hints := "Esc stop · Tab agents · click a tool for details"
	line := " " + m.palette.faint.Render("mode") + " " + modeStyle.Render(mode)
	if m.runtime.Engine != nil && m.runtime.Engine.PlanMode() {
		line += "  " + m.palette.border.Render("·") + " " + m.palette.warning.Render("plan mode")
	}
	if m.runtime.Engine != nil {
		if goal, ok := m.runtime.Engine.GoalSnapshot(); ok && goal.Status == orchestrator.GoalActive {
			line += "  " + m.palette.border.Render("·") + " " + m.palette.brandSoft.Render("goal active")
		}
	}
	if n := len(m.selectedSkills); n > 0 {
		line += "  " + m.palette.border.Render("·") + " " + m.palette.brandSoft.Render(fmt.Sprintf("%d skill(s) queued", n))
	}
	line += "  " + m.palette.border.Render("·") + " " + m.palette.faint.Render(hints)
	return fitLine(line, m.width)
}

// renderNotice shows the transient system message (settings saved, MCP status,
// task summary, errors) so those never enter the transcript.
func (m *Model) renderNotice() string {
	if m.flash.text == "" {
		return ""
	}
	style := m.palette.muted
	label := m.palette.faint.Render("›")
	if m.flash.level == "warn" {
		style = m.palette.warning
		label = m.palette.warning.Render("!")
	}
	return fitLine(" "+label+" "+style.Render(oneLine(m.flash.text, max(12, m.width-4))), m.width)
}

// commandLayout is the single source of truth for the command-palette window: the
// filtered matches, the scroll-window start, and how many rows are shown. Both the
// renderer and the mouse target recorder consume it so a click always resolves to
// the exact row the user sees. ok is false when the palette is not showing.
func (m *Model) commandLayout() (matches []commandEntry, start, limit int, ok bool) {
	if !strings.HasPrefix(m.input.Value(), "/") {
		return nil, 0, 0, false
	}
	matches = m.commandMatches()
	if len(matches) == 0 {
		return nil, 0, 0, false
	}
	limit = min(m.commandWindow(), len(matches))
	selected := max(0, min(m.commandIndex, len(matches)-1))
	start = max(0, min(selected-limit/2, len(matches)-limit))
	return matches, start, limit, true
}

func (m *Model) renderCommands() string {
	matches, start, limit, ok := m.commandLayout()
	if !ok {
		return ""
	}
	selected := max(0, min(m.commandIndex, len(matches)-1))
	var lines []string
	for index := start; index < start+limit; index++ {
		marker, style := "  ", m.palette.muted
		if index == selected {
			marker, style = "› ", m.palette.brandSoft
		}
		lines = append(lines, fitLine(" "+style.Render(marker+matches[index].name)+"  "+m.palette.faint.Render(matches[index].description), m.width))
	}
	if len(matches) > limit {
		lines = append(lines, fitLine(" "+m.palette.faint.Render(fmt.Sprintf("   %d/%d — ↑/↓ to browse, Tab to complete, Enter to run", selected+1, len(matches))), m.width))
	}
	return strings.Join(lines, "\n")
}

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

// renderModal builds the dialog inside an explicit row budget derived from the
// terminal height, so the box always fits: on short terminals it drops the
// vertical padding, blank separators, and description row, and it truncates
// the message before it can squeeze out the choices or the hint.
func (m *Model) renderModal() string {
	modal := m.modal
	m.modalRows = m.modalRows[:0] // rebuilt below so mouse targets match this render
	width := min(max(38, m.width-12), 78)
	inner := width - 6
	budget := max(6, m.height-4) // rows for the whole box; header + mode line excluded
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
		minContent = min(len(modal.choices), 2)
	}
	message := wrapMessage(modal.message, inner)
	if limit := max(1, body-2-3*sep-minContent); len(message) > limit {
		message = message[:limit]
		message[limit-1] += " …"
	}

	lines := []string{m.palette.brand.Render(modal.title)}
	lines = blank(lines)
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
		lines = blank(lines)
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
			marker := "  "
			if modal.multi {
				marker = "[ ]"
				if modal.checked[index] {
					marker = "[×]"
				}
			}
			style := m.palette.text
			if index == selected {
				style, marker = m.palette.brandSoft, "› "+marker
			}
			label := style.Render(marker + " " + m.rtl(oneLine(choice.Label, width-10)))
			if choice.Recommended {
				label += " " + m.palette.brand.Render("current")
			}
			// Record this choice's dialog-relative row (top border + vertical padding
			// + inner line index) so recordModalTargets can place a click region on it.
			m.modalRows = append(m.modalRows, modalRowSpan{choice: index, row: 1 + pad + len(lines)})
			lines = append(lines, label)
			if choice.Description != "" && index == selected && descRows > 0 {
				lines = append(lines, m.palette.faint.Render("    "+oneLine(choice.Description, width-10)))
			}
		}
		hint := "↑/↓ choose · Enter confirms · Esc cancels"
		if modal.multi {
			hint = "↑/↓ choose · Space toggles · Enter confirms"
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

// commandWindow is how many palette rows fit without pushing the input off the
// screen. It shrinks on short terminals so the palette can never overflow.
func (m *Model) commandWindow() int {
	reserved := 3 + 1 + (m.input.Height() + 2) + 1 + 3 // header, status, input, mode line, min viewport
	return max(1, min(6, m.height-reserved-1))         // -1 leaves room for the count hint
}

func (m *Model) refreshViewport(forceBottom bool) {
	// transcriptRenders counts full transcript re-renders (US1/US2 perf guard):
	// tests assert this does NOT advance on a keystroke or an idle tick, so the
	// O(all items) render cost stays off the typing hot path.
	m.transcriptRenders++
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) renderTranscript() string {
	items := m.items
	inAgentView := false
	if m.viewAgent != "" {
		if agent := m.agentByID[m.viewAgent]; agent != nil {
			items = append([]item{{kind: "system", content: "Agent task: " + agent.task}}, agent.items...)
			inAgentView = true
		}
	}
	// US6: an empty transcript shows a first-run cue instead of a blank void — it
	// also teaches the "/" palette and the mouse affordances so they are discoverable.
	if len(items) == 0 {
		m.transcriptChips = m.transcriptChips[:0]
		hint := m.firstRunHint()
		m.transcriptContent = hint
		return hint
	}
	// Reserve the left gutter and a small right margin so text never touches
	// either edge of the terminal. T048: use the single shared contentWidth so
	// transcript, thinking, and footer never drift apart.
	width := m.contentWidth()
	rtl := m.runtime.Settings.RTL.Mode
	m.transcriptChips = m.transcriptChips[:0]
	blocks := make([]string, 0, len(items))
	// row tracks the content-row where the NEXT appended block begins. Blocks are
	// joined with "\n\n", so each contributes its own rows plus one separator blank.
	// Empty blocks are skipped entirely — they would otherwise inject stray blank
	// rows and desync the chip row math. A tool/agent chip's clickable header is its
	// block's first row (row), recorded only for the main transcript.
	row := 0
	appendBlock := func(block string, tool *toolView, agentID string) {
		if block == "" {
			return
		}
		if !inAgentView && (tool != nil || agentID != "") {
			m.transcriptChips = append(m.transcriptChips, chipSpan{row: row, tool: tool, agentID: agentID})
		}
		blocks = append(blocks, block)
		row += lineCount(block) + 1
	}
	for i := range items {
		entry := &items[i]
		switch entry.kind {
		case "user", "assistant", "assistant_draft", "system":
			// US1: reuse the memoized block when nothing this render depends on has
			// changed (width, content length, title). Immutable items hit this every
			// frame, so streaming re-renders only the growing draft, not the whole
			// history. cachedWidth==0 (zero value) means "no cache yet".
			if entry.cachedWidth == width && entry.cachedLen == len(entry.content) && entry.cachedTitle == entry.title {
				appendBlock(entry.cachedBlock, nil, "")
				continue
			}
			block := m.renderTextBlock(entry, width, rtl)
			entry.cachedBlock, entry.cachedWidth, entry.cachedLen, entry.cachedTitle = block, width, len(entry.content), entry.title
			appendBlock(block, nil, "")
		case "summary":
			if entry.stats != nil {
				appendBlock(taskSummaryLine(*entry.stats, m.palette), nil, "")
			}
		case "tool":
			appendBlock(m.renderTool(entry.tool, width), entry.tool, "")
		case "agent":
			if agent := m.agentByID[entry.agentID]; agent != nil {
				appendBlock(m.renderAgentChip(agent, width), nil, entry.agentID)
			}
		}
	}
	content := indentLines(strings.Join(blocks, "\n\n"), transcriptGutter)
	// US4 T058: keep the exact styled content so a text selection can extract its
	// ANSI-free plain text on copy without re-deriving the transcript.
	m.transcriptContent = content
	return content
}

// firstRunHint is the US6 empty-session cue: a calm welcome that teaches the
// command palette and the mouse affordances so a new user is never faced with a
// blank screen. It disappears the moment the first turn is added.
func (m *Model) firstRunHint() string {
	lines := []string{
		m.palette.brand.Render(m.glyphs.brand + " Welcome to MuhiyaCode"),
		"",
		m.palette.muted.Render("Type a request below, or press ") + m.palette.brandSoft.Render("/") + m.palette.muted.Render(" for commands."),
		m.palette.faint.Render("Project instructions (MUHIYA.md) and saved memory load automatically."),
		m.palette.faint.Render("Drag to select · Ctrl+C copies · click a tool row to expand it."),
	}
	return indentLines(strings.Join(lines, "\n"), transcriptGutter)
}

// renderTextBlock renders one cacheable text item (user/assistant/system). It is
// a pure function of the entry, width, and RTL mode (palette is startup-constant),
// which is what makes the per-item cache in renderTranscript correct.
func (m *Model) renderTextBlock(entry *item, width int, rtl string) string {
	switch entry.kind {
	case "user":
		// 003 (T027/FR-015): user messages are identified by the surface background
		// band alone — no "> " prefix. An optional title (e.g. "steering") is a
		// small faint prefix, not a marker. 006: wrap the LOGICAL text first, then
		// shape each display line (wrap-then-shape) so Arabic joins correctly.
		prefix := ""
		if entry.title != "" {
			prefix = m.palette.faint.Render(entry.title + " ")
		}
		wrapped := wrapPlain(entry.content, max(10, width-4))
		for i, line := range wrapped {
			visual := renderForDisplay(line, rtl, "left").Visual
			wrapped[i] = m.palette.surface.Padding(0, 1).Render(visual)
		}
		return prefix + strings.Join(wrapped, "\n  ")
	case "assistant", "assistant_draft":
		return RenderMarkdown(entry.content, width, rtl, m.runtime.Settings.RTL.Align, m.palette)
	case "system":
		return m.palette.faint.Render("○ ") + RenderMarkdown(entry.content, width-2, rtl, m.runtime.Settings.RTL.Align, m.palette)
	}
	return ""
}

// indentLines prefixes every non-empty line with the gutter so the whole
// transcript keeps a consistent left margin.
func indentLines(value, gutter string) string {
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = gutter + line
		}
	}
	return strings.Join(lines, "\n")
}

// rtl shapes s for display in the current RTL mode (visual only, no alignment),
// for inline chrome strings — tool targets, modal text/choices, plan steps — where
// Arabic can appear. It is a byte-identical no-op for pure-LTR content (006).
func (m *Model) rtl(s string) string {
	return renderForDisplay(s, m.runtime.Settings.RTL.Mode, "left").Visual
}

// renderTool renders one tool entry (contract tool-display.md). Collapsed
// (default): exactly one line — marker + label + target + a minimal outcome
// measure, no content. Expanded (click the tool row): the same header plus a
// uniform detail block (colorized diff, or boxed output) with a gutter.
func (m *Model) renderTool(tool *toolView, width int) string {
	if tool == nil {
		return ""
	}
	marker := m.palette.brand.Render(m.glyphs.markerOK)
	if tool.state == "running" {
		marker = m.palette.brand.Render(m.spinner())
	} else if tool.state == "cancelled" {
		// 004 US1 (T013): a tool cut short at task end reads as neutral-stopped,
		// never as success (markerOK) and never as an error it did not raise.
		marker = m.palette.muted.Render(m.glyphs.markerFail)
	} else if tool.state == "fail" {
		marker = m.palette.danger.Render(m.glyphs.markerFail)
	}
	// Hover affordance: underline the label and target so a tool row under the mouse
	// reads as clickable, without shifting the layout (a click expands its detail).
	labelStyle, targetStyle := m.palette.text, m.palette.brandSoft
	if tool.hovered {
		labelStyle = labelStyle.Underline(true)
		targetStyle = targetStyle.Underline(true)
	}
	line := marker + " " + labelStyle.Render(toolLabel(tool.name))
	// Always surface the file path in the MAIN line for path-target tools (Edit,
	// Write, Patch, …), before the changed-line count. When the live target was not
	// captured (a resumed or paged-in row), derive it from the diff's file headers
	// so the path is never hidden inside the expanded details.
	target := tool.target
	if target == "" {
		target = deriveToolTarget(tool.name, tool.output)
	}
	if target != "" {
		// 004 US5 (T047): a file path is middle-truncated so its filename survives
		// on narrow terminals; command/query targets keep the head-anchored cut.
		targetWidth := max(12, width/2)
		segment := oneLine(target, targetWidth)
		if isPathTargetTool(tool.name) {
			segment = truncateMiddle(strings.Join(strings.Fields(target), " "), targetWidth, m.glyphs.ellipsis)
		}
		line += " " + targetStyle.Render(m.rtl(segment))
	}
	if outcome := m.toolOutcome(tool); outcome != "" {
		line += "  " + m.styledOutcome(tool, outcome, max(12, width/3))
	}
	lines := []string{fitLine(line, width)}
	if !tool.expanded {
		// Collapsed: header only — no content lines. Click the tool's header to
		// expand just that tool's detail block.
		return lines[0]
	}
	// Expanded: uniform detail block. A diff renders colorized; any other output
	// renders in the same gutter-prefixed style.
	if diff := diffLines(tool.output); len(diff) > 0 {
		for _, source := range diff {
			lines = append(lines, "  "+m.diffLineStyle(source).Render(oneLine(source, width-2)))
		}
	} else if tool.output != "" {
		body := strings.Join(wrapPlain(tool.output, width-4), "\n")
		lines = append(lines, m.palette.surface2.Padding(0, 1).Width(width-2).Render(body))
	}
	return strings.Join(lines, "\n")
}

// styledOutcome renders the collapsed outcome measure with its semantic color:
// failures in the danger style, "+N −N" diff counts split into diff.add (green
// additions) and diff.remove (red deletions) so the two are distinguishable at
// a glance, everything else in the faint token.
func (m *Model) styledOutcome(tool *toolView, outcome string, limit int) string {
	if tool.state == "fail" {
		return m.palette.danger.Render(oneLine(outcome, limit))
	}
	switch tool.name {
	case "edit_file", "multi_edit", "apply_patch", "write_file", "git_diff":
		if add, remove, ok := diffCounts(tool.output); ok {
			return m.palette.add.Render(fmt.Sprintf("+%d", add)) + " " + m.palette.remove.Render(fmt.Sprintf("−%d", remove))
		}
	}
	return m.palette.faint.Render(oneLine(outcome, limit))
}

// toolOutcome derives the collapsed one-line outcome measure per tool class
// (contract tool-display.md §2). It degrades to the first-line summary when no
// specific measure can be derived — never guesses.
func (m *Model) toolOutcome(tool *toolView) string {
	if tool.state == "running" {
		if !tool.started.IsZero() && time.Since(tool.started) > 2*time.Second {
			return "running… " + formatDuration(time.Since(tool.started))
		}
		return "running…"
	}
	if tool.state == "cancelled" {
		return "cancelled"
	}
	switch tool.name {
	case "edit_file", "multi_edit", "apply_patch", "write_file", "git_diff":
		if add, remove, ok := diffCounts(tool.output); ok {
			return fmt.Sprintf("+%d −%d", add, remove)
		}
	case "read_file":
		if n := contentLineCount(tool.output); n > 0 {
			return fmt.Sprintf("%d lines", n)
		}
	case "grep", "search_text":
		if n := contentLineCount(tool.output); n > 0 {
			return fmt.Sprintf("%d matches", n)
		}
	case "list_files", "glob":
		if n := contentLineCount(tool.output); n > 0 {
			return fmt.Sprintf("%d entries", n)
		}
	case "web_search":
		if n := webResultCount(tool.output); n > 0 {
			return fmt.Sprintf("%d results", n)
		}
	}
	return summarizeTool(tool.output)
}

// diffCounts counts added/removed lines in an embedded diff payload, excluding
// the +++/--- file headers. ok is false when there is no diff to count.
func diffCounts(output string) (add, remove int, ok bool) {
	diff := diffLines(output)
	if len(diff) == 0 {
		return 0, 0, false
	}
	for _, line := range diff {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			remove++
		}
	}
	return add, remove, true
}

// contentLineCount counts non-empty lines, ignoring an embedded diff marker.
func contentLineCount(output string) int {
	output = strings.TrimSpace(output)
	if output == "" {
		return 0
	}
	if idx := strings.Index(output, "\n--- diff ---\n"); idx >= 0 {
		output = strings.TrimSpace(output[:idx])
	}
	n := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// webResultCount counts numbered "[n]" result markers in web-search output. The
// provider/engine name is never present (gateway returns provider-agnostic
// results) and is never rendered.
func webResultCount(output string) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 3 && line[0] == '[' && line[1] >= '0' && line[1] <= '9' && strings.Contains(line, "]") {
			n++
		}
	}
	return n
}

func (m *Model) diffLineStyle(source string) lipgloss.Style {
	switch {
	case strings.HasPrefix(source, "+") && !strings.HasPrefix(source, "+++"):
		return m.palette.add
	case strings.HasPrefix(source, "-") && !strings.HasPrefix(source, "---"):
		return m.palette.remove
	case strings.HasPrefix(source, "@@"):
		return m.palette.warning
	case strings.HasPrefix(source, "+++") || strings.HasPrefix(source, "---"):
		return m.palette.brandSoft
	default:
		return m.palette.faint
	}
}

func (m *Model) renderAgentChip(agent *agentView, width int) string {
	marker := m.palette.brand.Render(m.glyphs.brand)
	if agent.status == "running" {
		marker = m.palette.brand.Render(m.spinner())
	} else if agent.status == "failed" || agent.status == "cancelled" {
		marker = m.palette.danger.Render(m.glyphs.brand)
	}
	// Hover affordance: underline the subagent title when the row is under the mouse.
	titleStyle := m.palette.text
	if agent.hovered {
		titleStyle = titleStyle.Underline(true)
	}
	line := fmt.Sprintf("%s [%d] %s", marker, agent.index, titleStyle.Render(agent.title))
	label := agent.agent + " · " + agent.status
	if agent.model != "" {
		label = agent.agent + " (" + agent.model + ") · " + agent.status
	}
	line += "  " + m.palette.faint.Render(label)
	if agent.usage.TotalTokens > 0 {
		line += "  " + m.palette.faint.Render(formatTokens(agent.usage.TotalTokens))
	}
	if agent.active != "" {
		line += "\n  " + m.palette.faint.Render("└ "+agent.active)
	}
	return fitLine(line, width)
}

// layout recomputes the transcript viewport size from the height actually
// consumed by every other pane. Counting rendered lines (rather than
// re-deriving them) guarantees the panes always sum to the terminal height, so
// the command palette or plan can never push the input off-screen.
func (m *Model) layout() {
	m.input.SetWidth(max(20, m.width-6))
	if m.modal != nil {
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(max(3, m.height-lineCount(m.renderHeader())-lineCount(m.renderModeLine())))
		m.snapshotLayout()
		return
	}
	used := lineCount(m.renderHeader()) + (m.input.Height() + 2) + lineCount(m.renderModeLine())
	if activity := m.renderActivity(); activity != "" {
		used += lineCount(activity)
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
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(max(3, m.height-used))
	m.snapshotLayout() // US1/US2 T006: record the resolved pane geometry
}

func (m *Model) loadEvents(events []contract.Event) {
	// Only user, assistant, and tool turns belong in the transcript. System
	// and setup events are surfaced through the transient notice line instead.
	for _, event := range events {
		switch event.Role {
		case "user":
			m.items = append(m.items, item{kind: "user", content: event.Content})
		case "assistant":
			m.items = append(m.items, item{kind: "assistant", content: event.Content})
		case "tool":
			m.items = append(m.items, item{kind: "tool", tool: &toolView{name: event.Type, state: map[bool]string{true: "fail", false: "ok"}[isFailure(event.Content)], output: event.Content, summary: summarizeTool(event.Content), started: event.CreatedAt}})
		}
	}
}

func toolTarget(name string, input []byte) string {
	var values map[string]any
	_ = json.Unmarshal(input, &values)
	// 004 US5 (T046): apply_patch carries no "path" argument — its target lives in
	// the unified-diff body's file headers. Derive it so a Patch row names its
	// file like every other write/edit row.
	if name == "apply_patch" {
		if patch, ok := values["patch"].(string); ok {
			return patchTargetFile(patch)
		}
		return ""
	}
	for _, key := range []string{"path", "command", "query", "pattern", "title", "agent"} {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// patchTargetFile (004 US5) derives the display target for an apply_patch call
// from its unified-diff body, mirroring workspace.ApplyPatch's file resolution:
// the +++ name, or the --- name when the +++ side is /dev/null (a deletion),
// with a/ or b/ prefixes stripped. Multiple distinct files render as the first
// followed by " (+N more)". Returns "" when no file header can be parsed.
func patchTargetFile(patch string) string {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	name := func(v string) string {
		v = strings.TrimSpace(strings.SplitN(v, "\t", 2)[0])
		v = strings.SplitN(v, " ", 2)[0]
		return strings.TrimPrefix(strings.TrimPrefix(v, "a/"), "b/")
	}
	var files []string
	seen := map[string]bool{}
	for i := 0; i+1 < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "--- ") || !strings.HasPrefix(lines[i+1], "+++ ") {
			continue
		}
		target := name(strings.TrimPrefix(lines[i+1], "+++ "))
		if target == "/dev/null" || target == "" {
			target = name(strings.TrimPrefix(lines[i], "--- "))
		}
		if target == "" || target == "/dev/null" || seen[target] {
			continue
		}
		seen[target] = true
		files = append(files, target)
	}
	if len(files) == 0 {
		return ""
	}
	if len(files) == 1 {
		return files[0]
	}
	return fmt.Sprintf("%s (+%d more)", files[0], len(files)-1)
}

// deriveToolTarget recovers a path-target tool's file from its diff output when
// the live target was not captured (a resumed or paged-in transcript row). It
// reuses patchTargetFile on the embedded unified diff (a/… ---, b/… +++ headers),
// so an Edit/Write/Patch row always names its file in the main line.
func deriveToolTarget(name, output string) string {
	if !isPathTargetTool(name) {
		return ""
	}
	diff := diffLines(output)
	if len(diff) == 0 {
		return ""
	}
	return patchTargetFile(strings.Join(diff, "\n"))
}

func toolLabel(name string) string {
	labels := map[string]string{"list_files": "List", "read_file": "Read", "grep": "Grep", "search_text": "Search", "glob": "Glob", "edit_file": "Edit", "multi_edit": "Edit", "write_file": "Write", "apply_patch": "Patch", "run_shell": "Shell", "git_status": "Git status", "git_diff": "Git diff", "update_plan": "Plan", "ask_user": "Question", "propose_changes": "Change plan", "run_subagent": "Delegate", "web_search": "Web search"}
	if label := labels[name]; label != "" {
		return label
	}
	if strings.HasPrefix(name, "mcp__") {
		return "MCP · " + strings.ReplaceAll(strings.TrimPrefix(name, "mcp__"), "__", " / ")
	}
	return strings.Title(strings.ReplaceAll(name, "_", " "))
}

func summarizeTool(output string) string {
	first := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	// Rune-safe cut: a byte-index slice would split a multibyte CJK/emoji/Arabic
	// glyph and render a replacement character.
	if runes := []rune(first); len(runes) > 100 {
		first = string(runes[:99]) + "…"
	}
	return first
}

func isFailure(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	for _, prefix := range []string{"tool ", "edit failed", "patch failed", "permission denied", "blocked:", "invalid ", "unknown tool"} {
		if strings.HasPrefix(lower, prefix) && (prefix != "tool " || strings.Contains(lower, "failed")) {
			return true
		}
	}
	return false
}

func diffLines(output string) []string {
	marker := "\n--- diff ---\n"
	index := strings.Index(output, marker)
	if index < 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(output[index+len(marker):]), "\n")
}

// taskSummaryLine renders the calm three-metric task summary (contract
// task-summary.md §3): credits · tokens · cache %, each omitted when its source
// is unavailable, with a dimmed "interrupted" marker for early-ended tasks.
// Credits come from the task's record-range scan (stats.CreditsUSD, converted
// via contract.USDToCredits — 2,500 credits per $25 of budget); tokens are all
// reported input+output; cache % is token-weighted.
func taskSummaryLine(stats contract.TaskStats, colors palette) string {
	var parts []string
	if stats.CreditsUSD != nil {
		prefix := ""
		if stats.CreditsEstimated {
			prefix = "~"
		}
		parts = append(parts, fmt.Sprintf("%scredits %.2f", prefix, contract.USDToCredits(*stats.CreditsUSD)))
	}
	parts = append(parts, formatTokens(stats.Usage.TotalTokens)+" tokens")
	if rate := contract.HitRate(stats.Usage.CacheReadTokens, stats.Usage.CacheMissTokens); rate != nil {
		parts = append(parts, fmt.Sprintf("cache %.0f%%", *rate*100))
	}
	line := colors.muted.Render(strings.Join(parts, " · "))
	if stats.StopCause != "" {
		// The interrupted marker is a state signal, not metadata — warning color
		// keeps it legible on every terminal theme.
		line += "  " + colors.warning.Render("interrupted")
	}
	return line
}

func cacheTag(usage contract.Usage) string {
	if usage.CacheReadTokens == nil || usage.CacheMissTokens == nil {
		return ""
	}
	rate := contract.HitRate(usage.CacheReadTokens, usage.CacheMissTokens)
	label := "cache n/a"
	if rate != nil {
		label = fmt.Sprintf("cache %.0f%%", *rate*100)
	}
	newLabel := formatTokens(*usage.CacheMissTokens) + " new"
	if usage.MissDerived {
		newLabel += " derived"
	}
	return fmt.Sprintf("%s (%s read / %s)", label, formatTokens(*usage.CacheReadTokens), newLabel)
}

func modelDisplay(settings *contract.Settings, id string) string {
	if settings == nil {
		return "model unset"
	}
	for _, model := range settings.Provider.Models {
		if model.ID == id {
			return model.Name
		}
	}
	if id != "" {
		return id
	}
	return "model unset"
}

// fitLine truncates a possibly styled line to the given visible width. It is
// ANSI-aware so escape sequences are measured as zero width and never cut in
// half, which keeps styled status/hint lines from truncating their visible
// text prematurely on narrow terminals.
func fitLine(value string, width int) string {
	if ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(1, width-1), "…")
}

func oneLine(value string, width int) string {
	// Collapse whitespace, then cluster-safe truncate by display width (uniseg), so
	// vocalized Arabic is not over-measured by per-rune width (006 T011).
	return truncateToWidth(strings.Join(strings.Fields(value), " "), width)
}

func lineCount(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}
