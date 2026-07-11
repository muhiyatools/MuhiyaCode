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

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *Model) View() tea.View {
	header := m.renderHeader()
	var content string
	if m.modal != nil {
		hint := m.renderModeLine()
		available := max(4, m.height-lineCount(header)-lineCount(hint))
		dialog := m.renderModal()
		// renderModal sizes itself to the terminal, but clamp as a hard
		// invariant: an oversized dialog is cut rather than allowed to push
		// the rest of the interface off-screen.
		if rows := strings.Split(dialog, "\n"); len(rows) > available {
			dialog = strings.Join(rows[:available], "\n")
		}
		body := lipgloss.Place(m.width, available, lipgloss.Center, lipgloss.Center, dialog)
		content = header + "\n" + body + "\n" + hint
	} else {
		parts := []string{header, m.viewport.View()}
		if commands := m.renderCommands(); commands != "" {
			parts = append(parts, commands)
		}
		if flash := m.renderNotice(); flash != "" {
			parts = append(parts, flash)
		}
		if activity := m.renderActivity(); activity != "" {
			parts = append(parts, activity)
		}
		parts = append(parts, m.renderInput(), m.renderModeLine())
		content = strings.Join(parts, "\n")
	}
	view := tea.NewView(content)
	view.AltScreen = true
	// Mouse capture stays off (MouseModeNone) so the terminal keeps native
	// text selection and copy; the transcript scrolls with PageUp/PageDown
	// and the arrow keys instead.
	view.WindowTitle = "MuhiyaCode · " + filepath.Base(m.runtime.Session.WorkspacePath)
	view.BackgroundColor = lipgloss.Color("#0B100E")
	view.ForegroundColor = lipgloss.Color("#E7F0EB")
	return view
}

func (m *Model) renderHeader() string {
	contextLabel := "context —"
	if m.context.ContextLimit > 0 {
		contextLabel = fmt.Sprintf("context %.1f%%", m.context.Percent)
	}
	main := modelDisplay(m.runtime.Settings, m.runtime.Settings.Provider.ActiveModelID)
	sub := modelDisplay(m.runtime.Settings, m.runtime.Settings.Provider.SubagentModelID)
	line1 := " " + m.palette.brand.Render("◆ MuhiyaCode") + " " + m.palette.faint.Render("v"+m.version) +
		"  " + m.palette.border.Render("·") + " " + m.palette.faint.Render("model") + " " + m.palette.text.Render(main) +
		"  " + m.palette.faint.Render("agents") + " " + m.palette.muted.Render(sub) +
		"  " + m.palette.faint.Render("reasoning") + " " + m.palette.brandSoft.Render(string(m.runtime.Settings.Effort)) +
		"  " + m.palette.border.Render("·") + " " + m.palette.text.Render(contextLabel)
	workspace := filepath.Base(m.runtime.Session.WorkspacePath)
	if workspace == "." || workspace == string(filepath.Separator) || workspace == "" {
		workspace = m.runtime.Session.WorkspacePath
	}
	parts := []string{m.palette.faint.Render(workspace)}
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
	rule := m.palette.border.Render(strings.Repeat("─", m.width))
	return fitLine(line1, m.width) + "\n" + fitLine(line2, m.width) + "\n" + rule
}

// renderActivity is the single live-work component above the input: while a
// task runs it shows the spinner, status, elapsed time, token count, a
// thinking snippet, and the plan's progress. When idle it renders nothing —
// no standing "Ready" line.
func (m *Model) renderActivity() string {
	// Yield to the command palette while the user is browsing commands.
	if strings.HasPrefix(m.input.Value(), "/") {
		return ""
	}
	if !m.busy {
		return ""
	}
	mark := m.palette.brand.Render(spinnerFrames[m.frame%len(spinnerFrames)])
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
	lines := []string{fitLine(" "+mark+" "+m.palette.muted.Render(status), m.width)}
	if m.reasoning != "" {
		lines = append(lines, fitLine("   "+m.palette.faint.Render("· "+oneLine(m.reasoning, max(12, m.width-8))), m.width))
	}
	if steps := len(m.plan.Steps); steps > 0 {
		done, current := 0, ""
		for _, step := range m.plan.Steps {
			if step.Status == contract.PlanCompleted {
				done++
			} else if step.Status == contract.PlanInProgress && current == "" {
				current = step.Title
			}
		}
		progress := fmt.Sprintf("plan %d/%d", done, steps)
		if current != "" {
			progress += "  " + current
		}
		lines = append(lines, fitLine("   "+m.palette.brandSoft.Render("▸ ")+m.palette.muted.Render(progress), m.width))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderInput() string {
	input := m.input.View()
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#314239")).Padding(0, 1).Width(max(10, m.width-2))
	if m.busy {
		style = style.BorderForeground(lipgloss.Color("#43D17D"))
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
	send := "Enter send"
	if m.busy {
		send = "Enter steer"
	}
	hints := send + " · Esc stop/back · Tab agents · Ctrl+O details · Shift+Tab mode · Ctrl+P commands"
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

func (m *Model) renderCommands() string {
	if !strings.HasPrefix(m.input.Value(), "/") {
		return ""
	}
	matches := m.commandMatches()
	if len(matches) == 0 {
		return ""
	}
	limit := min(m.commandWindow(), len(matches))
	selected := max(0, min(m.commandIndex, len(matches)-1))
	start := max(0, min(selected-limit/2, len(matches)-limit))
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

// renderModal builds the dialog inside an explicit row budget derived from the
// terminal height, so the box always fits: on short terminals it drops the
// vertical padding, blank separators, and description row, and it truncates
// the message before it can squeeze out the choices or the hint.
func (m *Model) renderModal() string {
	modal := m.modal
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
		lines = append(lines, m.palette.text.Render(line))
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
			label := style.Render(marker + " " + oneLine(choice.Label, width-10))
			if choice.Recommended {
				label += " " + m.palette.brand.Render("current")
			}
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
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#43D17D")).Background(lipgloss.Color("#121A16")).Padding(pad, 2).Width(width).Render(strings.Join(lines, "\n"))
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
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom {
		m.viewport.GotoBottom()
	}
}

const transcriptGutter = "  "

func (m *Model) renderTranscript() string {
	items := m.items
	if m.viewAgent != "" {
		if agent := m.agentByID[m.viewAgent]; agent != nil {
			items = append([]item{{kind: "system", content: "Agent task: " + agent.task}}, agent.items...)
		}
	}
	// Reserve the left gutter and a small right margin so text never touches
	// either edge of the terminal.
	width := max(20, m.viewport.Width()-len(transcriptGutter)-1)
	var blocks []string
	for _, entry := range items {
		switch entry.kind {
		case "user":
			// Claude Code style: a "> " marker with the highlight covering
			// only the prompt text itself — no header, no full-width band.
			content := RenderRTL(entry.content, m.runtime.Settings.RTL.Mode)
			marker := m.palette.brandSoft.Render("> ")
			if entry.title != "" {
				marker = m.palette.faint.Render(entry.title+" ") + marker
			}
			wrapped := wrapPlain(content, max(10, width-4))
			for i, line := range wrapped {
				wrapped[i] = m.palette.surface.Padding(0, 1).Render(line)
			}
			blocks = append(blocks, marker+strings.Join(wrapped, "\n  "))
		case "assistant", "assistant_draft":
			blocks = append(blocks, RenderMarkdown(entry.content, width, m.runtime.Settings.RTL.Mode, m.palette))
		case "system":
			blocks = append(blocks, m.palette.faint.Render("○ ")+RenderMarkdown(entry.content, width-2, m.runtime.Settings.RTL.Mode, m.palette))
		case "tool":
			blocks = append(blocks, m.renderTool(entry.tool, width))
		case "agent":
			if agent := m.agentByID[entry.agentID]; agent != nil {
				blocks = append(blocks, m.renderAgentChip(agent, width))
			}
		}
	}
	return indentLines(strings.Join(blocks, "\n\n"), transcriptGutter)
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

func (m *Model) renderTool(tool *toolView, width int) string {
	if tool == nil {
		return ""
	}
	marker := m.palette.brand.Render("●")
	if tool.state == "running" {
		marker = m.palette.brand.Render(spinnerFrames[m.frame%len(spinnerFrames)])
	} else if tool.state == "fail" {
		marker = m.palette.danger.Render("×")
	}
	line := marker + " " + m.palette.text.Render(toolLabel(tool.name))
	if tool.target != "" {
		line += " " + m.palette.brandSoft.Render(oneLine(tool.target, max(12, width/2)))
	}
	if tool.summary != "" {
		line += "  " + m.palette.faint.Render(oneLine(tool.summary, max(12, width/3)))
	}
	lines := []string{fitLine(line, width)}
	// Normal mode: a compact preview of the diff (first lines). Ctrl+O
	// (verbose): the complete diff with syntax colors, or the full raw output
	// for non-diff tools.
	if diff := diffLines(tool.output); len(diff) > 0 {
		visible := diff
		if !m.verbose && len(diff) > 8 {
			visible = diff[:8]
		}
		for _, source := range visible {
			lines = append(lines, "  "+m.diffLineStyle(source).Render(oneLine(source, width-2)))
		}
		if hidden := len(diff) - len(visible); hidden > 0 {
			lines = append(lines, "  "+m.palette.faint.Render(fmt.Sprintf("… %d more lines (Ctrl+O to expand)", hidden)))
		}
	} else if m.verbose && tool.output != "" {
		lines = append(lines, m.palette.surface2.Padding(0, 1).Width(width-2).Render(strings.Join(wrapPlain(tool.output, width-4), "\n")))
	}
	return strings.Join(lines, "\n")
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
	marker := m.palette.brand.Render("◆")
	if agent.status == "running" {
		marker = m.palette.brand.Render(spinnerFrames[m.frame%len(spinnerFrames)])
	} else if agent.status == "failed" || agent.status == "cancelled" {
		marker = m.palette.danger.Render("◆")
	}
	line := fmt.Sprintf("%s [%d] %s", marker, agent.index, m.palette.text.Render(agent.title))
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
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(max(3, m.height-used))
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

func toolTarget(_ string, input []byte) string {
	var values map[string]any
	_ = json.Unmarshal(input, &values)
	for _, key := range []string{"path", "command", "query", "pattern", "title", "agent"} {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
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
	if len(first) > 100 {
		first = first[:99] + "…"
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

func taskSummary(stats contract.TaskStats) string {
	parts := []string{formatDuration(time.Duration(stats.DurationMS) * time.Millisecond), string(stats.Effort), stats.TaskClass, formatTokens(stats.Usage.TotalTokens) + " billed", fmt.Sprintf("%d tools", stats.ToolCalls)}
	if stats.Usage.CacheReadTokens != nil && stats.Usage.CacheMissTokens != nil {
		parts = append(parts, formatTokens(*stats.Usage.CacheReadTokens)+" cached", formatTokens(*stats.Usage.CacheMissTokens)+" new")
	} else {
		parts = append(parts, "cache unavailable")
	}
	if stats.AgentRuns > 0 {
		parts = append(parts, fmt.Sprintf("%d agents", stats.AgentRuns))
	}
	if len(stats.FilesChanged) > 0 {
		parts = append(parts, fmt.Sprintf("%d files", len(stats.FilesChanged)))
	}
	if len(stats.Invalidations) > 0 {
		event := stats.Invalidations[len(stats.Invalidations)-1]
		parts = append(parts, fmt.Sprintf("cache prefix changed: %s (%s)", event.Cause, event.Scope))
	}
	return strings.Join(parts, " · ")
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
	value = strings.Join(strings.Fields(value), " ")
	if runewidth.StringWidth(value) <= width {
		return value
	}
	var result strings.Builder
	used := 0
	for _, char := range value {
		charWidth := runewidth.RuneWidth(char)
		if used+charWidth > width {
			break
		}
		result.WriteRune(char)
		used += charWidth
	}
	return result.String()
}

func lineCount(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}
