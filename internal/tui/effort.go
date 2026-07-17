package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muhiya/muhiyacode/internal/contract"
)

// effortLabel is the capitalized display name for a reasoning-effort level, shown
// as the colored chip at the right end of the mode line (Experience Overhaul A1).
// The word "reasoning" is deliberately absent — the chip shows only the level
// name (Low/Medium/High/Max). An unknown value is capitalized as-is.
func effortLabel(effort contract.EffortLevel) string {
	s := strings.TrimSpace(string(effort))
	if s == "" {
		return "—"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// effortStyle maps each effort level to a palette token so the chip's color
// signals depth at a glance: Low is faint, Medium info-blue, High warning-amber,
// Max the brand accent (bold, since palette.brand carries Bold). It uses only the
// existing adaptive palette tokens, so light/dark and NO_COLOR keep working
// without a new color literal. Unknown levels fall back to text.
func effortStyle(colors palette, effort contract.EffortLevel) lipgloss.Style {
	switch effort {
	case contract.EffortLow:
		return colors.faint
	case contract.EffortMedium:
		return colors.info
	case contract.EffortHigh:
		return colors.warning
	case contract.EffortMax:
		return colors.brand
	default:
		return colors.text
	}
}

// contextMeterStyle colors the header context indicator by how full the window is
// (Experience Overhaul A1/D9): calm below 60% used, warning at 60–85%, danger
// above 85%. report.Percent is the used share ("% full"), so a higher value is
// more urgent. hasLimit is false when no context limit is known, in which case the
// indicator stays neutral (no ramp).
func contextMeterStyle(colors palette, percent float64, hasLimit bool) lipgloss.Style {
	if !hasLimit {
		return colors.text
	}
	switch {
	case percent >= 85:
		return colors.danger
	case percent >= 60:
		return colors.warning
	default:
		return colors.text
	}
}
