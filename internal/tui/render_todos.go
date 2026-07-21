package tui

import (
	"fmt"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// todoMaxRows is the checklist row budget shown above the input (Experience
// Overhaul A3). Beyond it, completed steps roll up and any remaining overflow
// collapses to a "… N more" line, mirroring Claude Code's five-visible convention.
const todoMaxRows = 5

// renderTodos draws the live to-do checklist under the activity zone while a task
// runs, or a compact resume reminder when idle with a saved/interrupted plan. The
// to-dos ARE the plan steps re-surfaced — there is no new state or persistence:
// the panel reads m.plan.Steps and the engine lifecycle, so it retires itself the
// moment the plan reaches a terminal state (Experience Overhaul A3, replacing the
// old "plan N/M" bar).
func (m *Model) renderTodos() string {
	steps := m.plan.Steps
	if len(steps) == 0 {
		return ""
	}
	// Every item checked off means the checklist is done; the panel retires
	// rather than showing a wall of completed rows.
	remaining := 0
	for _, s := range steps {
		if s.Status != contract.PlanCompleted {
			remaining++
		}
	}
	if remaining == 0 {
		return ""
	}
	if !m.busy {
		// Idle with open items: one faint line, not the whole checklist.
		line := fmt.Sprintf("   %s %d to-dos remaining in tasks.md", m.glyphs.todoPending, remaining)
		return "\n" + fitLine(m.palette.faint.Render(line), m.width)
	}
	// Busy: the live checklist, always (013 FR-025/FR-026). The Ctrl+T hide is
	// gone — while the agent is working, what it is working through is exactly
	// what the user wants on screen, so hiding it was a setting nobody needed.
	rows := m.todoRows(steps)
	if len(rows) == 0 {
		return ""
	}
	// A leading blank line separates the panel from the activity zone above it
	// (Experience Overhaul A3 spacing contract); renderTodos owns this margin.
	return "\n" + strings.Join(rows, "\n")
}

// todoRows applies the collapse algorithm: at most todoMaxRows lines, with
// completed steps rolled up when there are at least two, and any leftover overflow
// summarized as a trailing "… N more" line. Every returned row is already fit to
// the terminal width.
func (m *Model) todoRows(steps []contract.PlanStep) []string {
	done := 0
	for _, s := range steps {
		if s.Status == contract.PlanCompleted {
			done++
		}
	}
	// Under budget: show every step in plan order.
	if len(steps) <= todoMaxRows {
		rows := make([]string, 0, len(steps))
		for _, s := range steps {
			rows = append(rows, m.todoRow(s))
		}
		return rows
	}
	// Over budget: build display entries, each recording how many steps it covers,
	// so the "… N more" tail can report an exact hidden count.
	type entry struct {
		text   string
		covers int
	}
	var entries []entry
	if done >= 2 {
		roll := fmt.Sprintf("%s %d done", m.glyphs.todoDone, done)
		entries = append(entries, entry{fitLine("   "+m.palette.faint.Render(roll), m.width), done})
	}
	for _, s := range steps {
		if s.Status == contract.PlanCompleted && done >= 2 {
			continue // represented by the roll-up row above
		}
		entries = append(entries, entry{m.todoRow(s), 1})
	}
	if len(entries) <= todoMaxRows {
		rows := make([]string, len(entries))
		for i, e := range entries {
			rows[i] = e.text
		}
		return rows
	}
	// Keep the first todoMaxRows-1 entries, then collapse the rest into "… N more".
	rows := make([]string, 0, todoMaxRows)
	covered := 0
	for i := 0; i < todoMaxRows-1; i++ {
		rows = append(rows, entries[i].text)
		covered += entries[i].covers
	}
	hidden := len(steps) - covered
	more := fmt.Sprintf("%s %d more", m.glyphs.ellipsis, hidden)
	rows = append(rows, fitLine("   "+m.palette.faint.Render(more), m.width))
	return rows
}

// todoRow renders one checklist line: a state glyph plus the (RTL-shaped) title.
// Completed is faint, in-progress is bold with a brand glyph, pending is muted.
func (m *Model) todoRow(step contract.PlanStep) string {
	title := m.rtl(oneLine(step.Title, max(10, m.width-6)))
	switch step.Status {
	case contract.PlanInProgress:
		return fitLine("   "+m.palette.brand.Render(m.glyphs.todoActive)+" "+m.palette.text.Bold(true).Render(title), m.width)
	case contract.PlanCompleted:
		return fitLine("   "+m.palette.faint.Render(m.glyphs.todoDone+" "+title), m.width)
	default:
		return fitLine("   "+m.palette.muted.Render(m.glyphs.todoPending+" "+title), m.width)
	}
}
