package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/updatecheck"
)

// renderHeader returns empty string; topbar is removed and brand identity
// is rendered in the empty session banner and bottom status bar.
func (m *Model) renderHeader() string {
	return ""
}

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
	if m.loading {
		return fitLine(" "+m.palette.brand.Render(m.spinner())+" "+m.palette.muted.Render("Loading workspace…"), m.width)
	}
	if !m.busy {
		return ""
	}
	mark := m.palette.brand.Render(m.spinner())
	sep := " " + m.palette.faint.Render(m.glyphs.bullet) + " "
	segs := []string{m.palette.muted.Render(m.status)}
	if !m.started.IsZero() {
		segs = append(segs, m.palette.faint.Render(formatDuration(time.Since(m.started))))
	}
	if m.usage.TotalTokens > 0 || m.usage.CacheReadTokens != nil {
		tokens, tag := headlineTokens(m.usage)
		segs = append(segs, m.palette.faint.Render(contract.FullTokens(tokens)+" tokens"))
		segs = append(segs, m.palette.brandSoft.Render(tag))
	}
	lines := []string{fitLine(" "+mark+" "+strings.Join(segs, sep), m.width)}
	return "\n" + strings.Join(lines, "\n")
}

func (m *Model) renderInput() string {
	if m.busy {
		m.input.Placeholder = "Type a message to steer · Esc to stop"
	} else {
		m.input.Placeholder = "Type a request · / for commands"
	}
	input := m.composerView(max(10, m.width-10))
	style := lipgloss.NewStyle().
		Border(m.glyphs.border).
		BorderForeground(m.palette.border.GetForeground()).
		Padding(0, 1).
		MarginLeft(2).
		Width(max(10, m.width-6))
	if m.busy {
		style = style.BorderForeground(m.palette.focus.GetForeground())
	}
	return style.Render(input)
}

// permissionCycleHint is the literal hint rendered beneath the permission chip,
// descaled inside brackets.
const permissionCycleHint = "(Shift + Tab to cycle)"

// renderModeLine renders 1 single unified status line:
// Left: version · workspace path · context meter
// Right: permission mode · cycle hint · reasoning effort
func (m *Model) renderModeLine() string {
	version := m.palette.faint.Render("v" + m.version)

	contextLabel := "context —"
	hasLimit := m.context.ContextLimit > 0
	if hasLimit {
		contextLabel = fmt.Sprintf("context %.1f%%", m.context.Percent)
	}
	meter := contextMeterStyle(m.palette, m.context.Percent, hasLimit).Render(contextLabel)

	workspace := m.runtime.Session.WorkspacePath
	if workspace == "" {
		workspace = "."
	}

	update := ""
	if updatecheck.Newer(m.latestVersion, m.version) {
		update = m.palette.brandSoft.Render("Update available "+m.latestVersion) + " " + m.palette.faint.Render("(you have "+m.version+")")
	}

	effort := contract.EffortLow
	if m.runtime.Settings != nil {
		effort = m.runtime.Settings.Effort
	}
	effortChip := effortStyle(m.palette, effort).Render(effortLabel(effort))

	cycleHint := m.renderCycleHint()
	permChip := m.permissionChip()

	rightEstW := lipgloss.Width(permChip) + lipgloss.Width(cycleHint) + lipgloss.Width(effortChip) + 6
	fixedLeftNoWorkspace := lipgloss.Width(version) + lipgloss.Width(meter) + lipgloss.Width(update) + 6

	if (update != "" && m.width < 110) || (m.width < 70 && len(workspace) > 35) {
		cycleHint = ""
		rightEstW = lipgloss.Width(permChip) + lipgloss.Width(effortChip) + 3
	}

	availForWorkspace := max(1, m.width-5-rightEstW-fixedLeftNoWorkspace)
	workspace = truncateLeft(workspace, availForWorkspace, m.glyphs.ellipsis)

	var right []string
	if n := len(m.selectedSkills); n > 0 {
		right = append(right, m.palette.brandSoft.Render(fmt.Sprintf("%d skills", n)))
	}
	right = append(right, permChip)
	if cycleHint != "" {
		right = append(right, cycleHint)
	}
	right = append(right, effortChip)

	dotSep := " " + m.palette.border.Render(m.glyphs.bullet) + " "
	faintSep := " " + m.palette.faint.Render(m.glyphs.bullet) + " "

	rightStr := strings.Join(right, dotSep)

	left := []string{version, m.palette.muted.Render(workspace)}
	if update != "" {
		left = append(left, update)
	}
	left = append(left, meter)

	return "  " + splitLineParts(left, rightStr, faintSep, max(1, m.width-4))
}

// permissionChip names the current mode: muted for the default, warning-colored
// for auto-accept so the looser mode stays visually distinct at a glance.
func (m *Model) permissionChip() string {
	mode := contract.PermissionNormal
	if m.runtime.Settings != nil {
		mode = m.runtime.Settings.PermissionMode
	}
	if mode == contract.PermissionAutoAccept {
		return m.palette.warning.Render(string(contract.PermissionAutoAccept))
	}
	return m.palette.muted.Render(string(contract.PermissionNormal))
}

func (m *Model) renderCycleHint() string {
	return m.palette.faint.Render(permissionCycleHint)
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
