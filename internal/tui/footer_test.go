package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// TestSplitLineParts covers the two-column footer layout helper (A1 T010): exact
// fit, right-priority hint dropping, right-only fallback, ANSI-aware width, and
// the zero-width guard.
func TestSplitLineParts(t *testing.T) {
	// Exact fit: "a-b" (3) + 1 gap + "R" (1) = width 5.
	if got := splitLineParts([]string{"a", "b"}, "R", "-", 5); got != "a-b R" {
		t.Fatalf("exact fit: got %q", got)
	}
	// Overflow drops the trailing left part; the remainder stays right-flush.
	if got := splitLineParts([]string{"aa", "bb"}, "R", "-", 6); got != "aa   R" {
		t.Fatalf("hint drop: got %q (width %d)", got, ansi.StringWidth(got))
	}
	// No left part fits alongside the right cluster → right flush-right only.
	if got := splitLineParts([]string{"aaaa"}, "R", "-", 3); got != "  R" {
		t.Fatalf("right-only: got %q", got)
	}
	// Zero width is a clean empty string, never a panic.
	if got := splitLineParts([]string{"a"}, "R", "-", 0); got != "" {
		t.Fatalf("zero width: got %q", got)
	}
	// ANSI escapes count as zero width: a styled right of visible width 1 still
	// leaves room for the left part at width 5.
	styledR := "\x1b[31mR\x1b[0m"
	got := splitLineParts([]string{"ab"}, styledR, "-", 5)
	if ansi.StringWidth(got) != 5 || !strings.Contains(got, "ab") || !strings.HasSuffix(got, styledR) {
		t.Fatalf("ansi-aware: got %q width %d", got, ansi.StringWidth(got))
	}
}

// TestEffortLabel pins the chip label: capitalized level name only, no "reasoning"
// word, with an em dash for the empty value (A1 T011).
func TestEffortLabel(t *testing.T) {
	cases := map[contract.EffortLevel]string{
		contract.EffortLow:    "Low",
		contract.EffortMedium: "Medium",
		contract.EffortHigh:   "High",
		contract.EffortMax:    "Max",
		"":                    "—",
		"turbo":               "Turbo",
	}
	for in, want := range cases {
		if got := effortLabel(in); got != want {
			t.Errorf("effortLabel(%q) = %q, want %q", in, got, want)
		}
		if strings.Contains(strings.ToLower(effortLabel(in)), "reasoning") {
			t.Errorf("effortLabel(%q) leaked the word 'reasoning'", in)
		}
	}
}

// TestEffortStyle proves each level maps to its distinct palette token (A1 T011).
func TestEffortStyle(t *testing.T) {
	// Ambient NO_COLOR strips every foreground, so each assertion below would
	// compare nil to nil and pass without testing anything (visibility_test.go
	// clears it for the same reason). Pin the color path explicitly.
	t.Setenv("NO_COLOR", "")
	colors := newPalette("")
	fg := func(effort contract.EffortLevel) any { return effortStyle(colors, effort).GetForeground() }
	if !reflect.DeepEqual(fg(contract.EffortLow), colors.faint.GetForeground()) {
		t.Error("low should be faint")
	}
	if !reflect.DeepEqual(fg(contract.EffortMedium), colors.info.GetForeground()) {
		t.Error("medium should be info")
	}
	if !reflect.DeepEqual(fg(contract.EffortHigh), colors.warning.GetForeground()) {
		t.Error("high should be warning")
	}
	if !reflect.DeepEqual(fg(contract.EffortMax), colors.brand.GetForeground()) {
		t.Error("max should be brand")
	}
	// Low and Max must be visibly different colors.
	if reflect.DeepEqual(fg(contract.EffortLow), fg(contract.EffortMax)) {
		t.Error("low and max chips must differ in color")
	}
}

// TestContextMeterStyle pins the urgency ramp thresholds against used share (A1 T015).
func TestContextMeterStyle(t *testing.T) {
	t.Setenv("NO_COLOR", "") // as above: without this the ramp is untested, not tested-and-passing
	colors := newPalette("")
	fg := func(pct float64, has bool) any { return contextMeterStyle(colors, pct, has).GetForeground() }
	if !reflect.DeepEqual(fg(59, true), colors.text.GetForeground()) {
		t.Error("59% should be calm/text")
	}
	if !reflect.DeepEqual(fg(60, true), colors.warning.GetForeground()) {
		t.Error("60% should ramp to warning")
	}
	if !reflect.DeepEqual(fg(85, true), colors.danger.GetForeground()) {
		t.Error("85% should be danger")
	}
	if !reflect.DeepEqual(fg(86, true), colors.danger.GetForeground()) {
		t.Error("86% should be danger")
	}
	// No known limit → neutral, regardless of the (meaningless) percent.
	if !reflect.DeepEqual(fg(99, false), colors.text.GetForeground()) {
		t.Error("no limit should stay neutral")
	}
}

// TestModeLineFooterLayout proves the mode line leaves a 2-column right margin so
// the effort chip aligns with the composer content edge instead of the terminal
// edge (Ultimate Polish U1), and always ends with the chip (A1 T012). Since 013
// the pane is two lines — chips, then the Shift+Tab hint beneath them — so the
// margin invariant is asserted per line.
func TestModeLineFooterLayout(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		_ = m.Init()
		m = mustUpdate(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
		m.runtime.Settings.Effort = contract.EffortMax
		pane := m.renderModeLine()
		lines := strings.Split(pane, "\n")
		for i, line := range lines {
			// U1: never past column W-2 — a 2-column right margin at every width.
			if w := ansi.StringWidth(line); w > width-2 {
				t.Fatalf("width %d line %d lacks the 2-col right margin (%d): %q", width, i, w, line)
			}
		}
		chips := lines[0]
		// At a wide terminal the whole cluster fits, so the chip ends exactly at W-2.
		if width == 120 {
			if w := ansi.StringWidth(chips); w != width-2 {
				t.Fatalf("width %d: chip should end at column %d, line width %d: %q", width, width-2, w, chips)
			}
		}
		if !strings.Contains(chips, "Max") {
			t.Fatalf("width %d: mode line missing the effort chip: %q", width, chips)
		}
		// 013 FR-015: the mode is now named in every mode, with the cycle hint
		// beneath it — that pair is the only remaining way to discover Shift+Tab.
		if !strings.Contains(chips, "normal") {
			t.Fatalf("width %d: permission mode must always be shown: %q", width, chips)
		}
		if strings.Contains(chips, "auto-accept") {
			t.Fatalf("width %d: normal mode must not read as auto-accept: %q", width, chips)
		}
		if len(lines) < 2 || !strings.Contains(lines[1], "Shift + Tab to cycle") {
			t.Fatalf("width %d: cycle hint missing beneath the chips: %q", width, pane)
		}
	}
}

// TestModeLineShowsAutoAcceptDistinctly: the looser mode has to be visually
// unmistakable, since it is the one where the agent stops asking.
func TestModeLineShowsAutoAccept(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	_ = m.Init()
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.runtime.Settings.PermissionMode = contract.PermissionAutoAccept
	line := m.renderModeLine()
	if !strings.Contains(line, "auto-accept") {
		t.Fatalf("auto-accept mode not shown: %q", line)
	}
	if strings.Contains(strings.SplitN(line, "\n", 2)[0], "normal") {
		t.Fatalf("both modes rendered at once: %q", line)
	}
}
