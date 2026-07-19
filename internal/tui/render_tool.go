package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// isPathTargetTool reports whether a tool's target is a filesystem path, and so
// should be middle-truncated (filename preserved) rather than head-truncated.
func isPathTargetTool(name string) bool {
	switch name {
	case "edit_file", "multi_edit", "write_file", "apply_patch", "read_file", "git_diff":
		return true
	}
	return false
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
	// 008 T031 (MT-11): a Memory row without a captured title still names what
	// was remembered — the first words of the entry line from the output body
	// (saves and edits both echo the entry under their status line).
	if target == "" && (tool.name == "save_memory" || tool.name == "edit_memory") {
		target = memoryEntryHead(tool.output)
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
	case "save_memory", "edit_memory":
		// 008 T031 (MT-13): the Memory row's outcome is the entry state reported
		// on the tool's first output line ("saved" / "already known" /
		// "updated" / "removed" / "merged — …"); it is deliberately excluded
		// from the diff-count case above so a memory mutation can never render
		// "+N −N". Failures keep the standard danger styling via
		// styledOutcome's state check.
		return summarizeTool(tool.output)
	case "recall_memory":
		// Experience Overhaul B3: a recall reads a topic file; the outcome is a
		// simple "recalled" marker (not the topic body, and never a diff count).
		if tool.state == "fail" {
			return summarizeTool(tool.output)
		}
		return "recalled"
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
	case "run_shell":
		return shellOutcome(tool.output)
	}
	return summarizeTool(tool.output)
}

// shellOutcome renders the run_shell row's collapsed measure. run_shell output is
// "exit code: N\n<captured output>". A successful command (exit 0) shows a short
// summary of its output — or "ok" when it printed nothing — instead of the noisy
// "exit code: 0"; a non-zero exit shows a tight "exit N" so the failure signal
// stays visible without the verbose prefix. Output that does not match the
// envelope falls back to the generic first-line summary.
func shellOutcome(output string) string {
	code, rest, ok := parseExitCode(output)
	if !ok {
		return summarizeTool(output)
	}
	if code != 0 {
		return fmt.Sprintf("exit %d", code)
	}
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) != "" {
			return summarizeTool(line)
		}
	}
	return "ok"
}

// parseExitCode splits the run_shell "exit code: N\n<rest>" envelope. ok is false
// when the output does not lead with that prefix and a parseable integer, so any
// other format falls back to the generic summary.
func parseExitCode(output string) (code int, rest string, ok bool) {
	const prefix = "exit code: "
	if !strings.HasPrefix(output, prefix) {
		return 0, "", false
	}
	line := output[len(prefix):]
	numStr := line
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		numStr, rest = line[:nl], line[nl+1:]
	}
	n, err := strconv.Atoi(strings.TrimSpace(numStr))
	if err != nil {
		return 0, "", false
	}
	return n, rest, true
}

// diffCounts delegates to the shared contract helper (feature 008 T023) so the
// per-row counts and the engine's session-cumulative lines± can never drift.
func diffCounts(output string) (add, remove int, ok bool) {
	return contract.DiffCounts(output)
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
	identity := agent.agent
	if agent.phase != "" && agent.phase != string(contract.LifecycleDirect) {
		identity = agent.phase + "/" + agent.role
	}
	label := identity + " · " + agent.status
	if agent.model != "" {
		label = identity + " (" + agent.model + ") · " + agent.status
	}
	line += "  " + m.palette.faint.Render(label)
	if agent.usage.TotalTokens > 0 {
		line += "  " + m.palette.faint.Render(contract.HumanTokens(agent.usage.TotalTokens))
	}
	if agent.active != "" {
		line += "\n  " + m.palette.faint.Render("└ "+agent.active)
	}
	return fitLine(line, width)
}

// deriveToolTarget recovers a path-target tool's file from its diff output when
// no persisted target is available (a tool row from a session that predates the
// events.target column). It reuses contract.PatchTargetFile on the embedded
// unified diff so an Edit/Write/Patch row still names its file in the main line.
func deriveToolTarget(name, output string) string {
	if !isPathTargetTool(name) {
		return ""
	}
	diff := diffLines(output)
	if len(diff) == 0 {
		return ""
	}
	return contract.PatchTargetFile(strings.Join(diff, "\n"))
}

func toolLabel(name string) string {
	labels := map[string]string{"list_files": "List", "read_file": "Read", "grep": "Grep", "search_text": "Search", "glob": "Glob", "edit_file": "Edit", "multi_edit": "Edit", "write_file": "Write", "apply_patch": "Patch", "run_shell": "Shell", "git_status": "Git status", "git_diff": "Git diff", "update_plan": "To-dos", "ask_user": "Question", "propose_changes": "Change plan", "run_subagent": "Delegate", "web_search": "Web search", "save_memory": "Memory", "recall_memory": "Memory", "edit_memory": "Memory"}
	if label := labels[name]; label != "" {
		return label
	}
	if strings.HasPrefix(name, "mcp__") {
		return "MCP · " + strings.ReplaceAll(strings.TrimPrefix(name, "mcp__"), "__", " / ")
	}
	return contract.TitleWords(strings.ReplaceAll(name, "_", " "))
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

// memoryEntryHead recovers the collapsed Memory row's headline when no save
// title was captured (008 T031, memory-tool.md MT-11): the first words of the
// saved entry — the first output line that is not the bare state word — so the
// row never reads "Memory saved  saved". Returns "" when the output carries no
// entry body (the outcome measure still shows the state).
func memoryEntryHead(output string) string {
	fallback := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "saved") || strings.EqualFold(line, "already known") {
			continue
		}
		// The entry itself is a "- …" bullet (edits echo it under a status line
		// like "updated"); prefer it over any status text.
		if strings.HasPrefix(line, "- ") {
			return summarizeTool(line)
		}
		if fallback == "" {
			fallback = summarizeTool(line)
		}
	}
	return fallback
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
	return contract.DiffLines(output)
}
