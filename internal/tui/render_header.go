package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/updatecheck"
)

func (m *Model) renderHeader() string {
	contextLabel := "context —"
	hasLimit := m.context.ContextLimit > 0
	if hasLimit {
		contextLabel = fmt.Sprintf("context %.1f%%", m.context.Percent)
	}
	dot := m.palette.border.Render(m.glyphs.bullet)
	// Line 1 carries identity, location, and pressure: brand+version, the
	// workspace path, an update notice when there is one, and the context meter.
	// Model names are deliberately absent — models are chosen for the session,
	// not managed by the user, so naming them here would be noise (/context
	// discloses them).
	brand := " " + m.palette.brand.Render("MuhiyaCode") + " " + m.palette.faint.Render("v"+m.version)
	update := ""
	if updatecheck.Newer(m.latestVersion, m.version) {
		update = dot + " " + m.palette.brandSoft.Render("Update available "+m.latestVersion) + " " + m.palette.faint.Render("(you have "+m.version+")")
	}
	meter := dot + " " + contextMeterStyle(m.palette, m.context.Percent, hasLimit).Render(contextLabel)
	// The path is the FLEXIBLE segment: the fixed segments are measured first and
	// the path takes whatever is left, left-truncated (leading ellipsis) so the
	// most-specific directories survive (FR-021). The context meter is never
	// pushed off by a long path.
	workspace := m.runtime.Session.WorkspacePath
	if workspace == "" {
		workspace = "."
	}
	fixed := lipgloss.Width(brand) + lipgloss.Width(meter) + lipgloss.Width(update) + 8 // separators
	workspace = truncateLeft(workspace, max(12, m.width-fixed), m.glyphs.ellipsis)
	seg := []string{brand, dot + " " + m.palette.muted.Render(workspace)}
	if update != "" {
		seg = append(seg, update)
	}
	seg = append(seg, meter)
	line1 := strings.Join(seg, "  ")
	rule := m.palette.border.Render(strings.Repeat(m.glyphs.ruleH, max(0, m.width)))
	// One blank spacer row above the brand line keeps the top bar off the
	// terminal's upper edge. layout() counts rendered lines, so the spacer is
	// automatically part of the height budget.
	//
	// The header is now a fixed three rows. The running-agent count and the
	// focused-agent line it could expand into are gone with the subagents they
	// described: one session has one identity row.
	return strings.Join([]string{"", fitLine(line1, m.width), rule}, "\n")
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
	// Status line: spinner + verb + elapsed + tokens + cache tag, segments joined
	// by a faint " · " (Experience Overhaul A2). The status verb is muted; elapsed
	// and tokens are faint; the cache tag carries the brand-soft accent.
	sep := " " + m.palette.faint.Render(m.glyphs.bullet) + " "
	segs := []string{m.palette.muted.Render(m.status)}
	if !m.started.IsZero() {
		segs = append(segs, m.palette.faint.Render(formatDuration(time.Since(m.started))))
	}
	// B-4: also render when only cache fields are reported (TotalTokens derived to
	// 0 but the provider still told us about cache), so the token+cache segment
	// does not vanish for a cache-only payload.
	if m.usage.TotalTokens > 0 || m.usage.CacheReadTokens != nil {
		// 008 T019 (UD-1..3): the live headline shows the honest per-task figure
		// (miss+completion when cache metrics exist, TotalTokens otherwise) and a
		// percentage-only cache tag — the read/new split lives in /context (UD-2).
		tokens, tag := headlineTokens(m.usage)
		segs = append(segs, m.palette.faint.Render(contract.FullTokens(tokens)+" tokens"))
		segs = append(segs, m.palette.brandSoft.Render(tag))
	}
	lines := []string{fitLine(" "+mark+" "+strings.Join(segs, sep), m.width)}
	// The live reasoning tail that rendered here is gone (Fix R3): raw model
	// thinking must never appear on screen. The spinner + status verb are the
	// whole "working" indication, exactly like the removed " thinking… " label
	// before it (Experience Overhaul A2, D2). The old "plan N/M" progress bar is
	// likewise replaced by the dedicated to-do checklist panel (renderTodos),
	// rendered as its own View() section below the activity zone (A3, D3).
	//
	// A leading blank line separates the activity zone from the transcript/notice
	// above it (Experience Overhaul A2 spacing contract). renderActivity owns this
	// margin so View() never adds ad-hoc separators around it.
	return "\n" + strings.Join(lines, "\n")
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

// permissionCycleHint is the literal hint rendered beneath the permission chip
// (013 FR-015). With /permissions removed, this line is the ONLY thing telling a
// user the mode can be changed at all, so it renders in every mode.
const permissionCycleHint = "Shift + Tab to cycle"

// renderModeLine sits directly below the input box: contextual key hints on the
// left, and a state cluster (queued skills / permission mode) plus the
// always-present reasoning-effort chip flush against the right edge (Experience
// Overhaul A1). The right cluster has layout priority: when the line is narrow,
// left hints drop first.
//
// 013 changed two things here. The permission chip now renders in EVERY mode,
// not just auto-accept — it is the anchor for the cycle hint below, and a mode
// you cannot see is a mode you cannot reason about. And the hints name only
// bindings that exist: the arrow-key agent switcher went with the subagents,
// and the to-do panel has no toggle to advertise.
func (m *Model) renderModeLine() string {
	hints := []string{
		m.palette.faint.Render("Esc stop"),
		m.palette.faint.Render("/ commands"),
	}
	// Right cluster, least-specific first; the effort chip is appended last so it
	// sits at the far right ("top-right beneath the input").
	var right []string
	if n := len(m.selectedSkills); n > 0 {
		right = append(right, m.palette.brandSoft.Render(fmt.Sprintf("%d skills", n)))
	}
	right = append(right, m.permissionChip())
	effort := contract.EffortLow
	if m.runtime.Settings != nil {
		effort = m.runtime.Settings.Effort
	}
	right = append(right, effortStyle(m.palette, effort).Render(effortLabel(effort)))
	hintSep := " " + m.palette.faint.Render(m.glyphs.bullet) + " "
	clusterSep := " " + m.palette.border.Render(m.glyphs.bullet) + " "
	// A 1-column left margin and a 2-column right margin (Ultimate Polish U1): the
	// split fills m.width-3 columns, so with the leading space the effort chip ends
	// at column W-2 — aligned with the composer's content right edge (the input box
	// is border+pad+content over columns 1..W), not jammed against the terminal edge.
	line := " " + splitLineParts(hints, strings.Join(right, clusterSep), hintSep, max(1, m.width-3))
	if hint := m.renderCycleHint(); hint != "" {
		line += "\n" + hint
	}
	return line
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

// renderCycleHint puts the shortcut directly beneath the permission chip,
// right-aligned to the same edge. It is the first thing to go on a narrow
// terminal: losing a hint is survivable, losing the mode itself is not.
func (m *Model) renderCycleHint() string {
	hint := m.palette.faint.Render(permissionCycleHint)
	// Right-align the hint under the PERMISSION chip, not the effort chip (FR-015 /
	// H-4): the effort chip and its separator sit to the permission chip's right, so
	// subtract their width from the full right edge.
	effort := contract.EffortLow
	if m.runtime.Settings != nil {
		effort = m.runtime.Settings.Effort
	}
	effortWidth := lipgloss.Width(effortStyle(m.palette, effort).Render(effortLabel(effort)))
	clusterSepWidth := lipgloss.Width(" " + m.palette.border.Render(m.glyphs.bullet) + " ")
	width := max(1, m.width-3-effortWidth-clusterSepWidth)
	if lipgloss.Width(hint) > width {
		return ""
	}
	return " " + splitLineParts(nil, hint, "", width)
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
