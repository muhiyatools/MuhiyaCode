package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// planBarPattern is the old activity "plan N/M" bar wording that the to-do panel
// replaced (Experience Overhaul A3). It must never reappear in a rendered frame.
var planBarPattern = regexp.MustCompile(`plan \d+/\d+`)

// busyLanguageFixture builds a busy model with a live status, a two-step plan,
// and max effort — the worst case for the removed-language guard. (The live
// reasoning tail is gone entirely — Fix R3 — so no thinking text can render.)
func busyLanguageFixture(t *testing.T) *Model {
	t.Helper()
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	_ = m.Init()
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	m.runtime.Settings.Effort = contract.EffortMax
	m.busy = true
	m.status = "Working…"
	m.plan = contract.Plan{Steps: []contract.PlanStep{
		{Title: "Move the effort chip", Status: contract.PlanCompleted},
		{Title: "Wire the to-do panel", Status: contract.PlanInProgress},
	}}
	return m
}

// TestRenderLanguageForbidsRemovedStrings pins INV-7: the "Thinking…" status and
// the " thinking… " tail label are gone from the rendered surface, and the header
// no longer carries a reasoning segment (Experience Overhaul A2/A3). The plan-bar
// "plan N/M" forbid is added in T031 once the to-do panel replaces the bar.
func TestRenderLanguageForbidsRemovedStrings(t *testing.T) {
	view := busyLanguageFixture(t).View().Content
	for _, banned := range []string{"Thinking…", "thinking…"} {
		if strings.Contains(view, banned) {
			t.Errorf("removed string %q still renders:\n%s", banned, view)
		}
	}
	// The word "reasoning" left the header entirely (it lives on the footer chip as
	// the bare level name now). The fixture uses no "reasoning" content, so any hit
	// is a regression.
	if strings.Contains(view, "reasoning") {
		t.Errorf("header/footer still shows a 'reasoning' label:\n%s", view)
	}
	// The old "plan N/M" activity bar is gone — progress lives in the to-do panel.
	if planBarPattern.MatchString(view) {
		t.Errorf("old plan-bar wording still renders (use the to-do panel):\n%s", view)
	}
}

// TestRenderLanguageShowsEffortChip proves the footer carries the bare capitalized
// effort level and that the status/footer use the " · " separator (A2/A1).
func TestRenderLanguageShowsEffortChip(t *testing.T) {
	view := busyLanguageFixture(t).View().Content
	if !strings.Contains(view, "Max") {
		t.Errorf("effort chip label 'Max' missing:\n%s", view)
	}
	if !strings.Contains(view, "·") {
		t.Errorf("expected ' · ' separators in the frame:\n%s", view)
	}
	// The activity zone must be set off by at least one blank line (spacing contract).
	if !strings.Contains(view, "\n\n") {
		t.Errorf("expected blank-line separation in the busy frame:\n%s", view)
	}
}
