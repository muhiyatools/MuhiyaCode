package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

func (m *Model) renderHeader() string {
	contextLabel := "context —"
	hasLimit := m.context.ContextLimit > 0
	if hasLimit {
		contextLabel = fmt.Sprintf("context %.1f%%", m.context.Percent)
	}
	dot := m.palette.border.Render(m.glyphs.bullet)
	// Header segments as a slice so a narrow width can drop trailing segments
	// (visual-system.md §4) before the path is truncated. Model names are
	// deliberately absent: models are chosen for the session, not managed by the
	// user, so naming them here would be noise. /context discloses them.
	seg := []string{
		" " + m.palette.brand.Render(m.glyphs.brand+" MuhiyaCode") + " " + m.palette.faint.Render("v"+m.version),
	}
	seg = append(seg, dot+" "+contextMeterStyle(m.palette, m.context.Percent, hasLimit).Render(contextLabel))
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
				parts = append(parts, m.palette.faint.Render(contract.HumanTokens(agent.usage.TotalTokens)+" tokens"))
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
	if m.usage.TotalTokens > 0 {
		// 008 T019 (UD-1..3): the live headline shows the honest per-task figure
		// (miss+completion when cache metrics exist, TotalTokens otherwise) and a
		// percentage-only cache tag — the read/new split lives in /context (UD-2).
		tokens, tag := headlineTokens(m.usage)
		segs = append(segs, m.palette.faint.Render(contract.HumanTokens(tokens)+" tokens"))
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

// renderModeLine sits directly below the input box: contextual key hints on the
// left, and a state cluster (plan / goal / queued skills / permission) plus the
// always-present reasoning-effort chip flush against the right edge (Experience
// Overhaul A1). The default "normal" permission mode shows no badge, so the footer
// stays quiet until something is actually active. The right cluster has layout
// priority: when the line is narrow, left hints drop first.
func (m *Model) renderModeLine() string {
	hints := []string{
		m.palette.faint.Render("Esc stop"),
		m.palette.faint.Render("Tab agents"),
		m.palette.faint.Render("Ctrl+T to-dos"),
		m.palette.faint.Render("/ commands"),
	}
	// Right cluster, least-specific first; the effort chip is appended last so it
	// sits at the far right ("top-right beneath the input").
	var right []string
	if n := len(m.selectedSkills); n > 0 {
		right = append(right, m.palette.brandSoft.Render(fmt.Sprintf("%d skills", n)))
	}
	if m.runtime.Settings != nil && m.runtime.Settings.PermissionMode == contract.PermissionAutoAccept {
		right = append(right, m.palette.warning.Render("auto-accept"))
	}
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
	return " " + splitLineParts(hints, strings.Join(right, clusterSep), hintSep, max(1, m.width-3))
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
